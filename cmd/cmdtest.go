package cmd

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"ldcli/internal/flags"
	"ldcli/internal/projects"
)

var ValidResponse = `{"valid": true}`

func ArgsValidFlagsCreate() []string {
	args := append(ArgsFlagsCreateCommand(), ArgsAccess()...)
	args = append(args, ArgsData()...)

	return args
}

func ArgsValidProjectsCreate() []string {
	args := append(ArgsProjectsCreateCommand(), ArgsAccess()...)
	args = append(args, ArgsData()...)

	return args
}

func ArgsValidList() []string {
	return append(ArgsListCommand(), ArgsAccess()...)
}

func ArgsData() []string {
	return []string{
		"-d",
		`{"key": "test-key", "name": "test-name"}`,
	}
}

func ArgsAccess() []string {
	return []string{
		"-t",
		"testAccessToken",
		"-u",
		"http://test.com",
	}
}

func ArgsFlagsCreateCommand() []string {
	return []string{
		"flags",
		"create",
	}
}

func ArgsProjectsCreateCommand() []string {
	return []string{
		"projects",
		"create",
	}
}

func ArgsListCommand() []string {
	return []string{
		"projects",
		"list",
	}
}

func CallCmd(
	t *testing.T,
	flagsClient *flags.MockClient,
	projectsClient *projects.MockClient,
	args []string,
) ([]byte, error) {
	rootCmd, err := NewRootCommand(flagsClient, projectsClient)
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
