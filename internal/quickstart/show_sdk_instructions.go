package quickstart

import (
	"fmt"
	"io"
	"ldcli/internal/errors"
	"net/http"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/wordwrap"
)

const instructionsURL = "https://raw.githubusercontent.com/launchdarkly/hello-%s/main/README.md"

type showSDKInstructionsModel struct {
	instructions string
	sdk          string
	width        int
}

func NewShowSDKInstructionsModel() tea.Model {
	return showSDKInstructionsModel{}
}

func (m showSDKInstructionsModel) Init() tea.Cmd {
	// send command to make request?
	return nil
}

func (m showSDKInstructionsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case fetchSDKInstructionsMsg:
		url := fmt.Sprintf(instructionsURL, msg.canonicalName)
		c := &http.Client{
			Timeout: 5 * time.Second,
		}
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return m, sendErr(err)
		}
		resp, err := c.Do(req)
		if err != nil {
			return m, sendErr(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return m, sendErr(err)
		}

		if resp.StatusCode != 200 {
			return m, sendErr(errors.NewError(fmt.Sprintf("could not find %s SDK instructions", msg.name)))
		}

		m.sdk = msg.name
		m.instructions = string(body)
	}

	return m, nil
}

func (m showSDKInstructionsModel) View() string {
	style := lipgloss.NewStyle().Border(lipgloss.NormalBorder(), true, false)
	md, err := m.renderMarkdown()
	if err != nil {
		return fmt.Sprintf("error rendering instructions: %s", err)
	}

	return wordwrap.String(
		fmt.Sprintf(
			"Set up your application. Here are the steps to incorporate the LaunchDarkly %s SDK into your code.\n%s",
			m.sdk,
			style.Render(md),
		),
		m.width,
	)
}

func (m showSDKInstructionsModel) renderMarkdown() (string, error) {
	out, err := glamour.Render(m.instructions, "auto")
	if err != nil {
		return "", err
	}

	return out, nil
}