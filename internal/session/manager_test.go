package session

import (
	"context"
	"net/netip"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"remlink/internal/database"
	"remlink/internal/model"
	"remlink/internal/protocol"
)

func TestManagerFullLifecycleAndCounters(t *testing.T) {
	manager, store, sender, engineer, site := newManagerTest(t, time.Second)
	ctx := context.Background()
	create := envelope(t, protocol.ControlCreateSession, "request-1", protocol.CreateSessionPayload{
		SiteNodeID: site.ID, TargetCIDRs: []string{"192.168.13.0/24", "172.20.0.0/16"},
	})
	if err := manager.HandleControl(ctx, engineer, create); err != nil {
		t.Fatal(err)
	}
	prepare := sender.last(t, protocol.ControlPrepareSession).payload.(protocol.PrepareSessionPayload)
	stored, err := store.GetSession(ctx, prepare.SessionID)
	if err != nil || stored.Status != model.SessionPreparingSite {
		t.Fatalf("persisted preparing Session = %+v, %v", stored, err)
	}

	if err := manager.HandleControl(ctx, engineer, create); err != nil {
		t.Fatal(err)
	}
	rejection := sender.lastForNode(t, protocol.ControlStopSession, engineer.ID)
	if rejection.requestID != "request-1" || rejection.payload.(protocol.StopSessionPayload).Reason != string(protocol.ErrorEngineerSessionExists) {
		t.Fatalf("second Session rejection = %+v", rejection)
	}

	result := protocol.PrepareResultPayload{
		SessionID: prepare.SessionID, OK: true, SubnetGatewayStatus: "netstack", TCPCapacity: 2048, UDPCapacity: 4096,
		RouteResults: []protocol.RouteResult{{CIDR: "192.168.13.0/24", Result: "DIRECT"}, {CIDR: "172.20.0.0/16", Result: "ROUTED"}},
	}
	if err := manager.HandleControl(ctx, site, envelope(t, protocol.ControlPrepareResult, "", result)); err != nil {
		t.Fatal(err)
	}
	configuration := sender.last(t, protocol.ControlSessionConfig)
	if configuration.nodeID != engineer.ID || configuration.requestID != "request-1" {
		t.Fatalf("SESSION_CONFIG routing = %+v", configuration)
	}
	if got := configuration.payload.(protocol.SessionConfigPayload); got.PeerOverlayIP != site.OverlayIP.String() || got.UDPPort != 51821 || len(got.CIDRs) != 2 {
		t.Fatalf("SESSION_CONFIG = %+v", got)
	}
	stored, _ = store.GetSession(ctx, prepare.SessionID)
	if stored.Status != model.SessionReady {
		t.Fatalf("status = %s, want READY", stored.Status)
	}

	if err := manager.HandleControl(ctx, engineer, envelope(t, protocol.ControlRoutesReady, "", protocol.RoutesReadyPayload{SessionID: prepare.SessionID})); err != nil {
		t.Fatal(err)
	}
	stored, _ = store.GetSession(ctx, prepare.SessionID)
	if stored.Status != model.SessionActive || stored.ActiveAt == nil || sender.count(protocol.ControlSessionActive) != 2 {
		t.Fatalf("Active Session = %+v, active notifications=%d", stored, sender.count(protocol.ControlSessionActive))
	}

	counters := model.SessionCounters{UploadBytes: 1234, DownloadBytes: 5678, UploadPackets: 12, DownloadPackets: 34}
	stats := envelope(t, protocol.ControlSessionStats, "", protocol.SessionStatsPayload{SessionID: prepare.SessionID, Counters: counters})
	if err := manager.HandleControl(ctx, engineer, stats); err != nil {
		t.Fatal(err)
	}
	stored, _ = store.GetSession(ctx, prepare.SessionID)
	if stored.Counters != counters {
		t.Fatalf("persisted counters = %+v", stored.Counters)
	}
	if err := manager.HandleControl(ctx, engineer, envelope(t, protocol.ControlSessionStats, "", protocol.SessionStatsPayload{
		SessionID: prepare.SessionID, Counters: model.SessionCounters{UploadBytes: 1},
	})); err != nil {
		t.Fatal(err)
	}
	stored, _ = store.GetSession(ctx, prepare.SessionID)
	if stored.Counters != counters {
		t.Fatalf("stale report moved counters backward: %+v", stored.Counters)
	}

	if err := manager.HandleControl(ctx, site, envelope(t, protocol.ControlStopSession, "", protocol.StopSessionPayload{
		SessionID: prepare.SessionID, Reason: "operator",
	})); err != nil {
		t.Fatal(err)
	}
	stored, _ = store.GetSession(ctx, prepare.SessionID)
	if stored.Status != model.SessionClosed || stored.ClosedAt == nil {
		t.Fatalf("closed Session = %+v", stored)
	}
}

