package clientwg

import (
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestConfigUAPIGolden(t *testing.T) {
	t.Parallel()
	privateKey := Key{1, 2, 3}
	serverKey := Key{4, 5, 6}
	config := Config{
		PrivateKey:          privateKey,
		ServerPublicKey:     serverKey,
		ServerEndpoint:      "203.0.113.10:51820",
		OverlayAllowedIPs:   []netip.Prefix{netip.MustParsePrefix("10.88.0.0/16")},
		PersistentKeepalive: 25 * time.Second,
	}
	got, err := config.uapi()
	if err != nil {
		t.Fatalf("uapi() error = %v", err)
	}
	checks := []string{
		"private_key=0102030000000000000000000000000000000000000000000000000000000000\n",
		"listen_port=0\n",
		"replace_peers=true\n",
		"public_key=0405060000000000000000000000000000000000000000000000000000000000\n",
		"endpoint=203.0.113.10:51820\n",
		"persistent_keepalive_interval=25\n",
		"replace_allowed_ips=true\n",
		"allowed_ip=10.88.0.0/16\n",
	}
	for _, check := range checks {
		if !strings.Contains(got, check) {
			t.Fatalf("uapi() missing %q in:\n%s", check, got)
		}
	}
	if !strings.HasSuffix(got, "\n\n") {
		t.Fatalf("uapi() must terminate with a blank line: %q", got)
	}
}

func TestConfigRejectsNonV1Values(t *testing.T) {
	t.Parallel()
	valid := Config{
		PrivateKey:          Key{1},
		ServerPublicKey:     Key{2},
		ServerEndpoint:      "203.0.113.10:51820",
		OverlayAllowedIPs:   []netip.Prefix{netip.MustParsePrefix("10.88.0.0/16")},
		PersistentKeepalive: 25 * time.Second,
	}
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "zero private key", mutate: func(c *Config) { c.PrivateKey = Key{} }},
		{name: "zero public key", mutate: func(c *Config) { c.ServerPublicKey = Key{} }},
		{name: "same keys", mutate: func(c *Config) { c.ServerPublicKey = c.PrivateKey }},
		{name: "endpoint", mutate: func(c *Config) { c.ServerEndpoint = "203.0.113.10" }},
		{name: "endpoint port", mutate: func(c *Config) { c.ServerEndpoint = "203.0.113.10:not-a-port" }},
		{name: "no AllowedIPs", mutate: func(c *Config) { c.OverlayAllowedIPs = nil }},
		{name: "multiple AllowedIPs", mutate: func(c *Config) {
			c.OverlayAllowedIPs = []netip.Prefix{netip.MustParsePrefix("10.88.0.0/16"), netip.MustParsePrefix("192.168.13.0/24")}
		}},
		{name: "IPv6", mutate: func(c *Config) { c.OverlayAllowedIPs = []netip.Prefix{netip.MustParsePrefix("fd00::/64")} }},
		{name: "Exit Node", mutate: func(c *Config) { c.OverlayAllowedIPs = []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0")} }},
		{name: "unmasked", mutate: func(c *Config) { c.OverlayAllowedIPs = []netip.Prefix{netip.MustParsePrefix("10.88.0.1/16")} }},
		{name: "fractional keepalive", mutate: func(c *Config) { c.PersistentKeepalive = 1500 * time.Millisecond }},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := valid
			test.mutate(&config)
			if _, err := config.uapi(); err == nil {
				t.Fatal("uapi() unexpectedly accepted invalid config")
			}
		})
	}
}
