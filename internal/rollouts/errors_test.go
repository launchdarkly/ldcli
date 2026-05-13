package rollouts

// Internal (white-box) test package so the unexported `mapAPIError` is callable directly.
// The httptest-based 4xx coverage in client_test.go exercises the full client path; this file
// targets the message-passthrough behavior for the 403 branch (mirroring the 404 pattern)
// without spinning up a server.

import (
	stderrors "errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMapAPIErrorPhase2MutationErrors covers the four new Phase 2 mutation-specific error codes
// added by Plan 02-02 (D-12: message-matching block before StatusBadRequest branch).
func TestMapAPIErrorPhase2MutationErrors(t *testing.T) {
	tests := []struct {
		name         string
		body         []byte
		statusCode   int
		expectedCode string
	}{
		{
			name:         "flag is off → flag_not_configured_for_rollout",
			body:         []byte(`{"code":"bad_request","message":"flag my-flag is off"}`),
			statusCode:   http.StatusBadRequest,
			expectedCode: ErrCodeFlagNotConfiguredForRollout,
		},
		{
			name:         "ongoing guarded rollout → rollout_already_running",
			body:         []byte(`{"code":"bad_request","message":"Flag must not have ongoing guarded rollout"}`),
			statusCode:   http.StatusBadRequest,
			expectedCode: ErrCodeRolloutAlreadyRunning,
		},
		{
			name:         "ongoing progressive rollout → rollout_already_running",
			body:         []byte(`{"code":"bad_request","message":"Flag must not have ongoing progressive rollout"}`),
			statusCode:   http.StatusBadRequest,
			expectedCode: ErrCodeRolloutAlreadyRunning,
		},
		{
			name:         "invalid original variation → invalid_variation",
			body:         []byte(`{"code":"bad_request","message":"originalVariationId must be a valid variation id 'true'"}`),
			statusCode:   http.StatusBadRequest,
			expectedCode: ErrCodeInvalidVariation,
		},
		{
			name:         "same variation IDs → invalid_variation",
			body:         []byte(`{"code":"bad_request","message":"instruction targetVariationId and originalVariationId must be different"}`),
			statusCode:   http.StatusBadRequest,
			expectedCode: ErrCodeInvalidVariation,
		},
		{
			name:         "beta gate closed → beta_gate_closed",
			body:         []byte(`{"code":"bad_request","message":"instruction kind 'startAutomatedRelease' unsupported"}`),
			statusCode:   http.StatusBadRequest,
			expectedCode: ErrCodeBetaGateClosed,
		},
		{
			// D-08: "default rule rollouts disabled" is a server-policy condition; falls through
			// to the generic bad_request branch (no dedicated code per D-08).
			name:         "default rule disabled falls through to bad_request",
			body:         []byte(`{"code":"bad_request","message":"Automated releases cannot be created on the default rule"}`),
			statusCode:   http.StatusBadRequest,
			expectedCode: ErrCodeBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := mapAPIError(tc.body, tc.statusCode)
			require.Error(t, err)

			var rerr *RolloutError
			require.True(t, stderrors.As(err, &rerr))
			assert.Equal(t, tc.expectedCode, rerr.Code, "wrong code for body %s", string(tc.body))
		})
	}
}

// TestMapAPIError403 covers both 403 paths (apiBody.Message populated → used; empty → fallback)
// so the LD-API-Version-required wording — and any other server-supplied 403 detail — surfaces
// to the caller rather than being swallowed by the generic "Access denied" string.
func TestMapAPIError403(t *testing.T) {
	t.Run("403 with apiBody.Message uses server message verbatim", func(t *testing.T) {
		body := []byte(`{"code":"forbidden","message":"LD-API-Version: beta header required"}`)
		err := mapAPIError(body, http.StatusForbidden)
		require.Error(t, err)

		var rerr *RolloutError
		require.True(t, stderrors.As(err, &rerr))
		assert.Equal(t, ErrCodeForbidden, rerr.Code)
		assert.Equal(t, "LD-API-Version: beta header required", rerr.Message)
		// NextAction is still populated from SuggestionForStatus (role/permission/scope hint).
		assert.Regexp(t, "(?i)(role|scope|permission)", rerr.NextAction)
		assert.Equal(t, http.StatusForbidden, rerr.StatusCode)
	})

	t.Run("403 with empty body falls back to generic role/scope wording", func(t *testing.T) {
		body := []byte(``)
		err := mapAPIError(body, http.StatusForbidden)
		require.Error(t, err)

		var rerr *RolloutError
		require.True(t, stderrors.As(err, &rerr))
		assert.Equal(t, ErrCodeForbidden, rerr.Code)
		assert.Equal(t, "Access denied; token may lack required scope", rerr.Message)
		assert.Regexp(t, "(?i)(role|scope|permission)", rerr.NextAction)
	})

	t.Run("403 with body missing message field falls back to generic wording", func(t *testing.T) {
		body := []byte(`{"code":"forbidden"}`)
		err := mapAPIError(body, http.StatusForbidden)
		require.Error(t, err)

		var rerr *RolloutError
		require.True(t, stderrors.As(err, &rerr))
		assert.Equal(t, ErrCodeForbidden, rerr.Code)
		assert.Equal(t, "Access denied; token may lack required scope", rerr.Message)
	})
}
