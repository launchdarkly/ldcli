package prompt_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

func TestCLILinksAndDetachesRepositorySource(t *testing.T) {
	root := initRepository(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "prompt.md"), []byte("Local CLI prompt.\n"), 0o644))
	server := variation("Server")
	api := &directAPI{
		variation: &server,
		directions: map[string]syncdomain.SyncDirection{
			"variation:support/default": syncdomain.SyncDirectionBoth,
		},
	}

	_, _, err := runPrompt(
		t, root, api,
		"link",
		"--file", "prompt.md",
		"--format", "markdown",
		"--project", "production",
		"--config-key", "support",
		"--key", "default",
		"--conflict", "local",
		"--yes", "--no-input",
	)
	require.NoError(t, err)
	assert.Equal(t, "Local CLI prompt.", api.variation.Instructions)
	descriptor := filepath.Join(root, ".launchdarkly", "production", "configs", "support", "default.ref.yaml")
	require.FileExists(t, descriptor)

	_, _, err = runPrompt(
		t, root, api,
		"detach",
		"--project", "production",
		"--config-key", "support",
		"--key", "default",
		"--yes", "--no-input",
	)
	require.NoError(t, err)
	require.NoFileExists(t, descriptor)
	require.FileExists(t, filepath.Join(root, "prompt.md"))
}
