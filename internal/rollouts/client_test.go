package rollouts_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/rollouts"
)

// recordedRequest captures the request envelope (path, query, headers) and the number of
// calls received so retry-envelope tests can assert exact counts.
type recordedRequest struct {
	mu          sync.Mutex
	calls       int
	lastPath    string
	lastQuery   string
	lastHeaders http.Header
	allPaths    []string
}

func (r *recordedRequest) snapshot() (int, string, string, http.Header) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls, r.lastPath, r.lastQuery, r.lastHeaders.Clone()
}

// makeServer returns a test server that responds with the given status code and body on
// every request. The returned *recordedRequest captures call count and last request shape.
func makeServer(t *testing.T, statusCode int, body string) (*httptest.Server, *recordedRequest) {
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
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(body))
	}))
	return srv, rec
}

// makeFlakyServer returns a server that responds with the failure status code for the first
// `failuresBeforeSuccess` calls, then 200 + successBody on every subsequent call.
func makeFlakyServer(t *testing.T, failureStatus, successStatus int, failuresBeforeSuccess int, successBody string) (*httptest.Server, *recordedRequest) {
	t.Helper()
	rec := &recordedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.calls++
		current := rec.calls
		rec.lastPath = r.URL.Path
		rec.lastQuery = r.URL.RawQuery
		rec.lastHeaders = r.Header.Clone()
		rec.allPaths = append(rec.allPaths, r.URL.Path)
		rec.mu.Unlock()
		if current <= failuresBeforeSuccess {
			w.WriteHeader(failureStatus)
			_, _ = w.Write([]byte(`{"code":"upstream_busy","message":"try again"}`))
			return
		}
		w.WriteHeader(successStatus)
		_, _ = w.Write([]byte(successBody))
	}))
	return srv, rec
}

func loadFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	require.NoError(t, err, "load fixture %s", name)
	return string(b)
}

