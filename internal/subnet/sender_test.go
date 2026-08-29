package subnet

import (
	"context"
	"net/netip"
	"testing"
)

func TestSenderRejectsEnqueueAfterClose(t *testing.T) {
	sender, err := NewSender(context.Background(), SenderConfig{
		SessionID: 55,
		LocalIP:   netip.MustParseAddr("127.0.0.1"),
		PeerIP:    netip.MustParseAddr("127.0.0.2"),
		PeerPort:  6200,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := sender.Close(); err != nil {
		t.Fatal(err)
	}
	if sender.Enqueue([]byte{1, 2, 3}) {
		t.Fatal("closed Sender accepted a packet with no running consumer")
	}
}

func TestValidateSenderConfigRejectsInvalidAddressRoles(t *testing.T) {
	valid := SenderConfig{SessionID: 1, LocalIP: netip.MustParseAddr("127.0.0.1"), PeerIP: netip.MustParseAddr("127.0.0.2"), PeerPort: 6200}
	if err := validateSenderConfig(valid); err != nil {
		t.Fatalf("valid Sender config error = %v", err)
	}
	invalid := []SenderConfig{
		{SessionID: 1, LocalIP: netip.IPv4Unspecified(), PeerIP: valid.PeerIP, PeerPort: 6200},
		{SessionID: 1, LocalIP: valid.LocalIP, PeerIP: netip.MustParseAddr("224.0.0.1"), PeerPort: 6200},
		{SessionID: 1, LocalIP: valid.LocalIP, PeerIP: valid.LocalIP, PeerPort: 6200},
	}
	for _, current := range invalid {
		if err := validateSenderConfig(current); err == nil {
			t.Errorf("invalid Sender config accepted: %+v", current)
		}
	}
}
