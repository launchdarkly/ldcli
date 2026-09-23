package detach

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var detachSelectionColor = lipgloss.Color("2")

type resourceItem struct {
	resource Resource
	selected bool
}

func (item resourceItem) Title() string {
	return item.resource.ProjectKey + "/" + item.resource.LookupKey
}
func (item resourceItem) Description() string { return string(item.resource.Kind) }
func (item resourceItem) FilterValue() string { return item.Title() + " " + item.Description() }

type resourceDelegate struct{}

func (resourceDelegate) Height() int                         { return 2 }
func (resourceDelegate) Spacing() int                        { return 1 }
func (resourceDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }
func (resourceDelegate) Render(writer io.Writer, model list.Model, index int, raw list.Item) {
	item, ok := raw.(resourceItem)
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

	title := fmt.Sprintf("%s%s %s", cursor, checkbox, item.Title())
	description := "      " + item.Description()
	if index == model.Index() {
		style := lipgloss.NewStyle().Foreground(detachSelectionColor)
		_, _ = fmt.Fprintln(writer, style.Render(title))
		_, _ = fmt.Fprint(writer, style.Render(description))
		return
	}
	_, _ = fmt.Fprintln(writer, title)
	_, _ = fmt.Fprint(writer, description)
}

type model struct {
	resources list.Model
	notice    string
	canceled  bool
}

// newModel creates the interactive multi-resource selector.
func newModel(resources []Resource) model {
	items := make([]list.Item, len(resources))
	for index, resource := range resources {
		items[index] = resourceItem{resource: resource}
	}

	selector := list.New(items, resourceDelegate{}, 0, 0)
	selector.Title = "Select resources to detach"
	selector.SetShowStatusBar(false)
	selector.Styles.Title = lipgloss.NewStyle().Bold(true).Padding(0, 1)
	selector.Styles.FilterPrompt = lipgloss.NewStyle()
	selector.Styles.FilterCursor = lipgloss.NewStyle().Foreground(detachSelectionColor)
	selector.Styles.StatusBarActiveFilter = lipgloss.NewStyle()
	selector.Styles.ActivePaginationDot = selector.Styles.ActivePaginationDot.Foreground(detachSelectionColor)
	selector.FilterInput.PromptStyle = selector.Styles.FilterPrompt
	selector.FilterInput.Cursor.Style = selector.Styles.FilterCursor
	selector.Paginator.ActiveDot = selector.Styles.ActivePaginationDot.String()
	selector.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{
			key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "toggle")),
			key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "select all")),
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "detach")),
		}
	}
	return model{resources: selector}
}

func (state model) Init() tea.Cmd { return nil }

func (state model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		state.resources.SetSize(message.Width, max(1, message.Height-2))
	case tea.KeyMsg:
		if state.resources.FilterState() != list.Filtering {
			switch message.String() {
			case "ctrl+c", "q", "esc":
				state.canceled = true
				return state, tea.Quit
			case " ":
				return state, state.toggleSelected()
			case "a":
				return state, state.selectAll()
			case "enter":
				if len(state.selectedResources()) == 0 {
					state.notice = "Select at least one resource."
					return state, nil
				}
				return state, tea.Quit
			}
		}
	}

	var command tea.Cmd
	state.resources, command = state.resources.Update(message)
	return state, command
}

func (state model) View() string {
	if state.canceled {
		return ""
	}
	var view strings.Builder
	view.WriteString(state.resources.View())
	_, _ = fmt.Fprintf(&view, "\nSelected: %d", len(state.selectedResources()))
	if state.notice != "" {
		_, _ = fmt.Fprintf(&view, "\n%s", lipgloss.NewStyle().Bold(true).Render(state.notice))
	}
	return view.String()
}

// toggleSelected toggles the currently visible resource.
func (state *model) toggleSelected() tea.Cmd {
	selected, ok := state.resources.SelectedItem().(resourceItem)
	if !ok {
		return nil
	}
	items := state.resources.Items()
	for index, raw := range items {
		item, ok := raw.(resourceItem)
		if ok && item.resource == selected.resource {
			item.selected = !item.selected
			items[index] = item
			break
		}
	}
	state.notice = ""
	return state.resources.SetItems(items)
}

// selectAll selects every available resource.
func (state *model) selectAll() tea.Cmd {
	items := state.resources.Items()
	for index, raw := range items {
		item, ok := raw.(resourceItem)
		if ok {
			item.selected = true
			items[index] = item
		}
	}
	state.notice = ""
	return state.resources.SetItems(items)
}

// selectedResources returns selected resources in display order.
func (state model) selectedResources() []Resource {
	var resources []Resource
	for _, raw := range state.resources.Items() {
		item, ok := raw.(resourceItem)
		if ok && item.selected {
			resources = append(resources, item.resource)
		}
	}
	return resources
}
