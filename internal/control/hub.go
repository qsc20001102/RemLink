// Package control implements the Overlay-only Control WebSocket plane.
package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"remlink/internal/localization"
	"remlink/internal/logging"
	"remlink/internal/model"
	"remlink/internal/protocol"
)

const (
	DefaultHeartbeatInterval = 5 * time.Second
	OnlineThreshold          = 15 * time.Second
	UnstableThreshold        = 30 * time.Second
	maxControlMessage        = 1 << 20
	defaultSendTimeout       = 10 * time.Second
)

// Authenticator verifies NodeID+NodeToken without exposing token hashes.
type Authenticator interface {
	AuthenticateNode(context.Context, string, string) (model.Node, error)
}

// NodeStore persists heartbeat-derived Node state.
type NodeStore interface {
	ListNodes(context.Context) ([]model.Node, error)
	UpdateNodeHeartbeat(context.Context, string, model.NodeStatus, time.Time, string, string) error
	UpdateNodeStatus(context.Context, string, model.NodeStatus) error
}

type eventAppender interface {
	AppendEvent(context.Context, model.EventLog) error
}

// MessageHandler receives authenticated non-heartbeat messages.
type MessageHandler interface {
	HandleControl(context.Context, model.Node, protocol.ControlEnvelope) error
}

// NodeStatusChangeHandler receives authoritative heartbeat state transitions.
// Session orchestration uses OFFLINE transitions to close stale live Sessions.
type NodeStatusChangeHandler interface {
	HandleNodeStatusChange(context.Context, model.Node, model.NodeStatus) error
}

type HubConfig struct {
	NetworkConfigVersion uint64
	EnforceRemoteIP      bool
	HeartbeatInterval    time.Duration
	HandshakeTimeout     time.Duration
}

// Hub owns at most one active Control socket per NodeID.
type Hub struct {
	mu            sync.RWMutex
	authenticator Authenticator
	store         NodeStore
	handler       MessageHandler
	config        HubConfig
	connections   map[string]*connection
	capabilities  map[string]protocol.NodeCapabilities
}

type connection struct {
	node   model.Node
	socket *websocket.Conn
	sendMu sync.Mutex
}

func NewHub(authenticator Authenticator, store NodeStore, handler MessageHandler, config HubConfig) (*Hub, error) {
	if authenticator == nil || store == nil {
		return nil, errors.New("Control authenticator and NodeStore are required")
	}
	if config.NetworkConfigVersion == 0 {
		return nil, errors.New("Control network config version must be positive")
	}
	if config.HeartbeatInterval <= 0 {
		config.HeartbeatInterval = DefaultHeartbeatInterval
	}
	if config.HandshakeTimeout <= 0 {
		config.HandshakeTimeout = 10 * time.Second
	}
	return &Hub{
		authenticator: authenticator, store: store, handler: handler, config: config,
		connections: make(map[string]*connection), capabilities: make(map[string]protocol.NodeCapabilities),
	}, nil
}

// SetMessageHandler installs the post-handshake protocol handler. It is safe
// to call during startup before accepting Control connections.
func (h *Hub) SetMessageHandler(handler MessageHandler) {
	h.mu.Lock()
	h.handler = handler
	h.mu.Unlock()
}

