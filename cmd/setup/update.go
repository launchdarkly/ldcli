package setup

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/launchdarkly/ldcli/internal/setup"
)

func (m wizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "q", "esc":
			if m.isFiltering() {
				break // let the list receive 'q' / clear its filter
			}
			m.quitting = true
			return m, tea.Quit
		case "left", "h":
			if m.isFiltering() {
				break // let the list receive the key as filter input
			}
			return m.handleBack()
		case "c":
			if m.isFiltering() {
				break // let the list receive the key as filter input
			}
			content, _, ok := m.copyableContent()
			if !ok {
				break
			}
			return m, m.copyToClipboard(content)
		case "enter":
			return m.handleEnter()
		}

	case copiedMsg:
		m.copyState = copyDone
		if msg.viaTerminal {
			m.copyState = copyRequested
		}
		return m, nil

	case projectsFetchedMsg:
		m.projects = msg.projects
		items := make([]list.Item, len(msg.projects))
		for i, p := range msg.projects {
			items[i] = p
		}
		delegate := list.NewDefaultDelegate()
		m.projectList = list.New(items, delegate, m.width, m.height-4)
		m.projectList.Title = "Select a project:"
		m.projectList.SetShowStatusBar(false)
		return m, nil

	case envsFetchedMsg:
		m.environments = msg.environments
		items := make([]list.Item, len(msg.environments))
		for i, e := range msg.environments {
			items[i] = e
		}
		delegate := list.NewDefaultDelegate()
		m.envList = list.New(items, delegate, m.width, m.height-4)
		m.envList.Title = "Select an environment:"
		m.envList.SetShowStatusBar(false)
		return m, nil

	case envDetailsFetchedMsg:
		m.sdkKey = msg.sdkKey
		m.clientSideID = msg.clientSideID
		m.mobileKey = msg.mobileKey
		// Detection was kicked off at launch; go straight to the SDK screen if
		// it's already done, otherwise show a brief wait until it lands.
		if m.detectComplete {
			m.enterSDKStep()
		} else {
			m.step = stepDetect
		}
		return m, nil

	case detectFailedMsg:
		m.detectComplete = true
		m.detectedSDKID = ""
		if m.step == stepDetect {
			m.enterSDKStep()
		}
		return m, nil

	case detectDoneMsg:
		m.detectComplete = true
		m.detectedSDKID = msg.result.SDKID
		m.detected = msg.result
		if m.step == stepDetect {
			m.enterSDKStep()
		}
		return m, nil

	case installDoneMsg:
		m.installResult = msg.result
		m.step = stepCreateFlag
		return m, m.runCreateFlag()

	case flagCreatedMsg:
		m.flagKey = msg.key
		m.step = stepInit
		return m, m.runInit()

	case initDoneMsg:
		m.initResult = msg.result
		// Skip the live verify if init didn't inject runnable code, or if the SDK
		// wasn't actually installed (auto-install failed) — the app can't connect.
		if !msg.result.Success || (m.installResult != nil && m.installResult.Failed) {
			m.step = stepDone
			return m, nil
		}
		m.step = stepWaitForApp
		return m, nil

	case verifyDoneMsg:
		m.verifyResult = msg.result
		m.step = stepDone
		return m, nil

	case wizardErrMsg:
		m.err = msg.err
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	// delegate to list models
	var cmd tea.Cmd
	switch m.step {
	case stepSelectProject:
		if len(m.projects) > 0 {
			m.projectList, cmd = m.projectList.Update(msg)
		}
	case stepSelectEnvironment:
		if len(m.environments) > 0 {
			m.envList, cmd = m.envList.Update(msg)
		}
	case stepSelectSDK:
		// Two panels when a detected SDK is shown: the detected panel (focus 0)
		// and the list of other SDKs (focus 1). Arrows move focus between them.
		if m.detectedSDK != nil {
			if km, ok := msg.(tea.KeyMsg); ok {
				switch km.String() {
				case "down", "tab", "j":
					if m.sdkFocus == 0 {
						m.sdkFocus = 1
						m.sdkList.SetDelegate(sdkDelegate(true))
						return m, nil
					}
				case "up", "shift+tab", "k":
					if m.sdkFocus == 1 && m.sdkList.Index() == 0 {
						m.sdkFocus = 0
						m.sdkList.SetDelegate(sdkDelegate(false))
						return m, nil
					}
				}
			}
			if m.sdkFocus == 1 && m.sdkList.Items() != nil {
				m.sdkList, cmd = m.sdkList.Update(msg)
			}
		} else if m.sdkList.Items() != nil {
			m.sdkList, cmd = m.sdkList.Update(msg)
		}
	}
	return m, cmd
}

