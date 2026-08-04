package setup

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/launchdarkly/ldcli/internal/setup"
)

func (m wizardModel) fetchProjects() tea.Cmd {
	return func() tea.Msg {
		ps, err := m.svc.ListProjects(m.auth)
		if err != nil {
			return wizardErrMsg{err: err}
		}
		projects := make([]projectItem, len(ps))
		for i, p := range ps {
			projects[i] = projectItem{key: p.Key, name: p.Name}
		}
		return projectsFetchedMsg{projects: projects}
	}
}

func (m wizardModel) fetchEnvironments() tea.Cmd {
	return func() tea.Msg {
		es, err := m.svc.ListEnvironments(m.auth, m.selectedProject)
		if err != nil {
			return wizardErrMsg{err: err}
		}
		envs := make([]envItem, len(es))
		for i, e := range es {
			envs[i] = envItem{key: e.Key, name: e.Name}
		}
		return envsFetchedMsg{environments: envs}
	}
}

func (m wizardModel) fetchEnvDetails() tea.Cmd {
	return func() tea.Msg {
		keys, err := m.svc.EnvKeys(m.auth, m.selectedProject, m.selectedEnv)
		if err != nil {
			return wizardErrMsg{err: err}
		}
		return envDetailsFetchedMsg{
			sdkKey:       keys.SDKKey,
			clientSideID: keys.ClientSideID,
			mobileKey:    keys.MobileKey,
		}
	}
}

func (m wizardModel) runDetect() tea.Cmd {
	return func() tea.Msg {
		dir, err := os.Getwd()
		if err != nil {
			return wizardErrMsg{err: err}
		}
		result, err := m.svc.Detect(dir)
		if err != nil {
			return detectFailedMsg{}
		}
		return detectDoneMsg{result: result}
	}
}

func (m wizardModel) runInstall() tea.Cmd {
	return func() tea.Msg {
		dir, err := os.Getwd()
		if err != nil {
			return wizardErrMsg{err: err}
		}
		result, err := m.svc.Install(dir, m.detectResult)
		if err != nil {
			// Don't dead-end the interactive flow on a failed auto-install (e.g.
			// Ruby gem perms, no network): surface the command to run by hand.
			args, _ := setup.InstallArgs(m.detectResult.SDKID, m.detectResult.PackageManager)
			return installDoneMsg{result: &setup.InstallResult{
				SDKID:         m.detectResult.SDKID,
				Command:       strings.Join(args, " "),
				Failed:        true,
				FailureReason: err.Error(),
			}}
		}
		return installDoneMsg{result: result}
	}
}

func (m wizardModel) runCreateFlag() tea.Cmd {
	return func() tea.Msg {
		key, err := m.svc.CreateFlag(m.auth, m.selectedProject, "my-new-flag", "My New Flag")
		if err != nil {
			return wizardErrMsg{err: err}
		}
		return flagCreatedMsg{key: key}
	}
}

func (m wizardModel) runInit() tea.Cmd {
	return func() tea.Msg {
		cfg := setup.InitConfig{
			SDKKey:       m.sdkKey,
			ClientSideID: m.clientSideID,
			MobileKey:    m.mobileKey,
			FlagKey:      m.flagKey,
		}
		result, err := m.svc.Inject(m.detectResult.SDKID, m.detectResult.EntryPoint, cfg)
		if err != nil {
			return wizardErrMsg{err: err}
		}
		return initDoneMsg{result: result}
	}
}

func (m wizardModel) runVerify() tea.Cmd {
	return func() tea.Msg {
		result, err := m.svc.Verify(m.auth, m.selectedProject, m.selectedEnv, m.detectResult.SDKID)
		if err != nil {
			return wizardErrMsg{err: err}
		}
		return verifyDoneMsg{result: result}
	}
}

// copyableContent returns the code the current screen is asking the user to copy,
// along with the word the hint uses for it. A screen can show both an install command
// and a snippet; the snippet is the one that has to be pasted verbatim, so it wins.
// Returns false when the screen has nothing to copy.
func (m wizardModel) copyableContent() (content, label string, ok bool) {
	if m.step != stepDone {
		return "", "", false
	}
	if m.initResult != nil && !m.initResult.Success && m.initResult.Snippet != "" {
		return m.initResult.Snippet, "snippet", true
	}
	if m.installResult != nil && m.installResult.Failed && m.installResult.Command != "" {
		return m.installResult.Command, "command", true
	}
	return "", "", false
}

// copyToClipboard writes an OSC 52 sequence, which asks the terminal to put the
// content on the system clipboard. The wizard renders code inside a bordered block
// and runs in the alternate screen, so selecting it with the mouse picks up the
// gutter characters and the snippet is gone from scrollback once the wizard exits.
// Terminals that do not implement OSC 52 ignore the sequence.
func (m wizardModel) copyToClipboard(content string) tea.Cmd {
	return func() tea.Msg {
		fmt.Fprint(m.clipboard, ansi.SetSystemClipboard(content))
		return nil
	}
}
