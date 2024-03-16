package setup

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/muesli/reflow/wordwrap"
)

// TODO: we may want to rename this for clarity
type sessionState int

// generic message type to pass into each models' Update method when we want to perform a new GET request
type fetchResources struct{}

// list of steps in the wizard
const (
	autoCreateStep sessionState = iota
	projectsStep
	environmentsStep
	flagsStep
	sdksStep
)

// WizardModel is a high level container model that controls the nested models which each
// represent a step in the setup wizard.
type WizardModel struct {
	quitting                bool
	err                     error
	currStep                sessionState
	steps                   []tea.Model
	useRecommendedResources bool
	currProjectKey          string
	currEnvironmentKey      string
	currFlagKey             string
	currSdk                 sdk
	width                   int
}

func NewWizardModel() tea.Model {
	steps := []tea.Model{
		// Since there isn't a model for the initial step, the currStep value will always be one ahead of the step in
		// this slice. It may be convenient to add a model for the initial step to contain its own view logic and to
		// prevent this off-by-one issue.
		NewAutoCreate(),
		NewProject(),
		NewEnvironment(),
		NewFlag(),
		NewSdk(),
	}

	return WizardModel{
		currStep: autoCreateStep,
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
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Enter):
			switch m.currStep {
			case autoCreateStep:
				model, _ := m.steps[autoCreateStep].Update(msg)
				p, ok := model.(autoCreateModel)
				if ok {
					m.useRecommendedResources = p.choice == "Yes"
					if m.useRecommendedResources {
						// create project, environment, and flag
						// go to step after flagsStep
						m.currProjectKey = "setup-wizard-project"
						m.currEnvironmentKey = "test"
						m.currFlagKey = "setup-wizard-flag"
						m.currStep = flagsStep + 1
					} else {
						// pre-load projects
						m.steps[projectsStep], _ = m.steps[projectsStep].Update(fetchResources{})
						m.currStep += 1
					}
				}
			case projectsStep:
				projModel, _ := m.steps[projectsStep].Update(msg)
				// we need to cast this to get the data out of it, but maybe we can create our own interface with
				// common values such as Choice() and Err() so we don't have to cast
				p, ok := projModel.(projectModel)
				if ok {
					m.currProjectKey = p.choice
					// update projModel with new input model
					m.steps[projectsStep] = p
					// only progress if we don't want to show input
					if !p.showInput {
						// pre-load environments based on project selected
						envModel := m.steps[environmentsStep]
						e, ok := envModel.(environmentModel)
						if ok {
							e.parentKey = m.currProjectKey
							m.steps[environmentsStep], _ = e.Update(fetchResources{})
							m.currStep += 1
						}
					}
				}
			case environmentsStep:
				envModel, _ := m.steps[environmentsStep].Update(msg)
				p, ok := envModel.(environmentModel)
				if ok {
					m.currEnvironmentKey = p.choice
					m.currStep += 1
				}
			case flagsStep:
				model, _ := m.steps[flagsStep].Update(msg)
				f, ok := model.(flagModel)
				if ok {
					m.currFlagKey = f.choice
					m.currStep += 1
				}
			case sdksStep:
				model, _ := m.steps[sdksStep].Update(msg)
				f, ok := model.(sdkModel)
				if ok {
					m.currSdk = f.choice
					m.currStep += 1
				}
				// add additional cases for additional steps
			default:
			}
		case key.Matches(msg, keys.Back):
			// if we've opted to use recommended resources but want to go back from the SDK step,
			// make sure we go back to the right step
			if m.useRecommendedResources && m.currStep == sdksStep {
				m.currStep = autoCreateStep
			}
			// only go back if not on the first step
			if m.currStep > autoCreateStep {
				// fetch resources for the previous step again in case we created new ones
				m.steps[m.currStep-1], _ = m.steps[m.currStep-1].Update(fetchResources{})
				m.currStep -= 1
			}
		case key.Matches(msg, keys.Quit):
			m.quitting = true
			return m, tea.Quit
		default:
			updatedModel, _ := m.steps[m.currStep].Update(msg)
			m.steps[m.currStep] = updatedModel
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

	if m.currStep > sdksStep {
		// consider moving this to its own view (in a new model?)
		content, err := os.ReadFile(m.currSdk.InstructionsFileName)
		if err != nil {
			fmt.Println("could not load file:", err)
			os.Exit(1)
		}
		sdkInstructions := strings.ReplaceAll(string(content), "my-flag-key", m.currFlagKey)
		return wordwrap.String(fmt.Sprintf(
			"Selected project:     %s\nSelected environment: %s\n\nSet up your application. Here are the steps to incorporate the LaunchDarkly %s SDK into your code. \n\n%s",
			m.currProjectKey,
			m.currEnvironmentKey,
			m.currSdk.Name,
			sdkInstructions,
		),
			m.width,
		)
	}

	return fmt.Sprintf("\nstep %d of %d\n"+m.steps[m.currStep].View(), m.currStep+1, len(m.steps))
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
