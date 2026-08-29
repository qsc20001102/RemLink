// Package tcprelay performs protocol-agnostic TCP byte forwarding.
package tcprelay

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
)

const bufferSize = 32 * 1024

var bufferPool = sync.Pool{New: func() any { return make([]byte, bufferSize) }}

// Relay copies both directions until EOF, cancellation, or an I/O failure.
func Relay(ctx context.Context, left, right net.Conn) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan error, 2)
	copyDirection := func(destination, source net.Conn) {
		buffer := bufferPool.Get().([]byte)
		_, err := io.CopyBuffer(destination, source, buffer)
		bufferPool.Put(buffer)
		if closeWriter, ok := destination.(interface{ CloseWrite() error }); ok {
			_ = closeWriter.CloseWrite()
		}
		results <- err
	}
	go copyDirection(left, right)
	go copyDirection(right, left)
	go func() {
		<-ctx.Done()
		_ = left.Close()
		_ = right.Close()
	}()
	first := <-results
	cancel()
	_ = left.Close()
	_ = right.Close()
	second := <-results
	if first != nil && !errors.Is(first, net.ErrClosed) {
		return first
	}
	if second != nil && !errors.Is(second, net.ErrClosed) {
		return second
	}
	return nil
}
