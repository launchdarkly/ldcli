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

	"github.com/aws/aws-sdk-go-v2/credentials/ssocreds"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

// errCreds acts like NewClients on an expired SSO token.
var errCreds = fmt.Errorf("wrapped: %w: %w", awsdevops.ErrNoCredentials, &ssocreds.InvalidTokenError{})

const ssoSession = "\n[sso-session corp]\nsso_start_url = https://example.awsapps.com/start\nsso_region = us-east-1\n"

const ssoConfig = "[profile dev]\nregion = us-east-1\n\n[profile dev-admin]\nsso_session = corp\n\n[profile sso]\nsso_session = corp\n\n" +
	"[profile app]\nrole_arn = arn:aws:iam::123456789012:role/App\nsource_profile = sso\n" + ssoSession

func newLoginTest(t *testing.T, answer string, results ...error) (*loginFake, ssoLogin) {
	t.Helper()
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_DEFAULT_PROFILE", "")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "/dev/null")
	path := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(path, []byte(ssoConfig), 0o600))
	t.Setenv("AWS_CONFIG_FILE", path)
	f := &loginFake{results: results, prompted: &bytes.Buffer{}}

	return f, ssoLogin{
		newClients: f.newClients,
		in:         countingReader{f: f, r: strings.NewReader(answer)},
		out:        f.prompted,
		tty:        func() bool { return true },
		lookPath:   func(string) (string, error) { return "/usr/local/bin/aws", nil },
		run:        f.run,
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

// A login does not fix an SSO error with another cause.
func TestLoginSkipsOtherSSOCauses(t *testing.T) {
	forbidden := fmt.Errorf("wrapped: %w", awsdevops.ErrNoCredentials)
	f, l := newLoginTest(t, "y\n", forbidden, nil)

	_, err := l.clients(context.Background(), "", "sso")

	assert.Same(t, forbidden, err)
	assert.Zero(t, f.reads)
	assert.Empty(t, f.runs)
}

// The retry error does not repeat the login advice.
func TestLoginSaysTheTokenIsStillNotValid(t *testing.T) {
	f, l := newLoginTest(t, "y\n", errCreds, errCreds)

	_, err := l.clients(context.Background(), "", "sso")

	assert.EqualError(t, err, "the SSO token for profile sso is not valid after aws sso login")
	assert.ErrorIs(t, err, awsdevops.ErrNoCredentials)
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
	l.tty = func() bool { return false }

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
	require.NoError(t, os.WriteFile(os.Getenv("AWS_CONFIG_FILE"), []byte("[default]\nsso_session = corp\n"+ssoSession), 0o600))

	_, err := l.clients(context.Background(), "", "")

	require.NoError(t, err)
	assert.Equal(t, [][]string{{"aws", "sso", "login", "--profile", "default"}}, f.runs)
}

// A role profile gets its credentials from its source profile, so the login
// uses the source profile.
func TestLoginUsesTheSourceProfile(t *testing.T) {
	f, l := newLoginTest(t, "y\n", errCreds, nil)

	_, err := l.clients(context.Background(), "", "app")

	require.NoError(t, err)
	assert.Equal(t, [][]string{{"aws", "sso", "login", "--profile", "sso"}}, f.runs)
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

// status has no --yes flag, so its login always asks first.
func TestLoginYesIsOffWithoutTheFlag(t *testing.T) {
	l := newSSOLogin(NewStatusCmd(nil), func() bool { return true })

	assert.False(t, l.yes)
}

// --yes accepts the login, so a terminal run logs in without a question.
func TestLoginRunsWithoutAskingWithYes(t *testing.T) {
	f, l := newLoginTest(t, "", errCreds, nil)
	l.yes = true

	_, err := l.clients(context.Background(), "", "sso")

	require.NoError(t, err)
	assert.Zero(t, f.reads)
	assert.Equal(t, [][]string{{"aws", "sso", "login", "--profile", "sso"}}, f.runs)
	assert.Equal(t, "Running aws sso login --profile sso\n", f.prompted.String())
}

// Nobody can finish the browser sign-in without a terminal, so --yes does
// not start it.
func TestLoginSkipsWithYesWithoutTerminal(t *testing.T) {
	f, l := newLoginTest(t, "", errCreds, nil)
	l.yes = true
	l.tty = func() bool { return false }

	_, err := l.clients(context.Background(), "", "sso")

	assert.Same(t, errCreds, err)
	assert.Empty(t, f.runs)
	assert.Empty(t, f.prompted.String())
}

func TestLoginReadsTheYesFlag(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Bool(yesFlag, false, "")
	require.NoError(t, cmd.Flags().Set(yesFlag, "true"))

	l := newSSOLogin(cmd, func() bool { return true })

	assert.True(t, l.yes)
}

// aws configure sso writes an sso_session profile. The real SDK error for a
// missing token must get the prompt.
func TestLoginAsksForARealSSOSessionProfile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_DEFAULT_PROFILE", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "/dev/null")
	path := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(path, []byte("[profile dev]\nsso_session = corp\nsso_account_id = 123456789012\n"+
		"sso_role_name = Admin\nregion = us-east-1\n\n[sso-session corp]\n"+
		"sso_start_url = https://example.awsapps.com/start\nsso_region = us-east-1\n"), 0o600))
	t.Setenv("AWS_CONFIG_FILE", path)
	prompted := &bytes.Buffer{}
	l := ssoLogin{
		newClients: awsdevops.NewClients,
		in:         strings.NewReader("n\n"),
		out:        prompted,
		tty:        func() bool { return true },
		lookPath:   func(string) (string, error) { return "/usr/local/bin/aws", nil },
		run:        func(context.Context, string, ...string) error { return nil },
	}

	_, err := l.clients(context.Background(), "us-east-1", "dev")

	require.ErrorIs(t, err, awsdevops.ErrNoCredentials)
	assert.Contains(t, prompted.String(), "Run aws sso login --profile dev now?")
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

func TestLoginReadsYesFromSetup(t *testing.T) {
	cmd := NewSetupCmd(nil)
	require.NoError(t, cmd.Flags().Set(yesFlag, "true"))

	l := newSSOLogin(cmd, func() bool { return true })

	assert.True(t, l.yes)
}
