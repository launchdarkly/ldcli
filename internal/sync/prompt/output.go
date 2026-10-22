package prompt

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncconsole "github.com/launchdarkly/ldcli/internal/sync/console"
)

// The output kinds that the --output flag selects.
const (
	outputPlaintext = "plaintext"
	outputMarkdown  = "markdown"
	outputJSON      = "json"
)

// OutcomeStatus is the result of one planned action.
type OutcomeStatus string

const (
	OutcomeSucceeded OutcomeStatus = "succeeded"
	OutcomeFailed    OutcomeStatus = "failed"
	OutcomeSkipped   OutcomeStatus = "skipped"
)

// ResourceOutcome is the result of the action of one planned resource.
type ResourceOutcome struct {
	ID     ResourceID    `json:"-"`
	Action Action        `json:"action"`
	Status OutcomeStatus `json:"status"`
	Error  string        `json:"error,omitempty"`
}

// resourceOutput is the identity of a resource in JSON output.
type resourceOutput struct {
	ResourceKind string `json:"resourceKind"`
	ProjectKey   string `json:"projectKey"`
	LookupKey    string `json:"lookupKey"`
}

func newResourceOutput(id ResourceID) resourceOutput {
	return resourceOutput{ResourceKind: string(id.Kind), ProjectKey: id.ProjectKey, LookupKey: id.LookupKey}
}

// writePlanOutput writes the plan in the selected output kind. Plain text and
// Markdown show a full diff for a conflict, and for every row when showDiffs
// is true.
func writePlanOutput(out io.Writer, outputKind string, plan Plan, showDiffs bool) error {
	outputKind, err := checkOutputKind(outputKind)
	if err != nil {
		return err
	}
	if outputKind != outputJSON {
		return writePlanReview(out, outputKind, plan, terminalWidth(out), showDiffs)
	}

	type planResourceOutput struct {
		resourceOutput
		Action          Action              `json:"action"`
		SyncedElsewhere bool                `json:"syncedElsewhere,omitempty"`
		Suggestion      string              `json:"suggestion,omitempty"`
		Error           string              `json:"error,omitempty"`
		Diff            variationDiffFields `json:"diff,omitempty"`
	}
	resources := make([]planResourceOutput, 0, len(plan.Resources))
	for _, resource := range plan.Resources {
		resources = append(resources, planResourceOutput{
			resourceOutput:  newResourceOutput(resource.ID),
			Action:          resource.Action,
			SyncedElsewhere: resource.SyncedElsewhere,
			Suggestion:      staleSuggestion(resource),
			Error:           resource.Error,
			Diff:            resource.Diff,
		})
	}
	return writeJSON(out, map[string]any{"resources": resources})
}

// writeOutcomeOutput writes the result of each action, which includes
// failures. Plain text and Markdown take the reason of each result from the
// plan that produced the outcomes.
func writeOutcomeOutput(out io.Writer, outputKind string, plan Plan, outcomes []ResourceOutcome) error {
	outputKind, err := checkOutputKind(outputKind)
	if err != nil {
		return err
	}
	if outputKind == outputJSON {
		type outcomeOutput struct {
			resourceOutput
			ResourceOutcome
		}
		resources := make([]outcomeOutput, 0, len(outcomes))
		for _, outcome := range outcomes {
			resources = append(resources, outcomeOutput{resourceOutput: newResourceOutput(outcome.ID), ResourceOutcome: outcome})
		}
		return writeJSON(out, map[string]any{"resources": resources})
	}

	planned := make(map[ResourceID]PlannedResource, len(plan.Resources))
	for _, resource := range plan.Resources {
		planned[resource.ID] = resource
	}
	var rows []reviewRow
	var failures []ResourceOutcome
	inSync := 0
	for _, outcome := range outcomes {
		resource := planned[outcome.ID]
		switch {
		case outcome.Status != OutcomeSucceeded:
			failures = append(failures, outcome)
		case outcome.Action == ActionInSync:
			inSync++
		default:
			rows = append(rows, reviewRow{
				kind:  outcome.ID.Kind,
				cells: []string{actionWordFor(resource).result, outcome.ID.String(), planReason(resource)},
			})
		}
	}

	console := syncconsole.New(out)
	if len(rows) != 0 {
		writeRows(console, outputKind, []string{"Result", "Resource", "Reason"}, rows, terminalWidth(out))
		_ = console.Line("")
	}
	for _, failure := range failures {
		_ = console.Printf("Failed: %s: %s\n", failure.ID.Label(), failure.Error)
	}
	_ = console.Line(resultTotals(len(rows), len(failures), inSync))
	return nil
}

