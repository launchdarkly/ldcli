package link

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
)

func TestPrepareDoesNotWriteUntilExplicitWrite(t *testing.T) {
	root := t.TempDir()
	source := []byte(`from agents import Agent
support = Agent(instructions="Help with orders.")
`)
	require.NoError(t, os.WriteFile(filepath.Join(root, "agents.py"), source, 0o644))
	id := syncdomain.VariationID("production", "support", "default")

	prepared, err := Prepare(Options{
		Store:            synclocal.NewStore(root),
		RepositoryRoot:   root,
		WorkingDirectory: root,
		ID:               id,
		File:             "agents.py",
		Format:           "python",
		Select:           `agents[id="support"]`,
		Map:              map[string]string{"prompt": "instructions"},
	})

	require.NoError(t, err)
	require.Equal(t, ".launchdarkly/production/configs/support/default.ref.yaml", prepared.Path)
	require.NoFileExists(t, filepath.Join(root, filepath.FromSlash(prepared.Path)))
	path, err := prepared.Write(synclocal.NewStore(root))
	require.NoError(t, err)
	descriptor, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	require.NoError(t, err)
	require.Contains(t, string(descriptor), "formatVersion: 1")
	require.Contains(t, string(descriptor), `select: agents[id="support"]`)
	actual, err := os.ReadFile(filepath.Join(root, "agents.py"))
	require.NoError(t, err)
	require.Equal(t, source, actual)
}
