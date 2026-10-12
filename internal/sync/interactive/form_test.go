package interactive

import (
	"bytes"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChoiceLabelPlacesDescriptionBelowTitle(t *testing.T) {
	assert.Equal(
		t,
		"Support prompt\nKey: support\n",
		choiceLabel(Choice[string]{
			Title:       "Support prompt",
			Description: "Key: support",
		}),
	)
	assert.Equal(
		t,
		"Support prompt",
		choiceLabel(Choice[string]{Title: "Support prompt"}),
	)
}

func TestFormThemeDoesNotRenderFieldSidebars(t *testing.T) {
	theme := formTheme()

	assert.Equal(t, "content", theme.Focused.Base.Render("content"))
	assert.Equal(t, "content", theme.Focused.Card.Render("content"))
	assert.Equal(t, "content", theme.Blurred.Base.Render("content"))
	assert.Equal(t, "content", theme.Blurred.Card.Render("content"))
}

func TestSelectUsesAlternateScreen(t *testing.T) {
	var output bytes.Buffer

	selected, canceled, err := Select(
		strings.NewReader("\r"),
		&output,
		"Choose",
		[]Choice[string]{{Title: "First", Value: "first"}},
	)

	require.NoError(t, err)
	assert.False(t, canceled)
	assert.Equal(t, "first", selected)
	assert.Contains(t, output.String(), "\x1b[?1049h")
	assert.Contains(t, output.String(), "\x1b[?1049l")
}

func TestSearchModelUsesBackendForSlashSearch(t *testing.T) {
	var queries []string
	model := newSearchModel(SearchOptions[string]{
		SearchTitle:       "Search projects",
		SearchPlaceholder: "Name or key",
		SelectTitle:       "Choose a project",
		ItemName:          "projects",
		Fetch: func(query string, _, _ int) ([]string, int, error) {
			queries = append(queries, query)
			return []string{"support"}, 1, nil
		},
		Choice: func(value string) Choice[string] {
			return Choice[string]{Title: "Support", Description: value, Value: value}
		},
	}, []string{"alpha"}, 1, 25)

	_, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	require.NotNil(t, command)
	assert.True(t, model.searching)

	model.input.SetValue("support")
	_, command = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, command)
	assert.Empty(t, queries)

	_, command = model.Update(command())
	assert.Nil(t, command)
	assert.Equal(t, []string{"support"}, queries)
	assert.Equal(t, []string{"support"}, model.items)

	_, command = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, command)
	assert.True(t, model.selected)
	assert.Equal(t, "support", model.value)
}

func TestSearchModelLoadsTheNextBackendPage(t *testing.T) {
	var offsets []int
	model := newSearchModel(SearchOptions[string]{
		SelectTitle: "Choose",
		ItemName:    "items",
		Fetch: func(_ string, _, offset int) ([]string, int, error) {
			offsets = append(offsets, offset)
			return []string{"third"}, 3, nil
		},
		Choice: func(value string) Choice[string] {
			return Choice[string]{Title: value, Value: value}
		},
	}, []string{"first", "second"}, 3, 2)
	model.list.Select(2)

	_, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, command)
	_, _ = model.Update(command())

	assert.Equal(t, []int{2}, offsets)
	assert.Equal(t, []string{"first", "second", "third"}, model.items)
	assert.False(t, model.hasMore)
	assert.Equal(t, 2, model.list.Index())
}
