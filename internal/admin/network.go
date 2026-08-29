// Package admin implements the v1 Server Admin API and network migration.
package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"sync"
	"time"

	"remlink/internal/bootstrap"
	"remlink/internal/database"
	"remlink/internal/ipam"
	"remlink/internal/overlay/serverwg"
	"remlink/internal/protocol"
)

const networkSettingKey = "admin.network"

type Network struct {
	OverlayCIDR     string `json:"overlay_cidr"`
	ServerOverlayIP string `json:"server_overlay_ip"`
	WireGuardPort   int    `json:"wireguard_port"`
	SessionUDPPort  int    `json:"session_udp_port"`
	MTU             int    `json:"mtu"`
	ConfigVersion   uint64 `json:"config_version"`
}

type NetworkUpdate struct {
	OverlayCIDR     string `json:"overlay_cidr"`
	ServerOverlayIP string `json:"server_overlay_ip"`
	WireGuardPort   int    `json:"wireguard_port"`
	SessionUDPPort  int    `json:"session_udp_port"`
	MTU             int    `json:"mtu"`
	RotateJoinToken bool   `json:"rotate_join_token,omitempty"`
}

type NetworkView struct {
	Network
	UptimeSeconds int64 `json:"uptime_seconds"`
}

type IPAM interface {
	ChangeNodeAddress(context.Context, string, netip.Addr) error
	ReleaseNode(context.Context, string) error
	Reconfigure(netip.Prefix, netip.Addr) error
}

type PeerManager interface {
	EnsurePeer(context.Context, string, netip.Addr) error
	RemovePeer(context.Context, string) error
	Reconfigure(context.Context, netip.Prefix, int, []serverwg.Peer) error
	LastHandshake(context.Context, string) (*time.Time, error)
}

type BootstrapNetwork interface {
	NetworkSnapshot() bootstrap.ServiceConfig
	UpdateNetwork(bootstrap.ServiceConfig) error
}

type ControlNetwork interface {
	Send(context.Context, string, protocol.ControlMessageType, any) error
	SetNetworkConfigVersion(uint64) error
	ResetNodeConnection(string, string)
	ResetConnections(string)
}

type SessionControl interface {
	Disconnect(context.Context, uint64, string) error
	DisconnectAll(context.Context, string) error
	DisconnectNode(context.Context, string, string) error
	BeginNetworkMigration(context.Context, string) error
	EndNetworkMigration()
	ReconfigureNetwork(netip.Prefix, int, int) error
}

type NetworkManager struct {
	mu            sync.Mutex
	store         *database.Store
	ipam          IPAM
	peers         PeerManager
	bootstrap     BootstrapNetwork
	control       ControlNetwork
	sessions      SessionControl
	current       Network
	startedAt     time.Time
	rebindControl func(netip.Addr) error
}

func LoadStoredNetwork(ctx context.Context, store *database.Store, fallback Network) (Network, error) {
	value, found, err := database.GetSetting(ctx, store.DB(), networkSettingKey)
	if err != nil || !found {
		return fallback, err
	}
	var network Network
	if err := json.Unmarshal([]byte(value), &network); err != nil {
		return Network{}, fmt.Errorf("decode stored Admin network: %w", err)
	}
	if _, _, err := validateNetwork(network); err != nil {
		return Network{}, fmt.Errorf("validate stored Admin network: %w", err)
	}
	return network, nil
}

func NewNetworkManager(store *database.Store, ipamManager IPAM, peers PeerManager, bootstrapService BootstrapNetwork,
	control ControlNetwork, sessions SessionControl, initial Network, rebindControl func(netip.Addr) error) (*NetworkManager, error) {
	if store == nil || ipamManager == nil || peers == nil || bootstrapService == nil || control == nil || sessions == nil {
		return nil, errors.New("Admin NetworkManager dependencies are required")
	}
	if _, _, err := validateNetwork(initial); err != nil {
		return nil, err
	}
	if rebindControl == nil {
		rebindControl = func(netip.Addr) error { return nil }
	}
	return &NetworkManager{
		store: store, ipam: ipamManager, peers: peers, bootstrap: bootstrapService,
		control: control, sessions: sessions, current: initial, startedAt: time.Now(), rebindControl: rebindControl,
	}, nil
}

