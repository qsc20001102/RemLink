// Package bootstrap implements the public Node enrollment and configuration API.
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/google/uuid"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"remlink/internal/database"
	"remlink/internal/ipam"
	"remlink/internal/model"
)

var (
	ErrJoinTokenInvalid = errors.New("Join Token invalid")
	ErrNodeAuthFailed   = errors.New("Node authentication failed")
	ErrNodeConflict     = errors.New("Node identity conflicts with existing registration")
)

// PeerManager is the narrow kernel-WireGuard boundary used by Bootstrap.
type PeerManager interface {
	EnsurePeer(context.Context, string, netip.Addr) error
}

// ServiceConfig describes the Server values returned to Windows nodes.
type ServiceConfig struct {
	ServerID        string
	Version         string
	WGPublicKey     string
	WGEndpoint      string
	OverlayCIDR     netip.Prefix
	ServerOverlayIP netip.Addr
	ControlURL      string
	SessionUDPPort  int
	MTU             int
	ConfigVersion   uint64
}

// Service coordinates registration, IPAM, token rotation, and peer creation.
type Service struct {
	mu              sync.RWMutex
	store           *database.Store
	ipam            *ipam.Manager
	joins           *JoinTokens
	peers           PeerManager
	config          ServiceConfig
	onNodeBootstrap func(context.Context, string) error
}

// SetNodeBootstrapHandler installs the Server runtime reconciliation boundary.
// A Node calls the public Config endpoint only when constructing a fresh local
// runtime; any old in-memory Session for that Node can no longer be resumed.
func (s *Service) SetNodeBootstrapHandler(handler func(context.Context, string) error) {
	s.mu.Lock()
	s.onNodeBootstrap = handler
	s.mu.Unlock()
}

func NewService(store *database.Store, ipamManager *ipam.Manager, joins *JoinTokens, peers PeerManager, config ServiceConfig) (*Service, error) {
	if store == nil || ipamManager == nil || joins == nil || peers == nil {
		return nil, errors.New("Bootstrap dependencies must not be nil")
	}
	if err := validateServiceConfig(config); err != nil {
		return nil, err
	}
	return &Service{store: store, ipam: ipamManager, joins: joins, peers: peers, config: config}, nil
}

func usableOverlayAddress(prefix netip.Prefix, address netip.Addr) bool {
	if !address.Is4() || !prefix.Contains(address) || address == prefix.Addr() {
		return false
	}
	base := prefix.Masked().Addr().As4()
	value := uint32(base[0])<<24 | uint32(base[1])<<16 | uint32(base[2])<<8 | uint32(base[3])
	value |= ^uint32(0) >> prefix.Bits()
	broadcast := netip.AddrFrom4([4]byte{byte(value >> 24), byte(value >> 16), byte(value >> 8), byte(value)})
	return address != broadcast
}

type RegisterRequest struct {
	JoinToken   string         `json:"join_token"`
	NodeID      string         `json:"node_id"`
	NodeType    model.NodeType `json:"node_type"`
	NodeName    string         `json:"node_name"`
	WGPublicKey string         `json:"wg_public_key"`
	Version     string         `json:"version"`
	OSVersion   string         `json:"os_version"`
}

type ConfigRequest struct {
	NodeID    string `json:"node_id"`
	NodeToken string `json:"node_token"`
}

type ServerInfo struct {
	ServerID        string `json:"server_id"`
	Version         string `json:"version"`
	APIVersion      int    `json:"api_version"`
	WGEndpoint      string `json:"wg_endpoint"`
	RegistrationURL string `json:"registration_url"`
}

type NetworkConfig struct {
	ConfigVersion     uint64 `json:"config_version"`
	OverlayCIDR       string `json:"overlay_cidr"`
	OverlayIP         string `json:"overlay_ip"`
	ServerOverlayIP   string `json:"server_overlay_ip"`
	ServerWGPublicKey string `json:"server_wg_public_key"`
	ServerWGEndpoint  string `json:"server_wg_endpoint"`
	ControlURL        string `json:"control_url"`
	SessionUDPPort    int    `json:"session_udp_port"`
	MTU               int    `json:"mtu"`
}

type RegisterResponse struct {
	NodeToken string        `json:"node_token"`
	Network   NetworkConfig `json:"network_config"`
}

type ConfigResponse struct {
	Network NetworkConfig `json:"network_config"`
}

func (s *Service) ServerInfo() ServerInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return ServerInfo{
		ServerID: s.config.ServerID, Version: s.config.Version, APIVersion: 1,
		WGEndpoint: s.config.WGEndpoint, RegistrationURL: "/api/v1/bootstrap/register",
	}
}