// checkOutputKind returns the output kind. An empty kind is plain text.
func checkOutputKind(outputKind string) (string, error) {
	switch outputKind {
	case "":
		return outputPlaintext, nil
	case outputPlaintext, outputMarkdown, outputJSON:
		return outputKind, nil
	default:
		return "", fmt.Errorf("unsupported output kind %q", outputKind)
	}
}

// writePlanReview writes the plan for a person to read: a summary line, and
// then one row for each change under a heading for its resource kind. Rows
// that are in sync count only in the summary. Each row shows the size of its
// diff. The full diff follows a conflict, and every row when showDiffs is true.
func writePlanReview(out io.Writer, outputKind string, plan Plan, width int, showDiffs bool) error {
	console := syncconsole.New(out)
	if len(plan.Resources) == 0 {
		_ = console.Line("No prompt variations are synced in this repository. To start, run ldcli sync prompts add.")
		return nil
	}
	_ = console.Line(planSummary(plan))

	color := outputKind != outputMarkdown && width > 0
	var rows []reviewRow
	for _, resource := range plan.Resources {
		if resource.Action == ActionInSync {
			continue
		}
		presentation := diffPresentation(resource.Action)
		added, removed, err := diffSize(resource.Diff, presentation)
		if err != nil {
			return err
		}
		row := reviewRow{
			kind:  resource.ID.Kind,
			cells: []string{actionWordFor(resource).plan, resource.ID.String(), planReason(resource), diffSizeText(added, removed, color)},
		}
		if len(resource.Diff) != 0 && (showDiffs || resource.Action == ActionConflict) {
			if row.diff, err = renderVariationDiff(resource.Diff, outputKind, width, presentation); err != nil {
				return err
			}
		}
		rows = append(rows, row)
	}
	if len(rows) != 0 {
		_ = console.Line("")
		writeRows(console, outputKind, []string{"Action", "Resource", "Reason", "Size"}, rows, width)
	}
	return nil
}

// writePlanNotes writes one line for each resource that another working copy
// synced after this working copy wrote its sync.lock file.
func writePlanNotes(out io.Writer, plan Plan) {
	console := syncconsole.New(out)
	for _, resource := range plan.Resources {
		if resource.SyncedElsewhere {
			_ = console.Printf(
				"Note: Another working copy synced %s after your sync.lock. %s\n", resource.ID.Label(), staleSuggestion(resource),
			)
		}
	}
}

// planSummary describes the plan in one line, for example
// "3 changes: 1 prompt push, 2 prompt pull. 1 conflict. 4 in sync."
func planSummary(plan Plan) string {
	type change struct {
		kind syncdomain.Kind
		word int
	}
	counts := make(map[change]int)
	var changes, conflicts, failures, inSync int
	for _, resource := range plan.Resources {
		switch resource.Action {
		case ActionInSync:
			inSync++
		case ActionConflict:
			conflicts++
		case ActionError:
			failures++
		default:
			changes++
			counts[change{kind: resource.ID.Kind, word: actionWordIndex(resource)}]++
		}
	}

	var clauses []string
	if changes != 0 {
		var phrases []string
		for _, key := range slices.SortedFunc(maps.Keys(counts), func(left, right change) int {
			return cmp.Or(strings.Compare(string(left.kind), string(right.kind)), cmp.Compare(left.word, right.word))
		}) {
			phrases = append(phrases, fmt.Sprintf("%d %s %s", counts[key], kindNoun(key.kind), actionWords[key.word].plan))
		}
		clauses = append(clauses, fmt.Sprintf("%s: %s.", countOf(changes, "change"), strings.Join(phrases, ", ")))
	}
	if conflicts != 0 {
		clauses = append(clauses, countOf(conflicts, "conflict")+".")
	}
	if failures != 0 {
		clauses = append(clauses, countOf(failures, "error")+".")
	}
	if len(clauses) == 0 {
		clauses = append(clauses, "No changes.")
	}
	if inSync != 0 {
		clauses = append(clauses, fmt.Sprintf("%d in sync.", inSync))
	}
	return strings.Join(clauses, " ")
}

