package rollouts

import "fmt"

// error.code enum values per FOUND-08 / D-01. The structured `error.code` taxonomy lives in the
// JSON envelope; exit code stays 1 for any error (D-01). Plan 02 wires `mapAPIError` /
// `mapTransportError` to populate these from real HTTP responses.
const (
	ErrCodeUnauthorized        = "unauthorized"
	ErrCodeForbidden           = "forbidden"
	ErrCodeNotFound            = "not_found"
	ErrCodeBadRequest          = "bad_request"
	ErrCodeConflict            = "conflict"
	ErrCodeRateLimited         = "rate_limited"
	ErrCodeUpstreamUnavailable = "upstream_unavailable"
	ErrCodeNetworkError        = "network_error"
	ErrCodeBetaGateClosed      = "beta_gate_closed"
	ErrCodeUnknownUpstream     = "unknown_upstream"
)

// RolloutError is the typed error returned from the rollouts client. The `Code` field maps to
// one of the ErrCode* constants above so callers can switch on it.
//
// `RawBody` is intentionally NOT serialized — it may contain sensitive upstream details that
// should not leak into the envelope on stdout (per T-01-02 in the plan threat model). The
// command-layer wrapper marshals `Code` / `Message` / `NextAction` into the envelope and
// discards `RawBody`.
type RolloutError struct {
	Code       string
	Message    string
	NextAction string
	StatusCode int
	RawBody    []byte
}

// Error implements the standard error interface, returning the user-facing message.
func (e *RolloutError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// Is allows callers to use errors.As/Is with a *RolloutError sentinel.
func (e *RolloutError) Is(target error) bool {
	if _, ok := target.(*RolloutError); ok {
		return true
	}
	return false
}

// mapAPIError is the Phase 1 skeleton — it returns a RolloutError with
// Code=ErrCodeUnknownUpstream and a placeholder message that references the upstream status
// code. Plan 02 replaces the body with a full status-code → error.code mapping table.
func mapAPIError(body []byte, statusCode int) error {
	return &RolloutError{
		Code:       ErrCodeUnknownUpstream,
		Message:    fmt.Sprintf("Phase 2 will refine; upstream returned %d", statusCode),
		StatusCode: statusCode,
		RawBody:    body,
	}
}

// mapTransportError is the Phase 1 skeleton — it returns a RolloutError with
// Code=ErrCodeNetworkError and the underlying error's message. Plan 02 distinguishes timeouts,
// DNS failures, and TLS errors.
func mapTransportError(err error) error {
	if err == nil {
		return nil
	}
	return &RolloutError{
		Code:    ErrCodeNetworkError,
		Message: err.Error(),
	}
}
