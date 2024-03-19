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
	choice    string
	err       error
	list      list.Model
	parentKey string
	showInput bool
	textInput tea.Model
}

var flags = map[string][]flag{
	"env1": {
		{
			Key:  "flag1",
			Name: "flag 1",
		},
		{
			Key:  "flag2",
			Name: "flag 2",
		},
	},
	"env2": {
		{
			Key:  "flag3",
			Name: "flag 3",
		},
		{
			Key:  "flag4",
			Name: "flag 4",
		},
	},
	"env3": {
		{
			Key:  "flag5",
			Name: "flag 5",
		},
		{
			Key:  "flag6",
			Name: "flag 6",
		},
	},
	"env4": {
		{
			Key:  "flag7",
			Name: "flag 7",
		},
		{
			Key:  "flag8",
			Name: "flag 8",
		},
	},
	"env5": {
		{
			Key:  "flag9",
			Name: "flag 9",
		},
		{
			Key:  "flag10",
			Name: "flag 10",
		},
	},
	"env6": {
		{
			Key:  "flag11",
			Name: "flag 11",
		},
		{
			Key:  "flag12",
			Name: "flag 12",
		},
	},
}

func NewFlag() tea.Model {
	l := list.New(nil, flagDelegate{}, 30, 14)
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
	if m.showInput {
		m.textInput, cmd = m.textInput.Update(msg)
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch {
			case key.Matches(msg, keys.Enter):
				iModel, ok := m.textInput.(inputModel)
				if ok {
					m.choice = iModel.textInput.Value()
					m.showInput = false
				}

				flags[m.parentKey] = append(flags[m.parentKey], flag{Key: m.choice, Name: m.choice})
			}
		default:

		}
		return m, cmd
	}
	switch msg := msg.(type) {
	case fetchResources:
		fs, err := getFlags(m.parentKey)
		if err != nil {
			m.err = err
			return m, nil
		}
		m.list.SetItems(flagsToItems(fs))
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Enter):
			i, ok := m.list.SelectedItem().(flag)
			if ok {
				if i.Key == CreateNewResourceKey {
					iModel := newTextInputModel("desired-flag-key", "Enter flag key")
					m.textInput = iModel
					m.showInput = true
				}
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
	if m.showInput {
		return m.textInput.View()
	}

	return "\n" + m.list.View()
}

func getFlags(envKey string) ([]flag, error) {
	flagList := flags[envKey]
	createNewOption := flag{Key: CreateNewResourceKey, Name: "Create a new flag"}
	flagList = append(flagList, createNewOption)
	return flagList, nil
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
