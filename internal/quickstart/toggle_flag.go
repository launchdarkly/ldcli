package quickstart

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type toggleFlagModel struct {
	flagKey string
}

func NewToggleFlagModel(flagKey string) tea.Model {
	return toggleFlagModel{
		flagKey: flagKey,
	}
}

func (m toggleFlagModel) Init() tea.Cmd {
	return nil
}

func (m toggleFlagModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Tab):
			// TODO: toggle flag
		}
	}

	return m, cmd
}

func (m toggleFlagModel) View() string {
	title := "Toggle your feature flag (press tab)"
	toggle := "OFF"

	toggleStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("#646a73")).
		Padding(0, 1).
		MarginRight(1)

	return title + "\n\n" + toggleStyle.Render(toggle) + m.flagKey
}
