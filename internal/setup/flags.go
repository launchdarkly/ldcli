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
	flagStyle             = lipgloss.NewStyle().PaddingLeft(4)
	selectedFlagItemStyle = lipgloss.NewStyle().PaddingLeft(2).Foreground(lipgloss.Color("170"))

	_ list.Item = flag{}
)

type flag struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

func (p flag) FilterValue() string { return "" }

type flagModel struct {
	choice string
	err    error
	list   list.Model
}

func NewFlag() tea.Model {
	flags := []flag{
		{
			Key:  "flag1",
			Name: "flag 1",
		},
		{
			Key:  "flag2",
			Name: "flag 2",
		},
	}

	l := list.New(flagsToItems(flags), flagDelegate{}, 30, 14)
	l.Title = "Select a flag"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)

	return flagModel{
		list: l,
	}
}

func (p flagModel) Init() tea.Cmd {
	return nil
}

// This method has drifted from the ProjectModel's version, but it should do something similar.
func (m flagModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Enter):
			i, ok := m.list.SelectedItem().(flag)
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

func (m flagModel) View() string {
	return "\n" + m.list.View()
}

type flagDelegate struct{}

func (d flagDelegate) Height() int                             { return 1 }
func (d flagDelegate) Spacing() int                            { return 0 }
func (d flagDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d flagDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(flag)
	if !ok {
		return
	}

	str := fmt.Sprintf("%d. %s", index+1, i.Name)

	fn := flagStyle.Render
	if index == m.Index() {
		fn = func(s ...string) string {
			return selectedFlagItemStyle.Render("> " + strings.Join(s, " "))
		}
	}

	fmt.Fprint(w, fn(str))
}

func flagsToItems(flags []flag) []list.Item {
	items := make([]list.Item, len(flags))
	for i, proj := range flags {
		items[i] = list.Item(proj)
	}

	return items
}
