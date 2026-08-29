// Package identity persists the Windows Node identity without plaintext secrets.
package identity

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"remlink/internal/config"
	"remlink/internal/model"
)

const diskVersion = 1

// Protector is implemented by the Windows DPAPI adapter.
type Protector interface {
	Protect([]byte) ([]byte, error)
	Unprotect([]byte) ([]byte, error)
}

// Identity is the durable Node identity used by Bootstrap and reconciliation.
type Identity struct {
	NodeID        string
	NodeType      model.NodeType
	NodeName      string
	PrivateKey    wgtypes.Key
	NodeToken     string
	ServerURL     string
	ConfigVersion uint64
	OwnedRoutes   []string
}

// PublicKey derives the shareable WireGuard key from the protected private key.
func (i Identity) PublicKey() string { return i.PrivateKey.PublicKey().String() }

// New generates a UUID and independent WireGuard key pair.
func New(nodeType model.NodeType, nodeName, serverURL string) (Identity, error) {
	privateKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return Identity{}, fmt.Errorf("generate Node WireGuard key: %w", err)
	}
	identity := Identity{
		NodeID: uuid.NewString(), NodeType: nodeType, NodeName: strings.TrimSpace(nodeName),
		PrivateKey: privateKey, ServerURL: strings.TrimRight(strings.TrimSpace(serverURL), "/"),
	}
	if err := identity.Validate(false); err != nil {
		return Identity{}, err
	}
	return identity, nil
}

// Validate checks persistent identity invariants. requireToken is true after enrollment.
func (i Identity) Validate(requireToken bool) error {
	if _, err := uuid.Parse(i.NodeID); err != nil {
		return fmt.Errorf("Node ID must be a UUID: %w", err)
	}
	if !i.NodeType.Valid() {
		return fmt.Errorf("invalid Node type %q", i.NodeType)
	}
	if i.NodeName == "" || len(i.NodeName) > 128 {
		return errors.New("Node name must contain 1 to 128 bytes")
	}
	if err := config.ValidateServerURL(i.ServerURL); err != nil {
		return fmt.Errorf("invalid Server URL: %w", err)
	}
	if i.PrivateKey == (wgtypes.Key{}) {
		return errors.New("WireGuard private key must not be zero")
	}
	if requireToken && i.NodeToken == "" {
		return errors.New("Node Token is required after registration")
	}
	return nil
}

// Store saves one role-specific identity JSON file.
type Store struct {
	path      string
	protector Protector
}

func NewStore(path string, protector Protector) (*Store, error) {
	if strings.TrimSpace(path) == "" || protector == nil {
		return nil, errors.New("identity path and Protector are required")
	}
	return &Store{path: path, protector: protector}, nil
}

// Path returns the exact identity artifact location.
func (s *Store) Path() string { return s.path }

// LoadOwnedRoutes implements RouteManager's persistence boundary.
func (s *Store) LoadOwnedRoutes() ([]netip.Prefix, error) {
	current, err := s.Load()
	if err != nil {
		return nil, err
	}
	routes := make([]netip.Prefix, 0, len(current.OwnedRoutes))
	for _, raw := range current.OwnedRoutes {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil || !prefix.Addr().Is4() || prefix != prefix.Masked() {
			return nil, fmt.Errorf("invalid owned Remote route %q", raw)
		}
		routes = append(routes, prefix)
	}
	return routes, nil
}

// SaveOwnedRoutes atomically updates RouteManager ownership metadata.
func (s *Store) SaveOwnedRoutes(routes []netip.Prefix) error {
	current, err := s.Load()
	if err != nil {
		return err
	}
	current.OwnedRoutes = make([]string, 0, len(routes))
	for _, prefix := range routes {
		if !prefix.Addr().Is4() || prefix != prefix.Masked() {
			return fmt.Errorf("owned Remote route must be canonical IPv4: %s", prefix)
		}
		current.OwnedRoutes = append(current.OwnedRoutes, prefix.String())
	}
	return s.Save(current)
}

