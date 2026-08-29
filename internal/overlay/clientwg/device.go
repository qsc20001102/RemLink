package clientwg

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
)

// Device owns the MuxTun, wireguard-go engine, and transport bind.
type Device struct {
	tun       *MuxTun
	wireguard *device.Device
	closeOnce sync.Once
}

// NewDevice transfers ownership of base to a new embedded wireguard-go Device.
func NewDevice(base tun.Device, logger *slog.Logger) (*Device, error) {
	if base == nil {
		return nil, errors.New("base TUN device must not be nil")
	}
	mux := NewMuxTun(base)
	wireguard := device.NewDevice(mux, conn.NewDefaultBind(), wireGuardLogger(logger))
	return &Device{tun: mux, wireguard: wireguard}, nil
}

// Configure atomically replaces the single Server peer through wireguard-go UAPI.
func (d *Device) Configure(config Config) error {
	uapi, err := config.uapi()
	if err != nil {
		return err
	}
	if err := d.wireguard.IpcSet(uapi); err != nil {
		return fmt.Errorf("configure embedded wireguard-go: %w", err)
	}
	return nil
}

// Up starts the embedded WireGuard device after configuration.
func (d *Device) Up() error {
	if err := d.wireguard.Up(); err != nil {
		return fmt.Errorf("bring embedded wireguard-go up: %w", err)
	}
	return nil
}

// UAPIState returns wireguard-go's current UAPI state for diagnostics.
func (d *Device) UAPIState() (string, error) {
	return d.wireguard.IpcGet()
}

// MuxTun returns the single packet boundary used by Session transport.
func (d *Device) MuxTun() *MuxTun { return d.tun }

// Close is idempotent and stops wireguard-go, MuxTun, and the base Wintun.
func (d *Device) Close() {
	if d == nil || d.wireguard == nil {
		return
	}
	d.closeOnce.Do(func() {
		d.wireguard.Close()
	})
}

func wireGuardLogger(logger *slog.Logger) *device.Logger {
	if logger == nil {
		return &device.Logger{
			Verbosef: device.DiscardLogf,
			Errorf:   device.DiscardLogf,
		}
	}
	return &device.Logger{
		Verbosef: func(format string, args ...any) {
			logger.Debug(fmt.Sprintf(format, args...))
		},
		Errorf: func(format string, args ...any) {
			logger.Error(fmt.Sprintf(format, args...))
		},
	}
}
