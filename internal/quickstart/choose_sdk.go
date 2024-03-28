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
	err         error
	list        list.Model
	selectedSdk sdkDetail
}

func NewChooseSDKModel() tea.Model {
	l := list.New(sdksToItems(), sdkDelegate{}, 30, 14)
	l.Title = "Select your SDK:\n"
	// reset title styles
	l.Styles.Title = lipgloss.NewStyle()
	l.Styles.TitleBar = lipgloss.NewStyle()
	l.SetShowPagination(true)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true) // TODO: try to get filtering working
	l.Paginator.PerPage = 5

	return chooseSDKModel{
		list: l,
	}
}

func (m chooseSDKModel) Init() tea.Cmd { return nil }

func (m chooseSDKModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Enter):
			i, ok := m.list.SelectedItem().(sdkDetail)
			if ok {
				m.selectedSdk = i
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
	DisplayName string `json:"displayName"`
	SDKType     string `json:"sdkType"`
}

func (s sdkDetail) FilterValue() string { return "" }

const clientSideSDK = "client"
const serverSideSDK = "server"

var SDKs = []sdkDetail{
	{DisplayName: "React", SDKType: clientSideSDK},
	{DisplayName: "Node.js (server-side)", SDKType: serverSideSDK},
	{DisplayName: "Python", SDKType: serverSideSDK},
	{DisplayName: "Java", SDKType: serverSideSDK},
	{DisplayName: ".NET (server-side)", SDKType: serverSideSDK},
	{DisplayName: "JavaScript", SDKType: clientSideSDK},
	{DisplayName: "Vue", SDKType: clientSideSDK},
	{DisplayName: "iOS", SDKType: clientSideSDK},
	{DisplayName: "Go", SDKType: serverSideSDK},
	{DisplayName: "Android", SDKType: clientSideSDK},
	{DisplayName: "React Native", SDKType: clientSideSDK},
	{DisplayName: "Ruby", SDKType: serverSideSDK},
	{DisplayName: "Flutter", SDKType: clientSideSDK},
	{DisplayName: ".NET (client-side)", SDKType: clientSideSDK},
	{DisplayName: "Erlang", SDKType: serverSideSDK},
	{DisplayName: "Rust", SDKType: serverSideSDK},
	{DisplayName: "Electron", SDKType: clientSideSDK},
	{DisplayName: "C/C++ (client-side)", SDKType: clientSideSDK},
	{DisplayName: "Roku", SDKType: clientSideSDK},
	{DisplayName: "Node.js (client-side)", SDKType: clientSideSDK},
	{DisplayName: "C/C++ (server-side)", SDKType: serverSideSDK},
	{DisplayName: "Lua", SDKType: serverSideSDK},
	{DisplayName: "Haskell", SDKType: serverSideSDK},
	{DisplayName: "Apex", SDKType: serverSideSDK},
	{DisplayName: "PHP", SDKType: serverSideSDK},
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
