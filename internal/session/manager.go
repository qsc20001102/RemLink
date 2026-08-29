// Package session implements the Server-authoritative Remote Subnet state machine.
package session

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"time"

	"remlink/internal/database"
	"remlink/internal/localization"
	"remlink/internal/logging"
	"remlink/internal/model"
	"remlink/internal/protocol"
)

const DefaultPrepareTimeout = 30 * time.Second

type Store interface {
	GetNode(context.Context, string) (model.Node, error)
	CreateSession(context.Context, model.Session) error
	GetSession(context.Context, uint64) (model.Session, error)
	UpdateSessionStatus(context.Context, uint64, model.SessionStatus, string) error
	UpdateSessionStats(context.Context, uint64, model.SessionCounters) error
}

type Sender interface {
	Send(context.Context, string, protocol.ControlMessageType, any) error
	SendRequest(context.Context, string, protocol.ControlMessageType, string, any) error
}

type eventAppender interface {
	AppendEvent(context.Context, model.EventLog) error
}

type Config struct {
	OverlayCIDR    netip.Prefix
	MTU            int
	UDPPort        int
	PrepareTimeout time.Duration
}

type Manager struct {
	mu        sync.Mutex
	store     Store
	sender    Sender
	config    Config
	sessions  map[uint64]*runtimeSession
	migrating bool
}

type runtimeSession struct {
	session   model.Session
	requestID string
	timer     *time.Timer
}

func NewManager(store Store, sender Sender, config Config) (*Manager, error) {
	if store == nil || sender == nil {
		return nil, errors.New("Session Manager requires Store, Sender, and IPv4 Overlay CIDR")
	}
	if err := validateManagerConfig(config); err != nil {
		return nil, err
	}
	if config.PrepareTimeout <= 0 {
		config.PrepareTimeout = DefaultPrepareTimeout
	}
	return &Manager{store: store, sender: sender, config: config, sessions: make(map[uint64]*runtimeSession)}, nil
}

// ReconfigureNetwork switches the Server-authoritative values used by all
// subsequently created Sessions after an Admin network migration. Callers must
// close every nonterminal Session first; partially migrating live Sessions is
// deliberately unsupported by v1.
func (m *Manager) ReconfigureNetwork(overlayCIDR netip.Prefix, mtu, udpPort int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := Config{OverlayCIDR: overlayCIDR, MTU: mtu, UDPPort: udpPort, PrepareTimeout: m.config.PrepareTimeout}
	if err := validateManagerConfig(next); err != nil {
		return err
	}
	if len(m.sessions) != 0 {
		return errors.New("Session Manager network cannot change while Sessions are open")
	}
	m.config.OverlayCIDR = overlayCIDR.Masked()
	m.config.MTU = mtu
	m.config.UDPPort = udpPort
	return nil
}

// BeginNetworkMigration atomically quiesces CREATE_SESSION and closes every
// existing runtime Session. EndNetworkMigration must always be called by the
// orchestrator, including on rollback paths.
func (m *Manager) BeginNetworkMigration(ctx context.Context, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.migrating {
		return errors.New("Session Manager network migration is already active")
	}
	m.migrating = true
	for _, runtime := range m.sessions {
		if err := m.closeLocked(ctx, runtime, reason); err != nil {
			m.migrating = false
			return err
		}
	}
	return nil
}

// EndNetworkMigration re-enables CREATE_SESSION after commit or rollback.
func (m *Manager) EndNetworkMigration() {
	m.mu.Lock()
	m.migrating = false
	m.mu.Unlock()
}

func validateManagerConfig(config Config) error {
	if !config.OverlayCIDR.IsValid() || !config.OverlayCIDR.Addr().Is4() || config.OverlayCIDR != config.OverlayCIDR.Masked() || config.OverlayCIDR.Bits() == 0 {
		return errors.New("Session Manager requires a canonical IPv4 Overlay CIDR")
	}
	if config.MTU < 576 || config.MTU > 65535 || config.UDPPort < 1 || config.UDPPort > 65535 {
		return errors.New("Session Manager requires valid MTU and UDP port")
	}
	return nil
}