// ServeHTTP upgrades only /control requests and requires HELLO as message one.
func (h *Hub) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	remoteIP, remoteErr := remoteAddress(request.RemoteAddr)
	socket, err := websocket.Accept(writer, request, &websocket.AcceptOptions{
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		return
	}
	socket.SetReadLimit(maxControlMessage)
	defer socket.Close(websocket.StatusNormalClosure, "Control connection closed")
	if remoteErr != nil {
		_ = socket.Close(websocket.StatusPolicyViolation, "invalid Overlay source")
		return
	}

	handshakeContext, cancel := context.WithTimeout(context.Background(), h.config.HandshakeTimeout)
	var helloEnvelope protocol.ControlEnvelope
	if err := wsjson.Read(handshakeContext, socket, &helloEnvelope); err != nil {
		cancel()
		_ = socket.Close(websocket.StatusPolicyViolation, "HELLO required")
		return
	}
	cancel()
	if helloEnvelope.Type != protocol.ControlHello {
		_ = socket.Close(websocket.StatusPolicyViolation, "HELLO must be first")
		return
	}
	var hello protocol.HelloPayload
	if err := helloEnvelope.DecodePayload(&hello); err != nil {
		_ = socket.Close(websocket.StatusPolicyViolation, "invalid HELLO")
		return
	}
	node, err := h.authenticator.AuthenticateNode(context.Background(), hello.NodeID, hello.NodeToken)
	if err != nil {
		_ = socket.Close(websocket.StatusPolicyViolation, "Node authentication failed")
		return
	}
	if h.config.EnforceRemoteIP && node.OverlayIP != remoteIP {
		_ = socket.Close(websocket.StatusPolicyViolation, "Overlay source mismatch")
		return
	}

	connected := &connection{node: node, socket: socket}
	previous := h.register(connected, hello.Capabilities)
	if previous != nil {
		_ = previous.socket.Close(websocket.StatusPolicyViolation, "replaced by newer Node connection")
	}
	defer h.unregister(node.ID, connected)
	now := time.Now().UTC()
	if err := h.store.UpdateNodeHeartbeat(context.Background(), node.ID, model.NodeOnline, now, hello.Version, hello.OSVersion); err != nil {
		_ = socket.Close(websocket.StatusInternalError, "persist HELLO failed")
		return
	}
	connected.node.Status = model.NodeOnline
	connected.node.LastSeen = &now
	connected.node.Version = hello.Version
	connected.node.OSVersion = hello.OSVersion
	h.recordEvent(context.Background(), node.ID, "INFO", "节点 Control 通道已连接", map[string]any{
		"overlay_ip": node.OverlayIP.String(), "version": hello.Version, "os_version": hello.OSVersion,
	})
	h.mu.RLock()
	networkConfigVersion := h.config.NetworkConfigVersion
	h.mu.RUnlock()
	if err := h.send(connected, protocol.ControlWelcome, helloEnvelope.RequestID, protocol.WelcomePayload{
		ServerTime: now, NetworkConfigVersion: networkConfigVersion,
	}); err != nil {
		return
	}
	if hello.ConfigVersion != networkConfigVersion {
		_ = h.send(connected, protocol.ControlRebootstrapRequired, "", protocol.RebootstrapRequiredPayload{
			ConfigVersion: networkConfigVersion, Reason: "CONFIG_VERSION_MISMATCH",
		})
		return
	}
	if node.Type == model.NodeTypeEngineer {
		if err := h.sendNodeList(context.Background(), connected); err != nil {
			return
		}
	}
	if node.Type == model.NodeTypeSite {
		h.broadcastNodeLists(context.Background())
	}

	for {
		var envelope protocol.ControlEnvelope
		if err := wsjson.Read(context.Background(), socket, &envelope); err != nil {
			return
		}
		if envelope.Type == protocol.ControlHello || !envelope.Type.Valid() {
			_ = socket.Close(websocket.StatusPolicyViolation, "invalid Control message type")
			return
		}
		if envelope.Type == protocol.ControlHeartbeat {
			if err := h.handleHeartbeat(connected, envelope, hello.Version, hello.OSVersion); err != nil {
				return
			}
			continue
		}
		h.mu.RLock()
		handler := h.handler
		h.mu.RUnlock()
		if handler == nil {
			_ = socket.Close(websocket.StatusUnsupportedData, "message is not available in this phase")
			return
		}
		if err := handler.HandleControl(context.Background(), connected.node, envelope); err != nil {
			_ = socket.Close(websocket.StatusPolicyViolation, "Control message rejected")
			return
		}
	}
}

// SetNetworkConfigVersion updates WELCOME after a completed network migration.
func (h *Hub) SetNetworkConfigVersion(version uint64) error {
	if version == 0 {
		return errors.New("network config version must be positive")
	}
	h.mu.Lock()
	h.config.NetworkConfigVersion = version
	h.mu.Unlock()
	return nil
}