// Register enrolls a new Node or safely rotates credentials for an identical identity.
func (s *Service) Register(ctx context.Context, request RegisterRequest) (RegisterResponse, error) {
	validJoin, err := s.joins.Verify(ctx, request.JoinToken)
	if err != nil {
		return RegisterResponse{}, err
	}
	if !validJoin {
		return RegisterResponse{}, ErrJoinTokenInvalid
	}
	request.NodeName = strings.TrimSpace(request.NodeName)
	if err := validateRegistration(request); err != nil {
		return RegisterResponse{}, err
	}
	key, _ := wgtypes.ParseKey(request.WGPublicKey)
	canonicalKey := key.String()
	plainToken, tokenHash, err := newNodeToken()
	if err != nil {
		return RegisterResponse{}, err
	}

	existing, err := s.store.GetNode(ctx, request.NodeID)
	if err == nil {
		if existing.Type != request.NodeType || existing.WGPublicKey != canonicalKey {
			return RegisterResponse{}, ErrNodeConflict
		}
		if err := s.reconcileNodeBootstrap(ctx, existing.ID); err != nil {
			return RegisterResponse{}, err
		}
		if err := s.peers.EnsurePeer(ctx, canonicalKey, existing.OverlayIP); err != nil {
			return RegisterResponse{}, fmt.Errorf("ensure existing WireGuard peer: %w", err)
		}
		existing.Name = request.NodeName
		existing.NodeTokenHash = tokenHash
		existing.Version = request.Version
		existing.OSVersion = request.OSVersion
		if err := s.store.UpdateNodeRegistration(ctx, existing); err != nil {
			return RegisterResponse{}, err
		}
		return RegisterResponse{NodeToken: plainToken, Network: s.network(existing.OverlayIP)}, nil
	}
	if !errors.Is(err, database.ErrNodeNotFound) {
		return RegisterResponse{}, err
	}

	s.mu.RLock()
	ipamManager := s.ipam
	s.mu.RUnlock()
	node, created, err := ipamManager.ReserveNode(ctx, model.Node{
		ID: request.NodeID, Type: request.NodeType, Name: request.NodeName,
		WGPublicKey: canonicalKey, NodeTokenHash: tokenHash, Status: model.NodeOffline,
		Version: request.Version, OSVersion: request.OSVersion,
	})
	if err != nil {
		return RegisterResponse{}, err
	}
	if !created {
		return RegisterResponse{}, ErrNodeConflict
	}
	if err := s.peers.EnsurePeer(ctx, canonicalKey, node.OverlayIP); err != nil {
		if rollbackErr := ipamManager.ReleaseNode(ctx, node.ID); rollbackErr != nil {
			return RegisterResponse{}, fmt.Errorf("ensure WireGuard peer: %w (registration rollback failed: %v)", err, rollbackErr)
		}
		return RegisterResponse{}, fmt.Errorf("ensure WireGuard peer: %w", err)
	}
	return RegisterResponse{NodeToken: plainToken, Network: s.network(node.OverlayIP)}, nil
}

// Config authenticates an enrolled Node and returns the authoritative latest configuration.
func (s *Service) Config(ctx context.Context, request ConfigRequest) (ConfigResponse, error) {
	node, err := s.AuthenticateNode(ctx, request.NodeID, request.NodeToken)
	if err != nil {
		return ConfigResponse{}, err
	}
	if err := s.reconcileNodeBootstrap(ctx, node.ID); err != nil {
		return ConfigResponse{}, err
	}
	if err := s.peers.EnsurePeer(ctx, node.WGPublicKey, node.OverlayIP); err != nil {
		return ConfigResponse{}, fmt.Errorf("ensure WireGuard peer: %w", err)
	}
	return ConfigResponse{Network: s.network(node.OverlayIP)}, nil
}

func (s *Service) reconcileNodeBootstrap(ctx context.Context, nodeID string) error {
	s.mu.RLock()
	handler := s.onNodeBootstrap
	s.mu.RUnlock()
	if handler == nil {
		return nil
	}
	if err := handler(ctx, nodeID); err != nil {
		return fmt.Errorf("reconcile Node runtime before Bootstrap: %w", err)
	}
	return nil
}

// AuthenticateNode verifies application identity for Bootstrap and Control.
func (s *Service) AuthenticateNode(ctx context.Context, nodeID, nodeToken string) (model.Node, error) {
	node, err := s.store.GetNode(ctx, nodeID)
	if err != nil {
		if errors.Is(err, database.ErrNodeNotFound) {
			return model.Node{}, ErrNodeAuthFailed
		}
		return model.Node{}, err
	}
	if !nodeTokenMatches(node.NodeTokenHash, nodeToken) {
		return model.Node{}, ErrNodeAuthFailed
	}
	return node, nil
}