// HandleControl validates the authenticated sender role before changing state.
func (m *Manager) HandleControl(ctx context.Context, node model.Node, envelope protocol.ControlEnvelope) error {
	switch envelope.Type {
	case protocol.ControlCreateSession:
		if node.Type != model.NodeTypeEngineer {
			return errors.New("CREATE_SESSION is Engineer-only")
		}
		var payload protocol.CreateSessionPayload
		if err := envelope.DecodePayload(&payload); err != nil {
			return err
		}
		return m.create(ctx, node, envelope.RequestID, payload)
	case protocol.ControlPrepareResult:
		if node.Type != model.NodeTypeSite {
			return errors.New("PREPARE_RESULT is Site-only")
		}
		var payload protocol.PrepareResultPayload
		if err := envelope.DecodePayload(&payload); err != nil {
			return err
		}
		return m.prepareResult(ctx, node, payload)
	case protocol.ControlRoutesReady:
		if node.Type != model.NodeTypeEngineer {
			return errors.New("ROUTES_READY is Engineer-only")
		}
		var payload protocol.RoutesReadyPayload
		if err := envelope.DecodePayload(&payload); err != nil {
			return err
		}
		return m.routesReady(ctx, node, payload)
	case protocol.ControlStopSession:
		var payload protocol.StopSessionPayload
		if err := envelope.DecodePayload(&payload); err != nil {
			return err
		}
		return m.stop(ctx, node, payload)
	case protocol.ControlSessionStats:
		var payload protocol.SessionStatsPayload
		if err := envelope.DecodePayload(&payload); err != nil {
			return err
		}
		return m.stats(ctx, node, payload)
	default:
		return fmt.Errorf("%s is not a Node-to-Server Session message", envelope.Type)
	}
}

func (m *Manager) create(ctx context.Context, engineer model.Node, requestID string, payload protocol.CreateSessionPayload) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.migrating {
		return m.rejectLocked(ctx, engineer.ID, requestID, protocol.ErrorServerUnreachable)
	}
	if m.hasOpenEngineerLocked(engineer.ID) {
		return m.rejectLocked(ctx, engineer.ID, requestID, protocol.ErrorEngineerSessionExists)
	}
	site, err := m.store.GetNode(ctx, payload.SiteNodeID)
	if err != nil || site.Type != model.NodeTypeSite || site.Status != model.NodeOnline {
		return m.rejectLocked(ctx, engineer.ID, requestID, protocol.ErrorSiteOffline)
	}
	prefixes, code := validateCIDRs(payload.TargetCIDRs, m.config.OverlayCIDR)
	if code != "" {
		return m.rejectLocked(ctx, engineer.ID, requestID, code)
	}
	sessionID, err := m.createPersistentLocked(ctx, engineer.ID, site.ID, prefixes)
	if err != nil {
		return err
	}
	runtime := &runtimeSession{session: model.Session{
		ID: sessionID, EngineerNodeID: engineer.ID, SiteNodeID: site.ID,
		Status: model.SessionPreparingSite, CIDRs: prefixes, CreatedAt: time.Now().UTC(),
	}, requestID: requestID}
	m.sessions[sessionID] = runtime
	m.recordEvent(ctx, runtime, "INFO", "会话准备已开始", "")
	runtime.timer = time.AfterFunc(m.config.PrepareTimeout, func() { m.timeout(sessionID) })
	prepare := protocol.PrepareSessionPayload{
		SessionID: sessionID, EngineerOverlayIP: engineer.OverlayIP.String(), TargetCIDRs: prefixStrings(prefixes),
	}
	if err := m.sender.Send(ctx, site.ID, protocol.ControlPrepareSession, prepare); err != nil {
		return m.failLocked(ctx, runtime, protocol.ErrorSiteOffline)
	}
	return nil
}

