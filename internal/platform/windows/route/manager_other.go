//go:build !windows

package route

import (
	"errors"
	"net/netip"
)

var ErrWindowsRequired = errors.New("Windows RouteManager requires Windows")

type OwnershipStore interface {
	LoadOwnedRoutes() ([]netip.Prefix, error)
	SaveOwnedRoutes([]netip.Prefix) error
}

type Manager struct{}

func NewManager(uint64, netip.Prefix, OwnershipStore) (*Manager, error) {
	return nil, ErrWindowsRequired
}
func (*Manager) AddRemote(netip.Prefix) error            { return ErrWindowsRequired }
func (*Manager) RemoveRemote(netip.Prefix) error         { return ErrWindowsRequired }
func (*Manager) Conflicts(netip.Prefix) ([]Entry, error) { return nil, ErrWindowsRequired }
func (*Manager) Lookup(netip.Addr) (LookupResult, error) { return LookupNoRoute, ErrWindowsRequired }
func (*Manager) Reconcile() error                        { return ErrWindowsRequired }
