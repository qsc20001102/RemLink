//go:build windows

package route

import (
	"errors"
	"fmt"
	"net/netip"
	"sync"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

const remoteRouteMetric = 0

type OwnershipStore interface {
	LoadOwnedRoutes() ([]netip.Prefix, error)
	SaveOwnedRoutes([]netip.Prefix) error
}

// Manager is the sole writer for Engineer Remote CIDR routes.
type Manager struct {
	mu      sync.Mutex
	luid    winipcfg.LUID
	overlay netip.Prefix
	store   OwnershipStore
	owned   map[netip.Prefix]struct{}
}

func NewManager(luid uint64, overlay netip.Prefix, store OwnershipStore) (*Manager, error) {
	if luid == 0 || !overlay.Addr().Is4() || store == nil {
		return nil, errors.New("RouteManager requires RemLink LUID, IPv4 Overlay, and ownership store")
	}
	ownedRoutes, err := store.LoadOwnedRoutes()
	if err != nil {
		return nil, err
	}
	owned := make(map[netip.Prefix]struct{}, len(ownedRoutes))
	for _, prefix := range ownedRoutes {
		owned[prefix.Masked()] = struct{}{}
	}
	return &Manager{luid: winipcfg.LUID(luid), overlay: overlay.Masked(), store: store, owned: owned}, nil
}

func (m *Manager) AddRemote(prefix netip.Prefix) error {
	prefix = prefix.Masked()
	if err := validateRemote(prefix, m.overlay); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.owned[prefix]; exists {
		return nil
	}
	conflicts, err := m.conflictsLocked(prefix)
	if err != nil {
		return err
	}
	if len(conflicts) != 0 {
		return fmt.Errorf("Remote CIDR %s conflicts with existing route %s", prefix, conflicts[0].Destination)
	}
	if err := m.luid.AddRoute(prefix, netip.IPv4Unspecified(), remoteRouteMetric); err != nil {
		return fmt.Errorf("add RemLink Remote route %s: %w", prefix, err)
	}
	m.owned[prefix] = struct{}{}
	if err := m.persistLocked(); err != nil {
		delete(m.owned, prefix)
		_ = m.luid.DeleteRoute(prefix, netip.IPv4Unspecified())
		return err
	}
	return nil
}

func (m *Manager) RemoveRemote(prefix netip.Prefix) error {
	prefix = prefix.Masked()
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.owned[prefix]; !exists {
		return nil
	}
	if err := m.luid.DeleteRoute(prefix, netip.IPv4Unspecified()); err != nil && !errors.Is(err, windows.ERROR_NOT_FOUND) {
		return fmt.Errorf("remove RemLink Remote route %s: %w", prefix, err)
	}
	delete(m.owned, prefix)
	return m.persistLocked()
}

func (m *Manager) Conflicts(prefix netip.Prefix) ([]Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.conflictsLocked(prefix.Masked())
}

func (m *Manager) Lookup(target netip.Addr) (LookupResult, error) {
	entries, err := windowsEntries()
	if err != nil {
		return LookupNoRoute, err
	}
	return LookupFrom(entries, target, m.overlay, uint64(m.luid)), nil
}

// Reconcile removes every route recorded by a previous non-Active Session.
func (m *Manager) Reconcile() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for prefix := range m.owned {
		if err := m.luid.DeleteRoute(prefix, netip.IPv4Unspecified()); err != nil && !errors.Is(err, windows.ERROR_NOT_FOUND) {
			return fmt.Errorf("reconcile stale Remote route %s: %w", prefix, err)
		}
		delete(m.owned, prefix)
	}
	return m.persistLocked()
}

func (m *Manager) conflictsLocked(prefix netip.Prefix) ([]Entry, error) {
	entries, err := windowsEntries()
	if err != nil {
		return nil, err
	}
	return ConflictsFrom(entries, prefix, uint64(m.luid)), nil
}

func (m *Manager) persistLocked() error {
	prefixes := make([]netip.Prefix, 0, len(m.owned))
	for prefix := range m.owned {
		prefixes = append(prefixes, prefix)
	}
	if err := m.store.SaveOwnedRoutes(prefixes); err != nil {
		return fmt.Errorf("persist RemLink route ownership: %w", err)
	}
	return nil
}

func windowsEntries() ([]Entry, error) {
	rows, err := winipcfg.GetIPForwardTable2(winipcfg.AddressFamily(windows.AF_INET))
	if err != nil {
		return nil, fmt.Errorf("read Windows IPv4 route table: %w", err)
	}
	entries := make([]Entry, 0, len(rows))
	for index := range rows {
		prefix := rows[index].DestinationPrefix.Prefix()
		if !prefix.Addr().Is4() {
			continue
		}
		entries = append(entries, Entry{
			InterfaceLUID: uint64(rows[index].InterfaceLUID), Destination: prefix.Masked(), NextHop: rows[index].NextHop.Addr(),
		})
	}
	return entries, nil
}
