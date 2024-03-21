package setup

import (
	"fmt"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type flagToggleModel struct {
	enabled bool
	flagKey string
	logType string
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
