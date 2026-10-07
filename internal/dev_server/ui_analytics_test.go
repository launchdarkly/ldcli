package dev_server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/adrg/xdg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/dev_server/db"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
)

type recordingTracker struct {
	mu     sync.Mutex
	events []string
	props  []map[string]interface{}
}

func (r *recordingTracker) record(name string, properties map[string]interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	copied := map[string]interface{}{}
	for key, value := range properties {
		copied[key] = value
	}
	r.events = append(r.events, name)
	r.props = append(r.props, copied)
}

func (r *recordingTracker) SendCommandRunEvent(properties map[string]interface{}) {}
func (r *recordingTracker) SendCommandCompletedEvent(outcome string)              {}
func (r *recordingTracker) SendSetupStepStartedEvent(step string)                 {}
func (r *recordingTracker) SendSetupSDKSelectedEvent(sdk string)                  {}
func (r *recordingTracker) SendSetupFlagToggledEvent(on bool, count int, duration_ms int64) {
}
func (r *recordingTracker) SendDevServerUIEvent(name string, properties map[string]interface{}) {
	r.record(name, properties)
}
func (r *recordingTracker) Wait() {}

func (r *recordingTracker) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

func postUIAnalytics(handler http.Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, uiAnalyticsPath, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestUIAnalyticsRoute(t *testing.T) {
	t.Run("allowed body calls the tracker once", func(t *testing.T) {
		tracker := &recordingTracker{}
		handler := newUIAnalyticsHandler(tracker)
		rec := postUIAnalytics(handler, `{"event":"Dev Server UI Page Viewed","properties":{"page":"flags"}}`)

		assert.Equal(t, http.StatusNoContent, rec.Code)
		require.Equal(t, 1, tracker.count())
		assert.Equal(t, "Dev Server UI Page Viewed", tracker.events[0])
		assert.Equal(t, "flags", tracker.props[0]["page"])
	})

	t.Run("unknown name returns 400 and does not call the tracker", func(t *testing.T) {
		tracker := &recordingTracker{}
		handler := newUIAnalyticsHandler(tracker)
		rec := postUIAnalytics(handler, `{"event":"Dev Server UI Unknown","properties":{}}`)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Equal(t, 0, tracker.count())
	})

	t.Run("property that is not on the list returns 400", func(t *testing.T) {
		tracker := &recordingTracker{}
		handler := newUIAnalyticsHandler(tracker)
		rec := postUIAnalytics(handler, `{"event":"Dev Server UI Page Viewed","properties":{"page":"flags","flagKey":"secret"}}`)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Equal(t, 0, tracker.count())
		assert.NotContains(t, rec.Body.String(), "secret")
	})

	t.Run("body over 2048 bytes returns 400", func(t *testing.T) {
		tracker := &recordingTracker{}
		handler := newUIAnalyticsHandler(tracker)
		rec := postUIAnalytics(handler, strings.Repeat("a", uiAnalyticsMaxBody+1))

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Equal(t, 0, tracker.count())
	})

	t.Run("61st post in a minute returns 429", func(t *testing.T) {
		tracker := &recordingTracker{}
		handler := newUIAnalyticsHandler(tracker)
		body := `{"event":"Dev Server UI Events Stream Opened"}`
		var last *httptest.ResponseRecorder
		for i := 0; i < uiAnalyticsPerMinute+1; i++ {
			last = postUIAnalytics(handler, body)
		}

		assert.Equal(t, http.StatusTooManyRequests, last.Code)
		assert.Equal(t, uiAnalyticsPerMinute, tracker.count())
	})

	t.Run("opt out makes no outbound request", func(t *testing.T) {
		called := false
		tracking := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}))
		defer tracking.Close()

		tracker := analytics.ClientFn{ID: "test-id", Version: "1.0.0"}.Tracker("token", tracking.URL, true)
		handler := newUIAnalyticsHandler(tracker)
		rec := postUIAnalytics(handler, `{"event":"Dev Server UI Page Viewed","properties":{"page":"events"}}`)

		assert.Equal(t, http.StatusNoContent, rec.Code)
		tracker.Wait()
		assert.False(t, called)
	})
}

