package bootstrap

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
	syncmanifest "github.com/launchdarkly/ldcli/internal/sync/manifest"
)

func TestModelSelectsAndWritesVariations(t *testing.T) {
	root := t.TempDir()
	result := newModel(Options{
		Catalog: &fakeCatalog{},
		Store:   synclocal.NewStore(root),
		Initial: true,
	})
	result = updateModel(
		t,
		result,
		tea.WindowSizeMsg{Width: 80, Height: 24},
	)
	result = updateModel(t, result, projectsFetchedMsg{
		projects: []syncapi.Project{{Key: "project", Name: "Project"}},
	})

	next, command := result.handleEnter()
	result = next.(model)
	assert.Equal(t, selectConfig, result.step)
	assert.NotNil(t, command)

	result = updateModel(t, result, configsFetchedMsg{
		projectKey: "project",
		configs: []syncapi.Config{{
			Key:  "assistant",
			Name: "Assistant",
			Mode: syncdomain.VariationModeAgent,
		}},
	})

	next, command = result.handleEnter()
	result = next.(model)
	assert.Equal(t, selectVariations, result.step)
	assert.NotNil(t, command)

	result = updateModel(t, result, variationsLoadedMsg{
		projectKey: "project",
		config: syncapi.Config{
			Key:  "assistant",
			Name: "Assistant",
			Mode: syncdomain.VariationModeAgent,
			Variations: []syncdomain.Variation{
				{
					Mode: syncdomain.VariationModeAgent,
					Key:  "friendly",
					Name: "Friendly",
				},
				{
					Mode: syncdomain.VariationModeAgent,
					Key:  "existing",
					Name: "Existing",
				},
			},
		},
		existing: map[string]bool{"existing": true},
	})

	result.toggleVariation()
	selected, _ := result.variationCounts()
	assert.Equal(t, 1, selected)
	result.selectAllVariations()
	assert.False(t, result.variations.Items()[1].(variationItem).selected)

	next, command = result.handleEnter()
	result = next.(model)
	require.NotNil(t, command)

	files := result.selectedVariationFiles()
	require.Len(t, files, 1)
	paths, err := result.store.Bootstrap(files)
	require.NoError(t, err)
	assert.Equal(
		t,
		[]string{"project/configs/assistant/friendly.prompt.md"},
		paths,
	)

	contents, err := os.ReadFile(filepath.Join(
		root,
		syncdomain.RootDir,
		"project",
		"configs",
		"assistant",
		"friendly.prompt.md",
	))
	require.NoError(t, err)
	assert.Contains(t, string(contents), "mode: agent")

	_, err = os.Stat(filepath.Join(
		root,
		syncdomain.RootDir,
		"project",
		"configs",
		"assistant",
		"existing.prompt.md",
	))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestModelAllExistingIsNoOp(t *testing.T) {
	result := newVariationModel(
		t,
		variationItem{
			variation: syncdomain.Variation{
				Key:  "existing",
				Name: "Existing",
			},
			existing: true,
		},
	)

	next, command := result.handleEnter()
	result = next.(model)

	assert.Equal(t, selectVariations, result.step)
	selected, selectable := result.variationCounts()
	assert.Zero(t, selected)
	assert.Zero(t, selectable)
	assert.NotNil(t, command)
}

func TestModelRequiresVariationSelection(t *testing.T) {
	result := newVariationModel(
		t,
		variationItem{
			variation: syncdomain.Variation{
				Key:  "available",
				Name: "Available",
			},
		},
	)

	next, command := result.handleEnter()
	result = next.(model)

	assert.Equal(t, selectVariations, result.step)
	assert.Equal(
		t,
		"Select at least one variation to continue.",
		result.notice,
	)
	assert.Nil(t, command)
}

func TestModelFiltersLocallyWithoutQuitting(t *testing.T) {
	result := newModel(Options{
		Catalog: &fakeCatalog{},
		Store:   synclocal.NewStore(t.TempDir()),
	})
	result = updateModel(
		t,
		result,
		tea.WindowSizeMsg{Width: 80, Height: 24},
	)
	result = updateModel(t, result, projectsFetchedMsg{
		projects: []syncapi.Project{{Key: "project", Name: "Project"}},
	})

	result = updateModel(
		t,
		result,
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}},
	)
	require.True(t, result.isFiltering())

	result = updateModel(
		t,
		result,
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}},
	)
	assert.False(t, result.canceled)
	assert.True(t, result.isFiltering())
}

