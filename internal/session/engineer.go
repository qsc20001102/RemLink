package session

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"time"

	"github.com/google/uuid"

	"remlink/internal/model"
	"remlink/internal/overlay/clientwg"
	"remlink/internal/platform/windows/route"
	"remlink/internal/protocol"
	"remlink/internal/subnet"
)

const DefaultStatsInterval = 5 * time.Second

type ControlSender interface {
	Send(context.Context, protocol.ControlMessageType, string, any) error
}

type PacketDevice interface {
	SetPacketMux(*clientwg.PacketMux)
	InjectInbound([]byte) error
}

type EngineerRoutes interface {
	AddRemote(netip.Prefix) error
	RemoveRemote(netip.Prefix) error
	Conflicts(netip.Prefix) ([]route.Entry, error)
	Reconcile() error
}

type EngineerConfig struct {
	LocalOverlayIP netip.Addr
	OverlayCIDR    netip.Prefix
	UDPPort        int
	StatsInterval  time.Duration
	Routes         EngineerRoutes
	Device         PacketDevice
	Control        ControlSender
	OnNodeList     func(protocol.NodeListPayload)
	OnSession      func(model.SessionStatus, uint64, string)
	OnPacketReject subnet.RejectHandler
	OnPacketDrop   func(clientwg.DropEvent)
}

type EngineerRuntime struct {
	mu               sync.Mutex
	config           EngineerConfig
	ctx              context.Context
	cancel           context.CancelFunc
	registry         *subnet.Registry
	listener         *subnet.Listener
	current          *engineerSession
	pending          bool
	pendingRequestID string
	done             chan struct{}
	closeOnce        sync.Once
}

type engineerSession struct {
	id       uint64
	peer     netip.Addr
	prefixes []netip.Prefix
	routes   []netip.Prefix
	sender   *subnet.Sender
	router   *clientwg.PacketMux
	status   model.SessionStatus
}

type EngineerSnapshot struct {
	ID       uint64
	Peer     netip.Addr
	CIDRs    []netip.Prefix
	Status   model.SessionStatus
	Counters model.SessionCounters
}

func (e *EngineerRuntime) Snapshot() EngineerSnapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.current == nil {
		return EngineerSnapshot{}
	}
	uploadBytes, uploadPackets := e.current.router.RemoteCounters()
	downloadBytes, downloadPackets := e.listener.SessionCounters(e.current.id)
	return EngineerSnapshot{
		ID: e.current.id, Peer: e.current.peer, CIDRs: append([]netip.Prefix(nil), e.current.prefixes...), Status: e.current.status,
		Counters: model.SessionCounters{UploadBytes: uploadBytes, UploadPackets: uploadPackets, DownloadBytes: downloadBytes, DownloadPackets: downloadPackets},
	}
}

func NewEngineerRuntime(parent context.Context, config EngineerConfig) (*EngineerRuntime, error) {
	if !config.LocalOverlayIP.Is4() || !config.OverlayCIDR.IsValid() || !config.OverlayCIDR.Addr().Is4() ||
		config.UDPPort < 1 || config.UDPPort > 65535 || config.Routes == nil || config.Device == nil || config.Control == nil {
		return nil, errors.New("Engineer runtime requires Overlay addressing, UDP port, routes, packet device, and Control")
	}
	if config.StatsInterval <= 0 {
		config.StatsInterval = DefaultStatsInterval
	}
	if err := config.Routes.Reconcile(); err != nil {
		return nil, fmt.Errorf("reconcile Engineer Remote routes: %w", err)
	}
	ctx, cancel := context.WithCancel(parent)
	runtime := &EngineerRuntime{config: config, ctx: ctx, cancel: cancel, registry: subnet.NewRegistry(), done: make(chan struct{})}
	listener, err := subnet.NewListener(config.LocalOverlayIP, config.UDPPort, runtime.registry, runtime.inject, config.OnPacketReject)
	if err != nil {
		cancel()
		return nil, err
	}
	runtime.listener = listener
	go runtime.run()
	return runtime, nil
}

