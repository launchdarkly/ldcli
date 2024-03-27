package quickstart

import (
	"context"
	"fmt"
	"ldcli/internal/flags"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/viper"
)

const defaultFlagName = "my new flag"

type createFlagModel struct {
	err       error
	flagKey   string
	flagName  string
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

func (p createFlagModel) Init() tea.Cmd {
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
			flagKey, err := flags.NameToKey(m.flagName)
			if err != nil {
				m.err = err

				return m, nil
			}

			_, err = m.client.Create(
				context.Background(),
				viper.GetString("accessToken"),
				viper.GetString("baseUri"),
				m.flagName,
				flagKey,
				"default",
			)
			if err != nil {
				m.err = err

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
