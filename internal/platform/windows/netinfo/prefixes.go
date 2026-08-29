// Package netinfo inspects local IPv4 networks without mutating Windows state.
package netinfo

import (
	"fmt"
	"net"
	"net/netip"
)

// DirectIPv4Prefixes returns assigned non-loopback IPv4 interface prefixes,
// excluding the named RemLink adapter.
func DirectIPv4Prefixes(excludedInterface string) ([]netip.Prefix, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("list network interfaces: %w", err)
	}
	var prefixes []netip.Prefix
	for _, networkInterface := range interfaces {
		if networkInterface.Name == excludedInterface || networkInterface.Flags&net.FlagLoopback != 0 || networkInterface.Flags&net.FlagUp == 0 {
			continue
		}
		addresses, err := networkInterface.Addrs()
		if err != nil {
			return nil, fmt.Errorf("list addresses for %s: %w", networkInterface.Name, err)
		}
		for _, address := range addresses {
			prefix, err := netip.ParsePrefix(address.String())
			if err != nil || !prefix.Addr().Is4() {
				continue
			}
			prefixes = append(prefixes, prefix.Masked())
		}
	}
	return prefixes, nil
}

// PrefixesOverlap performs true containment-based IPv4 prefix overlap.
func PrefixesOverlap(left, right netip.Prefix) bool {
	if !left.Addr().Is4() || !right.Addr().Is4() {
		return false
	}
	left = left.Masked()
	right = right.Masked()
	return left.Contains(right.Addr()) || right.Contains(left.Addr())
}

// FindConflict returns the first local network overlapping desired.
func FindConflict(desired netip.Prefix, existing []netip.Prefix) (netip.Prefix, bool) {
	for _, candidate := range existing {
		if PrefixesOverlap(desired, candidate) {
			return candidate, true
		}
	}
	return netip.Prefix{}, false
}
