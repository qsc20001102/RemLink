// Package model contains transport-independent RemLink domain values.
package model

import (
	"fmt"
	"net/netip"
	"time"
)

// NodeType identifies the role of a RemLink Windows node.
type NodeType string

const (
	NodeTypeEngineer NodeType = "engineer"
	NodeTypeSite     NodeType = "site"
)

// NodeTypes is the complete v1 node-role set.
var NodeTypes = [...]NodeType{
	NodeTypeEngineer,
	NodeTypeSite,
}

// Valid reports whether the node type is part of the v1 protocol.
func (t NodeType) Valid() bool {
	switch t {
	case NodeTypeEngineer, NodeTypeSite:
		return true
	default:
		return false
	}
}

// ParseNodeType parses and validates a wire/storage node type.
func ParseNodeType(value string) (NodeType, error) {
	t := NodeType(value)
	if !t.Valid() {
		return "", fmt.Errorf("invalid node type %q", value)
	}
	return t, nil
}

// NodeStatus is the Server-observed application status of a Windows node.
type NodeStatus string

const (
	NodeOnline   NodeStatus = "ONLINE"
	NodeUnstable NodeStatus = "UNSTABLE"
	NodeOffline  NodeStatus = "OFFLINE"
)

// Valid reports whether the status is part of the v1 heartbeat model.
func (s NodeStatus) Valid() bool {
	switch s {
	case NodeOnline, NodeUnstable, NodeOffline:
		return true
	default:
		return false
	}
}

// Node is the persistent Server-side identity and overlay allocation.
type Node struct {
	ID            string     `json:"node_id"`
	Type          NodeType   `json:"type"`
	Name          string     `json:"name"`
	OverlayIP     netip.Addr `json:"overlay_ip"`
	WGPublicKey   string     `json:"wg_public_key"`
	NodeTokenHash []byte     `json:"-"`
	Status        NodeStatus `json:"status"`
	Version       string     `json:"version"`
	OSVersion     string     `json:"os_version"`
	LastSeen      *time.Time `json:"last_seen,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}
