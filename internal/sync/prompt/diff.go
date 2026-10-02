package prompt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	"github.com/pmezard/go-difflib/difflib"
	"golang.org/x/term"
)

type variationFieldDiff struct {
	Before json.RawMessage `json:"before"`
	After  json.RawMessage `json:"after"`
}

type variationDiffFields map[string]variationFieldDiff

type renderedDiffSection struct {
	title  string
	change string
	before string
	after  string
}

const (
	diffSectionIndent = 6
	diffBodyIndent    = 8
)

// renderVariationDiff formats structured field changes as terminal or Markdown
// unified diffs, choosing side-by-side output when the terminal is wide enough.
func renderVariationDiff(fields variationDiffFields, outputKind string, width int, presentation variationDiffPresentation) (string, error) {
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
		sections, err := diffSections(key, diff, presentation)
		if err != nil {
			return "", err
		}
		for _, section := range sections {
			diffLines, err := unifiedDiffLines(section.before, section.after, presentation.beforeLabel, presentation.afterLabel)
			if err != nil {
				return "", err
			}
			if key == "tools" || key == "skills" {
				diffLines = slices.DeleteFunc(diffLines, func(line string) bool {
					return strings.HasPrefix(line, "@@")
				})
			}
			if outputKind == "markdown" {
				_, _ = fmt.Fprintf(&rendered, "\n#### %s (%s)\n\n", section.title, section.change)
				_, _ = fmt.Fprintf(&rendered, "```diff\n%s\n```\n", strings.Join(diffLines, "\n"))
				continue
			}
			sectionTitle := fmt.Sprintf("%s (%s)", section.title, section.change)
			if width > 0 {
				sectionTitle = lipgloss.NewStyle().Foreground(lipgloss.Color("67")).Bold(true).Render(sectionTitle)
			}
			_, _ = fmt.Fprintf(&rendered, "\n%s%s\n\n", strings.Repeat(" ", diffSectionIndent), sectionTitle)
			if width >= 100 {
				rendered.WriteString(renderSideBySideUnifiedDiff(diffLines, width))
			} else {
				rendered.WriteString(renderUnifiedDiff(diffLines, width > 0))
			}
		}
	}
	return rendered.String(), nil
}

func diffSections(field string, diff variationFieldDiff, presentation variationDiffPresentation) ([]renderedDiffSection, error) {
	switch field {
	case "tools":
		return attachmentDiffSections[syncdomain.Tool]("Tool", diff, func(tool syncdomain.Tool) string { return tool.Key }, formatToolDetails)
	case "skills":
		return attachmentDiffSections[syncdomain.Skill](
			"Skill",
			diff,
			func(skill syncdomain.Skill) string { return skill.Key },
			formatSkillDetails,
		)
	}

	before, err := formatDiffValue(diff.Before, presentation.beforeLabel, "")
	if err != nil {
		return nil, err
	}
	after, err := formatDiffValue(diff.After, presentation.afterLabel, presentation.missingAfter)
	if err != nil {
		return nil, err
	}
	return []renderedDiffSection{{
		title: diffFieldTitle(field), change: diffChangeKind(diff.Before, diff.After),
		before: before, after: after,
	}}, nil
}

func attachmentDiffSections[T any](
	kind string,
	diff variationFieldDiff,
	key func(T) string,
	format func(T) string,
) ([]renderedDiffSection, error) {
	before, err := decodeAttachmentDiffItems[T](diff.Before)
	if err != nil {
		return nil, fmt.Errorf("format %s diff: %w", strings.ToLower(kind), err)
	}
	after, err := decodeAttachmentDiffItems[T](diff.After)
	if err != nil {
		return nil, fmt.Errorf("format %s diff: %w", strings.ToLower(kind), err)
	}

	beforeByKey := make(map[string]T, len(before))
	afterByKey := make(map[string]T, len(after))
	keys := make([]string, 0, len(before)+len(after))
	for _, item := range before {
		beforeByKey[key(item)] = item
		keys = append(keys, key(item))
	}
	for _, item := range after {
		afterByKey[key(item)] = item
		if _, exists := beforeByKey[key(item)]; !exists {
			keys = append(keys, key(item))
		}
	}
	slices.Sort(keys)

	sections := make([]renderedDiffSection, 0, len(keys))
	for _, itemKey := range keys {
		beforeItem, beforeExists := beforeByKey[itemKey]
		afterItem, afterExists := afterByKey[itemKey]
		if beforeExists && afterExists && reflect.DeepEqual(beforeItem, afterItem) {
			continue
		}

		beforeValue, afterValue := "(not attached)", "(not attached)"
		if beforeExists {
			beforeValue = format(beforeItem)
		}
		if afterExists {
			afterValue = format(afterItem)
		}
		sections = append(sections, renderedDiffSection{
			title:  fmt.Sprintf("%s %q", kind, itemKey),
			change: attachmentChangeKind(beforeExists, afterExists),
			before: beforeValue,
			after:  afterValue,
		})
	}
	return sections, nil
}

