package cmd

import (
	"bytes"
	"io"
	"ld-cli/internal/projects"
	"testing"

	"github.com/stretchr/testify/require"
)

var ValidResponse = `{"valid": true}`

func ArgsValidCreate() []string {
	args := append(ArgsCreateCommand(), ArgsAccess()...)
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

func ArgsCreateCommand() []string {
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

func CallCmd(t *testing.T, client *projects.MockClient, args []string) ([]byte, error) {
	rootCmd, err := NewRootCmd(client)
	require.NoError(t, err)
	b := bytes.NewBufferString("")
	rootCmd.Cmd.SetOut(b)
	rootCmd.Cmd.SetArgs(args)

	err = rootCmd.Cmd.Execute()
	if err != nil {
		return nil, err
	}

	out, err := io.ReadAll(b)
	require.NoError(t, err)

	return out, nil
}
