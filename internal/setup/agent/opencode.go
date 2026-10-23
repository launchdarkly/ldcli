package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/launchdarkly/go-ai-sdk/aiserver"
)

// MinOpenCodeVersion is the oldest OpenCode version that the adapter was
// tested with.
const MinOpenCodeVersion = "1.19.0"

// OpenCode is the Runner that uses the OpenCode CLI.
type OpenCode struct {
	// Binary is the OpenCode executable. The default is "opencode" on the PATH.
	Binary string
	// Config gives the model and the instructions for each run when SDK is
	// nil or cannot start.
	Config ConfigSource
	// SDK runs the agent through the AI SDK: the SDK resolves the setup-agent
	// AI Config, calls the OpenCode handler, and records the run.
	SDK *AIConfigSource

	// lookPath and version are fields so that tests can replace them.
	lookPath func(string) (string, error)
	version  func(ctx context.Context, binary string) (string, error)
}

// NewOpenCode returns a Runner that uses OpenCode from the PATH with the
// built-in configuration.
func NewOpenCode() *OpenCode {
	return &OpenCode{Binary: "opencode", Config: DefaultConfigSource{}}
}

// ErrNotInstalled reports that OpenCode is not on the PATH.
var ErrNotInstalled = errors.New("OpenCode is not installed. Install it from https://opencode.ai, then run setup again")

func (o *OpenCode) binary() string {
	if o.Binary == "" {
		return "opencode"
	}
	return o.Binary
}

func (o *OpenCode) Check(ctx context.Context) error {
	lookPath := o.lookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	path, err := lookPath(o.binary())
	if err != nil {
		return ErrNotInstalled
	}
	version := o.version
	if version == nil {
		version = binaryVersion
	}
	v, err := version(ctx, path)
	if err != nil {
		return fmt.Errorf("could not read the OpenCode version: %w", err)
	}
	if !versionAtLeast(v, MinOpenCodeVersion) {
		return fmt.Errorf("OpenCode %s is too old. Setup needs version %s or later. Run `opencode upgrade`, then run setup again", v, MinOpenCodeVersion)
	}
	return nil
}

func (o *OpenCode) Start(ctx context.Context, task Task) (Run, error) {
	if o.SDK != nil && o.SDK.Ready(ctx) == nil {
		return o.startWithSDK(ctx, task)
	}
	source := o.Config
	if source == nil {
		source = DefaultConfigSource{}
	}
	cfg, err := source.Resolve(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("could not get the setup agent configuration: %w", err)
	}
	config, err := openCodeConfig()
	if err != nil {
		return nil, err
	}
	srv, err := startServer(ctx, o.binary(), task.Dir, config)
	if err != nil {
		return nil, err
	}

	prompt := cfg.Task
	if prompt == "" {
		prompt = BuildPrompt(task)
	}
	run, err := startRun(ctx, srv.client, cfg, prompt)
	if err != nil {
		srv.stop()
		return nil, err
	}
	run.watch(srv)
	return run, nil
}

// startWithSDK runs the agent through the AI SDK. The SDK evaluates the
// setup-agent AI Config, composes the prompt from it, and calls the OpenCode
// handler, which sends that prompt to this run's session. The SDK then
// records the run, its tool calls, and its tokens against the variation.
//
// If the SDK cannot resolve the config before the handler starts, for
// example because the config is off or is not in agent mode, the run uses the
// built-in instructions and task.
func (o *OpenCode) startWithSDK(ctx context.Context, task Task) (Run, error) {
	config, err := openCodeConfig()
	if err != nil {
		return nil, err
	}
	srv, err := startServer(ctx, o.binary(), task.Dir, config)
	if err != nil {
		return nil, err
	}
	run, err := startSession(ctx, srv.client)
	if err != nil {
		srv.stop()
		return nil, err
	}
	run.watch(srv)

	sdkCtx, cancel := context.WithCancel(context.Background())
	run.cancelSDK = cancel
	run.sdkDone = make(chan struct{})
	go run.driveWithSDK(sdkCtx, o.SDK, task)
	return run, nil
}

