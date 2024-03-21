package projects_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	projcmd "ld-cli/cmd/projects"
	"ld-cli/internal/projects"
)

func TestList(t *testing.T) {
	t.Skip()
	listCmd, err := projcmd.NewListCmd(projects.MockClient{})
	require.NoError(t, err)

	err = listCmd.Cmd.Execute()

	require.NoError(t, err)
}
