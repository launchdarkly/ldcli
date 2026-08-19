package enrich

import "time"

// FlagDetail is the enriched view of a flag, combining scan data with LD API data.
type FlagDetail struct {
	Key          string               `json:"key"`
	Name         string               `json:"name"`
	Description  string               `json:"description,omitempty"`
	Temporary    bool                 `json:"temporary"`
	Archived     bool                 `json:"archived"`
	CreationDate time.Time            `json:"creationDate"`
	Tags         []string             `json:"tags,omitempty"`
	Kind         string               `json:"kind"` // boolean, multivariate
	Environments map[string]EnvStatus `json:"environments"`
	CodeRefs     int                  `json:"codeRefs"`
	CallSites    int                  `json:"callSites"`
	StaleState   string               `json:"staleState"`
	// RecommendedValue is the variation value a readyForCodeRemoval flag can be
	// hardcoded to (the single variation its relevant envs serve). Only set when
	// StaleState == "readyForCodeRemoval"; omitted otherwise.
	RecommendedValue any         `json:"recommendedValue,omitempty"`
	Variations       []Variation `json:"variations"`
}

type EnvStatus struct {
	On             bool      `json:"on"`
	LastModified   time.Time `json:"lastModified"`
	LastEvaluation time.Time `json:"lastEvaluation,omitempty"`
	FlagStatus     string    `json:"flagStatus"` // active, inactive, launched, new

	// Targeting complexity
	Targeting TargetingSummary `json:"targeting"`

	// Exposure: which variations are being served and how (from targeting config)
	Exposure ExposureSummary `json:"exposure"`

	// Evaluation counts: total SDK evaluation calls (NOT unique contexts).
	Evaluations EvaluationCounts `json:"evaluations"`

	// UniqueContexts: true unique-context counts (the "exposures" a guarded
	// release actually gates on), per context kind, over the eval window.
	// Only populated when exposure fetching is enabled (-exposures); omitted
	// otherwise.
	UniqueContexts *UniqueContextCounts `json:"uniqueContexts,omitempty"`
}

// UniqueContextCounts holds unique-context cardinality per context kind, over
// a window. Unlike EvaluationCounts (total SDK calls), each context is counted
// once — this is the "exposures" figure LD's guarded-release UI reports.
type UniqueContextCounts struct {
	Window        string                       `json:"window"`
	ByContextKind map[string]UniqueContextStat `json:"byContextKind"`
}

type UniqueContextStat struct {
	Count     int64 `json:"count"`
	IsSampled bool  `json:"isSampled"`
}

type EvaluationCounts struct {
	Total7d     int64             `json:"total7d"`
	Daily       []DailyEvaluation `json:"daily,omitempty"`
	ByVariation map[string]int64  `json:"byVariation,omitempty"`
}

type DailyEvaluation struct {
	Time  time.Time `json:"time"`
	Count int64     `json:"count"`
}

type TargetingSummary struct {
	RuleCount            int  `json:"ruleCount"`
	TargetCount          int  `json:"targetCount"`        // individual user targets
	ContextTargetCount   int  `json:"contextTargetCount"` // context-based targets
	PrerequisiteCount    int  `json:"prerequisiteCount"`
	IsSimpleToggle       bool `json:"isSimpleToggle"` // on + no rules/targets, single fallthrough variation
	FallthroughVariation *int `json:"fallthroughVariation,omitempty"`
	OffVariation         *int `json:"offVariation,omitempty"`
}

type ExposureSummary struct {
	// Per-variation summary from _summary field
	VariationExposure []VariationExposure `json:"variationExposure,omitempty"`
}

type VariationExposure struct {
	VariationIndex int  `json:"variationIndex"`
	Rules          int  `json:"rules"`
	Targets        int  `json:"targets"`
	ContextTargets int  `json:"contextTargets"`
	IsFallthrough  bool `json:"isFallthrough"`
	IsOff          bool `json:"isOff"`
}

