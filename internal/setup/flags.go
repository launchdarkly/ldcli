package setup

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	flagStyle             = lipgloss.NewStyle().PaddingLeft(4)
	selectedFlagItemStyle = lipgloss.NewStyle().PaddingLeft(2).Foreground(lipgloss.Color("170"))

	_ list.Item = flag{}
)

const defaultFlagKey = "setup-test-flag"

type flag struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

func (p flag) FilterValue() string { return "" }

type flagModel struct {
	input     string
	textInput textinput.Model
}

func NewFlag() tea.Model {
	ti := textinput.New()
	ti.Focus()
	ti.CharLimit = 156
	ti.Width = 20
	ti.Placeholder = defaultFlagKey

	return flagModel{
		textInput: ti,
	}
}

func (p flagModel) Init() tea.Cmd {
	return nil
}

func (m flagModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Enter):
			input := m.textInput.Value()
			if input == "" {
				input = defaultFlagKey
			}
			m.input = input
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit
		default:
			m.textInput, cmd = m.textInput.Update(msg)
		}
	}

	return m, cmd
}

func (m flagModel) View() string {
	style := lipgloss.NewStyle().
		MarginLeft(2)

	return fmt.Sprintf(
		"Name your first feature flag (enter for default value):\n\n%s",
		style.Render(m.textInput.View()),
	) + "\n"
}
