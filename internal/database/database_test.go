package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/netip"
	"path/filepath"
	"testing"
	"time"

	"remlink/internal/model"
)

func TestEmptyListRepositoriesEncodeAsArrays(t *testing.T) {
	db := openTestDB(t)
	store := NewStore(db)
	ctx := context.Background()

	nodes, err := store.ListNodes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := store.ListSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	events, err := store.ListEvents(ctx, model.EventLogFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]any{"nodes": nodes, "sessions": sessions, "events": events} {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if string(encoded) != "[]" {
			t.Errorf("empty %s encoded as %s, want []", name, encoded)
		}
	}
}

func TestOpenMigratesAndReopens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "remlink.db")
	ctx := context.Background()
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}

	wantTables := []string{"settings", "nodes", "sessions", "session_cidrs", "session_stats", "event_logs"}
	for _, table := range wantTables {
		var count int
		if err := db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("table %s count = %d, want 1", table, count)
		}
	}
	var migrationCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&migrationCount); err != nil {
		t.Fatal(err)
	}
	if migrationCount != 1 {
		t.Fatalf("migration count = %d, want 1", migrationCount)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&migrationCount); err != nil {
		t.Fatal(err)
	}
	if migrationCount != 1 {
		t.Fatalf("migration count after reopen = %d, want 1", migrationCount)
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if value, found, err := GetSetting(ctx, db, "overlay_cidr"); err != nil || found || value != "" {
		t.Fatalf("missing GetSetting = %q, %v, %v", value, found, err)
	}
	if err := SetSetting(ctx, db, "overlay_cidr", "10.88.0.0/16"); err != nil {
		t.Fatal(err)
	}
	if err := SetSetting(ctx, db, "overlay_cidr", "10.99.0.0/16"); err != nil {
		t.Fatal(err)
	}
	value, found, err := GetSetting(ctx, db, "overlay_cidr")
	if err != nil || !found || value != "10.99.0.0/16" {
		t.Fatalf("GetSetting = %q, %v, %v", value, found, err)
	}
}

