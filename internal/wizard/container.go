package wizard

import (
	"fmt"
	"path/filepath"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/muesli/reflow/wordwrap"

	"github.com/launchdarkly/ldcli/internal/environments"
	"github.com/launchdarkly/ldcli/internal/flags"
)

// noopModel is a placeholder that absorbs any stale messages after the wizard completes.
type noopModel struct{}

func (noopModel) Init() tea.Cmd                           { return nil }
func (noopModel) Update(tea.Msg) (tea.Model, tea.Cmd)     { return noopModel{}, nil }
func (noopModel) View() string                            { return "" }

type step int

const (
	_ step = iota
	stepChooseSDK   // 1
	stepInstallSDK  // 2
	stepCreateFlag  // 3
	stepInjectCode  // 4
	stepDone        // 5
)

func (s step) String() string {
	return []string{
		"_",
		"1 - sdk selection",
		"2 - sdk installation",
		"3 - feature flag",
		"4 - init code",
		"5 - done",
	}[s]
}

// ContainerModel is the top-level model that orchestrates the wizard steps.
type ContainerModel struct {
	accessToken        string
	baseURI            string
	currentModel       tea.Model
	currentStep        step
	environmentsClient environments.Client
	flagsClient        flags.Client
	workDir            string
	height             int
	width              int
	quitting           bool
	gettingStarted     bool

	// populated as the wizard progresses
	sdk      sdkDetail
	flag     flag
	env      *envData
	initFile string
}

func NewContainerModel(
	environmentsClient environments.Client,
	flagsClient flags.Client,
	accessToken, baseURI, workDir string,
) tea.Model {
	detectedIDs := DetectStack(workDir)
	return ContainerModel{
		accessToken:        accessToken,
		baseURI:            baseURI,
		currentModel:       NewChooseSDKModel(detectedIDs),
		currentStep:        stepChooseSDK,
		environmentsClient: environmentsClient,
		flagsClient:        flagsClient,
		workDir:            workDir,
		gettingStarted:     true,
	}
}

func (m ContainerModel) Init() tea.Cmd {
	return m.currentModel.Init()
}

func (m ContainerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
		m.width = msg.Width
		m.currentModel, cmd = m.currentModel.Update(msg)

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, pressableKeys.Quit):
			m.quitting = true
			cmd = tea.Quit
		default:
			m.currentModel, cmd = m.currentModel.Update(msg)
		}

	case choseSDKMsg:
		m.sdk = msg.sdk
		m.currentStep = stepInstallSDK
		m.gettingStarted = false
		packageManager := DetectPackageManager(m.workDir)
		m.currentModel = NewInstallSDKModel(m.sdk, packageManager, m.workDir)
		cmd = m.currentModel.Init()

	case continueFromInstallMsg:
		m.currentStep = stepCreateFlag
		m.currentModel = NewCreateFlagModel(m.flagsClient, m.accessToken, m.baseURI)
		cmd = m.currentModel.Init()

	case confirmedFlagMsg:
		m.flag = msg.flag
		m.currentStep = stepInjectCode
		m.currentModel = NewInjectCodeModel(
			m.environmentsClient,
			m.accessToken,
			m.baseURI,
			m.height,
			m.width,
			m.sdk,
			m.flag.key,
			m.workDir,
		)
		cmd = m.currentModel.Init()

	case wroteInitFileMsg:
		m.initFile = msg.filename
		m.currentStep = stepDone
		m.currentModel = noopModel{}

	case fetchedEnvMsg:
		m.env = &msg.env
		m.currentModel, cmd = m.currentModel.Update(msg)

	case errMsg:
		m.currentModel, cmd = m.currentModel.Update(msg)

	default:
		m.currentModel, cmd = m.currentModel.Update(msg)
	}

	return m, cmd
}

func (m ContainerModel) View() string {
	if m.quitting {
		return ""
	}

	totalSteps := 4 // steps 1-4; done is the exit state
	var body string

	if m.currentStep == stepDone {
		body = m.doneView()
	} else {
		body = fmt.Sprintf("Step %d of %d\n", m.currentStep, totalSteps) + m.currentModel.View()
	}

	if m.gettingStarted {
		intro := "This wizard will connect LaunchDarkly to your existing project.\n" +
			"We'll detect your stack, install the SDK, add an init file, and create\n" +
			"your first feature flag — without leaving the terminal.\n\nLet's get started!\n\n"
		body = intro + body
	}

	return wordwrap.String(body, m.width)
}

func (m ContainerModel) doneView() string {
	sdkKeyEnv := envVarName(m.sdk)
	sdkKeyVal := envVarValue(m.sdk, m.env)

	initFileDisplay := m.initFile
	if m.workDir != "" && m.initFile != "" {
		if rel, err := filepath.Rel(m.workDir, m.initFile); err == nil {
			initFileDisplay = rel
		}
	}

	out := "Setup complete!\n\n"
	out += fmt.Sprintf("  SDK installed : %s\n", m.sdk.displayName)
	if initFileDisplay != "" {
		out += fmt.Sprintf("  Init file     : %s\n", initFileDisplay)
	}
	out += fmt.Sprintf("  Feature flag  : %s\n", m.flag.key)
	out += "\nNext steps:\n\n"
	out += fmt.Sprintf("  1. Set your SDK key:\n     export %s=%s\n\n", sdkKeyEnv, sdkKeyVal)
	if initFileDisplay != "" {
		out += fmt.Sprintf("  2. Import the init code from %s into your project.\n\n", initFileDisplay)
	}
	out += fmt.Sprintf("  3. View your flag at:\n     https://app.launchdarkly.com/default/test/features/%s\n\n", m.flag.key)
	out += "Press ctrl+c to exit.\n"

	return out
}
