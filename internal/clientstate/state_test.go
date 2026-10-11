package clientstate

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"remlink/internal/config"
	"remlink/internal/identity"
	"remlink/internal/model"
	"remlink/internal/siteprofile"
)

var errForeignMachine = errors.New("identity belongs to another machine")

type testProtector struct{ foreign bool }

func (p testProtector) Protect(raw []byte) ([]byte, error) {
	return append([]byte("test-protected:"), raw...), nil
}

func (p testProtector) Unprotect(raw []byte) ([]byte, error) {
	if p.foreign {
		return nil, errForeignMachine
	}
	return bytes.TrimPrefix(raw, []byte("test-protected:")), nil
}

func foreignMachine(err error) bool { return errors.Is(err, errForeignMachine) }

func packageFixture(t *testing.T, role model.NodeType) (string, identity.Identity) {
	t.Helper()
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, string(role)+".yaml"), []byte("server: http://example.test:8080\nnode_name: original-node\njoin_token: original-join\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	value, err := identity.New(role, "original-node", "http://example.test:8080")
	if err != nil {
		t.Fatal(err)
	}
	value.RegisteredNodeName = value.NodeName
	value.NodeToken = "original-node-token"
	value.ConfigVersion = 7
	value.OwnedRoutes = []string{"192.168.13.0/24"}
	store, _ := identity.NewStore(filepath.Join(directory, "identity.json"), testProtector{})
	if err := store.Save(value); err != nil {
		t.Fatal(err)
	}
	if role == model.NodeTypeEngineer {
		profiles, _ := siteprofile.NewStore(filepath.Join(directory, "site-profiles.json"))
		if err := profiles.Save("site-a", "original-site", []string{"192.168.13.0/24"}); err != nil {
			t.Fatal(err)
		}
	}
	return directory, value
}

func TestPackageReplacementPreservesMachineIdentityAndSettings(t *testing.T) {
	for _, role := range model.NodeTypes {
		t.Run(string(role), func(t *testing.T) {
			originalPackage, original := packageFixture(t, role)
			dataDirectory := filepath.Join(t.TempDir(), string(role))
			first, err := prepareAt(role, originalPackage, dataDirectory, "", "", testProtector{}, foreignMachine)
			if err != nil {
				t.Fatal(err)
			}
			store, _ := identity.NewStore(first.Identity, testProtector{})
			loaded, err := store.Load()
			if err != nil || !reflect.DeepEqual(original, loaded) {
				t.Fatal("migration changed identity, credentials, or owned routes")
			}
			beforeConfig, err := os.ReadFile(first.Config)
			if err != nil {
				t.Fatal(err)
			}
			newPackage := t.TempDir()
			// Neither a different package's settings nor a broken legacy
			// identity may replace an existing machine's data.
			if err := os.WriteFile(filepath.Join(newPackage, string(role)+".yaml"), []byte("server: http://different.test\nnode_name: different-node\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(newPackage, "identity.json"), []byte("broken identity"), 0o600); err != nil {
				t.Fatal(err)
			}
			second, err := prepareAt(role, newPackage, dataDirectory, "", "", testProtector{}, foreignMachine)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(first, second) {
				t.Fatal("replacement package changed the machine data paths")
			}
			afterConfig, err := os.ReadFile(second.Config)
			if err != nil || !bytes.Equal(beforeConfig, afterConfig) {
				t.Fatal("replacement package overwrote the machine configuration")
			}
			loaded, err = store.Load()
			if err != nil || !reflect.DeepEqual(original, loaded) {
				t.Fatal("replacement package overwrote the machine identity")
			}
			if role == model.NodeTypeEngineer {
				profiles, _ := siteprofile.NewStore(second.Profiles)
				loadedProfiles, err := profiles.Load()
				if err != nil || len(loadedProfiles) != 1 || !reflect.DeepEqual(loadedProfiles["site-a"].CIDRs, original.OwnedRoutes) {
					t.Fatal("replacement package lost saved Site CIDRs")
				}
			}
			if _, err := os.Stat(filepath.Join(newPackage, "site-profiles.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("machine Site profiles were copied into the replacement package")
			}
		})
	}
}

func TestUsedPackageOnAnotherMachineDoesNotImportForeignIdentity(t *testing.T) {
	directory, _ := packageFixture(t, model.NodeTypeEngineer)
	paths, err := prepareAt(model.NodeTypeEngineer, directory, t.TempDir(), "", "", testProtector{foreign: true}, foreignMachine)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{paths.Identity, paths.Profiles} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("imported another machine's file: %s", path)
		}
	}
	if _, err := config.LoadEngineer(paths.Config); err != nil {
		t.Fatal(err)
	}
}

func TestFreshPackageSeedsSettingsWithoutCreatingPackageIdentity(t *testing.T) {
	directory, _ := packageFixture(t, model.NodeTypeSite)
	if err := os.Remove(filepath.Join(directory, "identity.json")); err != nil {
		t.Fatal(err)
	}
	paths, err := prepareAt(model.NodeTypeSite, directory, t.TempDir(), "", "", testProtector{}, foreignMachine)
	if err != nil {
		t.Fatal(err)
	}
	value, _ := identity.New(model.NodeTypeSite, "new-site", "http://example.test:8080")
	store, _ := identity.NewStore(paths.Identity, testProtector{})
	if err := store.Save(value); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(directory, "identity.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("new Node identity was written into the software package")
	}
}

func TestCorruptLegacyIdentityFailsWithoutDiscardingOriginal(t *testing.T) {
	directory, _ := packageFixture(t, model.NodeTypeSite)
	legacy := filepath.Join(directory, "identity.json")
	if err := os.WriteFile(legacy, []byte("broken identity"), 0o600); err != nil {
		t.Fatal(err)
	}
	dataDirectory := t.TempDir()
	if _, err := prepareAt(model.NodeTypeSite, directory, dataDirectory, "", "", testProtector{}, foreignMachine); err == nil {
		t.Fatal("corrupt legacy identity was silently replaced")
	}
	if raw, err := os.ReadFile(legacy); err != nil || string(raw) != "broken identity" {
		t.Fatal("failed migration modified the original identity")
	}
	if _, err := os.Stat(filepath.Join(dataDirectory, "identity.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed migration created a replacement identity")
	}
}

func TestExplicitPathsArePreserved(t *testing.T) {
	directory, _ := packageFixture(t, model.NodeTypeSite)
	configPath := filepath.Join(directory, "site.yaml")
	identityPath := filepath.Join(t.TempDir(), "custom-identity.json")
	paths, err := prepareAt(model.NodeTypeSite, directory, t.TempDir(), configPath, identityPath, testProtector{}, foreignMachine)
	if err != nil {
		t.Fatal(err)
	}
	if paths.Config != configPath || paths.Identity != identityPath || paths.Logs != filepath.Join(filepath.Dir(identityPath), "logs") {
		t.Fatalf("explicit paths were ignored: %+v", paths)
	}
	if _, err := os.Stat(identityPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("explicit identity path was populated from an unrelated package identity")
	}
}
