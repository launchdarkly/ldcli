package setup

import (
	"os"
	"path/filepath"
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

func TestWizard_SelectSDK_ProceedsToPlanThenInstall(t *testing.T) {
	m := wizardModel{step: stepDetect}

	next, _ := m.Update(detectDoneMsg{result: &setup.DetectResult{SDKID: "go-server-sdk", Language: "Go"}})
	updated := next.(wizardModel)
	require.Equal(t, stepSelectSDK, updated.step)

	// Enter accepts the detected SDK and shows the plan (no action taken yet).
	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	planned := next.(wizardModel)
	assert.Equal(t, stepPlan, planned.step)
	require.NotNil(t, planned.detectResult)
	assert.Equal(t, "go-server-sdk", planned.detectResult.SDKID)

	// Enter on the plan proceeds to install.
	next, cmd := planned.Update(tea.KeyMsg{Type: tea.KeyEnter})
	installing := next.(wizardModel)
	assert.Equal(t, stepInstall, installing.step)
	assert.NotNil(t, cmd)
}

func TestWizard_Plan_ListsSteps(t *testing.T) {
	m := wizardModel{
		step:            stepPlan,
		selectedProject: "default",
		selectedEnv:     "test",
		detectResult:    &setup.DetectResult{SDKID: "node-server", EntryPoint: "src/index.js"},
		planInstallCmd:  "npm install @launchdarkly/node-server-sdk",
		width:           80,
		height:          30,
	}

	view := m.planView()
	assert.Contains(t, view, "Here's what setup will do:")
	assert.Contains(t, view, "npm install @launchdarkly/node-server-sdk")
	assert.Contains(t, view, "Create a feature flag")
	assert.Contains(t, view, "Verify") // node-server injects in place -> verify step listed
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

func TestWizard_Back_ReturnsToPreviousStep(t *testing.T) {
	cases := []struct{ from, want wizardStep }{
		{stepPlan, stepSelectSDK},
		{stepSelectSDK, stepSelectEnvironment},
		{stepSelectEnvironment, stepSelectProject},
	}
	for _, c := range cases {
		m := wizardModel{step: c.from}
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyLeft})
		assert.Equal(t, c.want, next.(wizardModel).step)
	}
}

func TestWizard_Esc_Quits(t *testing.T) {
	m := wizardModel{step: stepSelectSDK}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.True(t, next.(wizardModel).quitting)
	assert.NotNil(t, cmd)
}

func TestSDKItem_Title_MarksManualInstall(t *testing.T) {
	assert.Contains(t, sdkItem{id: "java-server-sdk", name: "Java"}.Title(), "manual install")
	assert.Equal(t, "Node.js", sdkItem{id: "node-server", name: "Node.js"}.Title())
}