// Load decrypts and validates a persisted identity.
func (s *Store) Load() (Identity, error) {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return Identity{}, fmt.Errorf("read Node identity: %w", err)
	}
	var stored diskIdentity
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&stored); err != nil {
		return Identity{}, fmt.Errorf("decode Node identity: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Identity{}, errors.New("Node identity must contain one JSON object")
	}
	if stored.Version != diskVersion {
		return Identity{}, fmt.Errorf("unsupported Node identity version %d", stored.Version)
	}
	privateKeyRaw, err := s.unprotect(stored.ProtectedPrivateKey)
	if err != nil {
		return Identity{}, fmt.Errorf("decrypt WireGuard private key: %w", err)
	}
	privateKey, err := wgtypes.ParseKey(string(privateKeyRaw))
	clear(privateKeyRaw)
	if err != nil {
		return Identity{}, fmt.Errorf("parse decrypted WireGuard private key: %w", err)
	}
	nodeToken := ""
	if stored.ProtectedNodeToken != "" {
		nodeTokenRaw, err := s.unprotect(stored.ProtectedNodeToken)
		if err != nil {
			return Identity{}, fmt.Errorf("decrypt Node Token: %w", err)
		}
		nodeToken = string(nodeTokenRaw)
		clear(nodeTokenRaw)
	}
	identity := Identity{
		NodeID: stored.NodeID, NodeType: stored.NodeType, NodeName: stored.NodeName,
		PrivateKey: privateKey, NodeToken: nodeToken, ServerURL: stored.ServerURL,
		ConfigVersion: stored.ConfigVersion, OwnedRoutes: append([]string(nil), stored.OwnedRoutes...),
	}
	if err := identity.Validate(false); err != nil {
		return Identity{}, fmt.Errorf("validate Node identity: %w", err)
	}
	return identity, nil
}

// Save protects all secrets and atomically replaces the identity artifact.
func (s *Store) Save(identity Identity) error {
	if err := identity.Validate(false); err != nil {
		return err
	}
	privateKeyRaw := []byte(identity.PrivateKey.String())
	protectedPrivateKey, err := s.protect(privateKeyRaw)
	clear(privateKeyRaw)
	if err != nil {
		return fmt.Errorf("encrypt WireGuard private key: %w", err)
	}
	protectedNodeToken := ""
	if identity.NodeToken != "" {
		protectedNodeToken, err = s.protect([]byte(identity.NodeToken))
		if err != nil {
			return fmt.Errorf("encrypt Node Token: %w", err)
		}
	}
	stored := diskIdentity{
		Version: diskVersion, NodeID: identity.NodeID, NodeType: identity.NodeType,
		NodeName: identity.NodeName, ProtectedPrivateKey: protectedPrivateKey,
		ProtectedNodeToken: protectedNodeToken, ServerURL: identity.ServerURL,
		ConfigVersion: identity.ConfigVersion, OwnedRoutes: identity.OwnedRoutes,
	}
	encoded, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Node identity: %w", err)
	}
	encoded = append(encoded, '\n')
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create Node identity directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(s.path), ".identity-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary Node identity: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("restrict temporary Node identity: %w", err)
	}
	if _, err := temporary.Write(encoded); err != nil {
		temporary.Close()
		return fmt.Errorf("write temporary Node identity: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("flush temporary Node identity: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary Node identity: %w", err)
	}
	if err := replaceFile(temporaryPath, s.path); err != nil {
		return fmt.Errorf("replace Node identity: %w", err)
	}
	return nil
}

func (s *Store) protect(value []byte) (string, error) {
	protected, err := s.protector.Protect(value)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(protected), nil
}

func (s *Store) unprotect(value string) ([]byte, error) {
	protected, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, err
	}
	return s.protector.Unprotect(protected)
}

type diskIdentity struct {
	Version             int            `json:"version"`
	NodeID              string         `json:"node_id"`
	NodeType            model.NodeType `json:"node_type"`
	NodeName            string         `json:"node_name"`
	ProtectedPrivateKey string         `json:"protected_wg_private_key"`
	ProtectedNodeToken  string         `json:"protected_node_token,omitempty"`
	ServerURL           string         `json:"server_url"`
	ConfigVersion       uint64         `json:"config_version"`
	OwnedRoutes         []string       `json:"owned_routes,omitempty"`
}
