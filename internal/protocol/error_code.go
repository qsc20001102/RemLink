package protocol

// ErrorCode is a stable machine-readable RemLink failure identifier.
type ErrorCode string

const (
	ErrorServerUnreachable     ErrorCode = "SERVER_UNREACHABLE"
	ErrorJoinTokenInvalid      ErrorCode = "JOIN_TOKEN_INVALID"
	ErrorNodeAuthFailed        ErrorCode = "NODE_AUTH_FAILED"
	ErrorOverlayLocalConflict  ErrorCode = "OVERLAY_LOCAL_CONFLICT"
	ErrorEngineerSessionExists ErrorCode = "ENGINEER_SESSION_EXISTS"
	ErrorSiteOffline           ErrorCode = "SITE_OFFLINE"
	ErrorCIDRInvalid           ErrorCode = "CIDR_INVALID"
	ErrorCIDRLocalConflict     ErrorCode = "CIDR_LOCAL_CONFLICT"
	ErrorCIDROverlayConflict   ErrorCode = "CIDR_OVERLAY_CONFLICT"
	ErrorSiteNoRoute           ErrorCode = "SITE_NO_ROUTE"
	ErrorNetstackUnavailable   ErrorCode = "NETSTACK_UNAVAILABLE"
	ErrorFlowLimitReached      ErrorCode = "FLOW_LIMIT_REACHED"
	ErrorSessionTimeout        ErrorCode = "SESSION_TIMEOUT"
	ErrorSessionInjectFailed   ErrorCode = "SESSION_INJECT_FAILED"
)

// ErrorCodes is the authoritative 14-code v1 baseline.
var ErrorCodes = [...]ErrorCode{
	ErrorServerUnreachable,
	ErrorJoinTokenInvalid,
	ErrorNodeAuthFailed,
	ErrorOverlayLocalConflict,
	ErrorEngineerSessionExists,
	ErrorSiteOffline,
	ErrorCIDRInvalid,
	ErrorCIDRLocalConflict,
	ErrorCIDROverlayConflict,
	ErrorSiteNoRoute,
	ErrorNetstackUnavailable,
	ErrorFlowLimitReached,
	ErrorSessionTimeout,
	ErrorSessionInjectFailed,
}

// Valid reports whether the code belongs to the v1 baseline.
func (c ErrorCode) Valid() bool {
	switch c {
	case ErrorServerUnreachable,
		ErrorJoinTokenInvalid,
		ErrorNodeAuthFailed,
		ErrorOverlayLocalConflict,
		ErrorEngineerSessionExists,
		ErrorSiteOffline,
		ErrorCIDRInvalid,
		ErrorCIDRLocalConflict,
		ErrorCIDROverlayConflict,
		ErrorSiteNoRoute,
		ErrorNetstackUnavailable,
		ErrorFlowLimitReached,
		ErrorSessionTimeout,
		ErrorSessionInjectFailed:
		return true
	default:
		return false
	}
}
