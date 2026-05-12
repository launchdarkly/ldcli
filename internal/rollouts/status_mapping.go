package rollouts

import (
	"fmt"
	"strings"
	"time"
)

// MapStatus returns the nested StatusBlock for a Rollout — the raw API status, the 5-bucket
// lifecycle kind (active|regressed|reverted|paused|completed) per D-02, and the human-readable
// label string with the reason inline per D-03 (no separate Reason field).
//
// PAPERCUT: PC-005 — status enum mixes lifecycle + action-required + meta states; this
// 13 → 5 mapping is the CLI's normalization layer. The actual papercut anchor entry is
// seeded in Plan 03's API-PAPERCUTS.md.
func MapStatus(r *Rollout) StatusBlock {
	if r == nil {
		return StatusBlock{}
	}
	return StatusBlock{
		Status: r.Status.Status,
		Kind:   mapStatusToKind(r),
		Label:  formatLabel(r),
	}
}

// DeriveStatusBlock is the converter-facing alias for MapStatus. The DTO layer in models.go
// (raw.toRollout()) calls this at the end of conversion so every emitted Rollout has its
// nested Status block populated.
func DeriveStatusBlock(r *Rollout) StatusBlock {
	return MapStatus(r)
}

// mapStatusToKind reduces the 13 raw API statuses to the 5 lifecycle buckets per D-02.
// PAPERCUT: PC-005 — the enum mixes lifecycle + action-required + meta states (e.g.
// `monitoring_stopped` is paused-with-action-required, `archived` is a meta state). The
// CLI normalizes them into the 5 buckets so agents have a stable surface.
func mapStatusToKind(r *Rollout) string {
	switch r.Status.Status {
	case "not_started", "waiting", "in_progress":
		return "active"
	case "monitoring_regressed":
		return "regressed"
	case "monitoring_stopped", "srm_stopped", "archived":
		return "paused"
	case "completed", "manually_completed":
		return "completed"
	case "reverted", "manually_reverted":
		return "reverted"
	default:
		// Unknown / new status — surface as active so the rollout is not silently dropped;
		// label clearly signals the unknown raw value.
		return "active"
	}
}

// formatLabel produces the human-readable label string. The switch mirrors the 13-row
// status table in CONTEXT.md `<specifics>` and RESEARCH.md §Status Mapping.
func formatLabel(r *Rollout) string {
	rule := formatRule(r.RuleIDOrFallthrough)

	switch r.Status.Status {
	case "not_started", "waiting":
		return fmt.Sprintf("Monitoring %s", rule)

	case "in_progress":
		// Sub-condition discrimination:
		// 1. Extension active → "Monitoring extended by {duration}"
		// 2. Progressive (no metric configs) → "Monitoring {rule}"
		// 3. Guarded with min sample NOT reached → "Monitoring {rule} for regressions… (not enough data)"
		// 4. Guarded with min sample reached → "Monitoring {rule} for regressions…"
		if r.ExtensionDurationMillis != nil && *r.ExtensionDurationMillis > 0 {
			return fmt.Sprintf("Monitoring extended by %s", formatDuration(*r.ExtensionDurationMillis))
		}
		if len(r.MetricConfigurations) == 0 {
			return fmt.Sprintf("Monitoring %s", rule)
		}
		if anyMetricBelowMinSample(r.MetricConfigurations) {
			return fmt.Sprintf("Monitoring %s for regressions… (not enough data)", rule)
		}
		return fmt.Sprintf("Monitoring %s for regressions…", rule)

	case "monitoring_regressed":
		metrics := formatMetricNames(r)
		if metrics == "" {
			return fmt.Sprintf("Regressions detected on %s", rule)
		}
		return fmt.Sprintf("Regressions detected on %s for %s", rule, metrics)

	case "monitoring_stopped":
		metrics := formatMetricNames(r)
		alloc := currentAllocationPct(r)
		if metrics == "" {
			return fmt.Sprintf("%s paused at %d%%: regressions detected", rule, alloc)
		}
		return fmt.Sprintf("%s paused at %d%%: regressions detected for %s", rule, alloc, metrics)

	case "srm_stopped":
		alloc := currentAllocationPct(r)
		return fmt.Sprintf("%s paused at %d%%: sample ratio mismatch detected", rule, alloc)

	case "completed":
		return fmt.Sprintf("Monitoring completed on %s", rule)

	case "manually_completed":
		return fmt.Sprintf("%s rolled forward manually", rule)

	case "manually_reverted":
		return fmt.Sprintf("%s rolled back manually", rule)

	case "reverted":
		// Sub-condition discrimination (in order of specificity):
		// 1. Regression event present → "{rule} rolled back automatically after detecting a regression for {metric names}"
		// 2. SRM event present → "{rule} rolled back automatically"
		// 3. Otherwise → "{rule} rolled back due to insufficient sample size"
		if regEvt, ok := findEvent(r.Events, "regression_detected"); ok {
			metrics := metricNamesFromEvents(r.Events)
			if metrics == "" && regEvt.MetricKey != "" {
				metrics = regEvt.MetricKey
			}
			if metrics == "" {
				return fmt.Sprintf("%s rolled back automatically after detecting a regression", rule)
			}
			return fmt.Sprintf("%s rolled back automatically after detecting a regression for %s", rule, metrics)
		}
		if _, ok := findEvent(r.Events, "srm_detected"); ok {
			return fmt.Sprintf("%s rolled back automatically", rule)
		}
		return fmt.Sprintf("%s rolled back due to insufficient sample size", rule)

	case "archived":
		return fmt.Sprintf("Monitoring of %s stopped early", rule)

	default:
		return fmt.Sprintf("Monitoring (unknown status: %s)", r.Status.Status)
	}
}

