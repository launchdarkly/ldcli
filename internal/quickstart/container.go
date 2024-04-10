package quickstart

import (
	"fmt"
	"log"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/muesli/reflow/wordwrap"

	"ldcli/internal/environments"
	"ldcli/internal/flags"
)

const (
	defaultProjKey = "default"
	defaultEnvKey  = "test"
)

const (
	_ = iota
	stepCreateFlag
	stepChooseSDK
	stepShowSDKInstructions
	stepToggleFlag
)

// ContainerModel is a high level container model that controls the nested models where each
// represents a step in the quick-start flow.
type ContainerModel struct {
	accessToken        string
	baseUri            string
	currentModel       tea.Model
	currentStep        int
	environmentsClient environments.Client
	err                error
	flagKey            string
	flagsClient        flags.Client
	gettingStarted     bool
	quitting           bool
	sdk                sdkDetail
	totalSteps         int
	width              int
}

func NewContainerModel(
	environmentsClient environments.Client,
	flagsClient flags.Client,
	accessToken string,
	baseUri string,
) tea.Model {
	return ContainerModel{
		accessToken:        accessToken,
		baseUri:            baseUri,
		currentModel:       NewCreateFlagModel(flagsClient, accessToken, baseUri),
		currentStep:        1,
		environmentsClient: environmentsClient,
		flagsClient:        flagsClient,
		gettingStarted:     true,
		totalSteps:         4,
	}
}

func (m ContainerModel) Init() tea.Cmd {
	return nil
}

func (m ContainerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, pressableKeys.Quit):
			m.quitting = true
			cmd = tea.Quit
		case key.Matches(msg, pressableKeys.Back):
			switch m.currentStep {
			case stepCreateFlag:
				// can only go back if a flag has been created but not confirmed,
				// so let the model handle the Update
				m.currentModel, cmd = m.currentModel.Update(msg)
			case stepChooseSDK:
				m.currentStep -= 1
				m.currentModel = NewCreateFlagModel(m.flagsClient, m.accessToken, m.baseUri)
			case stepShowSDKInstructions:
				m.currentStep -= 1
				m.currentModel = NewChooseSDKModel(m.sdk.index)
				cmd = m.currentModel.Init()
			case stepToggleFlag:
				m.currentStep -= 1
				m.currentModel = NewShowSDKInstructionsModel(
					m.environmentsClient,
					m.accessToken,
					m.baseUri,
					m.sdk.canonicalName,
					m.sdk.displayName,
					m.sdk.url,
					m.flagKey,
					m.sdk.hasInstructions,
				)
				cmd = m.currentModel.Init()
			}
		default:
			// delegate all other input to the current model
			m.currentModel, cmd = m.currentModel.Update(msg)
		}
	case choseSDKMsg:
		m.currentModel = NewShowSDKInstructionsModel(
			m.environmentsClient,
			m.accessToken,
			m.baseUri,
			msg.sdk.canonicalName,
			msg.sdk.displayName,
			msg.sdk.url,
			m.flagKey,
			msg.sdk.hasInstructions,
		)
		cmd = m.currentModel.Init()
		m.sdk = msg.sdk
		m.currentStep += 1
	case confirmedFlagMsg:
		m.currentModel = NewChooseSDKModel(0)
		m.flagKey = msg.flag.key
		m.currentStep += 1
		m.err = nil
	case errMsg:
		m.currentModel, cmd = m.currentModel.Update(msg)
	case noInstructionsMsg:
		// skip the ShowSDKInstructionsModel and move along to toggling the flag
		m.currentModel = NewToggleFlagModel(
			m.flagsClient,
			m.accessToken,
			m.baseUri,
			m.flagKey,
			m.sdk.kind,
		)
		m.currentStep += 1
	case fetchedSDKInstructionsMsg, fetchedEnvMsg, selectedSDKMsg, toggledFlagMsg, spinner.TickMsg, createdFlagMsg:
		m.gettingStarted = false
		m.currentModel, cmd = m.currentModel.Update(msg)
		m.err = nil
	case showToggleFlagMsg:
		m.currentModel = NewToggleFlagModel(
			m.flagsClient,
			m.accessToken,
			m.baseUri,
			m.flagKey,
			m.sdk.kind,
		)
		m.currentStep += 1
	default:
		log.Printf("container default: %T\n", msg)
	}

	return m, cmd
}

func (m ContainerModel) View() string {
	out := fmt.Sprintf("\nStep %d of %d\n"+m.currentModel.View(), m.currentStep, m.totalSteps)

	if m.quitting {
		return ""
	}

	if m.gettingStarted {
		out = "Within this guided setup flow, you'll be creating a new feature flag and,\nusing the SDK of your choice, building a small sample application to see a\nfeature flag toggle on and off in real time.\n\nLet's get started!\n" + out
	}

	return wordwrap.String(out, m.width)
}