func decodeAttachmentDiffItems[T any](value json.RawMessage) ([]T, error) {
	if len(value) == 0 || bytes.Equal(value, []byte("null")) {
		return nil, nil
	}
	var items []T
	if err := json.Unmarshal(value, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func diffChangeKind(before, after json.RawMessage) string {
	switch {
	case len(before) == 0:
		return "added"
	case len(after) == 0:
		return "removed"
	default:
		return "changed"
	}
}

func attachmentChangeKind(before, after bool) string {
	switch {
	case !before:
		return "added"
	case !after:
		return "removed"
	default:
		return "changed"
	}
}

// unifiedDiffLines delegates line-level comparison to go-difflib while keeping
// labels and context consistent across output modes.
func unifiedDiffLines(before, after, beforeLabel, afterLabel string) ([]string, error) {
	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        diffInputLines(before),
		B:        diffInputLines(after),
		FromFile: beforeLabel,
		ToFile:   afterLabel,
		Context:  1,
	})
	if err != nil {
		return nil, fmt.Errorf("build variation diff: %w", err)
	}
	return strings.Split(strings.TrimSuffix(diff, "\n"), "\n"), nil
}

func diffFieldTitle(field string) string {
	switch field {
	case "variation":
		return "Variation content"
	case "attachment versions":
		return "Attachment versions for this variation"
	default:
		return field
	}
}

// diffInputLines gives every logical line the terminator expected by difflib.
func diffInputLines(value string) []string {
	lines := strings.Split(value, "\n")
	for index := range lines {
		lines[index] += "\n"
	}
	return lines
}

// renderUnifiedDiff renders a conventional single-column diff and highlights
// paired replacements more precisely than independent added/removed lines.
func renderUnifiedDiff(lines []string, color bool) string {
	var rendered strings.Builder
	for index := 0; index < len(lines); {
		change := scanDiffChange(lines, index)
		if len(change.removed) != 0 && len(change.added) != 0 {
			// Clone scanned lines before styling so later rendering still sees
			// the original diff text.
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
				_, _ = fmt.Fprintf(&rendered, "%s%s\n", strings.Repeat(" ", diffBodyIndent), line)
			}
			index = change.next
			continue
		}
		_, _ = fmt.Fprintf(
			&rendered,
			"%s%s\n",
			strings.Repeat(" ", diffBodyIndent),
			styleDiffLine(lines[index], color),
		)
		index++
	}
	return rendered.String()
}

