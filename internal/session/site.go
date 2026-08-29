package session

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"time"

	"remlink/internal/model"
	"remlink/internal/platform/windows/route"
	"remlink/internal/protocol"
	"remlink/internal/subnet"
	"remlink/internal/subnetgateway"
)

type SiteRoutes interface {
	Lookup(netip.Addr) (route.LookupResult, error)
}

type SiteGateway interface {
	subnetgateway.SubnetGateway
	FlowCounts() (int, int)
	Close() error
}

type SiteConfig struct {
	LocalOverlayIP netip.Addr
	OverlayCIDR    netip.Prefix
	UDPPort        int
	TCPFlowLimit   int
	UDPFlowLimit   int
	StatsInterval  time.Duration
	Routes         SiteRoutes
	Gateway        SiteGateway
	Control        ControlSender
	OnSession      func(model.SessionStatus, uint64, string)
	OnRoute        func(uint64, netip.Prefix, route.LookupResult)
	OnPacketReject subnet.RejectHandler
}

type SiteRuntime struct {
	mu        sync.Mutex
	config    SiteConfig
	ctx       context.Context
	cancel    context.CancelFunc
	registry  *subnet.Registry
	listener  *subnet.Listener
	sessions  map[uint64]*siteSession
	done      chan struct{}
	closeOnce sync.Once
}

type siteSession struct {
	id       uint64
	engineer netip.Addr
	prefixes []netip.Prefix
	sender   *subnet.Sender
	status   model.SessionStatus
}

func NewSiteRuntime(parent context.Context, config SiteConfig) (*SiteRuntime, error) {
	if !config.LocalOverlayIP.Is4() || !config.OverlayCIDR.IsValid() || !config.OverlayCIDR.Addr().Is4() ||
		config.UDPPort < 1 || config.UDPPort > 65535 || config.Routes == nil || config.Gateway == nil || config.Control == nil {
		return nil, errors.New("Site runtime requires Overlay addressing, UDP port, routes, netstack gateway, and Control")
	}
	if config.TCPFlowLimit <= 0 {
		config.TCPFlowLimit = 2048
	}
	if config.UDPFlowLimit <= 0 {
		config.UDPFlowLimit = 4096
	}
	if config.StatsInterval <= 0 {
		config.StatsInterval = DefaultStatsInterval
	}
	ctx, cancel := context.WithCancel(parent)
	runtime := &SiteRuntime{
		config: config, ctx: ctx, cancel: cancel, registry: subnet.NewRegistry(),
		sessions: make(map[uint64]*siteSession), done: make(chan struct{}),
	}
	listener, err := subnet.NewListener(config.LocalOverlayIP, config.UDPPort, runtime.registry, runtime.inject, config.OnPacketReject)
	if err != nil {
		cancel()
		return nil, err
	}
	runtime.listener = listener
	go runtime.run()
	return runtime, nil
}

func (s *SiteRuntime) HandleControl(ctx context.Context, envelope protocol.ControlEnvelope) error {
	switch envelope.Type {
	case protocol.ControlPrepareSession:
		var payload protocol.PrepareSessionPayload
		if err := envelope.DecodePayload(&payload); err != nil {
			return err
		}
		return s.prepare(ctx, payload)
	case protocol.ControlSessionActive:
		var payload protocol.SessionActivePayload
		if err := envelope.DecodePayload(&payload); err != nil {
			return err
		}
		return s.activate(payload.SessionID)
	case protocol.ControlStopSession:
		var payload protocol.StopSessionPayload
		if err := envelope.DecodePayload(&payload); err != nil {
			return err
		}
		return s.stop(ctx, payload)
	case protocol.ControlRebootstrapRequired:
		return protocol.ErrRebootstrapRequired
	default:
		return fmt.Errorf("unexpected Site Control message %s", envelope.Type)
	}
}

