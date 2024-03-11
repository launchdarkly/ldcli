package setup

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// TODO: we may want to rename this for clarity
type sessionState int

// list of steps in the wizard
const (
	initialStep sessionState = iota
	projectsStep
	environmentsStep
	flagsStep
)

// WizardModel is a high level container model that controls the nested models which each
// represent a step in the setup wizard.
type WizardModel struct {
	quitting           bool
	err                error
	currStep           sessionState
	steps              []tea.Model
	currProjectKey     string
	currEnvironmentKey string
	currFlagKey        string
}

func NewWizardModel() tea.Model {
	steps := []tea.Model{
		// Since there isn't a model for the initial step, the currStep value will always be one ahead of the step in
		// this slice. It may be convenient to add a model for the initial step to contain its own view logic and to
		// prevent this off-by-one issue.
		NewProject(),
		NewEnvironment(),
		Newflag(),
	}

	return WizardModel{
		currStep: initialStep,
		steps:    steps,
	}
}

func (m WizardModel) Init() tea.Cmd {
	return nil
}

// Update controls all the messages passed around and delegates to the relevant nested model depending on which step
// the user is on.
func (m WizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Enter):
			switch m.currStep {
			case initialStep:
				projModel, _ := m.steps[m.currStep].Update(fetchProjects{})
				// we need to cast this to get the data out of it, but maybe we can create our own interface with
				// common values such as Choice() and Err() so we don't have to cast
				p, ok := projModel.(projectModel)
				if ok {
					if p.err != nil {
						m.err = p.err
						return m, nil
					}
				}

				// update the nested model
				m.steps[m.currStep] = projModel
				// go to the next step
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
			case flagsStep:
				model, _ := m.steps[m.currStep-1].Update(msg)
				f, ok := model.(flagModel)
				if ok {
					m.currFlagKey = f.choice
					m.currStep += 1
				}
				// add additional cases for additional steps
			default:
			}
		case key.Matches(msg, keys.Back):
			// only go back if not on the first step
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

	if m.currStep > flagsStep {
		return fmt.Sprintf("envKey is %s, projKey is %s, flagKey is %s", m.currEnvironmentKey, m.currProjectKey, m.currFlagKey)
	}

	return fmt.Sprintf("\nstep %d of %d\n"+m.steps[m.currStep-1].View(), m.currStep, len(m.steps))
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
