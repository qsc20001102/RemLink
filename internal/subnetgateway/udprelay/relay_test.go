package udprelay

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestRelayIdleTimeoutIsNormalFlowCompletion(t *testing.T) {
	leftRelay, leftPeer := net.Pipe()
	rightRelay, rightPeer := net.Pipe()
	defer leftPeer.Close()
	defer rightPeer.Close()
	done := make(chan error, 1)
	go func() { done <- Relay(context.Background(), leftRelay, rightRelay, 20*time.Millisecond) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("idle Relay returned %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("idle Relay did not reclaim the flow")
	}
}

func TestRelayReturnsParentCancellation(t *testing.T) {
	leftRelay, leftPeer := net.Pipe()
	rightRelay, rightPeer := net.Pipe()
	defer leftPeer.Close()
	defer rightPeer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Relay(ctx, leftRelay, rightRelay, time.Hour) }()
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("Relay cancellation = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled Relay did not stop")
	}
}
