package setup

import (
	"fmt"

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
	flagsStep sessionState = iota
	sdksStep
	sdkInstructionsStep
	flagToggleStep
)

// WizardModel is a high level container model that controls the nested models which each
// represent a step in the setup wizard.
type WizardModel struct {
	quitting    bool
	err         error
	currStep    sessionState
	steps       []tea.Model
	currFlagKey string
	currSdk     sdk
	width       int
}

func NewWizardModel() tea.Model {
	steps := []tea.Model{
		// Since there isn't a model for the initial step, the currStep value will always be one ahead of the step in
		// this slice. It may be convenient to add a model for the initial step to contain its own view logic and to
		// prevent this off-by-one issue.
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
				m.currStep += 1
			default:
				return m, tea.Quit

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

	if m.currStep > flagToggleStep {
		return wordwrap.String("\nCongratulations! You’ve just used feature flags to control how your application works, without having to rebuild or redeploy your application.", m.width)
	}

	return fmt.Sprintf("\nStep %d of %d\n"+m.steps[m.currStep].View(), m.currStep+1, len(m.steps))
}

type keyMap struct {
	Back   key.Binding
	Enter  key.Binding
	Help   key.Binding
	Quit   key.Binding
	Toggle key.Binding
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
	Toggle: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "toggle"),
	),
}
