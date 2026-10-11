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

	siteui "remlink/frontend/site"
	"remlink/internal/appdir"
	"remlink/internal/clientsettings"
	"remlink/internal/clientstate"
	"remlink/internal/identity"
	"remlink/internal/logging"
	"remlink/internal/model"
	"remlink/internal/nodeagent"
	"remlink/internal/platform/windows/dpapi"
	"remlink/internal/version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "remlink-site: %v\n", err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	flags := flag.NewFlagSet("remlink-site", flag.ContinueOnError)
	configPath := flags.String("config", "", "import a legacy Site YAML configuration")
	identityPath := flags.String("identity", "", "override DPAPI identity path")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	paths, err := clientstate.PrepareDesktop(model.NodeTypeSite, *configPath, *identityPath)
	if err != nil {
		return err
	}
	defer paths.Close()
	settingsStore, err := clientsettings.NewStore(paths.Settings, model.NodeTypeSite, dpapi.Protector{})
	if err != nil {
		return err
	}
	identityStore, err := identity.NewStore(paths.Identity, dpapi.Protector{})
	if err != nil {
		return err
	}
	applicationLogger, err := logging.New(logging.DefaultConfig(filepath.Join(paths.Logs, "site.jsonl")))
	if err != nil {
		return err
	}
	defer applicationLogger.Close()
	logger, _ := applicationLogger.For(logging.ModuleCore)
	app := NewSiteApp(nodeagent.Options{NodeType: model.NodeTypeSite, IdentityPath: paths.Identity,
		Version: version.String(), Logger: logger, ApplicationLogger: applicationLogger}, settingsStore, identityStore)
	iconPath, err := appdir.Join("RemLinkSite.ico")
	if err != nil {
		return err
	}
	return wails.Run(&options.App{Title: "RemLink Site", Width: 1150, Height: 800, MinWidth: 900, MinHeight: 650,
		BackgroundColour: &options.RGBA{R: 247, G: 248, B: 252, A: 255},
		AssetServer:      &assetserver.Options{Assets: siteui.Assets}, OnStartup: app.Startup, OnShutdown: app.Shutdown,
		OnDomReady: func(ctx context.Context) {
			if err := app.window.Start(ctx, "RemLink Site", iconPath); err != nil {
				app.appendLog("ERROR", err.Error())
				app.emit()
			}
		},
		OnBeforeClose: app.window.BeforeClose,
		Bind:          []interface{}{app}})
}
