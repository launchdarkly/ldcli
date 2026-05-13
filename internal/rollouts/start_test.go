package rollouts_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/rollouts"
)

// twoStepServer builds a test server that dispatches by HTTP method:
// PATCH → patchStatus + patchBody; GET → getStatus + getBody.
// Returns the server and a shared *recordedRequest that tracks all calls.
func twoStepServer(t *testing.T, patchStatus int, patchBody string, getStatus int, getBody string) (*httptest.Server, *recordedRequest) {
	t.Helper()
	rec := &recordedRequest{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.calls++
		rec.lastPath = r.URL.Path
		rec.lastQuery = r.URL.RawQuery
		rec.lastHeaders = r.Header.Clone()
		rec.allPaths = append(rec.allPaths, r.URL.Path)
		rec.mu.Unlock()

		if r.Method == "PATCH" {
			w.WriteHeader(patchStatus)
			_, _ = w.Write([]byte(patchBody))
			return
		}
		w.WriteHeader(getStatus)
		_, _ = w.Write([]byte(getBody))
	}))

	return srv, rec
}

// twoStepServerWithHeaderCapture builds a two-step server that also exports PATCH and GET
// headers for per-call header assertion.
type capturedHeaders struct {
	mu           sync.Mutex
	patchHeaders http.Header
	getHeaders   http.Header
}

func twoStepServerWithHeaderCapture(t *testing.T, patchStatus int, patchBody string, getStatus int, getBody string) (*httptest.Server, *recordedRequest, *capturedHeaders) {
	t.Helper()
	rec := &recordedRequest{}
	cap := &capturedHeaders{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.calls++
		rec.lastPath = r.URL.Path
		rec.lastQuery = r.URL.RawQuery
		rec.lastHeaders = r.Header.Clone()
		rec.allPaths = append(rec.allPaths, r.URL.Path)
		rec.mu.Unlock()

		cap.mu.Lock()
		if r.Method == "PATCH" {
			cap.patchHeaders = r.Header.Clone()
		} else {
			cap.getHeaders = r.Header.Clone()
		}
		cap.mu.Unlock()

		if r.Method == "PATCH" {
			w.WriteHeader(patchStatus)
			_, _ = w.Write([]byte(patchBody))
			return
		}
		w.WriteHeader(getStatus)
		_, _ = w.Write([]byte(getBody))
	}))

	return srv, rec, cap
}

func TestStart_TwoStep_HappyPath(t *testing.T) {
	fixture := loadFixture(t, "start_success.json")
	srv, rec := twoStepServer(t, http.StatusOK, `{}`, http.StatusOK, fixture)
	defer srv.Close()

	c := rollouts.NewClient("test-version")
	instr := rollouts.StartInstruction{
		Kind:                "startAutomatedRelease",
		ReleaseKind:         "guarded",
		OriginalVariationID: "uuid-false",
		TargetVariationID:   "uuid-true",
		RandomizationUnit:   "user",
		Stages:              []rollouts.StageInput{{Allocation: 25000, DurationMillis: 300000}},
	}

	r, err := c.Start(context.Background(), "tok", srv.URL, "proj", "flagkey", "test", instr)
	require.NoError(t, err)
	require.NotNil(t, r)
	assert.Equal(t, "01JVTEST000000000000000001", r.ID)

	// Both PATCH and GET must have been called.
	calls, _, _, _ := rec.snapshot()
	assert.Equal(t, 2, calls, "expected exactly 2 HTTP calls (PATCH + GET)")
}