func TestFetchConfigSortsVariationsAndChecksExistingFiles(t *testing.T) {
	root := t.TempDir()
	store := synclocal.NewStore(root)
	_, err := store.Bootstrap([]synclocal.VariationFile{{
		ProjectKey: "project",
		ConfigKey:  "config",
		Variation: syncdomain.Variation{
			Mode: syncdomain.VariationModeCompletion,
			Key:  "existing",
			Name: "Existing",
		},
	}})
	require.NoError(t, err)

	result := newModel(Options{
		Catalog: &fakeCatalog{},
		Store:   store,
	})
	result.projectKey = "project"
	result.config = syncapi.Config{
		Key:  "config",
		Mode: syncdomain.VariationModeCompletion,
		Variations: []syncdomain.Variation{
			{Key: "z", Name: "Zulu"},
			{Key: "existing", Name: "Alpha"},
		},
	}

	message, ok := result.loadVariations()().(variationsLoadedMsg)
	require.True(t, ok)
	assert.Equal(t, "existing", message.config.Variations[0].Key)
	assert.True(t, message.existing["existing"])
	assert.False(t, message.existing["z"])
}

func TestLoadVariationsReturnsLocalInspectionError(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Mkdir(
		filepath.Join(root, syncdomain.RootDir),
		0o755,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(root, syncdomain.RootDir, "project"),
		[]byte("not a directory"),
		0o644,
	))

	result := newModel(Options{
		Catalog: &fakeCatalog{},
		Store:   synclocal.NewStore(root),
	})
	result.projectKey = "project"
	result.config = syncapi.Config{
		Key: "config",
		Variations: []syncdomain.Variation{{
			Key: "variation",
		}},
	}

	message, ok := result.loadVariations()().(errMsg)
	require.True(t, ok)
	require.ErrorContains(t, message.err, "inspect variation variation")
}

func TestModelAPIErrorQuitsWithError(t *testing.T) {
	result := newModel(Options{
		Catalog: &fakeCatalog{},
		Store:   synclocal.NewStore(t.TempDir()),
	})

	updated, command := result.Update(errMsg{err: assert.AnError})
	result = updated.(model)

	assert.ErrorIs(t, result.err, assert.AnError)
	assert.NotNil(t, command)
}

func TestModelIgnoresStaleAPIError(t *testing.T) {
	result := newModel(Options{
		Catalog: &fakeCatalog{},
		Store:   synclocal.NewStore(t.TempDir()),
	})

	updated, command := result.Update(errMsg{
		step:       selectConfig,
		projectKey: "old-project",
		err:        assert.AnError,
	})
	result = updated.(model)

	assert.NoError(t, result.err)
	assert.Nil(t, command)
}

func TestRunRequiresTerminal(t *testing.T) {
	err := Run(Options{
		Catalog: &fakeCatalog{},
		Store:   synclocal.NewStore(t.TempDir()),
		Input:   bytes.NewBuffer(nil),
		Output:  bytes.NewBuffer(nil),
		Initial: true,
	})

	require.ErrorContains(
		t,
		err,
		"interactive prompt selection requires a terminal",
	)
}

