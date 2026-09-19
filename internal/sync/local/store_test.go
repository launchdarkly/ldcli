package local

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

func TestStore_ProjectKeys(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	require.NoError(t, os.MkdirAll(
		filepath.Join(root, syncdomain.RootDir, "zeta"),
		0o755,
	))
	require.NoError(t, os.MkdirAll(
		filepath.Join(root, syncdomain.RootDir, "alpha"),
		0o755,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(root, syncdomain.RootDir, "README"),
		nil,
		0o644,
	))

	keys, err := store.ProjectKeys()

	require.NoError(t, err)
	assert.Equal(t, []string{"alpha", "zeta"}, keys)
}

func TestStore_BootstrapRoundTripsSupportedModes(t *testing.T) {
	root := t.TempDir()
	resources := []VariationFile{
		{
			ProjectKey: "project",
			ConfigKey:  "completion-config",
			Upsert:     true,
			Variation: syncdomain.Variation{
				Mode:               syncdomain.VariationModeCompletion,
				Key:                "friendly",
				Name:               "Friendly",
				ModelConfigKey:     "claude",
				ModelConfigVersion: 3,
				Model:              map[string]any{"modelName": "claude"},
				OutputFormat:       map[string]any{"type": "json_schema"},
				Messages: []syncdomain.Message{
					{Role: "system", Content: "Be helpful."},
					{Role: "user", Content: "Answer the question."},
				},
			},
		},
		{
			ProjectKey: "project",
			ConfigKey:  "agent-config",
			Upsert:     true,
			Variation: syncdomain.Variation{
				Mode:         syncdomain.VariationModeAgent,
				Key:          "researcher",
				Name:         "Researcher",
				Instructions: "Check the available sources.",
			},
		},
	}

	paths, err := NewStore(root).Bootstrap(resources)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{
		"project/configs/completion-config/friendly.prompt.md",
		"project/configs/agent-config/researcher.prompt.md",
	}, paths)

	agentFile, err := os.ReadFile(filepath.Join(
		root,
		syncdomain.RootDir,
		"project",
		configsDir,
		"agent-config",
		"researcher.prompt.md",
	))
	require.NoError(t, err)
	assert.NotContains(t, string(agentFile), "instructions:")
	assert.Contains(t, string(agentFile), "\nCheck the available sources.\n")

	compiled, err := Compile(os.DirFS(root))
	require.NoError(t, err)
	require.Len(t, compiled, len(resources))
	for _, local := range resources {
		resource := requireVariationResource(
			t,
			compiled,
			local.ConfigKey+"/"+local.Variation.Key,
		)
		expected, err := marshalPayload(local.Variation)
		require.NoError(t, err)
		assert.JSONEq(t, string(expected), string(resource.Payload))
		assert.True(t, resource.Upsert)
	}
}

func TestStore_RenderVariationsMatchesWrittenFile(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	resource := localVariation("preview")

	rendered, err := store.RenderVariations([]VariationFile{resource})

	require.NoError(t, err)
	require.Len(t, rendered, 1)
	assert.Equal(t, "project/configs/config/preview.prompt.md", rendered[0].Path)
	_, err = os.Stat(filepath.Join(root, syncdomain.RootDir))
	assert.ErrorIs(t, err, os.ErrNotExist)

	paths, err := store.Bootstrap([]VariationFile{resource})
	require.NoError(t, err)
	assert.Equal(t, []string{rendered[0].Path}, paths)
	content, err := os.ReadFile(filepath.Join(
		root,
		syncdomain.RootDir,
		filepath.FromSlash(rendered[0].Path),
	))
	require.NoError(t, err)
	assert.Equal(t, rendered[0].Content, content)
}

func TestStore_AddNeverOverwritesExistingVariation(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	existing := VariationFile{
		ProjectKey: "project",
		ConfigKey:  "config",
		Upsert:     true,
		Variation: syncdomain.Variation{
			Mode: syncdomain.VariationModeCompletion,
			Key:  "existing",
			Name: "Existing",
		},
	}
	_, err := store.Add([]VariationFile{existing})
	require.NoError(t, err)

	path := filepath.Join(
		root,
		syncdomain.RootDir,
		"project",
		configsDir,
		"config",
		"existing.prompt.md",
	)
	before, err := os.ReadFile(path)
	require.NoError(t, err)

	existing.Variation.Name = "Changed on server"
	_, err = store.Add([]VariationFile{existing})
	require.ErrorIs(t, err, ErrVariationExists)

	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func TestStore_AddRollsBackNewFilesWhenOneExists(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	existing := localVariation("existing")
	_, err := store.Add([]VariationFile{existing})
	require.NoError(t, err)

	_, err = store.Add([]VariationFile{localVariation("new"), existing})
	require.ErrorIs(t, err, ErrVariationExists)

	exists, err := store.VariationExists("project", "config", "new")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestStore_BootstrapFailureLeavesNoDirectory(t *testing.T) {
	root := t.TempDir()
	resource := localVariation("../unsafe")

	_, err := NewStore(root).Bootstrap([]VariationFile{resource})
	require.ErrorContains(t, err, "must be a single path segment")
	_, statErr := os.Stat(filepath.Join(root, syncdomain.RootDir))
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestStore_RejectsUnsupportedMessageRole(t *testing.T) {
	root := t.TempDir()
	resource := localVariation("unsupported-role")
	resource.Variation.Messages = []syncdomain.Message{{Role: "developer", Content: "result"}}

	_, err := NewStore(root).Bootstrap([]VariationFile{resource})
	require.ErrorContains(t, err, `unsupported message role "developer"`)
	_, statErr := os.Stat(filepath.Join(root, syncdomain.RootDir))
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestStore_BootstrapRefusesExistingDirectory(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, syncdomain.RootDir), 0o755))

	_, err := NewStore(root).Bootstrap([]VariationFile{localVariation("new")})
	require.Error(t, err)
	assert.False(t, errors.Is(err, os.ErrNotExist))
}

func localVariation(key string) VariationFile {
	return VariationFile{
		ProjectKey: "project",
		ConfigKey:  "config",
		Upsert:     true,
		Variation: syncdomain.Variation{
			Mode: syncdomain.VariationModeCompletion,
			Key:  key,
			Name: key,
		},
	}
}

func requireVariationResource(
	t *testing.T,
	resources []syncdomain.SyncedResource,
	lookupKey string,
) syncdomain.SyncedResource {
	t.Helper()

	for _, resource := range resources {
		if resource.Kind == syncdomain.KindVariation && resource.LookupKey == lookupKey {
			return resource
		}
	}

	require.FailNow(t, "variation resource not found", lookupKey)

	return syncdomain.SyncedResource{}
}
