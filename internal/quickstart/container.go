package quickstart

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"ldcli/internal/flags"
)

// step is an identifier for each step in the quick-start flow.
type step int

const (
	createFlag step = iota
)

// ContainerModel is a high level container model that controls the nested models which each
// represent a step in the quick-start flow.
type ContainerModel struct {
	currentStep step
	flagsClient flags.Client
	quitting    bool
	steps       []tea.Model
}

func NewContainerModel(flagsClient flags.Client) tea.Model {
	return ContainerModel{
		currentStep: createFlag,
		flagsClient: flagsClient,
		steps:       []tea.Model{},
	}
}

func (m ContainerModel) Init() tea.Cmd {
	return nil
}

func (m ContainerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Quit):
			m.quitting = true

			return m, tea.Quit
		default:
		}
	}

	return m, nil
}

func (m ContainerModel) View() string {
	if m.quitting {
		return ""
	}

	return "container"
}

type keyMap struct {
	Quit key.Binding
}

var keys = keyMap{
	Quit: key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("q", "quit"),
	),
}
