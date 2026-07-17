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
)

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