func TestRunServerUIAnalyticsAndOverride(t *testing.T) {
	seed := func(t *testing.T) {
		t.Helper()
		t.Setenv("XDG_STATE_HOME", t.TempDir())
		xdg.Reload()
		t.Cleanup(xdg.Reload)
		ctx := context.Background()
		dbPath, err := xdg.StateFile("ldcli/dev_server.db")
		require.NoError(t, err)
		store, err := db.NewSqlite(ctx, dbPath)
		require.NoError(t, err)
		err = store.InsertProject(ctx, model.Project{
			Key:                  "proj",
			SourceEnvironmentKey: "env",
			Context:              ldcontext.New("user"),
			LastSyncTime:         time.Now(),
			PayloadVersion:       1,
			AllFlagsState: model.FlagsState{
				"flag": {Value: ldvalue.Bool(false), Version: 1},
			},
		})
		require.NoError(t, err)
	}

	readOverride := func(t *testing.T) model.Overrides {
		t.Helper()
		ctx := context.Background()
		dbPath, err := xdg.StateFile("ldcli/dev_server.db")
		require.NoError(t, err)
		store, err := db.NewSqlite(ctx, dbPath)
		require.NoError(t, err)
		overrides, err := store.GetOverridesForProject(ctx, "proj")
		require.NoError(t, err)
		return overrides
	}

	putOverride := func(t *testing.T, handler http.Handler) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, "/dev/projects/proj/overrides/flag", strings.NewReader("true"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	t.Run("records one tracking post and the override when tracking fails", func(t *testing.T) {
		seed(t)
		var posts int
		tracking := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.ReadAll(r.Body)
			posts++
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer tracking.Close()

		tracker := analytics.ClientFn{ID: "e2e", Version: "test"}.Tracker("token", tracking.URL, false)
		handler, err := LDClient{cliVersion: "test"}.httpHandler(context.Background(), ServerParams{
			BaseURI:        "https://example.com",
			DevStreamURI:   "https://example.com",
			SdkInitTimeout: time.Second,
			Tracker:        tracker,
		})
		require.NoError(t, err)

		analyticsRec := postUIAnalytics(handler, `{"event":"Dev Server UI Page Viewed","properties":{"page":"flags"}}`)
		overrideRec := putOverride(t, handler)
		tracker.Wait()

		assert.Equal(t, http.StatusNoContent, analyticsRec.Code)
		assert.Equal(t, http.StatusOK, overrideRec.Code)
		assert.Equal(t, 1, posts)
		overrides := readOverride(t)
		require.Len(t, overrides, 1)
		assert.True(t, overrides[0].Active)
		assert.Equal(t, ldvalue.Bool(true), overrides[0].Value)
	})

	t.Run("opt out stores the override and does not post tracking", func(t *testing.T) {
		seed(t)
		var posts int
		tracking := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			posts++
			w.WriteHeader(http.StatusOK)
		}))
		defer tracking.Close()

		tracker := analytics.ClientFn{ID: "e2e", Version: "test"}.Tracker("token", tracking.URL, true)
		handler, err := LDClient{cliVersion: "test"}.httpHandler(context.Background(), ServerParams{
			BaseURI:        "https://example.com",
			DevStreamURI:   "https://example.com",
			SdkInitTimeout: time.Second,
			Tracker:        tracker,
		})
		require.NoError(t, err)

		analyticsRec := postUIAnalytics(handler, `{"event":"Dev Server UI Flag Override Set","properties":{"value_kind":"boolean","control":"switch","outcome":"success"}}`)
		overrideRec := putOverride(t, handler)
		tracker.Wait()

		assert.Equal(t, http.StatusNoContent, analyticsRec.Code)
		assert.Equal(t, http.StatusOK, overrideRec.Code)
		assert.Equal(t, 0, posts)
		overrides := readOverride(t)
		require.Len(t, overrides, 1)
		assert.True(t, overrides[0].Active)
	})
}
