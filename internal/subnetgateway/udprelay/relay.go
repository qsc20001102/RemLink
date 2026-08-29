// Package udprelay preserves UDP datagram boundaries across a host socket flow.
package udprelay

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"time"
)

// Relay copies connected UDP datagrams until idle timeout or cancellation.
func Relay(ctx context.Context, left, right net.Conn, idleTimeout time.Duration) error {
	parent := ctx
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	if idleTimeout <= 0 {
		idleTimeout = 60 * time.Second
	}
	var lastActivity atomic.Int64
	lastActivity.Store(time.Now().UnixNano())
	results := make(chan error, 2)
	copyDatagrams := func(destination, source net.Conn) {
		buffer := make([]byte, 65535)
		for {
			deadline := time.Now().Add(min(idleTimeout/2, time.Second))
			_ = source.SetReadDeadline(deadline)
			count, err := source.Read(buffer)
			if err != nil {
				if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
					last := time.Unix(0, lastActivity.Load())
					if time.Since(last) < idleTimeout {
						continue
					}
				}
				results <- err
				return
			}
			if _, err := destination.Write(buffer[:count]); err != nil {
				results <- err
				return
			}
			lastActivity.Store(time.Now().UnixNano())
		}
	}
	go copyDatagrams(left, right)
	go copyDatagrams(right, left)
	go func() {
		<-ctx.Done()
		_ = left.Close()
		_ = right.Close()
	}()
	err := <-results
	cancel()
	_ = left.Close()
	_ = right.Close()
	<-results
	if parent.Err() != nil {
		return parent.Err()
	}
	if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		return nil
	}
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}