// CreateSession performs the mandatory local prefix-overlap preflight before
// asking the Server to create any state.
func (e *EngineerRuntime) CreateSession(ctx context.Context, siteNodeID string, cidrs []string) (string, error) {
	prefixes, code := validateCIDRs(cidrs, e.config.OverlayCIDR)
	if code != "" {
		return "", fmt.Errorf("%s", code)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.pending || e.current != nil {
		return "", fmt.Errorf("%s", protocol.ErrorEngineerSessionExists)
	}
	if err := e.checkConflictsLocked(prefixes); err != nil {
		return "", err
	}
	requestID := uuid.NewString()
	e.pending = true
	e.pendingRequestID = requestID
	if err := e.config.Control.Send(ctx, protocol.ControlCreateSession, requestID,
		protocol.CreateSessionPayload{SiteNodeID: siteNodeID, TargetCIDRs: cidrs}); err != nil {
		e.clearPendingLocked()
		return "", err
	}
	e.notify(model.SessionCreating, 0, "")
	return requestID, nil
}

// PreflightCIDRs runs the same authoritative local route-overlap check used by
// CreateSession without creating Server or Route state.
func (e *EngineerRuntime) PreflightCIDRs(cidrs []string) error {
	prefixes, code := validateCIDRs(cidrs, e.config.OverlayCIDR)
	if code != "" {
		return fmt.Errorf("%s", code)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.checkConflictsLocked(prefixes)
}

func (e *EngineerRuntime) checkConflictsLocked(prefixes []netip.Prefix) error {
	for _, prefix := range prefixes {
		conflicts, err := e.config.Routes.Conflicts(prefix)
		if err != nil {
			return err
		}
		if len(conflicts) != 0 {
			return fmt.Errorf("%s: %s overlaps %s", protocol.ErrorCIDRLocalConflict, prefix, conflicts[0].Destination)
		}
	}
	return nil
}

func (e *EngineerRuntime) HandleControl(ctx context.Context, envelope protocol.ControlEnvelope) error {
	switch envelope.Type {
	case protocol.ControlNodeList:
		var payload protocol.NodeListPayload
		if err := envelope.DecodePayload(&payload); err != nil {
			return err
		}
		if e.config.OnNodeList != nil {
			e.config.OnNodeList(payload)
		}
		return nil
	case protocol.ControlSessionConfig:
		var payload protocol.SessionConfigPayload
		if err := envelope.DecodePayload(&payload); err != nil {
			return err
		}
		return e.configure(ctx, envelope.RequestID, payload)
	case protocol.ControlSessionActive:
		var payload protocol.SessionActivePayload
		if err := envelope.DecodePayload(&payload); err != nil {
			return err
		}
		return e.activate(payload.SessionID)
	case protocol.ControlStopSession:
		var payload protocol.StopSessionPayload
		if err := envelope.DecodePayload(&payload); err != nil {
			return err
		}
		return e.stop(envelope.RequestID, payload)
	case protocol.ControlRebootstrapRequired:
		return protocol.ErrRebootstrapRequired
	default:
		return fmt.Errorf("unexpected Engineer Control message %s", envelope.Type)
	}
}

func (e *EngineerRuntime) configure(ctx context.Context, requestID string, payload protocol.SessionConfigPayload) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.pending || requestID == "" || requestID != e.pendingRequestID || e.current != nil || payload.SessionID == 0 || payload.UDPPort != e.config.UDPPort {
		return errors.New("SESSION_CONFIG does not match a pending Engineer request")
	}
	peer, err := netip.ParseAddr(payload.PeerOverlayIP)
	if err != nil || !peer.Is4() || !e.config.OverlayCIDR.Contains(peer) {
		return errors.New("SESSION_CONFIG peer is outside Overlay")
	}
	prefixes, code := validateCIDRs(payload.CIDRs, e.config.OverlayCIDR)
	if code != "" {
		return fmt.Errorf("%s", code)
	}
	current := &engineerSession{id: payload.SessionID, peer: peer, prefixes: prefixes, status: model.SessionReady}
	for _, prefix := range prefixes {
		if err := e.config.Routes.AddRemote(prefix); err != nil {
			e.rollbackRoutes(current.routes)
			e.clearPendingLocked()
			_ = e.config.Control.Send(ctx, protocol.ControlStopSession, "", protocol.StopSessionPayload{SessionID: payload.SessionID, Reason: string(protocol.ErrorCIDRLocalConflict)})
			return err
		}
		current.routes = append(current.routes, prefix)
	}
	sender, err := subnet.NewSender(e.ctx, subnet.SenderConfig{
		SessionID: payload.SessionID, LocalIP: e.config.LocalOverlayIP, PeerIP: peer, PeerPort: payload.UDPPort,
	})
	if err != nil {
		e.rollbackRoutes(current.routes)
		e.clearPendingLocked()
		return err
	}
	current.sender = sender
	current.router = clientwg.NewPacketMux(e.config.OverlayCIDR, prefixes, sender)
	current.router.SetDropHandler(e.config.OnPacketDrop)
	if err := e.registry.Upsert(subnet.SessionBinding{
		SessionID: payload.SessionID, PeerOverlayIP: peer, EngineerOverlayIP: e.config.LocalOverlayIP,
		RemoteCIDRs: prefixes, Direction: subnet.SiteToEngineer, Active: false,
	}); err != nil {
		_ = sender.Close()
		e.rollbackRoutes(current.routes)
		e.clearPendingLocked()
		return err
	}
	e.current = current
	e.clearPendingLocked()
	e.config.Device.SetPacketMux(current.router)
	if err := e.config.Control.Send(ctx, protocol.ControlRoutesReady, "", protocol.RoutesReadyPayload{SessionID: payload.SessionID}); err != nil {
		e.cleanupLocked()
		return err
	}
	e.notify(model.SessionReady, payload.SessionID, "")
	return nil
}

func (e *EngineerRuntime) activate(sessionID uint64) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.current == nil || e.current.id != sessionID || e.current.status != model.SessionReady {
		return errors.New("SESSION_ACTIVE does not match Engineer READY Session")
	}
	binding, _ := e.registry.Lookup(sessionID)
	binding.Active = true
	if err := e.registry.Upsert(binding); err != nil {
		return err
	}
	e.current.status = model.SessionActive
	e.notify(model.SessionActive, sessionID, "")
	return nil
}

