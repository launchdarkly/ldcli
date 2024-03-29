package quickstart

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
)

type chooseSDKModel struct {
	choice sdk
	list   list.Model
}

func NewChooseSDKModel() tea.Model {
	sdks := []sdk{
		{
			canonicalName: "js",
			name:          "JavaScript",
		},
		{
			canonicalName: "node-server",
			name:          "Node.js (server)",
		},
		{
			canonicalName: "python",
			name:          "Python",
		},
		{
			canonicalName: "java",
			name:          "Java",
		},
		{
			canonicalName: "android",
			name:          "Android",
		},
		{
			canonicalName: "react-native",
			name:          "React Native",
		},
		{
			canonicalName: "ruby",
			name:          "Ruby",
		},
		{
			canonicalName: "flutter",
			name:          "Flutter",
		},
	}

	l := list.New(sdksToItems(sdks), sdkDelegate{}, 30, 14)
	// extra newlines to show pagination
	l.Title = "Select your SDK:\n\n"
	// reset title styles
	l.Styles.Title = lipgloss.NewStyle()
	l.Styles.TitleBar = lipgloss.NewStyle()
	l.SetShowPagination(true)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.Paginator.PerPage = 5

	return chooseSDKModel{
		list: l,
	}
}

func (m chooseSDKModel) Init() tea.Cmd {
	return nil
}

func (m chooseSDKModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

func (m chooseSDKModel) View() string {
	return m.list.View()
}

type sdk struct {
	canonicalName string
	name          string
}

func (s sdk) FilterValue() string { return "" }

type sdkDelegate struct{}

func (d sdkDelegate) Height() int                             { return 1 }
func (d sdkDelegate) Spacing() int                            { return 0 }
func (d sdkDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d sdkDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(sdk)
	if !ok {
		return
	}

	str := fmt.Sprintf("%d. %s", index+1, i.name)

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
	for i, sdk := range sdks {
		items[i] = list.Item(sdk)
	}

	return items
}
