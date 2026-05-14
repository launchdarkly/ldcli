package rollouts

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/launchdarkly/ldcli/internal/errors"
)

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

	// Phase 2 mutation-specific error codes (D-12).
	ErrCodeFlagNotConfiguredForRollout = "flag_not_configured_for_rollout"
	ErrCodeInvalidVariation            = "invalid_variation"
	ErrCodeRolloutAlreadyRunning       = "rollout_already_running"

	// Phase 3 status-specific error code (D-09): emitted when --rollout-id is absent and the flag has zero rollouts.
	ErrCodeNoRolloutsFound = "no_rollouts_found"
)

// RolloutError is the typed error returned from the rollouts client. The `Code` field maps to
// one of the ErrCode* constants above so callers can switch on it.
//
// `RawBody` is intentionally NOT serialized — it may contain sensitive upstream details that
// should not leak into the envelope on stdout (per T-02-02 in the plan threat model). The
// command-layer wrapper marshals `Code` / `Message` / `NextAction` into the envelope and
// discards `RawBody`.
type RolloutError struct {
	Code       string `json:"-"`
	Message    string `json:"-"`
	NextAction string `json:"-"`
	StatusCode int    `json:"-"`
	RawBody    []byte `json:"-"`
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

// apiErrorBody is the best-effort shape we attempt to unmarshal from upstream 4xx/5xx
// responses. Both fields are optional — if unmarshal fails or the body lacks them we fall
// back to status-code-derived strings.
type apiErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// mapAPIError converts an HTTP error response (status code >= 400) to a typed RolloutError
// with one of the documented error.code enum values. It is the single source of truth for
// the FOUND-08 status-code → error.code taxonomy. Per RESEARCH.md §"Mapping function":
//
//	401 → ErrCodeUnauthorized       (NextAction: ldcli config / login)
//	403 → ErrCodeForbidden          (NextAction: role / scope hint)
//	404 → ErrCodeNotFound           (NextAction: verify --flag/--project/--environment)
//	409 → ErrCodeConflict           (Message: pass through API message)
//	400 → ErrCodeBadRequest         (Message: pass through API message)
//	429 → ErrCodeRateLimited        (NextAction: retry after backoff)
//	5xx → ErrCodeUpstreamUnavailable (generic message; do NOT echo upstream body — T-02-03)
//	else → ErrCodeUnknownUpstream   (last-resort sentinel)
//
// `errors.SuggestionForStatus` from internal/errors/suggestions.go is consulted for 401/403/
// 404/409/429 to keep parity with existing CLI error envelopes; the RESEARCH-specified hint
// is used as fallback when SuggestionForStatus returns empty.
func mapAPIError(body []byte, statusCode int) error {
	e := &RolloutError{StatusCode: statusCode, RawBody: body}

	// Best-effort: attempt to unmarshal {"code": "...", "message": "..."} from the body.
	var apiBody apiErrorBody
	_ = json.Unmarshal(body, &apiBody)

	switch {
	case statusCode == http.StatusUnauthorized:
		e.Code = ErrCodeUnauthorized
		e.Message = "Access token rejected by LaunchDarkly"
		e.NextAction = suggestionOrFallback(statusCode,
			"Run `ldcli config --set access-token=<token>` or `ldcli login`")

	case statusCode == http.StatusForbidden:
		e.Code = ErrCodeForbidden
		// Mirror the 404 pattern: prefer the server's message when present so the real
		// cause (e.g. "LD-API-Version header required") is surfaced. Fall back to the
		// generic role/scope wording when the body lacks a message.
		if apiBody.Message != "" {
			e.Message = apiBody.Message
		} else {
			e.Message = "Access denied; token may lack required scope"
		}
		e.NextAction = suggestionOrFallback(statusCode,
			"Verify your access token's role includes the required permission/scope on the target project")

	case statusCode == http.StatusNotFound:
		e.Code = ErrCodeNotFound
		if apiBody.Message != "" {
			e.Message = apiBody.Message
		} else {
			e.Message = "Resource not found"
		}
		e.NextAction = suggestionOrFallback(statusCode,
			"Verify the --flag, --project, and --environment values are correct")

	case statusCode == http.StatusConflict:
		e.Code = ErrCodeConflict
		if apiBody.Message != "" {
			e.Message = apiBody.Message
		} else {
			e.Message = "Conflict"
		}
		e.NextAction = suggestionOrFallback(statusCode, "")

	// --- Phase 2 mutation-specific message matching (fires before the generic StatusBadRequest
	// branch) — server wraps instruction errors in a sempatch.NewInstructionError; the exact
	// HTTP status code (400, 409, or 422) is unconfirmed for some messages (RESEARCH A1), so
	// we match on message content first, regardless of status code. ---

	case strings.HasSuffix(apiBody.Message, " is off"):
		// "flag X is off" — server rejects startAutomatedRelease on a disabled flag.
		e.Code = ErrCodeFlagNotConfiguredForRollout
		e.Message = apiBody.Message
		e.NextAction = "Turn on the flag before starting a rollout"

	case strings.Contains(apiBody.Message, "Flag must not have ongoing guarded rollout"),
		strings.Contains(apiBody.Message, "Flag must not have ongoing progressive rollout"):
		e.Code = ErrCodeRolloutAlreadyRunning
		e.Message = apiBody.Message
		e.NextAction = "Stop the current rollout before starting a new one, or check the rollouts list for the active rollout"

	case strings.Contains(apiBody.Message, "instruction kind 'startAutomatedRelease' unsupported"):
		e.Code = ErrCodeBetaGateClosed
		e.Message = apiBody.Message
		e.NextAction = "Enable the release-guardian feature flag for this account in the LaunchDarkly UI"

	case strings.Contains(apiBody.Message, "originalVariationId must be a valid variation id"),
		strings.Contains(apiBody.Message, "instruction targetVariationId and originalVariationId must be different"):
		e.Code = ErrCodeInvalidVariation
		e.Message = apiBody.Message
		e.NextAction = "Pass the variation UUID (_id) from the flag definition, not the variation key; run: ldcli flags get --flag <key> --output json | jq '.variations[]'"

	// --- end Phase 2 mutation-specific block; generic status-code branches follow ---

	case statusCode == http.StatusBadRequest:
		e.Code = ErrCodeBadRequest
		if apiBody.Message != "" {
			e.Message = apiBody.Message
		} else {
			e.Message = "Bad request"
		}

	case statusCode == http.StatusTooManyRequests:
		e.Code = ErrCodeRateLimited
		e.Message = "Rate limited by LaunchDarkly"
		e.NextAction = suggestionOrFallback(statusCode,
			"Retry after the Retry-After interval, or reduce request rate")

	case statusCode >= 500 && statusCode < 600:
		e.Code = ErrCodeUpstreamUnavailable
		// Do NOT echo the upstream body for 5xx — it may contain sensitive infra detail
		// (T-02-03). Use a generic message keyed off the status code only.
		e.Message = fmt.Sprintf("LaunchDarkly returned %d %s", statusCode, http.StatusText(statusCode))
		e.NextAction = "Retry; if persistent, check the LaunchDarkly status page"

	default:
		e.Code = ErrCodeUnknownUpstream
		if apiBody.Message != "" {
			e.Message = apiBody.Message
		} else {
			e.Message = fmt.Sprintf("Unexpected upstream response: %d", statusCode)
		}
	}
	return e
}

// mapTransportError converts a transport-layer failure (network unreachable, TLS handshake
// failure, DNS lookup failure) — surfaced after retryablehttp has exhausted its retry
// envelope — to a typed RolloutError with Code = ErrCodeNetworkError.
func mapTransportError(err error) error {
	if err == nil {
		return nil
	}
	return &RolloutError{
		Code:       ErrCodeNetworkError,
		Message:    fmt.Sprintf("Network error: %v", err),
		NextAction: "Check connectivity and retry; if persistent, check firewall/proxy settings",
	}
}

// suggestionOrFallback returns the existing CLI-wide suggestion for the given status code
// (via internal/errors/suggestions.go) when available, falling back to the RESEARCH-specified
// rollouts-specific hint when SuggestionForStatus returns empty. The baseURI parameter is
// not threaded through here (the rollouts call site doesn't have it during error mapping);
// SuggestionForStatus tolerates an empty baseURI — the {baseURI} placeholder simply stays
// unsubstituted, which is acceptable for the rollouts error path.
func suggestionOrFallback(statusCode int, fallback string) string {
	if s := errors.SuggestionForStatus(statusCode, ""); s != "" {
		return s
	}
	return fallback
}
