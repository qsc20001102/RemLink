// Package subnetgateway defines the Site userspace subnet backend boundary.
package subnetgateway

import (
	"context"
	"net/netip"
)

type SessionConfig struct {
	SessionID         uint64
	EngineerOverlayIP netip.Addr
	RemoteCIDRs       []netip.Prefix
}

type SubnetGateway interface {
	Prepare(context.Context, SessionConfig) error
	InjectIPv4(context.Context, uint64, []byte) error
	CloseSession(context.Context, uint64) error
}
