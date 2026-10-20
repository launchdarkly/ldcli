package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildPrompt_NamesTheTaskAndNoCredential(t *testing.T) {
	prompt := BuildPrompt(Task{
		SDKID:          "node-server",
		Language:       "JavaScript",
		PackageManager: "npm",
		EntryPoint:     "index.js",
		FlagKey:        "my-new-flag",
		SDKInstalled:   true,
	})

	assert.Contains(t, prompt, `"node-server"`)
	assert.Contains(t, prompt, `"my-new-flag"`)
	assert.Contains(t, prompt, "- Entry point: index.js")
	assert.Contains(t, prompt, "- Framework: unknown")
	assert.Contains(t, prompt, "already installed")
	assert.Contains(t, prompt, "environment variable "+SDKKeyEnvVar)
	assert.NotContains(t, prompt, MobileKeyEnvVar, "the prompt names only the SDK's own credential")
	assert.NotContains(t, prompt, "did not find an entry point")
}

func TestCredentialEnvVar(t *testing.T) {
	assert.Equal(t, SDKKeyEnvVar, CredentialEnvVar("python-server-sdk"))
	assert.Equal(t, ClientSideIDEnvVar, CredentialEnvVar("js-client-sdk"))
	assert.Equal(t, MobileKeyEnvVar, CredentialEnvVar("swift-client-sdk"))
}

func TestBuildFacts_GivesFactsAndNoTask(t *testing.T) {
	facts := BuildFacts(Task{SDKID: "python-server-sdk", Language: "Python", EntryPoint: "main.py", FlagKey: "my-new-flag", SDKInstalled: true})

	assert.Contains(t, facts, "- SDK: python-server-sdk")
	assert.Contains(t, facts, "- Entry point: main.py")
	assert.Contains(t, facts, "- SDK package installed: yes")
	assert.Contains(t, facts, "- Flag created by setup: my-new-flag")
	assert.Contains(t, facts, "- Environment variable for the SDK credential: LAUNCHDARKLY_SDK_KEY")
	assert.NotContains(t, facts, "Evaluate")
}

func TestBuildPrompt_NoEntryPoint_AsksTheAgentToFindOne(t *testing.T) {
	prompt := BuildPrompt(Task{SDKID: "go-server-sdk", FlagKey: "f"})

	assert.Contains(t, prompt, "did not find an entry point")
	assert.Contains(t, prompt, "is not installed")
}

func TestChildEnv_RemovesLDVariablesAndSetsOwn(t *testing.T) {
	env := childEnv(
		[]string{"PATH=/bin", "LD_ACCESS_TOKEN=api-secret", "ld_base_uri=x", "OPENCODE_SERVER_PASSWORD=old", "HOME=/home/me"},
		map[string]string{"OPENCODE_SERVER_PASSWORD": "new"},
	)

	assert.ElementsMatch(t, []string{"PATH=/bin", "HOME=/home/me", "OPENCODE_SERVER_PASSWORD=new"}, env)
}

func TestOpenCodeConfig_RunsCommandsWithoutApprovals(t *testing.T) {
	data, err := openCodeConfig(nil)
	require.NoError(t, err)

	var cfg struct {
		Permission map[string]json.RawMessage `json:"permission"`
	}
	require.NoError(t, json.Unmarshal(data, &cfg))
	assert.JSONEq(t, `"allow"`, string(cfg.Permission["edit"]))
	assert.JSONEq(t, `"allow"`, string(cfg.Permission["question"]), "the agent can still ask for information")
	assert.JSONEq(t, `"deny"`, string(cfg.Permission["external_directory"]))
	assert.JSONEq(t, `"deny"`, string(cfg.Permission["webfetch"]))

	bash := string(cfg.Permission["bash"])
	assert.True(t, strings.HasPrefix(bash, `{"*":"allow",`), "the catch-all comes first, because the last matching rule wins: %s", bash)
	for _, denied := range []string{"git push*", "git commit*", "git reset*", "sudo *", "rm -rf /*"} {
		assert.Contains(t, bash, `"`+denied+`":"deny"`)
	}
}

func TestVersionAtLeast(t *testing.T) {
	assert.True(t, versionAtLeast("1.19.0", "1.19.0"))
	assert.True(t, versionAtLeast("v1.19.3", "1.19.0"))
	assert.True(t, versionAtLeast("opencode 2.0.1\n", "1.19.0"))
	assert.False(t, versionAtLeast("1.8.9", "1.19.0"))
	assert.False(t, versionAtLeast("dev", "1.19.0"))
}

func TestCheck_NotInstalled(t *testing.T) {
	o := &OpenCode{lookPath: func(string) (string, error) { return "", fmt.Errorf("not found") }}

	assert.ErrorIs(t, o.Check(context.Background()), ErrNotInstalled)
}

