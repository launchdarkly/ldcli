package projects_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ld-cli/cmd/internal/projects"
	projcmd "ld-cli/cmd/projects"
)

func TestList(t *testing.T) {
	listCmd, err := projcmd.NewListCmd(projects.MockClient{})
	require.NoError(t, err)

	err = listCmd.Cmd.Execute()

	require.NoError(t, err)
}
