package quickstart

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/viper"

	"ldcli/cmd/cliflags"
	"ldcli/internal/flags"
)

const defaultFlagName = "my new flag"

type createFlagModel struct {
	client    flags.Client
	err       error
	flagKey   string
	flagName  string
	quitMsg   string
	quitting  bool
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
			m.flagName = input
			flagKey, err := flags.NewKeyFromName(m.flagName)
			if err != nil {
				m.err = err

				return m, nil
			}

			_, err = m.client.Create(
				context.Background(),
				viper.GetString(cliflags.APITokenFlag),
				viper.GetString(cliflags.BaseURIFlag),
				m.flagName,
				flagKey,
				"default",
			)
			if err != nil {
				m.err = err
				// TODO: we may want a more robust error type so we don't need to do this
				var e struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				}
				_ = json.Unmarshal([]byte(m.err.Error()), &e)
				switch {
				case e.Code == "unauthorized":
					m.quitting = true
					m.quitMsg = "Your API key is unauthorized. Try another API key or speak to a LaunchDarkly account administrator."

					return m, tea.Quit
				case e.Code == "forbidden":
					m.quitting = true
					m.quitMsg = "You lack access to complete this action. Try authenticating with elevated access or speak to a LaunchDarkly account administrator."

					return m, tea.Quit
				}

				return m, nil
			}
			m.flagKey = flagKey

			return m, nil
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