// ResetNodeConnection forces one Node to bootstrap/reconnect after an
// authoritative address or credential change.
func (h *Hub) ResetNodeConnection(nodeID, reason string) {
	h.mu.RLock()
	connected := h.connections[nodeID]
	h.mu.RUnlock()
	if connected != nil {
		_ = connected.socket.Close(websocket.StatusGoingAway, reason)
	}
}

// ResetConnections closes all current sockets after migration notifications.
func (h *Hub) ResetConnections(reason string) {
	h.mu.RLock()
	connections := make([]*connection, 0, len(h.connections))
	for _, connected := range h.connections {
		connections = append(connections, connected)
	}
	h.mu.RUnlock()
	for _, connected := range connections {
		_ = connected.socket.Close(websocket.StatusGoingAway, reason)
	}
}

// Run classifies persisted heartbeat age until ctx is canceled.
func (h *Hub) Run(ctx context.Context) error {
	ticker := time.NewTicker(h.config.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			h.closeAll()
			return ctx.Err()
		case now := <-ticker.C:
			if err := h.Sweep(ctx, now.UTC()); err != nil {
				return err
			}
		}
	}
}

// Sweep applies ONLINE/UNSTABLE/OFFLINE thresholds and refreshes Engineer lists.
func (h *Hub) Sweep(ctx context.Context, now time.Time) error {
	nodes, err := h.store.ListNodes(ctx)
	if err != nil {
		return err
	}
	changed := false
	for _, node := range nodes {
		status := statusAt(node.LastSeen, now)
		if node.Status == status {
			continue
		}
		if err := h.store.UpdateNodeStatus(ctx, node.ID, status); err != nil {
			return err
		}
		node.Status = status
		level := "WARN"
		if status == model.NodeOffline {
			level = "ERROR"
		}
		h.recordEvent(ctx, node.ID, level, "节点心跳状态变更为 "+localization.NodeStatus(string(status)), nil)
		if status == model.NodeOffline {
			h.mu.RLock()
			handler := h.handler
			h.mu.RUnlock()
			if listener, ok := handler.(NodeStatusChangeHandler); ok {
				if err := listener.HandleNodeStatusChange(ctx, node, status); err != nil {
					return fmt.Errorf("handle Node %s status %s: %w", node.ID, status, err)
				}
			}
		}
		changed = true
	}
	if changed {
		h.broadcastNodeLists(ctx)
	}
	return nil
}

func (h *Hub) recordEvent(ctx context.Context, nodeID, level, message string, fields map[string]any) {
	appender, ok := h.store.(eventAppender)
	if !ok {
		return
	}
	if fields == nil {
		fields = map[string]any{}
	}
	raw, _ := json.Marshal(fields)
	_ = appender.AppendEvent(ctx, model.EventLog{
		Level: level, Module: string(logging.ModuleControl), NodeID: nodeID, Message: message, FieldsJSON: raw,
	})
}

// Send routes a typed Server message to one connected Node.
func (h *Hub) Send(ctx context.Context, nodeID string, messageType protocol.ControlMessageType, payload any) error {
	return h.SendRequest(ctx, nodeID, messageType, "", payload)
}

// SendRequest preserves a request ID while routing a typed Server message.
func (h *Hub) SendRequest(ctx context.Context, nodeID string, messageType protocol.ControlMessageType, requestID string, payload any) error {
	h.mu.RLock()
	connected := h.connections[nodeID]
	h.mu.RUnlock()
	if connected == nil {
		return fmt.Errorf("Node %s has no Control connection", nodeID)
	}
	return h.sendContext(ctx, connected, messageType, requestID, payload)
}

func (h *Hub) handleHeartbeat(connected *connection, envelope protocol.ControlEnvelope, version, osVersion string) error {
	var heartbeat protocol.HeartbeatPayload
	if err := envelope.DecodePayload(&heartbeat); err != nil {
		return err
	}
	if heartbeat.Status == string(protocol.ErrorOverlayLocalConflict) {
		h.recordEvent(context.Background(), connected.node.ID, "ERROR", "节点拒绝了 Overlay 网络配置", map[string]any{
			"error_code": string(protocol.ErrorOverlayLocalConflict), "reported_at": heartbeat.Timestamp.UTC(),
		})
	}
	now := time.Now().UTC()
	if err := h.store.UpdateNodeHeartbeat(context.Background(), connected.node.ID, model.NodeOnline, now, version, osVersion); err != nil {
		return err
	}
	connected.node.Status = model.NodeOnline
	connected.node.LastSeen = &now
	return h.send(connected, protocol.ControlHeartbeat, envelope.RequestID,
		protocol.HeartbeatPayload{Timestamp: now, Status: string(model.NodeOnline)})
}