func TestManagerDisconnectNodeClosesStaleRuntimeAndAllowsReconnect(t *testing.T) {
	manager, store, sender, engineer, site := newManagerTest(t, time.Second)
	ctx := context.Background()
	if err := manager.HandleControl(ctx, engineer, envelope(t, protocol.ControlCreateSession, "before-restart", protocol.CreateSessionPayload{
		SiteNodeID: site.ID, TargetCIDRs: []string{"192.168.13.0/24"},
	})); err != nil {
		t.Fatal(err)
	}
	prepare := sender.last(t, protocol.ControlPrepareSession).payload.(protocol.PrepareSessionPayload)
	if err := manager.HandleControl(ctx, site, envelope(t, protocol.ControlPrepareResult, "", protocol.PrepareResultPayload{
		SessionID: prepare.SessionID, OK: true, SubnetGatewayStatus: "netstack", TCPCapacity: 1, UDPCapacity: 1,
		RouteResults: []protocol.RouteResult{{CIDR: "192.168.13.0/24", Result: "DIRECT"}},
	})); err != nil {
		t.Fatal(err)
	}
	if err := manager.HandleControl(ctx, engineer, envelope(t, protocol.ControlRoutesReady, "", protocol.RoutesReadyPayload{SessionID: prepare.SessionID})); err != nil {
		t.Fatal(err)
	}
	if err := manager.DisconnectNode(ctx, engineer.ID, "NODE_RUNTIME_REBUILT"); err != nil {
		t.Fatal(err)
	}
	stored, err := store.GetSession(ctx, prepare.SessionID)
	if err != nil || stored.Status != model.SessionClosed || sender.count(protocol.ControlStopSession) != 2 {
		t.Fatalf("reconciled Session=%+v err=%v STOP count=%d", stored, err, sender.count(protocol.ControlStopSession))
	}
	if err := manager.HandleControl(ctx, engineer, envelope(t, protocol.ControlCreateSession, "after-restart", protocol.CreateSessionPayload{
		SiteNodeID: site.ID, TargetCIDRs: []string{"192.168.21.0/24"},
	})); err != nil {
		t.Fatalf("Node remained locked by stale Session after Bootstrap reconciliation: %v", err)
	}
	if next := sender.last(t, protocol.ControlPrepareSession).payload.(protocol.PrepareSessionPayload); next.SessionID == prepare.SessionID || sender.count(protocol.ControlPrepareSession) != 2 {
		t.Fatalf("new preparation was not accepted after reconciliation: old=%d new=%d count=%d", prepare.SessionID, next.SessionID, sender.count(protocol.ControlPrepareSession))
	}
}

