//go:build windows

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	engineerui "remlink/frontend/engineer"
	"remlink/internal/appdir"
	"remlink/internal/config"
	"remlink/internal/identity"
	"remlink/internal/logging"
	"remlink/internal/model"
	"remlink/internal/nodeagent"
	"remlink/internal/siteprofile"
	"remlink/internal/version"
)

const joinTokenEnvironment = "REMLINK_JOIN_TOKEN"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "remlink-engineer: %v\n", err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	defaultConfigPath, err := appdir.Join("engineer.yaml")
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet("remlink-engineer", flag.ContinueOnError)
	configPath := flags.String("config", defaultConfigPath, "Engineer YAML configuration path")
	joinToken := flags.String("join-token", os.Getenv(joinTokenEnvironment), "first-registration Join Token (CLI/environment override YAML)")
	identityPath := flags.String("identity", "", "override DPAPI identity path")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	clientConfig, err := config.LoadEngineer(*configPath)
	if err != nil {
		return err
	}
	resolvedJoinToken := config.ResolveJoinToken(*joinToken, clientConfig.JoinToken)
	if *identityPath == "" {
		*identityPath, err = identity.DefaultPath(model.NodeTypeEngineer)
		if err != nil {
			return err
		}
	}
	applicationLogger, err := logging.New(logging.DefaultConfig(filepath.Join(filepath.Dir(*identityPath), "logs", "engineer.jsonl")))
	if err != nil {
		return err
	}
	defer applicationLogger.Close()
	logger, _ := applicationLogger.For(logging.ModuleCore)
	profileStore, err := siteprofile.NewStore(filepath.Join(filepath.Dir(*identityPath), "site-profiles.json"))
	if err != nil {
		return err
	}
	app := NewEngineerApp(nodeagent.Options{
		NodeType: model.NodeTypeEngineer, NodeName: clientConfig.NodeName,
		ServerURL: clientConfig.Server, JoinToken: resolvedJoinToken, IdentityPath: *identityPath,
		Version: version.String(), Logger: logger, ApplicationLogger: applicationLogger,
	}, profileStore)
	return wails.Run(&options.App{
		Title: "RemLink Engineer", Width: 1400, Height: 880, MinWidth: 1024, MinHeight: 720,
		BackgroundColour: &options.RGBA{R: 247, G: 248, B: 252, A: 255},
		AssetServer:      &assetserver.Options{Assets: engineerui.Assets},
		Debug:            options.Debug{OpenInspectorOnStartup: true},
		OnStartup:        app.Startup, OnShutdown: app.Shutdown,
		Bind: []interface{}{app},
	})
}
