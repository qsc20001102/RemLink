package route

import (
	"net/netip"
	"testing"
)

func TestConflictsIgnoresDefaultAndRemLink(t *testing.T) {
	entries := []Entry{
		{InterfaceLUID: 1, Destination: netip.MustParsePrefix("0.0.0.0/0"), NextHop: netip.MustParseAddr("192.0.2.1")},
		{InterfaceLUID: 2, Destination: netip.MustParsePrefix("192.168.0.0/16")},
		{InterfaceLUID: 99, Destination: netip.MustParsePrefix("192.168.13.0/24")},
	}
	conflicts := ConflictsFrom(entries, netip.MustParsePrefix("192.168.13.0/24"), 99)
	if len(conflicts) != 1 || conflicts[0].Destination.String() != "192.168.0.0/16" {
		t.Fatalf("conflicts = %+v", conflicts)
	}
}

func TestLookupClassifications(t *testing.T) {
	overlay := netip.MustParsePrefix("10.88.0.0/16")
	entries := []Entry{
		{InterfaceLUID: 1, Destination: netip.MustParsePrefix("0.0.0.0/0"), NextHop: netip.MustParseAddr("192.0.2.1")},
		{InterfaceLUID: 2, Destination: netip.MustParsePrefix("192.168.13.0/24"), NextHop: netip.IPv4Unspecified()},
		{InterfaceLUID: 3, Destination: netip.MustParsePrefix("172.16.0.0/16"), NextHop: netip.MustParseAddr("192.0.2.254")},
	}
	for _, test := range []struct {
		target string
		want   LookupResult
	}{
		{"10.88.0.5", LookupOverlayConflict},
		{"192.168.13.10", LookupDirect},
		{"172.16.4.2", LookupRouted},
		{"8.8.8.8", LookupDefaultOnly},
	} {
		if got := LookupFrom(entries, netip.MustParseAddr(test.target), overlay, 99); got != test.want {
			t.Errorf("LookupFrom(%s) = %s, want %s", test.target, got, test.want)
		}
	}
	if got := LookupFrom(nil, netip.MustParseAddr("192.168.1.2"), overlay, 99); got != LookupNoRoute {
		t.Fatalf("empty route lookup = %s", got)
	}
}

func TestValidateRemote(t *testing.T) {
	overlay := netip.MustParsePrefix("10.88.0.0/16")
	for _, invalid := range []string{"0.0.0.0/0", "10.88.5.0/24"} {
		if err := validateRemote(netip.MustParsePrefix(invalid), overlay); err == nil {
			t.Errorf("validateRemote accepted %s", invalid)
		}
	}
	if err := validateRemote(netip.MustParsePrefix("192.168.13.0/24"), overlay); err != nil {
		t.Fatal(err)
	}
}
