package quickstart

import (
	"fmt"
	"ldcli/internal/sdks"
	"log"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/wordwrap"
)

const instructionsURL = "https://raw.githubusercontent.com/launchdarkly/hello-%s/main/README.md"

type showSDKInstructionsModel struct {
	instructions string
	sdk          string

	canonicalName string
	flagKey       string
	url           string
	sdkKey        string
	accessToken   string
	baseUri       string
}

func NewShowSDKInstructionsModel(accessToken string, baseUri string, canonicalName string, url string, flagKey string) tea.Model {
	return showSDKInstructionsModel{
		canonicalName: canonicalName,
		url:           url,
		flagKey:       flagKey,
		accessToken:   accessToken,
		baseUri:       baseUri,
	}
}

func (m showSDKInstructionsModel) Init() tea.Cmd {
	log.Println("showSDKInstructionsModel Init")
	return tea.Sequence(
		sendFetchSDKInstructionsMsg(m.url),
		sendFetchEnv(m.accessToken, m.baseUri, "test", "default"),
	)
}

func (m showSDKInstructionsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case fetchedSDKInstructions:
		log.Println("showSDKInstructionsModel received fetchedSDKInstructions")
		log.Println(string(msg.instructions))
		m.instructions = sdks.ReplaceFlagKey(string(msg.instructions), m.flagKey)

		// case fetchEnv:
		// case
	case fetchedEnv:
		m.sdkKey = msg.sdkKey
		m.instructions = sdks.ReplaceSDKKey(string(m.instructions), msg.sdkKey)
	default:
		log.Println("showSDKInstructionsModel default", msg)
	}

	return m, nil
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
			"Set up your application. Here are the steps to incorporate the LaunchDarkly %s SDK into your code.\n%s",
			m.sdk,
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
