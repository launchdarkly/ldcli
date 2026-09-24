package awsdevopsagent

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

const (
	skipKey   = 's'
	etx       = 3
	backspace = 8
	del       = 127
)

func manualStepPrompt(step awsdevops.ManualStep) string {
	switch step.Kind {
	case awsdevops.ManualStepOAuthConsent:
		return "Approve the LaunchDarkly MCP server consent screen at:"
	case awsdevops.ManualStepMCPServer:
		return "Create a LaunchDarkly service token for the agent at:"
	case awsdevops.ManualStepGitHubApp:
		return "Register GitHub and install the GitHub App at:"
	case awsdevops.ManualStepKiroAPIKey:
		return "Create a Kiro API key (optional) at:"
	default:
		return step.Description
	}
}

// walkManualSteps pauses on each browser-only step so the operator can finish
// it, then picks up whatever they created. Steps they skip are returned so the
// caller can still list them.
func walkManualSteps(
	cmd *cobra.Command,
	clients awsdevops.Clients,
	opts awsdevops.SetupOptions,
	result *awsdevops.SetupResult,
) []awsdevops.ManualStep {
	keys := newKeyReader(cmd.InOrStdin())
	defer keys.close()
	out := keys.writer(cmd.OutOrStdout())
	errOut := keys.writer(cmd.ErrOrStderr())

	var skipped []awsdevops.ManualStep
	for _, step := range result.RemainingManualSteps {
		_, _ = fmt.Fprintf(out, "\n%s\n  %s\n\n", manualStepPrompt(step), step.URL)

		if step.Kind == awsdevops.ManualStepGitHubApp {
			if serviceID := awaitGitHubService(cmd, clients, keys, out, errOut); serviceID != "" {
				associateGitHub(cmd, clients, opts, result, serviceID, out, errOut)

				continue
			}
			skipped = append(skipped, step)

			continue
		}

		if step.Kind == awsdevops.ManualStepMCPServer {
			if !connectMCPServer(cmd, clients, opts, result, keys, out, errOut) {
				skipped = append(skipped, step)
			}

			continue
		}

		_, _ = fmt.Fprint(out, "Press Enter when you're done, or s to skip: ")
		key := keys.next()
		_, _ = fmt.Fprintln(out)
		if key == skipKey {
			skipped = append(skipped, step)
		}
	}

	return skipped
}

// connectMCPServer registers the MCP server with a token pasted at the prompt,
// reporting whether the step is done.
func connectMCPServer(
	cmd *cobra.Command,
	clients awsdevops.Clients,
	opts awsdevops.SetupOptions,
	result *awsdevops.SetupResult,
	keys *keyReader,
	out io.Writer,
	errOut io.Writer,
) bool {
	_, _ = fmt.Fprint(out, "Paste the token to connect the MCP server, or press Enter to skip: ")
	token := keys.line()
	_, _ = fmt.Fprintln(out)
	if token == "" {
		return false
	}

	opts.LDAccessToken = token
	if err := awsdevops.RegisterMCPServer(cmd.Context(), clients, opts, result); err != nil {
		_, _ = fmt.Fprintf(errOut, "%s\n", err)

		return false
	}

	return true
}

func awaitGitHubService(
	cmd *cobra.Command,
	clients awsdevops.Clients,
	keys *keyReader,
	out io.Writer,
	errOut io.Writer,
) string {
	_, _ = fmt.Fprint(out, "Waiting for the GitHub registration, or press any key to skip... ")

	found := make(chan string, 1)
	failed := make(chan error, 1)
	go func() {
		serviceID, err := awsdevops.WaitForGitHubService(cmd.Context(), clients, 0)
		if err != nil {
			failed <- err

			return
		}
		found <- serviceID
	}()

	select {
	case serviceID := <-found:
		_, _ = fmt.Fprintf(out, "found service %s\n", serviceID)

		return serviceID
	case err := <-failed:
		_, _ = fmt.Fprintf(errOut, "\nstopped watching for the GitHub registration: %s\n", err)
	case <-keys.keys:
		_, _ = fmt.Fprintln(out, "skipped")
	}

	return ""
}

func associateGitHub(
	cmd *cobra.Command,
	clients awsdevops.Clients,
	opts awsdevops.SetupOptions,
	result *awsdevops.SetupResult,
	serviceID string,
	out io.Writer,
	errOut io.Writer,
) {
	if opts.GitHubOwner == "" || opts.GitHubRepo == "" || opts.GitHubRepoID == "" {
		_, _ = fmt.Fprintln(
			out,
			"GitHub is registered. Re-run setup with --github-owner <owner> --github-repo <repo> --github-repo-id <id> to connect a repository",
		)

		return
	}

	opts.GitHubServiceID = serviceID
	associationID, err := awsdevops.AssociateGitHub(cmd.Context(), clients, result.AgentSpaceID, opts)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "%s\n", err)

		return
	}
	result.GitHubAssociationID = associationID
	_, _ = fmt.Fprintf(out, "Connected %s/%s\n", opts.GitHubOwner, opts.GitHubRepo)
}

// keyReader delivers single keypresses from one long-lived goroutine, so a
// pause that gives up on stdin cannot leave a reader behind to swallow the
// keys meant for the next one. On a terminal it switches to raw mode, where a
// key registers without Enter.
type keyReader struct {
	keys    chan byte
	restore func()
	raw     bool
}

func newKeyReader(in io.Reader) *keyReader {
	reader := &keyReader{keys: make(chan byte, 1), restore: func() {}}

	if file, ok := in.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		if state, err := term.MakeRaw(int(file.Fd())); err == nil {
			reader.restore = func() { _ = term.Restore(int(file.Fd()), state) }
			reader.raw = true
		}
	}

	go reader.read(in)

	return reader
}

func (k *keyReader) read(in io.Reader) {
	buffered := bufio.NewReader(in)
	for {
		key, err := buffered.ReadByte()
		if err != nil {
			close(k.keys)

			return
		}
		if key == etx {
			k.restore()
			if process, err := os.FindProcess(os.Getpid()); err == nil {
				_ = process.Signal(os.Interrupt)
			}

			return
		}
		k.keys <- key
	}
}

func (k *keyReader) next() byte {
	key, ok := <-k.keys
	if !ok {
		return 0
	}
	if key == 'S' {
		return skipKey
	}

	return key
}

// line collects keys until Enter. It does not echo, since the only thing
// typed at one of these prompts is an access token.
func (k *keyReader) line() string {
	var typed []byte
	for {
		key, ok := <-k.keys
		if !ok || key == '\r' || key == '\n' {
			return strings.TrimSpace(string(typed))
		}
		if key == backspace || key == del {
			if len(typed) > 0 {
				typed = typed[:len(typed)-1]
			}

			continue
		}
		typed = append(typed, key)
	}
}

func (k *keyReader) close() {
	k.restore()
}

// writer keeps output readable while the terminal is raw, where a bare newline
// no longer returns the cursor to the first column.
func (k *keyReader) writer(w io.Writer) io.Writer {
	if !k.raw {
		return w
	}

	return crlfWriter{w: w}
}

type crlfWriter struct {
	w io.Writer
}

func (c crlfWriter) Write(p []byte) (int, error) {
	if _, err := c.w.Write(bytes.ReplaceAll(p, []byte("\n"), []byte("\r\n"))); err != nil {
		return 0, err
	}

	return len(p), nil
}

func canPrompt() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}