// resultTotals describes the results in one line, for example
// "3 applied, 1 failed. 4 in sync."
func resultTotals(applied, failed, inSync int) string {
	totals := fmt.Sprintf("%d applied", applied)
	if failed != 0 {
		totals += fmt.Sprintf(", %d failed", failed)
	}
	totals += "."
	if inSync != 0 {
		totals += fmt.Sprintf(" %d in sync.", inSync)
	}
	return totals
}

// actionWord is how a plan row and a result row name one action. A push that
// changes only attachment pins is a repin.
type actionWord struct {
	action Action
	repin  bool
	plan   string
	result string
}

// actionWords lists the word of each action. A plan summary counts the
// changes in this order.
var actionWords = []actionWord{
	{action: ActionCreateServer, plan: "create", result: "created"},
	{action: ActionUpdateServer, plan: "push", result: "pushed"},
	{action: ActionUpdateServer, repin: true, plan: "repin", result: "repinned"},
	{action: ActionUpdateLocal, plan: "pull", result: "pulled"},
	{action: ActionDeleteLocal, plan: "delete", result: "deleted"},
	{action: ActionUpdateManifest, plan: "record", result: "recorded"},
	{action: ActionRemoveManifest, plan: "forget", result: "forgot"},
	{action: ActionConflict, plan: "conflict", result: "not applied"},
	{action: ActionError, plan: "error", result: "not applied"},
}

func actionWordIndex(resource PlannedResource) int {
	repin := repinsOnly(resource)
	return slices.IndexFunc(actionWords, func(word actionWord) bool {
		return word.action == resource.Action && word.repin == repin
	})
}

func actionWordFor(resource PlannedResource) actionWord {
	if index := actionWordIndex(resource); index >= 0 {
		return actionWords[index]
	}
	return actionWord{}
}

// repinsOnly reports whether a push changes only the tool and skill versions
// that LaunchDarkly pins, and no content.
func repinsOnly(resource PlannedResource) bool {
	return resource.Action == ActionUpdateServer &&
		resource.ServerHasStaleAttachmentPins &&
		resource.LocalFingerprint == resource.ServerFingerprint
}

// planReason gives the short reason of a plan row, for example
// "instructions changed in LaunchDarkly".
func planReason(resource PlannedResource) string {
	switch {
	case resource.Action == ActionCreateServer:
		return "new"
	case repinsOnly(resource):
		return repinReason(resource.Diff["attachment versions"])
	case resource.Action == ActionUpdateServer:
		return changedFieldsReason(resource.Diff, "changed")
	case resource.Action == ActionUpdateLocal && resource.Local == nil:
		return "local file missing"
	case resource.Action == ActionUpdateLocal:
		return changedFieldsReason(resource.Diff, "changed in LaunchDarkly")
	case resource.Action == ActionDeleteLocal:
		return "deleted in LaunchDarkly"
	case resource.Action == ActionUpdateManifest:
		return "same as LaunchDarkly"
	case resource.Action == ActionRemoveManifest:
		return "gone on both sides"
	case resource.Action == ActionConflict:
		return "changed on both sides"
	case resource.Action == ActionError:
		return shortCause(resource.Error)
	default:
		return ""
	}
}

