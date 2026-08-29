package control

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"remlink/internal/protocol"
)

var ErrControlDisconnected = errors.New("Control WebSocket is not connected")

var DefaultReconnectBackoff = [...]time.Duration{
	1 * time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second,
}

const DefaultBootstrapRefreshAfter = 30 * time.Second

type ClientConfig struct {
	URL                   string
	Hello                 protocol.HelloPayload
	HeartbeatInterval     time.Duration
	HandshakeTimeout      time.Duration
	HTTPClient            *http.Client
	OnConnectionState     func(bool)
	OnHeartbeatRTT        func(time.Duration)
	BootstrapRefreshAfter time.Duration
}

type EnvelopeHandler func(context.Context, protocol.ControlEnvelope) error

// Client maintains one authenticated WebSocket with the specified backoff.
type Client struct {
	config  ClientConfig
	handler EnvelopeHandler
	mu      sync.RWMutex
	active  *clientConnection
}

type clientConnection struct {
	socket *websocket.Conn
	sendMu sync.Mutex
}

func NewClient(config ClientConfig, handler EnvelopeHandler) (*Client, error) {
	if config.URL == "" || config.Hello.NodeID == "" || config.Hello.NodeToken == "" {
		return nil, errors.New("Control URL, NodeID, and NodeToken are required")
	}
	if config.HeartbeatInterval <= 0 {
		config.HeartbeatInterval = DefaultHeartbeatInterval
	}
	if config.HandshakeTimeout <= 0 {
		config.HandshakeTimeout = 10 * time.Second
	}
	if config.BootstrapRefreshAfter <= 0 {
		config.BootstrapRefreshAfter = DefaultBootstrapRefreshAfter
	}
	return &Client{config: config, handler: handler}, nil
}

// Run reconnects until ctx is canceled. Successful handshakes reset backoff.
func (c *Client) Run(ctx context.Context) error {
	backoffIndex := 0
	disconnectedSince := time.Now()
	for {
		if time.Since(disconnectedSince) >= c.config.BootstrapRefreshAfter {
			return fmt.Errorf("Control unavailable for %s: %w", c.config.BootstrapRefreshAfter, protocol.ErrRebootstrapRequired)
		}
		connected, err := c.runOnce(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, protocol.ErrRebootstrapRequired) {
			return err
		}
		if connected {
			backoffIndex = 0
			disconnectedSince = time.Now()
		}
		delay := DefaultReconnectBackoff[backoffIndex]
		remaining := c.config.BootstrapRefreshAfter - time.Since(disconnectedSince)
		if delay > remaining {
			delay = remaining
		}
		if backoffIndex < len(DefaultReconnectBackoff)-1 {
			backoffIndex++
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
			_ = err
		}
	}
}

// Send writes one typed Node-to-Server message on the current authenticated
// connection. Callers may retry after ErrControlDisconnected.
func (c *Client) Send(ctx context.Context, messageType protocol.ControlMessageType, requestID string, payload any) error {
	c.mu.RLock()
	active := c.active
	c.mu.RUnlock()
	if active == nil {
		return ErrControlDisconnected
	}
	envelope, err := protocol.NewControlEnvelope(messageType, requestID, payload)
	if err != nil {
		return err
	}
	return active.write(ctx, envelope)
}

