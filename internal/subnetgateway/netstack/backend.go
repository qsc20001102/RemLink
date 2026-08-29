// Package netstack implements the Site SubnetGateway with pinned gVisor netstack.
package netstack

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"sync/atomic"
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
	"gvisor.dev/gvisor/pkg/waiter"

	"remlink/internal/subnetgateway"
	"remlink/internal/subnetgateway/pingrelay"
	"remlink/internal/subnetgateway/tcprelay"
	"remlink/internal/subnetgateway/udprelay"
)

const (
	DefaultTCPFlowLimit               = 2048
	DefaultUDPFlowLimit               = 4096
	DefaultUDPIdleTimeout             = 60 * time.Second
	defaultQueueSize                  = 1024
	defaultMTU                        = 1280
	nicID                 tcpip.NICID = 1
)

type Dialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

type EgressHandler func(context.Context, uint64, []byte) error

type EchoProber interface {
	Echo(context.Context, netip.Addr, int, int, []byte) error
}

type Config struct {
	MTU            int
	TCPFlowLimit   int
	UDPFlowLimit   int
	UDPIdleTimeout time.Duration
	Dialer         Dialer
	Egress         EgressHandler
	PingProber     EchoProber
}

type flowProtocol uint8

const (
	flowTCP flowProtocol = iota + 1
	flowUDP
)

type flowKey struct {
	SessionID    uint64
	Protocol     flowProtocol
	EngineerIP   netip.Addr
	EngineerPort uint16
	TargetIP     netip.Addr
	TargetPort   uint16
}

type sessionState struct {
	config subnetgateway.SessionConfig
	ctx    context.Context
	cancel context.CancelFunc
}

// Backend is one process-wide IPv4 stack shared by all Site Sessions.
type Backend struct {
	mu          sync.RWMutex
	stack       *stack.Stack
	endpoint    *channel.Endpoint
	dialer      Dialer
	egress      EgressHandler
	ping        EchoProber
	udpIdle     time.Duration
	sessions    map[uint64]*sessionState
	byEngineer  map[netip.Addr]uint64
	flows       map[flowKey]context.CancelFunc
	tcpSlots    chan struct{}
	udpSlots    chan struct{}
	ctx         context.Context
	cancel      context.CancelFunc
	closeOnce   sync.Once
	egressError atomic.Value
}

type errorBox struct{ err error }

func New(config Config) (*Backend, error) {
	if config.MTU == 0 {
		config.MTU = defaultMTU
	}
	if config.MTU < 576 || config.MTU > 65535 {
		return nil, errors.New("netstack MTU must be between 576 and 65535")
	}
	if config.TCPFlowLimit <= 0 {
		config.TCPFlowLimit = DefaultTCPFlowLimit
	}
	if config.UDPFlowLimit <= 0 {
		config.UDPFlowLimit = DefaultUDPFlowLimit
	}
	if config.UDPIdleTimeout <= 0 {
		config.UDPIdleTimeout = DefaultUDPIdleTimeout
	}
	if config.Dialer == nil {
		config.Dialer = &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	}
	if config.Egress == nil {
		return nil, errors.New("netstack Egress handler is required")
	}
	if config.PingProber == nil {
		config.PingProber = pingrelay.Relay{}
	}
	ctx, cancel := context.WithCancel(context.Background())
	backend := &Backend{
		dialer: config.Dialer, egress: config.Egress, ping: config.PingProber, udpIdle: config.UDPIdleTimeout,
		sessions: make(map[uint64]*sessionState), byEngineer: make(map[netip.Addr]uint64),
		flows: make(map[flowKey]context.CancelFunc), tcpSlots: make(chan struct{}, config.TCPFlowLimit),
		udpSlots: make(chan struct{}, config.UDPFlowLimit), ctx: ctx, cancel: cancel,
	}
	backend.stack = stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol},
	})
	backend.endpoint = channel.New(defaultQueueSize, uint32(config.MTU), "")
	if err := tcpipError("create netstack NIC", backend.stack.CreateNIC(nicID, backend.endpoint)); err != nil {
		cancel()
		return nil, err
	}
	if err := tcpipError("enable netstack promiscuous mode", backend.stack.SetPromiscuousMode(nicID, true)); err != nil {
		backend.Close()
		return nil, err
	}
	if err := tcpipError("enable netstack spoofing", backend.stack.SetSpoofing(nicID, true)); err != nil {
		backend.Close()
		return nil, err
	}
	backend.stack.SetRouteTable([]tcpip.Route{{Destination: header.IPv4EmptySubnet, NIC: nicID}})
	tcpForwarder := tcp.NewForwarder(backend.stack, 0, config.TCPFlowLimit, backend.handleTCP)
	udpForwarder := udp.NewForwarder(backend.stack, func(request *udp.ForwarderRequest) { go backend.handleUDP(request) })
	backend.stack.SetTransportProtocolHandler(tcp.ProtocolNumber, tcpForwarder.HandlePacket)
	backend.stack.SetTransportProtocolHandler(udp.ProtocolNumber, udpForwarder.HandlePacket)
	go backend.runEgress()
	return backend, nil
}

