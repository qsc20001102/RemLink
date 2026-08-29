package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"remlink/internal/database"
	"remlink/internal/identity"
	"remlink/internal/ipam"
	"remlink/internal/model"
)

type fakePeers struct {
	mu      sync.Mutex
	entries map[string]netip.Addr
	err     error
}

func (p *fakePeers) EnsurePeer(_ context.Context, key string, address netip.Addr) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return p.err
	}
	p.entries[key] = address
	return nil
}

func TestJoinTokenEnsureRotateAndVerify(t *testing.T) {
	_, store, joins, _ := testService(t)
	ctx := context.Background()
	first, err := joins.Ensure(ctx)
	if err != nil {
		t.Fatal(err)
	}
	again, err := joins.Ensure(ctx)
	if err != nil || again != first {
		t.Fatalf("second Ensure = %q, %v; want stable token", again, err)
	}
	rotated, err := joins.Rotate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rotated == first {
		t.Fatal("Join Token rotation returned the previous value")
	}
	valid, err := joins.Verify(ctx, rotated)
	if err != nil || !valid {
		t.Fatalf("rotated token verification = %v, %v", valid, err)
	}
	valid, err = joins.Verify(ctx, first)
	if err != nil || valid {
		t.Fatalf("revoked token verification = %v, %v", valid, err)
	}
	serverID, err := EnsureServerID(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	serverIDAgain, err := EnsureServerID(ctx, store)
	if err != nil || serverIDAgain != serverID {
		t.Fatalf("Server ID = %q, %v; want %q", serverIDAgain, err, serverID)
	}
}

func TestValidateNetworkConfigRequiresOverlayControlEndpoint(t *testing.T) {
	privateKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	valid := NetworkConfig{
		ConfigVersion: 1, OverlayCIDR: "10.88.0.0/16", OverlayIP: "10.88.0.2", ServerOverlayIP: "10.88.0.1",
		ServerWGPublicKey: privateKey.PublicKey().String(), ServerWGEndpoint: "203.0.113.4:51820",
		ControlURL: "ws://10.88.0.1:7001/control", SessionUDPPort: 6200, MTU: 1280,
	}
	if err := ValidateNetworkConfig(valid); err != nil {
		t.Fatal(err)
	}
	for _, invalidURL := range []string{
		"http://10.88.0.1:7001/control", "ws://203.0.113.4:7001/control", "ws://10.88.0.1:7001/other", "ws://10.88.0.1/control",
		"ws://user@10.88.0.1:7001/control", "ws://10.88.0.1:7001/control?unexpected=true", "ws://10.88.0.1:7001/control#fragment",
		"ws://10.88.0.1:bad/control", "ws://10.88.0.1:7001/control?",
	} {
		invalid := valid
		invalid.ControlURL = invalidURL
		if err := ValidateNetworkConfig(invalid); err == nil {
			t.Errorf("accepted invalid Control URL %q", invalidURL)
		}
	}
	for _, invalidEndpoint := range []string{"", "missing-port", ":51820", "203.0.113.4:0", "203.0.113.4:65536"} {
		invalid := valid
		invalid.ServerWGEndpoint = invalidEndpoint
		if err := ValidateNetworkConfig(invalid); err == nil {
			t.Errorf("accepted invalid WireGuard endpoint %q", invalidEndpoint)
		}
	}
	for name, mutate := range map[string]func(*NetworkConfig){
		"Node network address":   func(config *NetworkConfig) { config.OverlayIP = "10.88.0.0" },
		"Node broadcast address": func(config *NetworkConfig) { config.OverlayIP = "10.88.255.255" },
		"Server network address": func(config *NetworkConfig) {
			config.ServerOverlayIP = "10.88.0.0"
			config.ControlURL = "ws://10.88.0.0:7001/control"
		},
		"unusable prefix": func(config *NetworkConfig) {
			config.OverlayCIDR = "10.88.0.0/31"
			config.OverlayIP = "10.88.0.0"
			config.ServerOverlayIP = "10.88.0.1"
			config.ControlURL = "ws://10.88.0.1:7001/control"
		},
		"Exit Node prefix": func(config *NetworkConfig) {
			config.OverlayCIDR = "0.0.0.0/0"
		},
	} {
		invalid := valid
		mutate(&invalid)
		if err := ValidateNetworkConfig(invalid); err == nil {
			t.Errorf("accepted %s", name)
		}
	}
}

func TestRegisterTenNodesAndConfigIsStable(t *testing.T) {
	service, store, joins, peers := testService(t)
	joinToken, err := joins.Ensure(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	addresses := map[string]struct{}{}
	for index := range 10 {
		request := validRegisterRequest(t, index, joinToken)
		response, err := service.Register(context.Background(), request)
		if err != nil {
			t.Fatalf("register %d: %v", index, err)
		}
		if _, duplicate := addresses[response.Network.OverlayIP]; duplicate {
			t.Fatalf("duplicate address %s", response.Network.OverlayIP)
		}
		addresses[response.Network.OverlayIP] = struct{}{}
		config, err := service.Config(context.Background(), ConfigRequest{NodeID: request.NodeID, NodeToken: response.NodeToken})
		if err != nil {
			t.Fatalf("config %d: %v", index, err)
		}
		if config.Network.OverlayIP != response.Network.OverlayIP || config.Network.ConfigVersion != 1 {
			t.Fatalf("unstable config: register=%+v config=%+v", response.Network, config.Network)
		}
	}
	nodes, err := store.ListNodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 10 || len(peers.entries) != 10 {
		t.Fatalf("nodes=%d peers=%d, want 10 each", len(nodes), len(peers.entries))
	}
}

func TestReregisterRotatesNodeTokenAndPreservesAddress(t *testing.T) {
	service, _, joins, _ := testService(t)
	joinToken, _ := joins.Ensure(context.Background())
	request := validRegisterRequest(t, 1, joinToken)
	first, err := service.Register(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	request.NodeName = "Renamed"
	second, err := service.Register(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Network.OverlayIP != second.Network.OverlayIP || first.NodeToken == second.NodeToken {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	if _, err := service.Config(context.Background(), ConfigRequest{NodeID: request.NodeID, NodeToken: first.NodeToken}); !errors.Is(err, ErrNodeAuthFailed) {
		t.Fatalf("old token config error = %v", err)
	}
	if _, err := service.Config(context.Background(), ConfigRequest{NodeID: request.NodeID, NodeToken: second.NodeToken}); err != nil {
		t.Fatalf("new token config: %v", err)
	}
}

func TestConfigReconcilesOldNodeRuntimeBeforeBootstrap(t *testing.T) {
	service, _, joins, _ := testService(t)
	joinToken, err := joins.Ensure(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	registered, err := service.Register(context.Background(), validRegisterRequest(t, 77, joinToken))
	if err != nil {
		t.Fatal(err)
	}
	var reconciledNode string
	service.SetNodeBootstrapHandler(func(_ context.Context, nodeID string) error {
		reconciledNode = nodeID
		return nil
	})
	request := ConfigRequest{NodeID: "00000000-0000-4000-8000-000000000077", NodeToken: registered.NodeToken}
	if _, err := service.Config(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if reconciledNode != request.NodeID {
		t.Fatalf("reconciled Node = %q, want %q", reconciledNode, request.NodeID)
	}
	service.SetNodeBootstrapHandler(func(context.Context, string) error { return errors.New("Session cleanup failed") })
	if _, err := service.Config(context.Background(), request); err == nil {
		t.Fatal("Bootstrap ignored Node runtime reconciliation failure")
	}
}

func TestPeerFailureRollsBackNewRegistration(t *testing.T) {
	service, store, joins, peers := testService(t)
	peers.err = errors.New("kernel unavailable")
	joinToken, _ := joins.Ensure(context.Background())
	request := validRegisterRequest(t, 2, joinToken)
	if _, err := service.Register(context.Background(), request); err == nil {
		t.Fatal("registration unexpectedly succeeded")
	}
	if _, err := store.GetNode(context.Background(), request.NodeID); !errors.Is(err, database.ErrNodeNotFound) {
		t.Fatalf("failed registration persisted: %v", err)
	}
}

func TestHTTPContractAndStrictJSON(t *testing.T) {
	service, _, joins, _ := testService(t)
	joinToken, _ := joins.Ensure(context.Background())
	handler := Handler(service)

	info := httptest.NewRecorder()
	handler.ServeHTTP(info, httptest.NewRequest(http.MethodGet, "/api/v1/server/info", nil))
	if info.Code != http.StatusOK || info.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("server info status=%d headers=%v", info.Code, info.Header())
	}

	request := validRegisterRequest(t, 4, joinToken)
	registered := performJSON(t, handler, http.MethodPost, "/api/v1/bootstrap/register", request)
	if registered.Code != http.StatusCreated {
		t.Fatalf("register status=%d body=%s", registered.Code, registered.Body.String())
	}
	var response RegisterResponse
	if err := json.Unmarshal(registered.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	configured := performJSON(t, handler, http.MethodPost, "/api/v1/bootstrap/config", ConfigRequest{
		NodeID: request.NodeID, NodeToken: response.NodeToken,
	})
	if configured.Code != http.StatusOK {
		t.Fatalf("config status=%d body=%s", configured.Code, configured.Body.String())
	}

	unknownField := httptest.NewRecorder()
	unknownBody := bytes.NewBufferString(`{"node_id":"x","node_token":"x","surprise":true}`)
	unknownRequest := httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap/config", unknownBody)
	unknownRequest.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(unknownField, unknownRequest)
	if unknownField.Code != http.StatusBadRequest {
		t.Fatalf("unknown JSON field status=%d", unknownField.Code)
	}
}

func TestBootstrapClient(t *testing.T) {
	service, _, joins, _ := testService(t)
	server := httptest.NewServer(Handler(service))
	defer server.Close()
	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	info, err := client.ServerInfo(ctx)
	if err != nil || info.APIVersion != 1 {
		t.Fatalf("ServerInfo = %+v, %v", info, err)
	}
	joinToken, _ := joins.Ensure(ctx)
	request := validRegisterRequest(t, 8, joinToken)
	registered, err := client.Register(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	configured, err := client.Config(ctx, ConfigRequest{NodeID: request.NodeID, NodeToken: registered.NodeToken})
	if err != nil || configured.Network.OverlayIP != registered.Network.OverlayIP {
		t.Fatalf("Config = %+v, %v", configured, err)
	}
	_, err = client.Config(ctx, ConfigRequest{NodeID: request.NodeID, NodeToken: "wrong"})
	var clientError *ClientError
	if !errors.As(err, &clientError) || clientError.Status != http.StatusUnauthorized {
		t.Fatalf("wrong-token error = %#v", err)
	}
}

func TestEnrollPersistsIdentityAndUsesConfigOnRestart(t *testing.T) {
	service, _, joins, _ := testService(t)
	server := httptest.NewServer(Handler(service))
	defer server.Close()
	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	identityStore, err := identity.NewStore(filepath.Join(t.TempDir(), "identity.json"), xorProtector{})
	if err != nil {
		t.Fatal(err)
	}
	joinToken, _ := joins.Ensure(context.Background())
	config := EnrollConfig{
		NodeType: model.NodeTypeSite, NodeName: "Site-A", ServerURL: server.URL,
		JoinToken: joinToken, Version: "test", OSVersion: "windows/amd64",
	}
	firstIdentity, firstNetwork, err := Enroll(context.Background(), identityStore, client, config)
	if err != nil {
		t.Fatal(err)
	}
	if firstIdentity.NodeToken == "" || firstIdentity.ConfigVersion != firstNetwork.ConfigVersion {
		t.Fatalf("incomplete enrolled identity: %+v", firstIdentity)
	}
	config.JoinToken = ""
	secondIdentity, secondNetwork, err := Enroll(context.Background(), identityStore, client, config)
	if err != nil {
		t.Fatal(err)
	}
	if secondIdentity.NodeID != firstIdentity.NodeID || secondIdentity.NodeToken != firstIdentity.NodeToken ||
		secondNetwork.OverlayIP != firstNetwork.OverlayIP {
		t.Fatalf("restart changed identity/config: first=%+v/%+v second=%+v/%+v", firstIdentity, firstNetwork, secondIdentity, secondNetwork)
	}
}

func TestEnrollRebindsOnlyUnregisteredPortableIdentity(t *testing.T) {
	service, _, joins, _ := testService(t)
	server := httptest.NewServer(Handler(service))
	defer server.Close()
	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	identityStore, err := identity.NewStore(filepath.Join(t.TempDir(), "identity.json"), xorProtector{})
	if err != nil {
		t.Fatal(err)
	}
	draft, err := identity.New(model.NodeTypeEngineer, "Engineer-A", "http://192.0.2.1:8080")
	if err != nil {
		t.Fatal(err)
	}
	if err := identityStore.Save(draft); err != nil {
		t.Fatal(err)
	}
	joinToken, _ := joins.Ensure(context.Background())
	config := EnrollConfig{
		NodeType: model.NodeTypeEngineer, NodeName: "Engineer-A", ServerURL: server.URL,
		JoinToken: joinToken, Version: "test", OSVersion: "windows/amd64",
	}

	registered, _, err := Enroll(context.Background(), identityStore, client, config)
	if err != nil {
		t.Fatalf("rebind unregistered identity: %v", err)
	}
	if registered.NodeID != draft.NodeID || registered.ServerURL != server.URL || registered.NodeToken == "" {
		t.Fatalf("unexpected rebound identity: %+v", registered)
	}

	config.ServerURL = "http://192.0.2.2:8080"
	config.JoinToken = ""
	if _, _, err := Enroll(context.Background(), identityStore, client, config); err == nil ||
		!strings.Contains(err.Error(), "已注册的 Node 身份") {
		t.Fatalf("registered identity Server change error = %v", err)
	}
}

func TestUpdateNetworkRejectsInvalidBootstrapTrustBoundary(t *testing.T) {
	service, _, _, _ := testService(t)
	original := service.NetworkSnapshot()
	tests := []struct {
		name   string
		mutate func(*ServiceConfig)
	}{
		{"exit-node-overlay", func(c *ServiceConfig) { c.OverlayCIDR = netip.MustParsePrefix("0.0.0.0/0") }},
		{"network-server-address", func(c *ServiceConfig) { c.ServerOverlayIP = netip.MustParseAddr("10.88.0.0") }},
		{"wireguard-endpoint", func(c *ServiceConfig) { c.WGEndpoint = "missing-port" }},
		{"control-host", func(c *ServiceConfig) { c.ControlURL = "ws://203.0.113.1:7001/control" }},
		{"control-user", func(c *ServiceConfig) { c.ControlURL = "ws://user@10.88.0.1:7001/control" }},
		{"control-empty-query", func(c *ServiceConfig) { c.ControlURL = "ws://10.88.0.1:7001/control?" }},
		{"session-port", func(c *ServiceConfig) { c.SessionUDPPort = 0 }},
		{"mtu", func(c *ServiceConfig) { c.MTU = 0 }},
		{"config-version", func(c *ServiceConfig) { c.ConfigVersion = 0 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := original
			test.mutate(&candidate)
			if err := service.UpdateNetwork(candidate); err == nil {
				t.Fatal("invalid Bootstrap network update was accepted")
			}
			if current := service.NetworkSnapshot(); current != original {
				t.Fatalf("rejected update changed Bootstrap snapshot: %+v", current)
			}
		})
	}
}

type xorProtector struct{}

func (xorProtector) Protect(value []byte) ([]byte, error)   { return xor(value), nil }
func (xorProtector) Unprotect(value []byte) ([]byte, error) { return xor(value), nil }

func xor(value []byte) []byte {
	result := append([]byte(nil), value...)
	for index := range result {
		result[index] ^= 0xA5
	}
	return result
}

func testService(t *testing.T) (*Service, *database.Store, *JoinTokens, *fakePeers) {
	t.Helper()
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "remlink.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store := database.NewStore(db)
	manager, err := ipam.New(store, netip.MustParsePrefix("10.88.0.0/16"), netip.MustParseAddr("10.88.0.1"))
	if err != nil {
		t.Fatal(err)
	}
	serverPrivate, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	peers := &fakePeers{entries: make(map[string]netip.Addr)}
	joins := NewJoinTokens(store)
	service, err := NewService(store, manager, joins, peers, ServiceConfig{
		ServerID: "23ac1928-2334-49bb-8b3f-272572d919da", Version: "test",
		WGPublicKey: serverPrivate.PublicKey().String(), WGEndpoint: "203.0.113.1:51820",
		OverlayCIDR: netip.MustParsePrefix("10.88.0.0/16"), ServerOverlayIP: netip.MustParseAddr("10.88.0.1"),
		ControlURL: "ws://10.88.0.1:7001/control", SessionUDPPort: 6200, MTU: 1280, ConfigVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service, store, joins, peers
}

func validRegisterRequest(t *testing.T, index int, joinToken string) RegisterRequest {
	t.Helper()
	privateKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	return RegisterRequest{
		JoinToken: joinToken, NodeID: fmt.Sprintf("00000000-0000-4000-8000-%012d", index),
		NodeType: model.NodeTypeEngineer, NodeName: fmt.Sprintf("Engineer-%d", index),
		WGPublicKey: privateKey.PublicKey().String(), Version: "test", OSVersion: "Windows test",
	}
}

func performJSON(t *testing.T, handler http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, bytes.NewReader(encoded))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recorder, request)
	return recorder
}
