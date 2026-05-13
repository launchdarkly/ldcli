package setup

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/setup"
)

func TestWizard_DetectFailed_TransitionsToSDKSelection(t *testing.T) {
	m := wizardModel{step: stepDetect}

	next, _ := m.Update(detectFailedMsg{})
	updated := next.(wizardModel)

	assert.Equal(t, stepSelectSDK, updated.step)
	assert.Equal(t, len(setup.KnownSDKs), len(updated.sdkList.Items()))
}

func TestWizard_DetectFailed_SDKListTitles(t *testing.T) {
	m := wizardModel{step: stepDetect}

	next, _ := m.Update(detectFailedMsg{})
	updated := next.(wizardModel)

	// Verify the list items match KnownSDKs in order
	for i, item := range updated.sdkList.Items() {
		sdk := item.(sdkItem)
		assert.Equal(t, setup.KnownSDKs[i].ID, sdk.id)
		assert.Equal(t, setup.KnownSDKs[i].Name, sdk.name)
		assert.Equal(t, setup.KnownSDKs[i].Language, sdk.language)
	}
}

func TestWizard_SelectSDK_SetsDetectResultAndProceedsToInstall(t *testing.T) {
	m := wizardModel{step: stepDetect}

	// Transition to stepSelectSDK
	next, _ := m.Update(detectFailedMsg{})
	updated := next.(wizardModel)
	require.Equal(t, stepSelectSDK, updated.step)

	// Press enter — selects the first SDK in the list
	next, cmd := updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	selected := next.(wizardModel)

	assert.Equal(t, stepInstall, selected.step)
	require.NotNil(t, selected.detectResult)
	assert.Equal(t, setup.KnownSDKs[0].ID, selected.detectResult.SDKID)
	assert.Equal(t, setup.KnownSDKs[0].Language, selected.detectResult.Language)
	// cmd is the runInstall tea.Cmd — should be non-nil
	assert.NotNil(t, cmd)
}

func TestWizard_SelectSDK_EmptyList_DoesNotPanic(t *testing.T) {
	m := wizardModel{step: stepSelectSDK}
	// sdkList not initialized — SelectedItem returns nil

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(wizardModel)

	// Should stay on stepSelectSDK without panicking
	assert.Equal(t, stepSelectSDK, updated.step)
	assert.Nil(t, updated.detectResult)
}
