package quickstart

import (
	"fmt"
	"ldcli/internal/sdks"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/wordwrap"
)

type showSDKInstructionsModel struct {
	accessToken   string
	baseUri       string
	canonicalName string
	displayName   string
	flagKey       string
	instructions  string
	sdkKey        string
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
	return showSDKInstructionsModel{
		accessToken:   accessToken,
		baseUri:       baseUri,
		canonicalName: canonicalName,
		displayName:   displayName,
		flagKey:       flagKey,
		url:           url,
	}
}

func (m showSDKInstructionsModel) Init() tea.Cmd {
	return tea.Sequence(
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
		return "show spinner"
	}

	return wordwrap.String(
		fmt.Sprintf(
			"Set up your application. Here are the steps to incorporate the LaunchDarkly %s SDK into your code.\n%s\n\n (press enter to continue)",
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
