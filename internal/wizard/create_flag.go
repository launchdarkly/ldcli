package wizard

import (
	"fmt"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/launchdarkly/ldcli/internal/flags"
)

type createFlagModel struct {
	accessToken     string
	baseURI         string
	client          flags.Client
	err             error
	existingFlagUsed bool
	flag            flag
	help            help.Model
	helpKeys        keyMap
	showSuccessView bool
	textInput       textinput.Model
}

func NewCreateFlagModel(client flags.Client, accessToken, baseURI string) tea.Model {
	ti := textinput.New()
	ti.Focus()
	ti.CharLimit = 156
	ti.Width = 20
	ti.Prompt = ""

	return createFlagModel{
		accessToken: accessToken,
		baseURI:     baseURI,
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
				return m, confirmedFlagCmd(m.flag)
			}

			input := m.textInput.Value()
			if input == "" {
				input = defaultFlagName
			}
			flagKey, err := flags.NewKeyFromName(input)
			if err != nil {
				return m, sendErrMsg(err)
			}
			return m, createFlag(m.client, m.accessToken, m.baseURI, input, flagKey)
		default:
			m.textInput, cmd = m.textInput.Update(msg)
		}
	case createdFlagMsg:
		m.showSuccessView = true
		m.flag = msg.flag
		m.existingFlagUsed = msg.existingFlag
	case errMsg:
		m.err = msg.err
	}

	return m, cmd
}

func (m createFlagModel) View() string {
	style := lipgloss.NewStyle().MarginLeft(1)

	if m.showSuccessView {
		successMessage := fmt.Sprintf("Flag %q created successfully!", m.flag.name)
		if m.existingFlagUsed {
			successMessage = fmt.Sprintf("Using existing flag %q.", m.flag.name)
		}
		return successMessage + " Press enter to continue."
	}

	return fmt.Sprintf(
		"Name your first feature flag (enter for default value):%s",
		style.Render(m.textInput.View()),
	) + footerView(m.help.View(m.helpKeys), m.err)
}
