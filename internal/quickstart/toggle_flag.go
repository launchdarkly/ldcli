package quickstart

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/viper"
	
	"ldcli/cmd/cliflags"
	"ldcli/internal/flags"
)

const defaultEnvironmentKey = "test"
const defaultProjectKey = "default"

var logTypeMap = map[string]string{
	serverSideSDK: "application logs",
	clientSideSDK: "browser",
}

type toggleFlagModel struct {
	client   flags.Client
	enabled  bool
	on       bool
	err      error
	flagKey  string
	quitMsg  string
	quitting bool
	sdkKind  string
}

func NewToggleFlagModel(client flags.Client) toggleFlagModel {
	return toggleFlagModel{
		client: client,
	}
}

func (m toggleFlagModel) Init() tea.Cmd { return nil }

func (m toggleFlagModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Toggle):
			m.on = !m.on
			if m.on {
				m.enabled = true
			}
			m, cmd = m.patchFlag(context.Background())
		}
	case updateToggleFlagModelMsg:
		m.flagKey = msg.flagKey
		m.sdkKind = msg.sdkKind
	}
	return m, cmd
}

func (m toggleFlagModel) View() string {
	var furtherInstructions string
	title := "Toggle your feature flag (press tab)"
	toggle := "OFF"
	bgColor := "#646a73"
	margin := 1
	if m.on {
		bgColor = "#3d9c51"
		margin = 2
		toggle = "ON"
	}

	if m.enabled {
		furtherInstructions = fmt.Sprintf("\n\nCheck your %s to see the change!\n\n(press enter to continue)", logTypeMap[m.sdkKind])
	}

	toggleStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(bgColor)).
		Padding(0, 1).
		MarginRight(margin)

	return title + "\n\n" + toggleStyle.Render(toggle) + m.flagKey + furtherInstructions
}

func (m toggleFlagModel) patchFlag(ctx context.Context) (toggleFlagModel, tea.Cmd) {
	_, err := m.client.Update(
		ctx,
		viper.GetString(cliflags.AccessTokenFlag),
		viper.GetString(cliflags.BaseURIFlag),
		m.flagKey,
		defaultProjectKey,
		flags.BuildToggleFlagPatch(defaultEnvironmentKey, m.on),
	)

	if err != nil {
		m.err = err
		// TODO: refactor with flag create model
		var e struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal([]byte(m.err.Error()), &e)
		switch {
		case e.Code == "unauthorized":
			m.quitting = true
			m.quitMsg = "Your API key is unauthorized. Try another API key or speak to a LaunchDarkly account administrator."

			return m, tea.Quit
		case e.Code == "forbidden":
			m.quitting = true
			m.quitMsg = "You lack access to complete this action. Try authenticating with elevated access or speak to a LaunchDarkly account administrator."

			return m, tea.Quit
		}

		return m, sendErr(err)
	}

	return m, nil
}
