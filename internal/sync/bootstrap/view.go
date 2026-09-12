package bootstrap

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	selectionColor = lipgloss.Color("2")
	selectedStyle  = lipgloss.NewStyle().Foreground(selectionColor)
	noticeStyle    = lipgloss.NewStyle().Bold(true)
	mutedStyle     = lipgloss.NewStyle().Faint(true)
)

type variationDelegate struct{}

func (variationDelegate) Height() int {
	return 2
}

func (variationDelegate) Spacing() int {
	return 1
}

func (variationDelegate) Update(tea.Msg, *list.Model) tea.Cmd {
	return nil
}

func (variationDelegate) Render(
	writer io.Writer,
	model list.Model,
	index int,
	raw list.Item,
) {
	item, ok := raw.(variationItem)
	if !ok {
		return
	}

	cursor := "  "
	if index == model.Index() {
		cursor = "> "
	}

	checkbox := "[ ]"
	if item.selected {
		checkbox = "[x]"
	}
	if item.existing {
		checkbox = "[-]"
	}

	title := fmt.Sprintf("%s%s %s", cursor, checkbox, item.Title())
	description := "     " + item.Description()

	switch {
	case item.existing:
		_, _ = fmt.Fprintln(writer, mutedStyle.Render(title))
		_, _ = fmt.Fprint(writer, mutedStyle.Render(description))
	case index == model.Index():
		_, _ = fmt.Fprintln(writer, selectedStyle.Render(title))
		_, _ = fmt.Fprint(writer, selectedStyle.Render(description))
	default:
		_, _ = fmt.Fprintln(writer, title)
		_, _ = fmt.Fprint(writer, description)
	}
}

func themedDefaultDelegate() list.DefaultDelegate {
	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.
		Foreground(selectionColor).
		BorderForeground(selectionColor)
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.
		Foreground(selectionColor).
		BorderForeground(selectionColor)
	delegate.Styles.FilterMatch = delegate.Styles.FilterMatch.
		Foreground(selectionColor)

	return delegate
}

func applyListTheme(model *list.Model) {
	model.Styles.Title = lipgloss.NewStyle().Bold(true).Padding(0, 1)
	model.Styles.FilterPrompt = lipgloss.NewStyle()
	model.Styles.FilterCursor = lipgloss.NewStyle().Foreground(selectionColor)
	model.Styles.StatusBarActiveFilter = lipgloss.NewStyle()
	model.Styles.ActivePaginationDot = model.Styles.ActivePaginationDot.
		Foreground(selectionColor)
	model.FilterInput.PromptStyle = model.Styles.FilterPrompt
	model.FilterInput.Cursor.Style = model.Styles.FilterCursor
	model.Paginator.ActiveDot = model.Styles.ActivePaginationDot.String()
}

func variationListHints() func() []key.Binding {
	return func() []key.Binding {
		return []key.Binding{
			key.NewBinding(
				key.WithKeys("space"),
				key.WithHelp("space", "toggle"),
			),
			key.NewBinding(
				key.WithKeys("a"),
				key.WithHelp("a", "select all"),
			),
			key.NewBinding(
				key.WithKeys("enter"),
				key.WithHelp("enter", "write"),
			),
			key.NewBinding(
				key.WithKeys("b"),
				key.WithHelp("b", "back"),
			),
		}
	}
}

func (model model) View() string {
	if model.err != nil || model.canceled {
		return ""
	}

	switch model.step {
	case selectProject:
		if !model.projectsReady {
			return loadingView("Loading projects")
		}
		return model.projects.View()

	case selectConfig:
		if !model.configsReady {
			return loadingView("Loading AI Configs")
		}
		return model.configs.View()

	case selectVariations:
		if !model.variationsReady {
			return loadingView("Loading prompt variations")
		}

		var view strings.Builder
		view.WriteString(model.variations.View())
		selected, _ := model.variationCounts()
		_, _ = fmt.Fprintf(&view, "\nSelected: %d", selected)
		if model.notice != "" {
			view.WriteString("\n")
			view.WriteString(noticeStyle.Render(model.notice))
		}

		return view.String()

	default:
		return ""
	}
}

func loadingView(label string) string {
	return fmt.Sprintf("\n  %s...\n", label)
}
