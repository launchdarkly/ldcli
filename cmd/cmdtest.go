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
// CallCmdCapturingStderr runs a command and returns stdout and stderr separately, so
// a test can assert on output written deliberately to stderr — a transitional note,
// say — without it being mistaken for parseable output.
func CallCmdCapturingStderr(
	t *testing.T,
	clients APIClients,
	trackerFn analytics.TrackerFn,
	args []string,
) (stdout []byte, stderr []byte, err error) {
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
	out, errOut := bytes.NewBufferString(""), bytes.NewBufferString("")
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs(args)

	tracker := trackerFn("", "", false)
	if err := cmd.Execute(); err != nil {
		tracker.SendCommandCompletedEvent(analytics.ERROR)
		return out.Bytes(), errOut.Bytes(), err
	}
	tracker.SendCommandCompletedEvent(analytics.SUCCESS)
	return out.Bytes(), errOut.Bytes(), nil
}

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
