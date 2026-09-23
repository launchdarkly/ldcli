package link

import (
	"fmt"
	"io"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
)

type choice[T any] struct {
	title       string
	description string
	value       T
}

func (item choice[T]) Title() string       { return item.title }
func (item choice[T]) Description() string { return item.description }
func (item choice[T]) FilterValue() string { return item.title + " " + item.description }

type picker[T any] struct {
	list     list.Model
	selected *choice[T]
	canceled bool
}

func newPicker[T any](title string, choices []choice[T]) picker[T] {
	items := make([]list.Item, len(choices))
	for index := range choices {
		items[index] = choices[index]
	}
	selectionColor := lipgloss.Color("2")
	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.Foreground(selectionColor).BorderForeground(selectionColor)
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.Foreground(selectionColor).BorderForeground(selectionColor)
	delegate.Styles.FilterMatch = delegate.Styles.FilterMatch.Foreground(selectionColor)

	model := list.New(items, delegate, 0, 0)
	model.Title = title
	model.SetShowStatusBar(false)
	model.Styles.Title = lipgloss.NewStyle().Bold(true).Padding(0, 1)
	model.Styles.FilterPrompt = lipgloss.NewStyle()
	model.Styles.FilterCursor = lipgloss.NewStyle().Foreground(selectionColor)
	model.Styles.StatusBarActiveFilter = lipgloss.NewStyle()
	model.Styles.ActivePaginationDot = model.Styles.ActivePaginationDot.Foreground(selectionColor)
	model.FilterInput.PromptStyle = model.Styles.FilterPrompt
	model.FilterInput.Cursor.Style = model.Styles.FilterCursor
	model.Paginator.ActiveDot = model.Styles.ActivePaginationDot.String()
	return picker[T]{list: model}
}

func (model picker[T]) Init() tea.Cmd { return nil }

func (model picker[T]) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		model.list.SetSize(message.Width, message.Height)
	case tea.KeyMsg:
		switch message.String() {
		case "enter":
			if selected, ok := model.list.SelectedItem().(choice[T]); ok {
				model.selected = &selected
				return model, tea.Quit
			}
		case "q", "ctrl+c", "esc":
			model.canceled = true
			return model, tea.Quit
		}
	}

	var command tea.Cmd
	model.list, command = model.list.Update(message)
	return model, command
}

func (model picker[T]) View() string { return model.list.View() }

// choose displays one typed list of choices and returns the selected value.
func choose[T any](input io.Reader, output io.Writer, title string, choices []choice[T]) (choice[T], bool, error) {
	if len(choices) == 0 {
		return choice[T]{}, false, fmt.Errorf("%s: no choices are available", title)
	}
	result, err := tea.NewProgram(
		newPicker(title, choices),
		tea.WithAltScreen(),
		tea.WithInput(input),
		tea.WithOutput(output),
	).Run()
	if err != nil {
		return choice[T]{}, false, err
	}
	model, ok := result.(picker[T])
	if !ok {
		return choice[T]{}, false, fmt.Errorf("picker returned an unexpected model")
	}
	if model.canceled || model.selected == nil {
		return choice[T]{}, true, nil
	}
	return *model.selected, false, nil
}

func projectChoices(projects []syncapi.Project) []choice[syncapi.Project] {
	result := make([]choice[syncapi.Project], 0, len(projects))
	for _, project := range projects {
		result = append(result, choice[syncapi.Project]{title: project.Name, description: project.Key, value: project})
	}
	return result
}

func configChoices(configs []syncapi.Config) []choice[syncapi.Config] {
	result := make([]choice[syncapi.Config], 0, len(configs))
	for _, config := range configs {
		result = append(result, choice[syncapi.Config]{
			title: config.Name, description: fmt.Sprintf("%s · %s", config.Key, config.Mode), value: config,
		})
	}
	return result
}

func modelConfigChoices(configs []syncapi.ModelConfig) []choice[syncapi.ModelConfig] {
	result := make([]choice[syncapi.ModelConfig], 0, len(configs))
	for _, config := range configs {
		result = append(result, choice[syncapi.ModelConfig]{title: config.Name, description: config.Key, value: config})
	}
	return result
}
