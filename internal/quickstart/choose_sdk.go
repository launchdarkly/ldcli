package quickstart

import (
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/launchdarkly/ldcli/internal/sdks"
	"github.com/launchdarkly/sdk-meta/api/sdkmeta"
	"golang.org/x/exp/slices"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	sdkStyle             = lipgloss.NewStyle().PaddingLeft(4)
	selectedSdkItemStyle = lipgloss.NewStyle().PaddingLeft(2).Foreground(lipgloss.Color("170"))
	titleBarStyle        = lipgloss.NewStyle().MarginBottom(1)
)

type chooseSDKModel struct {
	help          help.Model
	helpKeys      keyMap
	list          list.Model
	sdkDetails    []sdkDetail
	selectedIndex int
	selectedSDK   sdkDetail
}

func NewChooseSDKModel(selectedIndex int) tea.Model {
	sdkDetails := initSDKs()
	l := list.New(sdksToItems(sdkDetails), sdkDelegate{}, 30, 9)
	l.FilterInput.PromptStyle = lipgloss.NewStyle()

	l.Title = "Select your SDK:"
	// reset title styles
	l.Styles.Title = lipgloss.NewStyle()
	l.Styles.TitleBar = titleBarStyle
	l.SetShowHelp(false)
	l.SetShowPagination(true)
	l.SetShowStatusBar(false)

	return chooseSDKModel{
		help: help.New(),
		helpKeys: keyMap{
			Back:          BindingBack,
			Filter:        BindingFilter,
			CursorUp:      BindingCursorUp,
			CursorDown:    BindingCursorDown,
			PrevPage:      BindingPrevPage,
			NextPage:      BindingNextPage,
			GoToStart:     BindingGoToStart,
			GoToEnd:       BindingGoToEnd,
			ShowFullHelp:  BindingShowFullHelp,
			CloseFullHelp: BindingCloseFullHelp,
			Quit:          BindingQuit,
		},
		list:          l,
		sdkDetails:    sdkDetails,
		selectedIndex: selectedIndex,
	}
}

// The CLI uses the sdkmeta project to obtain metadata about each SDK, including the display names
// and types (client, server, etc.)
// Currently, there is no sdkmeta for code examples associated with each SDK, so we hard-code the examples here.
// Once they are part of sdkmeta we can remove this list.
var sdkExamples = map[string]string{
	"react-client-sdk":  "https://github.com/launchdarkly/react-client-sdk/tree/main/examples/typescript",
	"vue":               "https://github.com/launchdarkly/vue-client-sdk/tree/main/example",
	"react-native":      "https://github.com/launchdarkly/js-core/tree/main/packages/sdk/react-native/example",
	"cpp-client-sdk":    "https://github.com/launchdarkly/cpp-sdks/tree/main/examples/hello-cpp-client",
	"cpp-server-sdk":    "https://github.com/launchdarkly/cpp-sdks/tree/main/examples/hello-cpp-server",
	"lua-server-server": "https://github.com/launchdarkly/lua-server-sdk/tree/main/examples/hello-lua-server",
}

// initSDKs is responsible for loading SDK quickstart instructions from the embedded filesystem.
//
// The names of the files are special: they are the ID of the SDK (e.g. react-native), and are used as an index or
// key to lookup associated sdk metadata (display name, SDK type, etc.)
//
// Therefore, take care when naming the files. A list of valid SDK IDs can be found here:
// https://github.com/launchdarkly/sdk-meta/blob/main/products/names.json
func initSDKs() []sdkDetail {
	items, err := sdks.InstructionFiles.ReadDir("sdk_instructions")
	if err != nil {
		panic("failed to load embedded SDK quickstart instructions: " + err.Error())
	}

	slices.SortFunc(items, func(a fs.DirEntry, b fs.DirEntry) int {
		return strings.Compare(a.Name(), b.Name())
	})

	index := 0
	details := make([]sdkDetail, 0, len(items))
	for _, item := range items {
		id, _, _ := strings.Cut(filepath.Base(item.Name()), ".")
		if _, ok := sdkmeta.Names[id]; !ok {
			continue
		}
		details = append(details, sdkDetail{
			id:          id,
			index:       index,
			displayName: sdkmeta.Names[id],
			sdkType:     sdkmeta.Types[id],
			url:         sdkExamples[id],
		})
		index += 1
	}

	return details
}

// Init sends commands when the model is created that will:
// * select an SDK if it's already been selected
func (m chooseSDKModel) Init() tea.Cmd {
	return selectedSDK(m.selectedIndex)
}

func (m chooseSDKModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case msg.String() == "/":
			if m.list.FilterState() == list.Filtering {
				m.list.SetFilteringEnabled(false)
			} else {
				m.list.SetFilteringEnabled(true)
			}
			m.list, cmd = m.list.Update(msg)
		case key.Matches(msg, pressableKeys.Enter):
			i, ok := m.list.SelectedItem().(sdkDetail)
			if ok {
				m.selectedSDK = i
				cmd = chooseSDK(m.selectedSDK)
			}
		case key.Matches(msg, m.helpKeys.CloseFullHelp):
			m.help.ShowAll = !m.help.ShowAll
		default:
			m.list, cmd = m.list.Update(msg)
		}
	case selectedSDKMsg:
		m.list.Select(msg.index)
	default:
		// update list from list.FilterMatchesMsg
		m.list, cmd = m.list.Update(msg)
	}

	return m, cmd
}

func (m chooseSDKModel) View() string {
	return m.list.View() + footerView(m.help.View(m.helpKeys), nil)
}

type sdkDetail struct {
	id          string
	displayName string
	index       int
	sdkType     sdkmeta.Type
	url         string // custom URL if it differs from the other SDKs
}

func (s sdkDetail) FilterValue() string { return s.displayName }

func sdksToItems(sdkDetails []sdkDetail) []list.Item {
	items := make([]list.Item, len(sdkDetails))
	for _, info := range sdkDetails {
		items[info.index] = list.Item(info)
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

	fn := sdkStyle.Render
	if index == m.Index() {
		fn = func(s ...string) string {
			return selectedSdkItemStyle.Render("> " + strings.Join(s, " "))
		}
	}

	fmt.Fprint(w, fn(fmt.Sprintf("%d. %s", index+1, i.displayName)))
}
