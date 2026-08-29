package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"remlink/internal/appdir"
	"remlink/internal/config"
	"remlink/internal/identity"
	"remlink/internal/logging"
	"remlink/internal/model"
	"remlink/internal/nodeagent"
	"remlink/internal/version"
)

const joinTokenEnvironment = "REMLINK_JOIN_TOKEN"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "remlink-site: %v\n", err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	defaultConfigPath, err := appdir.Join("site.yaml")
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet("remlink-site", flag.ContinueOnError)
	configPath := flags.String("config", defaultConfigPath, "Site YAML configuration path")
	joinToken := flags.String("join-token", os.Getenv(joinTokenEnvironment), "first-registration Join Token (CLI/environment override YAML)")
	identityPath := flags.String("identity", "", "override DPAPI identity path")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	clientConfig, err := config.LoadSite(*configPath)
	if err != nil {
		return err
	}
	resolvedJoinToken := config.ResolveJoinToken(*joinToken, clientConfig.JoinToken)
	if *identityPath == "" {
		*identityPath, err = identity.DefaultPath(model.NodeTypeSite)
		if err != nil {
			return err
		}
	}
	applicationLogger, err := logging.New(logging.DefaultConfig(filepath.Join(filepath.Dir(*identityPath), "logs", "site.jsonl")))
	if err != nil {
		return err
	}
	defer applicationLogger.Close()
	logger, _ := applicationLogger.For(logging.ModuleCore)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger.Info("RemLink Site 正在启动", "version", version.String())
	console := newSiteConsole(os.Stdout)
	console.Header(clientConfig.Server, clientConfig.NodeName, version.String())
	return nodeagent.Run(ctx, nodeagent.Options{
		NodeType: model.NodeTypeSite, NodeName: clientConfig.NodeName,
		ServerURL: clientConfig.Server, JoinToken: resolvedJoinToken, IdentityPath: *identityPath,
		Version: version.String(), Logger: logger, ApplicationLogger: applicationLogger,
		TCPFlowLimit:   clientConfig.Netstack.TCPFlowLimit,
		UDPFlowLimit:   clientConfig.Netstack.UDPFlowLimit,
		UDPIdleTimeout: time.Duration(clientConfig.Netstack.UDPIdleSeconds) * time.Second,
		OnOverlayReady: console.OverlayReady,
		OnControlState: console.ControlState,
		OnSiteReady:    console.SiteReady,
		OnSession:      console.Session,
		OnRoute:        console.Route,
	})
}
