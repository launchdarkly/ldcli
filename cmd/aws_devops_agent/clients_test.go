package awsdevopsagent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

type loginFake struct {
	results  []error
	calls    int
	runs     [][]string
	runErr   error
	reads    int
	prompted *bytes.Buffer
}

func (f *loginFake) newClients(context.Context, string, string) (awsdevops.Clients, error) {
	err := f.results[min(f.calls, len(f.results)-1)]
	f.calls++
	if err != nil {
		return awsdevops.Clients{}, err
	}

	return awsdevops.Clients{Region: "us-east-1"}, nil
}

func (f *loginFake) run(_ context.Context, name string, args ...string) error {
	f.runs = append(f.runs, append([]string{name}, args...))

	return f.runErr
}

type countingReader struct {
	f *loginFake
	r io.Reader
}

func (c countingReader) Read(p []byte) (int, error) {
	c.f.reads++

	return c.r.Read(p)
}

var errCreds = fmt.Errorf("wrapped: %w", awsdevops.ErrNoCredentials)

const ssoConfig = "[profile dev]\nregion = us-east-1\n\n[profile dev-admin]\nsso_session = corp\n\n[profile sso]\nsso_session = corp\n"

func newLoginTest(t *testing.T, answer string, results ...error) (*loginFake, ssoLogin) {
	t.Helper()
	t.Setenv("AWS_PROFILE", "")
	path := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(path, []byte(ssoConfig), 0o600))
	f := &loginFake{results: results, prompted: &bytes.Buffer{}}

	return f, ssoLogin{
		newClients:  f.newClients,
		configPath:  path,
		in:          countingReader{f: f, r: strings.NewReader(answer)},
		out:         f.prompted,
		interactive: func() bool { return true },
		lookPath:    func(string) (string, error) { return "/usr/local/bin/aws", nil },
		run:         f.run,
	}
}

func TestLoginRunsOnceOnUppercaseY(t *testing.T) {
	f, l := newLoginTest(t, "Y\n", errCreds, nil)

	clients, err := l.clients(context.Background(), "", "sso")

	require.NoError(t, err)
	assert.Equal(t, "us-east-1", clients.Region)
	assert.Equal(t, [][]string{{"aws", "sso", "login", "--profile", "sso"}}, f.runs)
	assert.Equal(t, 2, f.calls)
	assert.Contains(t, f.prompted.String(), "Run aws sso login --profile sso now? [y/N]")
}

func TestLoginAcceptsYesWithSpaces(t *testing.T) {
	f, l := newLoginTest(t, "  YeS \n", errCreds, nil)

	_, err := l.clients(context.Background(), "", "sso")

	require.NoError(t, err)
	assert.Len(t, f.runs, 1)
}

func TestLoginIgnoresOtherAnswers(t *testing.T) {
	for _, answer := range []string{"ye\n", "yess\n", "n\n", "\n", ""} {
		t.Run(fmt.Sprintf("%q", answer), func(t *testing.T) {
			f, l := newLoginTest(t, answer, errCreds, nil)

			_, err := l.clients(context.Background(), "", "sso")

			assert.Same(t, errCreds, err)
			assert.Empty(t, f.runs)
			assert.Equal(t, 1, f.calls)
		})
	}
}

func TestLoginRetriesOnlyOnce(t *testing.T) {
	second := fmt.Errorf("again: %w", awsdevops.ErrNoCredentials)
	f, l := newLoginTest(t, "y\ny\ny\n", errCreds, second)

	_, err := l.clients(context.Background(), "", "sso")

	assert.Same(t, second, err)
	assert.Equal(t, 2, f.calls)
	assert.Len(t, f.runs, 1)
}

func TestLoginFailureNamesTheCommand(t *testing.T) {
	f, l := newLoginTest(t, "y\n", errCreds, nil)
	f.runErr = errors.New("exit status 255")

	_, err := l.clients(context.Background(), "", "sso")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "aws sso login --profile sso")
	assert.ErrorIs(t, err, f.runErr)
	assert.Equal(t, 1, f.calls)
}

func TestLoginSkipsNearMissProfile(t *testing.T) {
	f, l := newLoginTest(t, "y\n", errCreds, nil)

	_, err := l.clients(context.Background(), "", "dev")

	assert.Same(t, errCreds, err)
	assert.Zero(t, f.reads)
	assert.Empty(t, f.runs)
}

func TestLoginSkipsWithoutTerminal(t *testing.T) {
	f, l := newLoginTest(t, "y\n", errCreds, nil)
	l.interactive = func() bool { return false }

	_, err := l.clients(context.Background(), "", "sso")

	assert.Same(t, errCreds, err)
	assert.Zero(t, f.reads)
	assert.Empty(t, f.runs)
	assert.Equal(t, 1, f.calls)
}