func (e *EngineerRuntime) stop(requestID string, payload protocol.StopSessionPayload) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.current == nil && e.pending {
		// A PREPARE failure arrives before SESSION_CONFIG, so SessionID alone
		// cannot identify Engineer-local state. Only the matching CREATE request
		// may release the pending guard; stale replies are ignored.
		if requestID != e.pendingRequestID {
			return nil
		}
		e.clearPendingLocked()
		e.notify(model.SessionFailed, payload.SessionID, payload.Reason)
		return nil
	}
	if e.current == nil || e.current.id != payload.SessionID {
		return nil
	}
	e.cleanupLocked()
	e.notify(model.SessionClosed, payload.SessionID, payload.Reason)
	return nil
}

func (e *EngineerRuntime) clearPendingLocked() {
	e.pending = false
	e.pendingRequestID = ""
}

func (e *EngineerRuntime) Disconnect(ctx context.Context, reason string) error {
	e.mu.Lock()
	if e.current == nil {
		e.mu.Unlock()
		return nil
	}
	id := e.current.id
	e.mu.Unlock()
	return e.config.Control.Send(ctx, protocol.ControlStopSession, "", protocol.StopSessionPayload{SessionID: id, Reason: reason})
}

func (e *EngineerRuntime) inject(_ context.Context, sessionID uint64, packet []byte) error {
	if err := e.config.Device.InjectInbound(packet); err != nil {
		_ = e.config.Control.Send(context.Background(), protocol.ControlStopSession, "", protocol.StopSessionPayload{
			SessionID: sessionID, Reason: string(protocol.ErrorSessionInjectFailed),
		})
		e.mu.Lock()
		if e.current != nil && e.current.id == sessionID {
			e.cleanupLocked()
		}
		e.mu.Unlock()
		e.notify(model.SessionFailed, sessionID, string(protocol.ErrorSessionInjectFailed))
		return err
	}
	return nil
}

func (e *EngineerRuntime) run() {
	defer close(e.done)
	listenerErrors := make(chan error, 1)
	go func() { listenerErrors <- e.listener.Run(e.ctx) }()
	ticker := time.NewTicker(e.config.StatsInterval)
	defer ticker.Stop()
	for {
		select {
		case <-e.ctx.Done():
			_ = e.listener.Close()
			return
		case <-listenerErrors:
			e.cancel()
			return
		case <-ticker.C:
			e.reportStats()
		}
	}
}

func (e *EngineerRuntime) reportStats() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.current == nil || e.current.status != model.SessionActive {
		return
	}
	uploadBytes, uploadPackets := e.current.router.RemoteCounters()
	rxBytes, rxPackets := e.listener.SessionCounters(e.current.id)
	counters := model.SessionCounters{
		UploadBytes: uploadBytes, UploadPackets: uploadPackets,
		DownloadBytes: rxBytes, DownloadPackets: rxPackets,
	}
	_ = e.config.Control.Send(e.ctx, protocol.ControlSessionStats, "", protocol.SessionStatsPayload{SessionID: e.current.id, Counters: counters})
}

func (e *EngineerRuntime) cleanupLocked() {
	if e.current == nil {
		return
	}
	e.config.Device.SetPacketMux(nil)
	e.registry.Remove(e.current.id)
	if e.current.sender != nil {
		_ = e.current.sender.Close()
	}
	e.rollbackRoutes(e.current.routes)
	e.current = nil
}

func (e *EngineerRuntime) rollbackRoutes(prefixes []netip.Prefix) {
	for index := len(prefixes) - 1; index >= 0; index-- {
		_ = e.config.Routes.RemoveRemote(prefixes[index])
	}
}

func (e *EngineerRuntime) notify(status model.SessionStatus, id uint64, reason string) {
	if e.config.OnSession != nil {
		e.config.OnSession(status, id, reason)
	}
}

func (e *EngineerRuntime) Close() error {
	e.closeOnce.Do(func() {
		e.cancel()
		e.mu.Lock()
		e.cleanupLocked()
		e.mu.Unlock()
		_ = e.listener.Close()
		<-e.done
	})
	return nil
}