// formatRule renders the {rule} placeholder. "fallthrough" → "the default rule"; any other
// value → "rule {value}".
func formatRule(ruleIDOrFallthrough string) string {
	if ruleIDOrFallthrough == "" || ruleIDOrFallthrough == "fallthrough" {
		return "the default rule"
	}
	return fmt.Sprintf("rule %s", ruleIDOrFallthrough)
}

// currentAllocationPct returns the percentage allocation of the current stage. The API
// represents allocation as basis points (0..100000), so we divide by 1000 for percent.
// Falls back to 0 when the rollout has no stages (defensive — keeps the label well-formed).
func currentAllocationPct(r *Rollout) int {
	if len(r.Stages) == 0 {
		return 0
	}
	idx := r.LatestStageIndex
	if idx < 0 || idx >= len(r.Stages) {
		idx = len(r.Stages) - 1
	}
	return r.Stages[idx].Allocation / 1000
}

// formatMetricNames extracts comma-joined metric keys from MetricConfigurations where
// Status == "regressed". Falls back to event-derived names when no metric-config matches.
func formatMetricNames(r *Rollout) string {
	var names []string
	for _, mc := range r.MetricConfigurations {
		if mc.Status == "regressed" {
			names = append(names, mc.MetricKey)
		}
	}
	if len(names) > 0 {
		return strings.Join(names, ", ")
	}
	return metricNamesFromEvents(r.Events)
}

// metricNamesFromEvents joins MetricKey values from events with Kind == "regression_detected".
func metricNamesFromEvents(events []Event) string {
	var names []string
	for _, ev := range events {
		if ev.Kind == "regression_detected" && ev.MetricKey != "" {
			names = append(names, ev.MetricKey)
		}
	}
	return strings.Join(names, ", ")
}

// findEvent returns the first event with the given Kind, plus a found bool.
func findEvent(events []Event, kind string) (Event, bool) {
	for _, ev := range events {
		if ev.Kind == kind {
			return ev, true
		}
	}
	return Event{}, false
}

// anyMetricBelowMinSample reports whether at least one metric config indicates the minimum
// sample size has not yet been reached. The API surfaces this via a per-metric Status value
// of "not_enough_data" (or similar). Defensive: if no metric configs have a Status set, the
// answer is false (we cannot tell, default to "min reached").
func anyMetricBelowMinSample(mcs []MetricConfiguration) bool {
	for _, mc := range mcs {
		if mc.Status == "not_enough_data" {
			return true
		}
	}
	return false
}

// formatDuration converts a millisecond duration to the Go-style unit-bearing string
// (e.g. 900000 → "15m0s") expected by AGENT-04. PAPERCUT: PC-014 — the API exposes
// durations only as int64 millis with no human-readable form, so the CLI computes one.
func formatDuration(millis int64) string {
	return (time.Duration(millis) * time.Millisecond).String()
}