// driveWithSDK runs the AI SDK stream for the run. The run's own event channel
// still gets every event, so the wizard shows the same progress as without the
// SDK.
func (r *openCodeRun) driveWithSDK(ctx context.Context, sdk *AIConfigSource, task Task) {
	if r.sdkDone != nil {
		defer close(r.sdkDone)
	}
	var mu sync.Mutex
	started := false
	handler := NewOpenCodeHandler(func(ctx context.Context, p SessionPrompt) (<-chan Event, error) {
		mu.Lock()
		started = true
		mu.Unlock()
		cfg := Config{Instructions: p.System, ProviderID: p.ProviderID, ModelID: p.ModelID}
		text := p.Text
		if cfg.Instructions == "" {
			cfg.Instructions, text = DefaultInstructions, BuildPrompt(task)
		}
		tap := r.startTap()
		if err := r.client.promptAsync(ctx, r.sessionID, cfg, text); err != nil {
			return nil, fmt.Errorf("could not send the task to OpenCode: %w", err)
		}
		return tap, nil
	})
	for _, err := range aiserver.Config(aiserver.ConfigArgs{
		Key:        sdk.key(),
		Handlers:   []*aiserver.ProviderHandler{handler},
		SkipJudges: true,
	}).Stream(ctx, BuildFacts(task), setupContext(task)) {
		if err == nil {
			continue
		}
		mu.Lock()
		handlerStarted := started
		mu.Unlock()
		if handlerStarted {
			// The handler already sent the failure to the run's events.
			return
		}
		cfg, _ := DefaultConfigSource{}.Resolve(ctx, task)
		if perr := r.client.promptAsync(ctx, r.sessionID, cfg, BuildPrompt(task)); perr != nil {
			r.fail(fmt.Sprintf("could not send the task to OpenCode: %v", perr))
		}
		return
	}
}

// watch ends the run's events if OpenCode exits during the run, so the caller
// gets an error and does not wait forever.
func (r *openCodeRun) watch(srv *server) {
	r.stopServer = srv.stop
	go func() {
		// If OpenCode exits during the run, end the event stream so the caller
		// gets an error and does not wait forever.
		select {
		case <-srv.exited:
			r.fail(fmt.Sprintf("OpenCode stopped during the run: %s", srv.output.String()))
		case <-r.closed:
		}
	}()
}

// openCodeRun is a Run on an OpenCode server.
type openCodeRun struct {
	client     *client
	sessionID  string
	events     chan Event
	cancel     context.CancelFunc
	closed     chan struct{}
	stopServer func()
	// cancelSDK stops the AI SDK stream of a run that started with the SDK,
	// and sdkDone closes when that stream ends.
	cancelSDK context.CancelFunc
	sdkDone   chan struct{}

	mu       sync.Mutex
	finished bool
	// tap gets a copy of every event, for the OpenCode handler.
	tap       chan Event
	closeOnce sync.Once
}

// startRun creates a session, subscribes to its events, and sends the task.
func startRun(ctx context.Context, c *client, cfg Config, prompt string) (*openCodeRun, error) {
	r, err := startSession(ctx, c)
	if err != nil {
		return nil, err
	}
	if err := c.promptAsync(ctx, r.sessionID, cfg, prompt); err != nil {
		r.Close()
		return nil, fmt.Errorf("could not send the task to OpenCode: %w", err)
	}
	return r, nil
}

// startSession creates a session and subscribes to its events. It sends no
// task.
func startSession(ctx context.Context, c *client) (*openCodeRun, error) {
	sessionID, err := c.createSession(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not create an OpenCode session: %w", err)
	}
	streamCtx, cancel := context.WithCancel(context.Background())
	// Subscribe before the prompt goes out, so no event of the run is missed.
	stream, err := c.subscribe(streamCtx)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("could not read OpenCode events: %w", err)
	}
	r := &openCodeRun{
		client:    c,
		sessionID: sessionID,
		events:    make(chan Event, 64),
		cancel:    cancel,
		closed:    make(chan struct{}),
	}
	go r.pump(stream)
	return r, nil
}

