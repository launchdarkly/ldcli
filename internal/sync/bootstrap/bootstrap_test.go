package bootstrap

import (
	"bytes"
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

func TestWriteSummary(t *testing.T) {
	var output bytes.Buffer

	writeSummary(
		&output,
		true,
		false,
		[]string{"a.prompt.md", "b.prompt.md"},
	)
	assert.Equal(
		t,
		"Bootstrapped 2 variation files in .launchdarkly.\n",
		output.String(),
	)

	output.Reset()
	writeSummary(&output, false, true, nil)
	assert.Contains(t, output.String(), "No variations added")
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
	result.variations = newVariationList(raw, 80, 20)

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
