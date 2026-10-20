package agent

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/launchdarkly/go-ai-sdk/aiserver"
)

func handlerRequest() aiserver.HandlerRequest {
	cfg := aiserver.AiConfig{Instructions: "Wire the AI call.", Model: aiserver.Model{Name: "claude-sonnet-5"}}
	cfg.Provider.Name = "Anthropic"
	return aiserver.HandlerRequest{Config: cfg, UserInput: "Facts from ldcli setup"}
}

func eventsOf(evs ...Event) <-chan Event {
	ch := make(chan Event, len(evs))
	for _, e := range evs {
		ch <- e
	}
	close(ch)
	return ch
}

func TestOpenCodeHandler_StreamsAndRecordsTheRun(t *testing.T) {
	spans := tracetest.NewSpanRecorder()
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)))
	defer otel.SetTracerProvider(prev)

	var got SessionPrompt
	h := NewOpenCodeHandler(func(_ context.Context, p SessionPrompt) (<-chan Event, error) {
		got = p
		return eventsOf(
			Event{Kind: EventTool, Tool: ToolCall{ID: "c1", Name: "edit", Status: "running"}},
			Event{Kind: EventText, Text: "Wired "},
			Event{Kind: EventTool, Tool: ToolCall{ID: "c1", Name: "edit", Status: "completed"}},
			Event{Kind: EventUsage, Usage: Usage{ModelID: "claude-sonnet-5", InputTokens: 100, OutputTokens: 20}},
			Event{Kind: EventText, Text: "main.py."},
			Event{Kind: EventUsage, Usage: Usage{ModelID: "claude-sonnet-5", InputTokens: 50, OutputTokens: 10}},
			Event{Kind: EventDone},
		), nil
	})

	var chunks []string
	var done aiserver.HandlerStreamEvent
	for ev, err := range h.Stream(context.Background(), handlerRequest()) {
		require.NoError(t, err)
		if ev.Type == aiserver.EventChunk {
			chunks = append(chunks, ev.Text)
		} else {
			done = ev
		}
	}

	assert.Equal(t, SessionPrompt{System: "Wire the AI call.", ProviderID: "anthropic", ModelID: "claude-sonnet-5", Text: "Facts from ldcli setup"}, got)
	assert.Equal(t, []string{"Wired ", "main.py."}, chunks)
	require.Equal(t, aiserver.EventDone, done.Type)
	assert.Equal(t, "Wired main.py.", *done.Output)
	assert.Equal(t, aiserver.TokenUsage{Input: 150, Output: 30, Total: 180}, aiserver.ParseUsage(done.Usage))

	var names []string
	for _, s := range spans.Ended() {
		names = append(names, s.Name())
	}
	assert.ElementsMatch(t, []string{HandlerSpanName, "execute_tool edit", "chat claude-sonnet-5", "chat claude-sonnet-5"}, names)
}

func TestOpenCodeHandler_ErrorEndsTheRun(t *testing.T) {
	h := NewOpenCodeHandler(func(context.Context, SessionPrompt) (<-chan Event, error) {
		return eventsOf(Event{Kind: EventError, Text: "no API key"}), nil
	})

	var gotErr error
	for _, err := range h.Stream(context.Background(), handlerRequest()) {
		if err != nil {
			gotErr = err
		}
	}

	require.Error(t, gotErr)
	assert.Contains(t, gotErr.Error(), "no API key")
}

func TestOpenCodeHandler_StreamWithoutAnEndIsAnError(t *testing.T) {
	h := NewOpenCodeHandler(func(context.Context, SessionPrompt) (<-chan Event, error) {
		return eventsOf(Event{Kind: EventText, Text: "partial"}), nil
	})

	var gotErr error
	for _, err := range h.Stream(context.Background(), handlerRequest()) {
		if err != nil {
			gotErr = err
		}
	}

	assert.Error(t, gotErr)
}

// TestRun_ThroughTheAISDK runs the agent the way setup does for a logged-in
// user: the real AI SDK behind the proxy reads the config from a fake relay,
// calls the OpenCode handler, and the handler drives a fake OpenCode session.
func TestRun_ThroughTheAISDK(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == RelayPath+"/polling/sdk/latest-all" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"flags":{"ldcli-setup-agent":`+strings.ReplaceAll(setupAgentFlag, "\n", "")+`},"segments":{}}`)
			return
		}
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, r.URL.Path+" "+string(body))
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	defer relay.Close()
	sdk := NewAIConfigSource(relay.URL, "api-123").(*AIConfigSource)
	require.NoError(t, sdk.Ready(context.Background()))

	oc := newFakeOpenCode(t)
	run, err := startSession(context.Background(), oc.client())
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	run.cancelSDK = cancel
	run.sdkDone = make(chan struct{})
	go run.driveWithSDK(ctx, sdk, Task{SDKID: "python-server-sdk", FlagKey: "my-new-flag"})

	require.Eventually(t, func() bool {
		oc.mu.Lock()
		defer oc.mu.Unlock()
		return oc.prompt != nil
	}, 5*time.Second, 10*time.Millisecond, "the handler sends the prompt")
	oc.mu.Lock()
	assert.Equal(t, "From AgentControl.", oc.prompt["system"], "the system prompt comes from the config")
	assert.Equal(t, map[string]any{"providerID": "anthropic", "modelID": "claude-sonnet-5"}, oc.prompt["model"])
	oc.mu.Unlock()

	oc.emit(`{"type":"message.updated","properties":{"info":{"id":"m1","sessionID":"ses","role":"assistant","providerID":"anthropic","modelID":"claude-sonnet-5","time":{"created":1,"completed":2},"tokens":{"input":100,"output":20}}}}`)
	oc.emit(`{"type":"session.idle","properties":{"sessionID":"ses"}}`)

	var kinds []EventKind
	for ev := range run.Events() {
		kinds = append(kinds, ev.Kind)
	}
	assert.Equal(t, []EventKind{EventUsage, EventDone}, kinds, "the wizard still gets every event")

	require.NoError(t, run.Close())
	require.NoError(t, sdk.Close(context.Background()))
	mu.Lock()
	defer mu.Unlock()
	all := strings.Join(bodies, "\n")
	for _, b := range bodies {
		path, rest, _ := strings.Cut(b, " ")
		var kinds []string
		for _, k := range []string{"$ld:ai:feedback", "$ld:ai:tokens:total", "$ld:ai:generation:success", "$ld:ai:duration:total", "$ld:ai:sdk:info", "$ld:ai:usage"} {
			if strings.Contains(rest, k) {
				kinds = append(kinds, k)
			}
		}
		t.Logf("relay got %s (%d bytes) %v", path, len(rest), kinds)
	}
	assert.Contains(t, all, RelayPath+"/events/bulk", "the SDK sends its events through the relay")
	assert.Contains(t, all, "$ld:ai:tokens:total", "the run's tokens are tracked against the AI Config")
	assert.Contains(t, all, "$ld:ai:generation:success", "the run's result is tracked")
	assert.Contains(t, all, RelayPath+"/otel/v1/traces", "the run's spans go out through the relay")
}
