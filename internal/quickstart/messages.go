package quickstart

import tea "github.com/charmbracelet/bubbletea"

type fetchSDKInstructionsMsg struct {
	canonicalName string
	flagKey       string
	name          string
}

type errMsg struct {
	err error
}

func sendErr(err error) tea.Cmd {
	return func() tea.Msg {
		return errMsg{err: err}
	}
}
