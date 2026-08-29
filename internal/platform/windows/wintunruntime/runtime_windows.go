//go:build windows

// Package wintunruntime installs and preloads the pinned embedded Wintun DLL.
package wintunruntime

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wintun"

	"remlink/internal/appdir"
)

const (
	Version        = "0.14.1"
	DLLName        = "wintun.dll"
	DLLSHA256AMD64 = "e5da8447dc2c320edc0fc52fa01885c103de8c118481f683643cacc3220dafce"
)

var (
	preloadOnce sync.Once
	preloadPath string
	preloadErr  error
	// Keep the module referenced for the process lifetime. The upstream Go
	// binding subsequently resolves the already-loaded DLL by base name.
	preloadHandle windows.Handle
)

// Install writes the embedded signed DLL into one portable package directory.
// An existing verified file is reused; an unexpected file is replaced.
func Install(packageDirectory string) (string, error) {
	if packageDirectory == "" {
		return "", errors.New("package directory must not be empty")
	}
	data, expectedHash, err := assetBytes()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(packageDirectory, 0o750); err != nil {
		return "", fmt.Errorf("create Wintun runtime directory: %w", err)
	}
	target := filepath.Join(packageDirectory, DLLName)

	verified, err := fileHasSHA256Stable(target, expectedHash)
	if err != nil {
		return "", err
	}
	if verified {
		return target, nil
	}
	if err := writeAtomically(target, data); err != nil {
		return "", err
	}
	verified, err = fileHasSHA256Stable(target, expectedHash)
	if err != nil {
		return "", err
	}
	if !verified {
		return "", errors.New("installed Wintun DLL failed SHA-256 verification")
	}
	return target, nil
}

// PreloadDefault installs and loads Wintun beside the running executable before
// the upstream lazy binding attempts to resolve wintun.dll by its base name.
func PreloadDefault() (string, error) {
	root, err := appdir.Executable()
	if err != nil {
		return "", err
	}
	return Preload(root)
}

// Preload installs and loads Wintun exactly once for the process.
func Preload(packageDirectory string) (string, error) {
	preloadOnce.Do(func() {
		preloadPath, preloadErr = Install(packageDirectory)
		if preloadErr != nil {
			return
		}
		preloadHandle, preloadErr = windows.LoadLibraryEx(
			preloadPath,
			0,
			windows.LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR|windows.LOAD_LIBRARY_SEARCH_SYSTEM32,
		)
		if preloadErr != nil {
			preloadErr = fmt.Errorf("preload Wintun DLL %q: %w", preloadPath, preloadErr)
		}
	})
	return preloadPath, preloadErr
}

// Probe installs/preloads the DLL and asks the official binding for its version.
func Probe() (path string, version string, err error) {
	path, err = PreloadDefault()
	if err != nil {
		return "", "", err
	}
	version = wintun.Version()
	if version == "unknown" {
		return "", "", errors.New("official Wintun binding could not resolve the preloaded DLL")
	}
	return path, version, nil
}

func fileHasSHA256(path string, expected [sha256.Size]byte) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read existing Wintun DLL: %w", err)
	}
	return sha256.Sum256(data) == expected, nil
}

func fileHasSHA256Stable(path string, expected [sha256.Size]byte) (bool, error) {
	var lastErr error
	for attempt := 0; attempt < 16; attempt++ {
		verified, err := fileHasSHA256(path, expected)
		if err == nil {
			return verified, nil
		}
		if !errors.Is(err, windows.ERROR_ACCESS_DENIED) && !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
			return false, err
		}
		lastErr = err
		time.Sleep(time.Duration(attempt+1) * time.Millisecond)
	}
	return false, lastErr
}

func writeAtomically(target string, data []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(target), ".wintun-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary Wintun DLL: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write temporary Wintun DLL: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync temporary Wintun DLL: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary Wintun DLL: %w", err)
	}
	if err := os.Chmod(temporaryPath, 0o644); err != nil {
		return fmt.Errorf("set Wintun DLL permissions: %w", err)
	}

	from, err := windows.UTF16PtrFromString(temporaryPath)
	if err != nil {
		return fmt.Errorf("encode temporary Wintun DLL path: %w", err)
	}
	to, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return fmt.Errorf("encode Wintun DLL target path: %w", err)
	}
	// MoveFileExW publishes the verified bytes in one replace operation. This
	// avoids a remove/rename gap where another launch from the same package could
	// observe the DLL path as missing. Concurrent publishers
	// may briefly hold a Windows file handle; accept their verified result or
	// retry only the documented sharing/access failures.
	for attempt := 0; attempt < 8; attempt++ {
		if verified, verifyErr := fileHasSHA256(target, sha256.Sum256(data)); verifyErr == nil && verified {
			return nil
		}
		err = windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
		if err == nil {
			return nil
		}
		if !errors.Is(err, windows.ERROR_ACCESS_DENIED) && !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
			return fmt.Errorf("install Wintun DLL: %w", err)
		}
		time.Sleep(time.Duration(attempt+1) * time.Millisecond)
	}
	if verified, verifyErr := fileHasSHA256(target, sha256.Sum256(data)); verifyErr == nil && verified {
		return nil
	}
	return fmt.Errorf("install Wintun DLL after concurrent publish retries: %w", err)
}