// startTap returns a channel that gets a copy of every later event. It closes
// after the last event.
func (r *openCodeRun) startTap() <-chan Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tap = make(chan Event, 256)
	if r.finished {
		close(r.tap)
	}
	return r.tap
}

func (r *openCodeRun) pump(stream io.ReadCloser) {
	defer stream.Close()
	dec := newDecoder(r.sessionID)
	err := readSSE(stream, func(data []byte) bool {
		for _, ev := range dec.decode(data) {
			if !r.send(ev) {
				return false
			}
		}
		return !dec.finished
	})
	if dec.finished {
		return
	}
	select {
	case <-r.closed:
		return
	default:
	}
	msg := "The OpenCode event stream ended before the run finished."
	if err != nil {
		msg = fmt.Sprintf("Could not read OpenCode events: %v", err)
	}
	r.fail(msg)
}

// send delivers an event, and closes the channel after the last event. It
// returns false if the run is already finished or closed.
func (r *openCodeRun) send(ev Event) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished {
		return false
	}
	select {
	case r.events <- ev:
	case <-r.closed:
		return false
	}
	if r.tap != nil {
		select {
		case r.tap <- ev:
		case <-r.closed:
			return false
		}
	}
	if ev.Kind == EventDone || ev.Kind == EventError {
		r.finished = true
		close(r.events)
		if r.tap != nil {
			close(r.tap)
		}
	}
	return true
}

// sdkFinishTimeout is how long Close waits for the AI SDK to record a run that
// ended.
const sdkFinishTimeout = 5 * time.Second

func (r *openCodeRun) isFinished() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.finished
}

func (r *openCodeRun) fail(msg string) {
	r.send(Event{Kind: EventError, Text: msg})
}

func (r *openCodeRun) Events() <-chan Event { return r.events }

func (r *openCodeRun) Reply(ctx context.Context, req PermissionRequest, allow bool) error {
	return r.client.replyPermission(ctx, req, allow)
}

func (r *openCodeRun) Answer(ctx context.Context, req QuestionRequest, answers [][]string) error {
	return r.client.answerQuestion(ctx, req, answers)
}

func (r *openCodeRun) Close() error {
	r.closeOnce.Do(func() {
		// Let the AI SDK finish recording a run that ended, so its tokens,
		// duration, and spans reach LaunchDarkly. A run that is still going
		// is stopped at once.
		if r.sdkDone != nil && r.isFinished() {
			select {
			case <-r.sdkDone:
			case <-time.After(sdkFinishTimeout):
			}
		}
		close(r.closed)
		r.cancel()
		if r.cancelSDK != nil {
			r.cancelSDK()
		}
		r.end()
		if r.stopServer != nil {
			r.stopServer()
		}
	})
	return nil
}

// end sends a final error event when Close stops an unfinished run.
// It closes the event channel and the tap, so readers do not wait forever. A
// send that waits on a full channel returns when r.closed closes, so end does
// not block.
func (r *openCodeRun) end() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished {
		return
	}
	r.finished = true
	ev := Event{Kind: EventError, Text: "The run stopped before it finished."}
	select {
	case r.events <- ev:
	default:
	}
	close(r.events)
	if r.tap != nil {
		select {
		case r.tap <- ev:
		default:
		}
		close(r.tap)
	}
}

// binaryVersion runs `opencode --version`.
func binaryVersion(ctx context.Context, binary string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, binary, "--version").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

var versionPattern = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

// versionAtLeast reports whether the first version in v is the same as or
// newer than min. It returns false if v has no version number.
func versionAtLeast(v, min string) bool {
	got := versionPattern.FindStringSubmatch(v)
	want := versionPattern.FindStringSubmatch(min)
	if got == nil || want == nil {
		return false
	}
	for i := 1; i <= 3; i++ {
		g, _ := strconv.Atoi(got[i])
		w, _ := strconv.Atoi(want[i])
		if g != w {
			return g > w
		}
	}
	return true
}