func (b *Backend) Prepare(_ context.Context, config subnetgateway.SessionConfig) error {
	if err := validateSession(config); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if existing := b.sessions[config.SessionID]; existing != nil {
		if existing.config.EngineerOverlayIP != config.EngineerOverlayIP || !samePrefixes(existing.config.RemoteCIDRs, config.RemoteCIDRs) {
			return errors.New("SessionID is already bound to another configuration")
		}
		// An exact retry is idempotent. Never mutate a published session config:
		// packet injection and flow lookup intentionally read it without holding
		// the map lock on their host-I/O paths.
		return nil
	}
	if existingID, exists := b.byEngineer[config.EngineerOverlayIP]; exists && existingID != config.SessionID {
		return errors.New("Engineer already has a prepared Site Session")
	}
	sessionContext, cancel := context.WithCancel(b.ctx)
	config.RemoteCIDRs = append([]netip.Prefix(nil), config.RemoteCIDRs...)
	b.sessions[config.SessionID] = &sessionState{config: config, ctx: sessionContext, cancel: cancel}
	b.byEngineer[config.EngineerOverlayIP] = config.SessionID
	return nil
}

func (b *Backend) InjectIPv4(ctx context.Context, sessionID uint64, packet []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	session := b.sessionByID(sessionID)
	if session == nil {
		return errors.New("inject into unknown Session")
	}
	source, destination, err := rawIPv4Addresses(packet)
	if err != nil {
		return err
	}
	if source != session.config.EngineerOverlayIP || !contains(session.config.RemoteCIDRs, destination) {
		return errors.New("injected IPv4 addresses do not match Session")
	}
	if packet[9] == uint8(header.ICMPv4ProtocolNumber) {
		return b.handleICMPEcho(session, packet, source, destination)
	}
	payload := append([]byte(nil), packet...)
	packetBuffer := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(payload)})
	b.endpoint.InjectInbound(ipv4.ProtocolNumber, packetBuffer)
	packetBuffer.DecRef()
	return nil
}

func (b *Backend) handleICMPEcho(session *sessionState, packet []byte, source, destination netip.Addr) error {
	headerLength := int(packet[0]&0x0F) * 4
	if len(packet) < headerLength+header.ICMPv4MinimumSize {
		return errors.New("ICMPv4 packet is too short")
	}
	message, err := icmp.ParseMessage(1, packet[headerLength:])
	if err != nil || message.Type != xipv4.ICMPTypeEcho || message.Code != 0 {
		return errors.New("only ICMPv4 Echo Request is supported")
	}
	echo, ok := message.Body.(*icmp.Echo)
	if !ok {
		return errors.New("ICMPv4 Echo body is invalid")
	}
	data := append([]byte(nil), echo.Data...)
	go func() {
		if err := b.ping.Echo(session.ctx, destination, echo.ID, echo.Seq, data); err != nil {
			return
		}
		replyMessage := icmp.Message{
			Type: xipv4.ICMPTypeEchoReply, Code: 0,
			Body: &icmp.Echo{ID: echo.ID, Seq: echo.Seq, Data: data},
		}
		encoded, err := replyMessage.Marshal(nil)
		if err != nil {
			return
		}
		reply := buildIPv4Packet(destination, source, uint16(packet[4])<<8|uint16(packet[5]), uint8(header.ICMPv4ProtocolNumber), encoded)
		if err := b.egress(session.ctx, session.config.SessionID, reply); err != nil {
			b.egressError.Store(&errorBox{err: err})
		}
	}()
	return nil
}

func (b *Backend) CloseSession(_ context.Context, sessionID uint64) error {
	b.mu.Lock()
	session := b.sessions[sessionID]
	if session == nil {
		b.mu.Unlock()
		return nil
	}
	delete(b.sessions, sessionID)
	delete(b.byEngineer, session.config.EngineerOverlayIP)
	session.cancel()
	for key, cancel := range b.flows {
		if key.SessionID == sessionID {
			cancel()
		}
	}
	b.mu.Unlock()
	return nil
}

