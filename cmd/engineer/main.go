//go:build windows

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	engineerui "remlink/frontend/engineer"
	"remlink/internal/appdir"
	"remlink/internal/clientsettings"
	"remlink/internal/clientstate"
	"remlink/internal/identity"
	"remlink/internal/logging"
	"remlink/internal/model"
	"remlink/internal/nodeagent"
	"remlink/internal/platform/windows/dpapi"
	"remlink/internal/siteprofile"
	"remlink/internal/version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "remlink-engineer: %v\n", err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	flags := flag.NewFlagSet("remlink-engineer", flag.ContinueOnError)
	configPath := flags.String("config", "", "import a legacy Engineer YAML configuration")
	identityPath := flags.String("identity", "", "override DPAPI identity path")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	paths, err := clientstate.PrepareDesktop(model.NodeTypeEngineer, *configPath, *identityPath)
	if err != nil {
		return err
	}
	defer paths.Close()
	settingsStore, err := clientsettings.NewStore(paths.Settings, model.NodeTypeEngineer, dpapi.Protector{})
	if err != nil {
		return err
	}
	identityStore, err := identity.NewStore(paths.Identity, dpapi.Protector{})
	if err != nil {
		return err
	}
	applicationLogger, err := logging.New(logging.DefaultConfig(filepath.Join(paths.Logs, "engineer.jsonl")))
	if err != nil {
		return err
	}
	defer applicationLogger.Close()
	logger, _ := applicationLogger.For(logging.ModuleCore)
	logger.Info("已使用本机保存的配置与节点身份", "settings_path", paths.Settings, "identity_path", paths.Identity)
	profileStore, err := siteprofile.NewStore(paths.Profiles)
	if err != nil {
		return err
	}
	app := NewEngineerApp(nodeagent.Options{
		NodeType: model.NodeTypeEngineer, IdentityPath: paths.Identity,
		Version: version.String(), Logger: logger, ApplicationLogger: applicationLogger,
	}, profileStore)
	app.configure(settingsStore, identityStore)
	iconPath, err := appdir.Join("RemLinkEngineer.ico")
	if err != nil {
		return err
	}
	return wails.Run(&options.App{
		Title: "RemLink Engineer", Width: 1400, Height: 880, MinWidth: 1024, MinHeight: 720,
		BackgroundColour: &options.RGBA{R: 247, G: 248, B: 252, A: 255},
		AssetServer:      &assetserver.Options{Assets: engineerui.Assets},
		Debug:            options.Debug{OpenInspectorOnStartup: true},
		OnStartup:        app.Startup, OnShutdown: app.Shutdown,
		OnDomReady: func(ctx context.Context) {
			if err := app.window.Start(ctx, "RemLink Engineer", iconPath); err != nil {
				app.appendLog("ERROR", err.Error())
				app.emit()
			}
		},
		OnBeforeClose: app.window.BeforeClose,
		Bind:          []interface{}{app},
	})
}
