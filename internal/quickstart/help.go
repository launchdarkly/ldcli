package quickstart

import (
	"github.com/charmbracelet/bubbles/key"
)

var (
	BindingBack = key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "back"),
	)
	BindingCursorUp = key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	)
	BindingCursorDown = key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	)
	BindingPrevPage = key.NewBinding(
		key.WithKeys("left", "h", "pgup", "b", "u"),
		key.WithHelp("←/h/pgup", "prev page"),
	)
	BindingNextPage = key.NewBinding(
		key.WithKeys("right", "l", "pgdown", "f", "d"),
		key.WithHelp("→/l/pgdn", "next page"),
	)
	BindingGoToStart = key.NewBinding(
		key.WithKeys("home", "g"),
		key.WithHelp("g/home", "go to start"),
	)
	BindingGoToEnd = key.NewBinding(
		key.WithKeys("end", "G"),
		key.WithHelp("G/end", "go to end"),
	)
	BindingShowFullHelp = key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "more"),
	)
	BindingCloseFullHelp = key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "close help"),
	)
	BindingQuit = key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("ctrl+c", "quit"),
	)
)

// keyMap defines all the possible key presses we would respond to
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

// pressableKeys are the possible key presses we support for all steps.
// We don't necessarily want to show these in the help text, but we want to handle them when
// pressed.
var pressableKeys = keyMap{
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

// footerView shows any error messages and help text.
func footerView(helpView string, err error) string {
	var errView string
	spacer := "\n\n\n"
	if err != nil {
		spacer = "\n\n"
		errView = "\n" + err.Error()
	}

	return errView + spacer + helpView
}
