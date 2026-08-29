//go:build windows

package nodeagent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"net/url"
	"time"

	"remlink/internal/bootstrap"
	"remlink/internal/control"
	"remlink/internal/identity"
	"remlink/internal/localization"
	"remlink/internal/logging"
	"remlink/internal/model"
	"remlink/internal/overlay/clientwg"
	windowsplatform "remlink/internal/platform/windows"
	"remlink/internal/platform/windows/dpapi"
	"remlink/internal/platform/windows/netinfo"
	"remlink/internal/platform/windows/route"
	"remlink/internal/protocol"
	sessionruntime "remlink/internal/session"
	netstackgateway "remlink/internal/subnetgateway/netstack"
)

// Run starts one Windows Node and rebuilds all network-derived state whenever
// the Server requires a new public Bootstrap configuration.
func Run(ctx context.Context, options Options) error {
	if err := options.validate(); err != nil {
		return err
	}
	bootstrapLogger := nodeLogger(options, logging.ModuleBootstrap)
	for {
		err := runOnce(ctx, options)
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
			return nil
		}
		if !errors.Is(err, protocol.ErrRebootstrapRequired) {
			return err
		}
		if bootstrapLogger != nil {
			bootstrapLogger.Info("正在根据公网 Bootstrap 配置重建节点网络运行时")
		}
	}
}

