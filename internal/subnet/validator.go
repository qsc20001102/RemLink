package subnet

import (
	"errors"
	"fmt"
	"net/netip"

	"remlink/internal/protocol"
)

var (
	ErrUnknownSession      = errors.New("unknown or inactive Session")
	ErrOuterSourceMismatch = errors.New("Session outer source does not match peer Overlay IP")
	ErrInnerIPv4Invalid    = errors.New("Session payload is not one complete IPv4 packet")
	ErrInnerSourceMismatch = errors.New("Session inner source is not allowed")
	ErrInnerTargetMismatch = errors.New("Session inner destination is not allowed")
)

// ValidateDatagram verifies framing, Active Session identity, outer source,
// IPv4 total length, and direction-specific inner addresses.
func ValidateDatagram(data []byte, outerSource netip.Addr, registry *Registry) (uint64, []byte, error) {
	if registry == nil {
		return 0, nil, ErrUnknownSession
	}
	header, packet, err := protocol.DecodeIPv4Session(data)
	if err != nil {
		return 0, nil, err
	}
	binding, found := registry.Lookup(header.SessionID)
	if !found || !binding.Active {
		return 0, nil, ErrUnknownSession
	}
	if outerSource.Unmap() != binding.PeerOverlayIP.Unmap() {
		return 0, nil, ErrOuterSourceMismatch
	}
	source, destination, err := ipv4Addresses(packet)
	if err != nil {
		return 0, nil, err
	}
	switch binding.Direction {
	case EngineerToSite:
		if source != binding.EngineerOverlayIP {
			return 0, nil, fmt.Errorf("%w: got %s want %s", ErrInnerSourceMismatch, source, binding.EngineerOverlayIP)
		}
		if !containedBy(destination, binding.RemoteCIDRs) {
			return 0, nil, fmt.Errorf("%w: %s is outside Remote CIDRs", ErrInnerTargetMismatch, destination)
		}
	case SiteToEngineer:
		if !containedBy(source, binding.RemoteCIDRs) {
			return 0, nil, fmt.Errorf("%w: %s is outside Remote CIDRs", ErrInnerSourceMismatch, source)
		}
		if destination != binding.EngineerOverlayIP {
			return 0, nil, fmt.Errorf("%w: got %s want %s", ErrInnerTargetMismatch, destination, binding.EngineerOverlayIP)
		}
	default:
		return 0, nil, ErrUnknownSession
	}
	return header.SessionID, packet, nil
}

func ipv4Addresses(packet []byte) (source, destination netip.Addr, err error) {
	if len(packet) < 20 || packet[0]>>4 != 4 {
		return netip.Addr{}, netip.Addr{}, ErrInnerIPv4Invalid
	}
	headerLength := int(packet[0]&0x0F) * 4
	totalLength := int(packet[2])<<8 | int(packet[3])
	if headerLength < 20 || headerLength > len(packet) || totalLength != len(packet) || totalLength < headerLength {
		return netip.Addr{}, netip.Addr{}, ErrInnerIPv4Invalid
	}
	source = netip.AddrFrom4([4]byte{packet[12], packet[13], packet[14], packet[15]})
	destination = netip.AddrFrom4([4]byte{packet[16], packet[17], packet[18], packet[19]})
	return source, destination, nil
}

func containedBy(address netip.Addr, prefixes []netip.Prefix) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}
