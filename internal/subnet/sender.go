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

const DefaultQueueCapacity = 1024

type SenderConfig struct {
	SessionID     uint64
	LocalIP       netip.Addr
	PeerIP        netip.Addr
	PeerPort      int
	QueueCapacity int
}

// Sender performs ordinary UDP socket I/O on a dedicated goroutine.
type Sender struct {
	conn        *net.UDPConn
	queue       chan []byte
	enqueueMu   sync.RWMutex
	closed      bool
	cancel      context.CancelFunc
	done        chan struct{}
	closeOnce   sync.Once
	sessionID   uint64
	sentBytes   atomic.Uint64
	sentPackets atomic.Uint64
	dropped     atomic.Uint64
	lastError   atomic.Value
}

type errorBox struct{ err error }

func NewSender(parent context.Context, config SenderConfig) (*Sender, error) {
	if err := validateSenderConfig(config); err != nil {
		return nil, err
	}
	if config.QueueCapacity <= 0 {
		config.QueueCapacity = DefaultQueueCapacity
	}
	connection, err := net.DialUDP("udp4",
		&net.UDPAddr{IP: net.IP(config.LocalIP.AsSlice())},
		&net.UDPAddr{IP: net.IP(config.PeerIP.AsSlice()), Port: config.PeerPort},
	)
	if err != nil {
		return nil, fmt.Errorf("bind Session Sender to %s and dial %s:%d: %w", config.LocalIP, config.PeerIP, config.PeerPort, err)
	}
	ctx, cancel := context.WithCancel(parent)
	sender := &Sender{
		conn: connection, queue: make(chan []byte, config.QueueCapacity), cancel: cancel,
		done: make(chan struct{}), sessionID: config.SessionID,
	}
	go sender.run(ctx)
	return sender, nil
}

func validateSenderConfig(config SenderConfig) error {
	if config.SessionID == 0 || !config.LocalIP.Is4() || !config.PeerIP.Is4() || config.PeerPort < 1 || config.PeerPort > 65535 {
		return errors.New("Sender requires SessionID, IPv4 local/peer addresses, and valid peer port")
	}
	if config.LocalIP.IsUnspecified() || config.PeerIP.IsUnspecified() || config.LocalIP.IsMulticast() || config.PeerIP.IsMulticast() || config.LocalIP == config.PeerIP {
		return errors.New("Sender local/peer addresses must be distinct unicast addresses")
	}
	return nil
}

// Enqueue never blocks PacketMux. packet ownership transfers to Sender on true.
func (s *Sender) Enqueue(packet []byte) bool {
	s.enqueueMu.RLock()
	defer s.enqueueMu.RUnlock()
	if s.closed {
		return false
	}
	select {
	case s.queue <- packet:
		return true
	default:
		s.dropped.Add(1)
		return false
	}
}

func (s *Sender) Close() error {
	s.closeOnce.Do(func() {
		s.enqueueMu.Lock()
		s.closed = true
		s.enqueueMu.Unlock()
		s.cancel()
		_ = s.conn.Close()
		<-s.done
	})
	return nil
}

func (s *Sender) Counters() (bytes, packets, dropped uint64) {
	return s.sentBytes.Load(), s.sentPackets.Load(), s.dropped.Load()
}

func (s *Sender) Err() error {
	value := s.lastError.Load()
	if value == nil {
		return nil
	}
	return value.(*errorBox).err
}

func (s *Sender) run(ctx context.Context) {
	defer close(s.done)
	for {
		select {
		case <-ctx.Done():
			return
		case packet := <-s.queue:
			datagram, err := EncodeDatagram(s.sessionID, packet)
			if err == nil {
				var written int
				written, err = s.conn.Write(datagram)
				if err == nil && written != len(datagram) {
					err = errors.New("short Session UDP write")
				}
			}
			if err != nil {
				s.lastError.Store(&errorBox{err: err})
				s.dropped.Add(1)
				continue
			}
			s.sentBytes.Add(uint64(len(packet)))
			s.sentPackets.Add(1)
		}
	}
}
