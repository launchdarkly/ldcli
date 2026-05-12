package rollouts

import "time"

// SchemaVersionV1Beta1 is the envelope version tag for every JSON response emitted by the
// `flags rollouts-beta` command tree. Per FOUND-03 / D-07, the schema version is part of the
// envelope contract; consumers (agents, pipelines) can branch on it across releases.
const SchemaVersionV1Beta1 = "rollouts.v1beta1"

// Rollout is the CLI-side representation of an automated release. Phase 1 defines the type
// shape (D-02 nested status; D-03 no structured reason); Plan 02 fills in the converter from
// the raw API response.
type Rollout struct {
	ID                      string                `json:"id"`
	FlagKey                 string                `json:"flagKey"`
	Kind                    string                `json:"kind"` // rollout kind: "guarded" | "progressive"
	EnvironmentID           string                `json:"environmentId,omitempty"`
	EnvironmentKey          string                `json:"environmentKey,omitempty"`
	OriginalVariationID     string                `json:"originalVariationId"`
	TargetVariationID       string                `json:"targetVariationId"`
	RandomizationUnit       string                `json:"randomizationUnit"`
	RuleIDOrFallthrough     string                `json:"ruleIdOrFallthrough"`
	Status                  StatusBlock           `json:"status"` // NESTED — D-02 three-field model
	CreatedAt               time.Time             `json:"createdAt"`
	StartedAt               *time.Time            `json:"startedAt,omitempty"`
	EndedAt                 *time.Time            `json:"endedAt,omitempty"`
	LatestStageIndex        int                   `json:"latestStageIndex"`
	ExtensionDurationMillis *int64                `json:"extensionDurationMillis,omitempty"`
	Stages                  []Stage               `json:"stages,omitempty"`
	Events                  []Event               `json:"events,omitempty"`
	MetricConfigurations    []MetricConfiguration `json:"metricConfigurations,omitempty"`
	Links                   map[string]Link       `json:"_links,omitempty"`
}

// StatusBlock is the nested three-field status model per D-02. `Status` is the raw API enum
// (e.g. "monitoring_regressed"); `Kind` is one of the five lifecycle buckets
// (active|regressed|reverted|paused|completed); `Label` is the human-readable copy with reason
// inline. Per D-03 there is intentionally no `Reason` field — agents read `Label`.
type StatusBlock struct {
	Status string `json:"status"`
	Kind   string `json:"kind"`
	Label  string `json:"label"`
}

// Stage represents one bucket in a progressive rollout's allocation timeline.
type Stage struct {
	StageIndex      int        `json:"stageIndex"`
	Allocation      int        `json:"allocation"`
	DurationMillis  int64      `json:"durationMillis,omitempty"`
	Duration        string     `json:"duration,omitempty"`
	StartedAt       *time.Time `json:"startedAt,omitempty"`
	SafeRollForward *bool      `json:"safeRollForward,omitempty"`
}

// Event captures a single transition in a rollout's lifecycle. Event.Kind values include
// `regression_detected`, `srm_detected`, and `minimum_monitoring_window_expired` among others.
// For regression events, MetricKey identifies the regressing metric so status_mapping can
// surface it in the human-readable label.
type Event struct {
	Kind       string    `json:"kind"`
	CreatedAt  time.Time `json:"createdAt"`
	StageIndex int       `json:"stageIndex,omitempty"`
	MetricKey  string    `json:"metricKey,omitempty"`
	Message    string    `json:"message,omitempty"`
}

// MetricConfiguration describes one metric watched during a guarded rollout. MinSampleSize
// is the threshold below which the "not enough data" label is surfaced; Status is the
// per-metric monitoring state (`ok` | `regressed` | `regression_dismissed` | `not_enough_data`).
type MetricConfiguration struct {
	MetricKey     string `json:"metricKey"`
	Kind          string `json:"kind,omitempty"`
	MinSampleSize int    `json:"minSampleSize,omitempty"`
	AutoRollback  bool   `json:"autoRollback,omitempty"`
	Status        string `json:"status,omitempty"`
}

// Link is the standard LD API HATEOAS link envelope.
type Link struct {
	Href string `json:"href"`
	Type string `json:"type,omitempty"`
}

// RolloutList is the collection payload returned by Client.List.
type RolloutList struct {
	Items []Rollout       `json:"items"`
	Links map[string]Link `json:"_links,omitempty"`
}

// Envelope is the versioned JSON wrapper emitted on stdout for every `rollouts-beta` command.
// `Data` carries the typed payload (`*RolloutList` for list, `*Rollout` for get/status). For
// error envelopes Data is nil and Error is populated.
type Envelope struct {
	SchemaVersion string         `json:"schemaVersion"`
	Kind          string         `json:"kind"`
	Data          interface{}    `json:"data,omitempty"`
	Meta          *EnvelopeMeta  `json:"meta,omitempty"`
	Error         *EnvelopeError `json:"error,omitempty"`
}

// EnvelopeError carries machine-parseable error context. `Code` is one of the documented
// ErrCode* constants in errors.go (D-08, FOUND-08); `NextAction` is a human-readable suggestion
// for what to do next; `Details` is a free-form map for upstream specifics.
type EnvelopeError struct {
	Code       string                 `json:"code"`
	Message    string                 `json:"message"`
	NextAction string                 `json:"nextAction,omitempty"`
	Details    map[string]interface{} `json:"details,omitempty"`
}

// EnvelopeMeta carries response-level metadata that is not part of the data payload itself.
type EnvelopeMeta struct {
	FetchedAt        time.Time `json:"fetchedAt,omitempty"`
	UIURL            string    `json:"uiURL,omitempty"`
	Warnings         []string  `json:"warnings,omitempty"`
	AvailableActions []string  `json:"availableActions,omitempty"`
}
