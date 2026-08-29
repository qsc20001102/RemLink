package model

import (
	"fmt"
	"net/netip"
	"time"
)

// SessionStatus is the Server-authoritative Remote Subnet Session state.
type SessionStatus string

const (
	SessionCreating      SessionStatus = "CREATING"
	SessionPreparingSite SessionStatus = "PREPARING_SITE"
	SessionReady         SessionStatus = "READY"
	SessionActive        SessionStatus = "ACTIVE"
	SessionStopping      SessionStatus = "STOPPING"
	SessionClosed        SessionStatus = "CLOSED"
	SessionFailed        SessionStatus = "FAILED"
)

// SessionStatuses is the complete v1 state set.
var SessionStatuses = [...]SessionStatus{
	SessionCreating,
	SessionPreparingSite,
	SessionReady,
	SessionActive,
	SessionStopping,
	SessionClosed,
	SessionFailed,
}

// Valid reports whether the status belongs to the v1 state machine.
func (s SessionStatus) Valid() bool {
	switch s {
	case SessionCreating,
		SessionPreparingSite,
		SessionReady,
		SessionActive,
		SessionStopping,
		SessionClosed,
		SessionFailed:
		return true
	default:
		return false
	}
}

// ParseSessionStatus parses and validates a wire/storage Session state.
func ParseSessionStatus(value string) (SessionStatus, error) {
	status := SessionStatus(value)
	if !status.Valid() {
		return "", fmt.Errorf("invalid session status %q", value)
	}
	return status, nil
}

// SessionCounters are cumulative and use the Engineer point of view.
type SessionCounters struct {
	UploadBytes     uint64 `json:"upload_bytes"`
	DownloadBytes   uint64 `json:"download_bytes"`
	UploadPackets   uint64 `json:"upload_packets"`
	DownloadPackets uint64 `json:"download_packets"`
}

// Session is the Server-authoritative persisted Remote Subnet lifecycle.
type Session struct {
	ID             uint64          `json:"session_id,string"`
	EngineerNodeID string          `json:"engineer_node_id"`
	SiteNodeID     string          `json:"site_node_id"`
	Status         SessionStatus   `json:"status"`
	CIDRs          []netip.Prefix  `json:"cidrs"`
	CreatedAt      time.Time       `json:"created_at"`
	ActiveAt       *time.Time      `json:"active_at,omitempty"`
	ClosedAt       *time.Time      `json:"closed_at,omitempty"`
	ErrorCode      string          `json:"error_code,omitempty"`
	Counters       SessionCounters `json:"counters"`
}
