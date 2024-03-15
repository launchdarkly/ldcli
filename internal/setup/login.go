package setup

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type loginMethod struct {
	Label string `json:"name"`
	Key   string `json:"key"`
}

func (p loginMethod) FilterValue() string { return "" }

type loginModel struct {
	choice     string
	loggedIn   bool
	showInput  bool
	err        error
	list       list.Model
	tokenInput textInputModel
}

func NewLogin() loginModel {
	loginMethods := []loginMethod{
		{
			Label: "Create a new account",
			Key:   "new-account",
		},
		{
			Label: "OAuth",
			Key:   "oauth",
		},
		{
			Label: "Personal access token",
			Key:   "access-token",
		},
		{
			Label: "Service token",
			Key:   "service-token",
		},
	}
	l := list.New(loginMethodsToItems(loginMethods), listDelegate{}, 30, 14)
	l.Title = "Log Into LaunchDarkly"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.SetShowFilter(false)

	ti := textinput.New()
	ti.Placeholder = "Pikachu"
	ti.Focus()
	ti.CharLimit = 156
	ti.Width = 20
	ti.EchoMode = textinput.EchoPassword

	return loginModel{
		list: l,
		tokenInput: textInputModel{
			title:     "Enter your token",
			textInput: ti,
			err:       nil,
		},
	}
}

func (m loginModel) Init() tea.Cmd {
	return nil
}

func (m loginModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	if m.showInput {
		var tm tea.Model
		tm, cmd = m.tokenInput.Update(msg)
		m.tokenInput = tm.(textInputModel)
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Enter):
			i, ok := m.list.SelectedItem().(loginMethod)
			if ok {
				m.choice = i.Key
				switch m.choice {
				case "new-account":
					openbrowser("https://app.launchdarkly.com/signup")
					m.loggedIn = true
				case "oauth":
					openbrowser("https://app.launchdarkly.com/oauth/authorize?client_id=launchdarkly-cli&response_type=token&redirect_uri=https://app.launchdarkly.com/cli/oauth/callback")
					m.loggedIn = true
				case "access-token":
					m.showInput = true
				case "service-token":
					m.showInput = true
				}
			}
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit
		default:
			m.list, cmd = m.list.Update(msg)
		}
	}

	return m, cmd
}

func (m loginModel) View() string {
	if m.showInput {
		return "\n" + m.tokenInput.View()
	}
	return "\n" + m.list.View()
}

type listDelegate struct{}

func (d listDelegate) Height() int                             { return 1 }
func (d listDelegate) Spacing() int                            { return 0 }
func (d listDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d listDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(loginMethod)
	if !ok {
		return
	}

	str := i.Label

	fn := choiceStyle.Render
	if index == m.Index() {
		fn = func(s ...string) string {
			return selectedChoiceItemStyle.Render("> " + strings.Join(s, " "))
		}
	}

	fmt.Fprint(w, fn(str))
}

func loginMethodsToItems(loginMethods []loginMethod) []list.Item {
	items := make([]list.Item, len(loginMethods))
	for i, m := range loginMethods {
		items[i] = list.Item(m)
	}

	return items
}