func runOnce(ctx context.Context, options Options) error {
	bootstrapLogger := nodeLogger(options, logging.ModuleBootstrap)
	wgLogger := nodeLogger(options, logging.ModuleWG)
	controlLogger := nodeLogger(options, logging.ModuleControl)
	sessionLogger := nodeLogger(options, logging.ModuleSession)
	routeLogger := nodeLogger(options, logging.ModuleRoute)
	identityStore, err := identity.NewStore(options.IdentityPath, dpapi.Protector{})
	if err != nil {
		return err
	}
	bootstrapClient, err := bootstrap.NewClient(options.ServerURL, nil)
	if err != nil {
		return err
	}
	nodeIdentity, network, err := bootstrap.Enroll(ctx, identityStore, bootstrapClient, bootstrap.EnrollConfig{
		NodeType: options.NodeType, NodeName: options.NodeName, ServerURL: options.ServerURL,
		JoinToken: options.JoinToken, Version: options.Version, OSVersion: bootstrap.CurrentOSVersion(),
	})
	if err != nil {
		return err
	}
	if bootstrapLogger != nil {
		bootstrapLogger.Info("已收到 Server 下发的权威 Bootstrap 配置", "node_id", nodeIdentity.NodeID, "config_version", network.ConfigVersion)
	}
	overlayCIDR, _ := netip.ParsePrefix(network.OverlayCIDR)
	localPrefixes, err := netinfo.DirectIPv4Prefixes(windowsplatform.AdapterName)
	if err != nil {
		return err
	}
	if conflict, found := netinfo.FindConflict(overlayCIDR, localPrefixes); found {
		return fmt.Errorf("%s：Overlay 网段 %s 与本地网络 %s 冲突", protocol.ErrorOverlayLocalConflict, overlayCIDR, conflict)
	}
	nodeIP, _ := netip.ParseAddr(network.OverlayIP)
	adapter, err := windowsplatform.OpenRemLink(windowsplatform.AdapterConfig{
		Address: netip.PrefixFrom(nodeIP, overlayCIDR.Bits()), MTU: network.MTU,
	})
	if err != nil {
		return err
	}
	ownedByWireGuard := false
	defer func() {
		if !ownedByWireGuard {
			_ = adapter.Close()
		}
	}()
	wireGuard, err := clientwg.NewFromAdapter(adapter, wgLogger)
	if err != nil {
		return err
	}
	ownedByWireGuard = true
	defer wireGuard.Close()
	privateKey, err := clientwg.ParseKeyBase64(nodeIdentity.PrivateKey.String())
	if err != nil {
		return err
	}
	serverPublicKey, err := clientwg.ParseKeyBase64(network.ServerWGPublicKey)
	if err != nil {
		return err
	}
	if err := wireGuard.Configure(clientwg.Config{
		PrivateKey: privateKey, ServerPublicKey: serverPublicKey,
		ServerEndpoint: network.ServerWGEndpoint, OverlayAllowedIPs: []netip.Prefix{overlayCIDR},
		PersistentKeepalive: 25 * time.Second,
	}); err != nil {
		return err
	}
	if err := wireGuard.Up(); err != nil {
		return err
	}
	if err := waitForOverlayControl(ctx, network.ControlURL); err != nil {
		return err
	}
	if wgLogger != nil {
		wgLogger.Info("节点 Overlay 网络已就绪", "node_id", nodeIdentity.NodeID, "overlay_ip", nodeIP, "server", network.ServerWGEndpoint)
	}
	if options.OnOverlayReady != nil {
		options.OnOverlayReady(nodeIP)
	}
	routeManager, err := route.NewManager(adapter.LUID(), overlayCIDR, identityStore)
	if err != nil {
		return err
	}
	capabilities := options.Capabilities
	tcpFlowLimit := options.TCPFlowLimit
	if tcpFlowLimit <= 0 {
		tcpFlowLimit = netstackgateway.DefaultTCPFlowLimit
	}
	udpFlowLimit := options.UDPFlowLimit
	if udpFlowLimit <= 0 {
		udpFlowLimit = netstackgateway.DefaultUDPFlowLimit
	}
	udpIdleTimeout := options.UDPIdleTimeout
	if udpIdleTimeout <= 0 {
		udpIdleTimeout = netstackgateway.DefaultUDPIdleTimeout
	}
	if options.NodeType == model.NodeTypeSite {
		capabilities = protocol.NodeCapabilities{
			RemoteSubnet: true, NetstackStatus: "netstack",
			TCPCapacity: tcpFlowLimit, UDPCapacity: udpFlowLimit,
		}
	}
	var handleEnvelope func(context.Context, protocol.ControlEnvelope) error
	onPacketReject := func(rejectErr error, source netip.Addr) {
		if options.ApplicationLogger != nil {
			_, _ = options.ApplicationLogger.SampleSecurityWarning(context.Background(), logging.ModuleSubnet, "session-datagram-rejected", "会话数据报已被拒绝",
				slog.String("source_overlay_ip", source.String()), slog.String("reason", rejectErr.Error()))
		}
	}
	onPacketDrop := func(event clientwg.DropEvent) {
		if options.ApplicationLogger != nil {
			_, _ = options.ApplicationLogger.SampleSecurityWarning(context.Background(), logging.ModuleTUN, string(event.Reason), "PacketMux 已丢弃数据包",
				slog.String("reason", string(event.Reason)), slog.String("destination", event.Destination.String()))
		}
	}
	var controlClient *control.Client
	controlClient, err = control.NewClient(control.ClientConfig{
		URL:               network.ControlURL,
		OnConnectionState: options.OnControlState,
		OnHeartbeatRTT:    options.OnLatency,
		Hello: protocol.HelloPayload{
			NodeID: nodeIdentity.NodeID, NodeToken: nodeIdentity.NodeToken,
			ConfigVersion: nodeIdentity.ConfigVersion, Capabilities: capabilities,
			OSVersion: bootstrap.CurrentOSVersion(), Version: options.Version,
		},
	}, func(messageContext context.Context, envelope protocol.ControlEnvelope) error {
		if controlLogger != nil {
			controlLogger.Info("收到 Control 控制消息", "type", envelope.Type, "request_id", envelope.RequestID)
		}
		if envelope.Type == protocol.ControlRebootstrapRequired {
			response, configErr := bootstrapClient.Config(messageContext, bootstrap.ConfigRequest{
				NodeID: nodeIdentity.NodeID, NodeToken: nodeIdentity.NodeToken,
			})
			if configErr == nil {
				configErr = bootstrap.ValidateNetworkConfig(response.Network)
			}
			if configErr == nil {
				nextOverlay, _ := netip.ParsePrefix(response.Network.OverlayCIDR)
				prefixes, prefixErr := netinfo.DirectIPv4Prefixes(windowsplatform.AdapterName)
				if prefixErr != nil {
					return prefixErr
				}
				if conflict, found := netinfo.FindConflict(nextOverlay, prefixes); found {
					if controlLogger != nil {
						controlLogger.Error("节点拒绝 Overlay 网段迁移", "error_code", protocol.ErrorOverlayLocalConflict,
							"overlay_cidr", nextOverlay, "local_prefix", conflict)
					}
					reportContext, cancelReport := context.WithTimeout(messageContext, 3*time.Second)
					reportErr := controlClient.Send(reportContext, protocol.ControlHeartbeat,
						fmt.Sprintf("overlay-conflict-%d", time.Now().UnixNano()), protocol.HeartbeatPayload{
							Timestamp: time.Now().UTC(), Status: string(protocol.ErrorOverlayLocalConflict),
						})
					cancelReport()
					if reportErr != nil {
						return fmt.Errorf("report %s: %v: %w", protocol.ErrorOverlayLocalConflict, reportErr, protocol.ErrRebootstrapRequired)
					}
					// Keep the old Overlay active long enough for the Server to receive
					// the status. The migration will close this old Control path.
					return nil
				}
			}
			// A public Bootstrap refresh is also the fallback when preflight
			// could not be completed over the old Control path.
			return protocol.ErrRebootstrapRequired
		}
		if handleEnvelope == nil {
			return errors.New("节点会话运行时尚未初始化")
		}
		return handleEnvelope(messageContext, envelope)
	})
	if err != nil {
		return err
	}
	var closeRuntime func() error
	switch options.NodeType {
	case model.NodeTypeEngineer:
		engineerRuntime, err := sessionruntime.NewEngineerRuntime(ctx, sessionruntime.EngineerConfig{
			LocalOverlayIP: nodeIP, OverlayCIDR: overlayCIDR, UDPPort: network.SessionUDPPort,
			Routes: routeManager, Device: wireGuard.MuxTun(), Control: controlClient,
			OnPacketReject: onPacketReject, OnPacketDrop: onPacketDrop,
			OnNodeList: options.OnNodeList,
			OnSession: func(status model.SessionStatus, sessionID uint64, reason string) {
				if sessionLogger != nil {
					sessionLogger.Info("Engineer 会话状态变更", "status", localization.SessionStatus(string(status)), "session_id", sessionID, "reason", localization.Reason(reason))
				}
				if options.OnSession != nil {
					options.OnSession(status, sessionID, reason)
				}
			},
		})
		if err != nil {
			return err
		}
		handleEnvelope = engineerRuntime.HandleControl
		closeRuntime = engineerRuntime.Close
		if options.OnEngineerReady != nil {
			options.OnEngineerReady(engineerRuntime)
		}
	case model.NodeTypeSite:
		var siteRuntime *sessionruntime.SiteRuntime
		gateway, err := netstackgateway.New(netstackgateway.Config{
			MTU: network.MTU, TCPFlowLimit: tcpFlowLimit,
			UDPFlowLimit: udpFlowLimit, UDPIdleTimeout: udpIdleTimeout,
			Egress: func(packetContext context.Context, sessionID uint64, packet []byte) error {
				if siteRuntime == nil {
					return errors.New("Site 会话运行时尚未就绪")
				}
				return siteRuntime.Egress(packetContext, sessionID, packet)
			},
		})
		if err != nil {
			return fmt.Errorf("初始化 Site netstack 网关失败：%w", err)
		}
		siteRuntime, err = sessionruntime.NewSiteRuntime(ctx, sessionruntime.SiteConfig{
			LocalOverlayIP: nodeIP, OverlayCIDR: overlayCIDR, UDPPort: network.SessionUDPPort,
			TCPFlowLimit: tcpFlowLimit, UDPFlowLimit: udpFlowLimit,
			Routes: routeManager, Gateway: gateway, Control: controlClient,
			OnPacketReject: onPacketReject,
			OnRoute: func(sessionID uint64, prefix netip.Prefix, result route.LookupResult) {
				if routeLogger != nil {
					routeLogger.Info("Site 路由查询", "session_id", sessionID, "cidr", prefix, "result", localization.RouteResult(string(result)))
				}
				if options.OnRoute != nil {
					options.OnRoute(sessionID, prefix, string(result))
				}
			},
			OnSession: func(status model.SessionStatus, sessionID uint64, reason string) {
				if sessionLogger != nil {
					sessionLogger.Info("Site 会话状态变更", "status", localization.SessionStatus(string(status)), "session_id", sessionID, "reason", localization.Reason(reason))
				}
				if options.OnSession != nil {
					options.OnSession(status, sessionID, reason)
				}
			},
		})
		if err != nil {
			_ = gateway.Close()
			return err
		}
		handleEnvelope = siteRuntime.HandleControl
		closeRuntime = siteRuntime.Close
		if options.OnSiteReady != nil {
			options.OnSiteReady()
		}
	default:
		return errors.New("不支持的节点类型")
	}
	defer func() {
		_ = closeRuntime()
		if options.OnEngineerReady != nil && options.NodeType == model.NodeTypeEngineer {
			options.OnEngineerReady(nil)
		}
	}()
	err = controlClient.Run(ctx)
	return err
}

func nodeLogger(options Options, module logging.Module) *slog.Logger {
	if options.ApplicationLogger != nil {
		logger, err := options.ApplicationLogger.For(module)
		if err == nil {
			return logger
		}
	}
	return options.Logger
}

func waitForOverlayControl(ctx context.Context, controlURL string) error {
	parsed, err := url.Parse(controlURL)
	if err != nil {
		return err
	}
	waitContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	var lastErr error
	for {
		connection, err := dialer.DialContext(waitContext, "tcp4", parsed.Host)
		if err == nil {
			return connection.Close()
		}
		lastErr = err
		if ctx.Err() != nil {
			return ctx.Err()
		}
		select {
		case <-waitContext.Done():
			return fmt.Errorf("Overlay Control endpoint is unreachable: %v: %w", lastErr, protocol.ErrRebootstrapRequired)
		case <-time.After(time.Second):
		}
	}
}
