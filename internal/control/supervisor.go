package control

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"time"
)

// Supervisor supports the required live Control-listener move during an
// Overlay network migration.
type Supervisor struct {
	mu       sync.Mutex
	handler  http.Handler
	port     int
	ctx      context.Context
	server   *http.Server
	listener net.Listener
	errors   chan error
	closed   bool
}

func NewSupervisor(handler http.Handler, port int) (*Supervisor, error) {
	if handler == nil || port < 1 || port > 65535 {
		return nil, errors.New("Control Supervisor requires Handler and valid port")
	}
	return &Supervisor{handler: handler, port: port, errors: make(chan error, 1)}, nil
}

func (s *Supervisor) Start(ctx context.Context, address netip.Addr) error {
	if !address.Is4() {
		return errors.New("Control listener address must be IPv4")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server != nil {
		return errors.New("Control Supervisor already started")
	}
	s.ctx = ctx
	server, listener, err := s.open(address)
	if err != nil {
		return err
	}
	s.server, s.listener = server, listener
	s.serve(server, listener)
	return nil
}

func (s *Supervisor) Rebind(address netip.Addr) error {
	if !address.Is4() {
		return errors.New("Control listener address must be IPv4")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.server == nil {
		return errors.New("Control Supervisor is not running")
	}
	newServer, newListener, err := s.open(address)
	if err != nil {
		return err
	}
	oldServer := s.server
	oldListener := s.listener
	s.server, s.listener = newServer, newListener
	s.serve(newServer, newListener)
	// From this point the rebind is committed and callers may safely switch the
	// rest of the Overlay. An old-server close failure must not be reported as
	// if the new listener were absent; that would cause the caller to roll back
	// wg0 while this Supervisor remained bound to the new address.
	_ = oldServer.Close()
	_ = oldListener.Close()
	return nil
}

func (s *Supervisor) Wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-s.errors:
		return err
	}
}

func (s *Supervisor) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	server := s.server
	s.mu.Unlock()
	if server == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		_ = server.Close()
		return err
	}
	return nil
}

func (s *Supervisor) open(address netip.Addr) (*http.Server, net.Listener, error) {
	listenAddress := net.JoinHostPort(address.String(), strconv.Itoa(s.port))
	listener, err := net.Listen("tcp4", listenAddress)
	if err != nil {
		return nil, nil, fmt.Errorf("listen for Overlay Control on %s: %w", listenAddress, err)
	}
	server := &http.Server{
		Addr: listenAddress, Handler: s.handler, ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 0, WriteTimeout: 0, IdleTimeout: 0, MaxHeaderBytes: 1 << 20,
	}
	return server, listener, nil
}

func (s *Supervisor) serve(server *http.Server, listener net.Listener) {
	go func() {
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) {
			return
		}
		select {
		case s.errors <- err:
		default:
		}
	}()
}
