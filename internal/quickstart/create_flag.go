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

type flag struct {
	key  string
	name string
}

type createFlagModel struct {
	accessToken      string
	baseUri          string
	client           flags.Client
	err              error
	existingFlagUsed bool
	flag             flag
	help             help.Model
	helpKeys         keyMap
	showSuccessView  bool
	textInput        textinput.Model
}

func NewCreateFlagModel(client flags.Client, accessToken, baseUri string) tea.Model {
	ti := textinput.New()
	ti.Focus()
	ti.CharLimit = 156
	ti.Width = 20
	ti.Prompt = ""

	return createFlagModel{
		accessToken: accessToken,
		baseUri:     baseUri,
		client:      client,
		help:        help.New(),
		helpKeys: keyMap{
			Quit: BindingQuit,
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
			if m.showSuccessView {
				return m, sendConfirmedFlagMsg(m.flag)
			}

			input := m.textInput.Value()
			if input == "" {
				input = defaultFlagName
			}
			flagKey, err := flags.NewKeyFromName(input)
			if err != nil {
				return m, sendErr(err)
			}

			return m, sendCreateFlagMsg(m.client, m.accessToken, m.baseUri, input, flagKey, defaultProjKey)
		case key.Matches(msg, pressableKeys.Back):
			if m.showSuccessView {
				m.showSuccessView = false
			}
		default:
			m.textInput, cmd = m.textInput.Update(msg)
		}
	case createdFlagMsg:
		m.showSuccessView = true
		m.existingFlagUsed = msg.existingFlagUsed
		m.flag = msg.flag
	case errMsg:
		m.err = msg.err
	}

	return m, cmd
}

func (m createFlagModel) View() string {
	style := lipgloss.NewStyle().
		MarginLeft(2)

	if m.showSuccessView {
		successMessage := fmt.Sprintf("Flag %q created successfully!", m.flag.name)
		if m.existingFlagUsed {
			successMessage = fmt.Sprintf("Using existing flag %q for setup.", m.flag.name)
		}
		return successMessage + " Press enter to continue."
	}

	return fmt.Sprintf(
		"Name your first feature flag (enter for default value):%s",
		style.Render(m.textInput.View()),
	) + footerView(m.help.View(m.helpKeys), m.err)
}
