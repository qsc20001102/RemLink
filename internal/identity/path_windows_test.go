//go:build windows

package identity

import (
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"

	"remlink/internal/model"
)

func TestDefaultPathUsesSeparateMachineDirectories(t *testing.T) {
	root, err := windows.KnownFolderPath(windows.FOLDERID_ProgramData, windows.KF_FLAG_DEFAULT)
	if err != nil {
		t.Fatal(err)
	}
	for role, name := range map[model.NodeType]string{model.NodeTypeEngineer: "Engineer", model.NodeTypeSite: "Site"} {
		want := filepath.Join(root, "RemLink", name, "identity.json")
		got, err := DefaultPath(role)
		if err != nil {
			t.Fatalf("DefaultPath(%s): %v", role, err)
		}
		if got != want {
			t.Fatalf("DefaultPath(%s) = %q, want %q", role, got, want)
		}
	}
}

func TestDefaultPathRejectsUnknownRole(t *testing.T) {
	if _, err := DefaultPath(model.NodeType("server")); err == nil {
		t.Fatal("DefaultPath accepted an unknown Windows role")
	}
}
