package cmd

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"ldcli/internal/analytics"
	"ldcli/internal/environments"
	"ldcli/internal/flags"
	"ldcli/internal/members"
	"ldcli/internal/projects"
)

var ValidResponse = `{"valid": true}`

func CallCmd(
	t *testing.T,
	environmentsClient *environments.MockClient,
	flagsClient *flags.MockClient,
	membersClient *members.MockClient,
	projectsClient *projects.MockClient,
	args []string,
) ([]byte, error) {
	rootCmd, err := NewRootCommand(
		&analytics.NoopClient{},
		environmentsClient,
		flagsClient,
		membersClient,
		projectsClient,
		"test",
		false,
	)
	require.NoError(t, err)
	b := bytes.NewBufferString("")
	rootCmd.SetOut(b)
	rootCmd.SetArgs(args)

	err = rootCmd.Execute()
	if err != nil {
		return nil, err
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
