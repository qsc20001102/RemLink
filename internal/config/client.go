package config

import (
	"fmt"
	"net/url"
	"strings"
)

const (
	DefaultSiteTCPFlowLimit   = 2048
	DefaultSiteUDPFlowLimit   = 4096
	DefaultSiteUDPIdleSeconds = 60
)

// EngineerConfig contains portable Engineer settings. JoinToken is an optional
// plaintext first-registration convenience; durable Node secrets stay in DPAPI.
type EngineerConfig struct {
	Server    string `yaml:"server"`
	NodeName  string `yaml:"node_name"`
	JoinToken string `yaml:"join_token,omitempty"`
}

// SiteConfig contains portable Site settings. JoinToken has the same
// first-registration-only semantics as EngineerConfig.JoinToken.
type SiteConfig struct {
	Server    string             `yaml:"server"`
	NodeName  string             `yaml:"node_name"`
	JoinToken string             `yaml:"join_token,omitempty"`
	Netstack  SiteNetstackConfig `yaml:"netstack"`
}

type SiteNetstackConfig struct {
	TCPFlowLimit   int `yaml:"tcp_flow_limit"`
	UDPFlowLimit   int `yaml:"udp_flow_limit"`
	UDPIdleSeconds int `yaml:"udp_idle_seconds"`
}

// LoadEngineer loads and validates a strict Engineer YAML file.
func LoadEngineer(path string) (EngineerConfig, error) {
	var config EngineerConfig
	if err := decodeStrict(path, &config); err != nil {
		return EngineerConfig{}, err
	}
	if err := validateClient(config.Server, config.NodeName); err != nil {
		return EngineerConfig{}, fmt.Errorf("validate engineer config %q: %w", path, err)
	}
	config.JoinToken = strings.TrimSpace(config.JoinToken)
	return config, nil
}

// LoadSite loads and validates a strict Site YAML file.
func LoadSite(path string) (SiteConfig, error) {
	config := SiteConfig{Netstack: SiteNetstackConfig{
		TCPFlowLimit: DefaultSiteTCPFlowLimit, UDPFlowLimit: DefaultSiteUDPFlowLimit,
		UDPIdleSeconds: DefaultSiteUDPIdleSeconds,
	}}
	if err := decodeStrict(path, &config); err != nil {
		return SiteConfig{}, err
	}
	if err := validateClient(config.Server, config.NodeName); err != nil {
		return SiteConfig{}, fmt.Errorf("validate site config %q: %w", path, err)
	}
	config.JoinToken = strings.TrimSpace(config.JoinToken)
	if config.Netstack.TCPFlowLimit < 1 || config.Netstack.UDPFlowLimit < 1 || config.Netstack.UDPIdleSeconds < 1 || config.Netstack.UDPIdleSeconds > 86400 {
		return SiteConfig{}, fmt.Errorf("validate site config %q: netstack limits must be positive and udp_idle_seconds must be within 1..86400", path)
	}
	return config, nil
}

// ResolveJoinToken applies the documented precedence. override is populated by
// the -join-token flag, whose default is REMLINK_JOIN_TOKEN.
func ResolveJoinToken(override, configured string) string {
	if value := strings.TrimSpace(override); value != "" {
		return value
	}
	return strings.TrimSpace(configured)
}

func validateClient(server, nodeName string) error {
	if strings.TrimSpace(nodeName) == "" {
		return fmt.Errorf("node_name must not be empty")
	}
	return ValidateServerURL(server)
}

// ValidateServerURL applies the single public Server URL trust boundary used
// by YAML loading, persisted Node identities, and the Bootstrap client.
func ValidateServerURL(server string) error {
	parsed, err := url.Parse(server)
	if err != nil || parsed.Host == "" || parsed.Opaque != "" {
		return fmt.Errorf("server must be an absolute HTTP URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("server URL scheme must be http or https")
	}
	if parsed.User != nil {
		return fmt.Errorf("server URL must not contain credentials")
	}
	if parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return fmt.Errorf("server URL must not contain a query or fragment")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return fmt.Errorf("server URL must not contain a path")
	}
	return nil
}
