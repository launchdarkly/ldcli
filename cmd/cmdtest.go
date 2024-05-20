package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/analytics"
)

var StubbedSuccessResponse = `{
	"key": "test-key",
	"name": "test-name"
}`

func CallCmd(
	t *testing.T,
	clients APIClients,
	trackerFn analytics.TrackerFn,
	args []string,
) ([]byte, error) {
	rootCmd, err := NewRootCommand(
		trackerFn,
		clients,
		"test",
		false,
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