func TestStart_PatchHeaders(t *testing.T) {
	fixture := loadFixture(t, "start_success.json")
	srv, _, cap := twoStepServerWithHeaderCapture(t, http.StatusOK, `{}`, http.StatusOK, fixture)
	defer srv.Close()

	c := rollouts.NewClient("test-version")
	instr := rollouts.StartInstruction{Kind: "startAutomatedRelease"}

	_, err := c.Start(context.Background(), "my-token", srv.URL, "proj", "flag", "env", instr)
	require.NoError(t, err)

	// PATCH must carry the semantic-patch Content-Type.
	cap.mu.Lock()
	defer cap.mu.Unlock()
	assert.Equal(t, "application/json; domain-model=launchdarkly.semanticpatch", cap.patchHeaders.Get("Content-Type"),
		"PATCH must use semantic-patch Content-Type")
	assert.Equal(t, "beta", cap.patchHeaders.Get("LD-API-Version"),
		"PATCH must include LD-API-Version: beta")
	assert.Equal(t, "my-token", cap.patchHeaders.Get("Authorization"),
		"PATCH must include Authorization header")

	// GET re-fetch must NOT use the semantic-patch Content-Type.
	assert.Equal(t, "application/json", cap.getHeaders.Get("Content-Type"),
		"GET re-fetch must use standard Content-Type (not semantic-patch)")
	assert.Equal(t, "beta", cap.getHeaders.Get("LD-API-Version"),
		"GET re-fetch must include LD-API-Version: beta")
}

func TestStart_PatchBody(t *testing.T) {
	fixture := loadFixture(t, "start_success.json")

	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PATCH" {
			b := make([]byte, r.ContentLength)
			_, _ = r.Body.Read(b)
			capturedBody = b
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fixture))
	}))
	defer srv.Close()

	c := rollouts.NewClient("test-version")
	instr := rollouts.StartInstruction{
		Kind:                "startAutomatedRelease",
		ReleaseKind:         "progressive",
		OriginalVariationID: "orig-uuid",
		TargetVariationID:   "tgt-uuid",
		RandomizationUnit:   "user",
		Stages:              []rollouts.StageInput{{Allocation: 25000, DurationMillis: 3600000}},
	}

	_, err := c.Start(context.Background(), "tok", srv.URL, "proj", "flagkey", "myenv", instr)
	require.NoError(t, err)

	// Body must be a valid SemanticPatch.
	type semPatch struct {
		EnvironmentKey string            `json:"environmentKey"`
		Instructions   []json.RawMessage `json:"instructions"`
	}
	var patch semPatch
	require.NoError(t, json.Unmarshal(capturedBody, &patch),
		"PATCH body must be valid JSON: %s", string(capturedBody))
	assert.Equal(t, "myenv", patch.EnvironmentKey)
	require.Len(t, patch.Instructions, 1, "must have exactly one instruction")

	// Round-trip the instruction.
	var roundTripped rollouts.StartInstruction
	require.NoError(t, json.Unmarshal(patch.Instructions[0], &roundTripped))
	assert.Equal(t, instr.Kind, roundTripped.Kind)
	assert.Equal(t, instr.ReleaseKind, roundTripped.ReleaseKind)
	assert.Equal(t, instr.OriginalVariationID, roundTripped.OriginalVariationID)
	assert.Equal(t, instr.TargetVariationID, roundTripped.TargetVariationID)
}

func TestStart_EmptyRefetch_RetriesThenErrors(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Method == "PATCH" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		// GET always returns empty items.
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer srv.Close()

	// Use very short retry waits so the test doesn't take seconds.
	c := rollouts.NewClientWithRetryWaitsForTest("test-version", 1*time.Millisecond, 1*time.Millisecond)
	_, err := c.Start(context.Background(), "tok", srv.URL, "proj", "flag", "env", rollouts.StartInstruction{})

	require.Error(t, err)
	// Must have called PATCH once + GET 4 times (1 initial + 3 retries).
	assert.Equal(t, 5, callCount, "expected 1 PATCH + 4 GET attempts (1 initial + 3 retries)")

	var rerr *rollouts.RolloutError
	require.True(t, errors.As(err, &rerr), "error must be *RolloutError")
	assert.Equal(t, rollouts.ErrCodeUnknownUpstream, rerr.Code)
	assert.Contains(t, rerr.Message, "Start succeeded but rollout could not be fetched")
}

