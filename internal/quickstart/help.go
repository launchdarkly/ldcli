package quickstart

import "github.com/charmbracelet/bubbles/key"

type listKeyMap struct {
	Back          key.Binding
	CloseFullHelp key.Binding
	CursorDown    key.Binding
	CursorUp      key.Binding
	GoToEnd       key.Binding
	GoToStart     key.Binding
	NextPage      key.Binding
	PrevPage      key.Binding
	Quit          key.Binding
	ShowFullHelp  key.Binding
}

func (k listKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.CursorUp, k.CursorDown, k.PrevPage, k.NextPage},
		{k.Back, k.Quit, k.CloseFullHelp},
	}
}

func (k listKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Back, k.Quit, k.ShowFullHelp}
}

type keyMap struct {
	Back  key.Binding
	Enter key.Binding
	Quit  key.Binding
	Tab   key.Binding
	// Help  key.Binding
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		// {k.Up, k.Down, k.Left, k.Right}, // first column
		// {k.Back, k.Quit}, // second column
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Back, k.Quit}
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
	// Help: key.NewBinding(
	// 	key.WithKeys("?"),
	// 	key.WithHelp("?", "help"),
	// ),
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

func chooseSDKModelKeys() listKeyMap {
	return listKeyMap{
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
