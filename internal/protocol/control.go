package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"remlink/internal/model"
)

// ErrRebootstrapRequired is returned by a Node runtime when all state derived
// from the public Bootstrap API must be rebuilt. Callers must tear down the
// current adapter, routes, WireGuard device, and Session runtime before retrying.
var ErrRebootstrapRequired = errors.New("Node rebootstrap is required")

// ControlMessageType identifies a Control WebSocket message.
type ControlMessageType string

const (
	ControlHello               ControlMessageType = "HELLO"
	ControlWelcome             ControlMessageType = "WELCOME"
	ControlNodeList            ControlMessageType = "NODE_LIST"
	ControlCreateSession       ControlMessageType = "CREATE_SESSION"
	ControlPrepareSession      ControlMessageType = "PREPARE_SESSION"
	ControlPrepareResult       ControlMessageType = "PREPARE_RESULT"
	ControlSessionConfig       ControlMessageType = "SESSION_CONFIG"
	ControlRoutesReady         ControlMessageType = "ROUTES_READY"
	ControlSessionActive       ControlMessageType = "SESSION_ACTIVE"
	ControlStopSession         ControlMessageType = "STOP_SESSION"
	ControlSessionStats        ControlMessageType = "SESSION_STATS"
	ControlHeartbeat           ControlMessageType = "HEARTBEAT"
	ControlRebootstrapRequired ControlMessageType = "REBOOTSTRAP_REQUIRED"
)

// ControlMessageTypes is the complete v1 Control message set.
var ControlMessageTypes = [...]ControlMessageType{
	ControlHello,
	ControlWelcome,
	ControlNodeList,
	ControlCreateSession,
	ControlPrepareSession,
	ControlPrepareResult,
	ControlSessionConfig,
	ControlRoutesReady,
	ControlSessionActive,
	ControlStopSession,
	ControlSessionStats,
	ControlHeartbeat,
	ControlRebootstrapRequired,
}

// Valid reports whether the message type is part of the v1 Control protocol.
func (t ControlMessageType) Valid() bool {
	switch t {
	case ControlHello,
		ControlWelcome,
		ControlNodeList,
		ControlCreateSession,
		ControlPrepareSession,
		ControlPrepareResult,
		ControlSessionConfig,
		ControlRoutesReady,
		ControlSessionActive,
		ControlStopSession,
		ControlSessionStats,
		ControlHeartbeat,
		ControlRebootstrapRequired:
		return true
	default:
		return false
	}
}

// ControlEnvelope is the stable v1 WebSocket framing shared by all messages.
type ControlEnvelope struct {
	Type      ControlMessageType `json:"type"`
	RequestID string             `json:"request_id,omitempty"`
	Payload   json.RawMessage    `json:"payload"`
}

// NewControlEnvelope serializes a typed payload.
func NewControlEnvelope(messageType ControlMessageType, requestID string, payload any) (ControlEnvelope, error) {
	if !messageType.Valid() {
		return ControlEnvelope{}, fmt.Errorf("invalid Control message type %q", messageType)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ControlEnvelope{}, fmt.Errorf("encode %s payload: %w", messageType, err)
	}
	return ControlEnvelope{Type: messageType, RequestID: requestID, Payload: raw}, nil
}

// DecodePayload strictly decodes exactly one JSON object.
func (e ControlEnvelope) DecodePayload(destination any) error {
	if !e.Type.Valid() {
		return fmt.Errorf("invalid Control message type %q", e.Type)
	}
	decoder := json.NewDecoder(bytes.NewReader(e.Payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode %s payload: %w", e.Type, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%s payload must contain one JSON value", e.Type)
	}
	return nil
}

type NodeCapabilities struct {
	RemoteSubnet   bool   `json:"remote_subnet"`
	NetstackStatus string `json:"netstack_status,omitempty"`
	TCPCapacity    int    `json:"tcp_capacity,omitempty"`
	UDPCapacity    int    `json:"udp_capacity,omitempty"`
}

type HelloPayload struct {
	NodeID        string           `json:"node_id"`
	NodeToken     string           `json:"node_token"`
	ConfigVersion uint64           `json:"config_version"`
	Capabilities  NodeCapabilities `json:"capabilities"`
	OSVersion     string           `json:"os_version"`
	Version       string           `json:"version"`
}

type WelcomePayload struct {
	ServerTime           time.Time `json:"server_time"`
	NetworkConfigVersion uint64    `json:"network_config_version"`
}

type HeartbeatPayload struct {
	Timestamp time.Time `json:"timestamp"`
	Status    string    `json:"status"`
}

type SiteSummary struct {
	NodeID                 string    `json:"node_id"`
	Name                   string    `json:"name"`
	OverlayIP              string    `json:"overlay_ip"`
	Online                 bool      `json:"online"`
	RemoteSubnetCapability bool      `json:"remote_subnet_capability"`
	LastSeen               time.Time `json:"last_seen,omitempty"`
}

type NodeListPayload struct {
	Sites []SiteSummary `json:"sites"`
}

// CreateSessionPayload is accepted only from an authenticated Engineer.
type CreateSessionPayload struct {
	SiteNodeID  string   `json:"site_node_id"`
	TargetCIDRs []string `json:"target_cidrs"`
}

type PrepareSessionPayload struct {
	SessionID         uint64   `json:"session_id"`
	EngineerOverlayIP string   `json:"engineer_overlay_ip"`
	TargetCIDRs       []string `json:"target_cidrs"`
}

type RouteResult struct {
	CIDR   string `json:"cidr"`
	Result string `json:"result"`
}

type PrepareResultPayload struct {
	SessionID           uint64        `json:"session_id"`
	OK                  bool          `json:"ok"`
	RouteResults        []RouteResult `json:"route_results"`
	SubnetGatewayStatus string        `json:"subnet_gateway_status"`
	TCPCapacity         int           `json:"tcp_capacity"`
	UDPCapacity         int           `json:"udp_capacity"`
	ErrorCode           ErrorCode     `json:"error_code,omitempty"`
	Error               string        `json:"error,omitempty"`
}

type SessionConfigPayload struct {
	SessionID     uint64   `json:"session_id"`
	PeerOverlayIP string   `json:"peer_overlay_ip"`
	CIDRs         []string `json:"cidrs"`
	MTU           int      `json:"mtu"`
	UDPPort       int      `json:"udp_port"`
}

type RoutesReadyPayload struct {
	SessionID uint64 `json:"session_id"`
}

type SessionActivePayload struct {
	SessionID uint64 `json:"session_id"`
}

type StopSessionPayload struct {
	SessionID uint64 `json:"session_id"`
	Reason    string `json:"reason"`
}

type SessionStatsPayload struct {
	SessionID uint64                `json:"session_id"`
	Counters  model.SessionCounters `json:"counters"`
}

type RebootstrapRequiredPayload struct {
	ConfigVersion uint64 `json:"config_version"`
	Reason        string `json:"reason"`
}