func TestLoginSkipsWithoutAWSBinary(t *testing.T) {
	f, l := newLoginTest(t, "y\n", errCreds, nil)
	l.lookPath = func(string) (string, error) { return "", errors.New("not found") }

	_, err := l.clients(context.Background(), "", "sso")

	assert.Same(t, errCreds, err)
	assert.Zero(t, f.reads)
	assert.Empty(t, f.runs)
}

func TestLoginSkipsOtherErrors(t *testing.T) {
	regionErr := errors.New("AWS DevOps Agent is not available in us-east-3")
	f, l := newLoginTest(t, "y\n", regionErr, nil)

	_, err := l.clients(context.Background(), "", "sso")

	assert.Same(t, regionErr, err)
	assert.Zero(t, f.reads)
	assert.Empty(t, f.runs)
	assert.Equal(t, 1, f.calls)
}

func TestLoginSkipsWithoutProfile(t *testing.T) {
	f, l := newLoginTest(t, "y\n", errCreds, nil)

	_, err := l.clients(context.Background(), "", "")

	assert.Same(t, errCreds, err)
	assert.Zero(t, f.reads)
	assert.Empty(t, f.runs)
}

// Without --profile or AWS_PROFILE, the SDK uses the default profile.
func TestLoginOffersTheDefaultSSOProfile(t *testing.T) {
	f, l := newLoginTest(t, "y\n", errCreds, nil)
	require.NoError(t, os.WriteFile(l.configPath, []byte("[default]\nsso_session = corp\n"), 0o600))

	_, err := l.clients(context.Background(), "", "")

	require.NoError(t, err)
	assert.Equal(t, [][]string{{"aws", "sso", "login", "--profile", "default"}}, f.runs)
}

func TestLoginUsesAWSProfileVariable(t *testing.T) {
	f, l := newLoginTest(t, "y\n", errCreds, nil)
	t.Setenv("AWS_PROFILE", "sso")

	_, err := l.clients(context.Background(), "", "")

	require.NoError(t, err)
	assert.Equal(t, [][]string{{"aws", "sso", "login", "--profile", "sso"}}, f.runs)
}

func TestLoginFlagWinsOverAWSProfile(t *testing.T) {
	f, l := newLoginTest(t, "y\n", errCreds, nil)
	t.Setenv("AWS_PROFILE", "sso")

	_, err := l.clients(context.Background(), "", "dev")

	assert.Same(t, errCreds, err)
	assert.Empty(t, f.runs)
}

func TestLoginSkipsWhenFirstCallWorks(t *testing.T) {
	f, l := newLoginTest(t, "y\n", nil)

	_, err := l.clients(context.Background(), "", "sso")

	require.NoError(t, err)
	assert.Zero(t, f.reads)
	assert.Equal(t, 1, f.calls)
}

// JSON runs never ask, like the setup plan.
func TestLoginNeverAsksInJSONMode(t *testing.T) {
	viper.Set(cliflags.OutputFlag, "json")
	t.Cleanup(func() { viper.Set(cliflags.OutputFlag, "") })
	cmd := NewStatusCmd(nil)

	l := newSSOLogin(cmd, func() bool { return true })

	assert.False(t, l.interactive())
}

func TestLoginAsksInPlaintextTerminal(t *testing.T) {
	viper.Set(cliflags.OutputFlag, "plaintext")
	t.Cleanup(func() { viper.Set(cliflags.OutputFlag, "") })
	cmd := NewStatusCmd(nil)

	l := newSSOLogin(cmd, func() bool { return true })

	assert.True(t, l.interactive())
}

// The SSO prompt shares stdin with later prompts.
func TestLoginPromptLeavesLaterLines(t *testing.T) {
	cmd := NewSetupCmd(nil)
	cmd.SetIn(strings.NewReader("y\n1\nyes\n"))

	l := newSSOLogin(cmd, func() bool { return true })

	assert.Equal(t, "y", readLine(l.in))
	assert.Equal(t, "1", readLine(cmd.InOrStdin()))
	assert.Equal(t, "yes", readLine(cmd.InOrStdin()))
}

func TestLoginNeverAsksWithYes(t *testing.T) {
	cmd := NewSetupCmd(nil)
	require.NoError(t, cmd.Flags().Set(yesFlag, "true"))

	l := newSSOLogin(cmd, func() bool { return true })

	assert.False(t, l.interactive())
}
