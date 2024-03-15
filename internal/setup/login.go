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
	l := list.New(loginMethodsToItems(loginMethods), loginDelegate{}, 30, 14)
	l.Title = "Log Into LaunchDarkly"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.SetShowFilter(false)

	return loginModel{
		list: l,
	}
}

func (m loginModel) Init() tea.Cmd {
	return nil
}

func (m loginModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case showInput:
		ti := textinput.New()
		ti.Placeholder = "Pikachu"
		ti.Focus()
		ti.CharLimit = 156
		ti.Width = 20

		m.showInput = true
		m.tokenInput = textInputModel{
			title:     "Enter your " + msg.tokenType + " token",
			textInput: ti,
			err:       nil,
		}
	case successfulLogin:
		m.loggedIn = true
		success := []loginMethod{
			{
				Label: "Logged in!",
				Key:   "logged-in",
			},
		}
		m.list.SetItems(loginMethodsToItems(success))
		m.list.FilterInput.SetCursor(0)

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Enter):
			i, ok := m.list.SelectedItem().(loginMethod)
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

func (m loginModel) View() string {
	if m.showInput {
		return "\n" + m.tokenInput.View()
	}
	return "\n" + m.list.View()
}

type loginDelegate struct{}

func (d loginDelegate) Height() int                             { return 1 }
func (d loginDelegate) Spacing() int                            { return 0 }
func (d loginDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d loginDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
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

type successfulLogin struct{}

type showInput struct {
	tokenType string
}