func (s *SiteRuntime) prepare(ctx context.Context, payload protocol.PrepareSessionPayload) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if payload.SessionID == 0 || s.sessions[payload.SessionID] != nil {
		return errors.New("invalid or duplicate PREPARE_SESSION")
	}
	engineer, err := netip.ParseAddr(payload.EngineerOverlayIP)
	if err != nil || !engineer.Is4() || !s.config.OverlayCIDR.Contains(engineer) {
		return s.sendPrepareFailure(ctx, payload.SessionID, nil, protocol.ErrorCIDRInvalid, "Engineer Overlay IP is invalid")
	}
	prefixes, code := validateCIDRs(payload.TargetCIDRs, s.config.OverlayCIDR)
	if code != "" {
		return s.sendPrepareFailure(ctx, payload.SessionID, nil, code, "Remote CIDR validation failed")
	}
	routeResults := make([]protocol.RouteResult, 0, len(prefixes))
	for _, prefix := range prefixes {
		result, err := s.config.Routes.Lookup(prefix.Addr())
		if err != nil {
			return s.sendPrepareFailure(ctx, payload.SessionID, routeResults, protocol.ErrorSiteNoRoute, err.Error())
		}
		routeResults = append(routeResults, protocol.RouteResult{CIDR: prefix.String(), Result: string(result)})
		if s.config.OnRoute != nil {
			s.config.OnRoute(payload.SessionID, prefix, result)
		}
		if result == route.LookupNoRoute || result == route.LookupDefaultOnly {
			return s.sendPrepareFailure(ctx, payload.SessionID, routeResults, protocol.ErrorSiteNoRoute, "Site has no route to Remote CIDR")
		}
		if result == route.LookupOverlayConflict {
			return s.sendPrepareFailure(ctx, payload.SessionID, routeResults, protocol.ErrorCIDROverlayConflict, "Remote CIDR overlaps Overlay")
		}
	}
	tcpFlows, udpFlows := s.config.Gateway.FlowCounts()
	if tcpFlows >= s.config.TCPFlowLimit || udpFlows >= s.config.UDPFlowLimit {
		return s.sendPrepareFailure(ctx, payload.SessionID, routeResults, protocol.ErrorFlowLimitReached, "Site netstack flow capacity is exhausted")
	}
	if err := s.config.Gateway.Prepare(ctx, subnetgateway.SessionConfig{
		SessionID: payload.SessionID, EngineerOverlayIP: engineer, RemoteCIDRs: prefixes,
	}); err != nil {
		return s.sendPrepareFailure(ctx, payload.SessionID, routeResults, protocol.ErrorNetstackUnavailable, err.Error())
	}
	sender, err := subnet.NewSender(s.ctx, subnet.SenderConfig{
		SessionID: payload.SessionID, LocalIP: s.config.LocalOverlayIP, PeerIP: engineer, PeerPort: s.config.UDPPort,
	})
	if err != nil {
		_ = s.config.Gateway.CloseSession(ctx, payload.SessionID)
		return s.sendPrepareFailure(ctx, payload.SessionID, routeResults, protocol.ErrorNetstackUnavailable, err.Error())
	}
	if err := s.registry.Upsert(subnet.SessionBinding{
		SessionID: payload.SessionID, PeerOverlayIP: engineer, EngineerOverlayIP: engineer,
		RemoteCIDRs: prefixes, Direction: subnet.EngineerToSite, Active: false,
	}); err != nil {
		_ = sender.Close()
		_ = s.config.Gateway.CloseSession(ctx, payload.SessionID)
		return err
	}
	s.sessions[payload.SessionID] = &siteSession{
		id: payload.SessionID, engineer: engineer, prefixes: prefixes, sender: sender, status: model.SessionReady,
	}
	result := protocol.PrepareResultPayload{
		SessionID: payload.SessionID, OK: true, RouteResults: routeResults, SubnetGatewayStatus: "netstack",
		TCPCapacity: s.config.TCPFlowLimit - tcpFlows, UDPCapacity: s.config.UDPFlowLimit - udpFlows,
	}
	if err := s.config.Control.Send(ctx, protocol.ControlPrepareResult, "", result); err != nil {
		s.cleanupLocked(ctx, payload.SessionID)
		return err
	}
	s.notify(model.SessionReady, payload.SessionID, "")
	return nil
}

func (s *SiteRuntime) sendPrepareFailure(ctx context.Context, sessionID uint64, results []protocol.RouteResult, code protocol.ErrorCode, message string) error {
	return s.config.Control.Send(ctx, protocol.ControlPrepareResult, "", protocol.PrepareResultPayload{
		SessionID: sessionID, OK: false, RouteResults: results, SubnetGatewayStatus: "netstack",
		ErrorCode: code, Error: message,
	})
}

