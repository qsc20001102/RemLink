//go:build windows

// Package windowsplatform centralizes RemLink-owned Windows networking changes.
package windowsplatform

import (
	"errors"
	"fmt"
	"net/netip"
	"sync"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/tun"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"

	"remlink/internal/platform/windows/wintunruntime"
)

const (
	AdapterName       = "RemLink"
	AdapterTunnelType = "RemLink"
	DefaultMTU        = 1280
)

var ErrAdministratorRequired = errors.New("administrator privileges are required to manage the RemLink Wintun adapter")

// AdapterConfig defines the single IPv4 Overlay address owned by RemLink.
type AdapterConfig struct {
	Address netip.Prefix
	MTU     int
}

// Adapter owns one live Wintun session. Closing it leaves the persistent
// Windows adapter installed so a later RemLink process can reuse it.
type Adapter struct {
	device         tun.Device
	luid           winipcfg.LUID
	interfaceIndex uint32
	closeOnce      sync.Once
	closeErr       error
}

// OpenRemLink creates or reuses the one fixed-name RemLink Wintun, applies its
// IPv4 address and MTU through Windows APIs, and returns the live TUN device.
func OpenRemLink(config AdapterConfig) (*Adapter, error) {
	if err := validateAdapterConfig(config); err != nil {
		return nil, err
	}
	if !windows.GetCurrentProcessToken().IsElevated() {
		return nil, ErrAdministratorRequired
	}
	if _, err := wintunruntime.PreloadDefault(); err != nil {
		return nil, err
	}

	tun.WintunTunnelType = AdapterTunnelType
	base, err := tun.CreateTUN(AdapterName, config.MTU)
	if err != nil {
		return nil, fmt.Errorf("create or reuse %s Wintun: %w", AdapterName, err)
	}
	succeeded := false
	defer func() {
		if !succeeded {
			_ = base.Close()
		}
	}()

	luidSource, ok := base.(interface{ LUID() uint64 })
	if !ok {
		return nil, errors.New("wireguard-go Windows TUN does not expose its interface LUID")
	}
	luid := winipcfg.LUID(luidSource.LUID())
	if luid == 0 {
		return nil, errors.New("RemLink Wintun returned an invalid interface LUID")
	}
	if err := luid.SetIPAddressesForFamily(
		winipcfg.AddressFamily(windows.AF_INET),
		[]netip.Prefix{config.Address},
	); err != nil {
		return nil, fmt.Errorf("set RemLink IPv4 address %s: %w", config.Address, err)
	}

	ipInterface, err := luid.IPInterface(winipcfg.AddressFamily(windows.AF_INET))
	if err != nil {
		return nil, fmt.Errorf("read RemLink IPv4 interface: %w", err)
	}
	ipInterface.NLMTU = uint32(config.MTU)
	if err := ipInterface.Set(); err != nil {
		return nil, fmt.Errorf("set RemLink MTU %d: %w", config.MTU, err)
	}
	interfaceRow, err := luid.Interface()
	if err != nil {
		return nil, fmt.Errorf("read RemLink interface index: %w", err)
	}

	succeeded = true
	return &Adapter{
		device:         base,
		luid:           luid,
		interfaceIndex: interfaceRow.InterfaceIndex,
	}, nil
}

// Device returns the TUN device. Ownership transfers to wireguard-go when it is
// passed to clientwg.NewDevice; callers must then close the wireguard-go owner.
func (a *Adapter) Device() tun.Device {
	return a.device
}

// LUID returns the Windows interface locally unique identifier.
func (a *Adapter) LUID() uint64 {
	return uint64(a.luid)
}

// InterfaceIndex returns the Windows interface index used by later RouteManager work.
func (a *Adapter) InterfaceIndex() uint32 {
	return a.interfaceIndex
}

// Close stops the Wintun session without deleting the persistent adapter.
func (a *Adapter) Close() error {
	if a == nil || a.device == nil {
		return nil
	}
	a.closeOnce.Do(func() {
		a.closeErr = a.device.Close()
	})
	return a.closeErr
}

func validateAdapterConfig(config AdapterConfig) error {
	if !config.Address.IsValid() || !config.Address.Addr().Is4() {
		return errors.New("RemLink adapter address must be a valid IPv4 prefix")
	}
	if config.Address.Bits() > 30 {
		return errors.New("RemLink adapter prefix must leave usable host addresses")
	}
	if config.Address.Addr() == config.Address.Masked().Addr() || config.Address.Addr() == lastAddress(config.Address) {
		return errors.New("RemLink adapter address must not be the network or broadcast address")
	}
	if config.MTU < 576 || config.MTU > 65535 {
		return errors.New("RemLink adapter MTU must be between 576 and 65535")
	}
	return nil
}

func lastAddress(prefix netip.Prefix) netip.Addr {
	bytes := prefix.Masked().Addr().As4()
	value := uint32(bytes[0])<<24 | uint32(bytes[1])<<16 | uint32(bytes[2])<<8 | uint32(bytes[3])
	value |= ^uint32(0) >> prefix.Bits()
	return netip.AddrFrom4([4]byte{byte(value >> 24), byte(value >> 16), byte(value >> 8), byte(value)})
}
