package wizard

import (
	"errors"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestInstallModel(sdk sdkDetail) installSDKModel {
	s := spinner.New()
	return installSDKModel{
		sdk:        sdk,
		installing: true,
		spinner:    s,
	}
}

func TestInstallSDKModel_Update(t *testing.T) {
	sdk := sdkDetail{id: "go-server-sdk", displayName: "Go"}

	t.Run("installedSDKMsg marks done and clears installing", func(t *testing.T) {
		m := newTestInstallModel(sdk)

		result, _ := m.Update(installedSDKMsg{output: "success output"})

		got := result.(installSDKModel)
		assert.False(t, got.installing)
		assert.True(t, got.done)
		assert.False(t, got.skipped)
		assert.Equal(t, "success output", got.output)
	})

	t.Run("installSkippedMsg marks skipped and clears installing", func(t *testing.T) {
		m := newTestInstallModel(sdk)

		result, _ := m.Update(installSkippedMsg{})

		got := result.(installSDKModel)
		assert.False(t, got.installing)
		assert.True(t, got.skipped)
		assert.False(t, got.done)
	})

	t.Run("installErrMsg records error and clears installing", func(t *testing.T) {
		m := newTestInstallModel(sdk)
		testErr := errors.New("install failed")

		result, _ := m.Update(installErrMsg{err: testErr})

		got := result.(installSDKModel)
		assert.False(t, got.installing)
		assert.False(t, got.done)
		assert.Equal(t, testErr, got.err)
	})

	t.Run("enter key while installing does not advance", func(t *testing.T) {
		m := newTestInstallModel(sdk)

		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

		assert.Nil(t, cmd)
	})

	t.Run("enter key after done emits continueFromInstallMsg", func(t *testing.T) {
		m := newTestInstallModel(sdk)
		m.installing = false
		m.done = true

		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

		require.NotNil(t, cmd)
		msg := cmd()
		_, ok := msg.(continueFromInstallMsg)
		assert.True(t, ok, "expected continueFromInstallMsg, got %T", msg)
	})

	t.Run("enter key after skipped emits continueFromInstallMsg", func(t *testing.T) {
		m := newTestInstallModel(sdk)
		m.installing = false
		m.skipped = true

		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

		require.NotNil(t, cmd)
		msg := cmd()
		_, ok := msg.(continueFromInstallMsg)
		assert.True(t, ok)
	})

	t.Run("enter key after error emits continueFromInstallMsg", func(t *testing.T) {
		m := newTestInstallModel(sdk)
		m.installing = false
		m.err = errors.New("some error")

		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

		require.NotNil(t, cmd)
		msg := cmd()
		_, ok := msg.(continueFromInstallMsg)
		assert.True(t, ok)
	})
}

func TestInstallSDKModel_View(t *testing.T) {
	sdk := sdkDetail{id: "go-server-sdk", displayName: "Go"}

	t.Run("shows spinner while installing", func(t *testing.T) {
		m := newTestInstallModel(sdk)

		view := m.View()

		assert.Contains(t, view, "Installing Go SDK")
	})

	t.Run("shows skip message when skipped", func(t *testing.T) {
		m := newTestInstallModel(sdk)
		m.installing = false
		m.skipped = true

		view := m.View()

		assert.Contains(t, view, "manual installation")
		assert.Contains(t, view, "press enter to continue")
	})

	t.Run("shows error message on failure", func(t *testing.T) {
		m := newTestInstallModel(sdk)
		m.installing = false
		m.err = errors.New("exit status 1")

		view := m.View()

		assert.Contains(t, view, "exit status 1")
		assert.Contains(t, view, "Press enter to continue")
	})

	t.Run("shows success message when done", func(t *testing.T) {
		m := newTestInstallModel(sdk)
		m.installing = false
		m.done = true

		view := m.View()

		assert.Contains(t, view, "installed successfully")
		assert.Contains(t, view, "Press enter to continue")
	})
}
