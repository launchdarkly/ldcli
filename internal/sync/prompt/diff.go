package prompt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/pmezard/go-difflib/difflib"
	"golang.org/x/term"
)

type variationFieldDiff struct {
	Before json.RawMessage `json:"before"`
	After  json.RawMessage `json:"after"`
}

type variationDiffFields map[string]variationFieldDiff

func renderVariationDiff(
	fields variationDiffFields,
	outputKind string,
	width int,
	presentation variationDiffPresentation,
) (string, error) {
	fields, err := collapseWholeVariationDiff(fields)
	if err != nil {
		return "", err
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	var rendered strings.Builder
	for _, key := range keys {
		diff := fields[key]
		if presentation.reverse {
			diff.Before, diff.After = diff.After, diff.Before
		}
		before, err := formatDiffValue(diff.Before, presentation.beforeLabel, "")
		if err != nil {
			return "", err
		}
		after, err := formatDiffValue(diff.After, presentation.afterLabel, presentation.missingAfter)
		if err != nil {
			return "", err
		}
		change := "changed"
		if len(diff.Before) == 0 {
			change = "added"
		}
		if len(diff.After) == 0 {
			change = "removed"
		}

		diffLines, err := unifiedDiffLines(
			before,
			after,
			presentation.beforeLabel,
			presentation.afterLabel,
		)
		if err != nil {
			return "", err
		}
		if outputKind == "markdown" {
			_, _ = fmt.Fprintf(&rendered, "\n#### %s (%s)\n\n", key, change)
			_, _ = fmt.Fprintf(
				&rendered,
				"```diff\n%s\n```\n",
				strings.Join(diffLines, "\n"),
			)
			continue
		}
		_, _ = fmt.Fprintf(&rendered, "\n  %s (%s)\n", key, change)
		if width >= 100 {
			rendered.WriteString(renderSideBySideUnifiedDiff(diffLines, width))
		} else {
			rendered.WriteString(renderUnifiedDiff(diffLines, width > 0))
		}
	}
	return rendered.String(), nil
}

func collapseWholeVariationDiff(
	fields map[string]variationFieldDiff,
) (map[string]variationFieldDiff, error) {
	if len(fields) == 0 {
		return fields, nil
	}

	allAdded := true
	allRemoved := true
	for _, diff := range fields {
		allAdded = allAdded && len(diff.Before) == 0
		allRemoved = allRemoved && len(diff.After) == 0
	}
	if !allAdded && !allRemoved {
		return fields, nil
	}

	values := make(map[string]json.RawMessage, len(fields))
	for key, diff := range fields {
		if allAdded {
			values[key] = diff.After
		} else {
			values[key] = diff.Before
		}
	}
	value, err := json.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("combine variation diff: %w", err)
	}

	combined := variationFieldDiff{}
	if allAdded {
		combined.After = value
	} else {
		combined.Before = value
	}
	return map[string]variationFieldDiff{"variation": combined}, nil
}

func unifiedDiffLines(
	before string,
	after string,
	beforeLabel string,
	afterLabel string,
) ([]string, error) {
	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        diffInputLines(before),
		B:        diffInputLines(after),
		FromFile: beforeLabel,
		ToFile:   afterLabel,
		Context:  3,
	})
	if err != nil {
		return nil, fmt.Errorf("build variation diff: %w", err)
	}
	return strings.Split(strings.TrimSuffix(diff, "\n"), "\n"), nil
}

func diffInputLines(value string) []string {
	lines := strings.Split(value, "\n")
	for index := range lines {
		lines[index] += "\n"
	}
	return lines
}

func renderUnifiedDiff(lines []string, color bool) string {
	var rendered strings.Builder
	for index := 0; index < len(lines); {
		change := scanDiffChange(lines, index)
		if len(change.removed) != 0 && len(change.added) != 0 {
			styledRemoved := append([]string(nil), change.removed...)
			styledAdded := append([]string(nil), change.added...)
			pairs := min(len(change.removed), len(change.added))
			for pair := 0; pair < pairs; pair++ {
				if color {
					styledRemoved[pair], styledAdded[pair] = renderChangedLinePair(
						change.removed[pair],
						change.added[pair],
					)
				}
			}
			for index := pairs; index < len(styledRemoved); index++ {
				styledRemoved[index] = styleDiffLine(styledRemoved[index], color)
			}
			for index := pairs; index < len(styledAdded); index++ {
				styledAdded[index] = styleDiffLine(styledAdded[index], color)
			}
			for _, line := range append(styledRemoved, styledAdded...) {
				_, _ = fmt.Fprintf(&rendered, "    %s\n", line)
			}
			index = change.next
			continue
		}
		_, _ = fmt.Fprintf(
			&rendered,
			"    %s\n",
			styleDiffLine(lines[index], color),
		)
		index++
	}
	return rendered.String()
}

