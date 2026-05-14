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

// RenderRolloutStopPlaintext returns a concise post-stop confirmation for plaintext output.
// Mirrors RenderRolloutPlaintext's shape but uses the "Stopped rollout" header. The
// post-stop Rollout's Status.Kind should reflect the terminal state — typically "completed"
// or "reverted" depending on which variation was chosen — but the exact mapping is observed
// empirically during Plan 04-03 smoke (the API team has not documented the post-stop status
// enumeration).
func RenderRolloutStopPlaintext(r *rollouts.Rollout) string {
	if r == nil {
		return "Rollout stopped.\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Stopped rollout %s (%s) in environment %s\n", emptyDash(r.ID), emptyDash(r.Kind), emptyDash(r.EnvironmentKey))
	fmt.Fprintf(&b, "Status: %s\n", emptyDash(r.Status.Kind))
	if r.Status.Label != "" {
		fmt.Fprintf(&b, "Label: %s\n", r.Status.Label)
	}
	return b.String()
}

// RenderRolloutDismissPlaintext returns a concise post-dismiss confirmation for plaintext
// output. The post-dismiss `Status.Kind` should typically be 'active' (rollout resumed)
// but may still read 'regressed' when the eventual-consistency window has not yet
// propagated; in that case the command layer surfaces a `meta.warnings` line to stderr
// separately (JSON consumers see warnings in meta.warnings instead).
func RenderRolloutDismissPlaintext(r *rollouts.Rollout) string {
	if r == nil {
		return "Regression dismissed.\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Dismissed regression on rollout %s (%s) in environment %s\n", emptyDash(r.ID), emptyDash(r.Kind), emptyDash(r.EnvironmentKey))
	fmt.Fprintf(&b, "Status: %s\n", emptyDash(r.Status.Kind))
	if r.Status.Label != "" {
		fmt.Fprintf(&b, "Label: %s\n", r.Status.Label)
	}
	return b.String()
}

// timeOrDash returns the RFC 3339 timestamp or an em-dash placeholder when zero.
// Companion to timePtrOrDash for non-pointer time.Time fields (e.g., Rollout.CreatedAt).
func timeOrDash(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.UTC().Format(time.RFC3339)
}