func TestWizard_Done_InstallFailed_ShowsManualCommand(t *testing.T) {
	m := wizardModel{
		step:            stepDone,
		width:           80,
		height:          30,
		flagKey:         "my-new-flag",
		selectedProject: "default",
		installResult:   &setup.InstallResult{SDKID: "ruby-server-sdk", Command: "gem install launchdarkly-server-sdk", Failed: true},
		initResult:      &setup.InitResult{SDKID: "ruby-server-sdk", FilePath: "app.rb", Success: true},
	}

	v := m.View()
	assert.Contains(t, v, "Manual install needed")
	assert.Contains(t, v, "gem install launchdarkly-server-sdk")
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

func TestWizard_Plan_ExistingEntryPoint_SaysAdd(t *testing.T) {
	m := wizardModel{
		step:            stepPlan,
		selectedProject: "default",
		selectedEnv:     "test",
		detectResult: &setup.DetectResult{
			SDKID:            "node-server",
			EntryPoint:       "src/index.js",
			EntryPointExists: true,
		},
		width:  80,
		height: 30,
	}

	view := m.planView()
	assert.Contains(t, view, "Add initialization code to src/index.js")
	assert.NotContains(t, view, "Create src/index.js")
}

// A guessed entry point means we would write a file the project does not load, so
// the plan has to say so while the user can still back out.
func TestWizard_Plan_MissingEntryPoint_SaysCreate(t *testing.T) {
	m := wizardModel{
		step:            stepPlan,
		selectedProject: "default",
		selectedEnv:     "test",
		detectResult: &setup.DetectResult{
			SDKID:            "node-server",
			EntryPoint:       "instrumentation.ts",
			EntryPointExists: false,
		},
		width:  80,
		height: 30,
	}

	view := m.planView()
	assert.Contains(t, view, "Create instrumentation.ts")
	assert.Contains(t, view, "no entry file found")
	assert.NotContains(t, view, "Add initialization code to")
}

// The SDK screen rebuilds detectResult, and the plan and install steps read it, so
// every detected value has to survive that step — not just the SDK.
func TestWizard_SelectSDK_CarriesDetectionThrough(t *testing.T) {
	m := wizardModel{step: stepDetect, width: 80, height: 30}

	next, _ := m.Update(detectDoneMsg{result: &setup.DetectResult{
		SDKID:            "ruby-server-sdk",
		Language:         "Ruby",
		Framework:        "Rails",
		PackageManager:   "bundle",
		EntryPoint:       "config.ru",
		EntryPointExists: true,
	}})
	m2 := next.(wizardModel)
	require.Equal(t, stepSelectSDK, m2.step)

	next2, _ := m2.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m3 := next2.(wizardModel)
	require.Equal(t, stepPlan, m3.step)

	assert.Equal(t, "bundle", m3.detectResult.PackageManager, "install would fall back to gem install")
	assert.True(t, m3.detectResult.EntryPointExists, "plan would claim it will create an existing file")
	assert.Equal(t, "Rails", m3.detectResult.Framework)
	assert.Equal(t, "config.ru", m3.detectResult.EntryPoint)
}

func TestWizard_SelectSDK_PlanUsesDetectedPackageManager(t *testing.T) {
	m := wizardModel{step: stepDetect, width: 80, height: 30}

	next, _ := m.Update(detectDoneMsg{result: &setup.DetectResult{
		SDKID: "ruby-server-sdk", Language: "Ruby", PackageManager: "bundle",
	}})
	next2, _ := next.(wizardModel).Update(tea.KeyMsg{Type: tea.KeyEnter})
	m3 := next2.(wizardModel)

	assert.Equal(t, "bundle add launchdarkly-server-sdk", m3.planInstallCmd)
}

// selectOtherSDK moves focus to the list of non-detected SDKs and highlights id.
func selectOtherSDK(t *testing.T, m wizardModel, id string) wizardModel {
	t.Helper()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(wizardModel)
	require.Equal(t, 1, m.sdkFocus)
	for i, item := range m.sdkList.Items() {
		if sdk, ok := item.(sdkItem); ok && sdk.id == id {
			m.sdkList.Select(i)
			return m
		}
	}
	t.Fatalf("%s is not in the list of other SDKs", id)
	return m
}

// The detected entry point belongs to the detected language. ruby-server-sdk is
// append-safe, so reusing it would append Ruby to a Node project's index.js.
func TestWizard_OverrideSDK_DoesNotReuseDetectedEntryPoint(t *testing.T) {
	m := wizardModel{step: stepDetect, width: 80, height: 30}
	next, _ := m.Update(detectDoneMsg{result: &setup.DetectResult{
		SDKID:            "node-server",
		Language:         "JavaScript",
		Framework:        "Next.js",
		PackageManager:   "pnpm",
		EntryPoint:       "/proj/index.js",
		EntryPointExists: true,
	}})

	m2 := selectOtherSDK(t, next.(wizardModel), "ruby-server-sdk")
	next2, _ := m2.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m3 := next2.(wizardModel)

	require.Equal(t, stepPlan, m3.step)
	assert.Equal(t, "ruby-server-sdk", m3.detectResult.SDKID)
	assert.NotEqual(t, "/proj/index.js", m3.detectResult.EntryPoint,
		"setup would append Ruby to the Node entry file")
	assert.False(t, m3.detectResult.EntryPointExists,
		"a file we have not found must not be reported as found")
	assert.Contains(t, m3.detectResult.EntryPoint, "main.rb")
	assert.Empty(t, m3.detectResult.Framework, "Next.js does not describe a Ruby project")
	// The package manager describes the project, not the SDK, so it survives.
	assert.Equal(t, "pnpm", m3.detectResult.PackageManager)
}

// SDKs that only ever return a snippet have no file to name.
func TestWizard_OverrideSDK_SnippetOnlySDKHasNoEntryPoint(t *testing.T) {
	m := wizardModel{step: stepDetect, width: 80, height: 30}
	next, _ := m.Update(detectDoneMsg{result: &setup.DetectResult{
		SDKID:            "node-server",
		Language:         "JavaScript",
		EntryPoint:       "/proj/index.js",
		EntryPointExists: true,
	}})

	m2 := selectOtherSDK(t, next.(wizardModel), "go-server-sdk")
	next2, _ := m2.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m3 := next2.(wizardModel)

	assert.Empty(t, m3.detectResult.EntryPoint)
	assert.False(t, setup.InjectsInPlace(m3.detectResult.SDKID))
	assert.Contains(t, m3.View(), "Show initialization code for you to add")
}

// Confirming the detected SDK is not an override, so its entry point stands.
func TestWizard_KeepDetectedSDK_KeepsEntryPoint(t *testing.T) {
	m := wizardModel{step: stepDetect, width: 80, height: 30}
	next, _ := m.Update(detectDoneMsg{result: &setup.DetectResult{
		SDKID:            "node-server",
		Language:         "JavaScript",
		Framework:        "Next.js",
		EntryPoint:       "/proj/instrumentation.ts",
		EntryPointExists: true,
	}})

	next2, _ := next.(wizardModel).Update(tea.KeyMsg{Type: tea.KeyEnter})
	m3 := next2.(wizardModel)

	assert.Equal(t, "/proj/instrumentation.ts", m3.detectResult.EntryPoint)
	assert.True(t, m3.detectResult.EntryPointExists)
	assert.Equal(t, "Next.js", m3.detectResult.Framework)
}

// overrideToSDK runs detection, switches to id, and returns the model on the plan
// screen.
func overrideToSDK(t *testing.T, detected *setup.DetectResult, id string) wizardModel {
	t.Helper()
	m := wizardModel{step: stepDetect, width: 80, height: 30}
	next, _ := m.Update(detectDoneMsg{result: detected})
	m2 := selectOtherSDK(t, next.(wizardModel), id)
	next2, _ := m2.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m3 := next2.(wizardModel)
	require.Equal(t, stepPlan, m3.step)
	return m3
}

// Injection appends to a file that already exists, so the plan must not offer to
// create one. Promising to create and then appending edits a file the user did not
// agree to have touched.
func TestWizard_OverrideSDK_DefaultEntryPointAlreadyPresent(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.rb"), []byte("puts 1\n"), 0600))
	t.Chdir(dir)

	m := overrideToSDK(t, &setup.DetectResult{
		SDKID: "node-server", Language: "JavaScript",
		EntryPoint: filepath.Join(dir, "index.js"), EntryPointExists: true,
	}, "ruby-server-sdk")

	assert.Equal(t, filepath.Join(dir, "main.rb"), m.detectResult.EntryPoint)
	assert.True(t, m.detectResult.EntryPointExists)
	view := m.View()
	assert.Contains(t, view, "Add initialization code to")
	assert.NotContains(t, view, "no entry file found")
}

func TestWizard_OverrideSDK_DefaultEntryPointMissing(t *testing.T) {
	t.Chdir(t.TempDir())

	m := overrideToSDK(t, &setup.DetectResult{
		SDKID: "node-server", Language: "JavaScript",
		EntryPoint: "index.js", EntryPointExists: true,
	}, "ruby-server-sdk")

	assert.False(t, m.detectResult.EntryPointExists)
	assert.Contains(t, m.View(), "no entry file found")
}

func TestWizard_Done_DeclinedInstall_ShowsReasonWithoutCommand(t *testing.T) {
	m := wizardModel{
		step:            stepDone,
		width:           78,
		selectedProject: "default",
		flagKey:         "my-new-flag",
		installResult: &setup.InstallResult{
			SDKID:         "dotnet-server-sdk",
			Package:       "LaunchDarkly.ServerSdk",
			Failed:        true,
			FailureReason: "found 2 projects in this solution",
		},
	}

	v := m.View()
	assert.Contains(t, v, "Manual install needed")
	assert.Contains(t, v, "found 2 projects in this solution")
	// No command to offer, so the screen must not render an empty code block or
	// promise one.
	assert.NotContains(t, v, "Install it yourself with")
}

func TestWizard_Done_FailedInstall_ShowsCommand(t *testing.T) {
	m := wizardModel{
		step:            stepDone,
		width:           78,
		selectedProject: "default",
		flagKey:         "my-new-flag",
		installResult: &setup.InstallResult{
			SDKID:         "ruby-server-sdk",
			Command:       "gem install launchdarkly-server-sdk",
			Failed:        true,
			FailureReason: "permission denied",
		},
	}

	v := m.View()
	assert.Contains(t, v, "Install it yourself with")
	assert.Contains(t, v, "gem install launchdarkly-server-sdk")
	assert.Contains(t, v, "permission denied")
}
