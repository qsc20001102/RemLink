//go:build !linux

package serverwg

import (
	"context"
	"errors"
	"net/netip"
	"time"
)

var ErrLinuxRequired = errors.New("kernel WireGuard Server requires Linux")

// Manager is unavailable on non-Linux build targets.
type Manager struct{}

func New(context.Context, Config) (*Manager, error) { return nil, ErrLinuxRequired }
func (*Manager) PublicKey() string                  { return "" }
func (*Manager) Close() error                       { return nil }
func (*Manager) EnsurePeer(context.Context, string, netip.Addr) error {
	return ErrLinuxRequired
}
func (*Manager) RemovePeer(context.Context, string) error { return ErrLinuxRequired }
func (*Manager) ReconcilePeers(context.Context, []Peer) error {
	return ErrLinuxRequired
}
func (*Manager) Reconfigure(context.Context, netip.Prefix, int, []Peer) error {
	return ErrLinuxRequired
}
func (*Manager) LastHandshake(context.Context, string) (*time.Time, error) {
	return nil, ErrLinuxRequired
}
