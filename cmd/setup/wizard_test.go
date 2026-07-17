package setup

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/setup"
)

// detectDoneMsg goes to stepSelectSDK: detected SDK in its own panel, the rest
// in a separate list, focus defaulting to the detected panel.

func TestWizard_DetectDone_TransitionsToSDKSelection(t *testing.T) {
	m := wizardModel{step: stepDetect}

	next, _ := m.Update(detectDoneMsg{result: &setup.DetectResult{SDKID: "go-server-sdk", Language: "Go"}})
	updated := next.(wizardModel)

	assert.Equal(t, stepSelectSDK, updated.step)
	// detected SDK lives in the panel, not the list, so the list has the rest.
	assert.Equal(t, len(setup.KnownSDKs)-1, len(updated.sdkList.Items()))
}

func TestWizard_DetectDone_DetectedSDKInOwnPanel_FocusedFirst(t *testing.T) {
	m := wizardModel{step: stepDetect}

	next, _ := m.Update(detectDoneMsg{result: &setup.DetectResult{SDKID: "go-server-sdk", Language: "Go"}})
	updated := next.(wizardModel)

	require.NotNil(t, updated.detectedSDK)
	assert.Equal(t, "go-server-sdk", updated.detectedSDK.id)
	assert.Equal(t, 0, updated.sdkFocus) // detected panel focused by default
}

func TestWizard_DetectDone_ListExcludesDetectedSDK(t *testing.T) {
	m := wizardModel{step: stepDetect}

	next, _ := m.Update(detectDoneMsg{result: &setup.DetectResult{SDKID: "go-server-sdk"}})
	updated := next.(wizardModel)

	for _, item := range updated.sdkList.Items() {
		assert.NotEqual(t, "go-server-sdk", item.(sdkItem).id)
	}
}

func TestWizard_DetectDone_DetectResultNotSetUntilUserConfirms(t *testing.T) {
	m := wizardModel{step: stepDetect}

	next, _ := m.Update(detectDoneMsg{result: &setup.DetectResult{SDKID: "go-server-sdk"}})
	updated := next.(wizardModel)

	assert.Nil(t, updated.detectResult)
}

func TestWizard_DetectDone_ShowsIdentifiedPanel(t *testing.T) {
	m := wizardModel{step: stepDetect, width: 80, height: 30}

	next, _ := m.Update(detectDoneMsg{result: &setup.DetectResult{SDKID: "go-server-sdk", Language: "Go"}})
	updated := next.(wizardModel)

	view := updated.View()
	assert.Contains(t, view, "We identified this as your SDK")
	assert.Contains(t, view, "❯") // detected choice is pointed to while its panel is focused
}

// detectFailedMsg goes to stepSelectSDK in default KnownSDKs order.

func TestWizard_DetectFailed_UsesGenericSDKTitle(t *testing.T) {
	m := wizardModel{step: stepDetect}

	next, _ := m.Update(detectFailedMsg{})
	updated := next.(wizardModel)

	assert.Equal(t, "Select your SDK:", updated.sdkList.Title)
}

func TestWizard_DetectFailed_TransitionsToSDKSelection(t *testing.T) {
	m := wizardModel{step: stepDetect}

	next, _ := m.Update(detectFailedMsg{})
	updated := next.(wizardModel)

	assert.Equal(t, stepSelectSDK, updated.step)
	assert.Equal(t, len(setup.KnownSDKs), len(updated.sdkList.Items()))
}

func TestWizard_DetectFailed_ListInDefaultOrder(t *testing.T) {
	m := wizardModel{step: stepDetect}

	next, _ := m.Update(detectFailedMsg{})
	updated := next.(wizardModel)

	for i, item := range updated.sdkList.Items() {
		sdk := item.(sdkItem)
		assert.Equal(t, setup.KnownSDKs[i].ID, sdk.id)
	}
}

// Selecting an SDK always sets detectResult and proceeds to install.

func TestWizard_SelectSDK_SetsDetectResultAndProceedsToInstall(t *testing.T) {
	m := wizardModel{step: stepDetect}

	next, _ := m.Update(detectDoneMsg{result: &setup.DetectResult{SDKID: "go-server-sdk", Language: "Go"}})
	updated := next.(wizardModel)
	require.Equal(t, stepSelectSDK, updated.step)

	// Press enter — selects the first (prioritized) SDK
	next, cmd := updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	selected := next.(wizardModel)

	assert.Equal(t, stepInstall, selected.step)
	require.NotNil(t, selected.detectResult)
	assert.Equal(t, "go-server-sdk", selected.detectResult.SDKID)
	assert.NotNil(t, cmd)
}

func TestWizard_SelectSDK_UserCanOverrideDetection(t *testing.T) {
	// Detection said go-server-sdk, but we'll navigate down and pick something else.
	// Here we just verify that whatever is selected (not necessarily the detected SDK)
	// becomes the detectResult.
	m := wizardModel{step: stepDetect}

	next, _ := m.Update(detectDoneMsg{result: &setup.DetectResult{SDKID: "go-server-sdk"}})
	updated := next.(wizardModel)

	// Move down to the second item
	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated = next.(wizardModel)

	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	selected := next.(wizardModel)

	require.NotNil(t, selected.detectResult)
	// Second item should not be go-server-sdk
	assert.NotEqual(t, "go-server-sdk", selected.detectResult.SDKID)
}

func TestWizard_DetectDone_EntryPointStoredForLaterUse(t *testing.T) {
	m := wizardModel{step: stepDetect}

	next, _ := m.Update(detectDoneMsg{result: &setup.DetectResult{
		SDKID:      "go-server-sdk",
		Language:   "Go",
		EntryPoint: "/my/project/main.go",
	}})
	updated := next.(wizardModel)

	// Entry point is not exposed on detectResult yet (user hasn't confirmed)
	assert.Nil(t, updated.detectResult)

	// Confirm SDK selection — entry point should now be on detectResult
	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	selected := next.(wizardModel)

	require.NotNil(t, selected.detectResult)
	assert.Equal(t, "/my/project/main.go", selected.detectResult.EntryPoint)
}

func TestWizard_Done_Success_ShowsQuitHint(t *testing.T) {
	m := wizardModel{
		step:         stepDone,
		detectResult: &setup.DetectResult{SDKID: "node-server"},
		verifyResult: &setup.VerifyResult{Active: true},
		flagKey:      "my-new-flag",
		width:        80,
		height:       30,
	}

	assert.Contains(t, m.View(), "Press q to quit")
}

func TestWizard_WaitForApp_EnterTriggersVerify(t *testing.T) {
	m := wizardModel{
		step:       stepWaitForApp,
		initResult: &setup.InitResult{SDKID: "go-server-sdk", FilePath: "/tmp/main.go", Success: true},
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(wizardModel)

	assert.Equal(t, stepVerify, updated.step)
	assert.NotNil(t, cmd)
}

func TestWizard_SelectSDK_EmptyList_DoesNotPanic(t *testing.T) {
	m := wizardModel{step: stepSelectSDK}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(wizardModel)

	assert.Equal(t, stepSelectSDK, updated.step)
	assert.Nil(t, updated.detectResult)
}
