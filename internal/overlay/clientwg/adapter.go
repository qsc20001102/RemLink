package clientwg

import (
	"errors"
	"log/slog"

	"golang.zx2c4.com/wireguard/tun"
)

// Adapter supplies the one RemLink TUN device created by platform/windows.
type Adapter interface {
	Device() tun.Device
}

// NewFromAdapter transfers ownership of the Adapter's live TUN session to an
// embedded wireguard-go Device.
func NewFromAdapter(adapter Adapter, logger *slog.Logger) (*Device, error) {
	if adapter == nil {
		return nil, errors.New("RemLink adapter must not be nil")
	}
	return NewDevice(adapter.Device(), logger)
}
