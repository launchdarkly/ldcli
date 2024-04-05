package quickstart

import (
	"fmt"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"ldcli/internal/flags"
)

const defaultFlagName = "My New Flag"

type createFlagModel struct {
	accessToken string
	baseUri     string
	client      flags.Client
	help        help.Model
	helpKeys    keyMap
	textInput   textinput.Model
}

func NewCreateFlagModel(client flags.Client, accessToken, baseUri string) tea.Model {
	ti := textinput.New()
	ti.Focus()
	ti.CharLimit = 156
	ti.Width = 20

	return createFlagModel{
		accessToken: accessToken,
		baseUri:     baseUri,
		client:      client,
		help:        help.New(),
		helpKeys: keyMap{
			Back: key.NewBinding(
				key.WithKeys("esc"),
				key.WithHelp("esc", "back"),
			),
			Quit: key.NewBinding(
				key.WithKeys("ctrl+c"),
				key.WithHelp("ctrl+c", "quit"),
			),
		},
		textInput: ti,
	}
}

func (m createFlagModel) Init() tea.Cmd {
	return nil
}

func (m createFlagModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, pressableKeys.Enter):
			input := m.textInput.Value()
			if input == "" {
				input = defaultFlagName
			}
			flagKey, err := flags.NewKeyFromName(input)
			if err != nil {
				return m, sendErr(err)
			}

			return m, sendCreateFlagMsg(m.client, m.accessToken, m.baseUri, input, flagKey, defaultProjKey)
		default:
			m.textInput, cmd = m.textInput.Update(msg)
		}
	}

	return m, cmd
}

func (m createFlagModel) View() string {
	style := lipgloss.NewStyle().
		MarginLeft(2)
	helpView := m.help.View(m.helpKeys)

	return fmt.Sprintf(
		"Name your first feature flag (enter for default value %q):\n\n%s",
		defaultFlagName,
		style.Render(m.textInput.View()),
	) + "\n\n" + helpView
}
