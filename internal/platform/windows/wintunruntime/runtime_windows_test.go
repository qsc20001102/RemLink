//go:build windows && amd64

package wintunruntime

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestInstallIsVerifiedIdempotentAndRepairsUnexpectedFile(t *testing.T) {
	root := t.TempDir()
	path, err := Install(root)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if got, want := path, filepath.Join(root, DLLName); got != want {
		t.Fatalf("Install() path = %q, want %q", got, want)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	secondPath, err := Install(root)
	if err != nil {
		t.Fatalf("second Install() error = %v", err)
	}
	if secondPath != path {
		t.Fatalf("second Install() path = %q, want %q", secondPath, path)
	}

	if err := os.WriteFile(path, []byte("tampered"), 0o644); err != nil {
		t.Fatalf("tamper DLL: %v", err)
	}
	if _, err := Install(root); err != nil {
		t.Fatalf("repair Install() error = %v", err)
	}
	repaired, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read repaired DLL: %v", err)
	}
	if string(repaired) != string(first) {
		t.Fatal("Install() did not repair the unexpected DLL")
	}
}

func TestInstallPublishesAtomicallyAcrossConcurrentProcesses(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, DLLName)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	const workers = 16
	errorsByWorker := make([]error, workers)
	paths := make([]string, workers)
	var wait sync.WaitGroup
	for index := 0; index < workers; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			paths[index], errorsByWorker[index] = Install(root)
		}(index)
	}
	wait.Wait()
	for index := range errorsByWorker {
		if errorsByWorker[index] != nil || paths[index] != target {
			t.Fatalf("worker %d path=%q error=%v", index, paths[index], errorsByWorker[index])
		}
	}
	data, expected, err := assetBytes()
	if err != nil {
		t.Fatal(err)
	}
	installed, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(installed) != string(data) {
		t.Fatalf("concurrent install produced %d bytes, want %d (expected hash %x)", len(installed), len(data), expected)
	}
}
