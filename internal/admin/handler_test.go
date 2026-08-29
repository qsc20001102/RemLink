package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"testing"
	"time"

	"remlink/internal/bootstrap"
	"remlink/internal/database"
	"remlink/internal/ipam"
	"remlink/internal/model"
)

type fakeJoinTokenRotator struct{ count int }

func (f *fakeJoinTokenRotator) Rotate(context.Context) (string, error) {
	f.count++
	return "rotated-token", nil
}

func TestAdminHandlerAuthNodeUpdateLogsAndRotateOnly(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "admin.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := database.NewStore(db)
	if err := store.CreateNode(ctx, model.Node{
		ID: "engineer", Type: model.NodeTypeEngineer, Name: "Engineer",
		OverlayIP: netip.MustParseAddr("10.88.0.2"), WGPublicKey: "wg-key", NodeTokenHash: []byte("hash"),
	}); err != nil {
		t.Fatal(err)
	}
	ipamManager, err := ipam.New(store, netip.MustParsePrefix("10.88.0.0/24"), netip.MustParseAddr("10.88.0.1"))
	if err != nil {
		t.Fatal(err)
	}
	peers := &fakeAdminPeers{}
	bootstrapNetwork := &fakeBootstrapNetwork{config: bootstrap.ServiceConfig{
		WGEndpoint: "203.0.113.4:51820", ControlURL: "ws://10.88.0.1:7001/control",
		OverlayCIDR: netip.MustParsePrefix("10.88.0.0/24"), ServerOverlayIP: netip.MustParseAddr("10.88.0.1"),
		SessionUDPPort: 6200, MTU: 1280, ConfigVersion: 1,
	}}
	controlNetwork := &fakeAdminControl{}
	sessions := &fakeSessionControl{}
	network, err := NewNetworkManager(store, ipamManager, peers, bootstrapNetwork, controlNetwork, sessions, Network{
		OverlayCIDR: "10.88.0.0/24", ServerOverlayIP: "10.88.0.1", WireGuardPort: 51820,
		SessionUDPPort: 6200, MTU: 1280, ConfigVersion: 1,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rotator := &fakeJoinTokenRotator{}
	handler, err := Handler(HandlerConfig{
		Store: store, IPAM: ipamManager, Peers: peers, Control: controlNetwork,
		Sessions: sessions, Network: network, JoinTokens: rotator, AdminToken: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/v1/admin/nodes", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}
	nodesResponse := adminRequest(t, handler, http.MethodGet, "/api/v1/admin/nodes", nil)
	if nodesResponse.Code != http.StatusOK || !bytes.Contains(nodesResponse.Body.Bytes(), []byte(`"wg_handshake"`)) {
		t.Fatalf("nodes response status=%d body=%s", nodesResponse.Code, nodesResponse.Body.String())
	}

	patch := adminRequest(t, handler, http.MethodPatch, "/api/v1/admin/nodes/engineer", map[string]any{"name": "Field Engineer"})
	if patch.Code != http.StatusOK || bytes.Contains(patch.Body.Bytes(), []byte("node_token")) {
		t.Fatalf("PATCH response status=%d body=%s", patch.Code, patch.Body.String())
	}
	updated, err := store.GetNode(ctx, "engineer")
	if err != nil || updated.Name != "Field Engineer" {
		t.Fatalf("updated node = %+v, %v", updated, err)
	}
	invalidCombined := adminRequest(t, handler, http.MethodPatch, "/api/v1/admin/nodes/engineer", map[string]any{
		"name": "Must Not Persist", "overlay_ip": "not-an-ip",
	})
	unchanged, _ := store.GetNode(ctx, "engineer")
	if invalidCombined.Code != http.StatusBadRequest || unchanged.Name != "Field Engineer" {
		t.Fatalf("invalid combined PATCH status=%d node=%+v", invalidCombined.Code, unchanged)
	}

	steps := make([]string, 0, 2)
	peers.steps = &steps
	controlNetwork.steps = &steps
	addressPatch := adminRequest(t, handler, http.MethodPatch, "/api/v1/admin/nodes/engineer", map[string]any{
		"name": "Moved Engineer", "overlay_ip": "10.88.0.9",
	})
	moved, moveErr := store.GetNode(ctx, "engineer")
	if addressPatch.Code != http.StatusOK || moveErr != nil || moved.Name != "Moved Engineer" || moved.OverlayIP.String() != "10.88.0.9" {
		t.Fatalf("address PATCH status=%d node=%+v error=%v body=%s", addressPatch.Code, moved, moveErr, addressPatch.Body.String())
	}
	wantSteps := []string{"notify", "peers"}
	if len(steps) != len(wantSteps) || steps[0] != wantSteps[0] || steps[1] != wantSteps[1] {
		t.Fatalf("Node address change steps=%v, want=%v", steps, wantSteps)
	}
	if peers.ensured != moved.OverlayIP || !controlNetwork.resetNode || sessions.nodeDisconnects != 1 || sessions.nodeReason != "NODE_OVERLAY_IP_CHANGED" {
		t.Fatalf("Node address orchestration peer=%s reset=%v disconnects=%d reason=%s", peers.ensured, controlNetwork.resetNode, sessions.nodeDisconnects, sessions.nodeReason)
	}
	peers.steps = nil
	controlNetwork.steps = nil

	bad := adminRequest(t, handler, http.MethodPatch, "/api/v1/admin/nodes/engineer", map[string]any{"unknown": true})
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("unknown JSON field status = %d, want 400", bad.Code)
	}

	rotate := adminRequest(t, handler, http.MethodPut, "/api/v1/admin/network", map[string]any{
		"overlay_cidr": "10.88.0.0/24", "server_overlay_ip": "10.88.0.1",
		"wireguard_port": 51820, "session_udp_port": 6200, "mtu": 1280, "rotate_join_token": true,
	})
	if rotate.Code != http.StatusOK || rotator.count != 1 || sessions.all || controlNetwork.resetAll {
		t.Fatalf("rotate-only response=%s count=%d migrated=%v reset=%v", rotate.Body.String(), rotator.count, sessions.all, controlNetwork.resetAll)
	}
	var rotateBody map[string]any
	_ = json.Unmarshal(rotate.Body.Bytes(), &rotateBody)
	if rotateBody["join_token"] != "rotated-token" || rotateBody["config_version"] != float64(1) {
		t.Fatalf("rotate response = %v", rotateBody)
	}

	from := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	to := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	logs := adminRequest(t, handler, http.MethodGet, "/api/v1/admin/logs?module=CORE&limit=10&from="+from+"&to="+to, nil)
	if logs.Code != http.StatusOK {
		t.Fatalf("logs status=%d body=%s", logs.Code, logs.Body.String())
	}
	var events []model.EventLog
	if err := json.Unmarshal(logs.Body.Bytes(), &events); err != nil || len(events) < 2 {
		t.Fatalf("Admin events = %+v, %v", events, err)
	}
	badTime := adminRequest(t, handler, http.MethodGet, "/api/v1/admin/logs?from=not-a-time", nil)
	if badTime.Code != http.StatusBadRequest {
		t.Fatalf("invalid log time status = %d", badTime.Code)
	}

	view := adminRequest(t, handler, http.MethodGet, "/api/v1/admin/network", nil)
	var networkView map[string]any
	_ = json.Unmarshal(view.Body.Bytes(), &networkView)
	if _, exists := networkView["uptime_seconds"]; !exists {
		t.Fatalf("network response has no uptime_seconds: %s", view.Body.String())
	}

	deleted := adminRequest(t, handler, http.MethodDelete, "/api/v1/admin/nodes/engineer", nil)
	if deleted.Code != http.StatusNoContent || peers.removed != 1 || sessions.nodeDisconnects != 2 || controlNetwork.resetNodeCount != 2 || controlNetwork.resetReason != "Node revoked" {
		t.Fatalf("DELETE status=%d peer removals=%d disconnects=%d resets=%d reason=%s",
			deleted.Code, peers.removed, sessions.nodeDisconnects, controlNetwork.resetNodeCount, controlNetwork.resetReason)
	}
	if _, err := store.GetNode(ctx, "engineer"); err == nil {
		t.Fatal("deleted Node remains in the registry")
	}
}

func adminRequest(t *testing.T, handler http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	if body != nil {
		var err error
		raw, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(raw))
	request.Header.Set("Authorization", "Bearer secret")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}
