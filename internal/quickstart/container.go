package quickstart

import (
	"fmt"
	"log"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/muesli/reflow/wordwrap"

	"ldcli/internal/analytics"
	"ldcli/internal/environments"
	"ldcli/internal/flags"
)

const (
	defaultProjKey = "default"
	defaultEnvKey  = "test"
)

type step int

const (
	_ step = iota
	stepCreateFlag
	stepChooseSDK
	stepShowSDKInstructions
	stepToggleFlag
)

func (s step) String() string {
	return []string{
		"_",
		"1 - feature flag name",
		"2 - sdk selection",
		"3 - sdk installation",
		"4 - flag toggle",
	}[s]
}

// ContainerModel is a high level container model that controls the nested models where each
// represents a step in the quick-start flow.
type ContainerModel struct {
	accessToken        string
	analyticsOptOut    bool
	analyticsTracker   analytics.Tracker
	baseUri            string
	currentModel       tea.Model
	currentStep        step
	environment        *environment
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
	analyticsTracker analytics.Tracker,
	environmentsClient environments.Client,
	flagsClient flags.Client,
	accessToken string,
	analyticsOptOut bool,
	baseUri string,
) tea.Model {
	return ContainerModel{
		accessToken:        accessToken,
		analyticsOptOut:    analyticsOptOut,
		analyticsTracker:   analyticsTracker,
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
	return trackSetupStepStartedEvent(
		m.analyticsTracker,
		m.accessToken,
		m.baseUri,
		m.analyticsOptOut,
		m.currentStep.String(),
	)
}

func (m ContainerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var sendEvent bool
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
					m.environment,
				)
				cmd = m.currentModel.Init()
				sendEvent = true
			}
		default:
			// delegate all other input to the current model
			m.currentModel, cmd = m.currentModel.Update(msg)
		}
	case confirmedFlagMsg:
		m.currentModel = NewChooseSDKModel(0)
		m.flagKey = msg.flag.key
		m.currentStep += 1
		m.err = nil
		sendEvent = true
	case choseSDKMsg:
		m.currentModel = NewShowSDKInstructionsModel(
			m.environmentsClient,
			m.accessToken,
			m.baseUri,
			msg.sdk.canonicalName,
			msg.sdk.displayName,
			msg.sdk.url,
			m.flagKey,
			m.environment,
		)
		cmd = m.currentModel.Init()
		m.sdk = msg.sdk
		m.currentStep += 1
		sendEvent = true
	case errMsg:
		m.currentModel, cmd = m.currentModel.Update(msg)
	case fetchedEnvMsg:
		m.environment = &msg.environment
		m.currentModel, cmd = m.currentModel.Update(msg)
		m.err = nil
	case fetchedFlagStatusMsg:
		m.currentModel, cmd = m.currentModel.Update(msg)
		m.err = nil
	case fetchedSDKInstructionsMsg, selectedSDKMsg, toggledFlagMsg, spinner.TickMsg, createdFlagMsg:
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
		cmd = m.currentModel.Init()
		m.currentStep += 1
		sendEvent = true
	default:
		log.Printf("container default: %T\n", msg)
	}

	if sendEvent {
		cmd = tea.Batch(cmd, trackSetupStepStartedEvent(
			m.analyticsTracker,
			m.accessToken,
			m.baseUri,
			m.analyticsOptOut,
			m.currentStep.String(),
		))
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
