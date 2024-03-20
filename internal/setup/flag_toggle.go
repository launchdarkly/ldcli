package setup

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type flagToggleModel struct {
	enabled bool
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
		case key.Matches(msg, keys.Enter):
			m.enabled = !m.enabled
		}
	}

	return m, nil
}

func (m flagToggleModel) View() string {
	toggle := "OFF"
	bgColor := "#646a73"
	var furtherInstructions string
	if m.enabled {
		bgColor = "#3d9c51"
		toggle = "ON"
		furtherInstructions = "\n\nCheck your [browser|application logs] to see the change!"
	}

	var toggleStyle = lipgloss.NewStyle().
		Background(lipgloss.Color(bgColor)).
		Padding(0, 1).
		MarginRight(1)

	title := "Toggle your feature flag!"
	flagKey := "my-flag-key"

	return title + "\n\n" + toggleStyle.Render(toggle) + flagKey + furtherInstructions
}
