package setup

import tea "github.com/charmbracelet/bubbletea"

type OutputValue struct {
	Key   string
	Value string
}
type ViewModelWithTextInput interface {
	tea.Model
	FormFocus() bool
	SetFormFocus(bool) (tea.Model, tea.Cmd)
	InputValue() OutputValue
}
