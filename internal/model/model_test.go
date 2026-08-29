package model

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestNodeTypesAreCompleteAndValid(t *testing.T) {
	t.Parallel()
	if got, want := len(NodeTypes), 2; got != want {
		t.Fatalf("NodeTypes length = %d, want %d", got, want)
	}
	for _, nodeType := range NodeTypes {
		if !nodeType.Valid() {
			t.Fatalf("listed node type %q is invalid", nodeType)
		}
	}
	if _, err := ParseNodeType("server"); err == nil {
		t.Fatal("ParseNodeType accepted the Server, which is not a Windows Node role")
	}
}

func TestNodeStatusesAreCompleteAndValid(t *testing.T) {
	t.Parallel()
	for _, status := range []NodeStatus{NodeOnline, NodeUnstable, NodeOffline} {
		if !status.Valid() {
			t.Fatalf("listed Node status %q is invalid", status)
		}
	}
	if NodeStatus("UNKNOWN").Valid() {
		t.Fatal("unknown Node status accepted")
	}
}

func TestSessionStatusesAreCompleteAndValid(t *testing.T) {
	t.Parallel()
	if got, want := len(SessionStatuses), 7; got != want {
		t.Fatalf("SessionStatuses length = %d, want %d", got, want)
	}
	seen := make(map[SessionStatus]struct{}, len(SessionStatuses))
	for _, status := range SessionStatuses {
		if !status.Valid() {
			t.Fatalf("listed Session status %q is invalid", status)
		}
		if _, duplicate := seen[status]; duplicate {
			t.Fatalf("duplicate Session status %q", status)
		}
		seen[status] = struct{}{}
	}
	if _, err := ParseSessionStatus("RUNNING"); err == nil {
		t.Fatal("ParseSessionStatus accepted a non-spec state")
	}
}

func TestSessionIDsUseLosslessJSONDecimalStrings(t *testing.T) {
	t.Parallel()
	session := Session{ID: math.MaxUint64, Status: SessionActive}
	raw, err := json.Marshal(session)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"session_id":"18446744073709551615"`) {
		t.Fatalf("SessionID was not encoded losslessly for JavaScript clients: %s", raw)
	}
	var decoded Session
	if err := json.Unmarshal(raw, &decoded); err != nil || decoded.ID != session.ID {
		t.Fatalf("SessionID round trip = %d, %v", decoded.ID, err)
	}
	eventRaw, err := json.Marshal(EventLog{SessionID: math.MaxUint64})
	if err != nil || !strings.Contains(string(eventRaw), `"session_id":"18446744073709551615"`) {
		t.Fatalf("Event SessionID was not encoded losslessly: %s, %v", eventRaw, err)
	}
}
