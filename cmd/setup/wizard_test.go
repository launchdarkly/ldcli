package setup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
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

func TestWizard_NoProjects_ShowsEmptyStateNotSpinner(t *testing.T) {
	m := wizardModel{step: stepSelectProject, width: 78, height: 24, spinner: spinner.New()}

	// Before the fetch lands, the spinner is right.
	assert.Contains(t, m.View(), "Loading projects")

	updated, _ := m.Update(projectsFetchedMsg{projects: nil})
	v := updated.(wizardModel).View()

	assert.NotContains(t, v, "Loading projects")
	assert.Contains(t, v, "No projects available")
}

func TestWizard_NoEnvironments_ShowsEmptyStateNotSpinner(t *testing.T) {
	m := wizardModel{step: stepSelectEnvironment, width: 78, height: 24, spinner: spinner.New(), selectedProject: "my-proj"}

	assert.Contains(t, m.View(), "Loading environments")

	updated, _ := m.Update(envsFetchedMsg{project: "my-proj", environments: nil})
	v := updated.(wizardModel).View()

	assert.NotContains(t, v, "Loading environments")
	assert.Contains(t, v, "No environments available")
	assert.Contains(t, v, "my-proj")
}

// selectProjectAtIndex drives the project list to the given row and presses
// Enter, returning the model with the environment fetch in flight.
func selectProjectAtIndex(t *testing.T, m wizardModel, i int) wizardModel {
	t.Helper()
	m.projectList.Select(i)
	next, _ := m.handleEnter()
	return next.(wizardModel)
}

// wizardWithTwoProjectsAndEnvsFor returns a model that has already selected the
// first project and received its environments.
func wizardWithTwoProjectsAndEnvsFor(t *testing.T, envs []envItem) wizardModel {
	t.Helper()
	m := wizardModel{step: stepSelectProject, width: 78, height: 24, spinner: spinner.New()}
	loaded, _ := m.Update(projectsFetchedMsg{projects: []projectItem{
		{key: "proj-a", name: "A"},
		{key: "proj-b", name: "B"},
	}})
	first := selectProjectAtIndex(t, loaded.(wizardModel), 0)
	require.Equal(t, "proj-a", first.selectedProject)

	withEnvs, _ := first.Update(envsFetchedMsg{project: "proj-a", environments: envs})
	return withEnvs.(wizardModel)
}

func TestWizard_ReselectProject_CannotSelectPreviousProjectsEnvironment(t *testing.T) {
	m := wizardWithTwoProjectsAndEnvsFor(t, []envItem{{key: "a-production", name: "A Production"}})

	back, _ := m.handleBack()
	second := selectProjectAtIndex(t, back.(wizardModel), 1)
	require.Equal(t, "proj-b", second.selectedProject)

	assert.Empty(t, second.environments)
	assert.Empty(t, second.selectedEnv)

	// Enter while the new fetch is in flight must not commit a key from proj-a.
	pressed, _ := second.handleEnter()
	got := pressed.(wizardModel)
	assert.Empty(t, got.selectedEnv)
	assert.Equal(t, stepSelectEnvironment, got.step)
}

func TestWizard_ReselectProject_ShowsSpinnerNotStaleList(t *testing.T) {
	m := wizardWithTwoProjectsAndEnvsFor(t, []envItem{{key: "a-production", name: "A Production"}})
	require.Contains(t, m.View(), "A Production")

	back, _ := m.handleBack()
	second := selectProjectAtIndex(t, back.(wizardModel), 1)

	v := second.View()
	assert.Contains(t, v, "Loading environments")
	assert.NotContains(t, v, "A Production")
	assert.NotContains(t, v, "No environments available")
}

func TestWizard_ReselectProject_AfterEmptyList_ShowsSpinnerNotEmptyState(t *testing.T) {
	m := wizardWithTwoProjectsAndEnvsFor(t, nil)
	require.Contains(t, m.View(), "No environments available")

	back, _ := m.handleBack()
	second := selectProjectAtIndex(t, back.(wizardModel), 1)

	assert.Contains(t, second.View(), "Loading environments")

	// The new project's environments still land normally.
	withEnvs, _ := second.Update(envsFetchedMsg{project: "proj-b", environments: []envItem{{key: "b-production", name: "B Production"}}})
	assert.Contains(t, withEnvs.(wizardModel).View(), "B Production")
}

func TestWizard_ReselectProject_WindowSizeDoesNotPanic(t *testing.T) {
	m := wizardWithTwoProjectsAndEnvsFor(t, []envItem{{key: "a-production", name: "A Production"}})
	back, _ := m.handleBack()
	second := selectProjectAtIndex(t, back.(wizardModel), 1)

	// Resizing with the env list cleared, then again once the fetch lands.
	resized, _ := second.Update(tea.WindowSizeMsg{Width: 120, Height: 50})
	withEnvs, _ := resized.(wizardModel).Update(envsFetchedMsg{project: "proj-b", environments: []envItem{{key: "b-production", name: "B Production"}}})
	got := withEnvs.(wizardModel)
	assert.Equal(t, 120, got.envList.Width())

	again, _ := got.Update(tea.WindowSizeMsg{Width: 60, Height: 30})
	assert.Equal(t, 60, again.(wizardModel).envList.Width())
}

func TestWizard_BackFromSDK_KeepsEnvironmentList(t *testing.T) {
	// Back from the SDK step does not re-fetch, so the env list must survive it.
	m := wizardWithTwoProjectsAndEnvsFor(t, []envItem{{key: "a-production", name: "A Production"}})
	m.step = stepSelectSDK

	back, _ := m.handleBack()
	got := back.(wizardModel)

	assert.Equal(t, stepSelectEnvironment, got.step)
	assert.True(t, got.envsLoaded)
	assert.Contains(t, got.View(), "A Production")
}