func (m *NetworkManager) View() NetworkView {
	m.mu.Lock()
	defer m.mu.Unlock()
	return NetworkView{Network: m.current, UptimeSeconds: int64(time.Since(m.startedAt).Seconds())}
}

func (m *NetworkManager) Current() Network {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.current
}

func (m *NetworkManager) Update(ctx context.Context, input NetworkUpdate) (Network, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	desired := Network{
		OverlayCIDR: input.OverlayCIDR, ServerOverlayIP: input.ServerOverlayIP,
		WireGuardPort: input.WireGuardPort, SessionUDPPort: input.SessionUDPPort, MTU: input.MTU,
		ConfigVersion: m.current.ConfigVersion + 1,
	}
	prefix, serverIP, err := validateNetwork(desired)
	if err != nil {
		return Network{}, err
	}
	nodes, err := m.store.ListNodes(ctx)
	if err != nil {
		return Network{}, err
	}
	assignments, err := ipam.PlanNodeAddresses(nodes, prefix, serverIP)
	if err != nil {
		return Network{}, err
	}
	if err := m.sessions.BeginNetworkMigration(ctx, "NETWORK_CONFIG_CHANGED"); err != nil {
		return Network{}, err
	}
	defer m.sessions.EndNetworkMigration()
	old := m.current
	oldPrefix, oldServerIP, _ := validateNetwork(old)
	oldAssignments := make(map[string]netip.Addr, len(nodes))
	oldPeers := make([]serverwg.Peer, 0, len(nodes))
	newPeers := make([]serverwg.Peer, 0, len(nodes))
	for _, node := range nodes {
		oldAssignments[node.ID] = node.OverlayIP
		oldPeers = append(oldPeers, serverwg.Peer{PublicKey: node.WGPublicKey, Address: node.OverlayIP})
		newPeers = append(newPeers, serverwg.Peer{PublicKey: node.WGPublicKey, Address: assignments[node.ID]})
	}
	serviceConfig := m.bootstrap.NetworkSnapshot()
	oldServiceConfig := serviceConfig
	serviceConfig.OverlayCIDR = prefix
	serviceConfig.ServerOverlayIP = serverIP
	serviceConfig.WGEndpoint, err = replaceEndpointPort(serviceConfig.WGEndpoint, desired.WireGuardPort)
	if err != nil {
		return Network{}, err
	}
	serviceConfig.ControlURL, err = replaceControlHost(serviceConfig.ControlURL, serverIP)
	if err != nil {
		return Network{}, err
	}
	serviceConfig.SessionUDPPort = desired.SessionUDPPort
	serviceConfig.MTU = desired.MTU
	serviceConfig.ConfigVersion = desired.ConfigVersion
	oldEncoded, _ := json.Marshal(old)
	desiredEncoded, _ := json.Marshal(desired)

	if err := m.sessions.ReconfigureNetwork(prefix, desired.MTU, desired.SessionUDPPort); err != nil {
		return Network{}, err
	}
	rollbackSessions := func() { _ = m.sessions.ReconfigureNetwork(oldPrefix, old.MTU, old.SessionUDPPort) }
	if err := m.store.ReplaceNodeOverlayIPsAndSetting(ctx, assignments, networkSettingKey, string(desiredEncoded)); err != nil {
		rollbackSessions()
		return Network{}, err
	}
	rollbackDatabase := func() {
		_ = m.store.ReplaceNodeOverlayIPsAndSetting(context.Background(), oldAssignments, networkSettingKey, string(oldEncoded))
	}
	if err := m.ipam.Reconfigure(prefix, serverIP); err != nil {
		rollbackDatabase()
		rollbackSessions()
		return Network{}, err
	}
	if err := m.bootstrap.UpdateNetwork(serviceConfig); err != nil {
		_ = m.ipam.Reconfigure(oldPrefix, oldServerIP)
		rollbackDatabase()
		rollbackSessions()
		return Network{}, err
	}
	if err := m.control.SetNetworkConfigVersion(desired.ConfigVersion); err != nil {
		_ = m.bootstrap.UpdateNetwork(oldServiceConfig)
		_ = m.ipam.Reconfigure(oldPrefix, oldServerIP)
		rollbackDatabase()
		rollbackSessions()
		return Network{}, err
	}
	rollbackPublishedConfig := func() {
		_ = m.control.SetNetworkConfigVersion(old.ConfigVersion)
		_ = m.bootstrap.UpdateNetwork(oldServiceConfig)
		_ = m.ipam.Reconfigure(oldPrefix, oldServerIP)
		rollbackDatabase()
		rollbackSessions()
	}

	// Publish the authoritative Bootstrap snapshot before notifying over the
	// still-live old Control path. Switching wg0 peers or the listener first
	// would make REBOOTSTRAP_REQUIRED physically undeliverable.
	for _, node := range nodes {
		_ = m.control.Send(ctx, node.ID, protocol.ControlRebootstrapRequired, protocol.RebootstrapRequiredPayload{
			ConfigVersion: desired.ConfigVersion, Reason: "NETWORK_CONFIG_CHANGED",
		})
	}
	if err := m.peers.Reconfigure(ctx, netip.PrefixFrom(serverIP, prefix.Bits()), desired.WireGuardPort, newPeers); err != nil {
		rollbackPublishedConfig()
		return Network{}, err
	}
	rollbackKernel := func() {
		_ = m.peers.Reconfigure(context.Background(), netip.PrefixFrom(oldServerIP, oldPrefix.Bits()), old.WireGuardPort, oldPeers)
	}
	if serverIP != oldServerIP {
		if err := m.rebindControl(serverIP); err != nil {
			rollbackKernel()
			rollbackPublishedConfig()
			return Network{}, fmt.Errorf("rebind Control listener after migration notification: %w", err)
		}
	}
	m.control.ResetConnections("Network configuration changed")
	m.current = desired
	return desired, nil
}

