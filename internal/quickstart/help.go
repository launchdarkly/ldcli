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

func createFlagModelKeys() keyMap {
	return keyMap{
		Back: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "back"),
		),
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "quit"),
		),
	}
}

func chooseSDKModelKeys() keyMap {
	return keyMap{
		Back: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "back"),
		),
		CursorUp: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "up"),
		),
		CursorDown: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "down"),
		),
		PrevPage: key.NewBinding(
			key.WithKeys("left", "h", "pgup", "b", "u"),
			key.WithHelp("←/h/pgup", "prev page"),
		),
		NextPage: key.NewBinding(
			key.WithKeys("right", "l", "pgdown", "f", "d"),
			key.WithHelp("→/l/pgdn", "next page"),
		),
		GoToStart: key.NewBinding(
			key.WithKeys("home", "g"),
			key.WithHelp("g/home", "go to start"),
		),
		GoToEnd: key.NewBinding(
			key.WithKeys("end", "G"),
			key.WithHelp("G/end", "go to end"),
		),
		ShowFullHelp: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "more"),
		),
		CloseFullHelp: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "close help"),
		),
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "quit"),
		),
	}
}

func showSDKInstructionsModelKeys() keyMap {
	return keyMap{
		Back: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "back"),
		),
		// CursorUp: key.NewBinding(
		// 	key.WithKeys("up", "k"),
		// 	key.WithHelp("↑/k", "up"),
		// ),
		// CursorDown: key.NewBinding(
		// 	key.WithKeys("down", "j"),
		// 	key.WithHelp("↓/j", "down"),
		// ),
		// GoToStart: key.NewBinding(
		// 	key.WithKeys("home", "g"),
		// 	key.WithHelp("g/home", "go to start"),
		// ),
		// GoToEnd: key.NewBinding(
		// 	key.WithKeys("end", "G"),
		// 	key.WithHelp("G/end", "go to end"),
		// ),
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "quit"),
		),
	}
}

func toggleFlagModelKeys() keyMap {
	return keyMap{
		Back: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "back"),
		),
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "quit"),
		),
	}
}
