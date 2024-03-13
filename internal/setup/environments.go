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
	Key  string `json:"key"`
	Name string `json:"name"`
}

func (p environment) FilterValue() string { return "" }

type environmentModel struct {
	choice string
	err    error
	list   list.Model
}

func NewEnvironment() tea.Model {
	environments := []environment{
		{
			Key:  "env1",
			Name: "environment 1",
		},
		{
			Key:  "env2",
			Name: "environment 2",
		},
	}

	l := list.New(environmentsToItems(environments), envDelegate{}, 30, 14)
	l.Title = "Select an environment"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)

	return environmentModel{
		list: l,
	}
}

func (p environmentModel) Init() tea.Cmd {
	return nil
}

// This method has drifted from the ProjectModel's version, but it should do something similar.
func (m environmentModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Enter):
			i, ok := m.list.SelectedItem().(environment)
			if ok {
				m.choice = i.Key
			}
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit
		default:
			m.list, cmd = m.list.Update(msg)
		}
	}

	return m, cmd
}

func (m environmentModel) View() string {
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

	str := fmt.Sprintf("%d. %s", index+1, i.Name)

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
	for i, e := range environments {
		items[i] = list.Item(e)
	}

	return items
}
