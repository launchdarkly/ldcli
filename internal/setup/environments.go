package setup

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
	environmentStyle     = lipgloss.NewStyle().PaddingLeft(4)
	selectedEnvItemStyle = lipgloss.NewStyle().PaddingLeft(2).Foreground(lipgloss.Color("170"))

	_ list.Item = environment{}
)

type environment struct {
	key  string
	name string
}

func (p environment) FilterValue() string { return "" }

type environmentModel struct {
	list   list.Model
	choice string
}

func NewEnvironment() (tea.Model, tea.Cmd) {
	environments := []environment{
		{
			key:  "env1",
			name: "environment 1",
		},
		{
			key:  "env2",
			name: "environment 2",
		},
	}

	l := list.New(environmentsToItems(environments), envDelegate{}, 30, 14)
	l.Title = "Select an environment"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)

	return environmentModel{
		list: l,
	}, nil
}

func (p environmentModel) Init() tea.Cmd {
	return nil
}

func (m environmentModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Enter):
			i, ok := m.list.SelectedItem().(environment)
			if ok {
				m.choice = i.key
			}
		case key.Matches(msg, keys.Quit):
			// p.quitting = true
			return m, tea.Quit
		default:
			m.list, cmd = m.list.Update(msg)
		}
	}

	return m, cmd
}

func (m environmentModel) View() string {
	// if p.quitting {
	// 	return ""
	// }
	if m.choice != "" {
		return fmt.Sprintf("You have selected %s", m.choice)
	}

	return "\n" + m.list.View()
}

type envDelegate struct{}

func (d envDelegate) Height() int                             { return 1 }
func (d envDelegate) Spacing() int                            { return 0 }
func (d envDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d envDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(environment)
	if !ok {
		return
	}

	str := fmt.Sprintf("%d. %s", index+1, i.name)

	fn := environmentStyle.Render
	if index == m.Index() {
		fn = func(s ...string) string {
			return selectedEnvItemStyle.Render("> " + strings.Join(s, " "))
		}
	}

	fmt.Fprint(w, fn(str))
}

func environmentsToItems(environments []environment) []list.Item {
	items := make([]list.Item, len(environments))
	for i, proj := range environments {
		items[i] = list.Item(proj)
	}

	return items
}
