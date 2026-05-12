package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/config"
	"github.com/launchdarkly/ldcli/internal/resources"
)

var StubbedSuccessResponse = `{
	"key": "test-key",
	"name": "test-name"
}`

// CallCmd runs the root command for integration-style tests. It passes isTerminal always true so
// the default --output matches an interactive terminal (plaintext); non-TTY JSON defaults are
// covered in root_test.go.
func CallCmd(
	t *testing.T,
	clients APIClients,
	trackerFn analytics.TrackerFn,
	args []string,
) ([]byte, error) {
	rootCmd, err := NewRootCommand(
		config.NewService(&resources.MockClient{}),
		trackerFn,
		clients,
		"test",
		false,
		func() bool { return true },
		nil,
	)
	cmd := rootCmd.Cmd()
	require.NoError(t, err)
	b := bytes.NewBufferString("")
	cmd.SetOut(b)
	cmd.SetArgs(args)
	tracker := trackerFn("", "", false)

	err = cmd.Execute()
	if err != nil {
		tracker.SendCommandCompletedEvent(analytics.ERROR)
		return nil, err
	}

	tracker.SendCommandCompletedEvent(analytics.SUCCESS)

	out, err := io.ReadAll(b)
	require.NoError(t, err)

	return out, nil
}

// CallCmdWithStderr is identical to CallCmd but additionally captures stderr alongside stdout.
// It is required by tests that assert on stderr content (e.g. the rollouts-beta banner). The
// returned stderrBytes is empty when no command wrote to stderr. Existing CallCmd usages are
// unaffected — this helper is purely additive.
func CallCmdWithStderr(
	t *testing.T,
	clients APIClients,
	trackerFn analytics.TrackerFn,
	args []string,
) ([]byte, []byte, error) {
	rootCmd, err := NewRootCommand(
		config.NewService(&resources.MockClient{}),
		trackerFn,
		clients,
		"test",
		false,
		func() bool { return true },
		nil,
	)
	require.NoError(t, err)
	cmd := rootCmd.Cmd()
	stdout := bytes.NewBufferString("")
	stderr := bytes.NewBufferString("")
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs(args)
	tracker := trackerFn("", "", false)

	err = cmd.Execute()
	if err != nil {
		tracker.SendCommandCompletedEvent(analytics.ERROR)
		stdoutBytes, _ := io.ReadAll(stdout)
		stderrBytes, _ := io.ReadAll(stderr)
		return stdoutBytes, stderrBytes, err
	}

	tracker.SendCommandCompletedEvent(analytics.SUCCESS)

	stdoutBytes, err := io.ReadAll(stdout)
	require.NoError(t, err)
	stderrBytes, err := io.ReadAll(stderr)
	require.NoError(t, err)

	return stdoutBytes, stderrBytes, nil
}

// SetupTestEnvVars sets up and tears down tests for checking that environment variables are set.
func SetupTestEnvVars(_ *testing.T) func(t *testing.T) {
	os.Setenv("LD_ACCESS_TOKEN", "testAccessToken")
	os.Setenv("LD_BASE_URI", "http://test.com")

	return func(t *testing.T) {
		os.Unsetenv("LD_ACCESS_TOKEN")
		os.Unsetenv("LD_BASE_URI")
	}
}

func ExtraErrorHelp(cmdName string, cmdAction string) string {
	out := ".\n\nGo to https://app.launchdarkly.com/settings/authorization to create an access token.\n"
	out += "Use `ldcli config --set access-token <value>` to configure the value to persist across CLI commands."
	out += fmt.Sprintf("\n\nSee `ldcli %s %s --help` for supported flags and usage.", cmdName, cmdAction)

	return out
}
