package setup

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/errors"
	"github.com/launchdarkly/ldcli/internal/flags"
	"github.com/launchdarkly/ldcli/internal/resources"
	"github.com/launchdarkly/ldcli/internal/setup"
)

type wizardStep int

const (
	stepSelectProject wizardStep = iota
	stepSelectEnvironment
	stepDetect
	stepSelectSDK
	stepInstall
	stepCreateFlag
	stepInit
	stepWaitForApp
	stepVerify
	stepDone
)

type wizardModel struct {
	analyticsTrackerFn analytics.TrackerFn
	resourcesClient    resources.Client
	flagsClient        flags.Client
	detector           setup.Detector
	installer          setup.Installer

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

	detectedEntryPoint string
	detectResult       *setup.DetectResult
	detectedSDK        *sdkItem // the auto-detected SDK, shown in its own panel; nil if detection failed
	sdkFocus           int      // on the SDK screen: 0 = detected panel, 1 = the list of other SDKs
	flagKey            string
	initResult         *setup.InitResult
	verifyResult       *setup.VerifyResult

	quitting bool
}

type sdkItem struct {
	id       string
	language string
	name     string
}

func (s sdkItem) Title() string       { return s.name }
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
type verifyDoneMsg struct{ result *setup.VerifyResult }
type wizardErrMsg struct{ err error }

func runSetupWizard(
	analyticsTrackerFn analytics.TrackerFn,
	resourcesClient resources.Client,
	flagsClient flags.Client,
	detector setup.Detector,
	installer setup.Installer,
) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		// Pre-flight: the wizard's first action is an authenticated API call, so
		// bail early with clear guidance rather than dumping a raw 401 mid-TUI.
		if viper.GetString(cliflags.AccessTokenFlag) == "" {
			return errors.NewError("Hey — it looks like you're not logged in yet. Run `ldcli login` to authenticate, then run `ldcli setup` again. (Or pass --access-token, or set LD_ACCESS_TOKEN.)")
		}

		s := spinner.New()
		s.Spinner = spinner.Dot

		m := wizardModel{
			analyticsTrackerFn: analyticsTrackerFn,
			resourcesClient:    resourcesClient,
			flagsClient:        flagsClient,
			detector:           detector,
			installer:          installer,
			step:               stepSelectProject,
			spinner:            s,
		}

		p := tea.NewProgram(m, tea.WithAltScreen())
		_, err := p.Run()
		return err
	}
}

func (m wizardModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.fetchProjects())
}

func (m wizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.quitting = true
			return m, tea.Quit
		case "enter":
			return m.handleEnter()
		}

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
		m.step = stepDetect
		return m, m.runDetect()

	case detectFailedMsg:
		m.detectedSDK = nil
		m.sdkFocus = 1
		m.sdkList = m.newSDKList(sdkItemsExcept(""), "Select your SDK:")
		m.step = stepSelectSDK
		return m, nil

	case detectDoneMsg:
		m.detectedEntryPoint = msg.result.EntryPoint
		if det, ok := findKnownSDK(msg.result.SDKID); ok {
			m.detectedSDK = &det
			m.sdkFocus = 0
			m.sdkList = m.newSDKList(sdkItemsExcept(det.id), "Other SDKs:")
		} else {
			m.detectedSDK = nil
			m.sdkFocus = 1
			m.sdkList = m.newSDKList(sdkItemsExcept(""), "Select your SDK:")
		}
		m.step = stepSelectSDK
		return m, nil

	case installDoneMsg:
		m.step = stepCreateFlag
		return m, m.runCreateFlag()

	case flagCreatedMsg:
		m.flagKey = msg.key
		m.step = stepInit
		return m, m.runInit()

	case initDoneMsg:
		m.initResult = msg.result
		if !msg.result.Success {
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
						return m, nil
					}
				case "up", "shift+tab", "k":
					if m.sdkFocus == 1 && m.sdkList.Index() == 0 {
						m.sdkFocus = 0
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
		m.detectResult = &setup.DetectResult{
			SDKID:      chosen.id,
			Language:   chosen.language,
			EntryPoint: m.detectedEntryPoint,
		}
		m.step = stepInstall
		return m, m.runInstall()

	case stepWaitForApp:
		m.step = stepVerify
		return m, m.runVerify()
	}
	return m, nil
}

// quitHint is appended to terminal (done) screens so the user knows how to exit.
var quitHint = "\n" + lipgloss.NewStyle().Faint(true).Render("Press q to quit.") + "\n"

