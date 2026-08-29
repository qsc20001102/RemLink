//go:build windows

package main

import (
	"encoding/json"
	"strings"
	"testing"

	"remlink/internal/nodeagent"
)

func TestGetStateSerializesEmptyCollectionsAsArrays(t *testing.T) {
	app := NewEngineerApp(nodeagent.Options{}, nil)

	// Simulate an empty NodeList payload, which previously replaced the
	// initialized slices with nil slices.
	app.state.Sites = nil
	app.state.Logs = nil
	app.state.Session.CIDRs = nil
	app.state.SiteCIDRs = nil

	encoded, err := json.Marshal(app.GetState())
	if err != nil {
		t.Fatalf("marshal Engineer state: %v", err)
	}
	jsonState := string(encoded)
	for _, field := range []string{`"sites":[]`, `"siteCIDRs":{}`, `"logs":[]`, `"cidrs":[]`} {
		if !strings.Contains(jsonState, field) {
			t.Fatalf("expected %s in state JSON, got %s", field, jsonState)
		}
	}
}
