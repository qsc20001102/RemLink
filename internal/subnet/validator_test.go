package subnet

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"
)

func TestValidateDatagramBothDirections(t *testing.T) {
	remoteCIDR := netip.MustParsePrefix("192.168.13.0/24")
	engineerIP := netip.MustParseAddr("10.88.0.2")
	siteIP := netip.MustParseAddr("10.88.0.3")

	siteRegistry := NewRegistry()
	if err := siteRegistry.Upsert(SessionBinding{
		SessionID: 7, PeerOverlayIP: engineerIP, EngineerOverlayIP: engineerIP,
		RemoteCIDRs: []netip.Prefix{remoteCIDR}, Direction: EngineerToSite, Active: true,
	}); err != nil {
		t.Fatal(err)
	}
	requestDatagram, _ := EncodeDatagram(7, testIPv4("10.88.0.2", "192.168.13.10"))
	if _, _, err := ValidateDatagram(requestDatagram, engineerIP, siteRegistry); err != nil {
		t.Fatalf("Engineer-to-Site validation: %v", err)
	}

	engineerRegistry := NewRegistry()
	if err := engineerRegistry.Upsert(SessionBinding{
		SessionID: 7, PeerOverlayIP: siteIP, EngineerOverlayIP: engineerIP,
		RemoteCIDRs: []netip.Prefix{remoteCIDR}, Direction: SiteToEngineer, Active: true,
	}); err != nil {
		t.Fatal(err)
	}
	replyDatagram, _ := EncodeDatagram(7, testIPv4("192.168.13.10", "10.88.0.2"))
	if _, _, err := ValidateDatagram(replyDatagram, siteIP, engineerRegistry); err != nil {
		t.Fatalf("Site-to-Engineer validation: %v", err)
	}
	if _, _, err := ValidateDatagram(replyDatagram, netip.MustParseAddr("10.88.0.4"), engineerRegistry); !errors.Is(err, ErrOuterSourceMismatch) {
		t.Fatalf("wrong outer source error = %v", err)
	}
	wrongDatagram, _ := EncodeDatagram(7, testIPv4("192.168.14.10", "10.88.0.2"))
	if _, _, err := ValidateDatagram(wrongDatagram, siteIP, engineerRegistry); !errors.Is(err, ErrInnerSourceMismatch) {
		t.Fatalf("wrong inner source error = %v", err)
	}
}

func TestSenderListenerRoundTripOverOrdinaryUDP(t *testing.T) {
	listenerIP := netip.MustParseAddr("127.0.0.1")
	senderIP := netip.MustParseAddr("127.0.0.2")
	engineerIP := netip.MustParseAddr("10.88.0.2")
	registry := NewRegistry()
	if err := registry.Upsert(SessionBinding{
		SessionID: 42, PeerOverlayIP: senderIP, EngineerOverlayIP: engineerIP,
		RemoteCIDRs: []netip.Prefix{netip.MustParsePrefix("192.168.13.0/24")},
		Direction:   EngineerToSite, Active: true,
	}); err != nil {
		t.Fatal(err)
	}
	received := make(chan []byte, 1)
	listener, err := NewListener(listenerIP, 0, registry, func(_ context.Context, sessionID uint64, packet []byte) error {
		if sessionID != 42 {
			return errors.New("unexpected SessionID")
		}
		received <- packet
		return nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- listener.Run(ctx) }()
	sender, err := NewSender(ctx, SenderConfig{
		SessionID: 42, LocalIP: senderIP, PeerIP: listenerIP, PeerPort: listener.Port(), QueueCapacity: 4,
	})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	packet := testIPv4("10.88.0.2", "192.168.13.10")
	if !sender.Enqueue(append([]byte(nil), packet...)) {
		t.Fatal("Sender queue unexpectedly full")
	}
	select {
	case got := <-received:
		if string(got) != string(packet) {
			t.Fatal("received packet differs")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Session UDP")
	}
	_ = sender.Close()
	cancel()
	<-done
	accepted, rejected := listener.Counters()
	if accepted != 1 || rejected != 0 {
		t.Fatalf("listener counters accepted=%d rejected=%d", accepted, rejected)
	}
}

func testIPv4(sourceText, destinationText string) []byte {
	packet := make([]byte, 20)
	packet[0] = 0x45
	packet[3] = byte(len(packet))
	source := netip.MustParseAddr(sourceText).As4()
	destination := netip.MustParseAddr(destinationText).As4()
	copy(packet[12:16], source[:])
	copy(packet[16:20], destination[:])
	return packet
}
