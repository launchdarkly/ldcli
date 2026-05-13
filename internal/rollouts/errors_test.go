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
