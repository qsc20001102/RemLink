package subnet

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"
)

func TestListenerContinuesAfterPacketHandlerFailure(t *testing.T) {
	registry := NewRegistry()
	peer := netip.MustParseAddr("127.0.0.1")
	if err := registry.Upsert(SessionBinding{
		SessionID: 41, PeerOverlayIP: peer, EngineerOverlayIP: peer,
		RemoteCIDRs: []netip.Prefix{netip.MustParsePrefix("192.168.13.0/24")},
		Direction:   EngineerToSite, Active: true,
	}); err != nil {
		t.Fatal(err)
	}
	var calls, rejects atomic.Int32
	listener, err := NewListener(peer, 0, registry, func(context.Context, uint64, []byte) error {
		if calls.Add(1) == 1 {
			return errors.New("injection failed")
		}
		return nil
	}, func(error, netip.Addr) { rejects.Add(1) })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- listener.Run(ctx) }()
	connection, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IP(peer.AsSlice()), Port: listener.Port()})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	packet := testListenerIPv4("127.0.0.1", "192.168.13.10")
	datagram, err := EncodeDatagram(41, packet)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Write(datagram); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Write(datagram); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		accepted, rejected := listener.Counters()
		if accepted == 1 && rejected == 1 && rejects.Load() == 1 {
			cancel()
			<-done
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("listener counters accepted=%d rejected=%d callbacks=%d", func() uint64 { a, _ := listener.Counters(); return a }(), func() uint64 { _, r := listener.Counters(); return r }(), rejects.Load())
}

func testListenerIPv4(sourceText, destinationText string) []byte {
	packet := make([]byte, 20)
	packet[0] = 0x45
	packet[3] = 20
	source := netip.MustParseAddr(sourceText).As4()
	destination := netip.MustParseAddr(destinationText).As4()
	copy(packet[12:16], source[:])
	copy(packet[16:20], destination[:])
	return packet
}
