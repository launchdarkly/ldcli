package cmd

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"ldcli/internal/flags"
	"ldcli/internal/members"
	"ldcli/internal/projects"
)

var ValidResponse = `{"valid": true}`

func CallCmd(
	t *testing.T,
	flagsClient *flags.MockClient,
	membersClient *members.MockClient,
	projectsClient *projects.MockClient,
	args []string,
) ([]byte, error) {
	rootCmd, err := NewRootCommand(flagsClient, membersClient, projectsClient)
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
