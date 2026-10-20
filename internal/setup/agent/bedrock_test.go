package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/go-ai-sdk/aiserver"
)

func TestOpenCodeConfig_ManagedModelIsTheOnlyModel(t *testing.T) {
	data, err := openCodeConfig(&managedModel{Endpoint: "http://127.0.0.1:1/secret", ModelID: "anthropic.claude-sonnet-4-6"})
	require.NoError(t, err)
	var cfg map[string]any
	require.NoError(t, json.Unmarshal(data, &cfg))

	assert.Equal(t, []any{"amazon-bedrock"}, cfg["enabled_providers"], "the user's own providers are off")
	assert.Equal(t, "amazon-bedrock/anthropic.claude-sonnet-4-6", cfg["model"])
	assert.Equal(t, "amazon-bedrock/anthropic.claude-sonnet-4-6", cfg["small_model"], "title generation uses the same model")
	options := cfg["provider"].(map[string]any)["amazon-bedrock"].(map[string]any)["options"].(map[string]any)
	assert.Equal(t, "http://127.0.0.1:1/secret", options["endpoint"])
	assert.Equal(t, "us-east-1", options["region"], "a US region makes OpenCode use the US inference profile")
	assert.Contains(t, cfg, "permission", "the permission rules stay")
}

func TestOpenCodeConfig_WithoutAManagedModelHasNoProviderSettings(t *testing.T) {
	data, err := openCodeConfig(nil)
	require.NoError(t, err)
	var cfg map[string]any
	require.NoError(t, json.Unmarshal(data, &cfg))

	for _, key := range []string{"enabled_providers", "model", "small_model", "provider"} {
		assert.NotContains(t, cfg, key)
	}
}

func TestRelayProxy_BedrockRouteReachesOnlyTheModelRelay(t *testing.T) {
	relay, got := fakeRelay(t)
	p := startTestProxy(t, relay.URL)

	req, err := http.NewRequest(http.MethodPost, p.bedrockURL+"/model/us.anthropic.claude-sonnet-4-6/converse-stream", strings.NewReader(`{"messages":[]}`))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+RelayPlaceholderKey)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, BedrockRelayPath+"/model/us.anthropic.claude-sonnet-4-6/converse-stream", got.URL.Path)
	assert.Equal(t, "api-secret-token", got.Header.Get(RelayTokenHeader))
	assert.Empty(t, got.Header.Get("Authorization"), "the placeholder bearer token does not reach the relay")
	assert.NotEqual(t, p.baseURL, p.bedrockURL, "the model route has its own secret path")

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/model/us.anthropic.claude-sonnet-4-6/converse-stream"},
		{http.MethodPost, "/polling/sdk/latest-all"},
		{http.MethodPost, ""},
	} {
		req, err := http.NewRequest(tc.method, p.bedrockURL+tc.path, nil)
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
		assert.Equal(t, http.StatusNotFound, resp.StatusCode, "%s %s", tc.method, tc.path)
	}
}

func TestWithoutPrefix_RemovesTheUsersAWSVariables(t *testing.T) {
	got := withoutPrefix([]string{"AWS_PROFILE=work", "AWS_ACCESS_KEY_ID=AKIA", "HOME=/Users/me", "PATH=/bin"}, "AWS_")

	assert.Equal(t, []string{"HOME=/Users/me", "PATH=/bin"}, got)
}

func TestAIConfigSource_ManagedModel(t *testing.T) {
	s, _ := fakeSource(nil, enabled(agentConfig("Bedrock", "anthropic.claude-sonnet-4-6", "Wire the call.")))
	defer func() { _ = s.Close(context.Background()) }()

	m, err := s.managedModel(context.Background(), Task{SDKID: "python-server-sdk"})

	require.NoError(t, err)
	assert.Equal(t, "anthropic.claude-sonnet-4-6", m.ModelID)
	assert.Equal(t, s.proxy.bedrockURL, m.Endpoint)
}

func TestAIConfigSource_ManagedModel_Refusals(t *testing.T) {
	for name, res := range map[string]aiserver.InspectResult{
		"another provider": enabled(agentConfig("Anthropic", "claude-sonnet-5", "Wire the call.")),
		"config off":       {Enabled: false},
	} {
		t.Run(name, func(t *testing.T) {
			s, _ := fakeSource(nil, res)
			defer func() { _ = s.Close(context.Background()) }()

			_, err := s.managedModel(context.Background(), Task{})

			assert.Error(t, err, "setup must not fall back to the user's own provider")
		})
	}
}

// TestBedrockRoute_RealOpenCode starts the real OpenCode, in the sandbox on
// macOS, with the managed model configuration. It checks that OpenCode sends
// its model calls through the proxy to the model relay, with the US
// inference profile and without the placeholder bearer token.
func TestBedrockRoute_RealOpenCode(t *testing.T) {
	if _, err := exec.LookPath("opencode"); err != nil {
		t.Skip("OpenCode is not installed")
	}
	if testing.Short() {
		t.Skip("starts OpenCode")
	}
	var mu sync.Mutex
	var paths []string
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		paths = append(paths, r.Method+" "+r.URL.Path+" token="+r.Header.Get(RelayTokenHeader)+" auth="+r.Header.Get("Authorization"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"message":"fake relay"}`)
	}))
	defer relay.Close()
	p := startTestProxy(t, relay.URL)

	config, err := openCodeConfig(&managedModel{Endpoint: p.bedrockURL, ModelID: "anthropic.claude-sonnet-4-6"})
	require.NoError(t, err)
	srv, err := startServer(context.Background(), "opencode", t.TempDir(), config, map[string]string{bedrockTokenEnv: RelayPlaceholderKey})
	require.NoError(t, err)
	defer srv.stop()
	run, err := startRun(context.Background(), srv.client, Config{ProviderID: bedrockProviderID, ModelID: "anthropic.claude-sonnet-4-6", Instructions: "Say hi."}, "Say hi.")
	require.NoError(t, err)
	defer run.Close()

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(paths) > 0
	}, 60*time.Second, 200*time.Millisecond, "OpenCode calls the model relay")
	time.Sleep(2 * time.Second)
	mu.Lock()
	defer mu.Unlock()
	for _, got := range paths {
		assert.Equal(t, "POST "+BedrockRelayPath+"/model/us.anthropic.claude-sonnet-4-6/converse-stream token=api-secret-token auth=", got)
	}
}
