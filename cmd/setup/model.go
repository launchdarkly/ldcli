package setup

import (
	"io"
	"os"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/errors"
	"github.com/launchdarkly/ldcli/internal/setup"
)

// copyState records how the visible snippet was copied, so the view can confirm a
// clipboard write outright but only claim to have asked when the terminal did it.
type copyState int

const (
	copyNone      copyState = iota
	copyDone                // written to the OS clipboard
	copyRequested           // handed to the terminal over OSC 52, which cannot confirm
)

type wizardStep int

const (
	stepSelectProject wizardStep = iota
	stepSelectEnvironment
	stepDetect
	stepSelectSDK
	stepPlan
	stepInstall
	stepCreateFlag
	stepInit
	stepWaitForApp
	stepVerify
	stepDone
)

type wizardModel struct {
	analyticsTrackerFn analytics.TrackerFn
	svc                setup.Service
	auth               setup.Auth

	step    wizardStep
	spinner spinner.Model
	err     error
	width   int
	height  int

	// data gathered through the flow
	projects     []projectItem
	environments []envItem
	projectList  list.Model
	envList      list.Model
	sdkList      list.Model

	selectedProject string
	selectedEnv     string
	sdkKey          string
	clientSideID    string
	mobileKey       string

	detectComplete bool   // detection (run once at launch) has finished
	detectedSDKID  string // detected SDK id, cached from the one-time detection ("" if none)
	// detected is the unmodified result of the one-time detection. detectResult is
	// what the rest of the flow acts on: the same values with the SDK the user
	// actually chose. Keeping the whole struct means added fields reach the later
	// steps without every one having to be copied by hand.
	detected       *setup.DetectResult
	detectResult   *setup.DetectResult
	detectedSDK    *sdkItem // the auto-detected SDK, shown in its own panel; nil if detection failed
	sdkFocus       int      // on the SDK screen: 0 = detected panel, 1 = the list of other SDKs
	planInstallCmd string   // install command previewed on the plan screen
	planAlready    bool     // whether the SDK is already installed (previewed on the plan screen)
	installResult  *setup.InstallResult
	flagKey        string
	initResult     *setup.InitResult
	verifyResult   *setup.VerifyResult

	// nativeCopy puts content on the operating system's clipboard, and clipboard
	// receives the OSC 52 sequence used when that is not available. Both are fields
	// so tests can drive either path without a real clipboard or terminal.
	nativeCopy func(string) error
	clipboard  io.Writer
	copyState  copyState

	quitting bool
}

type sdkItem struct {
	id       string
	language string
	name     string
}

func (s sdkItem) Title() string {
	if setup.RequiresManualInstall(s.id) {
		return s.name + " (manual install)"
	}
	return s.name
}
func (s sdkItem) Description() string { return s.language }
func (s sdkItem) FilterValue() string { return s.name }

type projectItem struct {
	key  string
	name string
}

func (p projectItem) Title() string       { return p.name }
func (p projectItem) Description() string { return p.key }
func (p projectItem) FilterValue() string { return p.name }

type envItem struct {
	key  string
	name string
}

func (e envItem) Title() string       { return e.name }
func (e envItem) Description() string { return e.key }
func (e envItem) FilterValue() string { return e.name }

// messages
type projectsFetchedMsg struct{ projects []projectItem }
type envsFetchedMsg struct{ environments []envItem }
type envDetailsFetchedMsg struct {
	sdkKey       string
	clientSideID string
	mobileKey    string
}
type detectDoneMsg struct{ result *setup.DetectResult }
type detectFailedMsg struct{}
type installDoneMsg struct{ result *setup.InstallResult }
type flagCreatedMsg struct{ key string }
type initDoneMsg struct{ result *setup.InitResult }
type copiedMsg struct{ viaTerminal bool }
type verifyDoneMsg struct{ result *setup.VerifyResult }
type wizardErrMsg struct{ err error }

func runSetupWizard(
	analyticsTrackerFn analytics.TrackerFn,
	svc setup.Service,
) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		// Pre-flight: the wizard's first action is an authenticated API call, so
		// bail early with clear guidance rather than dumping a raw 401 mid-TUI.
		if viper.GetString(cliflags.AccessTokenFlag) == "" {
			return errors.NewError("It looks like you're not logged in yet.\n\nRun `ldcli login` to authenticate, then run `ldcli setup` again.\n(Or pass --access-token, or set LD_ACCESS_TOKEN.)")
		}

		s := spinner.New()
		s.Spinner = spinner.Dot

		m := wizardModel{
			analyticsTrackerFn: analyticsTrackerFn,
			svc:                svc,
			auth: setup.Auth{
				AccessToken: viper.GetString(cliflags.AccessTokenFlag),
				BaseURI:     viper.GetString(cliflags.BaseURIFlag),
			},
			step:       stepSelectProject,
			spinner:    s,
			clipboard:  os.Stdout,
			nativeCopy: clipboard.WriteAll,
		}

		p := tea.NewProgram(m, tea.WithAltScreen())
		_, err := p.Run()
		return err
	}
}

func (m wizardModel) Init() tea.Cmd {
	// Detect the project once, up front, so navigating the flow never re-runs it.
	return tea.Batch(m.spinner.Tick, m.fetchProjects(), m.runDetect())
}