func (m wizardModel) View() string {
	if m.quitting {
		return ""
	}

	titleStyle := lipgloss.NewStyle().Bold(true).MarginBottom(1)

	if m.err != nil {
		return titleStyle.Render("Error") + "\n\n" + m.err.Error() + "\n\nPress ctrl+c to quit."
	}

	switch m.step {
	case stepSelectProject:
		if len(m.projects) == 0 {
			return m.spinner.View() + " Loading projects..."
		}
		return m.projectList.View()

	case stepSelectEnvironment:
		if len(m.environments) == 0 {
			return m.spinner.View() + " Loading environments..."
		}
		return m.envList.View()

	case stepDetect:
		return m.spinner.View() + " Detecting project type..."

	case stepSelectSDK:
		return m.sdkSelectView()

	case stepInstall:
		return m.spinner.View() + " Installing SDK..."

	case stepCreateFlag:
		return m.spinner.View() + " Creating feature flag..."

	case stepInit:
		return m.spinner.View() + " Injecting initialization code..."

	case stepWaitForApp:
		return titleStyle.Render("Start your application") + "\n\n" +
			"SDK initialization code has been injected into:\n" +
			"  " + m.initResult.FilePath + "\n\n" +
			"Please start your application now, then press Enter to verify the connection.\n"

	case stepVerify:
		return m.spinner.View() + " Waiting for SDK to connect..."

	case stepDone:
		if m.initResult != nil && !m.initResult.Success {
			body := titleStyle.Render("Manual SDK setup required") + "\n\n"
			if m.initResult.Snippet != "" {
				body += fmt.Sprintf("Add the following %s initialization code to %s:\n\n%s\n\n",
					m.initResult.SDKID, m.initResult.FilePath, m.initResult.Snippet)
			} else {
				body += fmt.Sprintf("No initialization template is available for %s.\n", m.initResult.SDKID)
			}
			return body +
				fmt.Sprintf("Follow the setup guide at: %s\n\n", m.initResult.DocsURL) +
				fmt.Sprintf("Flag %q has been created in project %q.\n", m.flagKey, m.selectedProject) +
				"Once you've initialized the SDK manually, your flag will be ready to use.\n" +
				quitHint
		}
		if m.verifyResult != nil && m.verifyResult.Active && m.detectResult != nil {
			return titleStyle.Render("Setup complete!") + "\n\n" +
				fmt.Sprintf("Your %s SDK is connected to LaunchDarkly.\n", m.detectResult.SDKID) +
				fmt.Sprintf("Flag %q is ready to use.\n\n", m.flagKey) +
				fmt.Sprintf("You can now toggle your flag at https://app.launchdarkly.com/projects/%s/flags/%s/targeting?env=%s\n", m.selectedProject, m.flagKey, m.selectedEnv) +
				quitHint
		}
		return titleStyle.Render("Verification timed out") + "\n\n" +
			"The SDK did not report as active within the timeout period.\n" +
			"Make sure your application is running and try again.\n" +
			quitHint
	}

	return ""
}

// findKnownSDK returns the sdkItem for the given SDK id, if it is one we know.
func findKnownSDK(id string) (sdkItem, bool) {
	for _, sdk := range setup.KnownSDKs {
		if sdk.ID == id {
			return sdkItem{id: sdk.ID, language: sdk.Language, name: sdk.Name}, true
		}
	}
	return sdkItem{}, false
}

// sdkItemsExcept returns all known SDKs as list items, omitting the given id.
func sdkItemsExcept(exclude string) []list.Item {
	items := make([]list.Item, 0, len(setup.KnownSDKs))
	for _, sdk := range setup.KnownSDKs {
		if sdk.ID == exclude {
			continue
		}
		items = append(items, sdkItem{id: sdk.ID, language: sdk.Language, name: sdk.Name})
	}
	return items
}

// newSDKList builds the list model for the SDK selection screen.
func (m wizardModel) newSDKList(items []list.Item, title string) list.Model {
	h := m.height - 12
	if h < 3 {
		h = 3
	}
	l := list.New(items, list.NewDefaultDelegate(), m.width, h)
	l.Title = title
	l.SetShowStatusBar(false)
	return l
}

// sdkSelectView renders the SDK selection screen. When an SDK was auto-detected
// it shows two areas: an "identified" panel on top and the list of other SDKs
// below; the focused area is highlighted. When detection failed, only the list
// is shown.
func (m wizardModel) sdkSelectView() string {
	if m.detectedSDK == nil {
		return m.sdkList.View()
	}

	focused := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("6")).Padding(0, 1)
	blurred := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240")).Padding(0, 1)
	faint := lipgloss.NewStyle().Faint(true)

	panelStyle, listStyle := blurred, blurred
	if m.sdkFocus == 0 {
		panelStyle = focused
	} else {
		listStyle = focused
	}

	// Show a purple pointer on the detected SDK when the panel is focused, so the
	// auto-detected choice is highlighted the same way the list highlights its cursor.
	pointer, line := "  ", fmt.Sprintf("%s  (%s)", m.detectedSDK.name, m.detectedSDK.language)
	if m.sdkFocus == 0 {
		sel := lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
		pointer, line = sel.Render("❯ "), sel.Render(line)
	}
	panel := panelStyle.Render(
		lipgloss.NewStyle().Bold(true).Render("We identified this as your SDK") + "\n" +
			pointer + line + "\n" +
			faint.Render("Press Enter to use it"))

	listBox := listStyle.Render(m.sdkList.View())

	hint := faint.Render("↑/↓ move · Enter select · q quit")
	return panel + "\n\n" + listBox + "\n" + hint
}

// Commands that perform async work

