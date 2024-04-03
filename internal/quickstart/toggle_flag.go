package quickstart

import (
	"fmt"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"ldcli/internal/flags"
)

type toggleFlagModel struct {
	accessToken    string
	baseUri        string
	client         flags.Client
	enabled        bool
	flagKey        string
	flagWasEnabled bool
	sdkKind        string
}

func NewToggleFlagModel(client flags.Client, accessToken string, baseUri string, flagKey string, sdkKind string) tea.Model {
	return toggleFlagModel{
		accessToken: accessToken,
		baseUri:     baseUri,
		client:      client,
		flagKey:     flagKey,
		sdkKind:     sdkKind,
	}
}

func (m toggleFlagModel) Init() tea.Cmd {
	return nil
}

func (m toggleFlagModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, keys.Tab):
			m.flagWasEnabled = true
			m.enabled = !m.enabled
			return m, sendToggleFlagMsg(m.client, m.accessToken, m.baseUri, m.flagKey, m.enabled)
		}
	}

	return m, cmd
}

var logTypeMap = map[string]string{
	serverSideSDK: "application logs",
	clientSideSDK: "browser",
}

func (m toggleFlagModel) View() string {
	var furtherInstructions string
	title := "Toggle your feature flag in your Test environment (press tab)"
	toggle := "OFF"
	bgColor := "#646a73"
	margin := 1
	if m.enabled {
		bgColor = "#3d9c51"
		margin = 2
		toggle = "ON"
	}

	if m.flagWasEnabled {
		furtherInstructions = fmt.Sprintf("\n\nCheck your %s to see the change!\n\n(press ctrl + c to quit)", logTypeMap[m.sdkKind])
	}

	toggleStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(bgColor)).
		Padding(0, 1).
		MarginRight(margin)

	return title + "\n\n" + toggleStyle.Render(toggle) + m.flagKey + furtherInstructions
}