type Variation struct {
	Value       any    `json:"value"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

// API response types

type apiFlagResponse struct {
	Key          string                  `json:"key"`
	Name         string                  `json:"name"`
	Description  string                  `json:"description"`
	Temporary    bool                    `json:"temporary"`
	Archived     bool                    `json:"archived"`
	CreationDate int64                   `json:"creationDate"`
	Tags         []string                `json:"tags"`
	Kind         string                  `json:"kind"`
	Variations   []apiVariation          `json:"variations"`
	Environments map[string]apiEnvConfig `json:"environments"`
}

type apiVariation struct {
	ID          string `json:"_id"`
	Value       any    `json:"value"`
	Name        string `json:"name"`
	Description string `json:"description"`
	ValueHash   string `json:"valueHash"`
}

type apiEnvConfig struct {
	On             bool              `json:"on"`
	LastModified   int64             `json:"lastModified"`
	Rules          []apiRule         `json:"rules"`
	Targets        []apiTarget       `json:"targets"`
	ContextTargets []apiTarget       `json:"contextTargets"`
	Prerequisites  []apiPrerequisite `json:"prerequisites"`
	Fallthrough    apiFallthrough    `json:"fallthrough"`
	OffVariation   *int              `json:"offVariation"`
	Summary        apiSummary        `json:"_summary"`
}

type apiRule struct {
	ID        string      `json:"_id"`
	Variation *int        `json:"variation"`
	Clauses   []apiClause `json:"clauses"`
}

type apiClause struct {
	Attribute   string `json:"attribute"`
	Op          string `json:"op"`
	Values      []any  `json:"values"`
	ContextKind string `json:"contextKind"`
	Negate      bool   `json:"negate"`
}

type apiTarget struct {
	Values      []string `json:"values"`
	Variation   int      `json:"variation"`
	ContextKind string   `json:"contextKind"`
}

type apiPrerequisite struct {
	Key       string `json:"key"`
	Variation int    `json:"variation"`
}

type apiFallthrough struct {
	Variation *int        `json:"variation"`
	Rollout   *apiRollout `json:"rollout,omitempty"`
}

type apiRollout struct {
	Variations []apiWeightedVariation `json:"variations"`
}

type apiWeightedVariation struct {
	Variation int `json:"variation"`
	Weight    int `json:"weight"`
}

type apiSummary struct {
	Variations    map[string]apiSummaryVariation `json:"variations"`
	Prerequisites int                            `json:"prerequisites"`
}

type apiSummaryVariation struct {
	Rules          int  `json:"rules"`
	NullRules      int  `json:"nullRules"`
	Targets        int  `json:"targets"`
	ContextTargets int  `json:"contextTargets"`
	IsFallthrough  bool `json:"isFallthrough"`
	IsOff          bool `json:"isOff"`
}

type apiLink struct {
	Href string `json:"href"`
	Type string `json:"type"`
}

type apiFlagStatusByFlag struct {
	Name          string `json:"name"`
	LastRequested string `json:"lastRequested"`
}

type apiUsageResponse struct {
	TotalEvaluations int64              `json:"totalEvaluations"`
	Series           []map[string]any   `json:"series"`
	Metadata         []apiUsageMetadata `json:"metadata"`
}

type apiUsageMetadata struct {
	Key any `json:"key"`
}

// POST /internal/projects/{p}/evaluationSummaries — batched eval totals for
// many flags × envs in one call. variationCounts is keyed by variation _id.
type apiEvalSummariesRequest struct {
	FlagKeys        []string `json:"flagKeys"`
	EnvironmentKeys []string `json:"environmentKeys"`
	From            int64    `json:"from"`
	To              int64    `json:"to"`
}

type apiEvalSummariesResponse struct {
	Data []apiEvalSummary `json:"data"`
}

type apiEvalSummary struct {
	FlagKey      string                          `json:"flagKey"`
	Environments map[string]apiEvalEnvVariations `json:"environments"`
}

type apiEvalEnvVariations struct {
	TotalEvaluations int64            `json:"totalEvaluations"`
	VariationCounts  map[string]int64 `json:"variationCounts"`
}

// GET /internal/projects/{p}/flags/{key}/monitor/contextsCount — true unique
// context cardinality (uniqExact) for one flag/env/contextKind over a window.
type apiContextsCountResponse struct {
	Data struct {
		TotalUniqueContexts int64 `json:"totalUniqueContexts"`
		IsSampled           bool  `json:"isSampled"`
	} `json:"data"`
}

// GET /internal/projects/{p}/flags/{key}/monitor/contextKinds — the context
// kinds a flag is evaluated for (so we know which to query for exposures).
type apiContextKindsResponse struct {
	Data struct {
		ContextKinds []string `json:"contextKinds"`
	} `json:"data"`
}
