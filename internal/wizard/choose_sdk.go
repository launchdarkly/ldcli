package wizard

import (
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/launchdarkly/sdk-meta/api/sdkmeta"
	"golang.org/x/exp/slices"

	"github.com/launchdarkly/ldcli/internal/sdks"
)

var (
	sdkStyle             = lipgloss.NewStyle().PaddingLeft(4)
	selectedSdkItemStyle = lipgloss.NewStyle().PaddingLeft(2).Foreground(lipgloss.Color("170"))
	titleBarStyle        = lipgloss.NewStyle().MarginBottom(1)
)

type sdkDetail struct {
	id          string
	displayName string
	index       int
	sdkType     sdkmeta.Type
	url         string
}

func (s sdkDetail) FilterValue() string { return s.displayName }

type chooseSDKModel struct {
	help          help.Model
	helpKeys      keyMap
	list          list.Model
	selectedIndex int
	selectedSDK   sdkDetail
}

// NewChooseSDKModel creates the SDK selection model. detectedIDs contains SDK IDs
// found via stack detection and are placed at the top of the list.
func NewChooseSDKModel(detectedIDs []string) tea.Model {
	allSDKs := initSDKs()
	ordered := reorderSDKs(allSDKs, detectedIDs)
	l := list.New(sdksToItems(ordered), sdkDelegate{}, 30, 9)
	l.FilterInput.PromptStyle = lipgloss.NewStyle()
	l.Title = "Select your SDK:"
	l.Styles.Title = lipgloss.NewStyle()
	l.Styles.TitleBar = titleBarStyle
	l.SetShowHelp(false)
	l.SetShowPagination(true)
	l.SetShowStatusBar(false)

	return chooseSDKModel{
		help: help.New(),
		helpKeys: keyMap{
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
		selectedIndex: 0,
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
		case key.Matches(msg, pressableKeys.Enter):
			i, ok := m.list.SelectedItem().(sdkDetail)
			if ok {
				m.selectedSDK = i
				cmd = chooseSDKCmd(m.selectedSDK)
			}
		case key.Matches(msg, BindingFilter):
			if m.list.FilterState() == list.Filtering {
				m.list.SetFilteringEnabled(false)
			} else {
				m.list.SetFilteringEnabled(true)
			}
			m.list, cmd = m.list.Update(msg)
		case key.Matches(msg, m.helpKeys.CloseFullHelp):
			m.help.ShowAll = !m.help.ShowAll
		default:
			m.list, cmd = m.list.Update(msg)
		}
	default:
		m.list, cmd = m.list.Update(msg)
	}

	return m, cmd
}

func (m chooseSDKModel) View() string {
	return m.list.View() + footerView(m.help.View(m.helpKeys), nil)
}

// sdkExamples maps SDK IDs to example repo URLs that differ from the default
// https://github.com/launchdarkly/hello-<id> pattern.
var sdkExamples = map[string]string{
	"react-client-sdk": "https://github.com/launchdarkly/react-client-sdk/tree/main/examples/typescript",
}

// initSDKs loads all available SDKs from the embedded instruction files,
// sorted by popularity.
func initSDKs() []sdkDetail {
	items, err := sdks.InstructionFiles.ReadDir("sdk_instructions")
	if err != nil {
		panic("failed to load embedded SDK quickstart instructions: " + err.Error())
	}

	slices.SortFunc(items, func(a fs.DirEntry, b fs.DirEntry) int {
		return strings.Compare(a.Name(), b.Name())
	})

	details := make([]sdkDetail, 0, len(items))
	for _, item := range items {
		id, _, _ := strings.Cut(filepath.Base(item.Name()), ".")
		if _, ok := sdkmeta.Names[id]; !ok {
			continue
		}
		popularity, ok := sdkmeta.Popularity[id]
		if !ok {
			continue
		}
		details = append(details, sdkDetail{
			id:          id,
			index:       popularity - 1,
			displayName: sdkmeta.Names[id],
			sdkType:     sdkmeta.Types[id],
			url:         sdkExamples[id],
		})
	}

	// Sort by popularity so we can reorder deterministically.
	slices.SortFunc(details, func(a, b sdkDetail) int {
		return a.index - b.index
	})

	return details
}

// reorderSDKs moves detectedIDs to the front of the list (in detection order),
// followed by the remaining SDKs in their original popularity order.
// All indices are reassigned sequentially so sdksToItems places items correctly.
func reorderSDKs(all []sdkDetail, detectedIDs []string) []sdkDetail {
	detectedSet := make(map[string]bool, len(detectedIDs))
	for _, id := range detectedIDs {
		detectedSet[id] = true
	}

	detected := make([]sdkDetail, 0, len(detectedIDs))
	for _, id := range detectedIDs {
		for _, s := range all {
			if s.id == id {
				detected = append(detected, s)
				break
			}
		}
	}

	rest := make([]sdkDetail, 0, len(all)-len(detected))
	for _, s := range all {
		if !detectedSet[s.id] {
			rest = append(rest, s)
		}
	}

	result := append(detected, rest...)
	for i := range result {
		result[i].index = i
	}
	return result
}

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

	_, _ = fmt.Fprint(w, fn(fmt.Sprintf("%d. %s", index+1, i.displayName)))
}
