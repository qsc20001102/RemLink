package subnet

import (
	"errors"
	"net/netip"
	"sync"
)

type Direction uint8

const (
	EngineerToSite Direction = iota + 1
	SiteToEngineer
)

// SessionBinding contains the exact identities used by receive validation.
type SessionBinding struct {
	SessionID         uint64
	PeerOverlayIP     netip.Addr
	EngineerOverlayIP netip.Addr
	RemoteCIDRs       []netip.Prefix
	Direction         Direction
	Active            bool
}

// Registry is the listener's concurrency-safe Active Session lookup.
type Registry struct {
	mu       sync.RWMutex
	sessions map[uint64]SessionBinding
}

func NewRegistry() *Registry { return &Registry{sessions: make(map[uint64]SessionBinding)} }

func (r *Registry) Upsert(binding SessionBinding) error {
	if err := validateBinding(binding); err != nil {
		return err
	}
	binding.RemoteCIDRs = append([]netip.Prefix(nil), binding.RemoteCIDRs...)
	r.mu.Lock()
	r.sessions[binding.SessionID] = binding
	r.mu.Unlock()
	return nil
}

func (r *Registry) Remove(sessionID uint64) {
	r.mu.Lock()
	delete(r.sessions, sessionID)
	r.mu.Unlock()
}

func (r *Registry) Lookup(sessionID uint64) (SessionBinding, bool) {
	r.mu.RLock()
	binding, found := r.sessions[sessionID]
	r.mu.RUnlock()
	return binding, found
}

func validateBinding(binding SessionBinding) error {
	if binding.SessionID == 0 || !binding.PeerOverlayIP.Is4() || !binding.EngineerOverlayIP.Is4() || len(binding.RemoteCIDRs) == 0 {
		return errors.New("Session binding requires nonzero ID, IPv4 peers, and Remote CIDRs")
	}
	if binding.Direction != EngineerToSite && binding.Direction != SiteToEngineer {
		return errors.New("Session binding direction is invalid")
	}
	for _, prefix := range binding.RemoteCIDRs {
		if !prefix.Addr().Is4() || prefix != prefix.Masked() || prefix.Bits() == 0 {
			return errors.New("Session Remote CIDRs must be canonical non-default IPv4 prefixes")
		}
	}
	return nil
}