// RenderRolloutStatusPlaintext renders the sectioned-block plaintext output for the status
// verb per D-07. Sections: Overview / Stages / Metrics / Events.
//
// Stage markers reflect rollout progression: `✓` for completed stages, `→` for the current
// stage, ` ` for pending stages. When the rollout has terminated (status.kind ∈ {completed,
// reverted}) all stages render as completed regardless of LatestStageIndex.
//
// Stage duration prefers Stage.Duration (Go-style "1h30m" produced by toStage per AGENT-04
// / PC-014); falls back to "<millis>ms" only when Duration is empty.
func RenderRolloutStatusPlaintext(r *rollouts.Rollout) string {
	if r == nil {
		return "No rollout.\n"
	}

	var b strings.Builder

	// --- Overview ---
	fmt.Fprintf(&b, "Rollout: %s\n", emptyDash(r.ID))
	fmt.Fprintf(&b, "Flag: %s            Env: %s\n", emptyDash(r.FlagKey), emptyDash(r.EnvironmentKey))
	fmt.Fprintf(&b, "Kind: %s   State: %s\n", emptyDash(r.Kind), emptyDash(r.Status.Kind))
	fmt.Fprintf(&b, "Label: %s\n", emptyDash(r.Status.Label))
	fmt.Fprintf(&b, "Created: %s\n", timeOrDash(r.CreatedAt))
	fmt.Fprintf(&b, "Started: %s           Ended: %s\n", timePtrOrDash(r.StartedAt), timePtrOrDash(r.EndedAt))
	fmt.Fprintf(&b, "Target var: %s              Original var: %s\n",
		emptyDash(r.TargetVariationID), emptyDash(r.OriginalVariationID))

	// --- Stages ---
	b.WriteString("\nStages:\n")
	if len(r.Stages) == 0 {
		b.WriteString("  (no stages)\n")
	} else {
		// If the rollout has terminated, all stages render as completed.
		terminal := r.Status.Kind == "completed" || r.Status.Kind == "reverted"
		stagesBuf := bytes.Buffer{}
		w := tabwriter.NewWriter(&stagesBuf, 0, 0, 2, ' ', 0)
		for i, s := range r.Stages {
			marker, stageState := stageMarkerAndState(i, r.LatestStageIndex, terminal)
			alloc := s.Allocation / 1000 // basis-points → percent (PC-014 conversion via toStage)
			dur := s.Duration
			if dur == "" {
				dur = fmt.Sprintf("%dms", s.DurationMillis)
			}
			fmt.Fprintf(w, "  [%s]\t%d%%\t%s\t%s\n", marker, alloc, dur, stageState)
		}
		_ = w.Flush()
		b.WriteString(stagesBuf.String())
	}

	// --- Metrics ---
	b.WriteString("\nMetrics:\n")
	if len(r.MetricConfigurations) == 0 {
		b.WriteString("  (no metrics monitored)\n")
	} else {
		resultsByKey := make(map[string]*rollouts.MetricResult, len(r.MetricResults))
		for i := range r.MetricResults {
			resultsByKey[r.MetricResults[i].MetricKey] = &r.MetricResults[i]
		}
		metricsBuf := bytes.Buffer{}
		w := tabwriter.NewWriter(&metricsBuf, 0, 0, 2, ' ', 0)
		for _, m := range r.MetricConfigurations {
			fmt.Fprintf(w, "  %s\t%s\tauto-rollback: %v\n",
				emptyDash(m.MetricKey),
				emptyDash(m.Status),
				m.AutoRollback,
			)
			if mr := resultsByKey[m.MetricKey]; mr != nil {
				fmt.Fprintf(w, "    control\t%s\n", formatMetricValue(mr.ControlResult))
				fmt.Fprintf(w, "    treatment\t%s\n", formatMetricValue(mr.TreatmentResult))
				fmt.Fprintf(w, "    difference\t%s\n", formatMetricRange(mr.Difference))
			}
		}
		_ = w.Flush()
		b.WriteString(metricsBuf.String())
	}

	// --- Events ---
	b.WriteString("\nEvents:\n")
	if len(r.Events) == 0 {
		b.WriteString("  (no events)\n")
	} else {
		eventsBuf := bytes.Buffer{}
		w := tabwriter.NewWriter(&eventsBuf, 0, 0, 2, ' ', 0)
		for _, e := range r.Events {
			fmt.Fprintf(w, "  %s\t%s\t%s\n",
				timeOrDash(e.CreatedAt),
				emptyDash(e.Kind),
				emptyDash(e.MetricKey),
			)
		}
		_ = w.Flush()
		b.WriteString(eventsBuf.String())
	}

	return b.String()
}

// stageMarkerAndState returns the per-stage marker glyph and short state label given the
// stage's index, the rollout's current LatestStageIndex, and whether the rollout has
// terminated. Terminal rollouts show every stage as completed regardless of LatestStageIndex.
func stageMarkerAndState(idx, latest int, terminal bool) (string, string) {
	if terminal {
		return "✓", "completed"
	}
	switch {
	case idx < latest:
		return "✓", "completed"
	case idx == latest:
		return "→", "in progress"
	default:
		return " ", "pending"
	}
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

// formatMetricValue renders a per-arm estimate as "value (lower–upper)" using credible
// interval bounds when present, or just "value" otherwise. Em-dash when nil.
func formatMetricValue(e *rollouts.MetricResultEstimate) string {
	if e == nil {
		return "—"
	}
	if e.CredibleInterval != nil {
		return fmt.Sprintf("%.4f (%.4f–%.4f)", e.Value, e.CredibleInterval.Lower, e.CredibleInterval.Upper)
	}
	return fmt.Sprintf("%.4f", e.Value)
}

// formatMetricRange renders an absolute or relative difference as "estimate (lower–upper)".
func formatMetricRange(r *rollouts.MetricResultRange) string {
	if r == nil {
		return "—"
	}
	return fmt.Sprintf("%+.4f (%+.4f–%+.4f)", r.Estimate, r.Lower, r.Upper)
}
