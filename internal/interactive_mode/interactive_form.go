package interactive_mode

import (
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	blurredStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	defaultStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	focusedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("fff"))
	inputStyle    = lipgloss.NewStyle()
	blurredButton = fmt.Sprintf("[%s]", blurredStyle.Render("Submit"))
	focusedButton = focusedStyle.Copy().Render("[Submit]")
)

func callCmdWithData(resourceName, command, dataStr string) tea.Cmd {
	c := exec.Command("ldcli", resourceName, command, "--data", dataStr) //nolint:gosec
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return cmdFinishedMsg{err}
	})
}

type cmdFinishedMsg struct {
	err error
}

type inputModel struct {
	prompt   string
	required bool
	i        textinput.Model
}

type model struct {
	resourceName string
	command      string
	completed    bool
	focusIndex   int
	inputs       []inputModel
	formData     map[string]any
}

func NewInteractiveInputModel(resourceName, command string) model {
	m := model{
		resourceName: resourceName,
		command:      command,
		formData:     make(map[string]any),
		inputs: []inputModel{
			{
				i:        textinput.New(),
				prompt:   "name",
				required: true,
			},
			{
				i:        textinput.New(),
				prompt:   "key",
				required: true,
			},
			{
				i:      textinput.New(),
				prompt: "description",
			},
		},
	}

	for i := range m.inputs {
		m.inputs[i].i.PromptStyle = defaultStyle
		m.inputs[i].i.TextStyle = defaultStyle
		m.inputs[i].i.Prompt = m.inputs[i].prompt
		if m.inputs[i].required {
			m.inputs[i].i.Prompt += " (required): "
		} else {
			m.inputs[i].i.Prompt += ": "
		}
	}
	m.inputs[0].i.Focus()
	m.inputs[0].i.PromptStyle = focusedStyle
	m.inputs[0].i.TextStyle = focusedStyle

	return m
}

func (m model) Init() tea.Cmd {
	return textinput.Blink
}

type completedMsg bool

func completeForm() tea.Cmd {
	return func() tea.Msg {
		return completedMsg(true)
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case completedMsg:
		m.completed = true
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit
		case "enter":
			if m.completed {
				dataJson, _ := json.Marshal(m.formData)
				return m, callCmdWithData(m.resourceName, m.command, string(dataJson))
			}
			if m.focusIndex == len(m.inputs) {
				for _, input := range m.inputs {
					m.formData[input.prompt] = input.i.Value()
				}
				return m, completeForm()
			}
		// Set focus to next input
		case "tab", "shift+tab", "up", "down":
			s := msg.String()

			// Cycle indexes
			if s == "up" || s == "shift+tab" {
				m.focusIndex--
			} else {
				m.focusIndex++
			}

			if m.focusIndex > len(m.inputs) {
				m.focusIndex = 0
			} else if m.focusIndex < 0 {
				m.focusIndex = len(m.inputs)
			}

			cmds := make([]tea.Cmd, len(m.inputs))
			for i := 0; i <= len(m.inputs)-1; i++ {
				if i == m.focusIndex {
					// Set focused state
					cmds[i] = m.inputs[i].i.Focus()
					m.inputs[i].i.PromptStyle = focusedStyle
					m.inputs[i].i.TextStyle = focusedStyle
					continue
				}
				// Remove focused state
				m.inputs[i].i.Blur()
				m.inputs[i].i.PromptStyle = blurredStyle
				m.inputs[i].i.TextStyle = blurredStyle
			}

			return m, tea.Batch(cmds...)
		}
	case cmdFinishedMsg:
		if msg.err == nil {
			return m, tea.Quit
		} else {
			// TODO: handle errors
			log.Println(">>", msg.err)
		}

	}

	// Handle character input and blinking
	cmd := m.updateInputs(msg)

	return m, cmd
}

func (m *model) updateInputs(msg tea.Msg) tea.Cmd {
	cmds := make([]tea.Cmd, len(m.inputs))

	// Only text inputs with Focus() set will respond, so it's safe to simply
	// update all of them here without any further logic.
	for i := range m.inputs {
		m.inputs[i].i, cmds[i] = m.inputs[i].i.Update(msg)
	}

	return tea.Batch(cmds...)
}

func (m model) View() string {
	var b strings.Builder

	for i := range m.inputs {
		b.WriteString(inputStyle.Render(m.inputs[i].i.View()))
		if i < len(m.inputs)-1 {
			b.WriteRune('\n')
		}
	}

	button := &blurredButton
	if m.focusIndex == len(m.inputs) {
		button = &focusedButton
	}

	if m.completed {
		s := "Press enter to submit, esc to quit"
		button = &s
		b.Reset()
		for k, v := range m.formData {
			b.WriteString(fmt.Sprintf("%s: %s\n", k, v))
		}
	}
	fmt.Fprintf(&b, "\n\n%s\n\n", *button)

	return b.String()
}