// changedFieldsReason names the fields that a diff changes, for example
// "instructions, tools changed".
func changedFieldsReason(diff variationDiffFields, verb string) string {
	var fields []string
	if part, ok := diff["variation"]; ok {
		fields = append(fields, changedKeys(part.Before, part.After)...)
	}
	for _, part := range []string{"tools", "skills"} {
		if _, ok := diff[part]; ok {
			fields = append(fields, part)
		}
	}
	if len(fields) == 0 {
		return verb
	}
	return strings.Join(fields, ", ") + " " + verb
}

// changedKeys returns the top-level keys of two JSON objects whose values differ.
func changedKeys(before, after json.RawMessage) []string {
	var beforeFields, afterFields map[string]json.RawMessage
	_ = json.Unmarshal(before, &beforeFields)
	_ = json.Unmarshal(after, &afterFields)
	keys := slices.Collect(maps.Keys(beforeFields))
	for key := range afterFields {
		if _, ok := beforeFields[key]; !ok {
			keys = append(keys, key)
		}
	}
	keys = slices.DeleteFunc(keys, func(key string) bool { return bytes.Equal(beforeFields[key], afterFields[key]) })
	slices.Sort(keys)
	return keys
}

// repinReason names each tool and skill that a repin moves, for example
// "search to v4".
func repinReason(versions variationFieldDiff) string {
	var latest map[string]map[string]int
	_ = json.Unmarshal(versions.After, &latest)
	var pins []string
	for _, group := range []string{"tools", "skills"} {
		for _, key := range slices.Sorted(maps.Keys(latest[group])) {
			pins = append(pins, fmt.Sprintf("%s to v%d", key, latest[group][key]))
		}
	}
	return strings.Join(pins, ", ")
}

// shortCause returns the first clause of an error message, so that a plan
// row stays short. Plan.BlockingError, which follows the plan, has the full
// error of every row.
func shortCause(message string) string {
	for _, separator := range []string{"; ", ". "} {
		message, _, _ = strings.Cut(message, separator)
	}
	return message
}

// diffSizeText formats a diff size, for example "+3 -1". A zero part is left
// out. When color is true, each part has the color of its diff lines.
func diffSizeText(added, removed int, color bool) string {
	var parts []string
	if added != 0 {
		parts = append(parts, styleDiffLine(fmt.Sprintf("+%d", added), color))
	}
	if removed != 0 {
		parts = append(parts, styleDiffLine(fmt.Sprintf("-%d", removed), color))
	}
	return strings.Join(parts, " ")
}

// reviewRow is one row of a plan or of its results. The cells are the action
// word, the resource, the reason, and for a plan the diff size. diff is the
// rendered diff that follows the row, or empty.
type reviewRow struct {
	kind  syncdomain.Kind
	cells []string
	diff  string
}

// writeRows writes the rows under one heading for each resource kind. The rows
// must be in plan order, which keeps each kind together.
func writeRows(console syncconsole.Writer, outputKind string, headers []string, rows []reviewRow, width int) {
	var groups [][]reviewRow
	for start := 0; start < len(rows); {
		end := start + 1
		for end < len(rows) && rows[end].kind == rows[start].kind {
			end++
		}
		groups = append(groups, rows[start:end])
		start = end
	}
	if outputKind == outputMarkdown {
		writeMarkdownRows(console, headers, groups)
	} else {
		writeTextRows(console, len(headers), groups, width)
	}
}

// writeTextRows lines up the columns of every row. Each diff follows its row,
// with a blank line before the next row.
func writeTextRows(console syncconsole.Writer, columns int, groups [][]reviewRow, width int) {
	widths := make([]int, columns)
	for _, group := range groups {
		for _, row := range group {
			for column, cell := range row.cells {
				widths[column] = max(widths[column], lipgloss.Width(cell))
			}
		}
	}

	afterDiff := false
	for _, group := range groups {
		if afterDiff {
			_ = console.Line("")
		}
		_ = console.Line(reviewHeading(kindHeading(group[0].kind), width))
		for index, row := range group {
			if index > 0 && group[index-1].diff != "" {
				_ = console.Line("")
			}
			var line strings.Builder
			for column, cell := range row.cells {
				line.WriteString(cell)
				if column < len(row.cells)-1 {
					line.WriteString(strings.Repeat(" ", widths[column]-lipgloss.Width(cell)+3))
				}
			}
			_ = console.Printf("  %s\n", strings.TrimRight(line.String(), " "))
			_ = console.Write(row.diff)
		}
		afterDiff = group[len(group)-1].diff != ""
	}
}

