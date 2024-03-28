package quickstart

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

type chooseSDKModel struct{}

func NewChooseSDKModel() tea.Model {
	return chooseSDKModel{}
}

func (p chooseSDKModel) Init() tea.Cmd {
	return nil
}

func (m chooseSDKModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Enter):
		}
	}

	return m, cmd
}

func (m chooseSDKModel) View() string {
	return "Choose SDK"
}
