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

const (
	clientSideSDK = "client"
	serverSideSDK = "server"
)

type chooseSDKModel struct {
	list        list.Model
	selectedSDK sdkDetail
}

func NewChooseSDKModel() tea.Model {
	l := list.New(sdksToItems(), sdkDelegate{}, 30, 14)
	l.Title = "Select your SDK:\n\n" // extra newlines to show pagination
	// reset title styles
	l.Styles.Title = lipgloss.NewStyle()
	l.Styles.TitleBar = lipgloss.NewStyle()
	l.SetShowPagination(true)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false) // TODO: try to get filtering working
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
			i, ok := m.list.SelectedItem().(sdkDetail)
			if ok {
				m.selectedSDK = i
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

type sdkDetail struct {
	CanonicalName string
	DisplayName   string
	Type          string
}

func (s sdkDetail) FilterValue() string { return "" }

var SDKs = []sdkDetail{
	// TODO: react is still internal
	// {CanonicalName: "react", DisplayName: "React", SDKType: clientSideSDK},
	{CanonicalName: "node-server", DisplayName: "Node.js (server-side)", Type: serverSideSDK},
	{CanonicalName: "python", DisplayName: "Python", Type: serverSideSDK},
	{CanonicalName: "java", DisplayName: "Java", Type: serverSideSDK},
	{CanonicalName: "dotnet-server", DisplayName: ".NET (server-side)", Type: serverSideSDK},
	{CanonicalName: "js", DisplayName: "JavaScript", Type: clientSideSDK},
	{CanonicalName: "ios-swift", DisplayName: "iOS", Type: clientSideSDK},
	{CanonicalName: "go", DisplayName: "Go", Type: serverSideSDK},
	{CanonicalName: "android", DisplayName: "Android", Type: clientSideSDK},
	{CanonicalName: "react-native", DisplayName: "React Native", Type: clientSideSDK},
	{CanonicalName: "ruby", DisplayName: "Ruby", Type: serverSideSDK},
	{CanonicalName: "flutter", DisplayName: "Flutter", Type: clientSideSDK},
	{CanonicalName: "dotnet-client", DisplayName: ".NET (client-side)", Type: clientSideSDK},
	{CanonicalName: "erlang", DisplayName: "Erlang", Type: serverSideSDK},
	{CanonicalName: "rust", DisplayName: "Rust", Type: serverSideSDK},
	{CanonicalName: "electron", DisplayName: "Electron", Type: clientSideSDK},
	{CanonicalName: "c-client", DisplayName: "C/C++ (client-side)", Type: clientSideSDK},
	{CanonicalName: "roku", DisplayName: "Roku", Type: clientSideSDK},
	{CanonicalName: "node-client", DisplayName: "Node.js (client-side)", Type: clientSideSDK},
	{CanonicalName: "c-server", DisplayName: "C/C++ (server-side)", Type: serverSideSDK},
	{CanonicalName: "lua-server", DisplayName: "Lua", Type: serverSideSDK},
	{CanonicalName: "haskell-server", DisplayName: "Haskell", Type: serverSideSDK},
	{CanonicalName: "apex-server", DisplayName: "Apex", Type: serverSideSDK},
	{CanonicalName: "php", DisplayName: "PHP", Type: serverSideSDK},
}

func sdksToItems() []list.Item {
	items := make([]list.Item, len(SDKs))
	for i, sdk := range SDKs {
		items[i] = list.Item(sdk)
	}

	return items
}

type sdkDelegate struct{}

func (d sdkDelegate) Height() int                             { return 1 }
func (d sdkDelegate) Spacing() int                            { return 0 }
func (d sdkDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d sdkDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(sdkDetail)
	if !ok {
		return
	}

	str := fmt.Sprintf("%d. %s", index+1, i.DisplayName)

	fn := sdkStyle.Render
	if index == m.Index() {
		fn = func(s ...string) string {
			return selectedSdkItemStyle.Render("> " + strings.Join(s, " "))
		}
	}

	fmt.Fprint(w, fn(str))
}
