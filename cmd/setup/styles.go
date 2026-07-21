package setup

import "github.com/charmbracelet/lipgloss"

// Shared visual tokens for the setup wizard, aligned with ldcli's existing
// quickstart TUI: selected items use color 170, bordered panels use 62.
var (
	colorSelected = lipgloss.Color("170") // active selection / pointer
	colorBorder   = lipgloss.Color("62")  // focused panel border
	colorBlur     = lipgloss.Color("240") // unfocused panel border

	titleStyle    = lipgloss.NewStyle().Bold(true).MarginBottom(1)
	headerStyle   = lipgloss.NewStyle().Bold(true)
	selectedStyle = lipgloss.NewStyle().Foreground(colorSelected).Bold(true)
	mutedStyle    = lipgloss.NewStyle().Faint(true)

	// codeStyle marks copy-me code (snippets, commands) with a left gutter bar
	// and a distinct foreground, so the user can tell what to copy versus read.
	codeStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(colorBorder).
			Foreground(lipgloss.Color("252")).
			PaddingLeft(1)
)

// code renders a snippet or command as a distinct code block.
func code(s string) string { return codeStyle.Render(s) }

// wrapText reflows prose to the given width so it doesn't overflow narrow
// terminals. Returns the input unchanged when width is unknown (<=0).
func wrapText(s string, width int) string {
	if width <= 0 {
		return s
	}
	if width > 100 {
		width = 100
	}
	return lipgloss.NewStyle().Width(width).Render(s)
}

// box returns the panel style used on the SDK screen, highlighted when focused.
func box(focused bool, width int) lipgloss.Style {
	border := colorBlur
	if focused {
		border = colorBorder
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Padding(0, 1).
		Width(width)
}
