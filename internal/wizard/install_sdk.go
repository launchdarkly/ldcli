package wizard

import (
	"fmt"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

type installSDKModel struct {
	sdk            sdkDetail
	packageManager string
	workDir        string
	err            error
	installing     bool
	done           bool
	skipped        bool
	output         string
	spinner        spinner.Model
	help           help.Model
	helpKeys       keyMap
}

func NewInstallSDKModel(sdk sdkDetail, packageManager, workDir string) tea.Model {
	s := spinner.New()
	s.Spinner = spinner.Points

	return installSDKModel{
		sdk:            sdk,
		packageManager: packageManager,
		workDir:        workDir,
		installing:     true,
		spinner:        s,
		help:           help.New(),
		helpKeys: keyMap{
			Quit: BindingQuit,
		},
	}
}

func (m installSDKModel) Init() tea.Cmd {
	args := InstallArgs(m.sdk.id, m.packageManager)
	return tea.Batch(
		m.spinner.Tick,
		runInstall(args, m.workDir),
	)
}

func (m installSDKModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case installedSDKMsg:
		m.installing = false
		m.done = true
		m.output = msg.output
	case installSkippedMsg:
		m.installing = false
		m.skipped = true
	case installErrMsg:
		m.installing = false
		m.err = msg.err
	case spinner.TickMsg:
		m.spinner, cmd = m.spinner.Update(msg)
	case tea.KeyMsg:
		if !m.installing && (m.done || m.skipped || m.err != nil) {
			if key.Matches(msg, pressableKeys.Enter) {
				cmd = continueFromInstall()
			}
		}
	}
	return m, cmd
}

func (m installSDKModel) View() string {
	if m.installing {
		return m.spinner.View() + fmt.Sprintf(" Installing %s SDK...\n", m.sdk.displayName) +
			footerView(m.help.View(m.helpKeys), nil)
	}

	if m.skipped {
		return fmt.Sprintf(
			"The %s SDK requires manual installation.\n\n"+
				"Add the dependency to your project, then press enter to continue.\n",
			m.sdk.displayName,
		) + footerView(m.help.View(m.helpKeys), nil)
	}

	if m.err != nil {
		return fmt.Sprintf(
			"SDK installation encountered an error (you can continue anyway):\n%s\n\n"+
				"Press enter to continue.\n",
			m.err.Error(),
		) + footerView(m.help.View(m.helpKeys), nil)
	}

	return "SDK installed successfully. Press enter to continue.\n" +
		footerView(m.help.View(m.helpKeys), nil)
}

// continueFromInstall is a sentinel message that tells the container to advance.
type continueFromInstallMsg struct{}

func continueFromInstall() tea.Cmd {
	return func() tea.Msg { return continueFromInstallMsg{} }
}
