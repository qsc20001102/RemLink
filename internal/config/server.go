package config

import (
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

const (
	DefaultHTTPListen      = "0.0.0.0:8080"
	DefaultControlListen   = "10.88.0.1:7001"
	DefaultWireGuardPort   = 51820
	DefaultDataDirectory   = "./data"
	DefaultOverlayCIDR     = "10.88.0.0/16"
	DefaultServerOverlayIP = "10.88.0.1"
	DefaultSessionUDPPort  = 6200
	DefaultMTU             = 1280
)

// ServerConfig contains Server bootstrap values and initial network defaults.
type ServerConfig struct {
	Server  ServerListeners `yaml:"server"`
	Data    DataConfig      `yaml:"data"`
	Network NetworkConfig   `yaml:"network"`
}

// ServerListeners defines process listen endpoints.
type ServerListeners struct {
	HTTPListen    string `yaml:"http_listen"`
	ControlListen string `yaml:"control_listen"`
	WireGuardPort int    `yaml:"wireguard_port"`
}

// DataConfig defines the Server persistence directory.
type DataConfig struct {
	Directory string `yaml:"directory"`
}

// NetworkConfig supplies initial values later stored in Server SQLite.
type NetworkConfig struct {
	OverlayCIDR     string `yaml:"overlay_cidr"`
	ServerOverlayIP string `yaml:"server_overlay_ip"`
	SessionUDPPort  int    `yaml:"session_udp_port"`
	MTU             int    `yaml:"mtu"`
}

// DefaultServerConfig returns the Appendix B defaults.
func DefaultServerConfig() ServerConfig {
	return ServerConfig{
		Server: ServerListeners{
			HTTPListen:    DefaultHTTPListen,
			ControlListen: DefaultControlListen,
			WireGuardPort: DefaultWireGuardPort,
		},
		Data: DataConfig{Directory: DefaultDataDirectory},
		Network: NetworkConfig{
			OverlayCIDR:     DefaultOverlayCIDR,
			ServerOverlayIP: DefaultServerOverlayIP,
			SessionUDPPort:  DefaultSessionUDPPort,
			MTU:             DefaultMTU,
		},
	}
}

// LoadServer loads one strict YAML document and validates the result.
func LoadServer(path string) (ServerConfig, error) {
	config := DefaultServerConfig()
	if err := decodeStrict(path, &config); err != nil {
		return ServerConfig{}, err
	}
	if err := config.Validate(); err != nil {
		return ServerConfig{}, fmt.Errorf("validate server config %q: %w", path, err)
	}
	return config, nil
}

// Validate checks Phase 0 invariants without changing the host network.
func (c ServerConfig) Validate() error {
	if _, err := validateIPv4Listen("server.http_listen", c.Server.HTTPListen); err != nil {
		return err
	}
	controlIP, err := validateIPv4Listen("server.control_listen", c.Server.ControlListen)
	if err != nil {
		return err
	}
	if err := validatePort("server.wireguard_port", c.Server.WireGuardPort); err != nil {
		return err
	}
	if strings.TrimSpace(c.Data.Directory) == "" {
		return fmt.Errorf("data.directory must not be empty")
	}

	prefix, err := netip.ParsePrefix(c.Network.OverlayCIDR)
	if err != nil || !prefix.Addr().Is4() {
		return fmt.Errorf("network.overlay_cidr must be a valid IPv4 CIDR")
	}
	if prefix != prefix.Masked() {
		return fmt.Errorf("network.overlay_cidr must use its network address: got %s", prefix)
	}
	if prefix.Bits() == 0 {
		return fmt.Errorf("network.overlay_cidr must not enable 0.0.0.0/0 Exit Node routing")
	}
	if prefix.Bits() > 30 {
		return fmt.Errorf("network.overlay_cidr must leave addresses for the Server and at least one Node")
	}

	serverIP, err := netip.ParseAddr(c.Network.ServerOverlayIP)
	if err != nil || !serverIP.Is4() {
		return fmt.Errorf("network.server_overlay_ip must be a valid IPv4 address")
	}
	if !prefix.Contains(serverIP) {
		return fmt.Errorf("network.server_overlay_ip must belong to network.overlay_cidr")
	}
	if serverIP == prefix.Addr() || serverIP == lastIPv4(prefix) {
		return fmt.Errorf("network.server_overlay_ip must not be the network or broadcast address")
	}
	if controlIP != serverIP {
		return fmt.Errorf("server.control_listen must bind network.server_overlay_ip")
	}
	if err := validatePort("network.session_udp_port", c.Network.SessionUDPPort); err != nil {
		return err
	}
	if c.Network.MTU < 576 || c.Network.MTU > 65535 {
		return fmt.Errorf("network.mtu must be between 576 and 65535")
	}
	return nil
}

func validateIPv4Listen(field, value string) (netip.Addr, error) {
	host, portText, err := net.SplitHostPort(value)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("%s must be an IPv4 host:port: %w", field, err)
	}
	address, err := netip.ParseAddr(host)
	if err != nil || !address.Is4() {
		return netip.Addr{}, fmt.Errorf("%s must use an IPv4 address", field)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("%s has an invalid port", field)
	}
	if err := validatePort(field, port); err != nil {
		return netip.Addr{}, err
	}
	return address, nil
}

func validatePort(field string, port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("%s must be between 1 and 65535", field)
	}
	return nil
}

func lastIPv4(prefix netip.Prefix) netip.Addr {
	bytes := prefix.Addr().As4()
	value := uint32(bytes[0])<<24 | uint32(bytes[1])<<16 | uint32(bytes[2])<<8 | uint32(bytes[3])
	value |= ^uint32(0) >> prefix.Bits()
	return netip.AddrFrom4([4]byte{byte(value >> 24), byte(value >> 16), byte(value >> 8), byte(value)})
}
