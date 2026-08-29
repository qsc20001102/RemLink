package netstack

import (
	"context"
	"io"
	"net"
	"net/netip"
	"testing"
	"time"

	"golang.org/x/net/icmp"
	xipv4 "golang.org/x/net/ipv4"
	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"

	"remlink/internal/subnetgateway"
)

func TestTCPForwarderHostDialAndRoundTrip(t *testing.T) {
	targetIP := localTestIPv4(t)
	hostListener, err := net.Listen("tcp4", net.JoinHostPort(targetIP.String(), "0"))
	if err != nil {
		t.Fatal(err)
	}
	defer hostListener.Close()
	go func() {
		connection, err := hostListener.Accept()
		if err != nil {
			return
		}
		defer connection.Close()
		_, _ = io.Copy(connection, connection)
	}()

	testNetwork := newTestNetwork(t, targetIP)
	defer testNetwork.close()
	port := uint16(hostListener.Addr().(*net.TCPAddr).Port)
	connection, err := gonet.DialTCP(testNetwork.clientStack, tcpip.FullAddress{
		Addr: tcpip.AddrFrom4(targetIP.As4()), Port: port,
	}, ipv4.ProtocolNumber)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
	want := []byte("gVisor TCP forwarder")
	if _, err := connection.Write(want); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(want))
	if _, err := io.ReadFull(connection, got); err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("TCP echo = %q, want %q", got, want)
	}
}

func TestUDPForwarderHostSocketAndRoundTrip(t *testing.T) {
	targetIP := localTestIPv4(t)
	hostConnection, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IP(targetIP.AsSlice())})
	if err != nil {
		t.Fatal(err)
	}
	defer hostConnection.Close()
	go func() {
		buffer := make([]byte, 2048)
		count, source, err := hostConnection.ReadFromUDP(buffer)
		if err == nil {
			_, _ = hostConnection.WriteToUDP(buffer[:count], source)
		}
	}()

	testNetwork := newTestNetwork(t, targetIP)
	defer testNetwork.close()
	port := uint16(hostConnection.LocalAddr().(*net.UDPAddr).Port)
	connection, err := gonet.DialUDP(testNetwork.clientStack, nil, &tcpip.FullAddress{
		Addr: tcpip.AddrFrom4(targetIP.As4()), Port: port,
	}, ipv4.ProtocolNumber)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
	want := []byte("gVisor UDP forwarder")
	if _, err := connection.Write(want); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(want))
	count, err := connection.Read(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(got[:count]) != string(want) {
		t.Fatalf("UDP echo = %q, want %q", got[:count], want)
	}
}

