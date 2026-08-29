// Package ipam owns authoritative IPv4 overlay allocation.
package ipam

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sync"

	"remlink/internal/database"
	"remlink/internal/model"
)

var (
	// ErrAddressPoolExhausted reports that no usable node address remains.
	ErrAddressPoolExhausted = errors.New("overlay address pool exhausted")
	// ErrAddressUnavailable reports an invalid, reserved, or occupied manual address.
	ErrAddressUnavailable = errors.New("overlay address unavailable")
)

// Manager serializes all in-process allocation and modification operations.
// The database UNIQUE constraint remains the final consistency boundary.
type Manager struct {
	mu       sync.Mutex
	store    *database.Store
	prefix   netip.Prefix
	serverIP netip.Addr
}

// PlanNodeAddresses deterministically preserves usable allocations and assigns
// the lowest free addresses for Nodes that must move into a new Overlay pool.
func PlanNodeAddresses(nodes []model.Node, prefix netip.Prefix, serverIP netip.Addr) (map[string]netip.Addr, error) {
	prefix = prefix.Masked()
	if !prefix.Addr().Is4() || prefix.Bits() == 0 || prefix.Bits() > 30 || !serverIP.Is4() || !prefix.Contains(serverIP) ||
		serverIP == prefix.Addr() || serverIP == lastAddress(prefix) {
		return nil, errors.New("invalid Overlay migration network")
	}
	usable := func(address netip.Addr) bool {
		return address.Is4() && prefix.Contains(address) && address != prefix.Addr() && address != lastAddress(prefix) && address != serverIP
	}
	assignments := make(map[string]netip.Addr, len(nodes))
	used := make(map[netip.Addr]struct{}, len(nodes))
	for _, node := range nodes {
		if node.ID == "" {
			return nil, errors.New("Node ID is required for Overlay migration")
		}
		if usable(node.OverlayIP) {
			if _, duplicate := used[node.OverlayIP]; !duplicate {
				assignments[node.ID] = node.OverlayIP
				used[node.OverlayIP] = struct{}{}
			}
		}
	}
	for _, node := range nodes {
		if _, assigned := assignments[node.ID]; assigned {
			continue
		}
		found := false
		for address := prefix.Addr().Next(); prefix.Contains(address); address = address.Next() {
			if !usable(address) {
				continue
			}
			if _, occupied := used[address]; occupied {
				continue
			}
			assignments[node.ID] = address
			used[address] = struct{}{}
			found = true
			break
		}
		if !found {
			return nil, ErrAddressPoolExhausted
		}
	}
	return assignments, nil
}

// Reconfigure switches subsequent allocation/manual-validation to a migrated pool.
func (m *Manager) Reconfigure(prefix netip.Prefix, serverIP netip.Addr) error {
	prefix = prefix.Masked()
	if _, err := New(m.store, prefix, serverIP); err != nil {
		return err
	}
	m.mu.Lock()
	m.prefix = prefix
	m.serverIP = serverIP
	m.mu.Unlock()
	return nil
}

// New validates an IPv4 prefix and its reserved Server address.
func New(store *database.Store, prefix netip.Prefix, serverIP netip.Addr) (*Manager, error) {
	prefix = prefix.Masked()
	if !prefix.Addr().Is4() || prefix.Bits() == 0 || prefix.Bits() > 30 {
		return nil, errors.New("overlay prefix must be IPv4 with at least two usable addresses")
	}
	if !serverIP.Is4() || !prefix.Contains(serverIP) || serverIP == prefix.Addr() || serverIP == lastAddress(prefix) {
		return nil, errors.New("server address must be a usable address inside overlay prefix")
	}
	return &Manager{store: store, prefix: prefix, serverIP: serverIP}, nil
}

// ReserveNode creates a node using the lowest free usable address. Existing
// NodeIDs keep their allocation and return created=false.
func (m *Manager) ReserveNode(ctx context.Context, node model.Node) (reserved model.Node, created bool, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	existing, err := m.store.GetNode(ctx, node.ID)
	if err == nil {
		return existing, false, nil
	}
	if !errors.Is(err, database.ErrNodeNotFound) {
		return model.Node{}, false, err
	}
	used, err := m.used(ctx)
	if err != nil {
		return model.Node{}, false, err
	}
	address, ok := m.firstAvailable(used)
	if !ok {
		return model.Node{}, false, ErrAddressPoolExhausted
	}
	node.OverlayIP = address
	if err := m.store.CreateNode(ctx, node); err != nil {
		return model.Node{}, false, err
	}
	createdNode, err := m.store.GetNode(ctx, node.ID)
	if err != nil {
		return model.Node{}, false, err
	}
	return createdNode, true, nil
}

// ChangeNodeAddress performs the Web UI's authoritative manual reassignment.
func (m *Manager) ChangeNodeAddress(ctx context.Context, nodeID string, desired netip.Addr) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.usable(desired) {
		return fmt.Errorf("%w: %s is reserved or outside %s", ErrAddressUnavailable, desired, m.prefix)
	}
	used, err := m.used(ctx)
	if err != nil {
		return err
	}
	current, err := m.store.GetNode(ctx, nodeID)
	if err != nil {
		return err
	}
	if current.OverlayIP == desired {
		return nil
	}
	if _, occupied := used[desired]; occupied {
		return fmt.Errorf("%w: %s is already assigned", ErrAddressUnavailable, desired)
	}
	return m.store.UpdateNodeOverlayIP(ctx, nodeID, desired)
}

// ReleaseNode deletes the node record, token, and allocation. Peer revocation
// must succeed in the orchestration layer before this method is called.
func (m *Manager) ReleaseNode(ctx context.Context, nodeID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.store.DeleteNode(ctx, nodeID)
}

func (m *Manager) used(ctx context.Context) (map[netip.Addr]struct{}, error) {
	addresses, err := m.store.ListOverlayIPs(ctx)
	if err != nil {
		return nil, err
	}
	used := make(map[netip.Addr]struct{}, len(addresses))
	for _, address := range addresses {
		used[address] = struct{}{}
	}
	return used, nil
}

func (m *Manager) firstAvailable(used map[netip.Addr]struct{}) (netip.Addr, bool) {
	for address := m.prefix.Addr().Next(); m.prefix.Contains(address); address = address.Next() {
		if !m.usable(address) {
			continue
		}
		if _, occupied := used[address]; !occupied {
			return address, true
		}
	}
	return netip.Addr{}, false
}

func (m *Manager) usable(address netip.Addr) bool {
	return address.Is4() && m.prefix.Contains(address) && address != m.prefix.Addr() &&
		address != lastAddress(m.prefix) && address != m.serverIP
}

func lastAddress(prefix netip.Prefix) netip.Addr {
	bits := prefix.Bits()
	base := prefix.Masked().Addr().As4()
	hostBits := 32 - bits
	value := uint32(base[0])<<24 | uint32(base[1])<<16 | uint32(base[2])<<8 | uint32(base[3])
	value |= uint32(1)<<hostBits - 1
	return netip.AddrFrom4([4]byte{byte(value >> 24), byte(value >> 16), byte(value >> 8), byte(value)})
}
