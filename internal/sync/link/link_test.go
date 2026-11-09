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

func TestWriteStopsWhenTheDescriptorChangedAfterPrepare(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "agents.py"), []byte(`from agents import Agent
support = Agent(instructions="Help with orders.")
`), 0o644))
	id := syncdomain.VariationID("production", "support", "default")
	store := synclocal.NewStore(root)
	options := Options{
		Store: store, RepositoryRoot: root, WorkingDirectory: root, ID: id,
		File: "agents.py", Format: "python", Select: `agents[id="support"]`,
		Map: map[string]string{"prompt": "instructions"},
	}
	prepared, err := Prepare(options)
	require.NoError(t, err)
	// Another process writes the descriptor in the review window.
	tools := []syncdomain.AttachmentRef{{Key: "search"}}
	_, err = store.WriteDescriptor(id, syncdomain.Reference{
		File: "agents.py", Format: "python", Select: "agents[0]",
		Map: map[string]string{"prompt": "instructions"}, Tools: &tools,
	})
	require.NoError(t, err)
	other, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(prepared.Path)))
	require.NoError(t, err)

	_, err = prepared.Write(store)

	require.Error(t, err)
	problem, ok := syncdomain.ProblemFrom(err)
	require.True(t, ok)
	require.Equal(t, syncdomain.ErrorStateChanged, problem.Code)
	current, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(prepared.Path)))
	require.NoError(t, err)
	require.Equal(t, other, current, "the other write stays")

	// A new Prepare reads the current descriptor, so its Write proceeds.
	prepared, err = Prepare(options)
	require.NoError(t, err)
	_, err = prepared.Write(store)
	require.NoError(t, err)
}

func TestRollbackLeavesADescriptorThatAnotherProcessWrote(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "agents.py"), []byte(`from agents import Agent
support = Agent(instructions="Help with orders.")
`), 0o644))
	id := syncdomain.VariationID("production", "support", "default")
	store := synclocal.NewStore(root)
	prepared, err := Prepare(Options{
		Store: store, RepositoryRoot: root, WorkingDirectory: root, ID: id,
		File: "agents.py", Format: "python", Select: `agents[id="support"]`,
		Map: map[string]string{"prompt": "instructions"},
	})
	require.NoError(t, err)
	// Another process creates the descriptor in the review window.
	_, err = store.WriteDescriptor(id, syncdomain.Reference{
		File: "agents.py", Format: "python", Select: "agents[0]", Map: map[string]string{"prompt": "instructions"},
	})
	require.NoError(t, err)
	other, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(prepared.Path)))
	require.NoError(t, err)
	_, err = prepared.Write(store)
	require.Error(t, err, "the write refuses the descriptor of the other process")

	require.NoError(t, prepared.Rollback(store))

	current, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(prepared.Path)))
	require.NoError(t, err)
	require.Equal(t, other, current, "the rollback leaves the descriptor of the other process")
}

func TestPrepareKeepsTheToolsAndSkillsOfTheCurrentDescriptor(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "agents.py"), []byte(`from agents import Agent
support = Agent(instructions="Help with orders.")
`), 0o644))
	id := syncdomain.VariationID("production", "support", "default")
	store := synclocal.NewStore(root)
	tools := []syncdomain.AttachmentRef{{Key: "search"}}
	skills := []syncdomain.AttachmentRef{{Key: "handoff"}}
	_, err := store.WriteDescriptor(id, syncdomain.Reference{
		File: "agents.py", Format: "python", Select: "agents[0]",
		Map: map[string]string{"prompt": "instructions"}, Tools: &tools, Skills: &skills,
	})
	require.NoError(t, err)

	prepared, err := Prepare(Options{
		Store: store, RepositoryRoot: root, WorkingDirectory: root, ID: id,
		File: "agents.py", Format: "python", Select: `agents[id="support"]`,
		Map: map[string]string{"prompt": "instructions"},
	})

	require.NoError(t, err)
	require.Equal(t, `agents[id="support"]`, prepared.Reference.Select, "the relink changes the selection")
	require.Equal(t, &tools, prepared.Reference.Tools, "the relink keeps the tools")
	require.Equal(t, &skills, prepared.Reference.Skills, "the relink keeps the skills")
	require.Contains(t, string(prepared.Content), "key: search")
	require.Contains(t, string(prepared.Content), "key: handoff")
}

func TestRollbackAfterARefusedWriteLeavesAnIdenticalDescriptor(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "agents.py"), []byte(`from agents import Agent
support = Agent(instructions="Help with orders.")
`), 0o644))
	id := syncdomain.VariationID("production", "support", "default")
	store := synclocal.NewStore(root)
	options := Options{
		Store: store, RepositoryRoot: root, WorkingDirectory: root, ID: id,
		File: "agents.py", Format: "python", Select: `agents[id="support"]`,
		Map: map[string]string{"prompt": "instructions"},
	}
	prepared, err := Prepare(options)
	require.NoError(t, err)
	// Another process links the same resource with the same options in the
	// review window, so its descriptor has the same content.
	other, err := Prepare(options)
	require.NoError(t, err)
	_, err = other.Write(store)
	require.NoError(t, err)
	_, err = prepared.Write(store)
	require.Error(t, err, "the write refuses the descriptor of the other process")

	require.NoError(t, prepared.Rollback(store))

	current, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(prepared.Path)))
	require.NoError(t, err)
	require.Equal(t, other.Content, current, "a rollback without a write of its own removes nothing")
}

func TestRollbackWithoutAWriteLeavesTheDescriptor(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "agents.py"), []byte(`from agents import Agent
support = Agent(instructions="Help with orders.")
`), 0o644))
	id := syncdomain.VariationID("production", "support", "default")
	store := synclocal.NewStore(root)
	options := Options{
		Store: store, RepositoryRoot: root, WorkingDirectory: root, ID: id,
		File: "agents.py", Format: "python", Select: `agents[id="support"]`,
		Map: map[string]string{"prompt": "instructions"},
	}
	prepared, err := Prepare(options)
	require.NoError(t, err)
	other, err := Prepare(options)
	require.NoError(t, err)
	_, err = other.Write(store)
	require.NoError(t, err)

	require.NoError(t, prepared.Rollback(store))

	require.FileExists(t, filepath.Join(root, filepath.FromSlash(prepared.Path)))
}
