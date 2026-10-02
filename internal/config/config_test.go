package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadServerUsesDefaultsAndStrictFields(t *testing.T) {
	t.Parallel()
	path := writeConfig(t, "data:\n  directory: ./custom-data\n")
	config, err := LoadServer(path)
	if err != nil {
		t.Fatalf("LoadServer() error = %v", err)
	}
	if config.Data.Directory != "./custom-data" {
		t.Fatalf("Data.Directory = %q, want ./custom-data", config.Data.Directory)
	}
	if config.Network.OverlayCIDR != DefaultOverlayCIDR {
		t.Fatalf("OverlayCIDR = %q, want %q", config.Network.OverlayCIDR, DefaultOverlayCIDR)
	}

	unknown := writeConfig(t, "node_token: must-not-be-in-yaml\n")
	if _, err := LoadServer(unknown); err == nil || !strings.Contains(err.Error(), "field node_token not found") {
		t.Fatalf("LoadServer() unknown-field error = %v", err)
	}
}

func TestServerRuntimeSettingsComeFromYAML(t *testing.T) {
	t.Setenv("REMLINK_WG_ENDPOINT", "ignored.example:1234")
	t.Setenv("REMLINK_ADMIN_TOKEN", "ignored-environment-token")
	path := writeConfig(t, "server:\n  wg_endpoint: vpn.example:51820\n  admin_token: yaml-test-token\n")
	c, err := LoadServer(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Server.WGEndpoint != "vpn.example:51820" || c.Server.AdminToken != "yaml-test-token" {
		t.Fatal("YAML settings were not preserved")
	}
}

func TestServerConfigValidation(t *testing.T) {
	t.Parallel()
	config := DefaultServerConfig()
	config.Server.ControlListen = "10.88.0.2:7001"
	if err := config.Validate(); err == nil || !strings.Contains(err.Error(), "must bind") {
		t.Fatalf("Validate() error = %v, want Control listen mismatch", err)
	}

	config = DefaultServerConfig()
	config.Network.OverlayCIDR = "10.88.0.1/16"
	if err := config.Validate(); err == nil || !strings.Contains(err.Error(), "network address") {
		t.Fatalf("Validate() error = %v, want unmasked CIDR error", err)
	}

	config = DefaultServerConfig()
	config.Network.OverlayCIDR = "0.0.0.0/0"
	config.Network.ServerOverlayIP = "10.88.0.1"
	config.Server.ControlListen = "10.88.0.1:7001"
	if err := config.Validate(); err == nil || !strings.Contains(err.Error(), "Exit Node") {
		t.Fatalf("Validate() /0 error = %v", err)
	}
}

func TestLoadEngineerAndSite(t *testing.T) {
	t.Parallel()
	valid := writeConfig(t, "server: http://127.0.0.1:8080\nnode_name: test-node\njoin_token: '  yaml-token  '\n")
	engineer, err := LoadEngineer(valid)
	if err != nil {
		t.Fatalf("LoadEngineer() error = %v", err)
	}
	if engineer.NodeName != "test-node" || engineer.JoinToken != "yaml-token" {
		t.Fatalf("Engineer config = %+v", engineer)
	}
	site, err := LoadSite(valid)
	if err != nil {
		t.Fatalf("LoadSite() error = %v", err)
	}
	if site.JoinToken != "yaml-token" || site.Netstack.TCPFlowLimit != 2048 || site.Netstack.UDPFlowLimit != 4096 || site.Netstack.UDPIdleSeconds != 60 {
		t.Fatalf("Site netstack defaults = %+v", site.Netstack)
	}
	customSite := writeConfig(t, "server: http://127.0.0.1:8080\nnode_name: site\nnetstack:\n  tcp_flow_limit: 32\n  udp_flow_limit: 64\n  udp_idle_seconds: 15\n")
	site, err = LoadSite(customSite)
	if err != nil || site.Netstack.TCPFlowLimit != 32 || site.Netstack.UDPFlowLimit != 64 || site.Netstack.UDPIdleSeconds != 15 {
		t.Fatalf("custom Site netstack = %+v, %v", site.Netstack, err)
	}
	invalidSite := writeConfig(t, "server: http://127.0.0.1:8080\nnode_name: site\nnetstack:\n  udp_idle_seconds: 0\n")
	if _, err := LoadSite(invalidSite); err == nil {
		t.Fatal("LoadSite accepted zero UDP idle timeout")
	}

	sensitive := writeConfig(t, "server: http://127.0.0.1:8080\nnode_name: test-node\nwg_private_key: secret\n")
	if _, err := LoadEngineer(sensitive); err == nil || !strings.Contains(err.Error(), "field wg_private_key not found") {
		t.Fatalf("LoadEngineer() sensitive-field error = %v", err)
	}
}

func TestResolveJoinTokenPrecedence(t *testing.T) {
	t.Parallel()
	if got := ResolveJoinToken(" cli-or-env ", "yaml"); got != "cli-or-env" {
		t.Fatalf("ResolveJoinToken override = %q", got)
	}
	if got := ResolveJoinToken("", " yaml "); got != "yaml" {
		t.Fatalf("ResolveJoinToken YAML fallback = %q", got)
	}
	if got := ResolveJoinToken("  ", "  "); got != "" {
		t.Fatalf("ResolveJoinToken empty = %q", got)
	}
}

func TestValidateServerURLRejectsAmbiguousAuthority(t *testing.T) {
	t.Parallel()
	invalid := []string{
		"ftp://example.test", "http://user:secret@example.test", "http://example.test/api",
		"http://example.test?", "http://example.test?x=1", "http://example.test#fragment", "not-a-url",
	}
	for _, raw := range invalid {
		if err := ValidateServerURL(raw); err == nil {
			t.Errorf("ValidateServerURL(%q) accepted an unsafe or ambiguous URL", raw)
		}
	}
	for _, raw := range []string{"http://127.0.0.1:8080", "https://example.test/"} {
		if err := ValidateServerURL(raw); err != nil {
			t.Errorf("ValidateServerURL(%q) error = %v", raw, err)
		}
	}
}

func TestConfigRejectsMultipleDocuments(t *testing.T) {
	t.Parallel()
	path := writeConfig(t, "server: http://127.0.0.1:8080\nnode_name: one\n---\nserver: http://127.0.0.1:8080\nnode_name: two\n")
	if _, err := LoadSite(path); err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("LoadSite() error = %v, want multiple-document error", err)
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
