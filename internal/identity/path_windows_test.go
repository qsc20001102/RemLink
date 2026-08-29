//go:build windows

package identity

import (
	"path/filepath"
	"testing"

	"remlink/internal/appdir"
	"remlink/internal/model"
)

func TestDefaultPathUsesExecutableDirectoryForPortableRoles(t *testing.T) {
	root, err := appdir.Executable()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "identity.json")
	for _, role := range []model.NodeType{model.NodeTypeEngineer, model.NodeTypeSite} {
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
		t.Fatal("DefaultPath accepted a non-portable role")
	}
}