// isFiltering reports whether the current step's list is in filter-typing mode,
// so keys like esc/q are left for the list instead of triggering back/quit.
func (m wizardModel) isFiltering() bool {
	switch m.step {
	case stepSelectProject:
		return m.projectList.FilterState() == list.Filtering
	case stepSelectEnvironment:
		return m.envList.FilterState() == list.Filtering
	case stepSelectSDK:
		return m.sdkList.FilterState() == list.Filtering
	}
	return false
}

// enterSDKStep builds the SDK-selection screen from the cached one-time
// detection result and switches to it. Rebuilding the list is cheap and uses
// the current width; detection itself is never re-run.
func (m *wizardModel) enterSDKStep() {
	if id := m.detectedSDKID; id != "" {
		if det, ok := findKnownSDK(id); ok {
			m.detectedSDK = &det
			m.sdkFocus = 0
			m.sdkList = m.newSDKList(sdkItemsExcept(det.id), "Other SDKs:", false)
			m.step = stepSelectSDK
			return
		}
	}
	m.detectedSDK = nil
	m.sdkFocus = 1
	m.sdkList = m.newSDKList(sdkItemsExcept(""), "Select your SDK:", true)
	m.step = stepSelectSDK
}

// handleBack returns to the previous selection so the user can change the
// project, environment, or SDK.
func (m wizardModel) handleBack() (tea.Model, tea.Cmd) {
	switch m.step {
	case stepSelectEnvironment:
		m.step = stepSelectProject
	case stepSelectSDK:
		m.step = stepSelectEnvironment
	case stepPlan:
		m.step = stepSelectSDK
	}
	return m, nil
}

func (m wizardModel) handleEnter() (tea.Model, tea.Cmd) {
	switch m.step {
	case stepSelectProject:
		if len(m.projects) == 0 {
			return m, nil
		}
		selected, ok := m.projectList.SelectedItem().(projectItem)
		if !ok {
			return m, nil
		}
		m.selectedProject = selected.key
		m.step = stepSelectEnvironment
		return m, m.fetchEnvironments()

	case stepSelectEnvironment:
		if len(m.environments) == 0 {
			return m, nil
		}
		selected, ok := m.envList.SelectedItem().(envItem)
		if !ok {
			return m, nil
		}
		m.selectedEnv = selected.key
		return m, m.fetchEnvDetails()

	case stepSelectSDK:
		var chosen sdkItem
		if m.detectedSDK != nil && m.sdkFocus == 0 {
			chosen = *m.detectedSDK
		} else {
			selected, ok := m.sdkList.SelectedItem().(sdkItem)
			if !ok {
				return m, nil
			}
			chosen = selected
		}
		result := setup.DetectResult{}
		if m.detected != nil {
			result = *m.detected
		}
		result.SDKID = chosen.id
		result.Language = chosen.language
		if chosen.id != m.detectedSDKID {
			// The detected entry point belongs to the language we detected, not the
			// one the user picked. An append-safe SDK would otherwise write Ruby into
			// a Node project's index.js, so start over from that SDK's own default.
			result.Framework = ""
			result.EntryPoint = setup.DefaultEntryPoint(chosen.id)
			result.EntryPointExists = false
			if dir, err := os.Getwd(); err == nil && result.EntryPoint != "" {
				result.EntryPoint = filepath.Join(dir, result.EntryPoint)
				// Injection appends to a file that is already there, so report the
				// default as found when it exists. Claiming otherwise would promise
				// to create a file and then quietly append to the user's.
				if info, err := os.Stat(result.EntryPoint); err == nil && !info.IsDir() {
					result.EntryPointExists = true
				}
			}
		}
		m.detectResult = &result
		// Compute the plan preview shown before any action is taken.
		args, _ := setup.InstallArgs(chosen.id, result.PackageManager)
		m.planInstallCmd = strings.Join(args, " ")
		if dir, err := os.Getwd(); err == nil {
			m.planAlready = setup.IsInstalled(dir, chosen.id)
		}
		m.step = stepPlan
		return m, nil

	case stepPlan:
		m.step = stepInstall
		return m, m.runInstall()

	case stepWaitForApp:
		m.step = stepVerify
		return m, m.runVerify()
	}
	return m, nil
}

// quitHint is appended to terminal (done) screens so the user knows how to exit.