func validateNetwork(network Network) (netip.Prefix, netip.Addr, error) {
	prefix, err := netip.ParsePrefix(network.OverlayCIDR)
	if err != nil || !prefix.Addr().Is4() || prefix != prefix.Masked() || prefix.Bits() == 0 || prefix.Bits() > 30 {
		return netip.Prefix{}, netip.Addr{}, errors.New("Overlay CIDR must be canonical IPv4 with usable hosts")
	}
	serverIP, err := netip.ParseAddr(network.ServerOverlayIP)
	if err != nil || !serverIP.Is4() || !prefix.Contains(serverIP) || serverIP == prefix.Addr() || serverIP == lastIPv4(prefix) {
		return netip.Prefix{}, netip.Addr{}, errors.New("Server Overlay IP must be a usable address in Overlay CIDR")
	}
	for name, port := range map[string]int{"WireGuard port": network.WireGuardPort, "Session UDP port": network.SessionUDPPort} {
		if port < 1 || port > 65535 {
			return netip.Prefix{}, netip.Addr{}, fmt.Errorf("%s must be between 1 and 65535", name)
		}
	}
	if network.MTU < 576 || network.MTU > 65535 || network.ConfigVersion == 0 {
		return netip.Prefix{}, netip.Addr{}, errors.New("MTU or config version is invalid")
	}
	return prefix, serverIP, nil
}

func replaceEndpointPort(endpoint string, port int) (string, error) {
	host, _, err := net.SplitHostPort(endpoint)
	if err != nil {
		return "", err
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}

func replaceControlHost(raw string, host netip.Addr) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	_, port, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		return "", err
	}
	parsed.Host = net.JoinHostPort(host.String(), port)
	return parsed.String(), nil
}

func lastIPv4(prefix netip.Prefix) netip.Addr {
	bytes := prefix.Masked().Addr().As4()
	value := uint32(bytes[0])<<24 | uint32(bytes[1])<<16 | uint32(bytes[2])<<8 | uint32(bytes[3])
	value |= ^uint32(0) >> prefix.Bits()
	return netip.AddrFrom4([4]byte{byte(value >> 24), byte(value >> 16), byte(value >> 8), byte(value)})
}
