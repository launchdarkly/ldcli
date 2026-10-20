package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	readyTimeout = 30 * time.Second
	stopTimeout  = 5 * time.Second
)

// server is an `opencode serve` process that ldcli started for one run.
type server struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
	output *tailBuffer
	exited chan struct{}
	client *client

	stopOnce sync.Once
}

// startServer starts OpenCode in dir and waits until its API answers. The
// server listens on the loopback interface only, and it requires a password
// that is random for each run.
func startServer(ctx context.Context, binary, dir string, config []byte) (*server, error) {
	port, err := freePort()
	if err != nil {
		return nil, err
	}
	password, err := randomHex(32)
	if err != nil {
		return nil, err
	}

	set := map[string]string{
		"OPENCODE_SERVER_PASSWORD": password,
		"OPENCODE_CONFIG_CONTENT":  string(config),
	}
	box, err := sandboxCommand(binary, []string{"serve", "--hostname", "127.0.0.1", "--port", strconv.Itoa(port)}, dir, os.Environ())
	if err != nil {
		return nil, err
	}
	if box.TempDir != "" {
		set["TMPDIR"] = box.TempDir
	}
	cleanup := box.Cleanup

	procCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(procCtx, box.Name, box.Args...)
	cmd.Dir = dir
	cmd.Env = childEnv(os.Environ(), set)
	// Ask OpenCode to stop so it can end its own child processes, and kill it if
	// it does not stop in time.
	cmd.Cancel = func() error {
		if runtime.GOOS == "windows" {
			return cmd.Process.Kill()
		}
		return cmd.Process.Signal(os.Interrupt)
	}
	cmd.WaitDelay = stopTimeout
	out := &tailBuffer{max: 4096}
	cmd.Stdout = out
	cmd.Stderr = out

	if err := cmd.Start(); err != nil {
		cancel()
		cleanup()
		return nil, fmt.Errorf("could not start OpenCode: %w", err)
	}
	s := &server{
		cmd:    cmd,
		cancel: cancel,
		output: out,
		exited: make(chan struct{}),
		client: &client{
			baseURL:  "http://127.0.0.1:" + strconv.Itoa(port),
			password: password,
			http:     &http.Client{Timeout: 30 * time.Second},
		},
	}
	go func() {
		_ = cmd.Wait()
		cleanup()
		close(s.exited)
	}()

	if err := s.waitReady(ctx); err != nil {
		s.stop()
		return nil, err
	}
	return s, nil
}

func (s *server) waitReady(ctx context.Context) error {
	deadline := time.NewTimer(readyTimeout)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		reqCtx, cancel := context.WithTimeout(ctx, time.Second)
		err := s.client.do(reqCtx, http.MethodGet, "/session", nil, nil)
		cancel()
		if err == nil {
			return nil
		}
		var se *statusError
		if errors.As(err, &se) && se.code == http.StatusUnauthorized {
			return fmt.Errorf("OpenCode did not accept the server password. Upgrade OpenCode and try again")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.exited:
			return fmt.Errorf("OpenCode stopped before it was ready: %s", s.output.String())
		case <-deadline.C:
			return fmt.Errorf("OpenCode did not start within %s", readyTimeout)
		case <-tick.C:
		}
	}
}

// stop ends the OpenCode process and waits for it to exit.
func (s *server) stop() {
	s.stopOnce.Do(func() {
		s.cancel()
		<-s.exited
	})
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("could not find a free port for OpenCode: %w", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// tailBuffer keeps the last max bytes written to it, so an error can show what
// OpenCode printed before it stopped.
type tailBuffer struct {
	mu  sync.Mutex
	max int
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = t.buf[len(t.buf)-t.max:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(string(t.buf))
}
