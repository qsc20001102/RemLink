package siteprofile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreKeepsIndependentCIDRsPerSite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "site-profiles.json")
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := store.Load()
	if err != nil || len(profiles) != 0 {
		t.Fatalf("initial profiles = %+v, %v", profiles, err)
	}
	if err := store.Save("site-a", "现场 A", []string{"192.168.17.0/24"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("site-b", "现场 B", []string{"192.168.107.0/24"}); err != nil {
		t.Fatal(err)
	}
	profiles, err = store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 2 || profiles["site-a"].CIDRs[0] != "192.168.17.0/24" || profiles["site-b"].CIDRs[0] != "192.168.107.0/24" {
		t.Fatalf("saved profiles = %+v", profiles)
	}
	if err := store.Save("site-a", "现场 A", nil); err != nil {
		t.Fatal(err)
	}
	profiles, _ = store.Load()
	if len(profiles) != 1 || profiles["site-b"].CIDRs[0] != "192.168.107.0/24" {
		t.Fatalf("profiles after clearing Site A = %+v", profiles)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestStoreRejectsInvalidOrCorruptProfiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "site-profiles.json")
	store, _ := NewStore(path)
	for _, invalid := range [][]string{{"192.168.17.1/24"}, {"0.0.0.0/0"}, {"192.168.17.0/24", "192.168.17.0/24"}} {
		if err := store.Save("site", "现场", invalid); err == nil {
			t.Fatalf("accepted invalid CIDRs: %v", invalid)
		}
	}
	if err := os.WriteFile(path, []byte(`{"version":1,"sites":{},"unknown":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); err == nil {
		t.Fatal("accepted unknown Site profile field")
	}
}
