package setup

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/muesli/reflow/wordwrap"
)

type sdkInstructionModel struct {
	filename string
	flagKey  string
	name     string
	width    int
}

func (p sdkInstructionModel) Init() tea.Cmd {
	return nil
}

func (m sdkInstructionModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m, nil
}

func (m sdkInstructionModel) View() string {
	return wordwrap.String(
		fmt.Sprintf(
			"Set up your application. Here are the steps to incorporate the LaunchDarkly %s SDK into your code. \n\n%s",
			m.name,
			m.renderMarkdown(),
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

	gs := glamour.WithEnvironmentConfig()
	r, err := glamour.NewTermRenderer(
		gs,
		glamour.WithWordWrap(int(80)),
		glamour.WithPreservedNewLines(),
	)
	if err != nil {
		panic(err)
	}
	out, err := r.RenderBytes([]byte(sdkInstructions))
	if err != nil {
		panic(err)
	}
	lines := strings.Split(string(out), "\n")
	var cb strings.Builder
	for i, s := range lines {
		cb.WriteString(strings.TrimSpace(s))

		// don't add an artificial newline after the last split
		if i+1 < len(lines) {
			cb.WriteString("\n")
		}
	}
	return cb.String()
}
