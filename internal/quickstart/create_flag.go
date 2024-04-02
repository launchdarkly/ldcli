package quickstart

import (
	"fmt"
	"ldcli/cmd/cliflags"

	"github.com/spf13/viper"

	"ldcli/internal/flags"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const defaultFlagName = "my new flag"

type createFlagModel struct {
	client    flags.Client
	textInput textinput.Model
}

func NewCreateFlagModel(client flags.Client) tea.Model {
	ti := textinput.New()
	ti.Focus()
	ti.CharLimit = 156
	ti.Width = 20
	ti.Placeholder = defaultFlagName

	return createFlagModel{
		client:    client,
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
		case key.Matches(msg, keys.Enter):
			input := m.textInput.Value()
			if input == "" {
				input = defaultFlagName
			}
			flagKey, err := flags.NewKeyFromName(input)
			if err != nil {
				return m, sendErr(err)
			}

			accessToken := viper.GetString(cliflags.AccessTokenFlag)
			baseUri := viper.GetString(cliflags.BaseURIFlag)

			return m, sendCreateFlagMsg(m.client, accessToken, baseUri, input, flagKey, "default")
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit
		default:
			m.textInput, cmd = m.textInput.Update(msg)
		}
	}

	return m, cmd
}

func (m createFlagModel) View() string {
	style := lipgloss.NewStyle().
		MarginLeft(2)

	return fmt.Sprintf(
		"Name your first feature flag (enter for default value):\n\n%s",
		style.Render(m.textInput.View()),
	) + "\n"
}