func (m *Manager) createPersistentLocked(ctx context.Context, engineerID, siteID string, prefixes []netip.Prefix) (uint64, error) {
	for attempt := 0; attempt < 8; attempt++ {
		var raw [8]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return 0, fmt.Errorf("generate SessionID: %w", err)
		}
		id := binary.BigEndian.Uint64(raw[:])
		if id == 0 {
			continue
		}
		if _, err := m.store.GetSession(ctx, id); err == nil {
			continue
		} else if !errors.Is(err, database.ErrSessionNotFound) {
			return 0, err
		}
		session := model.Session{ID: id, EngineerNodeID: engineerID, SiteNodeID: siteID, Status: model.SessionCreating, CIDRs: prefixes}
		if err := m.store.CreateSession(ctx, session); err != nil {
			return 0, err
		}
		if err := m.store.UpdateSessionStatus(ctx, id, model.SessionPreparingSite, ""); err != nil {
			return 0, err
		}
		return id, nil
	}
	return 0, errors.New("could not allocate a unique random SessionID")
}

func (m *Manager) prepareResult(ctx context.Context, site model.Node, payload protocol.PrepareResultPayload) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	runtime := m.sessions[payload.SessionID]
	if runtime == nil || runtime.session.SiteNodeID != site.ID || runtime.session.Status != model.SessionPreparingSite {
		return errors.New("PREPARE_RESULT does not match a preparing Session")
	}
	if !payload.OK {
		code := payload.ErrorCode
		if !code.Valid() {
			code = protocol.ErrorNetstackUnavailable
		}
		return m.failLocked(ctx, runtime, code)
	}
	if payload.SubnetGatewayStatus != "netstack" || payload.TCPCapacity < 1 || payload.UDPCapacity < 1 {
		return m.failLocked(ctx, runtime, protocol.ErrorNetstackUnavailable)
	}
	if !routeResultsMatch(runtime.session.CIDRs, payload.RouteResults) {
		return m.failLocked(ctx, runtime, protocol.ErrorSiteNoRoute)
	}
	engineer, err := m.store.GetNode(ctx, runtime.session.EngineerNodeID)
	if err != nil {
		return m.failLocked(ctx, runtime, protocol.ErrorServerUnreachable)
	}
	runtime.stopTimer()
	if err := m.setStatusLocked(ctx, runtime, model.SessionReady, ""); err != nil {
		return err
	}
	config := protocol.SessionConfigPayload{
		SessionID: runtime.session.ID, PeerOverlayIP: site.OverlayIP.String(),
		CIDRs: prefixStrings(runtime.session.CIDRs), MTU: m.config.MTU, UDPPort: m.config.UDPPort,
	}
	if err := m.sender.SendRequest(ctx, engineer.ID, protocol.ControlSessionConfig, runtime.requestID, config); err != nil {
		return m.failLocked(ctx, runtime, protocol.ErrorServerUnreachable)
	}
	runtime.timer = time.AfterFunc(m.config.PrepareTimeout, func() { m.timeout(runtime.session.ID) })
	return nil
}

func (m *Manager) routesReady(ctx context.Context, engineer model.Node, payload protocol.RoutesReadyPayload) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	runtime := m.sessions[payload.SessionID]
	if runtime == nil || runtime.session.EngineerNodeID != engineer.ID || runtime.session.Status != model.SessionReady {
		return errors.New("ROUTES_READY does not match a ready Session")
	}
	runtime.stopTimer()
	if err := m.setStatusLocked(ctx, runtime, model.SessionActive, ""); err != nil {
		return err
	}
	active := protocol.SessionActivePayload{SessionID: runtime.session.ID}
	if err := m.sender.Send(ctx, runtime.session.SiteNodeID, protocol.ControlSessionActive, active); err != nil {
		return m.failLocked(ctx, runtime, protocol.ErrorSiteOffline)
	}
	if err := m.sender.Send(ctx, engineer.ID, protocol.ControlSessionActive, active); err != nil {
		return m.failLocked(ctx, runtime, protocol.ErrorServerUnreachable)
	}
	return nil
}

func (m *Manager) stop(ctx context.Context, node model.Node, payload protocol.StopSessionPayload) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	runtime := m.sessions[payload.SessionID]
	if runtime == nil {
		return nil
	}
	if node.ID != runtime.session.EngineerNodeID && node.ID != runtime.session.SiteNodeID {
		return errors.New("Node is not a Session participant")
	}
	return m.closeLocked(ctx, runtime, payload.Reason)
}

// Disconnect is used by the Admin API and follows the same bilateral cleanup path.
func (m *Manager) Disconnect(ctx context.Context, sessionID uint64, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	runtime := m.sessions[sessionID]
	if runtime == nil {
		return fmt.Errorf("%w: %d", database.ErrSessionNotFound, sessionID)
	}
	return m.closeLocked(ctx, runtime, reason)
}

