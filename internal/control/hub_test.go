package control

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"remlink/internal/model"
	"remlink/internal/protocol"
)

type memoryNodes struct {
	mu     sync.Mutex
	nodes  map[string]model.Node
	tokens map[string]string
	events []model.EventLog
}

type statusChangeRecorder struct {
	mu      sync.Mutex
	changes []struct {
		node   model.Node
		status model.NodeStatus
	}
}

func (*statusChangeRecorder) HandleControl(context.Context, model.Node, protocol.ControlEnvelope) error {
	return nil
}

func (r *statusChangeRecorder) HandleNodeStatusChange(_ context.Context, node model.Node, status model.NodeStatus) error {
	r.mu.Lock()
	r.changes = append(r.changes, struct {
		node   model.Node
		status model.NodeStatus
	}{node: node, status: status})
	r.mu.Unlock()
	return nil
}

func (m *memoryNodes) AppendEvent(_ context.Context, event model.EventLog) error {
	m.mu.Lock()
	m.events = append(m.events, event)
	m.mu.Unlock()
	return nil
}

func (m *memoryNodes) AuthenticateNode(_ context.Context, nodeID, token string) (model.Node, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.tokens[nodeID] != token {
		return model.Node{}, errors.New("authentication failed")
	}
	return m.nodes[nodeID], nil
}

func (m *memoryNodes) ListNodes(context.Context) ([]model.Node, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]model.Node, 0, len(m.nodes))
	for _, node := range m.nodes {
		result = append(result, node)
	}
	return result, nil
}

func (m *memoryNodes) UpdateNodeHeartbeat(_ context.Context, nodeID string, status model.NodeStatus, at time.Time, version, osVersion string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	node := m.nodes[nodeID]
	node.Status = status
	node.LastSeen = &at
	node.Version = version
	node.OSVersion = osVersion
	m.nodes[nodeID] = node
	return nil
}

func (m *memoryNodes) UpdateNodeStatus(_ context.Context, nodeID string, status model.NodeStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	node := m.nodes[nodeID]
	node.Status = status
	m.nodes[nodeID] = node
	return nil
}

