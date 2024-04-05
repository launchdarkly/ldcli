package quickstart

import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
	Back          key.Binding
	CloseFullHelp key.Binding
	CursorDown    key.Binding
	CursorUp      key.Binding
	Enter         key.Binding
	GoToEnd       key.Binding
	GoToStart     key.Binding
	NextPage      key.Binding
	PrevPage      key.Binding
	Quit          key.Binding
	ShowFullHelp  key.Binding
	Tab           key.Binding
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.CursorUp, k.CursorDown, k.PrevPage, k.NextPage},
		{k.Back, k.Quit, k.CloseFullHelp},
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Back, k.Quit, k.ShowFullHelp}
}

var keys = keyMap{
	Back: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "go back"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select"),
	),
	Quit: key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("ctrl+c", "quit"),
	),
	Tab: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "toggle"),
	),
}