func TestCheck_TooOld(t *testing.T) {
	o := &OpenCode{
		lookPath: func(string) (string, error) { return "/bin/opencode", nil },
		version:  func(context.Context, string) (string, error) { return "1.2.0", nil },
	}

	err := o.Check(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too old")
}

func TestCheck_Supported(t *testing.T) {
	o := &OpenCode{
		lookPath: func(string) (string, error) { return "/bin/opencode", nil },
		version:  func(context.Context, string) (string, error) { return "1.19.3", nil },
	}

	assert.NoError(t, o.Check(context.Background()))
}

func TestReadSSE_JoinsDataLinesAndSkipsOtherFields(t *testing.T) {
	stream := "event: message\nid: 1\ndata: {\"a\":\ndata: 1}\n\n: comment\ndata: {\"b\":2}\n\n"
	var got []string

	err := readSSE(strings.NewReader(stream), func(data []byte) bool {
		got = append(got, string(data))
		return true
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"{\"a\":\n1}", "{\"b\":2}"}, got)
}

func decodeAll(d *decoder, events ...string) []Event {
	var out []Event
	for _, e := range events {
		out = append(out, d.decode([]byte(e))...)
	}
	return out
}

func TestDecoder_TextDeltasOnlyForTextParts(t *testing.T) {
	d := newDecoder("ses")

	out := decodeAll(d,
		`{"type":"message.part.updated","properties":{"part":{"id":"p1","sessionID":"ses","type":"reasoning"}}}`,
		`{"type":"message.part.delta","properties":{"sessionID":"ses","partID":"p1","field":"text","delta":"thinking"}}`,
		`{"type":"message.part.updated","properties":{"part":{"id":"p2","sessionID":"ses","type":"text"}}}`,
		`{"type":"message.part.delta","properties":{"sessionID":"ses","partID":"p2","field":"text","delta":"Hello"}}`,
	)

	require.Len(t, out, 1)
	assert.Equal(t, Event{Kind: EventText, Text: "Hello"}, out[0])
}

func TestDecoder_IgnoresOtherSessions(t *testing.T) {
	d := newDecoder("ses")

	out := decodeAll(d,
		`{"type":"message.part.updated","properties":{"part":{"id":"p1","sessionID":"other","type":"tool","callID":"c1","tool":"bash","state":{"status":"running"}}}}`,
		`{"type":"permission.asked","properties":{"id":"per1","sessionID":"other","permission":"bash","patterns":["rm -rf /"]}}`,
		`{"type":"session.idle","properties":{"sessionID":"other"}}`,
	)

	assert.Empty(t, out)
}

func TestDecoder_ToolStatusChangesOnce(t *testing.T) {
	d := newDecoder("ses")
	tool := func(status string) string {
		return fmt.Sprintf(`{"type":"message.part.updated","properties":{"part":{"id":"p1","sessionID":"ses","type":"tool","callID":"c1","tool":"edit","state":{"status":%q,"title":"index.js"}}}}`, status)
	}

	out := decodeAll(d, tool("pending"), tool("running"), tool("running"), tool("completed"))

	require.Len(t, out, 2)
	assert.Equal(t, ToolCall{ID: "c1", Name: "edit", Status: "running", Title: "index.js"}, out[0].Tool)
	assert.Equal(t, "completed", out[1].Tool.Status)
}

func TestDecoder_Permission(t *testing.T) {
	d := newDecoder("ses")

	out := decodeAll(d, `{"type":"permission.asked","properties":{"id":"per1","sessionID":"ses","permission":"bash","patterns":["npm test"]}}`)

	require.Len(t, out, 1)
	assert.Equal(t, EventPermission, out[0].Kind)
	assert.Equal(t, PermissionRequest{ID: "per1", SessionID: "ses", Permission: "bash", Patterns: []string{"npm test"}}, out[0].Permission)
}

func TestDecoder_Question(t *testing.T) {
	d := newDecoder("ses")

	out := decodeAll(d, `{"type":"question.asked","properties":{"id":"que1","sessionID":"ses","questions":[{"question":"Where?","header":"Secret","options":[{"label":".env","description":"gitignored"}]}]}}`)

	require.Len(t, out, 1)
	assert.Equal(t, EventQuestion, out[0].Kind)
	assert.Equal(t, QuestionRequest{ID: "que1", SessionID: "ses", Questions: []Question{{
		Header: "Secret", Text: "Where?", Options: []QuestionOption{{Label: ".env", Description: "gitignored"}},
	}}}, out[0].Question)
}

func TestDecoder_LegacyPermission(t *testing.T) {
	d := newDecoder("ses")

	out := decodeAll(d, `{"type":"permission.updated","properties":{"id":"per1","sessionID":"ses","type":"bash","pattern":"npm test","title":"Run npm test"}}`)

	require.Len(t, out, 1)
	assert.True(t, out[0].Permission.legacy)
	assert.Equal(t, []string{"npm test"}, out[0].Permission.Patterns)
}

func TestDecoder_UsageOncePerFinishedReply(t *testing.T) {
	d := newDecoder("ses")
	msg := func(completed int) string {
		return fmt.Sprintf(`{"type":"message.updated","properties":{"info":{"id":"m1","sessionID":"ses","role":"assistant","providerID":"anthropic","modelID":"claude","cost":0.5,"time":{"created":1,"completed":%d},"tokens":{"input":10,"output":20}}}}`, completed)
	}

	out := decodeAll(d, msg(0), msg(2), msg(2))

	require.Len(t, out, 1)
	assert.Equal(t, Usage{ProviderID: "anthropic", ModelID: "claude", InputTokens: 10, OutputTokens: 20, Cost: 0.5}, out[0].Usage)
}

func TestDecoder_StopsAfterDoneOrError(t *testing.T) {
	d := newDecoder("ses")

	out := decodeAll(d,
		`{"type":"session.error","properties":{"sessionID":"ses","error":{"name":"ProviderAuthError","data":{"message":"no API key"}}}}`,
		`{"type":"session.idle","properties":{"sessionID":"ses"}}`,
	)

	require.Len(t, out, 1)
	assert.Equal(t, Event{Kind: EventError, Text: "no API key"}, out[0])
}

// fakeOpenCode is an HTTP server with the parts of the OpenCode API that a
// run uses. The test writes events to the stream with emit.
type fakeOpenCode struct {
	t      *testing.T
	srv    *httptest.Server
	stream chan string

	mu      sync.Mutex
	prompt  map[string]any
	replies []string
}

func newFakeOpenCode(t *testing.T) *fakeOpenCode {
	f := &fakeOpenCode{t: t, stream: make(chan string, 16)}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /session", func(w http.ResponseWriter, r *http.Request) {
		if _, pass, ok := r.BasicAuth(); !ok || pass != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"id":"ses"}`))
	})
	mux.HandleFunc("GET /event", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case e := <-f.stream:
				fmt.Fprintf(w, "data: %s\n\n", e)
				w.(http.Flusher).Flush()
			}
		}
	})
	mux.HandleFunc("POST /session/ses/prompt_async", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.prompt = body
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /question/{id}/{action}", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.replies = append(f.replies, r.PathValue("id")+" "+r.PathValue("action")+" "+strings.TrimSpace(string(body)))
		f.mu.Unlock()
		_, _ = w.Write([]byte("true"))
	})
	mux.HandleFunc("POST /permission/{id}/reply", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.replies = append(f.replies, r.PathValue("id")+"="+body["reply"])
		f.mu.Unlock()
		_, _ = w.Write([]byte("true"))
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeOpenCode) client() *client {
	return &client{baseURL: f.srv.URL, password: "secret", http: &http.Client{Timeout: 5 * time.Second}}
}

func (f *fakeOpenCode) emit(e string) { f.stream <- e }

func nextEvent(t *testing.T, run Run) (Event, bool) {
	t.Helper()
	select {
	case ev, ok := <-run.Events():
		return ev, ok
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for an event")
		return Event{}, false
	}
}

func TestRun_SendsTaskAndStreamsEventsUntilIdle(t *testing.T) {
	f := newFakeOpenCode(t)
	cfg := Config{ProviderID: "anthropic", ModelID: "claude", Instructions: "be brief"}

	run, err := startRun(context.Background(), f.client(), cfg, "set up the SDK")
	require.NoError(t, err)
	defer run.Close()

	f.mu.Lock()
	assert.Equal(t, "be brief", f.prompt["system"])
	assert.Equal(t, map[string]any{"providerID": "anthropic", "modelID": "claude"}, f.prompt["model"])
	f.mu.Unlock()

	f.emit(`{"type":"file.edited","properties":{"file":"index.js"}}`)
	f.emit(`{"type":"session.idle","properties":{"sessionID":"ses"}}`)

	ev, _ := nextEvent(t, run)
	assert.Equal(t, Event{Kind: EventFileEdited, File: "index.js"}, ev)
	ev, _ = nextEvent(t, run)
	assert.Equal(t, EventDone, ev.Kind)
	_, open := nextEvent(t, run)
	assert.False(t, open, "the channel closes after the last event")
}

func TestRun_RepliesToPermission(t *testing.T) {
	f := newFakeOpenCode(t)
	run, err := startRun(context.Background(), f.client(), Config{}, "task")
	require.NoError(t, err)
	defer run.Close()

	f.emit(`{"type":"permission.asked","properties":{"id":"per1","sessionID":"ses","permission":"bash","patterns":["npm test"]}}`)
	ev, _ := nextEvent(t, run)
	require.Equal(t, EventPermission, ev.Kind)

	require.NoError(t, run.Reply(context.Background(), ev.Permission, true))
	require.NoError(t, run.Reply(context.Background(), ev.Permission, false))

	f.mu.Lock()
	defer f.mu.Unlock()
	assert.Equal(t, []string{"per1=once", "per1=reject"}, f.replies)
}

func TestRun_AnswersQuestion(t *testing.T) {
	f := newFakeOpenCode(t)
	run, err := startRun(context.Background(), f.client(), Config{}, "task")
	require.NoError(t, err)
	defer run.Close()
	req := QuestionRequest{ID: "que1"}

	require.NoError(t, run.Answer(context.Background(), req, [][]string{{".env"}}))
	require.NoError(t, run.Answer(context.Background(), req, nil))

	f.mu.Lock()
	defer f.mu.Unlock()
	assert.Equal(t, []string{`que1 reply {"answers":[[".env"]]}`, "que1 reject {}"}, f.replies)
}

func TestRun_StreamEndsEarly_ReportsError(t *testing.T) {
	f := newFakeOpenCode(t)
	run, err := startRun(context.Background(), f.client(), Config{}, "task")
	require.NoError(t, err)
	defer run.Close()

	f.srv.CloseClientConnections()

	ev, _ := nextEvent(t, run)
	assert.Equal(t, EventError, ev.Kind)
}

func TestRun_CloseEndsTheEventsOfAnUnfinishedRun(t *testing.T) {
	f := newFakeOpenCode(t)
	run, err := startRun(context.Background(), f.client(), Config{}, "task")
	require.NoError(t, err)

	done := make(chan []Event)
	go func() {
		var got []Event
		for ev := range run.Events() {
			got = append(got, ev)
		}
		done <- got
	}()
	require.NoError(t, run.Close())

	select {
	case got := <-done:
		require.NotEmpty(t, got)
		assert.Equal(t, EventError, got[len(got)-1].Kind, "the last event says that the run did not finish")
	case <-time.After(2 * time.Second):
		t.Fatal("Events stayed open after Close")
	}
}

func TestCredentialEnvVar_CoversEveryAlias(t *testing.T) {
	for _, id := range []string{"js-client-sdk", "launchdarkly-react-client-sdk", "vue-client-sdk", "electron-client-sdk"} {
		assert.Equal(t, ClientSideIDEnvVar, CredentialEnvVar(id), id)
	}
	for _, id := range []string{"android", "android-client-sdk", "ios-client-sdk", "swift-client-sdk", "react-native", "react-native-client-sdk", "flutter-client-sdk"} {
		assert.Equal(t, MobileKeyEnvVar, CredentialEnvVar(id), id)
	}
	for _, id := range []string{"node-server-sdk", "python-server-sdk", "go-server-sdk"} {
		assert.Equal(t, SDKKeyEnvVar, CredentialEnvVar(id), id)
	}
}

func TestBuildPrompt_FlutterNamesTheWebCredential(t *testing.T) {
	p := BuildPrompt(Task{SDKID: "flutter-client-sdk", FlagKey: "f"})
	assert.Contains(t, p, MobileKeyEnvVar)
	assert.Contains(t, p, "If the app runs on the web, read the client-side ID from "+ClientSideIDEnvVar)
	assert.NotContains(t, BuildPrompt(Task{SDKID: "android", FlagKey: "f"}), ClientSideIDEnvVar)
}

func TestRun_CloseIsSafeTwice(t *testing.T) {
	f := newFakeOpenCode(t)
	run, err := startRun(context.Background(), f.client(), Config{}, "task")
	require.NoError(t, err)

	assert.NoError(t, run.Close())
	assert.NoError(t, run.Close())
}

func TestBuildPrompt_HasTheSameFactsAsBuildFacts(t *testing.T) {
	task := Task{SDKID: "python-server-sdk", FlagKey: "f", ProjectKey: "proj", EnvironmentKey: "prod", ProviderKeys: []string{"ANTHROPIC_API_KEY"}}
	for _, p := range []string{BuildPrompt(task), BuildFacts(task)} {
		assert.Contains(t, p, "LaunchDarkly project: proj")
		assert.Contains(t, p, "LaunchDarkly environment: prod")
		assert.Contains(t, p, "ANTHROPIC_API_KEY (names only)")
		assert.Contains(t, p, "$TMPDIR")
	}
}
