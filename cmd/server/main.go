package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	serverui "remlink/frontend/server"
	"remlink/internal/admin"
	"remlink/internal/bootstrap"
	"remlink/internal/config"
	"remlink/internal/control"
	"remlink/internal/database"
	"remlink/internal/ipam"
	"remlink/internal/logging"
	"remlink/internal/overlay/serverwg"
	sessionmanager "remlink/internal/session"
	"remlink/internal/version"
)

const wireGuardEndpointEnvironment = "REMLINK_WG_ENDPOINT"
const adminTokenEnvironment = "REMLINK_ADMIN_TOKEN"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "remlink-server: %v\n", err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	flags := flag.NewFlagSet("remlink-server", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	configPath := flags.String("config", "config/server.yaml", "Server YAML configuration path")
	wgEndpoint := flags.String("wg-endpoint", os.Getenv(wireGuardEndpointEnvironment), "public WireGuard host:port (or REMLINK_WG_ENDPOINT)")
	rotateJoinToken := flags.Bool("rotate-join-token", false, "rotate Join Token in SQLite, print it, and exit")
	printJoinToken := flags.Bool("print-join-token", false, "print current Join Token and exit")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	serverConfig, err := config.LoadServer(*configPath)
	if err != nil {
		return err
	}
	ctx := context.Background()
	databasePath := filepath.Join(serverConfig.Data.Directory, "remlink.db")
	db, err := database.Open(ctx, databasePath)
	if err != nil {
		return err
	}
	defer db.Close()
	store := database.NewStore(db)
	joins := bootstrap.NewJoinTokens(store)
	if *rotateJoinToken || *printJoinToken {
		var token string
		if *rotateJoinToken {
			token, err = joins.Rotate(ctx)
		} else {
			token, err = joins.Ensure(ctx)
		}
		if err != nil {
			return err
		}
		fmt.Println(token)
		return nil
	}
	closedSessions, err := store.CloseOpenSessions(ctx)
	if err != nil {
		return err
	}
	storedNetwork, err := admin.LoadStoredNetwork(ctx, store, admin.Network{
		OverlayCIDR: serverConfig.Network.OverlayCIDR, ServerOverlayIP: serverConfig.Network.ServerOverlayIP,
		WireGuardPort: serverConfig.Server.WireGuardPort, SessionUDPPort: serverConfig.Network.SessionUDPPort,
		MTU: serverConfig.Network.MTU, ConfigVersion: 1,
	})
	if err != nil {
		return err
	}
	serverConfig.Network.OverlayCIDR = storedNetwork.OverlayCIDR
	serverConfig.Network.ServerOverlayIP = storedNetwork.ServerOverlayIP
	serverConfig.Network.SessionUDPPort = storedNetwork.SessionUDPPort
	serverConfig.Network.MTU = storedNetwork.MTU
	serverConfig.Server.WireGuardPort = storedNetwork.WireGuardPort
	_, controlPortText, err := net.SplitHostPort(serverConfig.Server.ControlListen)
	if err != nil {
		return err
	}
	serverConfig.Server.ControlListen = net.JoinHostPort(storedNetwork.ServerOverlayIP, controlPortText)
	if err := validateEndpoint(*wgEndpoint, serverConfig.Server.WireGuardPort); err != nil {
		return err
	}

	logger, err := logging.New(logging.DefaultConfig(filepath.Join(serverConfig.Data.Directory, "logs", "server.jsonl")))
	if err != nil {
		return err
	}
	defer logger.Close()
	coreLogger, _ := logger.For(logging.ModuleCore)
	wgLogger, _ := logger.For(logging.ModuleWG)
	bootstrapLogger, _ := logger.For(logging.ModuleBootstrap)

	overlayCIDR, _ := netip.ParsePrefix(serverConfig.Network.OverlayCIDR)
	serverIP, _ := netip.ParseAddr(serverConfig.Network.ServerOverlayIP)
	serverAddress := netip.PrefixFrom(serverIP, overlayCIDR.Bits())
	wireGuard, err := serverwg.New(ctx, serverwg.Config{
		InterfaceName: serverwg.DefaultInterfaceName, Address: serverAddress,
		ListenPort:       serverConfig.Server.WireGuardPort,
		PrivateKeyPath:   filepath.Join(serverConfig.Data.Directory, "server-wg.key"),
		EnableForwarding: true,
	})
	if err != nil {
		return fmt.Errorf("kernel WireGuard preflight/configuration failed: %w", err)
	}
	defer wireGuard.Close()
	wgLogger.Info("内核 WireGuard 中心接口已就绪", "interface", serverwg.DefaultInterfaceName,
		"address", serverAddress, "listen_port", serverConfig.Server.WireGuardPort)

	nodes, err := store.ListNodes(ctx)
	if err != nil {
		return err
	}
	peers := make([]serverwg.Peer, 0, len(nodes))
	for _, node := range nodes {
		peers = append(peers, serverwg.Peer{PublicKey: node.WGPublicKey, Address: node.OverlayIP})
	}
	if err := wireGuard.ReconcilePeers(ctx, peers); err != nil {
		return err
	}
	wgLogger.Info("WireGuard 对等节点已完成同步", "count", len(peers))

	ipamManager, err := ipam.New(store, overlayCIDR, serverIP)
	if err != nil {
		return err
	}
	serverID, err := bootstrap.EnsureServerID(ctx, store)
	if err != nil {
		return err
	}
	joinToken, err := joins.Ensure(ctx)
	if err != nil {
		return err
	}
	bootstrapLogger.Info("Join Token 已就绪；可使用 Server 命令行查询或轮换", "token_initialized", joinToken != "")
	service, err := bootstrap.NewService(store, ipamManager, joins, wireGuard, bootstrap.ServiceConfig{
		ServerID: serverID, Version: version.String(), WGPublicKey: wireGuard.PublicKey(),
		WGEndpoint: *wgEndpoint, OverlayCIDR: overlayCIDR, ServerOverlayIP: serverIP,
		ControlURL:     "ws://" + serverConfig.Server.ControlListen + "/control",
		SessionUDPPort: serverConfig.Network.SessionUDPPort, MTU: serverConfig.Network.MTU,
		ConfigVersion: storedNetwork.ConfigVersion,
	})
	if err != nil {
		return err
	}

	controlHub, err := control.NewHub(service, store, nil, control.HubConfig{
		NetworkConfigVersion: storedNetwork.ConfigVersion, EnforceRemoteIP: true,
	})
	if err != nil {
		return err
	}
	sessionManager, err := sessionmanager.NewManager(store, controlHub, sessionmanager.Config{
		OverlayCIDR: overlayCIDR, MTU: serverConfig.Network.MTU, UDPPort: serverConfig.Network.SessionUDPPort,
	})
	if err != nil {
		return err
	}
	service.SetNodeBootstrapHandler(func(ctx context.Context, nodeID string) error {
		return sessionManager.DisconnectNode(ctx, nodeID, "NODE_RUNTIME_REBUILT")
	})
	controlHub.SetMessageHandler(sessionManager)
	coreLogger.Info("会话恢复完成", "closed_nonterminal_sessions", closedSessions)
	controlMux := http.NewServeMux()
	controlMux.Handle("/control", controlHub)
	_, controlPortText, _ = net.SplitHostPort(serverConfig.Server.ControlListen)
	controlPort, _ := strconv.Atoi(controlPortText)
	controlSupervisor, err := control.NewSupervisor(controlMux, controlPort)
	if err != nil {
		return err
	}
	runtimeContext, cancelRuntime := context.WithCancel(context.Background())
	defer cancelRuntime()
	if err := controlSupervisor.Start(runtimeContext, serverIP); err != nil {
		return err
	}
	defer controlSupervisor.Close()
	networkManager, err := admin.NewNetworkManager(store, ipamManager, wireGuard, service, controlHub, sessionManager,
		storedNetwork, func(address netip.Addr) error { return controlSupervisor.Rebind(address) })
	if err != nil {
		return err
	}
	adminHandler, err := admin.Handler(admin.HandlerConfig{
		Store: store, IPAM: ipamManager, Peers: wireGuard, Control: controlHub,
		Sessions: sessionManager, Network: networkManager, JoinTokens: joins,
		AdminToken: os.Getenv(adminTokenEnvironment),
	})
	if err != nil {
		return err
	}
	publicMux := http.NewServeMux()
	publicMux.Handle("/api/v1/admin/", adminHandler)
	bootstrapHandler := bootstrap.Handler(service)
	publicMux.Handle("/api/v1/server/", bootstrapHandler)
	publicMux.Handle("/api/v1/bootstrap/", bootstrapHandler)
	webUI, err := serverui.Handler()
	if err != nil {
		return err
	}
	publicMux.Handle("/", webUI)
	bootstrapServer := &http.Server{
		Addr: serverConfig.Server.HTTPListen, Handler: publicMux,
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}
	type runtimeError struct {
		component string
		err       error
	}
	serverErrors := make(chan runtimeError, 3)
	go func() {
		coreLogger.Info("公网 Bootstrap API 已开始监听", "address", bootstrapServer.Addr, "version", version.String())
		serverErrors <- runtimeError{component: "Bootstrap API", err: bootstrapServer.ListenAndServe()}
	}()
	go func() {
		coreLogger.Info("Overlay Control WebSocket 已开始监听", "address", serverConfig.Server.ControlListen)
		serverErrors <- runtimeError{component: "Control WebSocket", err: controlSupervisor.Wait(runtimeContext)}
	}()
	go func() {
		serverErrors <- runtimeError{component: "Control heartbeat monitor", err: controlHub.Run(runtimeContext)}
	}()

	signalContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case <-signalContext.Done():
		cancelRuntime()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := bootstrapServer.Shutdown(shutdownContext); err != nil {
			return fmt.Errorf("shutdown public Bootstrap API: %w", err)
		}
		if err := controlSupervisor.Close(); err != nil {
			return fmt.Errorf("shutdown Control WebSocket: %w", err)
		}
		coreLogger.Info("Server 已停止")
		return nil
	case failure := <-serverErrors:
		cancelRuntime()
		if errors.Is(failure.err, http.ErrServerClosed) || errors.Is(failure.err, context.Canceled) {
			return nil
		}
		return fmt.Errorf("%s failed: %w", failure.component, failure.err)
	}
}

func validateEndpoint(endpoint string, listenPort int) error {
	host, portText, err := net.SplitHostPort(endpoint)
	if err != nil || host == "" {
		return fmt.Errorf("--wg-endpoint (or %s) must be a public host:port", wireGuardEndpointEnvironment)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port != listenPort {
		return fmt.Errorf("public WireGuard endpoint port must match configured listen port %d", listenPort)
	}
	return nil
}
