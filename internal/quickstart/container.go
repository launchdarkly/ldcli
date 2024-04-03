package quickstart

import (
	"fmt"
	"log"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"ldcli/internal/flags"
)

const (
	defaultProjKey = "default"
	defaultEnvKey  = "test"
)

// ContainerModel is a high level container model that controls the nested models wher each
// represents a step in the quick-start flow.
type ContainerModel struct {
	err         error
	flagKey     string
	flagsClient flags.Client
	quitMsg     string // TODO: set this?
	quitting    bool

	accessToken  string
	baseUri      string
	currentModel tea.Model
	currentStep  int
	sdkKind      string
	totalSteps   int
}

func NewContainerModel(flagsClient flags.Client, accessToken string, baseUri string) tea.Model {
	return ContainerModel{
		accessToken:  accessToken,
		baseUri:      baseUri,
		currentModel: NewCreateFlagModel(flagsClient, accessToken, baseUri),
		currentStep:  1,
		flagsClient:  flagsClient,
		totalSteps:   4,
	}
}

func (m ContainerModel) Init() tea.Cmd {
	return nil
}

func (m ContainerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Quit):
			m.quitting = true
			cmd = tea.Quit
		default:
			// delegate all other input to the current model
			m.currentModel, cmd = m.currentModel.Update(msg)
		}
	case choseSDKMsg:
		m.currentModel = NewShowSDKInstructionsModel(m.accessToken, m.baseUri, msg.canonicalName, msg.url, m.flagKey)
		cmd = m.currentModel.Init()
		m.sdkKind = msg.sdkKind
		m.currentStep += 1
	case createdFlagMsg:
		m.currentModel = NewChooseSDKModel()
		m.flagKey = msg.flagKey // TODO: figure out if we maintain state here or pass in another message
		m.currentStep += 1
	case errMsg:
		m.err = msg.err
	case noInstructionsMsg:
		// skip the ShowSDKInstructionsModel and move along to toggling the flag
		m.currentModel = NewToggleFlagModel(m.flagsClient, m.accessToken, m.baseUri, m.flagKey, m.sdkKind)
		m.currentStep += 1
	case fetchedSDKInstructions, fetchedEnv, toggledFlagMsg:
		m.currentModel, cmd = m.currentModel.Update(msg)
	case showToggleFlagMsg:
		m.currentModel = NewToggleFlagModel(m.flagsClient, m.accessToken, m.baseUri, m.flagKey, m.sdkKind)
		m.currentStep += 1
	default:
		log.Println("container default - bad", msg)
	}

	return m, cmd
}

func (m ContainerModel) View() string {
	out := fmt.Sprintf("\nStep %d of %d\n"+m.currentModel.View(), m.currentStep, m.totalSteps)

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
	Tab   key.Binding
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
	Tab: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "toggle"),
	),
}
