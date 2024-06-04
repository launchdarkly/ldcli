package interactive_mode

import (
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	focusedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("fff"))
	blurredStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	defaultStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	inputStyle    = lipgloss.NewStyle()
	blurredButton = fmt.Sprintf("[%s]", blurredStyle.Render("Submit"))
	focusedButton = focusedStyle.Copy().Render("[Submit]")
)

type cmdFinishedMsg struct {
	err error
}

type Input struct {
	Prompt   string
	Required bool
	Type     string
}

type inputModel struct {
	input Input
	ti    textinput.Model
}

type model struct {
	resourceName string
	command      string
	completed    bool
	focusIndex   int
	inputs       []inputModel
	formData     map[string]any
}

func NewInteractiveInputModel(resourceName, command string, formInputs []Input) model {
	inputs := make([]inputModel, 0)
	for _, input := range formInputs {
		inputs = append(inputs, inputModel{ti: textinput.New(), input: input})
	}

	m := model{
		resourceName: resourceName,
		command:      command,
		formData:     make(map[string]any),
		inputs:       inputs,
	}

	for i := range m.inputs {
		m.inputs[i].ti.PromptStyle = defaultStyle
		m.inputs[i].ti.TextStyle = defaultStyle
		output := getDisplayPrompt(m.inputs[i].input.Prompt)
		m.inputs[i].ti.Prompt = output
		if m.inputs[i].input.Required {
			m.inputs[i].ti.Prompt += " (required): "
		} else {
			m.inputs[i].ti.Prompt += ": "
		}
	}
	m.inputs[0].ti.Focus()
	m.inputs[0].ti.PromptStyle = focusedStyle
	m.inputs[0].ti.TextStyle = focusedStyle

	return m
}

func getDisplayPrompt(prompt string) string {
	// Compile regex to find positions where a lowercase letter is followed by an uppercase letter
	re := regexp.MustCompile("([a-z])([A-Z])")
	// Replace matches with the lowercase letter, a space, and the uppercase letter
	out := re.ReplaceAllString(prompt, "$1 $2")
	// Capitalize the first letter of the result
	caser := cases.Title(language.English)
	out = caser.String(out)
	return out
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
				return m, m.runCmd()
			}
			if m.focusIndex == len(m.inputs) {
				return m.updateFormData()
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
					cmds[i] = m.inputs[i].ti.Focus()
					m.inputs[i].ti.PromptStyle = focusedStyle
					m.inputs[i].ti.TextStyle = focusedStyle
					continue
				}
				// Remove focused state
				m.inputs[i].ti.Blur()
				m.inputs[i].ti.PromptStyle = blurredStyle
				m.inputs[i].ti.TextStyle = blurredStyle
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

func (m model) updateFormData() (tea.Model, tea.Cmd) {
	for _, input := range m.inputs {
		inputValue := input.ti.Value()
		if inputValue != "" {
			var val interface{}
			switch input.input.Type {
			case "string":
				val = inputValue
			case "array":
				val = strings.Split(inputValue, ",")
			case "boolean":
				val, _ = strconv.ParseBool(inputValue)
				// TODO: handle error
			case "integer":
				val, _ = strconv.Atoi(inputValue)
				// TODO: handle error
			case "object":
				// TODO
			}
			m.formData[input.input.Prompt] = val
		}

	}
	return m, completeForm()
}

func (m *model) updateInputs(msg tea.Msg) tea.Cmd {
	cmds := make([]tea.Cmd, len(m.inputs))

	// Only text inputs with Focus() set will respond, so it's safe to simply
	// update all of them here without any further logic.
	for i := range m.inputs {
		m.inputs[i].ti, cmds[i] = m.inputs[i].ti.Update(msg)
	}

	return tea.Batch(cmds...)
}

func (m model) runCmd() tea.Cmd {
	data, _ := json.Marshal(m.formData)
	// TODO: handle error

	log.Println("ldcli", m.resourceName, m.command, "--data", string(data))
	c := exec.Command("ldcli", m.resourceName, m.command, "--data", string(data)) //nolint:gosec
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return cmdFinishedMsg{err}
	})
}

func (m model) View() string {
	var b strings.Builder

	for i := range m.inputs {
		b.WriteString(inputStyle.Render(m.inputs[i].ti.View()))
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