func (b *Backend) Close() error {
	b.closeOnce.Do(func() {
		b.cancel()
		b.mu.Lock()
		for _, session := range b.sessions {
			session.cancel()
		}
		b.sessions = make(map[uint64]*sessionState)
		b.byEngineer = make(map[netip.Addr]uint64)
		b.mu.Unlock()
		if b.endpoint != nil {
			b.endpoint.Close()
		}
		if b.stack != nil {
			b.stack.Close()
		}
	})
	return nil
}

func (b *Backend) Err() error {
	value := b.egressError.Load()
	if value == nil {
		return nil
	}
	return value.(*errorBox).err
}

func (b *Backend) FlowCounts() (tcpCount, udpCount int) {
	return len(b.tcpSlots), len(b.udpSlots)
}

func (b *Backend) handleTCP(request *tcp.ForwarderRequest) {
	id := request.ID()
	key, session, ok := b.flowFromID(flowTCP, id)
	if !ok {
		request.Complete(true)
		return
	}
	flowContext, finish, ok := b.beginFlow(key, session, b.tcpSlots)
	if !ok {
		request.Complete(true)
		return
	}
	defer finish()
	hostConnection, err := b.dialer.DialContext(flowContext, "tcp4", targetAddress(key.TargetIP, key.TargetPort))
	if err != nil {
		request.Complete(true)
		return
	}
	var queue waiter.Queue
	endpoint, endpointErr := request.CreateEndpoint(&queue)
	if endpointErr != nil {
		request.Complete(true)
		_ = hostConnection.Close()
		return
	}
	request.Complete(false)
	engineerConnection := gonet.NewTCPConn(&queue, endpoint)
	_ = tcprelay.Relay(flowContext, engineerConnection, hostConnection)
}

func (b *Backend) handleUDP(request *udp.ForwarderRequest) {
	id := request.ID()
	var queue waiter.Queue
	endpoint, endpointErr := request.CreateEndpoint(&queue)
	if endpointErr != nil {
		return
	}
	engineerConnection := gonet.NewUDPConn(&queue, endpoint)
	key, session, ok := b.flowFromID(flowUDP, id)
	if !ok {
		_ = engineerConnection.Close()
		return
	}
	flowContext, finish, ok := b.beginFlow(key, session, b.udpSlots)
	if !ok {
		_ = engineerConnection.Close()
		return
	}
	defer finish()
	hostConnection, err := b.dialer.DialContext(flowContext, "udp4", targetAddress(key.TargetIP, key.TargetPort))
	if err != nil {
		_ = engineerConnection.Close()
		return
	}
	_ = udprelay.Relay(flowContext, engineerConnection, hostConnection, b.udpIdle)
}

func (b *Backend) runEgress() {
	for {
		packetBuffer := b.endpoint.ReadContext(b.ctx)
		if packetBuffer == nil {
			return
		}
		view := packetBuffer.ToView()
		packet := append([]byte(nil), view.AsSlice()...)
		view.Release()
		packetBuffer.DecRef()
		source, destination, err := rawIPv4Addresses(packet)
		if err != nil {
			continue
		}
		session := b.sessionByEngineer(destination)
		if session == nil || !contains(session.config.RemoteCIDRs, source) {
			continue
		}
		if err := b.egress(session.ctx, session.config.SessionID, packet); err != nil {
			b.egressError.Store(&errorBox{err: err})
		}
	}
}

func (b *Backend) flowFromID(protocol flowProtocol, id stack.TransportEndpointID) (flowKey, *sessionState, bool) {
	engineerIP, ok := tcpipAddress(id.RemoteAddress)
	if !ok {
		return flowKey{}, nil, false
	}
	targetIP, ok := tcpipAddress(id.LocalAddress)
	if !ok {
		return flowKey{}, nil, false
	}
	session := b.sessionByEngineer(engineerIP)
	if session == nil || !contains(session.config.RemoteCIDRs, targetIP) {
		return flowKey{}, nil, false
	}
	return flowKey{
		SessionID: session.config.SessionID, Protocol: protocol,
		EngineerIP: engineerIP, EngineerPort: id.RemotePort, TargetIP: targetIP, TargetPort: id.LocalPort,
	}, session, true
}