func TestICMPEchoRelayPreservesIdentityAndBuildsRawReply(t *testing.T) {
	replies := make(chan []byte, 1)
	prober := &fakeEchoProber{}
	backend, err := New(Config{
		PingProber: prober,
		Egress: func(_ context.Context, sessionID uint64, packet []byte) error {
			if sessionID != 99 {
				t.Errorf("reply SessionID = %d", sessionID)
			}
			replies <- packet
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	engineer := netip.MustParseAddr("10.88.0.2")
	target := netip.MustParseAddr("192.168.13.10")
	if err := backend.Prepare(context.Background(), subnetgateway.SessionConfig{
		SessionID: 99, EngineerOverlayIP: engineer,
		RemoteCIDRs: []netip.Prefix{netip.MustParsePrefix("192.168.13.0/24")},
	}); err != nil {
		t.Fatal(err)
	}
	echoRequest, err := (&icmp.Message{
		Type: xipv4.ICMPTypeEcho, Body: &icmp.Echo{ID: 0x1234, Seq: 77, Data: []byte("ping-data")},
	}).Marshal(nil)
	if err != nil {
		t.Fatal(err)
	}
	request := buildIPv4Packet(engineer, target, 0xABCD, uint8(header.ICMPv4ProtocolNumber), echoRequest)
	if err := backend.InjectIPv4(context.Background(), 99, request); err != nil {
		t.Fatal(err)
	}
	select {
	case reply := <-replies:
		source, destination, err := rawIPv4Addresses(reply)
		if err != nil {
			t.Fatal(err)
		}
		if source != target || destination != engineer || uint16(reply[4])<<8|uint16(reply[5]) != 0xABCD {
			t.Fatalf("reply addresses/ID source=%s destination=%s id=%x", source, destination, reply[4:6])
		}
		message, err := icmp.ParseMessage(1, reply[header.IPv4MinimumSize:])
		if err != nil || message.Type != xipv4.ICMPTypeEchoReply {
			t.Fatalf("reply ICMP = %+v, %v", message, err)
		}
		echo := message.Body.(*icmp.Echo)
		if echo.ID != 0x1234 || echo.Seq != 77 || string(echo.Data) != "ping-data" {
			t.Fatalf("reply Echo = %+v", echo)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for ICMP Echo Reply")
	}
	if prober.target != target || prober.id != 0x1234 || prober.sequence != 77 {
		t.Fatalf("prober call = %+v", prober)
	}
}

func TestDuplicateRemoteCIDRsAreIsolatedBySessionAndEngineer(t *testing.T) {
	backend, err := New(Config{TCPFlowLimit: 4, UDPFlowLimit: 4, Egress: func(context.Context, uint64, []byte) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	remote := netip.MustParsePrefix("192.168.13.0/24")
	engineerA := netip.MustParseAddr("10.88.0.10")
	engineerB := netip.MustParseAddr("10.88.0.11")
	for _, session := range []subnetgateway.SessionConfig{
		{SessionID: 101, EngineerOverlayIP: engineerA, RemoteCIDRs: []netip.Prefix{remote}},
		{SessionID: 202, EngineerOverlayIP: engineerB, RemoteCIDRs: []netip.Prefix{remote}},
	} {
		if err := backend.Prepare(context.Background(), session); err != nil {
			t.Fatal(err)
		}
	}
	target := tcpip.AddrFrom4([4]byte{192, 168, 13, 50})
	makeID := func(engineer netip.Addr) stack.TransportEndpointID {
		return stack.TransportEndpointID{
			RemoteAddress: tcpip.AddrFrom4(engineer.As4()), RemotePort: 41000,
			LocalAddress: target, LocalPort: 502,
		}
	}
	keyA, sessionA, okA := backend.flowFromID(flowTCP, makeID(engineerA))
	keyB, sessionB, okB := backend.flowFromID(flowTCP, makeID(engineerB))
	if !okA || !okB || keyA.SessionID != 101 || keyB.SessionID != 202 || sessionA == sessionB || keyA == keyB {
		t.Fatalf("flow isolation A=%+v/%p/%v B=%+v/%p/%v", keyA, sessionA, okA, keyB, sessionB, okB)
	}
	_, finishA, startedA := backend.beginFlow(keyA, sessionA, backend.tcpSlots)
	_, finishB, startedB := backend.beginFlow(keyB, sessionB, backend.tcpSlots)
	if !startedA || !startedB || len(backend.tcpSlots) != 2 {
		t.Fatalf("parallel flows started A=%v B=%v count=%d", startedA, startedB, len(backend.tcpSlots))
	}
	finishA()
	finishB()
	if len(backend.tcpSlots) != 0 {
		t.Fatalf("flow slots leaked: %d", len(backend.tcpSlots))
	}
}

func TestPrepareRetryIsIdempotentButCannotMutatePublishedSession(t *testing.T) {
	backend, err := New(Config{Egress: func(context.Context, uint64, []byte) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	original := subnetgateway.SessionConfig{
		SessionID: 303, EngineerOverlayIP: netip.MustParseAddr("10.88.0.30"),
		RemoteCIDRs: []netip.Prefix{netip.MustParsePrefix("192.168.13.0/24")},
	}
	if err := backend.Prepare(context.Background(), original); err != nil {
		t.Fatal(err)
	}
	if err := backend.Prepare(context.Background(), original); err != nil {
		t.Fatalf("exact PREPARE retry failed: %v", err)
	}
	changed := original
	changed.RemoteCIDRs = []netip.Prefix{netip.MustParsePrefix("192.168.21.0/24")}
	if err := backend.Prepare(context.Background(), changed); err == nil {
		t.Fatal("PREPARE retry mutated an already published Session configuration")
	}
	if err := backend.InjectIPv4(context.Background(), original.SessionID,
		buildIPv4Packet(original.EngineerOverlayIP, netip.MustParseAddr("192.168.21.10"), 1, uint8(header.UDPProtocolNumber), []byte{0, 1})); err == nil {
		t.Fatal("changed Remote CIDR became visible after rejected PREPARE retry")
	}
}

type fakeEchoProber struct {
	target   netip.Addr
	id       int
	sequence int
}

func (p *fakeEchoProber) Echo(_ context.Context, target netip.Addr, id, sequence int, _ []byte) error {
	p.target, p.id, p.sequence = target, id, sequence
	return nil
}

type testNetwork struct {
	ctx            context.Context
	cancel         context.CancelFunc
	clientStack    *stack.Stack
	clientEndpoint *channel.Endpoint
	backend        *Backend
}

func newTestNetwork(t *testing.T, targetIP netip.Addr) *testNetwork {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	network := &testNetwork{ctx: ctx, cancel: cancel}
	network.clientStack = stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol},
	})
	network.clientEndpoint = channel.New(1024, 1280, "")
	if err := network.clientStack.CreateNIC(nicID, network.clientEndpoint); err != nil {
		cancel()
		t.Fatal(err.String())
	}
	engineer := tcpip.AddrFrom4([4]byte{10, 88, 0, 2})
	if err := network.clientStack.AddProtocolAddress(nicID, tcpip.ProtocolAddress{
		Protocol: ipv4.ProtocolNumber, AddressWithPrefix: engineer.WithPrefix(),
	}, stack.AddressProperties{}); err != nil {
		cancel()
		t.Fatal(err.String())
	}
	network.clientStack.SetRouteTable([]tcpip.Route{{Destination: header.IPv4EmptySubnet, NIC: nicID}})

	var backend *Backend
	var err error
	backend, err = New(Config{
		TCPFlowLimit: 8, UDPFlowLimit: 8, UDPIdleTimeout: time.Second,
		Egress: func(_ context.Context, sessionID uint64, packet []byte) error {
			if sessionID != 7 {
				t.Errorf("egress SessionID = %d", sessionID)
			}
			packetBuffer := stack.NewPacketBuffer(stack.PacketBufferOptions{
				Payload: buffer.MakeWithData(append([]byte(nil), packet...)),
			})
			network.clientEndpoint.InjectInbound(ipv4.ProtocolNumber, packetBuffer)
			packetBuffer.DecRef()
			return nil
		},
	})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	network.backend = backend
	if err := backend.Prepare(ctx, subnetgateway.SessionConfig{
		SessionID: 7, EngineerOverlayIP: netip.MustParseAddr("10.88.0.2"),
		RemoteCIDRs: []netip.Prefix{netip.PrefixFrom(targetIP, 32)},
	}); err != nil {
		network.close()
		t.Fatal(err)
	}
	go func() {
		for {
			packetBuffer := network.clientEndpoint.ReadContext(ctx)
			if packetBuffer == nil {
				return
			}
			view := packetBuffer.ToView()
			packet := append([]byte(nil), view.AsSlice()...)
			view.Release()
			packetBuffer.DecRef()
			_ = backend.InjectIPv4(ctx, 7, packet)
		}
	}()
	return network
}

func localTestIPv4(t *testing.T) netip.Addr {
	t.Helper()
	interfaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, networkInterface := range interfaces {
		if networkInterface.Flags&net.FlagUp == 0 || networkInterface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, err := networkInterface.Addrs()
		if err != nil {
			continue
		}
		for _, raw := range addresses {
			prefix, err := netip.ParsePrefix(raw.String())
			if err == nil && prefix.Addr().Is4() && !prefix.Addr().IsLoopback() {
				return prefix.Addr()
			}
		}
	}
	t.Skip("no non-loopback IPv4 address available for host relay test")
	return netip.Addr{}
}

func (n *testNetwork) close() {
	n.cancel()
	if n.backend != nil {
		_ = n.backend.Close()
	}
	if n.clientEndpoint != nil {
		n.clientEndpoint.Close()
	}
	if n.clientStack != nil {
		n.clientStack.Close()
	}
}