func (m wizardModel) fetchProjects() tea.Cmd {
	return func() tea.Msg {
		path, _ := url.JoinPath(
			viper.GetString(cliflags.BaseURIFlag),
			"api/v2/projects",
		)
		res, err := m.resourcesClient.MakeRequest(
			viper.GetString(cliflags.AccessTokenFlag),
			"GET", path, "application/json", nil, nil, false,
		)
		if err != nil {
			return wizardErrMsg{err: err}
		}

		var resp struct {
			Items []struct {
				Key  string `json:"key"`
				Name string `json:"name"`
			} `json:"items"`
		}
		if err := json.Unmarshal(res, &resp); err != nil {
			return wizardErrMsg{err: fmt.Errorf("parsing projects: %w", err)}
		}

		projects := make([]projectItem, len(resp.Items))
		for i, item := range resp.Items {
			projects[i] = projectItem{key: item.Key, name: item.Name}
		}
		return projectsFetchedMsg{projects: projects}
	}
}

func (m wizardModel) fetchEnvironments() tea.Cmd {
	return func() tea.Msg {
		path, _ := url.JoinPath(
			viper.GetString(cliflags.BaseURIFlag),
			"api/v2/projects", m.selectedProject, "environments",
		)
		res, err := m.resourcesClient.MakeRequest(
			viper.GetString(cliflags.AccessTokenFlag),
			"GET", path, "application/json", nil, nil, false,
		)
		if err != nil {
			return wizardErrMsg{err: err}
		}

		var resp struct {
			Items []struct {
				Key  string `json:"key"`
				Name string `json:"name"`
			} `json:"items"`
		}
		if err := json.Unmarshal(res, &resp); err != nil {
			return wizardErrMsg{err: fmt.Errorf("parsing environments: %w", err)}
		}

		envs := make([]envItem, len(resp.Items))
		for i, item := range resp.Items {
			envs[i] = envItem{key: item.Key, name: item.Name}
		}
		return envsFetchedMsg{environments: envs}
	}
}

func (m wizardModel) fetchEnvDetails() tea.Cmd {
	return func() tea.Msg {
		path, _ := url.JoinPath(
			viper.GetString(cliflags.BaseURIFlag),
			"api/v2/projects", m.selectedProject, "environments", m.selectedEnv,
		)
		res, err := m.resourcesClient.MakeRequest(
			viper.GetString(cliflags.AccessTokenFlag),
			"GET", path, "application/json", nil, nil, false,
		)
		if err != nil {
			return wizardErrMsg{err: err}
		}

		var resp struct {
			SDKKey       string `json:"apiKey"`
			ClientSideId string `json:"_id"`
			MobileKey    string `json:"mobileKey"`
		}
		if err := json.Unmarshal(res, &resp); err != nil {
			return wizardErrMsg{err: fmt.Errorf("parsing environment details: %w", err)}
		}

		return envDetailsFetchedMsg{
			sdkKey:       resp.SDKKey,
			clientSideID: resp.ClientSideId,
			mobileKey:    resp.MobileKey,
		}
	}
}

func (m wizardModel) runDetect() tea.Cmd {
	return func() tea.Msg {
		dir, err := os.Getwd()
		if err != nil {
			return wizardErrMsg{err: err}
		}
		result, err := m.detector.Detect(dir)
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
		result, err := m.installer.Install(dir, m.detectResult)
		if err != nil {
			return wizardErrMsg{err: err}
		}
		return installDoneMsg{result: result}
	}
}

func (m wizardModel) runCreateFlag() tea.Cmd {
	return func() tea.Msg {
		flagKey := "my-new-flag"
		flagName := "My New Flag"

		_, err := m.flagsClient.Create(
			context.Background(),
			viper.GetString(cliflags.AccessTokenFlag),
			viper.GetString(cliflags.BaseURIFlag),
			flagName,
			flagKey,
			m.selectedProject,
		)
		if err != nil {
			// If flag already exists (conflict), continue using it
			if jsonErr, parseErr := parseJSONError(err); parseErr == nil && jsonErr.Code == "conflict" {
				return flagCreatedMsg{key: flagKey}
			}
			return wizardErrMsg{err: err}
		}
		return flagCreatedMsg{key: flagKey}
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
		initializer := setup.Initializer{}
		result, err := initializer.InjectIntoFile(m.detectResult.SDKID, m.detectResult.EntryPoint, cfg)
		if err != nil {
			return wizardErrMsg{err: err}
		}
		return initDoneMsg{result: result}
	}
}

func (m wizardModel) runVerify() tea.Cmd {
	return func() tea.Msg {
		verifier := setup.DefaultVerifier(m.resourcesClient)
		result, err := verifier.Verify(
			viper.GetString(cliflags.AccessTokenFlag),
			viper.GetString(cliflags.BaseURIFlag),
			m.selectedProject,
			m.selectedEnv,
		)
		if err != nil {
			return wizardErrMsg{err: err}
		}
		return verifyDoneMsg{result: result}
	}
}

type jsonError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func parseJSONError(err error) (*jsonError, error) {
	var je jsonError
	if parseErr := json.Unmarshal([]byte(err.Error()), &je); parseErr != nil {
		return nil, parseErr
	}
	return &je, nil
}
