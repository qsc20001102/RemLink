//go:build windows

package clientstate

import (
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"

	"remlink/internal/identity"
	"remlink/internal/model"
	"remlink/internal/platform/windows/dpapi"
)

func TestIdentityLockRejectsAnotherPackageAndReleasesOnExit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.json")
	release, err := lockIdentityPath(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = release() }()
	if duplicate, err := lockIdentityPath(path); err == nil {
		_ = duplicate()
		t.Fatal("two packages acquired the same machine identity")
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	release, err = lockIdentityPath(path)
	if err != nil {
		t.Fatalf("identity remained locked after exit: %v", err)
	}
}

func TestMigrationPreservesRealWindowsDPAPIIdentity(t *testing.T) {
	directory, original := packageFixture(t, model.NodeTypeEngineer)
	legacy, _ := identity.NewStore(filepath.Join(directory, "identity.json"), dpapi.Protector{})
	if err := legacy.Save(original); err != nil {
		t.Fatal(err)
	}
	dataDirectory := t.TempDir()
	paths, err := prepareAt(model.NodeTypeEngineer, directory, dataDirectory, "", "", dpapi.Protector{}, func(err error) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	store, _ := identity.NewStore(paths.Identity, dpapi.Protector{})
	got, err := store.Load()
	if err != nil || got.NodeID != original.NodeID || got.PrivateKey != original.PrivateKey || got.NodeToken != original.NodeToken {
		t.Fatal("migration did not preserve the DPAPI-protected identity")
	}
}

func TestMachineDirectoryPermissions(t *testing.T) {
	directory := t.TempDir()
	original, err := windows.GetNamedSecurityInfo(directory, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	originalDACL, _, err := original.DACL()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := windows.SetNamedSecurityInfo(directory, windows.SE_FILE_OBJECT,
			windows.DACL_SECURITY_INFORMATION|windows.UNPROTECTED_DACL_SECURITY_INFORMATION, nil, nil, originalDACL, nil); err != nil {
			t.Errorf("restore test directory permissions: %v", err)
		}
	}()
	if err := secureDirectory(directory); err != nil {
		t.Fatal(err)
	}
	descriptor, err := windows.GetNamedSecurityInfo(directory, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := descriptor.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatal("machine data directory inherits ordinary user access")
	}
	dacl, _, err := descriptor.DACL()
	if err != nil || dacl == nil || dacl.AceCount != 2 ||
		!strings.Contains(descriptor.String(), ";;;SY)") || !strings.Contains(descriptor.String(), ";;;BA)") {
		t.Fatal("machine data directory does not restrict access to SYSTEM and Administrators")
	}
}
