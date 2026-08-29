// Package serverwg creates and configures the Linux kernel WireGuard hub.
package serverwg

import (
	"errors"
	"fmt"
	"net/netip"
)

const DefaultInterfaceName = "wg0"

// Config contains kernel interface and host-forwarding settings.
type Config struct {
	InterfaceName    string
	Address          netip.Prefix
	ListenPort       int
	PrivateKeyPath   string
	EnableForwarding bool
	IPTablesPath     string
}

func (c Config) validate() error {
	if c.InterfaceName == "" {
		return errors.New("WireGuard interface name must not be empty")
	}
	if !c.Address.Addr().Is4() || c.Address.Bits() > 30 {
		return errors.New("WireGuard address must be an IPv4 interface prefix")
	}
	if c.Address.Addr() == c.Address.Masked().Addr() {
		return errors.New("WireGuard address must be a usable host address, not its network address")
	}
	if c.ListenPort < 1 || c.ListenPort > 65535 {
		return fmt.Errorf("WireGuard listen port %d is outside 1..65535", c.ListenPort)
	}
	if c.PrivateKeyPath == "" {
		return errors.New("Server private key path must not be empty")
	}
	return nil
}

// Peer is one Node's cryptokey-routing entry.
type Peer struct {
	PublicKey string
	Address   netip.Addr
}
