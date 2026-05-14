package rollouts

import (
	"bytes"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/launchdarkly/ldcli/internal/rollouts"
)

// RenderRolloutListPlaintext returns the plaintext rendering of a RolloutList.
//
// Default form (D-06): a 5-column aligned table with the headers
//
//	ID  KIND  ENVIRONMENT  STATE  STARTED
//
// where STATE is the 5-bucket lifecycle (status.kind) and STARTED is the rollout's
// startedAt (falling back to createdAt when startedAt is unset).
//
// --detailed form (D-06): multi-line per-record records with the full field set including
// the original / target variation IDs and the raw API status. JSON output is ALWAYS the
// full field set regardless of --detailed (D-07); --detailed only changes plaintext.
//
// The renderer uses text/tabwriter directly. internal/output/table.go has a similar helper
// but it operates on a `resource` map type (interface{}-keyed), which would force us to
// flatten typed Rollout values into a map. text/tabwriter at the call site keeps the typed
// path clean.
func RenderRolloutListPlaintext(list *rollouts.RolloutList, detailed bool) string {
	if list == nil || len(list.Items) == 0 {
		return "No rollouts found.\n"
	}
	if detailed {
		return renderDetailed(list)
	}
	return renderTable(list)
}

// renderTable writes the default 5-column table.
func renderTable(list *rollouts.RolloutList) string {
	var buf bytes.Buffer
	w := tabwriter.NewWriter(&buf, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "ID\tKIND\tENVIRONMENT\tSTATE\tSTARTED")
	for _, r := range list.Items {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			emptyDash(r.ID),
			emptyDash(r.Kind),
			emptyDash(r.EnvironmentKey),
			emptyDash(r.Status.Kind),
			startedOrCreated(r),
		)
	}
	_ = w.Flush()
	return buf.String()
}

// renderDetailed writes the per-record multi-line form, one record per rollout separated
// by a `---` divider. The field set follows the D-06 detailed layout.
func renderDetailed(list *rollouts.RolloutList) string {
	var b strings.Builder
	for _, r := range list.Items {
		fmt.Fprintf(&b, "ID:           %s\n", emptyDash(r.ID))
		fmt.Fprintf(&b, "Kind:         %s\n", emptyDash(r.Kind))
		fmt.Fprintf(&b, "Environment:  %s\n", emptyDash(r.EnvironmentKey))
		fmt.Fprintf(&b, "State:        %s\n", emptyDash(r.Status.Kind))
		fmt.Fprintf(&b, "Label:        %s\n", emptyDash(r.Status.Label))
		fmt.Fprintf(&b, "Started:      %s\n", timePtrOrDash(r.StartedAt))
		fmt.Fprintf(&b, "Ended:        %s\n", timePtrOrDash(r.EndedAt))
		fmt.Fprintf(&b, "Stage:        %s\n", formatStage(r))
		fmt.Fprintf(&b, "Target var:   %s\n", emptyDash(r.TargetVariationID))
		fmt.Fprintf(&b, "Original var: %s\n", emptyDash(r.OriginalVariationID))
		fmt.Fprintf(&b, "Raw status:   %s\n", emptyDash(r.Status.Status))
		fmt.Fprintln(&b, "---")
	}
	return b.String()
}

// startedOrCreated returns the RFC 3339 startedAt timestamp when present; otherwise the
// createdAt timestamp. If both are zero, an em-dash placeholder is returned.
func startedOrCreated(r rollouts.Rollout) string {
	if r.StartedAt != nil {
		return r.StartedAt.UTC().Format(time.RFC3339)
	}
	if !r.CreatedAt.IsZero() {
		return r.CreatedAt.UTC().Format(time.RFC3339)
	}
	return "—"
}

// timePtrOrDash returns the RFC 3339 timestamp or an em-dash placeholder when nil/zero.
func timePtrOrDash(t *time.Time) string {
	if t == nil || t.IsZero() {
		return "—"
	}
	return t.UTC().Format(time.RFC3339)
}

// emptyDash returns the value or an em-dash placeholder when empty.
func emptyDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// RenderRolloutPlaintext returns a concise single-rollout summary for plaintext output from
// the start command. Shows the rollout ID, kind, environment, and initial status so the
// operator knows what was created. JSON output always emits the full envelope (D-07).
func RenderRolloutPlaintext(r *rollouts.Rollout) string {
	if r == nil {
		return "Rollout started.\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Started rollout %s (%s) in environment %s\n", emptyDash(r.ID), emptyDash(r.Kind), emptyDash(r.EnvironmentKey))
	fmt.Fprintf(&b, "Status: %s\n", emptyDash(r.Status.Kind))
	if len(r.Stages) > 0 {
		fmt.Fprintf(&b, "Stages: %s\n", formatStage(*r))
	}
	return b.String()
}

// formatStage renders "<current> of <total> (<alloc>%)" where current is LatestStageIndex+1,
// total is len(Stages), and alloc is the current stage's Allocation / 1000 (allocations are
// represented as parts-per-thousand). When stage data is missing, an em-dash is returned.
func formatStage(r rollouts.Rollout) string {
	total := len(r.Stages)
	if total == 0 {
		return "—"
	}
	current := r.LatestStageIndex + 1
	if current < 1 || current > total {
		current = 1
	}
	alloc := r.Stages[r.LatestStageIndex].Allocation / 1000
	return fmt.Sprintf("%d of %d (%d%%)", current, total, alloc)
}