func TestManagerClosesActiveSessionWhenSiteBecomesOffline(t *testing.T) {
	manager, store, sender, engineer, site := newManagerTest(t, time.Second)
	ctx := context.Background()
	if err := manager.HandleControl(ctx, engineer, envelope(t, protocol.ControlCreateSession, "site-offline", protocol.CreateSessionPayload{
		SiteNodeID: site.ID, TargetCIDRs: []string{"192.168.17.0/24"},
	})); err != nil {
		t.Fatal(err)
	}
	prepare := sender.last(t, protocol.ControlPrepareSession).payload.(protocol.PrepareSessionPayload)
	if err := manager.HandleControl(ctx, site, envelope(t, protocol.ControlPrepareResult, "", protocol.PrepareResultPayload{
		SessionID: prepare.SessionID, OK: true, SubnetGatewayStatus: "netstack", TCPCapacity: 1, UDPCapacity: 1,
		RouteResults: []protocol.RouteResult{{CIDR: "192.168.17.0/24", Result: "DIRECT"}},
	})); err != nil {
		t.Fatal(err)
	}
	if err := manager.HandleControl(ctx, engineer, envelope(t, protocol.ControlRoutesReady, "", protocol.RoutesReadyPayload{SessionID: prepare.SessionID})); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateNodeStatus(ctx, site.ID, model.NodeOffline); err != nil {
		t.Fatal(err)
	}
	site.Status = model.NodeOffline
	if err := manager.HandleNodeStatusChange(ctx, site, model.NodeOffline); err != nil {
		t.Fatal(err)
	}
	stored, err := store.GetSession(ctx, prepare.SessionID)
	if err != nil || stored.Status != model.SessionClosed || stored.ClosedAt == nil {
		t.Fatalf("offline Site Session = %+v, %v", stored, err)
	}
	stop := sender.lastForNode(t, protocol.ControlStopSession, engineer.ID).payload.(protocol.StopSessionPayload)
	if stop.SessionID != prepare.SessionID || stop.Reason != string(protocol.ErrorSiteOffline) {
		t.Fatalf("Engineer STOP_SESSION = %+v", stop)
	}
	if err := manager.HandleControl(ctx, engineer, envelope(t, protocol.ControlCreateSession, "after-offline", protocol.CreateSessionPayload{
		SiteNodeID: site.ID, TargetCIDRs: []string{"192.168.107.0/24"},
	})); err != nil {
		t.Fatal(err)
	}
	if got := sender.lastForNode(t, protocol.ControlStopSession, engineer.ID).payload.(protocol.StopSessionPayload).Reason; got != string(protocol.ErrorSiteOffline) {
		t.Fatalf("offline Site reconnect reason = %s", got)
	}
}

