// Package nodeagent composes Bootstrap, identity, Wintun, wireguard-go, and Control.
package nodeagent

import (
	"errors"
	"log/slog"
	"net/netip"
	"time"

	"remlink/internal/logging"
	"remlink/internal/model"
	"remlink/internal/protocol"
	sessionruntime "remlink/internal/session"
)

type Options struct {
	NodeType          model.NodeType
	NodeName          string
	ServerURL         string
	JoinToken         string
	IdentityPath      string
	Version           string
	Logger            *slog.Logger
	ApplicationLogger *logging.Logger
	Capabilities      protocol.NodeCapabilities
	TCPFlowLimit      int
	UDPFlowLimit      int
	UDPIdleTimeout    time.Duration
	OnOverlayReady    func(netip.Addr)
	OnControlState    func(bool)
	OnLatency         func(time.Duration)
	OnNodeList        func(protocol.NodeListPayload)
	OnSession         func(model.SessionStatus, uint64, string)
	OnEngineerReady   func(*sessionruntime.EngineerRuntime)
	OnSiteReady       func()
	OnRoute           func(uint64, netip.Prefix, string)
}

func (o Options) validate() error {
	if !o.NodeType.Valid() || o.NodeName == "" || o.ServerURL == "" || o.IdentityPath == "" {
		return errors.New("Node type, name, Server URL, and identity path are required")
	}
	return nil
}
