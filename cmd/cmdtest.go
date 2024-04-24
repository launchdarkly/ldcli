package cmd

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"ldcli/internal/analytics"
)

var ValidResponse = `{"valid": true}`

func CallCmd(
	t *testing.T,
	clients APIClients,
	tracker analytics.Tracker,
	args []string,
) ([]byte, error) {
	outcome := analytics.SUCCESS
	defer analytics.SendCommandCompletedEvent(&outcome, tracker)

	rootCmd, err := NewRootCommand(
		tracker,
		clients,
		"test",
		false,
	)
	require.NoError(t, err)
	b := bytes.NewBufferString("")
	rootCmd.SetOut(b)
	rootCmd.SetArgs(args)

	err = rootCmd.Execute()
	if err != nil {
		outcome = analytics.ERROR
		return nil, err
	} else if _, isHelp := rootCmd.Annotations["help"]; isHelp {
		outcome = analytics.HELP
	}

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