func TestFinishSelectionDryRunDoesNotCreateFiles(t *testing.T) {
	root := t.TempDir()
	var output bytes.Buffer

	err := finishSelection(Options{
		Store:   synclocal.NewStore(root),
		Output:  &output,
		Initial: true,
		DryRun:  true,
	}, []synclocal.VariationFile{{
		ProjectKey: "project",
		ConfigKey:  "config",
		Upsert:     true,
		Variation: syncdomain.Variation{
			Key:          "variation",
			Name:         "Variation",
			Mode:         syncdomain.VariationModeAgent,
			Instructions: "Be helpful.",
		},
	}})

	require.NoError(t, err)
	assert.Equal(
		t,
		`============================================================
File 1 of 1
Would create: .launchdarkly/project/configs/config/variation.prompt.md
------------------------------------------------------------
---
formatVersion: 1
upsert: true
mode: agent
key: variation
name: Variation
---

Be helpful.
============================================================
`,
		output.String(),
	)
	_, err = os.Stat(filepath.Join(root, syncdomain.RootDir))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestFinishSelectionWritesInitialManifest(t *testing.T) {
	root := t.TempDir()
	var output bytes.Buffer
	variation := syncdomain.Variation{
		Mode: syncdomain.VariationModeAgent, Key: "variation", Name: "Variation", Instructions: "Be helpful.",
	}

	err := finishSelection(Options{
		Store:    synclocal.NewStore(root),
		Manifest: syncmanifest.NewStore(root),
		Output:   &output,
		Initial:  true,
	}, []synclocal.VariationFile{{
		ProjectKey: "project", ConfigKey: "config", Upsert: true, Variation: variation,
	}})

	require.NoError(t, err)
	manifest, exists, err := syncmanifest.NewStore(root).Load()
	require.NoError(t, err)
	require.True(t, exists)
	require.Len(t, manifest.Resources, 1)
	expected, err := syncdomain.FingerprintVariation("project", "config/variation", variation)
	require.NoError(t, err)
	assert.Equal(t, expected, manifest.Resources[0].Fingerprint)
}

func TestFinishSelectionAddsMultipleVersionedVariationsToExistingManifest(t *testing.T) {
	root := t.TempDir()
	store := synclocal.NewStore(root)
	manifestStore := syncmanifest.NewStore(root)
	first := syncdomain.Variation{
		Mode: syncdomain.VariationModeAgent, Key: "first", Name: "First", Instructions: "First.",
	}
	second := syncdomain.Variation{
		Mode: syncdomain.VariationModeAgent, Key: "second", Name: "Second", Instructions: "Second.",
		ModelConfigKey: "model", ModelConfigVersion: 2,
	}
	third := syncdomain.Variation{
		Mode: syncdomain.VariationModeAgent, Key: "third", Name: "Third", Instructions: "Third.",
		ModelConfigKey: "model", ModelConfigVersion: 3,
	}
	require.NoError(t, finishSelection(Options{
		Store: store, Manifest: manifestStore, Output: io.Discard, Initial: true,
	}, []synclocal.VariationFile{{
		ProjectKey: "project", ConfigKey: "config", Variation: first,
	}}))

	require.NoError(t, finishSelection(Options{
		Store: store, Manifest: manifestStore, Output: io.Discard,
	}, []synclocal.VariationFile{
		{ProjectKey: "project", ConfigKey: "config", Variation: second},
		{ProjectKey: "project", ConfigKey: "config", Variation: third},
	}))

	manifest, exists, err := manifestStore.Load()
	require.NoError(t, err)
	require.True(t, exists)
	require.Len(t, manifest.Resources, 3)
	assert.Equal(t, "config/first", manifest.Resources[0].LookupKey)
	assert.Equal(t, "config/second", manifest.Resources[1].LookupKey)
	assert.Equal(t, "config/third", manifest.Resources[2].LookupKey)
}

func TestWriteSummary(t *testing.T) {
	var output bytes.Buffer

	writeSummary(
		&output,
		true,
		2,
	)
	assert.Equal(
		t,
		"Bootstrapped 2 variation files in .launchdarkly.\n",
		output.String(),
	)

	output.Reset()
	writeNoChangeSummary(&output, false)
	assert.Contains(t, output.String(), "No variations added")
}

func TestWritePreviewsPrintsEveryFile(t *testing.T) {
	var output bytes.Buffer

	writePreviews(&output, []synclocal.RenderedVariationFile{
		{Path: "project/configs/config/first.prompt.md", Content: []byte("first\n")},
		{Path: "project/configs/config/second.prompt.md", Content: []byte("second\n")},
	})

	assert.Equal(
		t,
		`============================================================
File 1 of 2
Would create: .launchdarkly/project/configs/config/first.prompt.md
------------------------------------------------------------
first
============================================================

============================================================
File 2 of 2
Would create: .launchdarkly/project/configs/config/second.prompt.md
------------------------------------------------------------
second
============================================================
`,
		output.String(),
	)
}

func newVariationModel(t *testing.T, items ...variationItem) model {
	t.Helper()

	raw := make([]list.Item, len(items))
	for index, item := range items {
		raw[index] = item
	}

	result := newModel(Options{
		Catalog: &fakeCatalog{},
		Store:   synclocal.NewStore(t.TempDir()),
	})
	result.step = selectVariations
	result.variationsReady = true
	result.variations = newVariationList(raw, 80, 20, false)

	return result
}

func updateModel(t *testing.T, current model, message tea.Msg) model {
	t.Helper()

	updated, _ := current.Update(message)
	result, ok := updated.(model)
	require.True(t, ok)

	return result
}

type fakeCatalog struct{}

var _ Catalog = &fakeCatalog{}

func (*fakeCatalog) Projects() ([]syncapi.Project, error) {
	return nil, nil
}

func (*fakeCatalog) Configs(string) ([]syncapi.Config, error) {
	return nil, nil
}
