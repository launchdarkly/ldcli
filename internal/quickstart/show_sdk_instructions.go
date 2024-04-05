package quickstart

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/wordwrap"

	"ldcli/internal/sdks"
)

type showSDKInstructionsModel struct {
	accessToken   string
	baseUri       string
	canonicalName string
	displayName   string
	flagKey       string
	instructions  string
	sdkKey        string
	spinner       spinner.Model
	url           string
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

	return showSDKInstructionsModel{
		accessToken:   accessToken,
		baseUri:       baseUri,
		canonicalName: canonicalName,
		displayName:   displayName,
		flagKey:       flagKey,
		spinner:       s,
		url:           url,
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
		case key.Matches(msg, keys.Enter):
			// TODO: only if all data are fetched?
			cmd = sendShowToggleFlagMsg()
		}
	case fetchedSDKInstructions:
		m.instructions = sdks.ReplaceFlagKey(string(msg.instructions), m.flagKey)
	case fetchedEnv:
		m.sdkKey = msg.sdkKey
		m.instructions = sdks.ReplaceSDKKey(string(m.instructions), msg.sdkKey)
	case spinner.TickMsg:
		m.spinner, cmd = m.spinner.Update(msg)
	}

	return m, cmd
}

func (m showSDKInstructionsModel) View() string {
	style := lipgloss.NewStyle().Border(lipgloss.NormalBorder(), true, false)
	md, err := m.renderMarkdown()
	if err != nil {
		return fmt.Sprintf("error rendering instructions: %s", err)
	}

	if m.instructions == "" || m.sdkKey == "" {
		return m.spinner.View() + fmt.Sprintf(" Fetching %s SDK instructions...", m.displayName)
	}

	return wordwrap.String(
		fmt.Sprintf(
			"Set up your application in your Default project & Test environment.\n\nHere are the steps to incorporate the LaunchDarkly %s SDK into your code. You should have everything you need to get started, including the flag from the previous step and your SDK key from your Test environment already embedded in the code!\n%s\n\n (press enter to continue)",
			m.displayName,
			style.Render(md),
		),
		0,
	)
}

func (m showSDKInstructionsModel) renderMarkdown() (string, error) {
	out, err := glamour.Render(m.instructions, "auto")
	if err != nil {
		return "", err
	}

	return out, nil
}
