package wizard

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/environments"
	"github.com/launchdarkly/ldcli/internal/flags"
)

func newTestContainer() ContainerModel {
	return ContainerModel{
		accessToken:        "test-token",
		baseURI:            "https://example.com",
		environmentsClient: &environments.MockClient{},
		flagsClient:        &flags.MockClient{},
		currentStep:        stepChooseSDK,
		currentModel:       noopModel{},
		gettingStarted:     true,
	}
}

func TestContainerModel_StepProgression(t *testing.T) {
	t.Run("choseSDKMsg advances to stepInstallSDK", func(t *testing.T) {
		m := newTestContainer()
		sdk := sdkDetail{id: "go-server-sdk", displayName: "Go"}

		result, _ := m.Update(choseSDKMsg{sdk: sdk})

		got := result.(ContainerModel)
		assert.Equal(t, stepInstallSDK, got.currentStep)
		assert.Equal(t, sdk, got.sdk)
		assert.False(t, got.gettingStarted)
		_, isInstallModel := got.currentModel.(installSDKModel)
		assert.True(t, isInstallModel, "expected installSDKModel, got %T", got.currentModel)
	})

	t.Run("continueFromInstallMsg advances to stepCreateFlag", func(t *testing.T) {
		m := newTestContainer()
		m.currentStep = stepInstallSDK
		m.currentModel = noopModel{}

		result, _ := m.Update(continueFromInstallMsg{})

		got := result.(ContainerModel)
		assert.Equal(t, stepCreateFlag, got.currentStep)
		_, isCreateFlagModel := got.currentModel.(createFlagModel)
		assert.True(t, isCreateFlagModel, "expected createFlagModel, got %T", got.currentModel)
	})

	t.Run("confirmedFlagMsg advances to stepInjectCode", func(t *testing.T) {
		m := newTestContainer()
		m.currentStep = stepCreateFlag
		m.currentModel = noopModel{}
		f := flag{key: "my-flag", name: "My Flag"}

		result, _ := m.Update(confirmedFlagMsg{flag: f})

		got := result.(ContainerModel)
		assert.Equal(t, stepInjectCode, got.currentStep)
		assert.Equal(t, f, got.flag)
		_, isInjectModel := got.currentModel.(injectCodeModel)
		assert.True(t, isInjectModel, "expected injectCodeModel, got %T", got.currentModel)
	})

	t.Run("wroteInitFileMsg advances to stepDone", func(t *testing.T) {
		m := newTestContainer()
		m.currentStep = stepInjectCode
		m.currentModel = noopModel{}

		result, _ := m.Update(wroteInitFileMsg{filename: "/project/launchdarkly.go"})

		got := result.(ContainerModel)
		assert.Equal(t, stepDone, got.currentStep)
		assert.Equal(t, "/project/launchdarkly.go", got.initFile)
		_, isNoop := got.currentModel.(noopModel)
		assert.True(t, isNoop, "expected noopModel after done, got %T", got.currentModel)
	})

	t.Run("fetchedEnvMsg is stored and forwarded to child model", func(t *testing.T) {
		m := newTestContainer()
		m.currentStep = stepInjectCode
		// Use a noop model so Update is a no-op on the child
		m.currentModel = noopModel{}
		env := envData{sdkKey: "sdk-abc", mobileKey: "mob-xyz", clientSideId: "csi-456"}

		result, _ := m.Update(fetchedEnvMsg{env: env})

		got := result.(ContainerModel)
		require.NotNil(t, got.env)
		assert.Equal(t, "sdk-abc", got.env.sdkKey)
	})
}

func TestContainerModel_QuitBehavior(t *testing.T) {
	t.Run("ctrl+c sets quitting and returns tea.Quit", func(t *testing.T) {
		m := newTestContainer()

		result, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})

		got := result.(ContainerModel)
		assert.True(t, got.quitting)
		require.NotNil(t, cmd)
		// tea.Quit returns a special quit message
		msg := cmd()
		_, isQuit := msg.(tea.QuitMsg)
		assert.True(t, isQuit, "expected tea.QuitMsg, got %T", msg)
	})

	t.Run("View returns empty string when quitting", func(t *testing.T) {
		m := newTestContainer()
		m.quitting = true

		view := m.View()

		assert.Equal(t, "", view)
	})
}

func TestContainerModel_View(t *testing.T) {
	t.Run("shows intro text on first step", func(t *testing.T) {
		m := newTestContainer()
		m.currentStep = stepChooseSDK
		m.gettingStarted = true
		m.currentModel = noopModel{}

		view := m.View()

		assert.Contains(t, view, "LaunchDarkly")
		assert.Contains(t, view, "Let's get started")
	})

	t.Run("shows step counter for non-done steps", func(t *testing.T) {
		m := newTestContainer()
		m.currentStep = stepCreateFlag
		m.gettingStarted = false
		m.currentModel = noopModel{}

		view := m.View()

		assert.Contains(t, view, "Step 3 of 4")
	})

	t.Run("done view shows setup complete", func(t *testing.T) {
		m := newTestContainer()
		m.currentStep = stepDone
		m.sdk = sdkDetail{id: "go-server-sdk", displayName: "Go"}
		m.flag = flag{key: "my-flag", name: "My Flag"}
		m.env = &envData{sdkKey: "sdk-abc123"}

		view := m.View()

		assert.Contains(t, view, "Setup complete!")
		assert.Contains(t, view, "my-flag")
		assert.Contains(t, view, "Go")
	})

	t.Run("done view shows env var export command", func(t *testing.T) {
		m := newTestContainer()
		m.currentStep = stepDone
		m.sdk = sdkDetail{id: "go-server-sdk", displayName: "Go"}
		m.flag = flag{key: "my-flag", name: "My Flag"}
		m.env = &envData{sdkKey: "sdk-abc123"}

		view := m.View()

		assert.Contains(t, view, "LAUNCHDARKLY_SDK_KEY")
		assert.Contains(t, view, "sdk-abc123")
	})

	t.Run("done view includes dashboard link", func(t *testing.T) {
		m := newTestContainer()
		m.currentStep = stepDone
		m.sdk = sdkDetail{id: "go-server-sdk", displayName: "Go"}
		m.flag = flag{key: "cool-flag", name: "Cool Flag"}

		view := m.View()

		assert.Contains(t, view, "cool-flag")
		assert.Contains(t, view, "launchdarkly.com")
	})

	t.Run("done view omits init file section when no init file was written", func(t *testing.T) {
		m := newTestContainer()
		m.currentStep = stepDone
		m.sdk = sdkDetail{id: "go-server-sdk", displayName: "Go"}
		m.flag = flag{key: "my-flag", name: "My Flag"}
		m.initFile = ""

		view := m.View()

		assert.NotContains(t, view, "Init file")
	})
}

func TestStep_String(t *testing.T) {
	tests := map[step]string{
		stepChooseSDK:  "1 - sdk selection",
		stepInstallSDK: "2 - sdk installation",
		stepCreateFlag: "3 - feature flag",
		stepInjectCode: "4 - init code",
		stepDone:       "5 - done",
	}

	for s, want := range tests {
		assert.Equal(t, want, s.String())
	}
}
