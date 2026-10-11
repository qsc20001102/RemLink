package clientsettings

import (
	"bytes"
	"os"
	"path/filepath"
	"remlink/internal/model"
	"testing"
)

type testProtector struct{}

func (testProtector) Protect(raw []byte) ([]byte, error) {
	result := append([]byte{}, raw...)
	for i := range result {
		result[i] ^= 0x6a
	}
	return result, nil
}
func (testProtector) Unprotect(raw []byte) ([]byte, error) { return (testProtector{}).Protect(raw) }

func TestSettingsRoundTripAndSecretProtection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	store, _ := NewStore(path, model.NodeTypeEngineer, testProtector{})
	if _, configured, err := store.Load(); err != nil || configured {
		t.Fatal("fresh machine should not be configured")
	}
	value := Defaults(model.NodeTypeEngineer)
	value.ServerURL, value.NodeName, value.JoinToken = " https://example.test/ ", " 测试电脑 ", "private-registration-token"
	if err := store.Save(value); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if bytes.Contains(raw, []byte(value.JoinToken)) {
		t.Fatal("Join Token was saved as plaintext")
	}
	loaded, configured, err := store.Load()
	if err != nil || !configured || loaded.ServerURL != "https://example.test" || loaded.NodeName != "测试电脑" || loaded.JoinToken != value.JoinToken {
		t.Fatalf("settings round trip failed: configured=%v error=%v", configured, err)
	}
	value.NodeName = "改名后的电脑"
	if err := store.Save(value); err != nil {
		t.Fatal(err)
	}
	loaded, _, err = store.Load()
	if err != nil || loaded.NodeName != value.NodeName {
		t.Fatal("settings update was not persisted")
	}
	value.ServerURL = "http://user:password@example.test"
	if err := store.Save(value); err == nil {
		t.Fatal("unsafe URL was accepted")
	}
	loaded, _, _ = store.Load()
	if loaded.ServerURL != "https://example.test" {
		t.Fatal("invalid settings replaced valid saved settings")
	}
}
