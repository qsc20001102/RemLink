package subnet

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
)

type PacketHandler func(context.Context, uint64, []byte) error
type RejectHandler func(error, netip.Addr)

// Listener binds only the configured Overlay IP and validates before dispatch.
type Listener struct {
	conn          *net.UDPConn
	registry      *Registry
	handler       PacketHandler
	onReject      RejectHandler
	closeOnce     sync.Once
	counterMu     sync.RWMutex
	bySession     map[uint64]listenerCounters
	accepted      atomic.Uint64
	acceptedBytes atomic.Uint64
	rejected      atomic.Uint64
}

type listenerCounters struct {
	bytes   uint64
	packets uint64
}

func NewListener(localIP netip.Addr, port int, registry *Registry, handler PacketHandler, onReject RejectHandler) (*Listener, error) {
	if !localIP.Is4() || localIP.IsUnspecified() || port < 0 || port > 65535 || registry == nil || handler == nil {
		return nil, errors.New("Listener requires exact IPv4 local address, valid port, Registry, and handler")
	}
	connection, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IP(localIP.AsSlice()), Port: port})
	if err != nil {
		return nil, fmt.Errorf("bind Session Listener to %s:%d: %w", localIP, port, err)
	}
	return &Listener{conn: connection, registry: registry, handler: handler, onReject: onReject, bySession: make(map[uint64]listenerCounters)}, nil
}

func (l *Listener) Port() int { return l.conn.LocalAddr().(*net.UDPAddr).Port }

func (l *Listener) Close() error {
	var err error
	l.closeOnce.Do(func() { err = l.conn.Close() })
	return err
}

func (l *Listener) Counters() (accepted, rejected uint64) {
	return l.accepted.Load(), l.rejected.Load()
}

// DetailedCounters returns accepted payload bytes/packets and rejected datagrams.
func (l *Listener) DetailedCounters() (bytes, packets, rejected uint64) {
	return l.acceptedBytes.Load(), l.accepted.Load(), l.rejected.Load()
}

// SessionCounters returns accepted raw IPv4 payload bytes and packets for one SessionID.
func (l *Listener) SessionCounters(sessionID uint64) (bytes, packets uint64) {
	l.counterMu.RLock()
	counters := l.bySession[sessionID]
	l.counterMu.RUnlock()
	return counters.bytes, counters.packets
}

func (l *Listener) Run(ctx context.Context) error {
	stopClose := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = l.Close()
		case <-stopClose:
		}
	}()
	defer close(stopClose)
	buffer := make([]byte, 65535)
	for {
		count, source, err := l.conn.ReadFromUDPAddrPort(buffer)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return ctx.Err()
			}
			return fmt.Errorf("read Session UDP: %w", err)
		}
		sessionID, packet, err := ValidateDatagram(buffer[:count], source.Addr(), l.registry)
		if err != nil {
			l.rejected.Add(1)
			if l.onReject != nil {
				l.onReject(err, source.Addr())
			}
			continue
		}
		owned := append([]byte(nil), packet...)
		if err := l.handler(ctx, sessionID, owned); err != nil {
			l.rejected.Add(1)
			if l.onReject != nil {
				l.onReject(fmt.Errorf("handle Session packet: %w", err), source.Addr())
			}
			continue
		}
		l.acceptedBytes.Add(uint64(len(owned)))
		l.accepted.Add(1)
		l.counterMu.Lock()
		counters := l.bySession[sessionID]
		counters.bytes += uint64(len(owned))
		counters.packets++
		l.bySession[sessionID] = counters
		l.counterMu.Unlock()
	}
}
