package agent

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupAgentFlag is the flag behind an agent-mode AI Config, as the stream
// sends it.
const setupAgentFlag = `{"key":"ldcli-setup-agent","on":true,"version":1,"salt":"s",
"fallthrough":{"variation":0},"offVariation":0,
"variations":[{"_ldMeta":{"enabled":true,"variationKey":"v1","mode":"agent","version":1},
"model":{"name":"claude-sonnet-5"},"provider":{"name":"anthropic"},
"instructions":"From AgentControl."}]}`

// TestAIConfigSource_RealSDKThroughTheProxy starts the real AI SDK through the
// loopback proxy against a fake relay. It checks that the SDK uses the relay
// paths, sends the placeholder key and the token, and reads the config.
func TestAIConfigSource_RealSDKThroughTheProxy(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.Method+" "+r.URL.Path)
		mu.Unlock()
		if r.Header.Get(RelayTokenHeader) != "api-123" || r.Header.Get("Authorization") != RelayPlaceholderKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path == RelayPath+"/polling/sdk/latest-all" {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set(RelayEnvironmentIDHeader, "catfood-client-id")
			fmt.Fprintf(w, `{"flags":{"ldcli-setup-agent":%s},"segments":{}}`, strings.ReplaceAll(setupAgentFlag, "\n", ""))
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer relay.Close()
	t.Setenv(environmentIDEnv, "the-users-own-app")

	source := NewAIConfigSource(relay.URL, "api-123")
	closer := source.(*AIConfigSource)
	defer func() { _ = closer.Close(context.Background()) }()

	cfg, err := source.Resolve(context.Background(), Task{SDKID: "node-server"})

	require.NoError(t, err)
	assert.Equal(t, Config{ProviderID: "anthropic", ModelID: "claude-sonnet-5", Instructions: "From AgentControl."}, cfg)
	assert.Equal(t, "catfood-client-id", os.Getenv(environmentIDEnv), "the ID from the relay header reaches the SDK")
	require.NoError(t, closer.Close(context.Background()))
	mu.Lock()
	defer mu.Unlock()
	for _, p := range paths {
		assert.Contains(t, p, RelayPath+"/", "every request goes to a relay route")
	}
	joined := strings.Join(paths, "\n")
	assert.Contains(t, joined, "GET "+RelayPath+"/polling/sdk/latest-all", "the SDK polls")
	assert.NotContains(t, joined, RelayPath+"/stream/", "the SDK does not open the flag stream")
	t.Logf("requests: %v", paths)
}

// TestAIConfigSource_RelayRefuses_FallsBack checks that a refusal, such as the
// kill switch being off, ends setup's wait at once and uses the built-in
// configuration.
func TestAIConfigSource_RelayRefuses_FallsBack(t *testing.T) {
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"code":"ldcli_setup_agent_disabled"}`)
	}))
	defer relay.Close()

	source := NewAIConfigSource(relay.URL, "api-123").(*AIConfigSource)
	defer func() { _ = source.Close(context.Background()) }()

	start := time.Now()
	cfg, err := source.Resolve(context.Background(), Task{})

	require.NoError(t, err)
	assert.Equal(t, Config{Instructions: DefaultInstructions}, cfg)
	assert.Less(t, time.Since(start), time.Second, "a refusal does not wait for the SDK start timeout")
	var refusal *RelayError
	require.ErrorAs(t, source.initErr, &refusal)
	assert.Equal(t, "ldcli_setup_agent_disabled", refusal.Code)
}

func TestAIConfigSource_RelayUnreachable_FallsBack(t *testing.T) {
	relay := httptest.NewServer(http.NotFoundHandler())
	url := relay.URL
	relay.Close()

	source := NewAIConfigSource(url, "api-123").(*AIConfigSource)
	defer func() { _ = source.Close(context.Background()) }()

	start := time.Now()
	cfg, err := source.Resolve(context.Background(), Task{})

	require.NoError(t, err)
	assert.Equal(t, Config{Instructions: DefaultInstructions}, cfg)
	assert.Less(t, time.Since(start), time.Second)
}
