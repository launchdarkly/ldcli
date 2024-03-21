package setup

import (
	"bytes"
	"fmt"
	"net/http"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type flagToggleModel struct {
	enabled bool
	flagKey string
	logType string
	//err     error
}

func NewFlagToggle() flagToggleModel {
	return flagToggleModel{}
}

func (m flagToggleModel) Init() tea.Cmd {
	return nil
}

func (m flagToggleModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Toggle):
			m.enabled = !m.enabled

			// uncomment to send PATCH
			//m.err = m.toggleFlag()
		}
	}

	return m, nil
}

func (m flagToggleModel) View() string {
	var furtherInstructions string
	title := "Toggle your feature flag (press tab)"
	toggle := "OFF"
	bgColor := "#646a73"
	margin := 1
	if m.enabled {
		bgColor = "#3d9c51"
		furtherInstructions = fmt.Sprintf("\n\nCheck your %s to see the change!", m.logType)
		margin = 2
		toggle = "ON"
	}

	toggleStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(bgColor)).
		Padding(0, 1).
		MarginRight(margin)

	return title + "\n\n" + toggleStyle.Render(toggle) + m.flagKey + furtherInstructions
}

func (m flagToggleModel) toggleFlag() error {
	url := fmt.Sprintf("http://localhost/api/v2/flags/default/%s", m.flagKey)
	c := &http.Client{
		Timeout: 10 * time.Second,
	}

	toggleInstruction := "turnFlagOn"
	if !m.enabled {
		toggleInstruction = "turnFlagOff"
	}

	body := fmt.Sprintf(`{
		  "environmentKey": "production",
		  "instructions": [ { "kind": %q } ]
		}`, toggleInstruction)

	req, _ := http.NewRequest("PATCH", url, bytes.NewBufferString(body))
	req.Header.Add("Authorization", "") // add token here
	req.Header.Add("Content-type", "application/json; domain-model=launchdarkly.semanticpatch")

	res, err := c.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	return nil
}