func (h *Hub) register(connected *connection, capabilities protocol.NodeCapabilities) *connection {
	h.mu.Lock()
	defer h.mu.Unlock()
	previous := h.connections[connected.node.ID]
	h.connections[connected.node.ID] = connected
	h.capabilities[connected.node.ID] = capabilities
	return previous
}

func (h *Hub) unregister(nodeID string, expected *connection) {
	h.mu.Lock()
	if h.connections[nodeID] == expected {
		delete(h.connections, nodeID)
	}
	h.mu.Unlock()
}

func (h *Hub) send(connected *connection, messageType protocol.ControlMessageType, requestID string, payload any) error {
	return h.sendContext(context.Background(), connected, messageType, requestID, payload)
}

func (h *Hub) sendContext(ctx context.Context, connected *connection, messageType protocol.ControlMessageType, requestID string, payload any) error {
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultSendTimeout)
		defer cancel()
	}
	envelope, err := protocol.NewControlEnvelope(messageType, requestID, payload)
	if err != nil {
		return err
	}
	connected.sendMu.Lock()
	defer connected.sendMu.Unlock()
	return wsjson.Write(ctx, connected.socket, envelope)
}

func (h *Hub) sendNodeList(ctx context.Context, connected *connection) error {
	payload, err := h.nodeList(ctx)
	if err != nil {
		return err
	}
	return h.sendContext(ctx, connected, protocol.ControlNodeList, "", payload)
}

func (h *Hub) broadcastNodeLists(ctx context.Context) {
	h.mu.RLock()
	engineers := make([]*connection, 0)
	for _, connected := range h.connections {
		if connected.node.Type == model.NodeTypeEngineer {
			engineers = append(engineers, connected)
		}
	}
	h.mu.RUnlock()
	for _, engineer := range engineers {
		_ = h.sendNodeList(ctx, engineer)
	}
}

func (h *Hub) nodeList(ctx context.Context) (protocol.NodeListPayload, error) {
	nodes, err := h.store.ListNodes(ctx)
	if err != nil {
		return protocol.NodeListPayload{}, err
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	payload := protocol.NodeListPayload{Sites: make([]protocol.SiteSummary, 0)}
	for _, node := range nodes {
		if node.Type != model.NodeTypeSite {
			continue
		}
		site := protocol.SiteSummary{
			NodeID: node.ID, Name: node.Name, OverlayIP: node.OverlayIP.String(),
			Online:                 node.Status == model.NodeOnline,
			RemoteSubnetCapability: h.capabilities[node.ID].RemoteSubnet,
		}
		if node.LastSeen != nil {
			site.LastSeen = *node.LastSeen
		}
		payload.Sites = append(payload.Sites, site)
	}
	return payload, nil
}

func (h *Hub) closeAll() {
	h.mu.Lock()
	connections := make([]*connection, 0, len(h.connections))
	for _, connected := range h.connections {
		connections = append(connections, connected)
	}
	h.connections = make(map[string]*connection)
	h.mu.Unlock()
	for _, connected := range connections {
		_ = connected.socket.Close(websocket.StatusGoingAway, "Server stopping")
	}
}

func statusAt(lastSeen *time.Time, now time.Time) model.NodeStatus {
	if lastSeen == nil {
		return model.NodeOffline
	}
	age := now.Sub(*lastSeen)
	if age <= OnlineThreshold {
		return model.NodeOnline
	}
	if age <= UnstableThreshold {
		return model.NodeUnstable
	}
	return model.NodeOffline
}

func remoteAddress(remote string) (netip.Addr, error) {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return netip.Addr{}, err
	}
	return netip.ParseAddr(host)
}