// renderSideBySideUnifiedDiff aligns removed and added lines into equal-width
// columns while retaining unified-diff headers and hunks.
func renderSideBySideUnifiedDiff(lines []string, width int) string {
	if len(lines) < 2 {
		return renderUnifiedDiff(lines, true)
	}

	const (
		columnGap = 2
	)
	columnWidth := (width - diffBodyIndent - columnGap) / 2
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
			indentBlock(row, diffBodyIndent),
		)
	}

	writeRow(styleDiffLine(lines[0], true), styleDiffLine(lines[1], true))
	for index := 2; index < len(lines); {
		if strings.HasPrefix(lines[index], "@@") {
			_, _ = fmt.Fprintf(
				&rendered,
				"%s%s\n",
				strings.Repeat(" ", diffBodyIndent),
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

// scanDiffChange groups adjacent removed and added lines into one replacement block.
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

// indentBlock applies the same left margin to every rendered line.
func indentBlock(value string, spaces int) string {
	prefix := strings.Repeat(" ", spaces)
	return prefix + strings.ReplaceAll(value, "\n", "\n"+prefix)
}

// isRemovedDiffLine distinguishes content removals from the --- file header.
func isRemovedDiffLine(line string) bool {
	return strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---")
}

// isAddedDiffLine distinguishes content additions from the +++ file header.
func isAddedDiffLine(line string) bool {
	return strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++")
}

// styleDiffLine applies semantic colors to headers, hunks, context, and changes.
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

// renderChangedLinePair highlights only the changed span within paired lines.
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

// changedParts separates two lines into their shared prefix, changed middle,
// and shared suffix using runes rather than bytes.
func changedParts(before, after string) (prefix, removed, added, suffix string) {
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

// formatDiffValue pretty-prints JSON and substitutes readable absence markers
// for missing local or LaunchDarkly resources.
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

func formatToolDetails(tool syncdomain.Tool) string {
	var rendered strings.Builder
	if tool.Description != nil && *tool.Description != "" {
		_, _ = fmt.Fprintf(&rendered, "Description: %s", *tool.Description)
	}
	writeDiffValue(&rendered, "Schema", tool.Schema, 0)
	if len(tool.CustomParameters) != 0 {
		writeDiffValue(&rendered, "Custom parameters", tool.CustomParameters, 0)
	}
	if len(tool.Tags) != 0 {
		writeDiffValue(&rendered, "Tags", tool.Tags, 0)
	}
	return strings.TrimPrefix(rendered.String(), "\n")
}

func formatSkillDetails(skill syncdomain.Skill) string {
	var rendered strings.Builder
	if skill.Description != "" {
		_, _ = fmt.Fprintf(&rendered, "Description: %s", skill.Description)
	}
	if skill.Markdown != "" {
		if rendered.Len() > 0 {
			rendered.WriteString("\n")
		}
		rendered.WriteString("Markdown:\n")
		rendered.WriteString(indentBlock(strings.TrimSpace(skill.Markdown), 2))
	}
	if rendered.Len() == 0 {
		return "(attached)"
	}
	return rendered.String()
}

func writeDiffValue(rendered *strings.Builder, label string, value any, indent int) {
	padding := strings.Repeat(" ", indent)
	switch value := value.(type) {
	case map[string]any:
		if len(value) == 0 {
			_, _ = fmt.Fprintf(rendered, "\n%s%s: {}", padding, label)
			return
		}
		_, _ = fmt.Fprintf(rendered, "\n%s%s:", padding, label)
		writeDiffMap(rendered, value, indent+2)
	case []string:
		_, _ = fmt.Fprintf(rendered, "\n%s%s:", padding, label)
		for _, item := range value {
			_, _ = fmt.Fprintf(rendered, "\n%s- %s", strings.Repeat(" ", indent+2), item)
		}
	default:
		_, _ = fmt.Fprintf(rendered, "\n%s%s: %v", padding, label, value)
	}
}

func writeDiffMap(rendered *strings.Builder, values map[string]any, indent int) {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	padding := strings.Repeat(" ", indent)
	for _, key := range keys {
		value := values[key]
		switch nested := value.(type) {
		case map[string]any:
			if len(nested) == 0 {
				_, _ = fmt.Fprintf(rendered, "\n%s%s: {}", padding, key)
				continue
			}
			_, _ = fmt.Fprintf(rendered, "\n%s%s:", padding, key)
			writeDiffMap(rendered, nested, indent+2)
		case []any:
			if len(nested) == 0 {
				_, _ = fmt.Fprintf(rendered, "\n%s%s: []", padding, key)
				continue
			}
			_, _ = fmt.Fprintf(rendered, "\n%s%s:", padding, key)
			for _, item := range nested {
				_, _ = fmt.Fprintf(rendered, "\n%s- %v", strings.Repeat(" ", indent+2), item)
			}
		default:
			_, _ = fmt.Fprintf(rendered, "\n%s%s: %v", padding, key, value)
		}
	}
}

// terminalWidth returns zero for redirected output or unavailable terminal metadata.
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