func (c *Client) runOnce(ctx context.Context) (bool, error) {
	dialOptions := &websocket.DialOptions{HTTPClient: c.config.HTTPClient, CompressionMode: websocket.CompressionDisabled}
	socket, response, err := websocket.Dial(ctx, c.config.URL, dialOptions)
	if err != nil {
		if response != nil {
			return false, fmt.Errorf("dial Control WebSocket: HTTP %d: %w", response.StatusCode, err)
		}
		return false, fmt.Errorf("dial Control WebSocket: %w", err)
	}
	defer socket.Close(websocket.StatusNormalClosure, "Node stopping")
	socket.SetReadLimit(maxControlMessage)

	handshakeContext, cancel := context.WithTimeout(ctx, c.config.HandshakeTimeout)
	helloEnvelope, err := protocol.NewControlEnvelope(protocol.ControlHello, "", c.config.Hello)
	if err != nil {
		cancel()
		return false, err
	}
	if err := wsjson.Write(handshakeContext, socket, helloEnvelope); err != nil {
		cancel()
		return false, fmt.Errorf("send HELLO: %w", err)
	}
	var welcomeEnvelope protocol.ControlEnvelope
	if err := wsjson.Read(handshakeContext, socket, &welcomeEnvelope); err != nil {
		cancel()
		return false, fmt.Errorf("read WELCOME: %w", err)
	}
	cancel()
	if welcomeEnvelope.Type != protocol.ControlWelcome {
		return false, fmt.Errorf("first Server message is %s, want WELCOME", welcomeEnvelope.Type)
	}
	var welcome protocol.WelcomePayload
	if err := welcomeEnvelope.DecodePayload(&welcome); err != nil {
		return false, err
	}
	active := &clientConnection{socket: socket}
	c.setActive(active)
	if c.config.OnConnectionState != nil {
		c.config.OnConnectionState(true)
	}
	defer func() {
		c.clearActive(active)
		if c.config.OnConnectionState != nil {
			c.config.OnConnectionState(false)
		}
	}()

	connectionContext, cancelConnection := context.WithCancel(ctx)
	defer cancelConnection()
	readErrors := make(chan error, 1)
	var heartbeatMu sync.Mutex
	heartbeats := make(map[string]time.Time)
	go func() {
		for {
			var envelope protocol.ControlEnvelope
			if err := wsjson.Read(connectionContext, socket, &envelope); err != nil {
				readErrors <- err
				return
			}
			if envelope.Type == protocol.ControlHeartbeat {
				heartbeatMu.Lock()
				sentAt, found := heartbeats[envelope.RequestID]
				if found {
					delete(heartbeats, envelope.RequestID)
				}
				heartbeatMu.Unlock()
				if found && c.config.OnHeartbeatRTT != nil {
					c.config.OnHeartbeatRTT(time.Since(sentAt))
				}
				continue
			}
			if !envelope.Type.Valid() {
				readErrors <- fmt.Errorf("invalid Server Control type %q", envelope.Type)
				return
			}
			if c.handler != nil {
				if err := c.handler(connectionContext, envelope); err != nil {
					readErrors <- err
					return
				}
			}
		}
	}()

	ticker := time.NewTicker(c.config.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return true, ctx.Err()
		case err := <-readErrors:
			return true, fmt.Errorf("read Control message: %w", err)
		case now := <-ticker.C:
			requestID := fmt.Sprintf("hb-%d", now.UnixNano())
			envelope, err := protocol.NewControlEnvelope(protocol.ControlHeartbeat, requestID, protocol.HeartbeatPayload{
				Timestamp: now.UTC(), Status: "OK",
			})
			if err != nil {
				return true, err
			}
			heartbeatMu.Lock()
			heartbeats[requestID] = time.Now()
			heartbeatMu.Unlock()
			err = active.write(ctx, envelope)
			if err != nil {
				heartbeatMu.Lock()
				delete(heartbeats, requestID)
				heartbeatMu.Unlock()
				return true, fmt.Errorf("send HEARTBEAT: %w", err)
			}
		}
	}
}

func (c *Client) setActive(active *clientConnection) {
	c.mu.Lock()
	c.active = active
	c.mu.Unlock()
}

func (c *Client) clearActive(expected *clientConnection) {
	c.mu.Lock()
	if c.active == expected {
		c.active = nil
	}
	c.mu.Unlock()
}

func (c *clientConnection) write(ctx context.Context, envelope protocol.ControlEnvelope) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	return wsjson.Write(ctx, c.socket, envelope)
}
