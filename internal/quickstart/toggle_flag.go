package quickstart

import tea "github.com/charmbracelet/bubbletea"

type toggleFlagModel struct {
	enabled bool
	flagKey string
}

func NewToggleFlagModel() toggleFlagModel { return toggleFlagModel{} }

func (m toggleFlagModel) Init() tea.Cmd { return nil }

func (m toggleFlagModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m, nil
}

func (m toggleFlagModel) View() string {
	return "toggle the flag"
}