func TestStart_ErrorMessageMapping(t *testing.T) {
	tests := []struct {
		name         string
		message      string
		expectedCode string
	}{
		{
			name:         "flag is off",
			message:      "flag my-feature-flag is off",
			expectedCode: rollouts.ErrCodeFlagNotConfiguredForRollout,
		},
		{
			name:         "ongoing guarded rollout",
			message:      "Flag must not have ongoing guarded rollout",
			expectedCode: rollouts.ErrCodeRolloutAlreadyRunning,
		},
		{
			name:         "ongoing progressive rollout",
			message:      "Flag must not have ongoing progressive rollout",
			expectedCode: rollouts.ErrCodeRolloutAlreadyRunning,
		},
		{
			name:         "beta gate closed",
			message:      "instruction kind 'startAutomatedRelease' unsupported",
			expectedCode: rollouts.ErrCodeBetaGateClosed,
		},
		{
			name:         "invalid original variation",
			message:      "originalVariationId must be a valid variation id 'true'",
			expectedCode: rollouts.ErrCodeInvalidVariation,
		},
		{
			name:         "same variation IDs",
			message:      "instruction targetVariationId and originalVariationId must be different",
			expectedCode: rollouts.ErrCodeInvalidVariation,
		},
		{
			// D-08: "default rule rollouts disabled" maps to unknown_upstream (ErrCodeBadRequest via
			// status 400 with a message that doesn't match any Phase 2 substring).
			// Actual result: ErrCodeBadRequest since it's a 400 with an unmatched message.
			name:         "default rule disabled falls through to bad_request",
			message:      "Automated releases cannot be created on the default rule",
			expectedCode: rollouts.ErrCodeBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"code":"bad_request","message":"` + tc.message + `"}`
			srv, _ := makeServer(t, http.StatusBadRequest, body)
			defer srv.Close()

			c := rollouts.NewClientWithRetryWaitsForTest("test-version", 1*time.Millisecond, 1*time.Millisecond)
			_, err := c.Start(context.Background(), "tok", srv.URL, "proj", "flag", "env", rollouts.StartInstruction{})
			require.Error(t, err)

			var rerr *rollouts.RolloutError
			require.True(t, errors.As(err, &rerr), "error must be *RolloutError")
			assert.Equal(t, tc.expectedCode, rerr.Code, "wrong error code for message: %q", tc.message)
		})
	}
}

func TestStart_TransportError(t *testing.T) {
	c := rollouts.NewClientWithRetryWaitsForTest("test-version", 1*time.Millisecond, 1*time.Millisecond)
	// Port 1 is not likely to be open and will fail immediately.
	_, err := c.Start(context.Background(), "tok", "http://localhost:1", "proj", "flag", "env", rollouts.StartInstruction{})

	require.Error(t, err)
	var rerr *rollouts.RolloutError
	require.True(t, errors.As(err, &rerr), "transport error must be *RolloutError")
	assert.Equal(t, rollouts.ErrCodeNetworkError, rerr.Code)
}

func TestStart_5xxRetried(t *testing.T) {
	fixture := loadFixture(t, "start_success.json")
	// PATCH: fail once with 503, then succeed; GET: succeed immediately.
	patchAttempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PATCH" {
			patchAttempts++
			if patchAttempts <= 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"code":"service_unavailable","message":"try again"}`))
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fixture))
	}))
	defer srv.Close()

	c := rollouts.NewClientWithRetryWaitsForTest("test-version", 1*time.Millisecond, 1*time.Millisecond)
	r, err := c.Start(context.Background(), "tok", srv.URL, "proj", "flag", "env", rollouts.StartInstruction{})

	require.NoError(t, err)
	require.NotNil(t, r)
	assert.GreaterOrEqual(t, patchAttempts, 2, "PATCH should have been retried at least once after 503")
}
