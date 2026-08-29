package clientwg

import (
	"errors"
	"os"
	"testing"

	"golang.zx2c4.com/wireguard/tun"
)

func TestMuxTunProxiesBaseSemanticsAndClosesOnce(t *testing.T) {
	t.Parallel()
	base := newFakeTUN()
	mux := NewMuxTun(base)
	if name, err := mux.Name(); err != nil || name != "RemLink" {
		t.Fatalf("Name() = (%q, %v)", name, err)
	}
	if mtu, err := mux.MTU(); err != nil || mtu != 1280 {
		t.Fatalf("MTU() = (%d, %v)", mtu, err)
	}
	if mux.BatchSize() != 1 {
		t.Fatalf("BatchSize() = %d, want 1", mux.BatchSize())
	}
	if mux.Events() != base.events {
		t.Fatal("Events() did not return the base channel")
	}

	buffer := make([]byte, 32)
	sizes := make([]int, 1)
	n, err := mux.Read([][]byte{buffer}, sizes, 4)
	if err != nil || n != 1 || sizes[0] != 3 || string(buffer[4:7]) != "out" {
		t.Fatalf("Read() = n=%d sizes=%v data=%q err=%v", n, sizes, buffer[4:7], err)
	}
	n, err = mux.Write([][]byte{[]byte("xxxxin")}, 4)
	if err != nil || n != 1 || string(base.written) != "in" {
		t.Fatalf("Write() = n=%d written=%q err=%v", n, base.written, err)
	}

	if err := mux.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := mux.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if base.closeCalls != 1 {
		t.Fatalf("base Close() calls = %d, want 1", base.closeCalls)
	}
}

func TestMuxTunPreservesBaseErrors(t *testing.T) {
	t.Parallel()
	want := errors.New("read failed")
	base := newFakeTUN()
	base.readErr = want
	_, err := NewMuxTun(base).Read([][]byte{make([]byte, 8)}, make([]int, 1), 0)
	if !errors.Is(err, want) {
		t.Fatalf("Read() error = %v, want %v", err, want)
	}
}

type fakeTUN struct {
	events     chan tun.Event
	written    []byte
	readErr    error
	closeCalls int
}

func newFakeTUN() *fakeTUN {
	return &fakeTUN{events: make(chan tun.Event, 1)}
}

func (f *fakeTUN) File() *os.File { return nil }

func (f *fakeTUN) Read(bufs [][]byte, sizes []int, offset int) (int, error) {
	if f.readErr != nil {
		return 0, f.readErr
	}
	sizes[0] = copy(bufs[0][offset:], []byte("out"))
	return 1, nil
}

func (f *fakeTUN) Write(bufs [][]byte, offset int) (int, error) {
	f.written = append(f.written[:0], bufs[0][offset:]...)
	return len(bufs), nil
}

func (f *fakeTUN) MTU() (int, error)     { return 1280, nil }
func (f *fakeTUN) Name() (string, error) { return "RemLink", nil }
func (f *fakeTUN) Events() <-chan tun.Event {
	return f.events
}
func (f *fakeTUN) Close() error {
	f.closeCalls++
	return nil
}
func (f *fakeTUN) BatchSize() int { return 1 }
