//go:build linux

package serverwg

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/vishvananda/netlink"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// Manager owns RemLink's wg0 and its dynamic Node peers.
type Manager struct {
	mu     sync.Mutex
	config Config
	client *wgctrl.Client
	key    wgtypes.Key
}

// New creates/reuses the kernel WireGuard interface and applies hub settings.
func New(ctx context.Context, config Config) (*Manager, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key, err := LoadOrCreatePrivateKey(config.PrivateKeyPath)
	if err != nil {
		return nil, err
	}
	link, err := ensureLink(config.InterfaceName)
	if err != nil {
		return nil, err
	}
	if err := configureAddress(link, config.Address); err != nil {
		return nil, err
	}
	client, err := wgctrl.New()
	if err != nil {
		return nil, fmt.Errorf("open wgctrl: %w", err)
	}
	manager := &Manager{config: config, client: client, key: key}
	if err := client.ConfigureDevice(config.InterfaceName, wgtypes.Config{
		PrivateKey: &key, ListenPort: &config.ListenPort,
	}); err != nil {
		client.Close()
		return nil, fmt.Errorf("configure kernel WireGuard interface %s: %w", config.InterfaceName, err)
	}
	if err := netlink.LinkSetUp(link); err != nil {
		client.Close()
		return nil, fmt.Errorf("bring up %s: %w", config.InterfaceName, err)
	}
	if config.EnableForwarding {
		if err := enableIPv4Forwarding(); err != nil {
			client.Close()
			return nil, err
		}
		if err := ensureHubForwardRule(ctx, config); err != nil {
			client.Close()
			return nil, err
		}
	}
	return manager, nil
}

// PublicKey is safe to return in Bootstrap responses.
func (m *Manager) PublicKey() string { return m.key.PublicKey().String() }

// Close releases control sockets but deliberately leaves the kernel interface.
func (m *Manager) Close() error { return m.client.Close() }

// EnsurePeer creates or replaces one Node's only AllowedIPs entry with /32.
func (m *Manager) EnsurePeer(ctx context.Context, publicKey string, address netip.Addr) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	peer, err := peerConfig(Peer{PublicKey: publicKey, Address: address})
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.client.ConfigureDevice(m.config.InterfaceName, wgtypes.Config{Peers: []wgtypes.PeerConfig{peer}}); err != nil {
		return fmt.Errorf("ensure WireGuard peer %s: %w", address, err)
	}
	// The kernel mutation has completed. A cancellation observed afterwards
	// must not be reported as failure, because callers may otherwise roll back
	// adjacent database state while leaving the peer applied.
	return nil
}

// RemovePeer revokes a Node's WireGuard public key.
func (m *Manager) RemovePeer(ctx context.Context, publicKey string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	key, err := wgtypes.ParseKey(publicKey)
	if err != nil {
		return fmt.Errorf("parse WireGuard public key: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.client.ConfigureDevice(m.config.InterfaceName, wgtypes.Config{
		Peers: []wgtypes.PeerConfig{{PublicKey: key, Remove: true}},
	}); err != nil {
		return fmt.Errorf("remove WireGuard peer: %w", err)
	}
	return nil
}

