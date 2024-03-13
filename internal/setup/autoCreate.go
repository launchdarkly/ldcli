package setup

// A simple example that shows how to retrieve a value from a Bubble Tea
// program after the Bubble Tea has exited.

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

var choices = []string{"Yes", "No"}
var title = "Do you want to get started with our recommended project, environment, and flag?"

type autoCreateModel struct {
	cursor int
	choice string
}

func NewAutoCreate() autoCreateModel {
	return autoCreateModel{}
}

func (m autoCreateModel) Init() tea.Cmd {
	return nil
}

func (m autoCreateModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			return m, tea.Quit

		case "enter":
			// Send the choice on the channel and exit.
			m.choice = choices[m.cursor]
			return m, tea.Quit

		case "down", "j":
			m.cursor++
			if m.cursor >= len(choices) {
				m.cursor = 0
			}

		case "up", "k":
			m.cursor--
			if m.cursor < 0 {
				m.cursor = len(choices) - 1
			}
		}
	}

	return m, nil
}

func (m autoCreateModel) View() string {
	s := strings.Builder{}
	s.WriteString(title + "\n\n")

	for i := 0; i < len(choices); i++ {
		if m.cursor == i {
			s.WriteString("(•) ")
		} else {
			s.WriteString("( ) ")
		}
		s.WriteString(choices[i])
		s.WriteString("\n")
	}
	s.WriteString("\n(press q to quit)\n")

	return s.String()
}
