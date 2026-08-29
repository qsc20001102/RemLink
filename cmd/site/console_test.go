package main

import (
	"bytes"
	"net/netip"
	"strings"
	"testing"

	"remlink/internal/model"
)

func TestSiteConsoleShowsRequiredStatusAndEvents(t *testing.T) {
	var output bytes.Buffer
	console := newSiteConsole(&output)
	console.Header("https://server.example", "Site-A", "1.0.0")
	console.OverlayReady(netip.MustParseAddr("10.88.0.20"))
	console.ControlState(true)
	console.SiteReady()
	console.Session(model.SessionActive, 42, "")
	console.Route(42, netip.MustParsePrefix("192.168.13.0/24"), "DIRECT")
	text := output.String()
	for _, wanted := range []string{"服务器=", "节点=", "OverlayIP=", "WireGuard=", "Control=", "远程网段=", "子网网关=", "[会话/SESSION]", "活动中（ACTIVE）", "[路由/ROUTE]", "直连路由（DIRECT）"} {
		if !strings.Contains(text, wanted) {
			t.Fatalf("console output missing %q: %s", wanted, text)
		}
	}
}