func renderSideBySideUnifiedDiff(lines []string, width int) string {
	if len(lines) < 2 {
		return renderUnifiedDiff(lines, true)
	}

	const (
		indentWidth = 4
		columnGap   = 2
	)
	columnWidth := (width - indentWidth - columnGap) / 2
	cellStyle := lipgloss.NewStyle().Width(columnWidth)

	var rendered strings.Builder
	writeRow := func(before, after string) {
		before = ansi.Wordwrap(before, columnWidth, ",:")
		after = ansi.Wordwrap(after, columnWidth, ",:")
		row := lipgloss.JoinHorizontal(
			lipgloss.Top,
			cellStyle.Render(before),
			strings.Repeat(" ", columnGap),
			cellStyle.Render(after),
		)
		_, _ = fmt.Fprintf(
			&rendered,
			"%s\n",
			indentBlock(row, indentWidth),
		)
	}

	writeRow(styleDiffLine(lines[0], true), styleDiffLine(lines[1], true))
	for index := 2; index < len(lines); {
		if strings.HasPrefix(lines[index], "@@") {
			_, _ = fmt.Fprintf(
				&rendered,
				"    %s\n",
				styleDiffLine(lines[index], true),
			)
			index++
			continue
		}

		change := scanDiffChange(lines, index)
		if len(change.removed) != 0 || len(change.added) != 0 {
			for pair := 0; pair < max(len(change.removed), len(change.added)); pair++ {
				var beforeLine, afterLine string
				switch {
				case pair < len(change.removed) && pair < len(change.added):
					beforeLine, afterLine = renderChangedLinePair(
						change.removed[pair],
						change.added[pair],
					)
				case pair < len(change.removed):
					beforeLine = styleDiffLine(change.removed[pair], true)
				default:
					afterLine = styleDiffLine(change.added[pair], true)
				}
				writeRow(beforeLine, afterLine)
			}
			index = change.next
			continue
		}

		context := styleDiffLine(lines[index], true)
		writeRow(context, context)
		index++
	}
	return rendered.String()
}

type diffChange struct {
	removed []string
	added   []string
	next    int
}

func scanDiffChange(lines []string, start int) diffChange {
	removedEnd := start
	for removedEnd < len(lines) && isRemovedDiffLine(lines[removedEnd]) {
		removedEnd++
	}

	addedEnd := removedEnd
	for addedEnd < len(lines) && isAddedDiffLine(lines[addedEnd]) {
		addedEnd++
	}

	return diffChange{
		removed: lines[start:removedEnd],
		added:   lines[removedEnd:addedEnd],
		next:    addedEnd,
	}
}

func indentBlock(value string, spaces int) string {
	prefix := strings.Repeat(" ", spaces)
	return prefix + strings.ReplaceAll(value, "\n", "\n"+prefix)
}

func isRemovedDiffLine(line string) bool {
	return strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---")
}

func isAddedDiffLine(line string) bool {
	return strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++")
}

func styleDiffLine(line string, color bool) string {
	if !color {
		return line
	}
	switch {
	case strings.HasPrefix(line, "---"), isRemovedDiffLine(line):
		return lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(line)
	case strings.HasPrefix(line, "+++"), isAddedDiffLine(line):
		return lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render(line)
	case strings.HasPrefix(line, "@@"):
		return lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Render(line)
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(line)
	}
}

func renderChangedLinePair(before, after string) (string, string) {
	if !isRemovedDiffLine(before) || !isAddedDiffLine(after) {
		return styleDiffLine(before, true), styleDiffLine(after, true)
	}

	prefix, removed, added, suffix := changedParts(before[1:], after[1:])
	removedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	addedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	removedHighlight := removedStyle.
		Background(lipgloss.Color("52")).
		Bold(true)
	addedHighlight := addedStyle.
		Background(lipgloss.Color("22")).
		Bold(true)

	return removedStyle.Render("-"+prefix) +
			removedHighlight.Render(removed) +
			removedStyle.Render(suffix),
		addedStyle.Render("+"+prefix) +
			addedHighlight.Render(added) +
			addedStyle.Render(suffix)
}

func changedParts(before, after string) (
	prefix string,
	removed string,
	added string,
	suffix string,
) {
	beforeRunes := []rune(before)
	afterRunes := []rune(after)
	prefixLength := 0
	for prefixLength < min(len(beforeRunes), len(afterRunes)) &&
		beforeRunes[prefixLength] == afterRunes[prefixLength] {
		prefixLength++
	}

	suffixLength := 0
	for suffixLength < len(beforeRunes)-prefixLength &&
		suffixLength < len(afterRunes)-prefixLength &&
		beforeRunes[len(beforeRunes)-1-suffixLength] ==
			afterRunes[len(afterRunes)-1-suffixLength] {
		suffixLength++
	}

	beforeChangeEnd := len(beforeRunes) - suffixLength
	afterChangeEnd := len(afterRunes) - suffixLength
	return string(beforeRunes[:prefixLength]),
		string(beforeRunes[prefixLength:beforeChangeEnd]),
		string(afterRunes[prefixLength:afterChangeEnd]),
		string(beforeRunes[beforeChangeEnd:])
}

func formatDiffValue(value json.RawMessage, label string, missingValue string) (string, error) {
	if len(value) == 0 || bytes.Equal(value, []byte("null")) {
		if missingValue != "" {
			return missingValue, nil
		}
		if strings.HasPrefix(label, "LaunchDarkly") {
			return "(does not exist in LaunchDarkly)", nil
		}
		return "(does not exist locally)", nil
	}
	var formatted bytes.Buffer
	if err := json.Indent(&formatted, value, "", "  "); err != nil {
		return "", fmt.Errorf("format variation diff: %w", err)
	}
	return formatted.String(), nil
}

func terminalWidth(out io.Writer) int {
	file, ok := out.(*os.File)
	if !ok || !term.IsTerminal(int(file.Fd())) {
		return 0
	}
	width, _, err := term.GetSize(int(file.Fd()))
	if err != nil {
		return 0
	}
	return width
}