func TestNodeRepository(t *testing.T) {
	db := openTestDB(t)
	store := NewStore(db)
	ctx := context.Background()
	node := model.Node{
		ID:            "96e5d037-2358-4ff6-b706-1fbf56d196bc",
		Type:          model.NodeTypeEngineer,
		Name:          "Engineer-A",
		OverlayIP:     netip.MustParseAddr("10.88.0.2"),
		WGPublicKey:   "public-key-a",
		NodeTokenHash: []byte("token-hash-a"),
	}
	if err := store.CreateNode(ctx, node); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetNode(ctx, node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != node.ID || got.OverlayIP != node.OverlayIP || got.Status != model.NodeOffline {
		t.Fatalf("unexpected node: %+v", got)
	}

	node.Name = "Engineer-Renamed"
	node.NodeTokenHash = []byte("token-hash-b")
	node.Version = "1.0.0"
	if err := store.UpdateNodeRegistration(ctx, node); err != nil {
		t.Fatal(err)
	}
	got, err = store.GetNode(ctx, node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != node.Name || got.Version != "1.0.0" || string(got.NodeTokenHash) != "token-hash-b" {
		t.Fatalf("registration update not persisted: %+v", got)
	}

	if err := store.UpdateNodeOverlayIP(ctx, node.ID, netip.MustParseAddr("10.88.0.50")); err != nil {
		t.Fatal(err)
	}
	addresses, err := store.ListOverlayIPs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(addresses) != 1 || addresses[0].String() != "10.88.0.50" {
		t.Fatalf("overlay addresses = %v", addresses)
	}
	if err := store.DeleteNode(ctx, node.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetNode(ctx, node.ID); !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("GetNode after delete error = %v", err)
	}
}

func TestOverlayMigrationCommitsNodeIPsAndNetworkSettingAtomically(t *testing.T) {
	db := openTestDB(t)
	store := NewStore(db)
	ctx := context.Background()
	node := model.Node{
		ID: "engineer", Type: model.NodeTypeEngineer, Name: "Engineer",
		OverlayIP: netip.MustParseAddr("10.88.0.2"), WGPublicKey: "engineer-key", NodeTokenHash: []byte("hash"),
	}
	if err := store.CreateNode(ctx, node); err != nil {
		t.Fatal(err)
	}
	if err := SetSetting(ctx, db, "admin.network", "old"); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceNodeOverlayIPsAndSetting(ctx,
		map[string]netip.Addr{"engineer": netip.MustParseAddr("10.99.0.2")}, "admin.network", "new"); err != nil {
		t.Fatal(err)
	}
	migrated, _ := store.GetNode(ctx, "engineer")
	setting, found, err := GetSetting(ctx, db, "admin.network")
	if err != nil || !found || migrated.OverlayIP.String() != "10.99.0.2" || setting != "new" {
		t.Fatalf("committed migration node=%s setting=%q found=%v error=%v", migrated.OverlayIP, setting, found, err)
	}

	err = store.ReplaceNodeOverlayIPsAndSetting(ctx, map[string]netip.Addr{
		"engineer": netip.MustParseAddr("10.77.0.2"),
		"missing":  netip.MustParseAddr("10.77.0.3"),
	}, "admin.network", "partial")
	if err == nil {
		t.Fatal("migration with a missing Node unexpectedly succeeded")
	}
	afterFailure, _ := store.GetNode(ctx, "engineer")
	setting, _, _ = GetSetting(ctx, db, "admin.network")
	if afterFailure.OverlayIP.String() != "10.99.0.2" || setting != "new" {
		t.Fatalf("failed migration was not atomic: node=%s setting=%q", afterFailure.OverlayIP, setting)
	}
}

func TestForeignKeysEnabled(t *testing.T) {
	db := openTestDB(t)
	_, err := db.Exec(`INSERT INTO sessions(session_id, engineer_node_id, site_node_id, status, created_at)
		VALUES('1', 'missing-a', 'missing-b', 'CREATING', '2026-08-25T00:00:00Z')`)
	if err == nil {
		t.Fatal("session with missing node references was accepted")
	}
}

func TestEventRepositoryFiltersByTimeAndNormalizesModule(t *testing.T) {
	store := NewStore(openTestDB(t))
	ctx := context.Background()
	base := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	for index, eventTime := range []time.Time{base.Add(-time.Hour), base, base.Add(time.Hour)} {
		if err := store.AppendEvent(ctx, model.EventLog{Time: eventTime, Level: "info", Module: "session", Message: fmt.Sprintf("event-%d", index)}); err != nil {
			t.Fatal(err)
		}
	}
	events, err := store.ListEvents(ctx, model.EventLogFilter{Module: "SESSION", From: base.Add(-time.Minute), To: base.Add(time.Minute)})
	if err != nil || len(events) != 1 || events[0].Message != "event-1" || events[0].Module != "SESSION" {
		t.Fatalf("events = %+v, %v", events, err)
	}
	if err := store.AppendEvent(ctx, model.EventLog{Level: "INFO", Module: "CUSTOM", Message: "outside taxonomy"}); err == nil {
		t.Fatal("AppendEvent accepted a twelfth logging module")
	}
}

func TestSessionRepositoryLifecycleAndStartupClose(t *testing.T) {
	db := openTestDB(t)
	store := NewStore(db)
	ctx := context.Background()
	createSessionTestNodes(t, store)
	session := model.Session{
		ID: 18446744073709551614, EngineerNodeID: "engineer", SiteNodeID: "site",
		Status: model.SessionCreating, CIDRs: []netip.Prefix{
			netip.MustParsePrefix("192.168.13.0/24"), netip.MustParsePrefix("172.20.0.0/16"),
		},
	}
	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateSessionStatus(ctx, session.ID, model.SessionActive, ""); err != nil {
		t.Fatal(err)
	}
	counters := model.SessionCounters{UploadBytes: 100, DownloadBytes: 200, UploadPackets: 3, DownloadPackets: 4}
	if err := store.UpdateSessionStats(ctx, session.ID, counters); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.SessionActive || got.ActiveAt == nil || got.Counters != counters || len(got.CIDRs) != 2 {
		t.Fatalf("unexpected persisted Session: %+v", got)
	}
	if err := store.UpdateSessionStats(ctx, session.ID, model.SessionCounters{UploadBytes: math.MaxUint64}); err == nil {
		t.Fatal("SQLite-unsafe counter was accepted")
	}
	closed, err := store.CloseOpenSessions(ctx)
	if err != nil || closed != 1 {
		t.Fatalf("CloseOpenSessions = %d, %v", closed, err)
	}
	got, err = store.GetSession(ctx, session.ID)
	if err != nil || got.Status != model.SessionClosed || got.ClosedAt == nil {
		t.Fatalf("Session after startup close = %+v, %v", got, err)
	}
}

func createSessionTestNodes(t *testing.T, store *Store) {
	t.Helper()
	for _, node := range []model.Node{
		{ID: "engineer", Type: model.NodeTypeEngineer, Name: "Engineer", OverlayIP: netip.MustParseAddr("10.88.0.2"), WGPublicKey: "engineer-key", NodeTokenHash: []byte("a")},
		{ID: "site", Type: model.NodeTypeSite, Name: "Site", OverlayIP: netip.MustParseAddr("10.88.0.3"), WGPublicKey: "site-key", NodeTokenHash: []byte("b")},
	} {
		if err := store.CreateNode(context.Background(), node); err != nil {
			t.Fatal(err)
		}
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
