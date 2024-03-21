package setup

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// TODO: we may want to rename this for clarity
type sessionState int

// generic message type to pass into each models' Update method when we want to perform a new GET request
type fetchResources struct{}

// list of steps in the wizard
const (
	loginStep sessionState = iota
	autoCreateStep
	projectsStep
	environmentsStep
	flagsStep
	sdksStep
	sdkInstructionsStep
	flagToggleStep
)

type inputs struct {
	TokenSecret string
}

// WizardModel is a high level container model that controls the nested models which each
// represent a step in the setup wizard.
type WizardModel struct {
	quitting                bool
	err                     error
	currStep                sessionState
	steps                   []tea.Model
	inputs                  inputs
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
		NewLogin(),
		NewAutoCreate(),
		NewProject(),
		NewEnvironment(),
		NewFlag(),
		NewSdk(),
		NewSDKInstructions(),
		NewFlagToggle(),
	}

	return WizardModel{
		currStep: 0,
		steps:    steps,
	}
}

func (m WizardModel) Init() tea.Cmd {
	return nil
}

func (m WizardModel) updateForm(msg tea.Msg, model ViewModelWithTextInput) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Enter):
			us, _ := model.SetFormFocus(false)
			usModel, ok := us.(ViewModelWithTextInput)
			if ok {
				ov := usModel.InputValue()
				reflect.ValueOf(&m.inputs).Elem().FieldByName(ov.Key).Set(reflect.ValueOf(ov.Value))
				m.steps[m.currStep] = usModel
				m.currStep += 1
			}
		case key.Matches(msg, keys.Back):
			us, _ := model.SetFormFocus(false)
			usModel, ok := us.(ViewModelWithTextInput)
			if ok {
				m.steps[m.currStep] = usModel
			}
		default:
			model, _ := m.steps[m.currStep].Update(msg)
			m.steps[m.currStep] = model
		}
	}

	return m, nil
}

// Update controls all the messages passed around and delegates to the relevant nested model depending on which step
// the user is on.
func (m WizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {

	if stepModel, ok := m.steps[m.currStep].(ViewModelWithTextInput); ok && stepModel.FormFocus() {
		return m.updateForm(msg, stepModel)
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Enter):
			switch m.currStep {
			case loginStep:
				model, _ := m.steps[loginStep].Update(msg)
				l, ok := model.(loginModel)
				if ok {
					m.steps[loginStep] = l
					if l.loggedIn {
						m.currStep += 1
					}
				}
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
					// pre-load flags based on environment selected
					fModel := m.steps[flagsStep]
					f, ok := fModel.(flagModel)
					if ok {
						f.parentKey = m.currEnvironmentKey
						m.steps[flagsStep], _ = f.Update(fetchResources{})
						m.currStep += 1
					}
				}
			case flagsStep:
				model, _ := m.steps[flagsStep].Update(msg)
				f, ok := model.(flagModel)
				if ok && f.input != "" {
					m.currFlagKey = f.input
					m.currStep += 1
				}
			case sdksStep:
				model, _ := m.steps[sdksStep].Update(msg)
				f, ok := model.(sdkModel)
				if ok {
					m.currSdk = f.choice
					m.currStep += 1

					// update the sdkInstructionModel so it can show the selected SDK instructions
					model := m.steps[sdkInstructionsStep]
					f, ok := model.(sdkInstructionModel)
					if ok {
						f.filename = m.currSdk.InstructionsFileName
						f.flagKey = m.currFlagKey
						f.name = m.currSdk.Name
						f.width = m.width
						m.steps[sdkInstructionsStep] = f
					}

					// update the flagToggleModel so it can show the flag key
					model = m.steps[flagToggleStep]
					f2, ok := model.(flagToggleModel)
					if ok {
						f2.flagKey = m.currFlagKey
						m.steps[flagToggleStep] = f2
					}
				}
			case sdkInstructionsStep:
				m.currStep += 1
			case flagToggleStep:
				updatedModel, _ := m.steps[flagToggleStep].Update(msg)
				m.steps[flagToggleStep] = updatedModel
			default:
			}
		case key.Matches(msg, keys.Back):
			// only go back if not on the first step
			if m.currStep > 0 {
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

func openbrowser(url string) {
	var err error

	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default:
		err = fmt.Errorf("unsupported platform")
	}
	if err != nil {
		log.Fatal(err)
	}

}