// LastHandshake returns the kernel-observed handshake time for one peer.
func (m *Manager) LastHandshake(ctx context.Context, publicKey string) (*time.Time, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key, err := wgtypes.ParseKey(publicKey)
	if err != nil {
		return nil, fmt.Errorf("parse WireGuard public key: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	device, err := m.client.Device(m.config.InterfaceName)
	if err != nil {
		return nil, fmt.Errorf("read WireGuard device %s: %w", m.config.InterfaceName, err)
	}
	for _, peer := range device.Peers {
		if peer.PublicKey == key {
			if peer.LastHandshakeTime.IsZero() {
				return nil, nil
			}
			handshake := peer.LastHandshakeTime.UTC()
			return &handshake, nil
		}
	}
	return nil, nil
}

// ReconcilePeers replaces kernel peers from the authoritative Node Registry.
func (m *Manager) ReconcilePeers(ctx context.Context, peers []Peer) error {
	configs := make([]wgtypes.PeerConfig, 0, len(peers))
	for _, peer := range peers {
		config, err := peerConfig(peer)
		if err != nil {
			return err
		}
		configs = append(configs, config)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.client.ConfigureDevice(m.config.InterfaceName, wgtypes.Config{
		ReplacePeers: true, Peers: configs,
	}); err != nil {
		return fmt.Errorf("reconcile WireGuard peers: %w", err)
	}
	return nil
}

// Reconfigure atomically applies a new Server Overlay address/listen port and
// complete peer set as part of Admin network migration.
func (m *Manager) Reconfigure(ctx context.Context, address netip.Prefix, listenPort int, peers []Peer) error {
	next := m.config
	next.Address = address
	next.ListenPort = listenPort
	if err := next.validate(); err != nil {
		return err
	}
	configs := make([]wgtypes.PeerConfig, 0, len(peers))
	for _, peer := range peers {
		config, err := peerConfig(peer)
		if err != nil {
			return err
		}
		configs = append(configs, config)
	}
	link, err := netlink.LinkByName(m.config.InterfaceName)
	if err != nil {
		return fmt.Errorf("look up %s for reconfiguration: %w", m.config.InterfaceName, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	oldAddress := m.config.Address
	if err := configureAddress(link, address); err != nil {
		_ = configureAddress(link, oldAddress)
		return err
	}
	if err := m.client.ConfigureDevice(m.config.InterfaceName, wgtypes.Config{
		ListenPort: &listenPort, ReplacePeers: true, Peers: configs,
	}); err != nil {
		_ = configureAddress(link, oldAddress)
		return fmt.Errorf("reconfigure kernel WireGuard: %w", err)
	}
	m.config = next
	return nil
}

func peerConfig(peer Peer) (wgtypes.PeerConfig, error) {
	key, err := wgtypes.ParseKey(peer.PublicKey)
	if err != nil {
		return wgtypes.PeerConfig{}, fmt.Errorf("parse WireGuard public key: %w", err)
	}
	if !peer.Address.Is4() {
		return wgtypes.PeerConfig{}, errors.New("WireGuard peer address must be IPv4")
	}
	bits := peer.Address.As4()
	allowedIP := net.IPNet{IP: net.IPv4(bits[0], bits[1], bits[2], bits[3]), Mask: net.CIDRMask(32, 32)}
	return wgtypes.PeerConfig{
		PublicKey: key, ReplaceAllowedIPs: true, AllowedIPs: []net.IPNet{allowedIP},
	}, nil
}

func ensureLink(name string) (netlink.Link, error) {
	link, err := netlink.LinkByName(name)
	if err == nil {
		if link.Type() != "wireguard" {
			return nil, fmt.Errorf("interface %s exists with type %s, want wireguard", name, link.Type())
		}
		return link, nil
	}
	if _, notFound := err.(netlink.LinkNotFoundError); !notFound {
		return nil, fmt.Errorf("look up interface %s: %w", name, err)
	}
	link = &netlink.GenericLink{LinkAttrs: netlink.LinkAttrs{Name: name}, LinkType: "wireguard"}
	if err := netlink.LinkAdd(link); err != nil {
		return nil, fmt.Errorf("create kernel WireGuard interface %s: %w", name, err)
	}
	return netlink.LinkByName(name)
}

func configureAddress(link netlink.Link, desired netip.Prefix) error {
	addresses, err := netlink.AddrList(link, netlink.FAMILY_V4)
	if err != nil {
		return fmt.Errorf("list %s addresses: %w", link.Attrs().Name, err)
	}
	desiredText := desired.String()
	for index := range addresses {
		if addresses[index].IPNet.String() == desiredText {
			continue
		}
		if err := netlink.AddrDel(link, &addresses[index]); err != nil {
			return fmt.Errorf("remove stale %s address %s: %w", link.Attrs().Name, addresses[index].IPNet, err)
		}
	}
	address, err := netlink.ParseAddr(desiredText)
	if err != nil {
		return fmt.Errorf("convert WireGuard address: %w", err)
	}
	if err := netlink.AddrReplace(link, address); err != nil {
		return fmt.Errorf("configure %s address %s: %w", link.Attrs().Name, desired, err)
	}
	return nil
}

func enableIPv4Forwarding() error {
	const path = "/proc/sys/net/ipv4/ip_forward"
	value, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read IPv4 forwarding state: %w", err)
	}
	if strings.TrimSpace(string(value)) == "1" {
		return nil
	}
	if err := os.WriteFile(path, []byte("1\n"), 0o644); err != nil {
		return fmt.Errorf("enable IPv4 forwarding: %w", err)
	}
	return nil
}

func ensureHubForwardRule(ctx context.Context, config Config) error {
	path := config.IPTablesPath
	if path == "" {
		var err error
		path, err = exec.LookPath("iptables")
		if err != nil {
			return errors.New("iptables is required to allow wg0-to-wg0 forwarding")
		}
	}
	arguments := []string{"FORWARD", "-i", config.InterfaceName, "-o", config.InterfaceName, "-j", "ACCEPT"}
	check := exec.CommandContext(ctx, path, append([]string{"-C"}, arguments...)...)
	if err := check.Run(); err == nil {
		return nil
	}
	insert := exec.CommandContext(ctx, path, append([]string{"-I"}, arguments...)...)
	output, err := insert.CombinedOutput()
	if err != nil {
		return fmt.Errorf("allow %s-to-%s forwarding: %w: %s", config.InterfaceName, config.InterfaceName, err, strings.TrimSpace(string(output)))
	}
	return nil
}
