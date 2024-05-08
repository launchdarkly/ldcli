package quickstart

import (
	"fmt"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"ldcli/internal/environments"
	"ldcli/internal/sdks"
)

const (
	viewportWidth  = 80
	viewportHeight = 30
)

type environment struct {
	sdkKey       string
	mobileKey    string
	clientSideId string
}

type showSDKInstructionsModel struct {
	accessToken        string
	baseUri            string
	canonicalName      string
	displayName        string
	environment        *environment
	environmentsClient environments.Client
	err                error
	flagKey            string
	help               help.Model
	helpKeys           keyMap
	instructions       string
	spinner            spinner.Model
	url                string
	viewport           viewport.Model
}

func NewShowSDKInstructionsModel(
	environmentsClient environments.Client,
	accessToken string,
	baseUri string,
	canonicalName string,
	displayName string,
	url string,
	flagKey string,
	environment *environment,
) tea.Model {
	s := spinner.New()
	s.Spinner = spinner.Points

	vp := viewport.New(viewportWidth, viewportHeight)
	vp.Style = lipgloss.NewStyle().
		BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(lipgloss.Color("62")).
		BorderTop(true).
		BorderBottom(true).
		PaddingRight(2)

	h := help.New()

	return showSDKInstructionsModel{
		accessToken:        accessToken,
		baseUri:            baseUri,
		canonicalName:      canonicalName,
		displayName:        displayName,
		environmentsClient: environmentsClient,
		environment:        environment,
		flagKey:            flagKey,
		help:               h,
		helpKeys: keyMap{
			Back:       BindingBack,
			CursorDown: BindingCursorDown,
			CursorUp:   BindingCursorUp,
			Quit:       BindingQuit,
		},
		spinner:  s,
		url:      url,
		viewport: vp,
	}
}

// Init sends commands when the model is created that will:
// show a spinner while SDK instructions are prepared
// fetch SDK instructions
// fetch the environment to get values to interpolate into the instructions
func (m showSDKInstructionsModel) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spinner.Tick, readSDKInstructions(m.canonicalName)}

	if m.environment == nil {
		cmds = append(cmds, fetchEnv(m.environmentsClient, m.accessToken, m.baseUri, defaultEnvKey, defaultProjKey))
	}

	return tea.Sequence(cmds...)
}

func (m showSDKInstructionsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, pressableKeys.Enter):
			// TODO: only if all data are fetched?
			cmd = showToggleFlag()
		default:
			m.viewport, cmd = m.viewport.Update(msg)
		}
	case fetchedSDKInstructionsMsg:
		m.instructions = string(msg.instructions)
		if m.environment != nil {
			md, err := m.renderMarkdown()
			if err != nil {
				return m, sendErrMsg(err)
			}
			m.viewport.SetContent(md)
		}
	case fetchedEnvMsg:
		m.environment = &msg.environment
		md, err := m.renderMarkdown()
		if err != nil {
			return m, sendErrMsg(err)
		}
		m.viewport.SetContent(md)
	case spinner.TickMsg:
		m.spinner, cmd = m.spinner.Update(msg)
	case errMsg:
		m.err = msg.err
	}

	return m, cmd
}

func (m showSDKInstructionsModel) View() string {
	if m.err != nil {
		return footerView(m.help.View(m.helpKeys), m.err)
	}

	if m.instructions == "" || m.environment == nil {
		return m.spinner.View() + fmt.Sprintf(" Fetching %s SDK instructions...\n", m.displayName) + footerView(m.help.View(m.helpKeys), nil)
	}

	m.help.ShowAll = true

	instructions := fmt.Sprintf(`
Here are the steps to set up a test app to see feature flagging in action
using the %s SDK in your Default project & Test environment.

You should have everything you need to get started, including the flag from
the previous step and your environment key from your Test environment already
embedded in the code!

Open a new terminal window to get started.

If you want to skip ahead, the final code is available in our GitHub repository:
%s
`,
		m.displayName,
		m.url,
	)
	return instructions + m.viewport.View() + "\n(press enter to continue)" + footerView(m.help.View(m.helpKeys), nil)
}

func (m showSDKInstructionsModel) renderMarkdown() (string, error) {
	instructions := sdks.ReplaceFlagKey(m.instructions, m.flagKey)
	instructions = sdks.ReplaceSDKKeys(
		instructions,
		m.environment.sdkKey,
		m.environment.clientSideId,
		m.environment.mobileKey,
	)

	renderer, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
	)
	if err != nil {
		return "", err
	}

	out, err := renderer.Render(instructions)
	if err != nil {
		return out, err
	}

	return out, nil
}
