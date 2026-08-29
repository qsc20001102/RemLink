//go:build windows

package windowsplatform

import (
	"net/netip"
	"testing"
)

func TestValidateAdapterConfig(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		address string
		mtu     int
		wantErr bool
	}{
		{name: "valid", address: "10.88.0.2/16", mtu: 1280},
		{name: "IPv6", address: "fd00::2/64", mtu: 1280, wantErr: true},
		{name: "network", address: "10.88.0.0/16", mtu: 1280, wantErr: true},
		{name: "broadcast", address: "10.88.255.255/16", mtu: 1280, wantErr: true},
		{name: "small MTU", address: "10.88.0.2/16", mtu: 575, wantErr: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			prefix := netip.MustParsePrefix(test.address)
			err := validateAdapterConfig(AdapterConfig{Address: prefix, MTU: test.mtu})
			if (err != nil) != test.wantErr {
				t.Fatalf("validateAdapterConfig() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}
