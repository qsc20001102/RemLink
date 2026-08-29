package clientwg

import (
	"net/netip"
	"os"
	"testing"

	"golang.zx2c4.com/wireguard/tun"
)

type collectingSink struct{ packets [][]byte }

func (s *collectingSink) Enqueue(packet []byte) bool {
	s.packets = append(s.packets, packet)
	return true
}

type rejectingSink struct{}

func (rejectingSink) Enqueue([]byte) bool { return false }

func TestPacketMuxClassifiesIPv4Destination(t *testing.T) {
	mux := NewPacketMux(
		netip.MustParsePrefix("10.88.0.0/16"),
		[]netip.Prefix{netip.MustParsePrefix("192.168.13.0/24")},
		nil,
	)
	for _, test := range []struct {
		packet []byte
		want   PacketClass
	}{
		{ipv4Packet("10.88.0.3"), PacketOverlay},
		{ipv4Packet("192.168.13.10"), PacketRemote},
		{ipv4Packet("8.8.8.8"), PacketDrop},
		{[]byte{0x60, 0, 0, 20}, PacketDrop},
		{[]byte{0x45, 0, 0, 40}, PacketDrop},
	} {
		if got := mux.Classify(test.packet); got != test.want {
			t.Errorf("Classify(%v) = %v, want %v", test.packet, got, test.want)
		}
	}
}

func TestMuxTunConsumesRemoteOnlyBatchUntilOverlay(t *testing.T) {
	base := &sequenceTUN{
		events: make(chan tun.Event),
		batches: [][][]byte{
			{ipv4Packet("192.168.13.10")},
			{ipv4Packet("10.88.0.3")},
		},
	}
	sink := &collectingSink{}
	router := NewPacketMux(netip.MustParsePrefix("10.88.0.0/16"),
		[]netip.Prefix{netip.MustParsePrefix("192.168.13.0/24")}, sink)
	mux := NewMuxTun(base)
	mux.SetPacketMux(router)
	buffer := make([]byte, 128)
	sizes := make([]int, 1)
	count, err := mux.Read([][]byte{buffer}, sizes, 4)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 || base.reads != 2 || len(sink.packets) != 1 {
		t.Fatalf("count=%d reads=%d remote=%d", count, base.reads, len(sink.packets))
	}
	if destination(buffer[4:4+sizes[0]]) != netip.MustParseAddr("10.88.0.3") {
		t.Fatalf("returned packet destination = %s", destination(buffer[4:4+sizes[0]]))
	}
	if destination(sink.packets[0]) != netip.MustParseAddr("192.168.13.10") {
		t.Fatalf("queued packet destination = %s", destination(sink.packets[0]))
	}
	base.batches[0][0][16] = 1
	if destination(sink.packets[0]) != netip.MustParseAddr("192.168.13.10") {
		t.Fatal("RemoteSink packet aliases the base TUN buffer")
	}
}

func TestMuxTunCompactsMixedBatch(t *testing.T) {
	base := &sequenceTUN{
		events: make(chan tun.Event),
		batches: [][][]byte{{
			ipv4Packet("192.168.13.10"), ipv4Packet("10.88.0.4"), ipv4Packet("8.8.8.8"), ipv4Packet("10.88.0.5"),
		}},
	}
	sink := &collectingSink{}
	router := NewPacketMux(netip.MustParsePrefix("10.88.0.0/16"),
		[]netip.Prefix{netip.MustParsePrefix("192.168.13.0/24")}, sink)
	mux := NewMuxTun(base)
	mux.SetPacketMux(router)
	bufs := [][]byte{make([]byte, 64), make([]byte, 64), make([]byte, 64), make([]byte, 64)}
	sizes := make([]int, len(bufs))
	count, err := mux.Read(bufs, sizes, 0)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 || destination(bufs[0][:sizes[0]]).String() != "10.88.0.4" || destination(bufs[1][:sizes[1]]).String() != "10.88.0.5" {
		t.Fatalf("compacted count=%d destinations=%s,%s", count, destination(bufs[0][:sizes[0]]), destination(bufs[1][:sizes[1]]))
	}
	overlay, remote, dropped := router.Counters()
	if overlay != 2 || remote != 1 || dropped != 1 {
		t.Fatalf("counters overlay=%d remote=%d dropped=%d", overlay, remote, dropped)
	}
}