func TestManagerRejectsInvalidCIDRAndTimesOut(t *testing.T) {
	manager, store, sender, engineer, site := newManagerTest(t, 20*time.Millisecond)
	ctx := context.Background()
	invalid := envelope(t, protocol.ControlCreateSession, "bad", protocol.CreateSessionPayload{
		SiteNodeID: site.ID, TargetCIDRs: []string{"10.88.4.0/24"},
	})
	if err := manager.HandleControl(ctx, engineer, invalid); err != nil {
		t.Fatal(err)
	}
	if got := sender.last(t, protocol.ControlStopSession).payload.(protocol.StopSessionPayload).Reason; got != string(protocol.ErrorCIDROverlayConflict) {
		t.Fatalf("invalid CIDR reason = %s", got)
	}

	valid := envelope(t, protocol.ControlCreateSession, "timeout", protocol.CreateSessionPayload{
		SiteNodeID: site.ID, TargetCIDRs: []string{"192.168.13.0/24"},
	})
	if err := manager.HandleControl(ctx, engineer, valid); err != nil {
		t.Fatal(err)
	}
	prepare := sender.last(t, protocol.ControlPrepareSession).payload.(protocol.PrepareSessionPayload)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		stored, err := store.GetSession(ctx, prepare.SessionID)
		if err == nil && stored.Status == model.SessionFailed {
			if stored.ErrorCode != string(protocol.ErrorSessionTimeout) {
				t.Fatalf("timeout error code = %s", stored.ErrorCode)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("Session did not reach FAILED after prepare timeout")
}

func TestManagerRejectsDefaultOnlyPrepareResult(t *testing.T) {
	manager, store, sender, engineer, site := newManagerTest(t, time.Second)
	ctx := context.Background()
	if err := manager.HandleControl(ctx, engineer, envelope(t, protocol.ControlCreateSession, "default-only", protocol.CreateSessionPayload{
		SiteNodeID: site.ID, TargetCIDRs: []string{"192.168.13.0/24"},
	})); err != nil {
		t.Fatal(err)
	}
	prepare := sender.last(t, protocol.ControlPrepareSession).payload.(protocol.PrepareSessionPayload)
	if err := manager.HandleControl(ctx, site, envelope(t, protocol.ControlPrepareResult, "", protocol.PrepareResultPayload{
		SessionID: prepare.SessionID, OK: true, SubnetGatewayStatus: "netstack", TCPCapacity: 2048, UDPCapacity: 4096,
		RouteResults: []protocol.RouteResult{{CIDR: "192.168.13.0/24", Result: "DEFAULT_ONLY"}},
	})); err != nil {
		t.Fatal(err)
	}
	stored, err := store.GetSession(ctx, prepare.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.SessionFailed || stored.ErrorCode != string(protocol.ErrorSiteNoRoute) {
		t.Fatalf("Session = %+v, want FAILED/%s", stored, protocol.ErrorSiteNoRoute)
	}
	rejection := sender.lastForNode(t, protocol.ControlStopSession, engineer.ID)
	if rejection.nodeID != engineer.ID || rejection.requestID != "default-only" || rejection.payload.(protocol.StopSessionPayload).SessionID != prepare.SessionID {
		t.Fatalf("PREPARE rejection lost CREATE correlation: %+v", rejection)
	}
}

func TestManagerRunsConcurrentDuplicateCIDRSessions(t *testing.T) {
	manager, store, sender, engineerA, siteA := newManagerTest(t, time.Second)
	ctx := context.Background()
	createOnlineNode := func(id string, nodeType model.NodeType, address string) model.Node {
		node := model.Node{ID: id, Type: nodeType, Name: id, OverlayIP: netip.MustParseAddr(address), WGPublicKey: id + "-key", NodeTokenHash: []byte(id)}
		if err := store.CreateNode(ctx, node); err != nil {
			t.Fatal(err)
		}
		if err := store.UpdateNodeHeartbeat(ctx, id, model.NodeOnline, time.Now().UTC(), "1.0", "test"); err != nil {
			t.Fatal(err)
		}
		node, _ = store.GetNode(ctx, id)
		return node
	}
	engineerB := createOnlineNode("engineer-b", model.NodeTypeEngineer, "10.88.0.4")
	engineerC := createOnlineNode("engineer-c", model.NodeTypeEngineer, "10.88.0.5")
	siteB := createOnlineNode("site-b", model.NodeTypeSite, "10.88.0.6")

	activate := func(engineer, site model.Node, requestID string) uint64 {
		if err := manager.HandleControl(ctx, engineer, envelope(t, protocol.ControlCreateSession, requestID, protocol.CreateSessionPayload{
			SiteNodeID: site.ID, TargetCIDRs: []string{"192.168.13.0/24"},
		})); err != nil {
			t.Fatal(err)
		}
		prepare := sender.last(t, protocol.ControlPrepareSession).payload.(protocol.PrepareSessionPayload)
		if err := manager.HandleControl(ctx, site, envelope(t, protocol.ControlPrepareResult, "", protocol.PrepareResultPayload{
			SessionID: prepare.SessionID, OK: true, SubnetGatewayStatus: "netstack", TCPCapacity: 2048, UDPCapacity: 4096,
			RouteResults: []protocol.RouteResult{{CIDR: "192.168.13.0/24", Result: "DIRECT"}},
		})); err != nil {
			t.Fatal(err)
		}
		if err := manager.HandleControl(ctx, engineer, envelope(t, protocol.ControlRoutesReady, "", protocol.RoutesReadyPayload{SessionID: prepare.SessionID})); err != nil {
			t.Fatal(err)
		}
		return prepare.SessionID
	}
	idA := activate(engineerA, siteA, "a")
	idB := activate(engineerB, siteB, "b")
	idC := activate(engineerC, siteA, "c")
	if idA == idB || idA == idC || idB == idC {
		t.Fatalf("SessionIDs are not unique: %d %d %d", idA, idB, idC)
	}
	sessions, err := store.ListSessions(ctx)
	if err != nil || len(sessions) != 3 {
		t.Fatalf("Sessions = %+v, %v", sessions, err)
	}
	for _, current := range sessions {
		if current.Status != model.SessionActive || len(current.CIDRs) != 1 || current.CIDRs[0].String() != "192.168.13.0/24" {
			t.Fatalf("unexpected concurrent Session: %+v", current)
		}
	}
}

func TestManagerUsesMigratedNetworkForNewSessions(t *testing.T) {
	manager, store, sender, engineer, site := newManagerTest(t, time.Second)
	ctx := context.Background()
	newOverlay := netip.MustParsePrefix("10.99.0.0/24")
	if err := manager.ReconfigureNetwork(newOverlay, 1400, 6300); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateNodeOverlayIP(ctx, engineer.ID, netip.MustParseAddr("10.99.0.2")); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateNodeOverlayIP(ctx, site.ID, netip.MustParseAddr("10.99.0.3")); err != nil {
		t.Fatal(err)
	}
	engineer, _ = store.GetNode(ctx, engineer.ID)
	site, _ = store.GetNode(ctx, site.ID)
	if err := manager.HandleControl(ctx, engineer, envelope(t, protocol.ControlCreateSession, "new-overlay-conflict", protocol.CreateSessionPayload{
		SiteNodeID: site.ID, TargetCIDRs: []string{"10.99.0.0/28"},
	})); err != nil {
		t.Fatal(err)
	}
	if got := sender.lastForNode(t, protocol.ControlStopSession, engineer.ID).payload.(protocol.StopSessionPayload).Reason; got != string(protocol.ErrorCIDROverlayConflict) {
		t.Fatalf("migrated Overlay conflict reason = %s", got)
	}
	if err := manager.HandleControl(ctx, engineer, envelope(t, protocol.ControlCreateSession, "after-migration", protocol.CreateSessionPayload{
		SiteNodeID: site.ID, TargetCIDRs: []string{"10.88.0.0/24"},
	})); err != nil {
		t.Fatal(err)
	}
	prepare := sender.last(t, protocol.ControlPrepareSession).payload.(protocol.PrepareSessionPayload)
	if prepare.EngineerOverlayIP != "10.99.0.2" {
		t.Fatalf("PREPARE Engineer IP = %s", prepare.EngineerOverlayIP)
	}
	if err := manager.HandleControl(ctx, site, envelope(t, protocol.ControlPrepareResult, "", protocol.PrepareResultPayload{
		SessionID: prepare.SessionID, OK: true, SubnetGatewayStatus: "netstack", TCPCapacity: 2048, UDPCapacity: 4096,
		RouteResults: []protocol.RouteResult{{CIDR: "10.88.0.0/24", Result: "DIRECT"}},
	})); err != nil {
		t.Fatal(err)
	}
	configured := sender.last(t, protocol.ControlSessionConfig).payload.(protocol.SessionConfigPayload)
	if configured.MTU != 1400 || configured.UDPPort != 6300 {
		t.Fatalf("SESSION_CONFIG retained stale network values: %+v", configured)
	}
}

func TestManagerQuiescesSessionCreationDuringNetworkMigration(t *testing.T) {
	manager, _, sender, engineer, site := newManagerTest(t, time.Second)
	ctx := context.Background()
	if err := manager.BeginNetworkMigration(ctx, "NETWORK_CONFIG_CHANGED"); err != nil {
		t.Fatal(err)
	}
	create := protocol.CreateSessionPayload{SiteNodeID: site.ID, TargetCIDRs: []string{"192.168.13.0/24"}}
	if err := manager.HandleControl(ctx, engineer, envelope(t, protocol.ControlCreateSession, "during-migration", create)); err != nil {
		t.Fatal(err)
	}
	rejection := sender.lastForNode(t, protocol.ControlStopSession, engineer.ID)
	if rejection.requestID != "during-migration" || rejection.payload.(protocol.StopSessionPayload).Reason != string(protocol.ErrorServerUnreachable) {
		t.Fatalf("migration rejection = %+v", rejection)
	}
	manager.EndNetworkMigration()
	if err := manager.HandleControl(ctx, engineer, envelope(t, protocol.ControlCreateSession, "after-migration", create)); err != nil {
		t.Fatal(err)
	}
	if sender.last(t, protocol.ControlPrepareSession).payload.(protocol.PrepareSessionPayload).SessionID == 0 {
		t.Fatal("Session creation remained quiesced after migration")
	}
}

type sentMessage struct {
	nodeID, requestID string
	messageType       protocol.ControlMessageType
	payload           any
}

type fakeSender struct {
	mu       sync.Mutex
	messages []sentMessage
}

func (f *fakeSender) Send(_ context.Context, nodeID string, messageType protocol.ControlMessageType, payload any) error {
	return f.SendRequest(context.Background(), nodeID, messageType, "", payload)
}

func (f *fakeSender) SendRequest(_ context.Context, nodeID string, messageType protocol.ControlMessageType, requestID string, payload any) error {
	f.mu.Lock()
	f.messages = append(f.messages, sentMessage{nodeID: nodeID, requestID: requestID, messageType: messageType, payload: payload})
	f.mu.Unlock()
	return nil
}

func (f *fakeSender) last(t *testing.T, messageType protocol.ControlMessageType) sentMessage {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for index := len(f.messages) - 1; index >= 0; index-- {
		if f.messages[index].messageType == messageType {
			return f.messages[index]
		}
	}
	t.Fatalf("no %s message", messageType)
	return sentMessage{}
}

func (f *fakeSender) lastForNode(t *testing.T, messageType protocol.ControlMessageType, nodeID string) sentMessage {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for index := len(f.messages) - 1; index >= 0; index-- {
		if f.messages[index].messageType == messageType && f.messages[index].nodeID == nodeID {
			return f.messages[index]
		}
	}
	t.Fatalf("no %s message for Node %s", messageType, nodeID)
	return sentMessage{}
}

func (f *fakeSender) count(messageType protocol.ControlMessageType) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, message := range f.messages {
		if message.messageType == messageType {
			count++
		}
	}
	return count
}

func newManagerTest(t *testing.T, timeout time.Duration) (*Manager, *database.Store, *fakeSender, model.Node, model.Node) {
	t.Helper()
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store := database.NewStore(db)
	engineer := model.Node{ID: "engineer", Type: model.NodeTypeEngineer, Name: "Engineer", OverlayIP: netip.MustParseAddr("10.88.0.2"), WGPublicKey: "engineer-key", NodeTokenHash: []byte("a")}
	site := model.Node{ID: "site", Type: model.NodeTypeSite, Name: "Site", OverlayIP: netip.MustParseAddr("10.88.0.3"), WGPublicKey: "site-key", NodeTokenHash: []byte("b")}
	for _, node := range []model.Node{engineer, site} {
		if err := store.CreateNode(context.Background(), node); err != nil {
			t.Fatal(err)
		}
		if err := store.UpdateNodeHeartbeat(context.Background(), node.ID, model.NodeOnline, time.Now().UTC(), "1.0", "test"); err != nil {
			t.Fatal(err)
		}
	}
	engineer, _ = store.GetNode(context.Background(), engineer.ID)
	site, _ = store.GetNode(context.Background(), site.ID)
	sender := &fakeSender{}
	manager, err := NewManager(store, sender, Config{
		OverlayCIDR: netip.MustParsePrefix("10.88.0.0/16"), MTU: 1280, UDPPort: 51821, PrepareTimeout: timeout,
	})
	if err != nil {
		t.Fatal(err)
	}
	return manager, store, sender, engineer, site
}

func envelope(t *testing.T, messageType protocol.ControlMessageType, requestID string, payload any) protocol.ControlEnvelope {
	t.Helper()
	envelope, err := protocol.NewControlEnvelope(messageType, requestID, payload)
	if err != nil {
		t.Fatal(err)
	}
	return envelope
}