func (b *Backend) beginFlow(key flowKey, session *sessionState, slots chan struct{}) (context.Context, func(), bool) {
	select {
	case slots <- struct{}{}:
	default:
		return nil, nil, false
	}
	b.mu.Lock()
	if b.sessions[key.SessionID] != session {
		b.mu.Unlock()
		<-slots
		return nil, nil, false
	}
	if _, duplicate := b.flows[key]; duplicate {
		b.mu.Unlock()
		<-slots
		return nil, nil, false
	}
	ctx, cancel := context.WithCancel(session.ctx)
	b.flows[key] = cancel
	b.mu.Unlock()
	var once sync.Once
	finish := func() {
		once.Do(func() {
			cancel()
			b.mu.Lock()
			delete(b.flows, key)
			b.mu.Unlock()
			<-slots
		})
	}
	return ctx, finish, true
}

func (b *Backend) sessionByID(sessionID uint64) *sessionState {
	b.mu.RLock()
	session := b.sessions[sessionID]
	b.mu.RUnlock()
	return session
}

func (b *Backend) sessionByEngineer(address netip.Addr) *sessionState {
	b.mu.RLock()
	session := b.sessions[b.byEngineer[address]]
	b.mu.RUnlock()
	return session
}

func validateSession(config subnetgateway.SessionConfig) error {
	if config.SessionID == 0 || !config.EngineerOverlayIP.Is4() || len(config.RemoteCIDRs) == 0 {
		return errors.New("netstack Session requires ID, Engineer IPv4, and Remote CIDRs")
	}
	for _, prefix := range config.RemoteCIDRs {
		if !prefix.Addr().Is4() || prefix != prefix.Masked() || prefix.Bits() == 0 || prefix.Contains(config.EngineerOverlayIP) {
			return errors.New("netstack Remote CIDRs must be canonical non-default IPv4 and exclude Engineer")
		}
	}
	return nil
}

func rawIPv4Addresses(packet []byte) (netip.Addr, netip.Addr, error) {
	if len(packet) < header.IPv4MinimumSize || packet[0]>>4 != 4 {
		return netip.Addr{}, netip.Addr{}, errors.New("invalid raw IPv4 packet")
	}
	headerLength := int(packet[0]&0x0F) * 4
	totalLength := int(packet[2])<<8 | int(packet[3])
	if headerLength < header.IPv4MinimumSize || totalLength != len(packet) || totalLength < headerLength {
		return netip.Addr{}, netip.Addr{}, errors.New("invalid raw IPv4 lengths")
	}
	return netip.AddrFrom4([4]byte{packet[12], packet[13], packet[14], packet[15]}),
		netip.AddrFrom4([4]byte{packet[16], packet[17], packet[18], packet[19]}), nil
}

func contains(prefixes []netip.Prefix, address netip.Addr) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func samePrefixes(left, right []netip.Prefix) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func tcpipAddress(address tcpip.Address) (netip.Addr, bool) {
	if address.Len() != 4 {
		return netip.Addr{}, false
	}
	return netip.AddrFrom4(address.As4()), true
}

func targetAddress(address netip.Addr, port uint16) string {
	return net.JoinHostPort(address.String(), strconv.Itoa(int(port)))
}

func tcpipError(operation string, err tcpip.Error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %s", operation, err.String())
}

func buildIPv4Packet(source, destination netip.Addr, identification uint16, protocol uint8, payload []byte) []byte {
	packet := make([]byte, header.IPv4MinimumSize+len(payload))
	packet[0] = 0x45
	totalLength := len(packet)
	packet[2], packet[3] = byte(totalLength>>8), byte(totalLength)
	packet[4], packet[5] = byte(identification>>8), byte(identification)
	packet[8] = 64
	packet[9] = protocol
	sourceBytes := source.As4()
	destinationBytes := destination.As4()
	copy(packet[12:16], sourceBytes[:])
	copy(packet[16:20], destinationBytes[:])
	checksum := ipv4HeaderChecksum(packet[:header.IPv4MinimumSize])
	packet[10], packet[11] = byte(checksum>>8), byte(checksum)
	copy(packet[header.IPv4MinimumSize:], payload)
	return packet
}

func ipv4HeaderChecksum(headerBytes []byte) uint16 {
	var sum uint32
	for index := 0; index+1 < len(headerBytes); index += 2 {
		sum += uint32(headerBytes[index])<<8 | uint32(headerBytes[index+1])
	}
	for sum>>16 != 0 {
		sum = sum&0xFFFF + sum>>16
	}
	return ^uint16(sum)
}
