package serverwg

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// LoadOrCreatePrivateKey reads a Server key or atomically creates a 0600 file.
func LoadOrCreatePrivateKey(path string) (wgtypes.Key, error) {
	if raw, err := os.ReadFile(path); err == nil {
		key, err := wgtypes.ParseKey(strings.TrimSpace(string(raw)))
		if err != nil {
			return wgtypes.Key{}, fmt.Errorf("parse Server private key %q: %w", path, err)
		}
		if err := os.Chmod(path, 0o600); err != nil {
			return wgtypes.Key{}, fmt.Errorf("restrict Server private key permissions: %w", err)
		}
		return key, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return wgtypes.Key{}, fmt.Errorf("read Server private key %q: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return wgtypes.Key{}, fmt.Errorf("create Server key directory: %w", err)
	}
	key, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return wgtypes.Key{}, fmt.Errorf("generate Server WireGuard private key: %w", err)
	}
	directory := filepath.Dir(path)
	file, err := os.CreateTemp(directory, ".server-wg-*.tmp")
	if err != nil {
		return wgtypes.Key{}, fmt.Errorf("create temporary Server private key: %w", err)
	}
	temporaryPath := file.Name()
	defer os.Remove(temporaryPath)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return wgtypes.Key{}, fmt.Errorf("restrict temporary Server private key: %w", err)
	}
	writeErr := func() error {
		if _, err := file.WriteString(key.String() + "\n"); err != nil {
			return err
		}
		return file.Sync()
	}()
	closeErr := file.Close()
	if writeErr != nil {
		return wgtypes.Key{}, fmt.Errorf("write Server private key: %w", writeErr)
	}
	if closeErr != nil {
		return wgtypes.Key{}, fmt.Errorf("close Server private key: %w", closeErr)
	}
	// A hard link publishes the fully flushed inode without overwriting a key
	// concurrently created by another Server process. Temporary and final files
	// are guaranteed to be on the same data-directory filesystem.
	if err := os.Link(temporaryPath, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return LoadOrCreatePrivateKey(path)
		}
		return wgtypes.Key{}, fmt.Errorf("publish Server private key %q: %w", path, err)
	}
	return key, nil
}
