package analytics

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientFn_Tracker(t *testing.T) {
	t.Run("returns NoopClient when opted out", func(t *testing.T) {
		fn := ClientFn{ID: "test-id", Version: "1.0.0"}
		tracker := fn.Tracker("token", "https://app.launchdarkly.com", true)

		_, ok := tracker.(*NoopClient)
		assert.True(t, ok, "expected NoopClient when optOut is true")
	})

	t.Run("returns Client when not opted out", func(t *testing.T) {
		fn := ClientFn{ID: "test-id", Version: "1.0.0"}
		tracker := fn.Tracker("token", "https://app.launchdarkly.com", false)

		c, ok := tracker.(*Client)
		require.True(t, ok, "expected *Client when optOut is false")
		assert.Equal(t, "test-id", c.id)
		assert.Equal(t, "1.0.0", c.version)
		assert.Equal(t, "token", c.accessToken)
		assert.Equal(t, "https://app.launchdarkly.com", c.baseURI)
	})

	t.Run("passes AgentContext to Client", func(t *testing.T) {
		fn := ClientFn{ID: "test-id", Version: "1.0.0", AgentContext: "cursor"}
		tracker := fn.Tracker("token", "https://app.launchdarkly.com", false)

		c, ok := tracker.(*Client)
		require.True(t, ok)
		assert.Equal(t, "cursor", c.agentContext)
	})

	t.Run("AgentContext ignored when opted out", func(t *testing.T) {
		fn := ClientFn{ID: "test-id", Version: "1.0.0", AgentContext: "cursor"}
		tracker := fn.Tracker("token", "https://app.launchdarkly.com", true)

		_, ok := tracker.(*NoopClient)
		assert.True(t, ok, "expected NoopClient regardless of AgentContext when optOut is true")
	})
}

type trackingPayload struct {
	Event      string                 `json:"event"`
	Properties map[string]interface{} `json:"properties"`
}

func TestClient_SendEvent(t *testing.T) {
	t.Run("includes agent_context when set", func(t *testing.T) {
		var received trackingPayload
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &received)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		fn := ClientFn{ID: "test-id", Version: "1.0.0", AgentContext: "cursor"}
		tracker := fn.Tracker("test-token", server.URL, false)

		tracker.SendCommandRunEvent(map[string]interface{}{
			"name":   "flags",
			"action": "list",
		})
		tracker.Wait()

		assert.Equal(t, "CLI Command Run", received.Event)
		assert.Equal(t, "cursor", received.Properties["agent_context"])
		assert.Equal(t, "test-id", received.Properties["id"])
		assert.Equal(t, "flags", received.Properties["name"])
	})

	t.Run("omits agent_context when empty", func(t *testing.T) {
		var received trackingPayload
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &received)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		fn := ClientFn{ID: "test-id", Version: "1.0.0"}
		tracker := fn.Tracker("test-token", server.URL, false)

		tracker.SendCommandRunEvent(map[string]interface{}{
			"name":   "flags",
			"action": "list",
		})
		tracker.Wait()

		assert.Equal(t, "CLI Command Run", received.Event)
		_, hasAgentCtx := received.Properties["agent_context"]
		assert.False(t, hasAgentCtx, "agent_context should not be present when empty")
		assert.Equal(t, "test-id", received.Properties["id"])
	})

	t.Run("SendCommandCompletedEvent includes agent_context", func(t *testing.T) {
		var received trackingPayload
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &received)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		fn := ClientFn{ID: "test-id", Version: "1.0.0", AgentContext: "claude-code"}
		tracker := fn.Tracker("test-token", server.URL, false)

		tracker.SendCommandCompletedEvent("success")
		tracker.Wait()

		assert.Equal(t, "CLI Command Completed", received.Event)
		assert.Equal(t, "claude-code", received.Properties["agent_context"])
		assert.Equal(t, "success", received.Properties["outcome"])
	})

	t.Run("sends correct HTTP headers", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "test-token", r.Header.Get("Authorization"))
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			assert.Equal(t, "launchdarkly-cli/1.0.0", r.Header.Get("User-Agent"))
			assert.Equal(t, "POST", r.Method)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		fn := ClientFn{ID: "test-id", Version: "1.0.0"}
		tracker := fn.Tracker("test-token", server.URL, false)

		tracker.SendCommandRunEvent(map[string]interface{}{"name": "test"})
		tracker.Wait()
	})

	t.Run("posts to /internal/tracking path", func(t *testing.T) {
		var requestPath string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestPath = r.URL.Path
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		fn := ClientFn{ID: "test-id", Version: "1.0.0"}
		tracker := fn.Tracker("test-token", server.URL, false)

		tracker.SendCommandRunEvent(map[string]interface{}{"name": "test"})
		tracker.Wait()

		assert.Equal(t, "/internal/tracking", requestPath)
	})

	t.Run("SendSetupStepStartedEvent sends correct event name", func(t *testing.T) {
		var received trackingPayload
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &received)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		fn := ClientFn{ID: "test-id", Version: "1.0.0", AgentContext: "codex"}
		tracker := fn.Tracker("test-token", server.URL, false)

		tracker.SendSetupStepStartedEvent("connect")
		tracker.Wait()

		assert.Equal(t, "CLI Setup Step Started", received.Event)
		assert.Equal(t, "connect", received.Properties["step"])
		assert.Equal(t, "codex", received.Properties["agent_context"])
	})
}
