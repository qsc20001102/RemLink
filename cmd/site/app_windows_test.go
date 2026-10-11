//go:build windows

package main

import (
	"encoding/json"
	"path/filepath"
	"remlink/internal/clientsettings"
	"remlink/internal/identity"
	"remlink/internal/model"
	"remlink/internal/nodeagent"
	"remlink/internal/platform/windows/dpapi"
	"strings"
	"testing"
)

func TestFreshSiteStateHasNoConnectionAndSerializesListsAsArrays(t *testing.T) {
	root := t.TempDir()
	settings, _ := clientsettings.NewStore(filepath.Join(root, "settings.json"), model.NodeTypeSite, dpapi.Protector{})
	identityStore, _ := identity.NewStore(filepath.Join(root, "identity.json"), dpapi.Protector{})
	app := NewSiteApp(nodeagent.Options{NodeType: model.NodeTypeSite}, settings, identityStore)
	state := app.GetState()
	if state.Configured || state.ServerConnected || state.ControlConnected {
		t.Fatal("new Site started connected")
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{`"sessions":[]`, `"logs":[]`, `"configured":false`} {
		if !strings.Contains(string(raw), value) {
			t.Fatalf("Site state is missing %s", value)
		}
	}
}
