package quickstart

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"ldcli/internal/flags"
)

// step is an identifier for each step in the quick-start flow.
type step int

const (
	createFlagStep step = iota
	chooseSDKStep
)

// ContainerModel is a high level container model that controls the nested models wher each
// represents a step in the quick-start flow.
type ContainerModel struct {
	currentStep step
	err         error
	flagKey     string
	flagsClient flags.Client
	quitMsg     string
	quitting    bool
	sdk         sdkDetail
	steps       []tea.Model
}

func NewContainerModel(flagsClient flags.Client) tea.Model {
	return ContainerModel{
		currentStep: createFlagStep,
		flagsClient: flagsClient,
		steps: []tea.Model{
			NewCreateFlagModel(flagsClient),
			NewChooseSDKModel(),
		},
	}
}

func (m ContainerModel) Init() tea.Cmd {
	return nil
}

func (m ContainerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Enter):
			switch m.currentStep {
			case createFlagStep:
				updated, cmd := m.steps[createFlagStep].Update(msg)
				if model, ok := updated.(createFlagModel); ok {
					if model.err != nil {
						m.err = model.err
						if model.quitting {
							m.quitMsg = model.quitMsg
							m.quitting = true

							return m, cmd
						}

						return m, nil
					}

					m.flagKey = model.flagKey
					m.currentStep += 1
				}
			case chooseSDKStep:
				updated, _ := m.steps[chooseSDKStep].Update(msg)
				if model, ok := updated.(chooseSDKModel); ok {
					// no error state for this step
					m.sdk = model.selectedSdk
					m.currentStep += 1
				}
			default:
			}
		case key.Matches(msg, keys.Quit):
			m.quitting = true

			return m, tea.Quit
		default:
			// delegate all other input to the current model
			updated, _ := m.steps[m.currentStep].Update(msg)
			m.steps[m.currentStep] = updated
		}
	default:
	}

	return m, nil
}

func (m ContainerModel) View() string {
	// TODO: remove after creating more steps
	if m.currentStep > chooseSDKStep {
		return fmt.Sprintf("created flag %s\nselected the %s SDK", m.flagKey, m.sdk.DisplayName)
	}

	out := fmt.Sprintf("\nStep %d of %d\n"+m.steps[m.currentStep].View(), m.currentStep+1, len(m.steps))
	if m.err != nil {
		if m.quitting {
			out := m.quitMsg + "\n\n"
			out += m.err.Error()

			return lipgloss.
				NewStyle().
				SetString(out).
				Render() + "\n"
		}

		// show error and stay on the same step
		out += "\n" + lipgloss.
			NewStyle().
			SetString(m.err.Error()).
			Render() + "\n"

		return out
	}

	return out
}

type keyMap struct {
	Enter key.Binding
	Quit  key.Binding
}

var keys = keyMap{
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select"),
	),
	Quit: key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("q", "quit"),
	),
}
