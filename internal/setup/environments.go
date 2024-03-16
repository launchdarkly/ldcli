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
	choice    string
	err       error
	list      list.Model
	parentKey string
}

var environments = map[string][]environment{
	"proj1": {
		{
			Key:  "env1",
			Name: "environment 1",
		},
		{
			Key:  "env2",
			Name: "environment 2",
		},
	},
	"proj2": {
		{
			Key:  "env3",
			Name: "environment 3",
		},
		{
			Key:  "env4",
			Name: "environment 4",
		},
	},
	"proj3": {
		{
			Key:  "env5",
			Name: "environment 5",
		},
		{
			Key:  "env6",
			Name: "environment 6",
		},
	},
}

func getEnvironments(projKey string) ([]environment, error) {
	envList := environments[projKey]
	createNewOption := environment{Key: CreateNewResourceKey, Name: "Create a new environment"}
	envList = append(envList, createNewOption)
	return envList, nil
}

func NewEnvironment() tea.Model {
	l := list.New(nil, envDelegate{}, 30, 14)
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

func (m environmentModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case fetchResources:
		envs, err := getEnvironments(m.parentKey)
		if err != nil {
			m.err = err
			return m, nil
		}
		m.list.SetItems(environmentsToItems(envs))
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
