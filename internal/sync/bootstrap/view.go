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

	selectedStyle = lipgloss.NewStyle().Foreground(selectionColor)
	noticeStyle   = lipgloss.NewStyle().Bold(true)
	mutedStyle    = lipgloss.NewStyle().Faint(true)
)

type variationDelegate struct{}

func (variationDelegate) Height() int  { return 2 }
func (variationDelegate) Spacing() int { return 1 }
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
		fmt.Fprintln(writer, mutedStyle.Render(title))
		fmt.Fprint(writer, mutedStyle.Render(description))
	case index == model.Index():
		fmt.Fprintln(writer, selectedStyle.Render(title))
		fmt.Fprint(writer, selectedStyle.Render(description))
	default:
		fmt.Fprintln(writer, title)
		fmt.Fprint(writer, description)
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
	delegate.Styles.FilterMatch = delegate.Styles.FilterMatch.Foreground(selectionColor)
	return delegate
}

func applyListTheme(model *list.Model) {
	model.Styles.Title = lipgloss.NewStyle().Bold(true).Padding(0, 1)
	model.Styles.FilterPrompt = lipgloss.NewStyle()
	model.Styles.FilterCursor = lipgloss.NewStyle().Foreground(selectionColor)
	model.Styles.StatusBarActiveFilter = lipgloss.NewStyle()
	model.Styles.ActivePaginationDot = model.Styles.ActivePaginationDot.Foreground(selectionColor)
	model.FilterInput.PromptStyle = model.Styles.FilterPrompt
	model.FilterInput.Cursor.Style = model.Styles.FilterCursor
	model.Paginator.ActiveDot = model.Styles.ActivePaginationDot.String()
}

func variationListHints() func() []key.Binding {
	return func() []key.Binding {
		return []key.Binding{
			key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "toggle")),
			key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "select all")),
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "write")),
			key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "back")),
		}
	}
}

func (m model) View() string {
	if m.err != nil || m.canceled {
		return ""
	}

	switch m.step {
	case selectProject:
		if !m.projects.ready {
			return m.loadingView("Loading projects")
		}
		return m.listView(m.projects.Model)

	case selectConfig:
		if !m.configs.ready {
			return m.loadingView("Loading Configs")
		}
		return m.listView(m.configs.Model)

	case selectVariations:
		if !m.variationsReady {
			return m.loadingView("Loading variations")
		}
		var view strings.Builder
		view.WriteString(m.variationList.View())
		selected, _ := m.variationCounts()
		fmt.Fprintf(&view, "\nSelected: %d", selected)
		if m.notice != "" {
			view.WriteString("\n")
			view.WriteString(noticeStyle.Render(m.notice))
		}
		return view.String()

	default:
		return ""
	}
}

func (m model) loadingView(label string) string {
	return fmt.Sprintf("\n  %s...\n", label)
}

func (m model) listView(items list.Model) string {
	view := items.View()
	if m.searching {
		view += "\nSearching..."
	}
	return view
}
