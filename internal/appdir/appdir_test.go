package appdir

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExecutableAndJoinUseRunningExecutableDirectory(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs(filepath.Dir(executable))
	if err != nil {
		t.Fatal(err)
	}
	root, err := Executable()
	if err != nil {
		t.Fatal(err)
	}
	if root != want {
		t.Fatalf("Executable() = %q, want %q", root, want)
	}
	joined, err := Join("logs", "node.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if joined != filepath.Join(want, "logs", "node.jsonl") {
		t.Fatalf("Join() = %q", joined)
	}
}
