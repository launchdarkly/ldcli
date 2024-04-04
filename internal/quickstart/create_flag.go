package quickstart

import (
	"fmt"
	"ldcli/internal/flags"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const defaultFlagName = "My New Flag"

type createFlagModel struct {
	accessToken      string
	baseUri          string
	client           flags.Client
	existingFlagUsed bool
	flagKey          string
	success          bool
	textInput        textinput.Model
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
		textInput:   ti,
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

			return m, sendCreateFlagMsg(m.client, m.accessToken, m.baseUri, input, flagKey, defaultProjKey)
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit
		default:
			m.textInput, cmd = m.textInput.Update(msg)
		}
	case createdFlagMsg:
		if !m.success {
			m.success = true
			m.existingFlagUsed = msg.existingFlagUsed
			m.flagKey = msg.flagKey
			return m, cmd
		}
		return m, sendConfirmedFlagMsg(msg.flagKey)
	}

	return m, cmd
}

func (m createFlagModel) View() string {
	style := lipgloss.NewStyle().
		MarginLeft(2)

	if m.success {
		successMessage := fmt.Sprintf("Flag %q created successfully!", m.flagKey)
		if m.existingFlagUsed {
			successMessage = fmt.Sprintf("Using existing existing flag %q for setup.", m.flagKey)
		}
		return successMessage + " Press enter to continue."
	}

	return fmt.Sprintf(
		"Name your first feature flag (enter for default value %q):\n\n%s",
		defaultFlagName,
		style.Render(m.textInput.View()),
	) + "\n"
}