func TestPacketMuxDropCallbackContainsMetadataOnly(t *testing.T) {
	mux := NewPacketMux(netip.MustParsePrefix("10.88.0.0/16"), nil, nil)
	var events []DropEvent
	mux.SetDropHandler(func(event DropEvent) { events = append(events, event) })
	mux.route(ipv4Packet("8.8.8.8"))
	mux.route([]byte{0x60})
	if len(events) != 2 || events[0].Reason != DropUnmanagedDestination || events[0].Destination.String() != "8.8.8.8" || events[1].Reason != DropInvalidIPv4 {
		t.Fatalf("drop events = %+v", events)
	}
}

func TestPacketMuxCountsInterceptedUploadAndReportsQueueDrop(t *testing.T) {
	base := &sequenceTUN{
		events:  make(chan tun.Event),
		batches: [][][]byte{{ipv4Packet("192.168.13.10")}, {ipv4Packet("10.88.0.3")}},
	}
	router := NewPacketMux(netip.MustParsePrefix("10.88.0.0/16"),
		[]netip.Prefix{netip.MustParsePrefix("192.168.13.0/24")}, rejectingSink{})
	var drops []DropEvent
	router.SetDropHandler(func(event DropEvent) { drops = append(drops, event) })
	mux := NewMuxTun(base)
	mux.SetPacketMux(router)
	buffer := make([]byte, 64)
	sizes := make([]int, 1)
	if _, err := mux.Read([][]byte{buffer}, sizes, 0); err != nil {
		t.Fatal(err)
	}
	bytes, packets := router.RemoteCounters()
	_, remote, dropped := router.Counters()
	if bytes != 20 || packets != 1 || remote != 1 || dropped != 1 {
		t.Fatalf("Upload/drop counters bytes=%d packets=%d remote=%d dropped=%d", bytes, packets, remote, dropped)
	}
	if len(drops) != 1 || drops[0].Reason != DropRemoteQueueFull {
		t.Fatalf("queue drop events = %+v", drops)
	}
}

func TestPacketMuxRejectsTrailingBytesBeyondIPv4TotalLength(t *testing.T) {
	packet := append(ipv4Packet("192.168.13.10"), 0)
	mux := NewPacketMux(netip.MustParsePrefix("10.88.0.0/16"),
		[]netip.Prefix{netip.MustParsePrefix("192.168.13.0/24")}, nil)
	if got := mux.Classify(packet); got != PacketDrop {
		t.Fatalf("Classify packet with trailing bytes = %v, want PacketDrop", got)
	}
}

func ipv4Packet(destinationText string) []byte {
	packet := make([]byte, 20)
	packet[0] = 0x45
	packet[2] = 0
	packet[3] = 20
	packet[12] = 10
	packet[13] = 88
	packet[14] = 0
	packet[15] = 2
	destination := netip.MustParseAddr(destinationText).As4()
	copy(packet[16:20], destination[:])
	return packet
}

func destination(packet []byte) netip.Addr {
	return netip.AddrFrom4([4]byte{packet[16], packet[17], packet[18], packet[19]})
}

type sequenceTUN struct {
	events  chan tun.Event
	batches [][][]byte
	reads   int
}

func (t *sequenceTUN) File() *os.File { return nil }
func (t *sequenceTUN) Read(bufs [][]byte, sizes []int, offset int) (int, error) {
	batch := t.batches[t.reads]
	t.reads++
	for index, packet := range batch {
		sizes[index] = copy(bufs[index][offset:], packet)
	}
	return len(batch), nil
}
func (t *sequenceTUN) Write(bufs [][]byte, offset int) (int, error) { return len(bufs), nil }
func (t *sequenceTUN) MTU() (int, error)                            { return 1280, nil }
func (t *sequenceTUN) Name() (string, error)                        { return "RemLink", nil }
func (t *sequenceTUN) Events() <-chan tun.Event                     { return t.events }
func (t *sequenceTUN) Close() error                                 { return nil }
func (t *sequenceTUN) BatchSize() int                               { return 4 }
