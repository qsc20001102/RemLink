// Package clientwg embeds wireguard-go around the single RemLink Wintun.
package clientwg

import (
	"errors"
	"os"
	"sync"
	"sync/atomic"

	"golang.zx2c4.com/wireguard/tun"
)

var _ tun.Device = (*MuxTun)(nil)

// MuxTun is the deliberately thin Phase 1 wrapper around the real Wintun.
// Later phases add Read-side CIDR classification without changing this surface.
type MuxTun struct {
	base      tun.Device
	router    atomic.Pointer[PacketMux]
	writeMu   sync.Mutex
	closeOnce sync.Once
	closeErr  error
}

// NewMuxTun wraps a live TUN device without changing its semantics.
func NewMuxTun(base tun.Device) *MuxTun {
	return &MuxTun{base: base}
}

func (m *MuxTun) File() *os.File {
	return m.base.File()
}

func (m *MuxTun) Read(bufs [][]byte, sizes []int, offset int) (int, error) {
	router := m.router.Load()
	if router == nil {
		return m.base.Read(bufs, sizes, offset)
	}
	if len(bufs) == 0 || len(sizes) < len(bufs) || offset < 0 {
		return 0, errors.New("invalid MuxTun read buffers")
	}
	for {
		count, readErr := m.base.Read(bufs, sizes, offset)
		if count < 0 || count > len(bufs) {
			return 0, errors.New("base TUN returned invalid batch count")
		}
		overlayCount := 0
		for index := 0; index < count; index++ {
			size := sizes[index]
			if size < 0 || offset+size > len(bufs[index]) {
				return 0, errors.New("base TUN returned invalid packet size")
			}
			packet := bufs[index][offset : offset+size]
			class, sink := router.route(packet)
			switch class {
			case PacketOverlay:
				if overlayCount != index {
					if offset+size > len(bufs[overlayCount]) {
						return 0, errors.New("destination batch buffer is too small")
					}
					copy(bufs[overlayCount][offset:offset+size], packet)
				}
				sizes[overlayCount] = size
				overlayCount++
			case PacketRemote:
				if sink != nil {
					owned := append([]byte(nil), packet...)
					if !sink.Enqueue(owned) {
						router.recordDrop(DropEvent{Reason: DropRemoteQueueFull})
					}
				}
			}
		}
		if overlayCount > 0 || readErr != nil {
			return overlayCount, readErr
		}
		// A batch containing only Remote/dropped packets is consumed here. Read
		// again instead of returning 0,nil to wireguard-go and spinning it.
	}
}

func (m *MuxTun) Write(bufs [][]byte, offset int) (int, error) {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	return m.base.Write(bufs, offset)
}

// SetPacketMux enables or replaces Engineer outbound classification. A nil
// value restores Phase 1 passthrough behavior.
func (m *MuxTun) SetPacketMux(router *PacketMux) { m.router.Store(router) }

// InjectInbound serializes Site Session replies with wireguard-go writes.
func (m *MuxTun) InjectInbound(packet []byte) error {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	_, err := m.base.Write([][]byte{packet}, 0)
	return err
}

func (m *MuxTun) MTU() (int, error) {
	return m.base.MTU()
}

func (m *MuxTun) Name() (string, error) {
	return m.base.Name()
}

func (m *MuxTun) Events() <-chan tun.Event {
	return m.base.Events()
}

func (m *MuxTun) Close() error {
	m.closeOnce.Do(func() {
		m.closeErr = m.base.Close()
	})
	return m.closeErr
}

func (m *MuxTun) BatchSize() int {
	return m.base.BatchSize()
}
