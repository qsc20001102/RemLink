// Package route owns RemLink Remote routes and performs conflict/lookup checks.
package route

import (
	"errors"
	"net/netip"

	"remlink/internal/platform/windows/netinfo"
)

type LookupResult string

const (
	LookupDirect          LookupResult = "DIRECT"
	LookupRouted          LookupResult = "ROUTED"
	LookupDefaultOnly     LookupResult = "DEFAULT_ONLY"
	LookupNoRoute         LookupResult = "NO_ROUTE"
	LookupOverlayConflict LookupResult = "OVERLAY_CONFLICT"
)

type Entry struct {
	InterfaceLUID uint64
	Destination   netip.Prefix
	NextHop       netip.Addr
}

// ConflictsFrom applies the Engineer rule: ignore default and RemLink-owned
// routes, reject any other prefix overlap.
func ConflictsFrom(entries []Entry, desired netip.Prefix, remLinkLUID uint64) []Entry {
	var conflicts []Entry
	for _, entry := range entries {
		if entry.InterfaceLUID == remLinkLUID || !entry.Destination.Addr().Is4() || entry.Destination.Bits() == 0 {
			continue
		}
		if netinfo.PrefixesOverlap(desired, entry.Destination) {
			conflicts = append(conflicts, entry)
		}
	}
	return conflicts
}

// LookupFrom returns the most-specific Windows route classification used by Site PREPARE.
func LookupFrom(entries []Entry, target netip.Addr, overlay netip.Prefix, remLinkLUID uint64) LookupResult {
	if !target.Is4() {
		return LookupNoRoute
	}
	if overlay.Contains(target) {
		return LookupOverlayConflict
	}
	var best *Entry
	for index := range entries {
		entry := &entries[index]
		if !entry.Destination.Contains(target) || entry.InterfaceLUID == remLinkLUID {
			continue
		}
		if best == nil || entry.Destination.Bits() > best.Destination.Bits() {
			best = entry
		}
	}
	if best == nil {
		return LookupNoRoute
	}
	if best.Destination.Bits() == 0 {
		return LookupDefaultOnly
	}
	if !best.NextHop.IsValid() || best.NextHop.IsUnspecified() {
		return LookupDirect
	}
	return LookupRouted
}

func validateRemote(prefix, overlay netip.Prefix) error {
	if !prefix.Addr().Is4() || prefix != prefix.Masked() || prefix.Bits() == 0 {
		return errors.New("Remote CIDR must be canonical IPv4 and must not be 0.0.0.0/0")
	}
	if netinfo.PrefixesOverlap(prefix, overlay) {
		return errors.New("Remote CIDR overlaps Overlay CIDR")
	}
	return nil
}
