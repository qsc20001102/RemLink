package session

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"remlink/internal/overlay/clientwg"
	"remlink/internal/platform/windows/route"
	"remlink/internal/protocol"
	"remlink/internal/subnetgateway"
)

func TestEngineerRuntimePreflightConfigureActivateStop(t *testing.T) {
	port := availableUDPPort(t, "127.0.0.1")
	routes := &fakeRoutes{}
	device := &fakePacketDevice{}
	control := &fakeNodeControl{}
	runtime, err := NewEngineerRuntime(context.Background(), EngineerConfig{
		LocalOverlayIP: netip.MustParseAddr("127.0.0.1"), OverlayCIDR: netip.MustParsePrefix("127.0.0.0/8"),
		UDPPort: port, StatsInterval: time.Hour, Routes: routes, Device: device, Control: control,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	requestID, err := runtime.CreateSession(context.Background(), "site", []string{"192.168.13.0/24"})
	if err != nil || requestID == "" || control.last(t, protocol.ControlCreateSession).requestID != requestID {
		t.Fatalf("CreateSession = %q, %v", requestID, err)
	}
	if err := runtime.HandleControl(context.Background(), envelope(t, protocol.ControlSessionConfig, requestID, protocol.SessionConfigPayload{
		SessionID: 42, PeerOverlayIP: "127.0.0.2", CIDRs: []string{"192.168.13.0/24"}, MTU: 1280, UDPPort: port,
	})); err != nil {
		t.Fatal(err)
	}
	if len(routes.added) != 1 || device.router == nil || control.last(t, protocol.ControlRoutesReady).payload.(protocol.RoutesReadyPayload).SessionID != 42 {
		t.Fatalf("Engineer READY wiring routes=%v router=%v", routes.added, device.router)
	}
	if err := runtime.HandleControl(context.Background(), envelope(t, protocol.ControlSessionActive, "", protocol.SessionActivePayload{SessionID: 42})); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Disconnect(context.Background(), "operator"); err != nil {
		t.Fatal(err)
	}
	if got := control.last(t, protocol.ControlStopSession).payload.(protocol.StopSessionPayload); got.SessionID != 42 {
		t.Fatalf("Disconnect payload = %+v", got)
	}
	if err := runtime.HandleControl(context.Background(), envelope(t, protocol.ControlStopSession, "", protocol.StopSessionPayload{SessionID: 42, Reason: "operator"})); err != nil {
		t.Fatal(err)
	}
	if device.router != nil || len(routes.removed) != 1 {
		t.Fatalf("Engineer cleanup router=%v removed=%v", device.router, routes.removed)
	}
}

func TestEngineerRuntimeRejectsLocalConflictBeforeControl(t *testing.T) {
	port := availableUDPPort(t, "127.0.0.1")
	routes := &fakeRoutes{conflict: true}
	control := &fakeNodeControl{}
	runtime, err := NewEngineerRuntime(context.Background(), EngineerConfig{
		LocalOverlayIP: netip.MustParseAddr("127.0.0.1"), OverlayCIDR: netip.MustParsePrefix("127.0.0.0/8"),
		UDPPort: port, Routes: routes, Device: &fakePacketDevice{}, Control: control,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	if err := runtime.PreflightCIDRs([]string{"192.168.13.0/24"}); err == nil {
		t.Fatal("preflight accepted local route conflict")
	}
	if control.count(protocol.ControlCreateSession) != 0 {
		t.Fatal("preflight changed Control state")
	}
	if _, err := runtime.CreateSession(context.Background(), "site", []string{"192.168.13.0/24"}); err == nil {
		t.Fatal("local route conflict was accepted")
	}
	if control.count(protocol.ControlCreateSession) != 0 {
		t.Fatal("CREATE_SESSION was sent despite local conflict")
	}
}

func TestEngineerRuntimePrepareFailureReleasesOnlyMatchingPendingRequest(t *testing.T) {
	port := availableUDPPort(t, "127.0.0.1")
	control := &fakeNodeControl{}
	runtime, err := NewEngineerRuntime(context.Background(), EngineerConfig{
		LocalOverlayIP: netip.MustParseAddr("127.0.0.1"), OverlayCIDR: netip.MustParsePrefix("127.0.0.0/8"),
		UDPPort: port, Routes: &fakeRoutes{}, Device: &fakePacketDevice{}, Control: control,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	requestID, err := runtime.CreateSession(context.Background(), "site", []string{"192.168.13.0/24"})
	if err != nil {
		t.Fatal(err)
	}
	rejection := protocol.StopSessionPayload{SessionID: 81, Reason: string(protocol.ErrorSiteNoRoute)}
	if err := runtime.HandleControl(context.Background(), envelope(t, protocol.ControlStopSession, "stale-request", rejection)); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.CreateSession(context.Background(), "site", []string{"192.168.21.0/24"}); err == nil {
		t.Fatal("stale rejection released the active pending request")
	}
	if err := runtime.HandleControl(context.Background(), envelope(t, protocol.ControlStopSession, requestID, rejection)); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.CreateSession(context.Background(), "site", []string{"192.168.21.0/24"}); err != nil {
		t.Fatalf("matching PREPARE rejection did not release pending state: %v", err)
	}
}

func TestSiteRuntimePrepareActivateAndCleanup(t *testing.T) {
	port := availableUDPPort(t, "127.0.0.2")
	control := &fakeNodeControl{}
	gateway := &fakeGateway{}
	runtime, err := NewSiteRuntime(context.Background(), SiteConfig{
		LocalOverlayIP: netip.MustParseAddr("127.0.0.2"), OverlayCIDR: netip.MustParsePrefix("127.0.0.0/8"),
		UDPPort: port, StatsInterval: time.Hour, Routes: &fakeRoutes{lookup: route.LookupDirect},
		Gateway: gateway, Control: control,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	prepare := protocol.PrepareSessionPayload{SessionID: 77, EngineerOverlayIP: "127.0.0.1", TargetCIDRs: []string{"192.168.13.0/24"}}
	if err := runtime.HandleControl(context.Background(), envelope(t, protocol.ControlPrepareSession, "", prepare)); err != nil {
		t.Fatal(err)
	}
	result := control.last(t, protocol.ControlPrepareResult).payload.(protocol.PrepareResultPayload)
	if !result.OK || result.SubnetGatewayStatus != "netstack" || gateway.prepared != 77 || len(result.RouteResults) != 1 {
		t.Fatalf("PREPARE_RESULT=%+v gateway=%d", result, gateway.prepared)
	}
	if err := runtime.HandleControl(context.Background(), envelope(t, protocol.ControlSessionActive, "", protocol.SessionActivePayload{SessionID: 77})); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Egress(context.Background(), 77, testRuntimeIPv4("192.168.13.10", "127.0.0.1")); err != nil {
		t.Fatal(err)
	}
	if err := runtime.HandleControl(context.Background(), envelope(t, protocol.ControlStopSession, "", protocol.StopSessionPayload{SessionID: 77})); err != nil {
		t.Fatal(err)
	}
	if gateway.closed != 77 {
		t.Fatalf("closed gateway Session = %d", gateway.closed)
	}
}

func TestSiteRuntimeRejectsDefaultOnlyRoute(t *testing.T) {
	port := availableUDPPort(t, "127.0.0.2")
	control := &fakeNodeControl{}
	gateway := &fakeGateway{}
	runtime, err := NewSiteRuntime(context.Background(), SiteConfig{
		LocalOverlayIP: netip.MustParseAddr("127.0.0.2"), OverlayCIDR: netip.MustParsePrefix("127.0.0.0/8"),
		UDPPort: port, StatsInterval: time.Hour, Routes: &fakeRoutes{lookup: route.LookupDefaultOnly},
		Gateway: gateway, Control: control,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	prepare := protocol.PrepareSessionPayload{SessionID: 78, EngineerOverlayIP: "127.0.0.1", TargetCIDRs: []string{"192.168.13.0/24"}}
	if err := runtime.HandleControl(context.Background(), envelope(t, protocol.ControlPrepareSession, "", prepare)); err != nil {
		t.Fatal(err)
	}
	result := control.last(t, protocol.ControlPrepareResult).payload.(protocol.PrepareResultPayload)
	if result.OK || result.ErrorCode != protocol.ErrorSiteNoRoute || gateway.prepared != 0 {
		t.Fatalf("PREPARE_RESULT=%+v gateway=%d", result, gateway.prepared)
	}
}

func TestSiteRuntimeRejectsPrepareAtFlowCapacity(t *testing.T) {
	for _, test := range []struct {
		name     string
		tcpFlows int
		udpFlows int
		tcpLimit int
		udpLimit int
	}{
		{name: "tcp", tcpFlows: 2, tcpLimit: 2, udpLimit: 4},
		{name: "udp", udpFlows: 4, tcpLimit: 2, udpLimit: 4},
	} {
		t.Run(test.name, func(t *testing.T) {
			port := availableUDPPort(t, "127.0.0.2")
			control := &fakeNodeControl{}
			gateway := &fakeGateway{tcpFlows: test.tcpFlows, udpFlows: test.udpFlows}
			runtime, err := NewSiteRuntime(context.Background(), SiteConfig{
				LocalOverlayIP: netip.MustParseAddr("127.0.0.2"), OverlayCIDR: netip.MustParsePrefix("127.0.0.0/8"),
				UDPPort: port, TCPFlowLimit: test.tcpLimit, UDPFlowLimit: test.udpLimit, StatsInterval: time.Hour,
				Routes: &fakeRoutes{lookup: route.LookupDirect}, Gateway: gateway, Control: control,
			})
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Close()
			prepare := protocol.PrepareSessionPayload{SessionID: 79, EngineerOverlayIP: "127.0.0.1", TargetCIDRs: []string{"192.168.13.0/24"}}
			if err := runtime.HandleControl(context.Background(), envelope(t, protocol.ControlPrepareSession, "", prepare)); err != nil {
				t.Fatal(err)
			}
			result := control.last(t, protocol.ControlPrepareResult).payload.(protocol.PrepareResultPayload)
			if result.OK || result.ErrorCode != protocol.ErrorFlowLimitReached || gateway.prepared != 0 {
				t.Fatalf("PREPARE_RESULT=%+v gateway=%d", result, gateway.prepared)
			}
		})
	}
}

func TestEngineerRuntimeInjectionFailureStopsOnlySession(t *testing.T) {
	port := availableUDPPort(t, "127.0.0.1")
	control := &fakeNodeControl{}
	device := &fakePacketDevice{injectErr: errors.New("wintun write failed")}
	runtime, err := NewEngineerRuntime(context.Background(), EngineerConfig{
		LocalOverlayIP: netip.MustParseAddr("127.0.0.1"), OverlayCIDR: netip.MustParsePrefix("127.0.0.0/8"), UDPPort: port,
		StatsInterval: time.Hour, Routes: &fakeRoutes{}, Device: device, Control: control,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	if _, err := runtime.CreateSession(context.Background(), "site", []string{"192.168.13.0/24"}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.HandleControl(context.Background(), envelope(t, protocol.ControlSessionConfig, control.last(t, protocol.ControlCreateSession).requestID, protocol.SessionConfigPayload{
		SessionID: 91, PeerOverlayIP: "127.0.0.2", CIDRs: []string{"192.168.13.0/24"}, MTU: 1280, UDPPort: port,
	})); err != nil {
		t.Fatal(err)
	}
	if err := runtime.inject(context.Background(), 91, testRuntimeIPv4("192.168.13.10", "127.0.0.1")); err == nil {
		t.Fatal("injection failure was hidden")
	}
	stop := control.last(t, protocol.ControlStopSession).payload.(protocol.StopSessionPayload)
	if stop.Reason != string(protocol.ErrorSessionInjectFailed) || runtime.Snapshot().ID != 0 || device.router != nil {
		t.Fatalf("STOP=%+v snapshot=%+v router=%v", stop, runtime.Snapshot(), device.router)
	}
}

func TestSiteRuntimeInjectionFailureClosesGatewaySession(t *testing.T) {
	port := availableUDPPort(t, "127.0.0.2")
	control := &fakeNodeControl{}
	gateway := &fakeGateway{injectErr: errors.New("netstack inject failed")}
	runtime, err := NewSiteRuntime(context.Background(), SiteConfig{
		LocalOverlayIP: netip.MustParseAddr("127.0.0.2"), OverlayCIDR: netip.MustParsePrefix("127.0.0.0/8"), UDPPort: port,
		StatsInterval: time.Hour, Routes: &fakeRoutes{lookup: route.LookupDirect}, Gateway: gateway, Control: control,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	prepare := protocol.PrepareSessionPayload{SessionID: 92, EngineerOverlayIP: "127.0.0.1", TargetCIDRs: []string{"192.168.13.0/24"}}
	if err := runtime.HandleControl(context.Background(), envelope(t, protocol.ControlPrepareSession, "", prepare)); err != nil {
		t.Fatal(err)
	}
	if err := runtime.HandleControl(context.Background(), envelope(t, protocol.ControlSessionActive, "", protocol.SessionActivePayload{SessionID: 92})); err != nil {
		t.Fatal(err)
	}
	if err := runtime.inject(context.Background(), 92, testRuntimeIPv4("127.0.0.1", "192.168.13.10")); err == nil {
		t.Fatal("injection failure was hidden")
	}
	stop := control.last(t, protocol.ControlStopSession).payload.(protocol.StopSessionPayload)
	if stop.Reason != string(protocol.ErrorSessionInjectFailed) || gateway.closed != 92 {
		t.Fatalf("STOP=%+v gateway.closed=%d", stop, gateway.closed)
	}
}

type fakeNodeControl struct {
	mu       sync.Mutex
	messages []nodeMessage
}

type nodeMessage struct {
	messageType protocol.ControlMessageType
	requestID   string
	payload     any
}

func (f *fakeNodeControl) Send(_ context.Context, messageType protocol.ControlMessageType, requestID string, payload any) error {
	f.mu.Lock()
	f.messages = append(f.messages, nodeMessage{messageType: messageType, requestID: requestID, payload: payload})
	f.mu.Unlock()
	return nil
}

func (f *fakeNodeControl) last(t *testing.T, messageType protocol.ControlMessageType) nodeMessage {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for index := len(f.messages) - 1; index >= 0; index-- {
		if f.messages[index].messageType == messageType {
			return f.messages[index]
		}
	}
	t.Fatalf("no %s message", messageType)
	return nodeMessage{}
}

func (f *fakeNodeControl) count(messageType protocol.ControlMessageType) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, message := range f.messages {
		if message.messageType == messageType {
			count++
		}
	}
	return count
}

type fakePacketDevice struct {
	router    *clientwg.PacketMux
	injectErr error
}

func (f *fakePacketDevice) SetPacketMux(router *clientwg.PacketMux) { f.router = router }
func (f *fakePacketDevice) InjectInbound([]byte) error              { return f.injectErr }

type fakeRoutes struct {
	added, removed []netip.Prefix
	conflict       bool
	lookup         route.LookupResult
}

func (f *fakeRoutes) AddRemote(prefix netip.Prefix) error {
	f.added = append(f.added, prefix)
	return nil
}
func (f *fakeRoutes) RemoveRemote(prefix netip.Prefix) error {
	f.removed = append(f.removed, prefix)
	return nil
}
func (f *fakeRoutes) Conflicts(prefix netip.Prefix) ([]route.Entry, error) {
	if f.conflict {
		return []route.Entry{{Destination: netip.MustParsePrefix("192.168.0.0/16")}}, nil
	}
	return nil, nil
}
func (*fakeRoutes) Reconcile() error { return nil }
func (f *fakeRoutes) Lookup(netip.Addr) (route.LookupResult, error) {
	if f.lookup == "" {
		return route.LookupNoRoute, nil
	}
	return f.lookup, nil
}

type fakeGateway struct {
	prepared, closed uint64
	injectErr        error
	tcpFlows         int
	udpFlows         int
}

func (f *fakeGateway) Prepare(_ context.Context, config subnetgateway.SessionConfig) error {
	f.prepared = config.SessionID
	return nil
}
func (f *fakeGateway) InjectIPv4(context.Context, uint64, []byte) error { return f.injectErr }
func (f *fakeGateway) CloseSession(_ context.Context, id uint64) error {
	f.closed = id
	return nil
}
func (f *fakeGateway) FlowCounts() (int, int) { return f.tcpFlows, f.udpFlows }
func (*fakeGateway) Close() error             { return nil }

func availableUDPPort(t *testing.T, host string) int {
	t.Helper()
	connection, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP(host), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	port := connection.LocalAddr().(*net.UDPAddr).Port
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

func testRuntimeIPv4(sourceText, destinationText string) []byte {
	packet := make([]byte, 20)
	packet[0] = 0x45
	packet[3] = 20
	source := netip.MustParseAddr(sourceText).As4()
	destination := netip.MustParseAddr(destinationText).As4()
	copy(packet[12:16], source[:])
	copy(packet[16:20], destination[:])
	return packet
}
