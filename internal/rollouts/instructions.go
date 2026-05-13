package rollouts

// SemanticPatch is the JSON envelope used by LaunchDarkly's flag semantic-patch endpoint.
// EnvironmentKey is required by the server to route the patch to the correct environment
// (RESEARCH Pitfall 2; verified against gonfalon instruction_start_automated_release.go).
// The `Instructions` slice carries one or more typed instruction structs; the upstream API
// matches each instruction by its "kind" tag (see StartInstruction, StopInstruction, etc.).
//
// Phase 1: declared as a skeleton only — the Client interface does not yet expose any mutation
// method (D-08). Phase 2 fleshes out StartInstruction; Phase 4 fleshes out the rest.
type SemanticPatch struct {
	EnvironmentKey string        `json:"environmentKey"`
	Comment        string        `json:"comment,omitempty"`
	Instructions   []interface{} `json:"instructions"`
}

// StartInstruction kicks off an automated rollout.
//
// PAPERCUT: PC-012 — `releaseKind` in the request body vs `kind` in the response.
//
// All field names match the gonfalon instruction shape exactly (verified against
// instruction_start_automated_release.go). The CLI command layer translates user-facing
// flag values (percent allocations, Go duration strings, pause/revert verbs) into the API
// wire format here.
type StartInstruction struct {
	Kind        string `json:"kind"`        // always "startAutomatedRelease"
	ReleaseKind string `json:"releaseKind"` // "guarded" | "progressive" (inferred per D-05)

	// UUID _id only — NOT variation key (RESEARCH Q1)
	// PAPERCUT: PC-013 — `originalVariationId` (not `controlVariationId`)
	OriginalVariationID string `json:"originalVariationId"`

	// UUID _id only — NOT variation key (RESEARCH Q1)
	TargetVariationID string `json:"targetVariationId"`

	RandomizationUnit string       `json:"randomizationUnit"`
	Stages            []StageInput `json:"stages"`

	// PAPERCUT: PC-010 — `Metrics` and `MetricMonitoringPreferences` are parallel collections
	// keyed by metric key; the CLI command layer reconciles them in a single pass (D-04).
	Metrics                     []MetricSource                  `json:"metrics,omitempty"`
	MetricMonitoringPreferences map[string]MetricMonitoringPref `json:"metricMonitoringPreferences,omitempty"`

	// D-07: empty = fallthrough rule
	RuleID string `json:"ruleId,omitempty"`
}

// StageInput represents a single rollout stage.
//
// PAPERCUT: PC-014 — `durationMillis` is int64 millis; the CLI converts from a Go duration
// string at parse time (D-03).
type StageInput struct {
	Allocation     int   `json:"allocation"`     // basis points: 25% → 25000 (D-02: multiply percent × 1000)
	DurationMillis int64 `json:"durationMillis"` // D-03: time.ParseDuration(s).Milliseconds()
}

// MetricSource identifies a metric to monitor during a guarded rollout.
// Metric group support is deferred to v1.1 per D-06.
type MetricSource struct {
	Key string `json:"key"`
}

// MetricMonitoringPref controls the auto-rollback behavior for a monitored metric.
//
// D-04: false = pause-on-regression; true = revert-on-regression.
type MetricMonitoringPref struct {
	AutoRollback bool `json:"autoRollback"`
}

// StopInstruction terminates an in-progress rollout. Phase 4 fleshes this out.
type StopInstruction struct {
	Kind string `json:"kind"`
}

// DismissRegressionInstruction clears a detected-regression flag on a guarded rollout so it
// can resume. Phase 4 fleshes this out.
type DismissRegressionInstruction struct {
	Kind string `json:"kind"`
}
