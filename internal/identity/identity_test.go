package identity

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"remlink/internal/model"
)

type testProtector struct{}

func (testProtector) Protect(value []byte) ([]byte, error) {
	result := append([]byte("protected:"), value...)
	for left, right := 0, len(result)-1; left < right; left, right = left+1, right-1 {
		result[left], result[right] = result[right], result[left]
	}
	return result, nil
}

func (testProtector) Unprotect(value []byte) ([]byte, error) {
	result := append([]byte(nil), value...)
	for left, right := 0, len(result)-1; left < right; left, right = left+1, right-1 {
		result[left], result[right] = result[right], result[left]
	}
	return bytes.TrimPrefix(result, []byte("protected:")), nil
}

func TestIdentityRoundTripWithoutPlaintextSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Engineer", "identity.json")
	store, err := NewStore(path, testProtector{})
	if err != nil {
		t.Fatal(err)
	}
	value, err := New(model.NodeTypeEngineer, "Engineer-A", "http://203.0.113.1:8080/")
	if err != nil {
		t.Fatal(err)
	}
	value.NodeToken = "plain-node-token-that-must-not-appear"
	value.ConfigVersion = 7
	value.OwnedRoutes = []string{"192.168.13.0/24"}
	if err := store.Save(value); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(value.NodeToken)) || bytes.Contains(raw, []byte(value.PrivateKey.String())) {
		t.Fatalf("identity file contains a plaintext secret: %s", raw)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.NodeID != value.NodeID || loaded.PrivateKey != value.PrivateKey || loaded.NodeToken != value.NodeToken ||
		loaded.ConfigVersion != 7 || len(loaded.OwnedRoutes) != 1 {
		t.Fatalf("loaded identity differs: %+v", loaded)
	}

	value.NodeName = "Engineer-Renamed"
	if err := store.Save(value); err != nil {
		t.Fatalf("replace identity: %v", err)
	}
	loaded, err = store.Load()
	if err != nil || loaded.NodeName != value.NodeName {
		t.Fatalf("replaced identity = %+v, %v", loaded, err)
	}
}

func TestIdentityValidation(t *testing.T) {
	if _, err := New(model.NodeType("server"), "x", "http://example.test"); err == nil {
		t.Fatal("invalid Node type accepted")
	}
	if _, err := New(model.NodeTypeSite, "x", "not-a-url"); err == nil {
		t.Fatal("invalid Server URL accepted")
	}
	for _, raw := range []string{"http://user:secret@example.test", "http://example.test/api", "http://example.test?redirect=elsewhere"} {
		if _, err := New(model.NodeTypeSite, "x", raw); err == nil {
			t.Errorf("unsafe Server URL %q accepted", raw)
		}
	}
}