func (m *Manager) DisconnectAll(ctx context.Context, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, runtime := range m.sessions {
		if err := m.closeLocked(ctx, runtime, reason); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) DisconnectNode(ctx context.Context, nodeID, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, runtime := range m.sessions {
		if runtime.session.EngineerNodeID == nodeID || runtime.session.SiteNodeID == nodeID {
			if err := m.closeLocked(ctx, runtime, reason); err != nil {
				return err
			}
		}
	}
	return nil
}

// HandleNodeStatusChange closes every Session involving a Node once the
// heartbeat authority classifies it as OFFLINE. A Site outage is surfaced to
// Engineer with the stable SITE_OFFLINE reason code.
func (m *Manager) HandleNodeStatusChange(ctx context.Context, node model.Node, status model.NodeStatus) error {
	if status != model.NodeOffline {
		return nil
	}
	reason := string(protocol.ErrorServerUnreachable)
	if node.Type == model.NodeTypeSite {
		reason = string(protocol.ErrorSiteOffline)
	}
	return m.DisconnectNode(ctx, node.ID, reason)
}

func (m *Manager) closeLocked(ctx context.Context, runtime *runtimeSession, reason string) error {
	runtime.stopTimer()
	if err := m.setStatusLocked(ctx, runtime, model.SessionStopping, ""); err != nil {
		return err
	}
	payload := protocol.StopSessionPayload{SessionID: runtime.session.ID, Reason: reason}
	_ = m.sender.Send(ctx, runtime.session.EngineerNodeID, protocol.ControlStopSession, payload)
	_ = m.sender.Send(ctx, runtime.session.SiteNodeID, protocol.ControlStopSession, payload)
	if err := m.setStatusLocked(ctx, runtime, model.SessionClosed, ""); err != nil {
		return err
	}
	delete(m.sessions, runtime.session.ID)
	return nil
}

func (m *Manager) stats(ctx context.Context, node model.Node, payload protocol.SessionStatsPayload) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	runtime := m.sessions[payload.SessionID]
	if runtime == nil || runtime.session.Status != model.SessionActive {
		return errors.New("SESSION_STATS requires an Active Session")
	}
	if node.ID != runtime.session.EngineerNodeID && node.ID != runtime.session.SiteNodeID {
		return errors.New("Node is not a Session participant")
	}
	// Engineer and Site report the same Engineer-view counters independently;
	// merge component-wise maxima so a delayed report can never move storage backward.
	merged := maxCounters(payload.Counters, runtime.session.Counters)
	if err := m.store.UpdateSessionStats(ctx, payload.SessionID, merged); err != nil {
		return err
	}
	runtime.session.Counters = merged
	return nil
}

func (m *Manager) timeout(sessionID uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	runtime := m.sessions[sessionID]
	if runtime == nil || (runtime.session.Status != model.SessionPreparingSite && runtime.session.Status != model.SessionReady) {
		return
	}
	_ = m.failLocked(context.Background(), runtime, protocol.ErrorSessionTimeout)
}

func (m *Manager) failLocked(ctx context.Context, runtime *runtimeSession, code protocol.ErrorCode) error {
	runtime.stopTimer()
	payload := protocol.StopSessionPayload{SessionID: runtime.session.ID, Reason: string(code)}
	// Preserve the CREATE_SESSION correlation until the Engineer has received
	// SESSION_CONFIG. In particular, PREPARE rejection/timeout happens while the
	// Engineer only has a pending request and does not yet know the SessionID.
	_ = m.sender.SendRequest(ctx, runtime.session.EngineerNodeID, protocol.ControlStopSession, runtime.requestID, payload)
	_ = m.sender.Send(ctx, runtime.session.SiteNodeID, protocol.ControlStopSession, payload)
	err := m.setStatusLocked(ctx, runtime, model.SessionFailed, string(code))
	delete(m.sessions, runtime.session.ID)
	return err
}

func (m *Manager) rejectLocked(ctx context.Context, engineerID, requestID string, code protocol.ErrorCode) error {
	return m.sender.SendRequest(ctx, engineerID, protocol.ControlStopSession, requestID,
		protocol.StopSessionPayload{Reason: string(code)})
}

