package setup

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

type sessionState int

// list of steps in the wizard
const (
	initialStep sessionState = iota
	projectsStep
	environmentsStep
)

// high level container model
type WizardModel struct {
	quitting           bool
	err                error
	currStep           sessionState
	steps              []tea.Model
	currProjectKey     string
	currEnvironmentKey string
	// currFlagKey        string
}

func NewWizardModel() tea.Model {
	projStep, _ := NewProject()
	envStep, _ := NewEnvironment()

	steps := []tea.Model{
		projStep,
		envStep,
	}

	return WizardModel{
		currStep: initialStep,
		steps:    steps,
	}
}

func (m WizardModel) Init() tea.Cmd {
	return nil
}

func (m WizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Enter):
			switch m.currStep {
			case initialStep:
				projModel, _ := m.steps[m.currStep].Update(fetchProjects{})
				p, ok := projModel.(projectModel)
				if ok {
					if p.err != nil {
						m.err = p.err
						return m, nil
					}
				}

				m.steps[m.currStep] = projModel
				m.currStep += 1
			case projectsStep:
				projModel, _ := m.steps[m.currStep-1].Update(msg)
				p, ok := projModel.(projectModel)
				if ok {
					m.currProjectKey = p.choice
					m.currStep += 1
				}
			case environmentsStep:
				envModel, _ := m.steps[m.currStep-1].Update(msg)
				p, ok := envModel.(environmentModel)
				if ok {
					m.currEnvironmentKey = p.choice
					m.currStep += 1
				}
			default:
			}

			return m, nil
		case key.Matches(msg, keys.Back):
			if m.currStep > initialStep {
				m.currStep -= 1
			}
		case key.Matches(msg, keys.Quit):
			m.quitting = true
			return m, tea.Quit
		default:
			updatedModel, _ := m.steps[m.currStep-1].Update(msg)
			m.steps[m.currStep-1] = updatedModel
		}
	}

	return m, nil
}

func (m WizardModel) View() string {
	if m.quitting {
		return ""
	}

	if m.err != nil {
		return fmt.Sprintf("ERROR: %s", m.err)
	}

	if m.currStep == initialStep {
		return "welcome"
	}

	if m.currStep > environmentsStep {
		return fmt.Sprintf("envKey is %s, projKey is %s", m.currEnvironmentKey, m.currProjectKey)
	}

	return "\nstep 1 of x\n" + m.steps[m.currStep-1].View()
}

type keyMap struct {
	Back  key.Binding
	Enter key.Binding
	Help  key.Binding
	Quit  key.Binding
}

var keys = keyMap{
	Back: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "back"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "help"),
	),
	Quit: key.NewBinding(
		key.WithKeys("ctrl+c", "q"),
		key.WithHelp("q", "quit"),
	),
}
