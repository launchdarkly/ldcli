package setup

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/wordwrap"
)

type sdkInstructionModel struct {
	filename string
	flagKey  string
	name     string
	width    int
}

func NewSDKInstructions() tea.Model {
	return sdkInstructionModel{}
}

func (p sdkInstructionModel) Init() tea.Cmd {
	return nil
}

func (m sdkInstructionModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m, nil
}

func (m sdkInstructionModel) View() string {
	style := lipgloss.NewStyle().Border(lipgloss.NormalBorder())

	return wordwrap.String(
		fmt.Sprintf(
			"Set up your application. Here are the steps to incorporate the LaunchDarkly %s SDK into your code.\n%s",
			m.name,
			style.Render(m.renderMarkdown()),
		),
		m.width,
	)
}

func (m sdkInstructionModel) renderMarkdown() string {
	content, err := os.ReadFile(m.filename)
	if err != nil {
		fmt.Println("could not load file:", err)
		os.Exit(1)
	}
	sdkInstructions := strings.ReplaceAll(string(content), "my-flag-key", m.flagKey)

	out, err := glamour.Render(sdkInstructions, "auto")
	if err != nil {
		fmt.Println("could not render markdown:", err)
		os.Exit(1)
	}

	return out
}