func (m *Manager) setStatusLocked(ctx context.Context, runtime *runtimeSession, status model.SessionStatus, code string) error {
	if err := m.store.UpdateSessionStatus(ctx, runtime.session.ID, status, code); err != nil {
		return err
	}
	runtime.session.Status = status
	runtime.session.ErrorCode = code
	level := "INFO"
	if status == model.SessionFailed {
		level = "ERROR"
	}
	message := "会话状态变更为 " + localization.SessionStatus(string(status))
	if code != "" {
		message += "；原因：" + localization.Reason(code)
	}
	m.recordEvent(ctx, runtime, level, message, code)
	return nil
}

func (m *Manager) recordEvent(ctx context.Context, runtime *runtimeSession, level, message, code string) {
	appender, ok := m.store.(eventAppender)
	if !ok {
		return
	}
	fields, _ := json.Marshal(map[string]string{
		"engineer_node_id": runtime.session.EngineerNodeID,
		"site_node_id":     runtime.session.SiteNodeID,
		"error_code":       code,
	})
	_ = appender.AppendEvent(ctx, model.EventLog{
		Level: level, Module: string(logging.ModuleSession), SessionID: runtime.session.ID,
		Message: message, FieldsJSON: fields,
	})
}

func (m *Manager) hasOpenEngineerLocked(engineerID string) bool {
	for _, runtime := range m.sessions {
		if runtime.session.EngineerNodeID == engineerID {
			return true
		}
	}
	return false
}

func (r *runtimeSession) stopTimer() {
	if r.timer != nil {
		r.timer.Stop()
		r.timer = nil
	}
}

func validateCIDRs(raw []string, overlay netip.Prefix) ([]netip.Prefix, protocol.ErrorCode) {
	if len(raw) == 0 {
		return nil, protocol.ErrorCIDRInvalid
	}
	prefixes := make([]netip.Prefix, 0, len(raw))
	seen := make(map[netip.Prefix]struct{}, len(raw))
	for _, value := range raw {
		prefix, err := netip.ParsePrefix(value)
		if err != nil || !prefix.Addr().Is4() || prefix.Bits() == 0 || prefix != prefix.Masked() {
			return nil, protocol.ErrorCIDRInvalid
		}
		if prefixesOverlap(prefix, overlay) {
			return nil, protocol.ErrorCIDROverlayConflict
		}
		if _, duplicate := seen[prefix]; duplicate {
			return nil, protocol.ErrorCIDRInvalid
		}
		seen[prefix] = struct{}{}
		prefixes = append(prefixes, prefix)
	}
	return prefixes, ""
}

func routeResultsMatch(prefixes []netip.Prefix, results []protocol.RouteResult) bool {
	if len(prefixes) != len(results) {
		return false
	}
	wanted := make(map[string]struct{}, len(prefixes))
	for _, prefix := range prefixes {
		wanted[prefix.String()] = struct{}{}
	}
	for _, result := range results {
		if _, ok := wanted[result.CIDR]; !ok {
			return false
		}
		delete(wanted, result.CIDR)
		switch result.Result {
		case "DIRECT", "ROUTED":
		default:
			return false
		}
	}
	return len(wanted) == 0
}

func prefixStrings(prefixes []netip.Prefix) []string {
	values := make([]string, len(prefixes))
	for index, prefix := range prefixes {
		values[index] = prefix.String()
	}
	return values
}

func prefixesOverlap(left, right netip.Prefix) bool {
	return left.Contains(right.Addr()) || right.Contains(left.Addr())
}

func maxCounters(next, previous model.SessionCounters) model.SessionCounters {
	if next.UploadBytes < previous.UploadBytes {
		next.UploadBytes = previous.UploadBytes
	}
	if next.DownloadBytes < previous.DownloadBytes {
		next.DownloadBytes = previous.DownloadBytes
	}
	if next.UploadPackets < previous.UploadPackets {
		next.UploadPackets = previous.UploadPackets
	}
	if next.DownloadPackets < previous.DownloadPackets {
		next.DownloadPackets = previous.DownloadPackets
	}
	return next
}
