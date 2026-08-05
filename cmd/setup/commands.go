package setup

import (
	"fmt"
	"io"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"

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

// copyToClipboard puts the content on the clipboard, preferring the operating
// system's own clipboard because it works in every terminal and reports whether it
// succeeded. OSC 52 is the fallback: it asks the terminal to do the copying, which is
// what works over SSH, where the OS clipboard belongs to the wrong machine. Not every
// terminal implements OSC 52 and support cannot be queried, so a copy that goes that
// route is reported as a request rather than a result.
func (m wizardModel) copyToClipboard(content string) tea.Cmd {
	return func() tea.Msg {
		// Over SSH the OS clipboard is the one on the machine running the code, not
		// the one the user pastes into, and it can succeed there — so a remote
		// session has to go to the terminal even though the local path would work.
		if !m.remoteSession {
			if err := m.nativeCopy(content); err == nil {
				return copiedMsg{viaTerminal: false}
			}
		}
		fmt.Fprint(m.clipboard, ansi.SetSystemClipboard(content))
		return copiedMsg{viaTerminal: true}
	}
}

// terminalWriter returns the writer to send OSC 52 to. It must not be stdout:
// Bubble Tea owns stdout for frame rendering while the wizard runs, so a
// sequence written there from a command goroutine can land in the middle of a
// frame. Stderr is preferred because it reaches the same terminal without that
// contention, but it may be redirected to a file or pipe, in which case the
// sequence would be swallowed instead of reaching the terminal — so fall back to
// the controlling terminal itself.
func terminalWriter() io.Writer {
	if term.IsTerminal(int(os.Stderr.Fd())) {
		return os.Stderr
	}
	if tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0); err == nil {
		return tty
	}
	return os.Stderr
}

// isRemoteSession reports whether the CLI is running over SSH. sshd sets these for
// the session it owns, so they distinguish "the clipboard here is the user's" from
// "the user's clipboard is on the other end of the connection".
func isRemoteSession() bool {
	for _, name := range []string{"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY"} {
		if os.Getenv(name) != "" {
			return true
		}
	}
	return false
}
