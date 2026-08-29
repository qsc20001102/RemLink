package clientwg

import (
	"net/netip"
	"sync/atomic"
)

// PacketClass is the only Engineer outbound routing decision.
type PacketClass uint8

const (
	PacketDrop PacketClass = iota
	PacketOverlay
	PacketRemote
)

type DropReason string

const (
	DropInvalidIPv4          DropReason = "INVALID_IPV4"
	DropUnmanagedDestination DropReason = "UNMANAGED_DESTINATION"
	DropRemoteQueueFull      DropReason = "REMOTE_QUEUE_FULL"
)

type DropEvent struct {
	Reason      DropReason
	Destination netip.Addr
}

type dropHandler struct{ callback func(DropEvent) }

// RemoteSink accepts an owned copy without performing network I/O in Read.
type RemoteSink interface {
	Enqueue(packet []byte) bool
}

type routeSnapshot struct {
	overlay netip.Prefix
	remote  []netip.Prefix
	sink    RemoteSink
}

// PacketMux classifies raw IPv4 packets using an atomically replaced snapshot.
type PacketMux struct {
	routes         atomic.Pointer[routeSnapshot]
	onDrop         atomic.Pointer[dropHandler]
	overlayPackets atomic.Uint64
	remotePackets  atomic.Uint64
	remoteBytes    atomic.Uint64
	droppedPackets atomic.Uint64
}

func NewPacketMux(overlay netip.Prefix, remote []netip.Prefix, sink RemoteSink) *PacketMux {
	mux := &PacketMux{}
	mux.SetRoutes(overlay, remote, sink)
	return mux
}

// SetRoutes replaces the Active Session CIDRs as one consistent snapshot.
func (m *PacketMux) SetRoutes(overlay netip.Prefix, remote []netip.Prefix, sink RemoteSink) {
	copyOfRemote := append([]netip.Prefix(nil), remote...)
	m.routes.Store(&routeSnapshot{overlay: overlay.Masked(), remote: copyOfRemote, sink: sink})
}

// SetDropHandler installs an optional metadata-only callback for rate-limited
// logging. The callback never receives packet bytes.
func (m *PacketMux) SetDropHandler(callback func(DropEvent)) {
	if callback == nil {
		m.onDrop.Store(nil)
		return
	}
	m.onDrop.Store(&dropHandler{callback: callback})
}

// Classify validates enough of IPv4 to safely read Destination Address.
func (m *PacketMux) Classify(packet []byte) PacketClass {
	routes := m.routes.Load()
	class, _, _ := classifySnapshot(routes, packet)
	return class
}

func classifySnapshot(routes *routeSnapshot, packet []byte) (PacketClass, RemoteSink, DropEvent) {
	if routes == nil || len(packet) < 20 || packet[0]>>4 != 4 {
		return PacketDrop, nil, DropEvent{Reason: DropInvalidIPv4}
	}
	headerLength := int(packet[0]&0x0F) * 4
	totalLength := int(packet[2])<<8 | int(packet[3])
	if headerLength < 20 || headerLength > len(packet) || totalLength < headerLength || totalLength != len(packet) {
		return PacketDrop, nil, DropEvent{Reason: DropInvalidIPv4}
	}
	destination := netip.AddrFrom4([4]byte{packet[16], packet[17], packet[18], packet[19]})
	if routes.overlay.IsValid() && routes.overlay.Contains(destination) {
		return PacketOverlay, nil, DropEvent{}
	}
	for _, prefix := range routes.remote {
		if prefix.Contains(destination) {
			return PacketRemote, routes.sink, DropEvent{}
		}
	}
	return PacketDrop, nil, DropEvent{Reason: DropUnmanagedDestination, Destination: destination}
}

// Counters returns cumulative classification outcomes.
func (m *PacketMux) Counters() (overlay, remote, dropped uint64) {
	return m.overlayPackets.Load(), m.remotePackets.Load(), m.droppedPackets.Load()
}

// RemoteCounters is the Engineer-view Upload source of truth: bytes and
// packets are counted when PacketMux intercepts them, before queueing or UDP
// host I/O, exactly as required by the Session statistics contract.
func (m *PacketMux) RemoteCounters() (bytes, packets uint64) {
	return m.remoteBytes.Load(), m.remotePackets.Load()
}

func (m *PacketMux) route(packet []byte) (PacketClass, RemoteSink) {
	class, sink, drop := classifySnapshot(m.routes.Load(), packet)
	switch class {
	case PacketOverlay:
		m.overlayPackets.Add(1)
	case PacketRemote:
		m.remotePackets.Add(1)
		m.remoteBytes.Add(uint64(len(packet)))
	default:
		m.recordDrop(drop)
	}
	return class, sink
}

func (m *PacketMux) recordDrop(event DropEvent) {
	m.droppedPackets.Add(1)
	if handler := m.onDrop.Load(); handler != nil {
		handler.callback(event)
	}
}
