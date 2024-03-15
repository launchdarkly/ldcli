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
	sdkStyle             = lipgloss.NewStyle().PaddingLeft(4)
	selectedSdkItemStyle = lipgloss.NewStyle().PaddingLeft(2).Foreground(lipgloss.Color("170"))

	_ list.Item = sdk{}
)

type sdk struct {
	Name                 string `json:"name"`
	InstructionsFileName string `json:"instructionFile"`
}

func (s sdk) FilterValue() string { return "" }

type sdkModel struct {
	choice       sdk
	instructions string
	err          error
	list         list.Model
}

const sdkInstructionsFilePath = "internal/setup/sdk_build_instructions/"

func NewSdk() tea.Model {
	sdks := []sdk{
		{
			Name:                 "JavaScript",
			InstructionsFileName: sdkInstructionsFilePath + "js.md",
		},
		{
			Name:                 "Python",
			InstructionsFileName: sdkInstructionsFilePath + "python.md",
		},
	}

	l := list.New(sdksToItems(sdks), sdkDelegate{}, 30, 14)
	l.Title = "Select your SDK."
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)

	return sdkModel{
		list: l,
	}
}

func (p sdkModel) Init() tea.Cmd {
	return nil
}

// This method has drifted from the ProjectModel's version, but it should do something similar.
func (m sdkModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Enter):
			i, ok := m.list.SelectedItem().(sdk)
			if ok {
				m.choice = i
			}
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit
		default:
			m.list, cmd = m.list.Update(msg)
		}
	}

	return m, cmd
}

func (m sdkModel) View() string {
	return "\n" + m.list.View()
}

type sdkDelegate struct{}

func (d sdkDelegate) Height() int                             { return 1 }
func (d sdkDelegate) Spacing() int                            { return 0 }
func (d sdkDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d sdkDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(sdk)
	if !ok {
		return
	}

	str := fmt.Sprintf("%d. %s", index+1, i.Name)

	fn := sdkStyle.Render
	if index == m.Index() {
		fn = func(s ...string) string {
			return selectedSdkItemStyle.Render("> " + strings.Join(s, " "))
		}
	}

	fmt.Fprint(w, fn(str))
}

func sdksToItems(sdks []sdk) []list.Item {
	items := make([]list.Item, len(sdks))
	for i, proj := range sdks {
		items[i] = list.Item(proj)
	}

	return items
}
