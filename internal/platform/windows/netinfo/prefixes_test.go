package netinfo

import (
	"net/netip"
	"testing"
)

func TestPrefixesOverlap(t *testing.T) {
	for _, test := range []struct {
		left, right string
		want        bool
	}{
		{"192.168.0.0/16", "192.168.13.0/24", true},
		{"192.168.13.0/24", "192.168.13.10/32", true},
		{"10.88.0.0/16", "10.89.0.0/16", false},
		{"0.0.0.0/0", "10.88.0.0/16", true},
	} {
		got := PrefixesOverlap(netip.MustParsePrefix(test.left), netip.MustParsePrefix(test.right))
		if got != test.want {
			t.Errorf("PrefixesOverlap(%s, %s) = %v, want %v", test.left, test.right, got, test.want)
		}
	}
}