// writeMarkdownRows writes one table for each kind. The diffs follow the table.
func writeMarkdownRows(console syncconsole.Writer, headers []string, groups [][]reviewRow) {
	for index, group := range groups {
		if index > 0 {
			_ = console.Line("")
		}
		_ = console.Printf("### %s\n\n| %s |\n|%s\n",
			kindHeading(group[0].kind), strings.Join(headers, " | "), strings.Repeat(" --- |", len(headers)))
		for _, row := range group {
			cells := make([]string, len(row.cells))
			for column, cell := range row.cells {
				cells[column] = strings.ReplaceAll(cell, "|", `\|`)
			}
			_ = console.Printf("| %s |\n", strings.Join(cells, " | "))
		}
		for _, row := range group {
			if row.diff != "" {
				_ = console.Printf("\n#### `%s`\n", row.cells[1])
				_ = console.Write(row.diff)
			}
		}
	}
}

// kindNoun names one resource of a kind in plain text.
func kindNoun(kind syncdomain.Kind) string {
	if kind == syncdomain.KindVariation {
		return "prompt"
	}
	return string(kind)
}

// kindHeading is the heading above the rows of one kind, for example "Prompts".
func kindHeading(kind syncdomain.Kind) string {
	noun := kindNoun(kind)
	return strings.ToUpper(noun[:1]) + noun[1:] + "s"
}

// countOf formats a count with its noun, for example "1 change" or "2 changes".
func countOf(count int, noun string) string {
	if count != 1 {
		noun += "s"
	}
	return fmt.Sprintf("%d %s", count, noun)
}

// staleSuggestion tells the user what to do when another working copy synced
// the resource. It returns an empty string for a resource that is not stale.
func staleSuggestion(resource PlannedResource) string {
	switch {
	case !resource.SyncedElsewhere:
		return ""
	case resource.Action == ActionUpdateLocal:
		return "If the other working copy pushed its change to Git, run git pull before you sync. " +
			"Then your sync.lock does not get a merge conflict."
	case resource.Action == ActionConflict:
		return "Run git pull to get the other change, and then run sync again. " +
			"If the conflict remains, choose a side with --conflict or --resolve."
	default:
		return "Run git pull to get the latest .launchdarkly files and sync.lock, and then run sync again."
	}
}

// reviewHeading colors a heading when the output is a terminal.
func reviewHeading(value string, width int) string {
	if width <= 0 {
		return value
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("67")).Bold(true).Render(value)
}

// variationDiffPresentation is the labels and the direction of a diff.
type variationDiffPresentation struct {
	beforeLabel string
	afterLabel  string
	// reverse shows the local file as "before", because the local file changes.
	reverse bool
}

// diffPresentation returns the labels that describe the change of an action.
func diffPresentation(action Action) variationDiffPresentation {
	switch action {
	case ActionUpdateLocal, ActionDeleteLocal:
		return variationDiffPresentation{beforeLabel: "Local file now", afterLabel: "Local file after sync", reverse: true}
	case ActionCreateServer, ActionUpdateServer:
		return variationDiffPresentation{beforeLabel: "LaunchDarkly now", afterLabel: "LaunchDarkly after sync"}
	default:
		return variationDiffPresentation{beforeLabel: "LaunchDarkly now", afterLabel: "Local file now"}
	}
}

// writeJSON writes indented JSON with a final newline.
func writeJSON(out io.Writer, value any) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("write sync output: %w", err)
	}
	return nil
}
