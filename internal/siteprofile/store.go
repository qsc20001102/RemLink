// Package siteprofile persists non-secret Engineer Remote CIDR preferences per Site.
package siteprofile

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const diskVersion = 1

type Profile struct {
	SiteName  string    `json:"site_name"`
	CIDRs     []string  `json:"remote_cidrs"`
	UpdatedAt time.Time `json:"updated_at"`
}

type diskProfiles struct {
	Version int                `json:"version"`
	Sites   map[string]Profile `json:"sites"`
}

type Store struct {
	mu   sync.Mutex
	path string
}

func NewStore(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("Site profile path is required")
	}
	return &Store{path: path}, nil
}

func (s *Store) Path() string { return s.path }

func (s *Store) Load() (map[string]Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	profiles, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	return cloneProfiles(profiles), nil
}

func (s *Store) Save(siteID, siteName string, cidrs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	siteID = strings.TrimSpace(siteID)
	siteName = strings.TrimSpace(siteName)
	if siteID == "" || siteName == "" {
		return errors.New("Site ID and name are required")
	}
	canonical, err := canonicalCIDRs(cidrs)
	if err != nil {
		return err
	}
	profiles, err := s.loadLocked()
	if err != nil {
		return err
	}
	if len(canonical) == 0 {
		delete(profiles, siteID)
	} else {
		profiles[siteID] = Profile{SiteName: siteName, CIDRs: canonical, UpdatedAt: time.Now().UTC()}
	}
	return s.saveLocked(profiles)
}

func (s *Store) loadLocked() (map[string]Profile, error) {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]Profile{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read Site profiles: %w", err)
	}
	var stored diskProfiles
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&stored); err != nil {
		return nil, fmt.Errorf("decode Site profiles: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("Site profiles must contain one JSON object")
	}
	if stored.Version != diskVersion {
		return nil, fmt.Errorf("unsupported Site profile version %d", stored.Version)
	}
	if stored.Sites == nil {
		stored.Sites = map[string]Profile{}
	}
	for siteID, profile := range stored.Sites {
		if strings.TrimSpace(siteID) == "" || strings.TrimSpace(profile.SiteName) == "" {
			return nil, errors.New("stored Site profile has an empty Site ID or name")
		}
		canonical, err := canonicalCIDRs(profile.CIDRs)
		if err != nil {
			return nil, fmt.Errorf("validate Site profile %q: %w", siteID, err)
		}
		if len(canonical) == 0 {
			return nil, fmt.Errorf("validate Site profile %q: Remote CIDRs are empty", siteID)
		}
		profile.CIDRs = canonical
		stored.Sites[siteID] = profile
	}
	return stored.Sites, nil
}

func (s *Store) saveLocked(profiles map[string]Profile) error {
	encoded, err := json.MarshalIndent(diskProfiles{Version: diskVersion, Sites: profiles}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Site profiles: %w", err)
	}
	encoded = append(encoded, '\n')
	directory := filepath.Dir(s.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create Site profile directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".site-profiles-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary Site profiles: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("restrict temporary Site profiles: %w", err)
	}
	if _, err := temporary.Write(encoded); err != nil {
		temporary.Close()
		return fmt.Errorf("write temporary Site profiles: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("flush temporary Site profiles: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary Site profiles: %w", err)
	}
	if err := replaceFile(temporaryPath, s.path); err != nil {
		return fmt.Errorf("replace Site profiles: %w", err)
	}
	return nil
}

func canonicalCIDRs(cidrs []string) ([]string, error) {
	result := make([]string, 0, len(cidrs))
	seen := make(map[netip.Prefix]struct{}, len(cidrs))
	for _, raw := range cidrs {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil || !prefix.Addr().Is4() || prefix.Bits() == 0 || prefix != prefix.Masked() {
			return nil, fmt.Errorf("Remote CIDR must be canonical IPv4 and cannot be /0: %q", raw)
		}
		if _, exists := seen[prefix]; exists {
			return nil, fmt.Errorf("duplicate Remote CIDR %q", prefix)
		}
		seen[prefix] = struct{}{}
		result = append(result, prefix.String())
	}
	return result, nil
}

func cloneProfiles(source map[string]Profile) map[string]Profile {
	result := make(map[string]Profile, len(source))
	for siteID, profile := range source {
		profile.CIDRs = append([]string(nil), profile.CIDRs...)
		result[siteID] = profile
	}
	return result
}
