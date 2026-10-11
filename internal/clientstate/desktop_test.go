package clientstate

import (
	"os"
	"path/filepath"
	"remlink/internal/clientsettings"
	"remlink/internal/identity"
	"remlink/internal/model"
	"testing"
)

func TestDesktopColdLaunchNeedsNeitherYAMLNorIdentity(t *testing.T) {
	for _, role := range model.NodeTypes {
		paths, err := prepareDesktopAt(role, t.TempDir(), t.TempDir(), "", "", testProtector{}, foreignMachine)
		if err != nil {
			t.Fatal(err)
		}
		store, _ := clientsettings.NewStore(paths.Settings, role, testProtector{})
		if _, configured, err := store.Load(); err != nil || configured {
			t.Fatal("fresh desktop launch was automatically configured")
		}
		if _, err := os.Stat(paths.Identity); !os.IsNotExist(err) {
			t.Fatal("opening the configuration screen created a Node identity")
		}
	}
}

func TestDesktopImportsPreviousMachineSettingsAndKeepsThemAfterPackageChange(t *testing.T) {
	packageDir, original := packageFixture(t, model.NodeTypeEngineer)
	dataDir := t.TempDir()
	paths, err := prepareDesktopAt(model.NodeTypeEngineer, packageDir, dataDir, "", "", testProtector{}, foreignMachine)
	if err != nil {
		t.Fatal(err)
	}
	store, _ := clientsettings.NewStore(paths.Settings, model.NodeTypeEngineer, testProtector{})
	settings, configured, err := store.Load()
	if err != nil || !configured || settings.NodeName != original.NodeName {
		t.Fatal("existing settings were not imported")
	}
	identityStore, _ := identity.NewStore(paths.Identity, testProtector{})
	loaded, err := identityStore.Load()
	if err != nil || loaded.NodeID != original.NodeID || loaded.NodeToken != original.NodeToken {
		t.Fatal("GUI migration changed registered identity")
	}
	settings.NodeName = "edited-in-app"
	if err := store.Save(settings); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareDesktopAt(model.NodeTypeEngineer, t.TempDir(), dataDir, "", "", testProtector{}, foreignMachine); err != nil {
		t.Fatal(err)
	}
	settings, _, _ = store.Load()
	if settings.NodeName != "edited-in-app" {
		t.Fatal("new package overwrote application settings")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "site-profiles.json")); err != nil {
		t.Fatal("GUI migration lost saved Site subnets")
	}
}

func TestUsedPackageOnFreshDesktopStillRequiresConfiguration(t *testing.T) {
	packageDir, _ := packageFixture(t, model.NodeTypeEngineer)
	paths, err := prepareDesktopAt(model.NodeTypeEngineer, packageDir, t.TempDir(), "", "", testProtector{foreign: true}, foreignMachine)
	if err != nil {
		t.Fatal(err)
	}
	store, _ := clientsettings.NewStore(paths.Settings, model.NodeTypeEngineer, testProtector{})
	if _, configured, _ := store.Load(); configured {
		t.Fatal("another PC's package silently configured this machine")
	}
}
