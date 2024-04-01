package quickstart

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type toggleFlagModel struct {
	enabled bool
	flagKey string
}

func NewToggleFlagModel() toggleFlagModel { return toggleFlagModel{} }

func (m toggleFlagModel) Init() tea.Cmd { return nil }

func (m toggleFlagModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Toggle):
			m.enabled = !m.enabled
		}
	case updateToggleFlagModelMsg:
		m.flagKey = msg.flagKey
	}
	return m, nil
}

func (m toggleFlagModel) View() string {
	title := "Toggle your feature flag (press tab)"
	toggle := "OFF"
	bgColor := "#646a73"
	margin := 1
	if m.enabled {
		bgColor = "#3d9c51"
		margin = 2
		toggle = "ON"
	}

	toggleStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(bgColor)).
		Padding(0, 1).
		MarginRight(margin)

	return title + "\n\n" + toggleStyle.Render(toggle) + m.flagKey
}