func TestHubHandshakeHeartbeatAndNodeList(t *testing.T) {
	store := testNodes()
	hub, err := NewHub(store, store, nil, HubConfig{NetworkConfigVersion: 3, HandshakeTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(hub)
	defer server.Close()
	controlURL := "ws" + strings.TrimPrefix(server.URL, "http")

	siteSocket := connectNode(t, controlURL, protocol.HelloPayload{
		NodeID: "site", NodeToken: "site-token", ConfigVersion: 3,
		Capabilities: protocol.NodeCapabilities{RemoteSubnet: true, NetstackStatus: "READY", TCPCapacity: 2048, UDPCapacity: 4096},
	})
	defer siteSocket.Close(websocket.StatusNormalClosure, "test done")
	engineerSocket := connectNode(t, controlURL, protocol.HelloPayload{
		NodeID: "engineer", NodeToken: "engineer-token", ConfigVersion: 3,
	})
	defer engineerSocket.Close(websocket.StatusNormalClosure, "test done")

	var nodeListEnvelope protocol.ControlEnvelope
	if err := wsjson.Read(context.Background(), engineerSocket, &nodeListEnvelope); err != nil {
		t.Fatal(err)
	}
	if nodeListEnvelope.Type != protocol.ControlNodeList {
		t.Fatalf("message type = %s, want NODE_LIST", nodeListEnvelope.Type)
	}
	var nodeList protocol.NodeListPayload
	if err := nodeListEnvelope.DecodePayload(&nodeList); err != nil {
		t.Fatal(err)
	}
	if len(nodeList.Sites) != 1 || !nodeList.Sites[0].Online || !nodeList.Sites[0].RemoteSubnetCapability {
		t.Fatalf("unexpected Node list: %+v", nodeList)
	}
	store.mu.Lock()
	eventCount := len(store.events)
	store.mu.Unlock()
	if eventCount < 2 {
		t.Fatalf("Control connection events = %d, want at least 2", eventCount)
	}

	heartbeat, _ := protocol.NewControlEnvelope(protocol.ControlHeartbeat, "hb-1", protocol.HeartbeatPayload{
		Timestamp: time.Now().UTC(), Status: "OK",
	})
	if err := wsjson.Write(context.Background(), engineerSocket, heartbeat); err != nil {
		t.Fatal(err)
	}
	var heartbeatReply protocol.ControlEnvelope
	if err := wsjson.Read(context.Background(), engineerSocket, &heartbeatReply); err != nil {
		t.Fatal(err)
	}
	if heartbeatReply.Type != protocol.ControlHeartbeat || heartbeatReply.RequestID != "hb-1" {
		t.Fatalf("heartbeat reply = %+v", heartbeatReply)
	}

	conflict, _ := protocol.NewControlEnvelope(protocol.ControlHeartbeat, "overlay-conflict-1", protocol.HeartbeatPayload{
		Timestamp: time.Now().UTC(), Status: string(protocol.ErrorOverlayLocalConflict),
	})
	if err := wsjson.Write(context.Background(), engineerSocket, conflict); err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Read(context.Background(), engineerSocket, &heartbeatReply); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	var conflictEvent *model.EventLog
	for index := range store.events {
		if store.events[index].Message == "节点拒绝了 Overlay 网络配置" {
			value := store.events[index]
			conflictEvent = &value
		}
	}
	store.mu.Unlock()
	if conflictEvent == nil || conflictEvent.Level != "ERROR" || conflictEvent.NodeID != "engineer" {
		t.Fatalf("Overlay conflict event = %+v", conflictEvent)
	}
	var fields map[string]any
	if err := json.Unmarshal(conflictEvent.FieldsJSON, &fields); err != nil || fields["error_code"] != string(protocol.ErrorOverlayLocalConflict) {
		t.Fatalf("Overlay conflict fields = %s, error=%v", conflictEvent.FieldsJSON, err)
	}
}

func TestHeartbeatThresholds(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		age  time.Duration
		want model.NodeStatus
	}{
		{15 * time.Second, model.NodeOnline},
		{15*time.Second + time.Nanosecond, model.NodeUnstable},
		{30 * time.Second, model.NodeUnstable},
		{30*time.Second + time.Nanosecond, model.NodeOffline},
	} {
		lastSeen := now.Add(-test.age)
		if got := statusAt(&lastSeen, now); got != test.want {
			t.Errorf("status at age %s = %s, want %s", test.age, got, test.want)
		}
	}
	if got := statusAt(nil, now); got != model.NodeOffline {
		t.Fatalf("nil last seen status = %s", got)
	}
}

func TestSweepNotifiesHandlerWhenSiteBecomesOffline(t *testing.T) {
	store := testNodes()
	recorder := &statusChangeRecorder{}
	hub, err := NewHub(store, store, recorder, HubConfig{NetworkConfigVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	if err := store.UpdateNodeHeartbeat(context.Background(), "site", model.NodeOnline, now.Add(-31*time.Second), "1.0", "test"); err != nil {
		t.Fatal(err)
	}
	if err := hub.Sweep(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if len(recorder.changes) != 1 || recorder.changes[0].node.ID != "site" || recorder.changes[0].status != model.NodeOffline {
		t.Fatalf("OFFLINE callbacks = %+v", recorder.changes)
	}
}

func TestHubRequiresRebootstrapOnConfigVersionMismatch(t *testing.T) {
	store := testNodes()
	hub, err := NewHub(store, store, nil, HubConfig{NetworkConfigVersion: 2})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(hub)
	defer server.Close()
	socket := connectNode(t, "ws"+strings.TrimPrefix(server.URL, "http"), protocol.HelloPayload{
		NodeID: "engineer", NodeToken: "engineer-token", ConfigVersion: 1,
	})
	defer socket.Close(websocket.StatusNormalClosure, "test done")
	var envelope protocol.ControlEnvelope
	if err := wsjson.Read(context.Background(), socket, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Type != protocol.ControlRebootstrapRequired {
		t.Fatalf("message type = %s, want REBOOTSTRAP_REQUIRED", envelope.Type)
	}
	var payload protocol.RebootstrapRequiredPayload
	if err := envelope.DecodePayload(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.ConfigVersion != 2 || payload.Reason != "CONFIG_VERSION_MISMATCH" {
		t.Fatalf("rebootstrap payload = %+v", payload)
	}
}

func TestClientCompletesHandshakeAndReceivesNodeList(t *testing.T) {
	store := testNodes()
	hub, err := NewHub(store, store, nil, HubConfig{NetworkConfigVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(hub)
	defer server.Close()
	received := make(chan protocol.ControlMessageType, 1)
	client, err := NewClient(ClientConfig{
		URL:               "ws" + strings.TrimPrefix(server.URL, "http"),
		Hello:             protocol.HelloPayload{NodeID: "engineer", NodeToken: "engineer-token", ConfigVersion: 1},
		HeartbeatInterval: 20 * time.Millisecond,
	}, func(_ context.Context, envelope protocol.ControlEnvelope) error {
		received <- envelope.Type
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := client.runOnce(ctx)
		done <- err
	}()
	select {
	case messageType := <-received:
		if messageType != protocol.ControlNodeList {
			t.Fatalf("received %s, want NODE_LIST", messageType)
		}
		cancel()
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("timed out waiting for Node list")
	}
	<-done
}

func TestClientReportsHeartbeatRTT(t *testing.T) {
	store := testNodes()
	hub, err := NewHub(store, store, nil, HubConfig{NetworkConfigVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(hub)
	defer server.Close()
	rtt := make(chan time.Duration, 1)
	client, err := NewClient(ClientConfig{
		URL:               "ws" + strings.TrimPrefix(server.URL, "http"),
		Hello:             protocol.HelloPayload{NodeID: "engineer", NodeToken: "engineer-token", ConfigVersion: 1},
		HeartbeatInterval: 10 * time.Millisecond,
		OnHeartbeatRTT:    func(delay time.Duration) { rtt <- delay },
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := client.runOnce(ctx)
		done <- err
	}()
	select {
	case delay := <-rtt:
		if delay < 0 || delay > time.Second {
			t.Fatalf("unexpected heartbeat RTT: %s", delay)
		}
		cancel()
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("timed out waiting for heartbeat RTT")
	}
	<-done
}

func TestClientRefreshesBootstrapAfterContinuousDisconnect(t *testing.T) {
	client, err := NewClient(ClientConfig{
		URL:                   "ws://127.0.0.1:1",
		Hello:                 protocol.HelloPayload{NodeID: "engineer", NodeToken: "token"},
		BootstrapRefreshAfter: 25 * time.Millisecond,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err = client.Run(ctx)
	if !errors.Is(err, protocol.ErrRebootstrapRequired) {
		t.Fatalf("Run error = %v, want ErrRebootstrapRequired", err)
	}
}

func connectNode(t *testing.T, controlURL string, hello protocol.HelloPayload) *websocket.Conn {
	t.Helper()
	socket, _, err := websocket.Dial(context.Background(), controlURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := protocol.NewControlEnvelope(protocol.ControlHello, "hello", hello)
	if err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Write(context.Background(), socket, envelope); err != nil {
		t.Fatal(err)
	}
	var welcomeEnvelope protocol.ControlEnvelope
	if err := wsjson.Read(context.Background(), socket, &welcomeEnvelope); err != nil {
		t.Fatal(err)
	}
	if welcomeEnvelope.Type != protocol.ControlWelcome {
		t.Fatalf("first message = %s, want WELCOME", welcomeEnvelope.Type)
	}
	return socket
}

func testNodes() *memoryNodes {
	return &memoryNodes{
		nodes: map[string]model.Node{
			"engineer": {ID: "engineer", Type: model.NodeTypeEngineer, Name: "Engineer", OverlayIP: netip.MustParseAddr("10.88.0.2"), Status: model.NodeOffline},
			"site":     {ID: "site", Type: model.NodeTypeSite, Name: "Site", OverlayIP: netip.MustParseAddr("10.88.0.3"), Status: model.NodeOffline},
		},
		tokens: map[string]string{"engineer": "engineer-token", "site": "site-token"},
	}
}
