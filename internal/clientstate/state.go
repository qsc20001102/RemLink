// Package clientstate prepares machine-local Windows client settings and imports
// legacy package-local data without replacing an existing machine identity.
package clientstate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"remlink/internal/config"
	"remlink/internal/identity"
	"remlink/internal/model"
	"remlink/internal/siteprofile"
)

type Paths struct {
	Config   string
	Settings string
	Identity string
	Profiles string
	Logs     string
	release  func() error
}

// Close releases the process lock held for the complete client lifetime.
func (p *Paths) Close() error {
	if p.release == nil {
		return nil
	}
	release := p.release
	p.release = nil
	return release()
}

func prepareAt(role model.NodeType, packageDirectory, dataDirectory, configOverride, identityOverride string,
	protector identity.Protector, foreignIdentity func(error) bool) (*Paths, error) {
	if !role.Valid() {
		return nil, errors.New("invalid Windows Node type")
	}
	paths := &Paths{Config: filepath.Join(dataDirectory, string(role)+".yaml"), Identity: filepath.Join(dataDirectory, "identity.json")}
	if configOverride != "" {
		paths.Config = configOverride
	}
	if identityOverride != "" {
		paths.Identity = identityOverride
	}
	paths.Profiles = filepath.Join(filepath.Dir(paths.Identity), "site-profiles.json")
	paths.Logs = filepath.Join(filepath.Dir(paths.Identity), "logs")
	if err := os.MkdirAll(dataDirectory, 0o700); err != nil {
		return nil, fmt.Errorf("create client data directory: %w", err)
	}
	var legacy *identity.Identity
	if identityOverride == "" {
		_, err := os.Stat(paths.Identity)
		if errors.Is(err, os.ErrNotExist) {
			store, err := identity.NewStore(filepath.Join(packageDirectory, "identity.json"), protector)
			if err != nil {
				return nil, err
			}
			loaded, err := store.Load()
			switch {
			case errors.Is(err, os.ErrNotExist):
			case err != nil && foreignIdentity(err):
				// A used package sent to another PC cannot decrypt its original
				// identity. First enrollment will create this PC's own identity.
			case err != nil:
				return nil, fmt.Errorf("import package Node identity: %w", err)
			case loaded.NodeType != role:
				return nil, errors.New("package Node identity does not match this executable")
			default:
				legacy = &loaded
			}
		} else if err != nil {
			return nil, fmt.Errorf("inspect machine Node identity: %w", err)
		}
	}
	if configOverride == "" {
		if err := copyMissing(filepath.Join(packageDirectory, string(role)+".yaml"), paths.Config, func(path string) error {
			return validateConfig(role, path)
		}); err != nil {
			return nil, err
		}
	}
	if err := validateConfig(role, paths.Config); err != nil {
		return nil, err
	}
	if legacy != nil {
		if role == model.NodeTypeEngineer {
			if err := copyMissing(filepath.Join(packageDirectory, "site-profiles.json"), paths.Profiles, func(path string) error {
				store, err := siteprofile.NewStore(path)
				if err != nil {
					return err
				}
				_, err = store.Load()
				return err
			}); err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
		}
		store, err := identity.NewStore(paths.Identity, protector)
		if err != nil {
			return nil, err
		}
		if err := store.Save(*legacy); err != nil {
			return nil, fmt.Errorf("save imported Node identity: %w", err)
		}
	}
	return paths, nil
}

func validateConfig(role model.NodeType, path string) error {
	if role == model.NodeTypeEngineer {
		_, err := config.LoadEngineer(path)
		return err
	}
	_, err := config.LoadSite(path)
	return err
}

func copyMissing(source, destination string, validate func(string) error) error {
	if _, err := os.Stat(destination); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := validate(source); err != nil {
		return err
	}
	raw, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read legacy client file %q: %w", source, err)
	}
	file, err := os.CreateTemp(filepath.Dir(destination), ".client-state-*.tmp")
	if err != nil {
		return fmt.Errorf("create machine client file %q: %w", destination, err)
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	_, writeErr := file.Write(raw)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return fmt.Errorf("import client file %q: %w", source, err)
	}
	if err := publishMissing(file.Name(), destination); err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("publish machine client file %q: %w", destination, err)
	}
	return nil
}
