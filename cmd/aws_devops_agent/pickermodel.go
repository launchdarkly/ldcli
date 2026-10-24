package awsdevopsagent

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// teaChooser asks below the current output, so the text above the picker
// stays on the screen. Type to search the labels.
func teaChooser(in io.Reader, out io.Writer) chooser {
	return func(title, description string, labels []string) (int, error) {
		final, err := tea.NewProgram(newPicker(title, description, labels), tea.WithInput(in), tea.WithOutput(out)).Run()
		if err != nil {
			return 0, err
		}
		picked, ok := final.(picker)
		if !ok || picked.canceled {
			return 0, errPickCanceled
		}

		return picked.chosen, nil
	}
}

// picker is a list with a search line. It shows pickerRows rows at most. The
// highlight moves down to the last row, and then the list scrolls under it.
type picker struct {
	title       string
	description string
	labels      []string
	search      textinput.Model
	// matches are the indexes of the labels that contain the search text.
	matches []int
	// cursor is the highlighted position in matches, and offset is the
	// position of the first row on the screen.
	cursor   int
	offset   int
	width    int
	chosen   int
	done     bool
	canceled bool
}

// pickerHighlight colors the title and the highlighted row with the accent of
// the sync pickers.
var pickerHighlight = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "24", Dark: "67"})

func newPicker(title, description string, labels []string) picker {
	search := textinput.New()
	search.Prompt = "Search: "
	search.Focus()
	p := picker{title: title, description: description, labels: labels, search: search}
	p.filter()

	return p
}

func (p picker) Init() tea.Cmd {
	return textinput.Blink
}

func (p picker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width = msg.Width

		return p, nil
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			p.canceled, p.done = true, true

			return p, tea.Quit
		case tea.KeyEsc:
			// Esc clears the search first, and closes the picker after that.
			if p.search.Value() != "" {
				p.search.SetValue("")
				p.filter()

				return p, nil
			}
			p.canceled, p.done = true, true

			return p, tea.Quit
		case tea.KeyEnter:
			if len(p.matches) == 0 {
				return p, nil
			}
			p.chosen, p.done = p.matches[p.cursor], true

			return p, tea.Quit
		case tea.KeyUp, tea.KeyCtrlP:
			p.move(-1)

			return p, nil
		case tea.KeyDown, tea.KeyCtrlN:
			p.move(1)

			return p, nil
		}
	}

	before := p.search.Value()
	var cmd tea.Cmd
	p.search, cmd = p.search.Update(msg)
	if p.search.Value() != before {
		p.filter()
	}

	return p, cmd
}

// move moves the highlight and keeps it on the screen. Moving past the last
// row scrolls the list, so the highlight stays on the last row.
func (p *picker) move(step int) {
	if len(p.matches) == 0 {
		return
	}
	p.cursor = min(max(p.cursor+step, 0), len(p.matches)-1)
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	if p.cursor >= p.offset+pickerRows {
		p.offset = p.cursor - pickerRows + 1
	}
}

// filter keeps the labels that contain the search text, ignoring case, and
// moves the highlight to the first match.
func (p *picker) filter() {
	query := strings.ToLower(p.search.Value())
	p.matches = p.matches[:0]
	for i, label := range p.labels {
		if strings.Contains(strings.ToLower(label), query) {
			p.matches = append(p.matches, i)
		}
	}
	p.cursor, p.offset = 0, 0
}

func (p picker) View() string {
	// The picker leaves no lines when it ends. The caller prints the choice.
	if p.done {
		return ""
	}

	var b strings.Builder
	p.line(&b, pickerHighlight.Render(p.title))
	if p.description != "" {
		p.line(&b, p.description)
	}
	p.line(&b, p.search.View())
	if len(p.matches) == 0 {
		p.line(&b, "  No match")
	}
	end := min(p.offset+pickerRows, len(p.matches))
	for row := p.offset; row < end; row++ {
		label := p.labels[p.matches[row]]
		if row == p.cursor {
			p.line(&b, pickerHighlight.Render("> "+label))
		} else {
			p.line(&b, "  "+label)
		}
	}
	p.line(&b, fmt.Sprintf("%d of %d • ↑/↓ move • enter select • esc cancel", len(p.matches), len(p.labels)))

	return b.String()
}

// line adds one line, cut to the terminal width. A line that wraps breaks the
// redraw of an inline program.
func (p picker) line(b *strings.Builder, text string) {
	if p.width > 0 {
		text = ansi.Truncate(text, p.width, "")
	}
	b.WriteString(text + "\n")
}
