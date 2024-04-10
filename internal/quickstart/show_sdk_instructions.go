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
	accessToken         string
	baseUri             string
	canonicalName       string
	displayName         string
	flagKey             string
	help                help.Model
	helpKeys            keyMap
	instructions        string
	hasInstructionsFile bool // TODO: remove when we have all instructions saved
	sdkKey              string
	spinner             spinner.Model
	url                 string
	viewport            viewport.Model
}

func NewShowSDKInstructionsModel(
	accessToken string,
	baseUri string,
	canonicalName string,
	displayName string,
	url string,
	flagKey string,
	hasInstructionsFile bool,
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
	h.ShowAll = true

	return showSDKInstructionsModel{
		accessToken:   accessToken,
		baseUri:       baseUri,
		canonicalName: canonicalName,
		displayName:   displayName,
		flagKey:       flagKey,
		help:          h,
		helpKeys: keyMap{
			Back:       BindingBack,
			CursorDown: BindingCursorDown,
			CursorUp:   BindingCursorUp,
			Quit:       BindingQuit,
		},
		spinner:             s,
		url:                 url,
		viewport:            vp,
		hasInstructionsFile: hasInstructionsFile,
	}
}

func (m showSDKInstructionsModel) Init() tea.Cmd {
	// to remove when we have all instruction files loaded
	instructionsCmd := sendFetchSDKInstructionsMsg(m.url)
	if m.hasInstructionsFile {
		instructionsCmd = sendReadSDKInstructionsMsg(m.canonicalName)
	}

	return tea.Sequence(
		m.spinner.Tick,
		instructionsCmd,
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
	case fetchedSDKInstructions:
		m.instructions = sdks.ReplaceFlagKey(string(msg.instructions), m.flagKey)
	case fetchedEnv:
		m.sdkKey = msg.sdkKey
		m.instructions = sdks.ReplaceSDKKeys(string(m.instructions), msg.sdkKey, msg.clientSideId)
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
