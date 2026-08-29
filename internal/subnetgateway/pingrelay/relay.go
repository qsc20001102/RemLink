// Package pingrelay probes a Site target and preserves Echo ID/Sequence.
package pingrelay

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

const DefaultTimeout = 3 * time.Second

type Relay struct {
	Timeout time.Duration
}

// Echo sends one host ICMP Echo and waits for the matching target reply.
func (r Relay) Echo(ctx context.Context, target netip.Addr, id, sequence int, data []byte) error {
	if !target.Is4() || id < 0 || id > 65535 || sequence < 0 || sequence > 65535 {
		return errors.New("PingRelay target, ID, or Sequence is invalid")
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	connection, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		return fmt.Errorf("open host ICMP socket: %w", err)
	}
	defer connection.Close()
	deadline := time.Now().Add(timeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := connection.SetDeadline(deadline); err != nil {
		return err
	}
	stopClose := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = connection.Close()
		case <-stopClose:
		}
	}()
	defer close(stopClose)
	message := icmp.Message{
		Type: ipv4.ICMPTypeEcho, Code: 0,
		Body: &icmp.Echo{ID: id, Seq: sequence, Data: append([]byte(nil), data...)},
	}
	encoded, err := message.Marshal(nil)
	if err != nil {
		return fmt.Errorf("marshal ICMP Echo: %w", err)
	}
	if _, err := connection.WriteTo(encoded, &net.IPAddr{IP: net.IP(target.AsSlice())}); err != nil {
		return fmt.Errorf("send ICMP Echo to %s: %w", target, err)
	}
	buffer := make([]byte, 1500)
	for {
		count, peer, err := connection.ReadFrom(buffer)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("wait for ICMP Echo Reply from %s: %w", target, err)
		}
		peerIP, ok := netip.AddrFromSlice(peer.(*net.IPAddr).IP)
		if !ok || peerIP.Unmap() != target {
			continue
		}
		parsed, err := icmp.ParseMessage(1, buffer[:count])
		if err != nil || parsed.Type != ipv4.ICMPTypeEchoReply {
			continue
		}
		echo, ok := parsed.Body.(*icmp.Echo)
		if ok && echo.ID == id && echo.Seq == sequence {
			return nil
		}
	}
}
