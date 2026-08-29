//go:build windows

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"os/signal"
	"syscall"
	"time"

	"remlink/internal/overlay/clientwg"
	windowsplatform "remlink/internal/platform/windows"
	"remlink/internal/platform/windows/wintunruntime"
)

const privateKeyEnvironment = "REMLINK_POC_PRIVATE_KEY"

func run(arguments []string) error {
	flags := flag.NewFlagSet("phase1-node", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	runtimeProbe := flags.Bool("runtime-probe", false, "install and load the pinned Wintun DLL without creating an adapter")
	adapterProbe := flags.Bool("adapter-probe", false, "create/configure the RemLink adapter, print details, and exit")
	addressText := flags.String("address", "", "Node Overlay address with prefix, for example 10.88.0.2/16")
	serverEndpoint := flags.String("server-endpoint", "", "Server WireGuard endpoint, for example 203.0.113.10:51820")
	serverPublicKeyText := flags.String("server-public-key", "", "Server WireGuard public key in standard base64 format")
	keepaliveSeconds := flags.Uint("keepalive", 25, "persistent keepalive interval in seconds")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if *runtimeProbe {
		path, version, err := wintunruntime.Probe()
		if err != nil {
			return err
		}
		fmt.Printf("Wintun %s loaded from %s\n", version, path)
		return nil
	}
	if *addressText == "" {
		return errors.New("--address is required")
	}
	address, err := netip.ParsePrefix(*addressText)
	if err != nil {
		return fmt.Errorf("parse --address: %w", err)
	}
	var privateKey clientwg.Key
	var serverPublicKey clientwg.Key
	if !*adapterProbe {
		if *serverEndpoint == "" {
			return errors.New("--server-endpoint is required unless --adapter-probe is used")
		}
		if *serverPublicKeyText == "" {
			return errors.New("--server-public-key is required unless --adapter-probe is used")
		}
		privateKeyText := os.Getenv(privateKeyEnvironment)
		if privateKeyText == "" {
			return fmt.Errorf("%s must contain the Node private key in standard base64 format", privateKeyEnvironment)
		}
		privateKey, err = clientwg.ParseKeyBase64(privateKeyText)
		if err != nil {
			return fmt.Errorf("parse %s: %w", privateKeyEnvironment, err)
		}
		serverPublicKey, err = clientwg.ParseKeyBase64(*serverPublicKeyText)
		if err != nil {
			return fmt.Errorf("parse --server-public-key: %w", err)
		}
		if *keepaliveSeconds > 65535 {
			return errors.New("--keepalive must not exceed 65535 seconds")
		}
	}

	adapter, err := windowsplatform.OpenRemLink(windowsplatform.AdapterConfig{
		Address: address,
		MTU:     windowsplatform.DefaultMTU,
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
	name, err := adapter.Device().Name()
	if err != nil {
		return fmt.Errorf("read adapter name: %w", err)
	}
	mtu, err := adapter.Device().MTU()
	if err != nil {
		return fmt.Errorf("read adapter MTU: %w", err)
	}
	fmt.Printf("Adapter ready: name=%s index=%d luid=%d address=%s mtu=%d\n", name, adapter.InterfaceIndex(), adapter.LUID(), address, mtu)
	if *adapterProbe {
		return nil
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})).With("module", "WG")
	wireguardDevice, err := clientwg.NewFromAdapter(adapter, logger)
	if err != nil {
		return err
	}
	ownedByWireGuard = true
	defer wireguardDevice.Close()
	if err := wireguardDevice.Configure(clientwg.Config{
		PrivateKey:          privateKey,
		ServerPublicKey:     serverPublicKey,
		ServerEndpoint:      *serverEndpoint,
		OverlayAllowedIPs:   []netip.Prefix{address.Masked()},
		PersistentKeepalive: time.Duration(*keepaliveSeconds) * time.Second,
	}); err != nil {
		return err
	}
	if err := wireguardDevice.Up(); err != nil {
		return err
	}

	fmt.Printf("wireguard-go is up; press Ctrl+C after completing Overlay ping checks\n")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	return nil
}