func TestRolloutsClient(t *testing.T) {
	t.Run("List returns parsed RolloutList on 200 with progressive in_progress fixture", func(t *testing.T) {
		body := loadFixture(t, "list_progressive_in_progress.json")
		srv, _ := makeServer(t, http.StatusOK, body)
		defer srv.Close()

		c := rollouts.NewClient("test-version")
		list, err := c.List(context.Background(), "tok", srv.URL, "sandbox", "ship-it", rollouts.ListOpts{})

		require.NoError(t, err)
		require.NotNil(t, list)
		require.Greater(t, len(list.Items), 0, "fixture has at least one item")

		first := list.Items[0]
		assert.Equal(t, "progressive", first.Kind)
		assert.Equal(t, "in_progress", first.Status.Status)
		// Status block is decorated by DeriveStatusBlock
		assert.Contains(t, []string{"active", "regressed", "reverted", "paused", "completed"}, first.Status.Kind)
		assert.NotEmpty(t, first.Status.Label)
		// Timestamps converted from int64 unix-millis to time.Time
		assert.False(t, first.CreatedAt.IsZero())
	})

	t.Run("List sends correct URL path and query string", func(t *testing.T) {
		srv, rec := makeServer(t, http.StatusOK, `{"items":[]}`)
		defer srv.Close()
		c := rollouts.NewClient("test-version")

		_, err := c.List(context.Background(), "tok", srv.URL, "myproj", "myflag", rollouts.ListOpts{
			Environment: "prod",
			Limit:       5,
		})
		require.NoError(t, err)

		calls, path, query, _ := rec.snapshot()
		assert.Equal(t, 1, calls)
		assert.Equal(t, "/internal/projects/myproj/flags/myflag/automated-releases", path)
		assert.Contains(t, query, "filter=environmentKey%3Aprod")
		assert.Contains(t, query, "limit=5")
	})

	t.Run("List defaults limit to 20 when ListOpts.Limit is zero", func(t *testing.T) {
		srv, rec := makeServer(t, http.StatusOK, `{"items":[]}`)
		defer srv.Close()
		c := rollouts.NewClient("test-version")

		_, err := c.List(context.Background(), "tok", srv.URL, "p", "f", rollouts.ListOpts{})
		require.NoError(t, err)

		_, _, query, _ := rec.snapshot()
		assert.Contains(t, query, "limit=20")
	})

	t.Run("List sets limit=1000 when --all is passed", func(t *testing.T) {
		srv, rec := makeServer(t, http.StatusOK, `{"items":[]}`)
		defer srv.Close()
		c := rollouts.NewClient("test-version")

		_, err := c.List(context.Background(), "tok", srv.URL, "p", "f", rollouts.ListOpts{All: true})
		require.NoError(t, err)

		_, _, query, _ := rec.snapshot()
		assert.Contains(t, query, "limit=1000")
	})

	t.Run("List sends required headers and no Idempotency-Key on GET", func(t *testing.T) {
		srv, rec := makeServer(t, http.StatusOK, `{"items":[]}`)
		defer srv.Close()
		c := rollouts.NewClient("test-version")

		_, err := c.List(context.Background(), "my-access-token", srv.URL, "p", "f", rollouts.ListOpts{})
		require.NoError(t, err)

		_, _, _, headers := rec.snapshot()
		assert.Equal(t, "my-access-token", headers.Get("Authorization"))
		assert.Equal(t, "application/json", headers.Get("Content-Type"))
		assert.Equal(t, "launchdarkly-cli/vtest-version", headers.Get("User-Agent"))
		// LD-API-Version: beta is required by the internal automated-releases API; without it
		// the server returns 403 (real staging behavior confirmed in quick task 260513-i1u).
		assert.Equal(t, "beta", headers.Get("LD-API-Version"))
		assert.Empty(t, headers.Get("Idempotency-Key"), "GET must not send Idempotency-Key")
	})

	t.Run("List retries 5xx twice then succeeds (3 total requests)", func(t *testing.T) {
		srv, rec := makeFlakyServer(t, http.StatusServiceUnavailable, http.StatusOK, 2, `{"items":[]}`)
		defer srv.Close()

		// Use a client with shortened retry waits so the test completes quickly.
		c := rollouts.NewClientWithRetryWaitsForTest("test-version", 0, 0)
		list, err := c.List(context.Background(), "tok", srv.URL, "p", "f", rollouts.ListOpts{})
		require.NoError(t, err)
		require.NotNil(t, list)
		calls, _, _, _ := rec.snapshot()
		assert.Equal(t, 3, calls, "expected 2 failures + 1 success = 3 requests")
	})

	t.Run("List retries 5xx up to RetryMax then fails with upstream_unavailable (5 total requests)", func(t *testing.T) {
		srv, rec := makeServer(t, http.StatusServiceUnavailable, `{"code":"down","message":"db unreachable"}`)
		defer srv.Close()

		c := rollouts.NewClientWithRetryWaitsForTest("test-version", 0, 0)
		_, err := c.List(context.Background(), "tok", srv.URL, "p", "f", rollouts.ListOpts{})
		require.Error(t, err)

		var rerr *rollouts.RolloutError
		require.True(t, errors.As(err, &rerr), "expected *RolloutError")
		assert.Equal(t, rollouts.ErrCodeUpstreamUnavailable, rerr.Code)

		calls, _, _, _ := rec.snapshot()
		assert.Equal(t, 5, calls, "expected 1 initial + 4 retries = 5 requests")
	})

	t.Run("List does NOT retry 4xx (404 returns immediately, exactly 1 request)", func(t *testing.T) {
		srv, rec := makeServer(t, http.StatusNotFound, `{"code":"not_found","message":"no such flag"}`)
		defer srv.Close()

		c := rollouts.NewClient("test-version")
		_, err := c.List(context.Background(), "tok", srv.URL, "p", "f", rollouts.ListOpts{})
		require.Error(t, err)

		var rerr *rollouts.RolloutError
		require.True(t, errors.As(err, &rerr))
		assert.Equal(t, rollouts.ErrCodeNotFound, rerr.Code)

		calls, _, _, _ := rec.snapshot()
		assert.Equal(t, 1, calls, "4xx must not retry")
	})

	t.Run("List maps 401 to unauthorized with login NextAction", func(t *testing.T) {
		srv, _ := makeServer(t, http.StatusUnauthorized, `{"code":"unauthorized","message":"bad token"}`)
		defer srv.Close()

		c := rollouts.NewClient("test-version")
		_, err := c.List(context.Background(), "tok", srv.URL, "p", "f", rollouts.ListOpts{})

		var rerr *rollouts.RolloutError
		require.True(t, errors.As(err, &rerr))
		assert.Equal(t, rollouts.ErrCodeUnauthorized, rerr.Code)
		assert.NotEmpty(t, rerr.Message)
		// NextAction should hint at the access-token recovery path
		assert.Regexp(t, "(?i)(access[- ]?token|login)", rerr.NextAction)
	})

	t.Run("List maps 403 to forbidden with role/scope NextAction", func(t *testing.T) {
		srv, _ := makeServer(t, http.StatusForbidden, `{"code":"forbidden","message":"missing scope"}`)
		defer srv.Close()

		c := rollouts.NewClient("test-version")
		_, err := c.List(context.Background(), "tok", srv.URL, "p", "f", rollouts.ListOpts{})

		var rerr *rollouts.RolloutError
		require.True(t, errors.As(err, &rerr))
		assert.Equal(t, rollouts.ErrCodeForbidden, rerr.Code)
		assert.Regexp(t, "(?i)(role|scope|permission)", rerr.NextAction)
	})

	t.Run("List maps 404 to not_found with flag/project NextAction", func(t *testing.T) {
		srv, _ := makeServer(t, http.StatusNotFound, `{"code":"not_found","message":"no flag"}`)
		defer srv.Close()

		c := rollouts.NewClient("test-version")
		_, err := c.List(context.Background(), "tok", srv.URL, "p", "f", rollouts.ListOpts{})

		var rerr *rollouts.RolloutError
		require.True(t, errors.As(err, &rerr))
		assert.Equal(t, rollouts.ErrCodeNotFound, rerr.Code)
		assert.Regexp(t, "(?i)(--flag|--project|--environment|flag key|project key)", rerr.NextAction)
	})

	t.Run("List maps 409 to conflict", func(t *testing.T) {
		srv, _ := makeServer(t, http.StatusConflict, `{"code":"conflict","message":"already running"}`)
		defer srv.Close()

		c := rollouts.NewClient("test-version")
		_, err := c.List(context.Background(), "tok", srv.URL, "p", "f", rollouts.ListOpts{})

		var rerr *rollouts.RolloutError
		require.True(t, errors.As(err, &rerr))
		assert.Equal(t, rollouts.ErrCodeConflict, rerr.Code)
	})

	t.Run("List maps 429 to rate_limited (exactly 1 request — not retried by ldcli policy)", func(t *testing.T) {
		// Note: retryablehttp's DefaultRetryPolicy DOES retry 429. We don't want to wait through
		// the retry envelope in this test; configure zero-wait retries so the test stays fast,
		// and assert the error.code mapping rather than asserting "exactly 1 request".
		srv, _ := makeServer(t, http.StatusTooManyRequests, `{"code":"rate_limited","message":"slow down"}`)
		defer srv.Close()

		c := rollouts.NewClientWithRetryWaitsForTest("test-version", 0, 0)
		_, err := c.List(context.Background(), "tok", srv.URL, "p", "f", rollouts.ListOpts{})

		var rerr *rollouts.RolloutError
		require.True(t, errors.As(err, &rerr))
		assert.Equal(t, rollouts.ErrCodeRateLimited, rerr.Code)
	})

	t.Run("List maps 400 to bad_request and surfaces API message", func(t *testing.T) {
		srv, _ := makeServer(t, http.StatusBadRequest, `{"code":"bad_request","message":"invalid filter syntax"}`)
		defer srv.Close()

		c := rollouts.NewClient("test-version")
		_, err := c.List(context.Background(), "tok", srv.URL, "p", "f", rollouts.ListOpts{})

		var rerr *rollouts.RolloutError
		require.True(t, errors.As(err, &rerr))
		assert.Equal(t, rollouts.ErrCodeBadRequest, rerr.Code)
		assert.Contains(t, rerr.Message, "invalid filter syntax")
	})

	t.Run("List converts int64 unix-millis timestamps to RFC 3339 UTC on JSON marshal", func(t *testing.T) {
		body := loadFixture(t, "list_progressive_in_progress.json")
		srv, _ := makeServer(t, http.StatusOK, body)
		defer srv.Close()

		c := rollouts.NewClient("test-version")
		list, err := c.List(context.Background(), "tok", srv.URL, "sandbox", "ship-it", rollouts.ListOpts{})
		require.NoError(t, err)
		require.Greater(t, len(list.Items), 0)

		// 1715353200000 ms = 2024-05-10T15:00:00Z
		assert.Equal(t, int64(1715353200), list.Items[0].CreatedAt.Unix())
		assert.Equal(t, "UTC", list.Items[0].CreatedAt.Location().String())

		// Round-trip through JSON marshal — must emit RFC 3339 UTC
		raw, err := json.Marshal(list.Items[0])
		require.NoError(t, err)
		assert.Contains(t, string(raw), `"createdAt":"2024-05-10T15:00:00Z"`)
	})

	t.Run("List converts stage durationMillis to Go duration string 15m0s", func(t *testing.T) {
		body := loadFixture(t, "list_progressive_in_progress.json")
		srv, _ := makeServer(t, http.StatusOK, body)
		defer srv.Close()

		c := rollouts.NewClient("test-version")
		list, err := c.List(context.Background(), "tok", srv.URL, "sandbox", "ship-it", rollouts.ListOpts{})
		require.NoError(t, err)
		require.Greater(t, len(list.Items), 0)
		require.Greater(t, len(list.Items[0].Stages), 0)

		// 900000 ms = 15m0s
		assert.Equal(t, int64(900000), list.Items[0].Stages[0].DurationMillis)
		assert.Equal(t, "15m0s", list.Items[0].Stages[0].Duration)
	})

	t.Run("List populates Status block via DeriveStatusBlock on guarded regressed fixture", func(t *testing.T) {
		body := loadFixture(t, "list_guarded_regressed.json")
		srv, _ := makeServer(t, http.StatusOK, body)
		defer srv.Close()

		c := rollouts.NewClient("test-version")
		list, err := c.List(context.Background(), "tok", srv.URL, "sandbox", "checkout-rewrite", rollouts.ListOpts{})
		require.NoError(t, err)
		require.Greater(t, len(list.Items), 0)

		item := list.Items[0]
		assert.Equal(t, "monitoring_regressed", item.Status.Status)
		assert.Equal(t, "regressed", item.Status.Kind)
		assert.NotEmpty(t, item.Status.Label)
		assert.Contains(t, item.Status.Label, "Regressions detected")
		assert.Contains(t, item.Status.Label, "latency-p99")
	})

	t.Run("Get sends correct URL path with environment in path and parses single rollout", func(t *testing.T) {
		body := loadFixture(t, "get_guarded_completed.json")
		srv, rec := makeServer(t, http.StatusOK, body)
		defer srv.Close()

		c := rollouts.NewClient("test-version")
		r, err := c.Get(context.Background(), "tok", srv.URL, "sandbox", "production", "01HXZ9GRD0099")
		require.NoError(t, err)
		require.NotNil(t, r)

		calls, path, _, _ := rec.snapshot()
		assert.Equal(t, 1, calls)
		assert.Equal(t, "/internal/projects/sandbox/environments/production/automated-releases/01HXZ9GRD0099", path)

		assert.Equal(t, "01HXZ9GRD0099", r.ID)
		assert.Equal(t, "guarded", r.Kind)
		assert.Equal(t, "completed", r.Status.Status)
		assert.Equal(t, "completed", r.Status.Kind)
	})
}
