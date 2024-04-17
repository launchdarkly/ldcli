package quickstart

import (
	"fmt"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"ldcli/internal/errors"
	"ldcli/internal/flags"
)

type toggleFlagModel struct {
	accessToken    string
	baseUri        string
	client         flags.Client
	enabled        bool
	err            error
	flagKey        string
	flagWasEnabled bool
	flagWasFetched bool
	help           help.Model
	helpKeys       keyMap
	sdkKind        string
	spinner        spinner.Model
}

func NewToggleFlagModel(client flags.Client, accessToken string, baseUri string, flagKey string, sdkKind string) tea.Model {
	s := spinner.New()
	s.Spinner = spinner.Points

	return toggleFlagModel{
		accessToken: accessToken,
		baseUri:     baseUri,
		client:      client,
		flagKey:     flagKey,
		help:        help.New(),
		helpKeys: keyMap{
			Back: BindingBack,
			Quit: BindingQuit,
		},
		sdkKind: sdkKind,
		spinner: s,
	}
}

func (m toggleFlagModel) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spinner.Tick, fetchFlagStatus(m.client, m.accessToken, m.baseUri, m.flagKey, defaultEnvKey, defaultProjKey)}

	return tea.Sequence(cmds...)
}

func (m toggleFlagModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, pressableKeys.Quit):
			return m, tea.Quit
		case key.Matches(msg, pressableKeys.Tab):
			if !m.flagWasFetched {
				return m, nil
			}
			m.flagWasEnabled = true
			m.enabled = !m.enabled
			m.err = nil
			return m, toggleFlag(m.client, m.accessToken, m.baseUri, m.flagKey, m.enabled)
		}
	case fetchedFlagStatusMsg:
		m.enabled = msg.enabled
		m.flagWasFetched = true
	case spinner.TickMsg:
		m.spinner, cmd = m.spinner.Update(msg)
	case errMsg:
		msgRequestErr, err := newMsgRequestError(msg.err.Error())
		if err != nil {
			m.err = err
			return m, cmd
		}
		if msgRequestErr.IsConflict() {
			m.err = errors.NewError("Error toggling flag: you have toggled too quickly.")
			return m, cmd
		}

		m.err = msg.err
	}

	return m, cmd
}

var logTypeMap = map[string]string{
	serverSideSDK: "application logs",
	mobileSDK:     "application",
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
	if !m.flagWasFetched {
		title = "Toggle your feature flag in your Test environment"
		bgColor = "#dcea5a"
		margin = 1
		toggle = m.spinner.View()
	}

	if m.flagWasEnabled {
		furtherInstructions = fmt.Sprintf("\n\nCheck your %s to see the change!", logTypeMap[m.sdkKind])
	}

	toggleStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(bgColor)).
		Padding(0, 1).
		MarginRight(margin)

	return title + "\n\n" + toggleStyle.Render(toggle) + m.flagKey + furtherInstructions + footerView(m.help.View(m.helpKeys), m.err)
}
