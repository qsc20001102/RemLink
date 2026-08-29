//go:build windows

package nodeagent

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestWaitForOverlayControlConnectsTCP(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan struct{})
	go func() {
		connection, err := listener.Accept()
		if err == nil {
			_ = connection.Close()
			close(accepted)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := waitForOverlayControl(ctx, "ws://"+listener.Addr().String()+"/control"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-accepted:
	case <-time.After(time.Second):
		t.Fatal("Overlay Control probe was not accepted")
	}
}
