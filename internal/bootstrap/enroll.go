package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"remlink/internal/identity"
	"remlink/internal/model"
)

// EnrollConfig contains first-run inputs which are deliberately not persisted
// as plaintext secrets. JoinToken is used only if identity has no NodeToken.
type EnrollConfig struct {
	NodeType  model.NodeType
	NodeName  string
	ServerURL string
	JoinToken string
	Version   string
	OSVersion string
}

// Enroll loads or creates an identity, calls register/config, validates the
// authoritative NetworkConfig, and persists the latest token/version.
func Enroll(ctx context.Context, store *identity.Store, client *Client, config EnrollConfig) (identity.Identity, NetworkConfig, error) {
	current, err := store.Load()
	if errors.Is(err, os.ErrNotExist) {
		current, err = identity.New(config.NodeType, config.NodeName, config.ServerURL)
		if err != nil {
			return identity.Identity{}, NetworkConfig{}, err
		}
		if err := store.Save(current); err != nil {
			return identity.Identity{}, NetworkConfig{}, err
		}
	} else if err != nil {
		return identity.Identity{}, NetworkConfig{}, err
	}
	if current.NodeType != config.NodeType {
		return identity.Identity{}, NetworkConfig{}, errors.New("persisted Node type does not match this executable")
	}
	configuredServerURL := strings.TrimRight(strings.TrimSpace(config.ServerURL), "/")
	if configuredServerURL != current.ServerURL {
		// A first launch may create identity.json before the operator has filled
		// in the real Server URL and Join Token. Until registration succeeds the
		// identity has no server-side credentials or owned routes, so rebinding
		// that local draft identity is safe and makes portable packages editable.
		if current.NodeToken == "" && current.ConfigVersion == 0 && len(current.OwnedRoutes) == 0 {
			current.ServerURL = configuredServerURL
			if err := store.Save(current); err != nil {
				return identity.Identity{}, NetworkConfig{}, fmt.Errorf("update unregistered Node identity Server URL: %w", err)
			}
		} else {
			return identity.Identity{}, NetworkConfig{}, errors.New("配置的 Server URL 与已注册的 Node 身份不一致；如需切换 Server，请退出程序后删除 identity.json 并重新注册")
		}
	}
	current.NodeName = strings.TrimSpace(config.NodeName)
	var network NetworkConfig
	if current.NodeToken == "" {
		if config.JoinToken == "" {
			return identity.Identity{}, NetworkConfig{}, errors.New("Join Token is required for first registration")
		}
		response, err := client.Register(ctx, RegisterRequest{
			JoinToken: config.JoinToken, NodeID: current.NodeID, NodeType: current.NodeType,
			NodeName: current.NodeName, WGPublicKey: current.PublicKey(),
			Version: config.Version, OSVersion: config.OSVersion,
		})
		if err != nil {
			return identity.Identity{}, NetworkConfig{}, err
		}
		current.NodeToken = response.NodeToken
		network = response.Network
	} else {
		response, err := client.Config(ctx, ConfigRequest{NodeID: current.NodeID, NodeToken: current.NodeToken})
		if err != nil {
			return identity.Identity{}, NetworkConfig{}, err
		}
		network = response.Network
	}
	if err := ValidateNetworkConfig(network); err != nil {
		return identity.Identity{}, NetworkConfig{}, err
	}
	current.ConfigVersion = network.ConfigVersion
	if err := store.Save(current); err != nil {
		return identity.Identity{}, NetworkConfig{}, err
	}
	return current, network, nil
}

// ValidateNetworkConfig rejects malformed or internally inconsistent Server data.
func ValidateNetworkConfig(config NetworkConfig) error {
	prefix, err := netip.ParsePrefix(config.OverlayCIDR)
	if err != nil || !prefix.Addr().Is4() || prefix != prefix.Masked() || prefix.Bits() == 0 || prefix.Bits() > 30 {
		return errors.New("NetworkConfig overlay_cidr must be a canonical IPv4 prefix")
	}
	nodeIP, err := netip.ParseAddr(config.OverlayIP)
	if err != nil || !usableOverlayAddress(prefix, nodeIP) {
		return errors.New("NetworkConfig overlay_ip must belong to overlay_cidr")
	}
	serverIP, err := netip.ParseAddr(config.ServerOverlayIP)
	if err != nil || !usableOverlayAddress(prefix, serverIP) || serverIP == nodeIP {
		return errors.New("NetworkConfig server_overlay_ip must be a distinct address in overlay_cidr")
	}
	if _, err := wgtypes.ParseKey(config.ServerWGPublicKey); err != nil {
		return fmt.Errorf("NetworkConfig Server WireGuard key: %w", err)
	}
	if config.ServerWGEndpoint == "" || config.ControlURL == "" {
		return errors.New("NetworkConfig endpoints must not be empty")
	}
	wgHost, wgPortText, err := net.SplitHostPort(config.ServerWGEndpoint)
	if err != nil || strings.TrimSpace(wgHost) == "" {
		return errors.New("NetworkConfig server_wg_endpoint must be host:port")
	}
	wgPort, err := strconv.Atoi(wgPortText)
	if err != nil || wgPort < 1 || wgPort > 65535 {
		return errors.New("NetworkConfig server_wg_endpoint port is invalid")
	}
	controlURL, err := url.Parse(config.ControlURL)
	if err != nil || (controlURL.Scheme != "ws" && controlURL.Scheme != "wss") || controlURL.Path != "/control" ||
		controlURL.Opaque != "" || controlURL.User != nil || controlURL.RawQuery != "" || controlURL.ForceQuery || controlURL.Fragment != "" {
		return errors.New("NetworkConfig control_url must be an absolute ws(s) /control URL")
	}
	controlHost, controlPortText, err := net.SplitHostPort(controlURL.Host)
	if err != nil || controlHost != serverIP.String() {
		return errors.New("NetworkConfig control_url must target server_overlay_ip with an explicit port")
	}
	controlPort, err := strconv.Atoi(controlPortText)
	if err != nil || controlPort < 1 || controlPort > 65535 {
		return errors.New("NetworkConfig control_url port is invalid")
	}
	if config.ConfigVersion == 0 || config.SessionUDPPort < 1 || config.SessionUDPPort > 65535 || config.MTU < 576 || config.MTU > 65535 {
		return errors.New("NetworkConfig version, UDP port, or MTU is invalid")
	}
	return nil
}

// CurrentOSVersion is a non-secret Bootstrap capability label.
func CurrentOSVersion() string { return runtime.GOOS + "/" + runtime.GOARCH }
