package setup

// A simple example that shows how to retrieve a value from a Bubble Tea
// program after the Bubble Tea has exited.

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
	choiceStyle             = lipgloss.NewStyle().PaddingLeft(4)
	selectedChoiceItemStyle = lipgloss.NewStyle().PaddingLeft(2).Foreground(lipgloss.Color("170"))

	_ list.Item = choice{}
)

type choice struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

func (p choice) FilterValue() string { return "" }

type autoCreateModel struct {
	choice string
	err    error
	list   list.Model
}

func NewAutoCreate() autoCreateModel {
	choices := []choice{
		{
			Key:  "yes",
			Name: "Yes",
		},
		{
			Key:  "no",
			Name: "No",
		},
	}
	l := list.New(choicesToItems(choices), autoCreateDelegate{}, 85, 14)
	l.Title = "Do you want to get started with our recommended project, environment, and flag?"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)

	return autoCreateModel{
		list: l,
	}
}

func (m autoCreateModel) Init() tea.Cmd {
	return nil
}

func (m autoCreateModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Enter):
			i, ok := m.list.SelectedItem().(choice)
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

func (m autoCreateModel) View() string {
	return "\n" + m.list.View()
}

type autoCreateDelegate struct{}

func (d autoCreateDelegate) Height() int                             { return 1 }
func (d autoCreateDelegate) Spacing() int                            { return 0 }
func (d autoCreateDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d autoCreateDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(choice)
	if !ok {
		return
	}

	str := i.Name

	fn := choiceStyle.Render
	if index == m.Index() {
		fn = func(s ...string) string {
			return selectedChoiceItemStyle.Render("> " + strings.Join(s, " "))
		}
	}

	fmt.Fprint(w, fn(str))
}

func choicesToItems(choices []choice) []list.Item {
	items := make([]list.Item, len(choices))
	for i, c := range choices {
		items[i] = list.Item(c)
	}

	return items
}
