package quickstart

import tea "github.com/charmbracelet/bubbletea"

// errMsg is sent when there is an error in one of the steps that the container model needs to
// know about.
type errMsg struct {
	err error
}

func sendErr(err error) tea.Cmd {
	return func() tea.Msg {
		return errMsg{err: err}
	}
}

type fetchSDKInstructionsMsg struct {
	canonicalName string
	flagKey       string
	name          string
}

func sendFetchSDKInstructionsMsg(canonicalName string, displayName string) tea.Cmd {
	return func() tea.Msg {
		return fetchSDKInstructionsMsg{
			canonicalName: canonicalName,
			name:          displayName,
		}
	}
}

// noInstructionsMsg is sent when we can't find the SDK instructions repository for the given SDK.
type noInstructionsMsg struct{}

func sendNoInstructions() tea.Cmd {
	return func() tea.Msg {
		return noInstructionsMsg{}
	}
}
