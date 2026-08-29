package serverwg

import (
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestConfigValidation(t *testing.T) {
	valid := Config{
		InterfaceName: "wg0", Address: netip.MustParsePrefix("10.88.0.1/16"),
		ListenPort: 51820, PrivateKeyPath: filepath.Join(t.TempDir(), "server.key"),
	}
	if err := valid.validate(); err != nil {
		t.Fatal(err)
	}
	invalid := valid
	invalid.Address = netip.MustParsePrefix("10.88.0.0/16")
	if err := invalid.validate(); err == nil {
		t.Fatal("network address accepted as Server address")
	}
}

func TestLoadOrCreatePrivateKeyIsConcurrentAndAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys", "server.key")
	const workers = 8
	keys := make(chan string, workers)
	errorsSeen := make(chan error, workers)
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			key, err := LoadOrCreatePrivateKey(path)
			if err != nil {
				errorsSeen <- err
				return
			}
			keys <- key.String()
		}()
	}
	group.Wait()
	close(keys)
	close(errorsSeen)
	for err := range errorsSeen {
		t.Fatal(err)
	}
	want := ""
	for key := range keys {
		if want == "" {
			want = key
		}
		if key != want {
			t.Fatalf("concurrent creators observed different keys %s and %s", want, key)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != want+"\n" {
		t.Fatalf("published key file = %q, %v", raw, err)
	}
}

func TestLoadOrCreatePrivateKeyIsStable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys", "server.key")
	first, err := LoadOrCreatePrivateKey(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadOrCreatePrivateKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("Server private key changed on reload")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != first.String()+"\n" {
		t.Fatal("private key file has unexpected contents")
	}
}
