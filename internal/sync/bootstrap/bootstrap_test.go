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
		API:     testAPIClient(),
		Store:   synclocal.NewStore(root),
		Initial: true,
	})
	result = updateModel(t, result, tea.WindowSizeMsg{Width: 80, Height: 24})
	result = updateModel(t, result, projectResults(
		"",
		syncapi.Project{Key: "project", Name: "Project"},
	))

	next, cmd := result.handleEnter()
	result = next.(model)
	assert.Equal(t, selectConfig, result.step)
	assert.NotNil(t, cmd)

	result = updateModel(t, result, remoteFetchedMsg{
		step:       selectConfig,
		projectKey: "project",
		items: []list.Item{configItem{config: syncapi.Config{
			Key: "assistant", Name: "Assistant", Mode: syncdomain.VariationModeAgent,
		}}},
	})
	next, cmd = result.handleEnter()
	result = next.(model)
	assert.Equal(t, selectVariations, result.step)
	assert.NotNil(t, cmd)

	result = updateModel(t, result, configFetchedMsg{
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
	assert.False(t, result.variationList.Items()[1].(variationItem).selected)

	next, cmd = result.handleEnter()
	result = next.(model)
	require.NotNil(t, cmd)

	files := result.selectedVariationFiles()
	require.Len(t, files, 1)
	paths, err := result.store.Bootstrap(files)
	require.NoError(t, err)
	assert.Equal(t, []string{"project/configs/assistant/friendly.prompt.md"}, paths)

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
	result := newModel(Options{
		API:   testAPIClient(),
		Store: synclocal.NewStore(t.TempDir()),
	})
	result.step = selectVariations
	result.variationsReady = true
	result.variationList = newVariationList([]list.Item{
		variationItem{
			variation: syncdomain.Variation{Key: "existing", Name: "Existing"},
			existing:  true,
		},
	}, 80, 20)

	next, cmd := result.handleEnter()
	result = next.(model)
	assert.Equal(t, selectVariations, result.step)
	selected, _ := result.variationCounts()
	assert.Zero(t, selected)
	assert.NotNil(t, cmd)
}

func TestModelRequiresSelection(t *testing.T) {
	result := newModel(Options{
		API:   testAPIClient(),
		Store: synclocal.NewStore(t.TempDir()),
	})
	result.step = selectVariations
	result.variationsReady = true
	result.variationList = newVariationList([]list.Item{
		variationItem{variation: syncdomain.Variation{Key: "available", Name: "Available"}},
	}, 80, 20)

	next, cmd := result.handleEnter()
	result = next.(model)
	assert.Equal(t, selectVariations, result.step)
	assert.Equal(t, "Select at least one variation to continue.", result.notice)
	assert.Nil(t, cmd)
}

func TestModelIgnoresStaleSearchResultsAndErrors(t *testing.T) {
	result := newModel(Options{
		API:   testAPIClient(),
		Store: synclocal.NewStore(t.TempDir()),
	})
	result = updateModel(t, result, projectResults(
		"",
		syncapi.Project{Key: "original", Name: "Original"},
	))
	result.projects.SetFilterText("customer")

	result = updateModel(t, result, projectResults(
		"old query",
		syncapi.Project{Key: "stale", Name: "Stale"},
	))
	assert.Equal(t, "original", result.projects.Items()[0].(projectItem).project.Key)
	result = updateModel(t, result, fetchFailedMsg{
		step:  selectProject,
		query: "old query",
		err:   assert.AnError,
	})
	assert.NoError(t, result.err)

	result = updateModel(t, result, projectResults(
		"customer",
		syncapi.Project{Key: "current", Name: "Current"},
	))
	assert.Equal(t, "current", result.projects.Items()[0].(projectItem).project.Key)
	require.Len(t, result.projects.VisibleItems(), 1)
	assert.Equal(t, "customer", result.projects.query)
}

func TestModelUsesConfigLabel(t *testing.T) {
	result := newModel(Options{
		API:   testAPIClient(),
		Store: synclocal.NewStore(t.TempDir()),
	})
	result.projectKey = "project"
	result.step = selectConfig
	result = updateModel(t, result, tea.WindowSizeMsg{Width: 80, Height: 24})
	result = updateModel(t, result, remoteFetchedMsg{
		step:       selectConfig,
		projectKey: "project",
		items: []list.Item{configItem{config: syncapi.Config{
			Key: "config", Name: "Config", Mode: syncdomain.VariationModeCompletion,
		}}},
	})

	assert.Equal(t, "Select a Config", result.configs.Title)
	assert.Contains(t, result.View(), "Select a Config")
}

func TestFetchConfigsScopesSearchAndModes(t *testing.T) {
	client := &fakeClient{}
	result := newModel(Options{
		API:   client,
		Store: synclocal.NewStore(t.TempDir()),
	})
	result.projectKey = "project"

	message, ok := result.fetchConfigs("support")().(remoteFetchedMsg)
	require.True(t, ok)
	assert.Equal(t, "project", message.projectKey)
	assert.Equal(t, "support", message.query)
	assert.Equal(t, "project", client.projectKey)
	assert.Equal(t, "support", client.search)
}

func TestModelFilterInputDoesNotQuit(t *testing.T) {
	result := newModel(Options{
		API:   testAPIClient(),
		Store: synclocal.NewStore(t.TempDir()),
	})
	result = updateModel(t, result, projectResults(
		"",
		syncapi.Project{Key: "project", Name: "Project"},
	))
	result = updateModel(t, result, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	require.True(t, result.isFiltering())

	result = updateModel(t, result, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	assert.False(t, result.canceled)
	assert.True(t, result.isFiltering())
}

func TestModelAPIErrorQuitsWithError(t *testing.T) {
	result := newModel(Options{
		API:   testAPIClient(),
		Store: synclocal.NewStore(t.TempDir()),
	})

	updated, cmd := result.Update(errMsg{err: assert.AnError})
	result = updated.(model)
	assert.ErrorIs(t, result.err, assert.AnError)
	assert.NotNil(t, cmd)
}

func TestRunRequiresTerminal(t *testing.T) {
	err := Run(Options{
		API:     testAPIClient(),
		Store:   synclocal.NewStore(t.TempDir()),
		Input:   bytes.NewBuffer(nil),
		Output:  bytes.NewBuffer(nil),
		Initial: true,
	})
	require.ErrorContains(t, err, "interactive prompt selection requires a terminal")
}

func TestWriteSummary(t *testing.T) {
	var output bytes.Buffer
	writeSummary(&output, true, false, []string{"a.prompt.md", "b.prompt.md"})
	assert.Equal(t, "Bootstrapped 2 variation files in .launchdarkly.\n", output.String())

	output.Reset()
	writeSummary(&output, false, true, nil)
	assert.Contains(t, output.String(), "No variations added")
}

func updateModel(t *testing.T, current model, message tea.Msg) model {
	t.Helper()
	updated, _ := current.Update(message)
	result, ok := updated.(model)
	require.True(t, ok)
	return result
}

func testAPIClient() Client {
	return &fakeClient{}
}

type fakeClient struct {
	projectKey string
	search     string
}

var _ Client = &fakeClient{}

func (*fakeClient) Projects(string) ([]syncapi.Project, error) {
	return nil, nil
}

func (client *fakeClient) Configs(
	projectKey, search string,
) ([]syncapi.Config, error) {
	client.projectKey = projectKey
	client.search = search
	return nil, nil
}

func (*fakeClient) Config(string, string) (syncapi.Config, error) {
	return syncapi.Config{}, nil
}

func projectResults(query string, projects ...syncapi.Project) remoteFetchedMsg {
	items := make([]list.Item, len(projects))
	for index, project := range projects {
		items[index] = projectItem{project: project}
	}
	return remoteFetchedMsg{step: selectProject, query: query, items: items}
}
