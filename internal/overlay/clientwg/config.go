package clientwg

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

const keySize = 32

// Key is one raw 32-byte WireGuard private or public key.
type Key [keySize]byte

// Config is the one-Server-peer wireguard-go configuration used by v1 Nodes.
type Config struct {
	PrivateKey          Key
	ServerPublicKey     Key
	ServerEndpoint      string
	OverlayAllowedIPs   []netip.Prefix
	ListenPort          uint16
	PersistentKeepalive time.Duration
}

func (c Config) uapi() (string, error) {
	if err := c.validate(); err != nil {
		return "", err
	}

	var builder strings.Builder
	fmt.Fprintf(&builder, "private_key=%s\n", hex.EncodeToString(c.PrivateKey[:]))
	fmt.Fprintf(&builder, "listen_port=%d\n", c.ListenPort)
	builder.WriteString("replace_peers=true\n")
	fmt.Fprintf(&builder, "public_key=%s\n", hex.EncodeToString(c.ServerPublicKey[:]))
	fmt.Fprintf(&builder, "endpoint=%s\n", c.ServerEndpoint)
	fmt.Fprintf(&builder, "persistent_keepalive_interval=%d\n", int(c.PersistentKeepalive/time.Second))
	builder.WriteString("replace_allowed_ips=true\n")
	for _, prefix := range c.OverlayAllowedIPs {
		fmt.Fprintf(&builder, "allowed_ip=%s\n", prefix)
	}
	builder.WriteString("\n")
	return builder.String(), nil
}

func (c Config) validate() error {
	if zeroKey(c.PrivateKey) {
		return errors.New("WireGuard private key must not be zero")
	}
	if zeroKey(c.ServerPublicKey) {
		return errors.New("Server WireGuard public key must not be zero")
	}
	if c.PrivateKey == c.ServerPublicKey {
		return errors.New("Node private key and Server public key must differ")
	}
	host, portText, err := net.SplitHostPort(c.ServerEndpoint)
	if err != nil {
		return fmt.Errorf("Server WireGuard endpoint must be host:port: %w", err)
	}
	if host == "" || portText == "" {
		return errors.New("Server WireGuard endpoint must include a host and port")
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil || port == 0 {
		return errors.New("Server WireGuard endpoint port must be between 1 and 65535")
	}
	if len(c.OverlayAllowedIPs) != 1 {
		return errors.New("v1 requires exactly one Overlay AllowedIP and one Server peer")
	}
	for _, prefix := range c.OverlayAllowedIPs {
		if !prefix.IsValid() || !prefix.Addr().Is4() || prefix.Bits() == 0 {
			return fmt.Errorf("Overlay AllowedIP must be IPv4: %s", prefix)
		}
		if prefix != prefix.Masked() {
			return fmt.Errorf("Overlay AllowedIP must use its network address: %s", prefix)
		}
	}
	if c.PersistentKeepalive < 0 || c.PersistentKeepalive > 65535*time.Second {
		return errors.New("persistent keepalive must be between 0 and 65535 seconds")
	}
	if c.PersistentKeepalive%time.Second != 0 {
		return errors.New("persistent keepalive must be a whole number of seconds")
	}
	return nil
}

func zeroKey(key Key) bool {
	return key == Key{}
}