func validateRegistration(request RegisterRequest) error {
	if _, err := uuid.Parse(request.NodeID); err != nil {
		return fmt.Errorf("node_id must be a UUID: %w", err)
	}
	if !request.NodeType.Valid() {
		return fmt.Errorf("invalid node_type %q", request.NodeType)
	}
	if request.NodeName == "" || len(request.NodeName) > 128 {
		return errors.New("node_name must contain 1 to 128 bytes")
	}
	if _, err := wgtypes.ParseKey(request.WGPublicKey); err != nil {
		return fmt.Errorf("wg_public_key must be a WireGuard key: %w", err)
	}
	return nil
}

func (s *Service) network(nodeIP netip.Addr) NetworkConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.networkLocked(nodeIP)
}

func (s *Service) networkLocked(nodeIP netip.Addr) NetworkConfig {
	return NetworkConfig{
		ConfigVersion: s.config.ConfigVersion, OverlayCIDR: s.config.OverlayCIDR.String(),
		OverlayIP: nodeIP.String(), ServerOverlayIP: s.config.ServerOverlayIP.String(),
		ServerWGPublicKey: s.config.WGPublicKey, ServerWGEndpoint: s.config.WGEndpoint,
		ControlURL: s.config.ControlURL, SessionUDPPort: s.config.SessionUDPPort, MTU: s.config.MTU,
	}
}

// NetworkSnapshot returns the current public configuration without a Node IP.
func (s *Service) NetworkSnapshot() ServiceConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

// UpdateNetwork publishes a completed Server network migration to Bootstrap.
func (s *Service) UpdateNetwork(config ServiceConfig) error {
	if err := validateServiceConfig(config); err != nil {
		return fmt.Errorf("invalid updated Bootstrap network: %w", err)
	}
	s.mu.Lock()
	s.config = config
	s.mu.Unlock()
	return nil
}

func validateServiceConfig(config ServiceConfig) error {
	if _, err := uuid.Parse(config.ServerID); err != nil {
		return fmt.Errorf("invalid Server ID: %w", err)
	}
	if strings.TrimSpace(config.Version) == "" {
		return errors.New("Bootstrap Server version is required")
	}
	if _, err := wgtypes.ParseKey(config.WGPublicKey); err != nil {
		return fmt.Errorf("invalid Server WireGuard public key: %w", err)
	}
	if !config.OverlayCIDR.IsValid() || !config.OverlayCIDR.Addr().Is4() || config.OverlayCIDR != config.OverlayCIDR.Masked() || config.OverlayCIDR.Bits() == 0 || config.OverlayCIDR.Bits() > 30 {
		return errors.New("Bootstrap overlay CIDR must be a canonical IPv4 prefix with usable hosts")
	}
	if !usableOverlayAddress(config.OverlayCIDR, config.ServerOverlayIP) {
		return errors.New("Bootstrap Server overlay address must be usable inside overlay CIDR")
	}
	wgHost, wgPortText, err := net.SplitHostPort(config.WGEndpoint)
	if err != nil || strings.TrimSpace(wgHost) == "" {
		return errors.New("Bootstrap WireGuard endpoint must be host:port")
	}
	wgPort, err := strconv.Atoi(wgPortText)
	if err != nil || wgPort < 1 || wgPort > 65535 {
		return errors.New("Bootstrap WireGuard endpoint port is invalid")
	}
	controlURL, err := url.Parse(config.ControlURL)
	if err != nil || (controlURL.Scheme != "ws" && controlURL.Scheme != "wss") || controlURL.Path != "/control" ||
		controlURL.Opaque != "" || controlURL.User != nil || controlURL.RawQuery != "" || controlURL.ForceQuery || controlURL.Fragment != "" {
		return errors.New("Bootstrap Control URL must be an absolute ws(s) /control URL")
	}
	controlHost, controlPortText, err := net.SplitHostPort(controlURL.Host)
	if err != nil || controlHost != config.ServerOverlayIP.String() {
		return errors.New("Bootstrap Control URL must target the Server overlay IP")
	}
	controlPort, err := strconv.Atoi(controlPortText)
	if err != nil || controlPort < 1 || controlPort > 65535 {
		return errors.New("Bootstrap Control URL port is invalid")
	}
	if config.SessionUDPPort < 1 || config.SessionUDPPort > 65535 || config.MTU < 576 || config.MTU > 65535 || config.ConfigVersion == 0 {
		return errors.New("Bootstrap Session UDP port, MTU, and config version must be valid")
	}
	return nil
}
