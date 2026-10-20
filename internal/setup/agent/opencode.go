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
)

// MinOpenCodeVersion is the oldest OpenCode version that the adapter was
// tested with.
const MinOpenCodeVersion = "1.19.0"

// OpenCode is the Runner that uses the OpenCode CLI.
type OpenCode struct {
	// Binary is the OpenCode executable. The default is "opencode" on the PATH.
	Binary string
	// Config gives the model and the instructions for each run.
	Config ConfigSource

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
	run.stopServer = srv.stop
	go func() {
		// If OpenCode exits during the run, end the event stream so the caller
		// gets an error and does not wait forever.
		select {
		case <-srv.exited:
			run.fail(fmt.Sprintf("OpenCode stopped during the run: %s", srv.output.String()))
		case <-run.closed:
		}
	}()
	return run, nil
}

// openCodeRun is a Run on an OpenCode server.
type openCodeRun struct {
	client     *client
	sessionID  string
	events     chan Event
	cancel     context.CancelFunc
	closed     chan struct{}
	stopServer func()

	mu        sync.Mutex
	finished  bool
	closeOnce sync.Once
}

// startRun creates a session, subscribes to its events, and sends the task.
func startRun(ctx context.Context, c *client, cfg Config, prompt string) (*openCodeRun, error) {
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

	if err := c.promptAsync(ctx, sessionID, cfg, prompt); err != nil {
		r.Close()
		return nil, fmt.Errorf("could not send the task to OpenCode: %w", err)
	}
	return r, nil
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
	if ev.Kind == EventDone || ev.Kind == EventError {
		r.finished = true
		close(r.events)
	}
	return true
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
		close(r.closed)
		r.cancel()
		r.end()
		if r.stopServer != nil {
			r.stopServer()
		}
	})
	return nil
}

// end gives a run that Close stopped before its last event a final error
// event, and closes the channel, so a reader of Events does not wait forever.
// A send that waits on a full channel returns when r.closed closes, so end
// does not block.
func (r *openCodeRun) end() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished {
		return
	}
	r.finished = true
	select {
	case r.events <- Event{Kind: EventError, Text: "The run was closed before it finished."}:
	default:
	}
	close(r.events)
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