func (s *SiteRuntime) activate(sessionID uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.sessions[sessionID]
	if current == nil || current.status != model.SessionReady {
		return errors.New("SESSION_ACTIVE does not match Site READY Session")
	}
	binding, _ := s.registry.Lookup(sessionID)
	binding.Active = true
	if err := s.registry.Upsert(binding); err != nil {
		return err
	}
	current.status = model.SessionActive
	s.notify(model.SessionActive, sessionID, "")
	return nil
}

func (s *SiteRuntime) stop(ctx context.Context, payload protocol.StopSessionPayload) error {
	s.mu.Lock()
	if s.sessions[payload.SessionID] == nil {
		s.mu.Unlock()
		return nil
	}
	s.cleanupLocked(ctx, payload.SessionID)
	s.mu.Unlock()
	s.notify(model.SessionClosed, payload.SessionID, payload.Reason)
	return nil
}

func (s *SiteRuntime) inject(ctx context.Context, sessionID uint64, packet []byte) error {
	if err := s.config.Gateway.InjectIPv4(ctx, sessionID, packet); err != nil {
		_ = s.config.Control.Send(context.Background(), protocol.ControlStopSession, "", protocol.StopSessionPayload{
			SessionID: sessionID, Reason: string(protocol.ErrorSessionInjectFailed),
		})
		s.mu.Lock()
		s.cleanupLocked(context.Background(), sessionID)
		s.mu.Unlock()
		s.notify(model.SessionFailed, sessionID, string(protocol.ErrorSessionInjectFailed))
		return err
	}
	return nil
}

// Egress is the netstack callback for raw Site-to-Engineer IPv4 packets.
func (s *SiteRuntime) Egress(_ context.Context, sessionID uint64, packet []byte) error {
	s.mu.Lock()
	current := s.sessions[sessionID]
	if current == nil || current.status != model.SessionActive {
		s.mu.Unlock()
		return errors.New("netstack egress references an inactive Session")
	}
	sender := current.sender
	s.mu.Unlock()
	if !sender.Enqueue(append([]byte(nil), packet...)) {
		return errors.New("Session UDP send queue is full")
	}
	return nil
}

func (s *SiteRuntime) run() {
	defer close(s.done)
	listenerErrors := make(chan error, 1)
	go func() { listenerErrors <- s.listener.Run(s.ctx) }()
	ticker := time.NewTicker(s.config.StatsInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			_ = s.listener.Close()
			return
		case <-listenerErrors:
			s.cancel()
			return
		case <-ticker.C:
			s.reportStats()
		}
	}
}

func (s *SiteRuntime) reportStats() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, current := range s.sessions {
		if current.status != model.SessionActive {
			continue
		}
		uploadBytes, uploadPackets := s.listener.SessionCounters(current.id)
		downloadBytes, downloadPackets, _ := current.sender.Counters()
		counters := model.SessionCounters{
			UploadBytes: uploadBytes, UploadPackets: uploadPackets,
			DownloadBytes: downloadBytes, DownloadPackets: downloadPackets,
		}
		_ = s.config.Control.Send(s.ctx, protocol.ControlSessionStats, "", protocol.SessionStatsPayload{SessionID: current.id, Counters: counters})
	}
}

func (s *SiteRuntime) cleanupLocked(ctx context.Context, sessionID uint64) {
	current := s.sessions[sessionID]
	if current == nil {
		return
	}
	s.registry.Remove(sessionID)
	_ = current.sender.Close()
	_ = s.config.Gateway.CloseSession(ctx, sessionID)
	delete(s.sessions, sessionID)
}

func (s *SiteRuntime) notify(status model.SessionStatus, id uint64, reason string) {
	if s.config.OnSession != nil {
		s.config.OnSession(status, id, reason)
	}
}

func (s *SiteRuntime) Close() error {
	s.closeOnce.Do(func() {
		s.cancel()
		s.mu.Lock()
		for id := range s.sessions {
			s.cleanupLocked(context.Background(), id)
		}
		s.mu.Unlock()
		_ = s.listener.Close()
		_ = s.config.Gateway.Close()
		<-s.done
	})
	return nil
}
