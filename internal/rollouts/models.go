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

// --- Raw API DTO layer ----------------------------------------------------------------
//
// The raw* types mirror the upstream `automated-releases` API exactly, including its
// int64 unix-millis timestamps. The converters (toRolloutList / toRollout / toStage)
// translate those to the CLI-side shapes with time.Time + Go-duration strings (AGENT-04)
// and call DeriveStatusBlock to populate the nested Status block (D-02). This keeps
// the API/CLI boundary explicit per RESEARCH.md §"rawRolloutList vs RolloutList" — no
// time.Time field ever appears in a raw* type.

type rawRolloutList struct {
	Items []rawRollout    `json:"items"`
	Links map[string]Link `json:"_links,omitempty"`
}

type rawRollout struct {
	ID                      string                `json:"id"`
	FlagKey                 string                `json:"flagKey"`
	Kind                    string                `json:"kind"` // PAPERCUT: PC-013 may surface here if API uses `controlVariationId` legacy name; converter normalizes.
	Status                  string                `json:"status"`
	EnvironmentID           string                `json:"environmentId,omitempty"`
	EnvironmentKey          string                `json:"environmentKey,omitempty"`
	OriginalVariationID     string                `json:"originalVariationId"`
	TargetVariationID       string                `json:"targetVariationId"`
	RandomizationUnit       string                `json:"randomizationUnit"`
	RuleIDOrFallthrough     string                `json:"ruleIdOrFallthrough"`
	CreatedAt               int64                 `json:"createdAt"`
	StartedAtMillis         *int64                `json:"startedAtMillis,omitempty"`
	EndedAtMillis           *int64                `json:"endedAtMillis,omitempty"`
	LatestStageIndex        int                   `json:"latestStageIndex"`
	ExtensionDurationMillis *int64                `json:"extensionDurationMillis,omitempty"`
	Stages                  []rawStage            `json:"stages,omitempty"`
	Events                  []rawEvent            `json:"events,omitempty"`
	MetricConfigurations    []MetricConfiguration `json:"metricConfigurations,omitempty"`
	Links                   map[string]Link       `json:"_links,omitempty"`
}

// rawEvent mirrors Event but keeps CreatedAt as int64 millis on the wire — the
// API emits unix-millis ints, not RFC 3339 strings, so a plain `time.Time` field
// fails json.Unmarshal with "input is not a JSON string". Converted to time.Time
// in toEvent().
type rawEvent struct {
	Kind       string `json:"kind"`
	CreatedAt  int64  `json:"createdAt"`
	StageIndex int    `json:"stageIndex,omitempty"`
	MetricKey  string `json:"metricKey,omitempty"`
	Message    string `json:"message,omitempty"`
}

func (raw rawEvent) toEvent() Event {
	return Event{
		Kind:       raw.Kind,
		CreatedAt:  time.Unix(0, raw.CreatedAt*int64(time.Millisecond)).UTC(),
		StageIndex: raw.StageIndex,
		MetricKey:  raw.MetricKey,
		Message:    raw.Message,
	}
}

type rawStage struct {
	StageIndex      int    `json:"stageIndex"`
	Allocation      int    `json:"allocation"`
	DurationMillis  int64  `json:"durationMillis"`
	StartedAtMillis *int64 `json:"startedAtMillis,omitempty"`
	SafeRollForward *bool  `json:"safeRollForward,omitempty"`
}

// millisToTimePtr converts an *int64 unix-millis value to *time.Time in UTC. Returns nil
// when the input is nil or zero (so absent timestamps stay absent in the CLI shape).
func millisToTimePtr(ms *int64) *time.Time {
	if ms == nil || *ms == 0 {
		return nil
	}
	t := time.Unix(0, *ms*int64(time.Millisecond)).UTC()
	return &t
}

// toRolloutList converts the raw API envelope into the CLI-side RolloutList, running each
// item through toRollout so every Rollout has its Status block decorated.
func (raw rawRolloutList) toRolloutList() *RolloutList {
	items := make([]Rollout, 0, len(raw.Items))
	for _, r := range raw.Items {
		items = append(items, r.toRollout())
	}
	return &RolloutList{Items: items, Links: raw.Links}
}

// toRollout converts a raw API rollout to the CLI shape: timestamps become time.Time UTC,
// the stage list is converted, and the nested Status block is derived from the raw status
// + sub-condition discriminators via DeriveStatusBlock.
func (raw rawRollout) toRollout() Rollout {
	stages := make([]Stage, 0, len(raw.Stages))
	for _, s := range raw.Stages {
		stages = append(stages, s.toStage())
	}

	events := make([]Event, 0, len(raw.Events))
	for _, e := range raw.Events {
		events = append(events, e.toEvent())
	}

	r := Rollout{
		ID:                      raw.ID,
		FlagKey:                 raw.FlagKey,
		Kind:                    raw.Kind,
		EnvironmentID:           raw.EnvironmentID,
		EnvironmentKey:          raw.EnvironmentKey,
		OriginalVariationID:     raw.OriginalVariationID,
		TargetVariationID:       raw.TargetVariationID,
		RandomizationUnit:       raw.RandomizationUnit,
		RuleIDOrFallthrough:     raw.RuleIDOrFallthrough,
		CreatedAt:               time.Unix(0, raw.CreatedAt*int64(time.Millisecond)).UTC(),
		StartedAt:               millisToTimePtr(raw.StartedAtMillis),
		EndedAt:                 millisToTimePtr(raw.EndedAtMillis),
		LatestStageIndex:        raw.LatestStageIndex,
		ExtensionDurationMillis: raw.ExtensionDurationMillis,
		Stages:                  stages,
		Events:                  events,
		MetricConfigurations:    raw.MetricConfigurations,
		Links:                   raw.Links,
		// Status.Status is needed before DeriveStatusBlock can run.
		Status: StatusBlock{Status: raw.Status},
	}
	// Derive the full Status block (Kind + Label) using the fully-populated rollout.
	r.Status = DeriveStatusBlock(&r)
	return r
}

// toStage converts a raw stage entry into the CLI Stage shape. The duration is exposed in
// both raw (DurationMillis) and unit-bearing (Duration, e.g. "15m0s") forms per AGENT-04.
// PAPERCUT: PC-014 — the API exposes durations only as int64 millis; the CLI computes the
// Go-style string here so agents and humans both get a readable form.
func (raw rawStage) toStage() Stage {
	return Stage{
		StageIndex:      raw.StageIndex,
		Allocation:      raw.Allocation,
		DurationMillis:  raw.DurationMillis,
		Duration:        (time.Duration(raw.DurationMillis) * time.Millisecond).String(),
		StartedAt:       millisToTimePtr(raw.StartedAtMillis),
		SafeRollForward: raw.SafeRollForward,
	}
}
