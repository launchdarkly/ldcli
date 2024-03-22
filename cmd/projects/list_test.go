package projects_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ld-cli/cmd"
	"ld-cli/internal/projects"
)

func TestList(t *testing.T) {
	t.Run("with valid flags returns list of projects", func(t *testing.T) {
		expected := `{
			"_links": {
				"last": {
					"href": "/api/v2/projects?expand=environments&limit=1&offset=1",
					"type": "application/json"
				},
				"next": {
					"href": "/api/v2/projects?expand=environments&limit=1&offset=0",
					"type": "application/json"
				},
				"self": {
					"href": "/api/v2/projects?expand=environments&limit=1",
					"type": "application/json"
				}
			},
			"items": [
			{
				"_id": "000000000000000000000001",
				"_links": null,
				"includeInSnippetByDefault": false,
				"key": "test-project",
				"name": "",
				"tags": null
			}
			],
			"totalCount": 1
		}`

		rootCmd, err := cmd.NewRootCmd(projects.NewMockClient)
		require.NoError(t, err)
		b := bytes.NewBufferString("")
		rootCmd.Cmd.SetOut(b)
		rootCmd.Cmd.SetArgs([]string{
			"projects",
			"list",
			"-t",
			"accessToken",
			"-u",
			"http://localhost",
		})
		err = rootCmd.Cmd.Execute()

		require.NoError(t, err)
		out, err := io.ReadAll(b)
		require.NoError(t, err)

		assert.JSONEq(t, expected, string(out))
	})

	t.Run("with missing accessToken is an error", func(t *testing.T) {
		rootCmd, err := cmd.NewRootCmd(projects.NewMockClient)
		require.NoError(t, err)
		b := bytes.NewBufferString("")
		rootCmd.Cmd.SetOut(b)
		rootCmd.Cmd.SetArgs([]string{
			"projects",
			"list",
			"-u",
			"http://localhost",
		})
		err = rootCmd.Cmd.Execute()

		assert.EqualError(t, err, `required flag(s) "accessToken" not set`)
	})

	t.Run("with invalid baseUri is an error", func(t *testing.T) {
		rootCmd, err := cmd.NewRootCmd(projects.NewMockClient)
		require.NoError(t, err)
		b := bytes.NewBufferString("")
		rootCmd.Cmd.SetOut(b)
		rootCmd.Cmd.SetArgs([]string{
			"projects",
			"list",
			"-t",
			"accessToken",
			"-u",
			"invalid",
		})
		err = rootCmd.Cmd.Execute()

		assert.EqualError(t, err, "baseUri is invalid")
	})
}
