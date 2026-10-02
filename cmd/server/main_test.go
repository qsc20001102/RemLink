package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"remlink/internal/database"
	"remlink/internal/model"
)

func TestHealthcheckUsesConfiguredListenerWithoutOpeningDatabase(t *testing.T) {
	var status atomic.Int32
	status.Store(http.StatusOK)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/server/info" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(int(status.Load()))
	}))
	defer server.Close()
	root := t.TempDir()
	data := filepath.Join(root, "not-created")
	path := filepath.Join(root, "server.yaml")
	content := fmt.Sprintf("server:\n  http_listen: %q\ndata:\n  directory: %q\n", strings.TrimPrefix(server.URL, "http://"), data)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"-config", path, "-healthcheck"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(data); !os.IsNotExist(err) {
		t.Fatalf("healthcheck touched data: %v", err)
	}
	status.Store(http.StatusServiceUnavailable)
	if err := run([]string{"-config", path, "-healthcheck"}); err == nil {
		t.Fatal("unhealthy listener accepted")
	}
}

func TestValidateEndpoint(t *testing.T) {
	for _, endpoint := range []string{"203.0.113.1:51820", "vpn.example.test:51820"} {
		if err := validateEndpoint(endpoint, 51820); err != nil {
			t.Errorf("validateEndpoint(%q): %v", endpoint, err)
		}
	}
	for _, endpoint := range []string{"", "203.0.113.1", ":51820", "203.0.113.1:1234"} {
		if err := validateEndpoint(endpoint, 51820); err == nil {
			t.Errorf("validateEndpoint(%q) accepted invalid endpoint", endpoint)
		}
	}
}

func TestPrintJoinTokenDoesNotCloseActiveSessions(t *testing.T) {
	t.Parallel()
	dataDirectory := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dataDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "server.yaml")
	configText := fmt.Sprintf("data:\n  directory: %q\n", dataDirectory)
	if err := os.WriteFile(configPath, []byte(configText), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(dataDirectory, "remlink.db"))
	if err != nil {
		t.Fatal(err)
	}
	store := database.NewStore(db)
	for _, node := range []model.Node{
		{ID: "engineer", Type: model.NodeTypeEngineer, Name: "Engineer", OverlayIP: netip.MustParseAddr("10.88.0.2"), WGPublicKey: "engineer-key", NodeTokenHash: []byte("a")},
		{ID: "site", Type: model.NodeTypeSite, Name: "Site", OverlayIP: netip.MustParseAddr("10.88.0.3"), WGPublicKey: "site-key", NodeTokenHash: []byte("b")},
	} {
		if err := store.CreateNode(ctx, node); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	const sessionID = 42
	if err := store.CreateSession(ctx, model.Session{
		ID: sessionID, EngineerNodeID: "engineer", SiteNodeID: "site",
		Status: model.SessionActive, CIDRs: []netip.Prefix{netip.MustParsePrefix("192.168.13.0/24")},
	}); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if err := run([]string{"-config", configPath, "-print-join-token"}); err != nil {
		t.Fatal(err)
	}

	db, err = database.Open(ctx, filepath.Join(dataDirectory, "remlink.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	session, err := database.NewStore(db).GetSession(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if session.Status != model.SessionActive {
		t.Fatalf("print Join Token changed active Session status to %s", session.Status)
	}
}
