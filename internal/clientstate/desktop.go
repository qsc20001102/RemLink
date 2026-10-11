package clientstate

import (
	"errors"
	"os"
	"path/filepath"

	"remlink/internal/clientsettings"
	"remlink/internal/config"
	"remlink/internal/identity"
	"remlink/internal/model"
	"remlink/internal/siteprofile"
)

// prepareDesktopAt permits a fresh machine to open its UI without YAML or
// credentials. Only previously used, local identities enable legacy auto-import.
func prepareDesktopAt(role model.NodeType, packageDirectory, dataDirectory, configOverride, identityOverride string,
	protector identity.Protector, foreignIdentity func(error) bool) (*Paths, error) {
	paths := &Paths{Identity: filepath.Join(dataDirectory, "identity.json")}
	if identityOverride != "" {
		paths.Identity = identityOverride
	}
	paths.Settings = filepath.Join(filepath.Dir(paths.Identity), "settings.json")
	paths.Profiles = filepath.Join(filepath.Dir(paths.Identity), "site-profiles.json")
	paths.Logs = filepath.Join(filepath.Dir(paths.Identity), "logs")
	store, err := clientsettings.NewStore(paths.Settings, role, protector)
	if err != nil {
		return nil, err
	}
	if _, exists, err := store.Load(); err != nil || exists {
		return paths, err
	}
	localIdentity, _ := identity.NewStore(paths.Identity, protector)
	current, identityErr := localIdentity.Load()
	if identityErr != nil && !errors.Is(identityErr, os.ErrNotExist) {
		return nil, identityErr
	}
	var legacy *identity.Identity
	if errors.Is(identityErr, os.ErrNotExist) && identityOverride == "" {
		oldStore, _ := identity.NewStore(filepath.Join(packageDirectory, "identity.json"), protector)
		loaded, err := oldStore.Load()
		if err != nil && !errors.Is(err, os.ErrNotExist) && !foreignIdentity(err) {
			return nil, err
		}
		if err == nil {
			if loaded.NodeType != role {
				return nil, errors.New("package Node identity does not match this executable")
			}
			legacy = &loaded
			current = loaded
		}
	}
	if identityErr == nil && current.NodeType != role {
		return nil, errors.New("machine Node identity does not match this executable")
	}
	if current.NodeID == "" && configOverride == "" {
		return paths, nil
	}
	legacyConfig := configOverride
	if legacyConfig == "" {
		legacyConfig = filepath.Join(dataDirectory, string(role)+".yaml")
		if _, err := os.Stat(legacyConfig); errors.Is(err, os.ErrNotExist) {
			legacyConfig = filepath.Join(packageDirectory, string(role)+".yaml")
		}
	}
	value := clientsettings.Defaults(role)
	if role == model.NodeTypeEngineer {
		cfg, err := config.LoadEngineer(legacyConfig)
		if err == nil {
			value.ServerURL, value.NodeName, value.JoinToken = cfg.Server, cfg.NodeName, cfg.JoinToken
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	} else {
		cfg, err := config.LoadSite(legacyConfig)
		if err == nil {
			value.ServerURL, value.NodeName, value.JoinToken, value.Netstack = cfg.Server, cfg.NodeName, cfg.JoinToken, cfg.Netstack
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	if value.ServerURL == "" && current.NodeID != "" {
		value.ServerURL, value.NodeName = current.ServerURL, current.NodeName
	}
	if legacy != nil {
		if role == model.NodeTypeEngineer {
			if err := copyMissing(filepath.Join(packageDirectory, "site-profiles.json"), paths.Profiles, func(path string) error {
				profiles, err := siteprofile.NewStore(path)
				if err != nil {
					return err
				}
				_, err = profiles.Load()
				return err
			}); err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
		}
		if err := localIdentity.Save(*legacy); err != nil {
			return nil, err
		}
	}
	if value.ServerURL != "" {
		if err := store.Save(value); err != nil {
			return nil, err
		}
	}
	return paths, nil
}
