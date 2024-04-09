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
	"ldcli/internal/sdks"
)

const (
	viewportWidth  = 80
	viewportHeight = 30
)

type showSDKInstructionsModel struct {
	accessToken   string
	baseUri       string
	canonicalName string
	displayName   string
	flagKey       string
	help          help.Model
	helpKeys      keyMap
	instructions  string
	sdkKey        string
	spinner       spinner.Model
	url           string
	viewport      viewport.Model
}

func NewShowSDKInstructionsModel(
	accessToken string,
	baseUri string,
	canonicalName string,
	displayName string,
	url string,
	flagKey string,
) tea.Model {
	s := spinner.New()
	s.Spinner = spinner.Points

	vp := viewport.New(viewportWidth, viewportHeight)
	vp.Style = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		PaddingRight(2)
	vp.MouseWheelEnabled = true

	return showSDKInstructionsModel{
		accessToken:   accessToken,
		baseUri:       baseUri,
		canonicalName: canonicalName,
		displayName:   displayName,
		flagKey:       flagKey,
		help:          help.New(),
		helpKeys: keyMap{
			Back: BindingBack,
			Quit: BindingQuit,
		},
		spinner:  s,
		url:      url,
		viewport: vp,
	}
}

func (m showSDKInstructionsModel) Init() tea.Cmd {
	return tea.Sequence(
		m.spinner.Tick,
		sendFetchSDKInstructionsMsg(m.url),
		sendFetchEnv(m.accessToken, m.baseUri, defaultEnvKey, defaultProjKey),
	)
}

func (m showSDKInstructionsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, pressableKeys.Enter):
			// TODO: only if all data are fetched?
			cmd = sendShowToggleFlagMsg()
		default:
			m.viewport, cmd = m.viewport.Update(msg)
		}
	case tea.MouseMsg:
		m.viewport, cmd = m.viewport.Update(msg)
	case fetchedSDKInstructions:
		m.instructions = sdks.ReplaceFlagKey(string(msg.instructions), m.flagKey)
	case fetchedEnv:
		m.sdkKey = msg.sdkKey
		m.instructions = sdks.ReplaceSDKKey(string(m.instructions), msg.sdkKey)
		md, err := m.renderMarkdown()
		if err != nil {
			return m, sendErr(err)
		}
		m.viewport.SetContent(md)
	case spinner.TickMsg:
		m.spinner, cmd = m.spinner.Update(msg)
	}

	return m, cmd
}

func (m showSDKInstructionsModel) View() string {
	if m.instructions == "" || m.sdkKey == "" {
		return m.spinner.View() + fmt.Sprintf(" Fetching %s SDK instructions...", m.displayName)
	}

	instructions := fmt.Sprintf("Set up your application in your Default project & Test environment.\n\nHere are the steps to incorporate the LaunchDarkly %s SDK into your code. You should have everything you need to get started, including the flag from the previous step and your SDK key from your Test environment already embedded in the code!\n", m.displayName)

	return instructions + m.viewport.View() + "\n(press enter to continue)" + footerView(m.help.View(m.helpKeys), nil)
}

func (m showSDKInstructionsModel) renderMarkdown() (string, error) {
	renderer, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(viewportWidth),
	)
	if err != nil {
		return "", err
	}

	out, err := renderer.Render(m.instructions)
	if err != nil {
		return out, err
	}

	return out, nil
}