func TestWizard_WindowSize_ResizesExistingLists(t *testing.T) {
	m := wizardModel{step: stepSelectProject, width: 40, height: 10, spinner: spinner.New()}
	withList, _ := m.Update(projectsFetchedMsg{projects: []projectItem{{key: "p1", name: "One"}}})

	resized, _ := withList.(wizardModel).Update(tea.WindowSizeMsg{Width: 120, Height: 50})
	got := resized.(wizardModel)

	assert.Equal(t, 120, got.projectList.Width())
	assert.Equal(t, got.listHeight(), got.projectList.Height())
}

func TestWizard_ListHeight_NeverNegativeBeforeWindowSize(t *testing.T) {
	// No WindowSizeMsg yet, so height is still zero and height-4 would be negative.
	m := wizardModel{step: stepSelectProject, spinner: spinner.New()}

	assert.GreaterOrEqual(t, m.listHeight(), 3)

	withList, _ := m.Update(projectsFetchedMsg{projects: []projectItem{{key: "p1", name: "One"}}})
	assert.GreaterOrEqual(t, withList.(wizardModel).projectList.Height(), 3)
}

// envDetailsInFlight returns a model that has selected proj-a/production and is
// waiting on the SDK keys for it.
func envDetailsInFlight(t *testing.T) wizardModel {
	t.Helper()
	m := wizardWithTwoProjectsAndEnvsFor(t, []envItem{{key: "production", name: "Prod"}})
	next, _ := m.handleEnter()
	got := next.(wizardModel)
	require.Equal(t, "production", got.selectedEnv)
	require.Equal(t, stepSelectEnvironment, got.step)
	return got
}

func TestWizard_EnvDetails_LandingAfterBack_IsIgnored(t *testing.T) {
	m := envDetailsInFlight(t)

	// User presses ← before the keys arrive.
	back, _ := m.handleBack()
	m = back.(wizardModel)
	require.Equal(t, stepSelectProject, m.step)

	late, _ := m.Update(envDetailsFetchedMsg{
		project: "proj-a", env: "production",
		sdkKey: "sdk-A", clientSideID: "cs-A", mobileKey: "mob-A",
	})
	got := late.(wizardModel)

	// Must not yank the user into SDK selection with no environment selected.
	assert.Equal(t, stepSelectProject, got.step)
	assert.Empty(t, got.sdkKey)
	assert.Empty(t, got.selectedEnv)
}

func TestWizard_EnvDetails_OutOfOrder_KeepsSelectedEnvsKeys(t *testing.T) {
	m := envDetailsInFlight(t) // production selected, its fetch in flight
	m.detectComplete = true

	// User goes back and selects a different environment before the first lands.
	back, _ := m.handleBack()
	m = back.(wizardModel)
	m = selectProjectAtIndex(t, m, 0)
	withEnvs, _ := m.Update(envsFetchedMsg{project: "proj-a", environments: []envItem{
		{key: "production", name: "Prod"}, {key: "test", name: "Test"},
	}})
	m = withEnvs.(wizardModel)
	m.envList.Select(1) // test
	next, _ := m.handleEnter()
	m = next.(wizardModel)
	require.Equal(t, "test", m.selectedEnv)

	// The superseded production response lands last and must be dropped.
	stale, _ := m.Update(envDetailsFetchedMsg{
		project: "proj-a", env: "production", sdkKey: "sdk-PROD",
	})
	m = stale.(wizardModel)
	assert.Empty(t, m.sdkKey, "production's key must not be adopted while test is selected")

	// test's own response is still accepted.
	fresh, _ := m.Update(envDetailsFetchedMsg{
		project: "proj-a", env: "test", sdkKey: "sdk-TEST",
	})
	assert.Equal(t, "sdk-TEST", fresh.(wizardModel).sdkKey)
}

func TestWizard_EnvDetails_DuplicateOnDoneScreen_IsIgnored(t *testing.T) {
	m := wizardModel{
		step: stepDone, width: 78, spinner: spinner.New(),
		selectedProject: "proj-a", selectedEnv: "production",
		verifyResult: &setup.VerifyResult{Active: true},
		detectResult: &setup.DetectResult{SDKID: "node-server"},
	}

	dup, _ := m.Update(envDetailsFetchedMsg{project: "proj-a", env: "production", sdkKey: "sdk-A"})

	assert.Equal(t, stepDone, dup.(wizardModel).step, "a duplicate must not reopen SDK selection")
}

func TestWizard_EnvsFetched_ForSupersededProject_IsIgnored(t *testing.T) {
	m := wizardWithTwoProjectsAndEnvsFor(t, []envItem{{key: "only-in-a", name: "Only In A"}})

	back, _ := m.handleBack()
	m = selectProjectAtIndex(t, back.(wizardModel), 1)
	require.Equal(t, "proj-b", m.selectedProject)

	// proj-a's in-flight list lands after proj-b was chosen.
	stale, _ := m.Update(envsFetchedMsg{project: "proj-a", environments: []envItem{{key: "only-in-a", name: "Only In A"}}})
	m = stale.(wizardModel)
	assert.Empty(t, m.environments)
	assert.False(t, m.envsLoaded)
	assert.Contains(t, m.View(), "Loading environments")

	// proj-b's own list is accepted.
	fresh, _ := m.Update(envsFetchedMsg{project: "proj-b", environments: []envItem{{key: "b-prod", name: "B Prod"}}})
	assert.Contains(t, fresh.(wizardModel).View(), "B Prod")
}
