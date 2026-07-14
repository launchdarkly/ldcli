package enrich

import (
	"fmt"
	"testing"
	"time"
)

var fixedNow = time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)

func intp(i int) *int { return &i }

// simpleEnv builds a traffic-bearing env that deterministically serves one
// variation via a simple on-toggle.
func simpleEnv(total7d int64, variation int, launched bool, modified time.Time) EnvStatus {
	status := "active"
	if launched {
		status = "launched"
	}
	return EnvStatus{
		On:           true,
		FlagStatus:   status,
		LastModified: modified,
		Evaluations:  EvaluationCounts{Total7d: total7d},
		Targeting:    TargetingSummary{IsSimpleToggle: true, FallthroughVariation: intp(variation)},
	}
}

// branchingEnv is on but still does real work (a targeting rule), so it does not
// serve a single deterministic variation.
func branchingEnv(total7d int64) EnvStatus {
	return EnvStatus{
		On:           true,
		FlagStatus:   "active",
		LastModified: fixedNow,
		Evaluations:  EvaluationCounts{Total7d: total7d},
		Targeting:    TargetingSummary{RuleCount: 1, FallthroughVariation: intp(0)},
	}
}

func TestRolledOut_StableAndTrafficked_IsReadyForCodeRemoval(t *testing.T) {
	d := &FlagDetail{
		Temporary:  true,
		CodeRefs:   2,
		Variations: []Variation{{Value: true}, {Value: false}},
		Environments: map[string]EnvStatus{
			"production": simpleEnv(20000, 0, false, fixedNow.AddDate(0, 0, -3)),
			"staging":    simpleEnv(15000, 0, true, fixedNow.AddDate(0, 0, -30)), // launched => stable
		},
	}
	if got := computeStaleStateAt(d, fixedNow); got != "readyForCodeRemoval" {
		t.Errorf("fully-rolled-out + stable should be readyForCodeRemoval, got %q", got)
	}
}

func TestRolledOut_PermanentFlag_IsNotRemovable(t *testing.T) {
	// Same fully-rolled-out, stable shape as above, but permanent (temporary=false):
	// it's config the owner intends to keep, not debt.
	d := &FlagDetail{
		Temporary:  false,
		CodeRefs:   2,
		Variations: []Variation{{Value: 11}, {Value: 0}},
		Environments: map[string]EnvStatus{
			"production": simpleEnv(20000, 0, true, fixedNow.AddDate(0, 0, -300)),
			"staging":    simpleEnv(15000, 0, true, fixedNow.AddDate(0, 0, -300)),
		},
	}
	if got := computeStaleStateAt(d, fixedNow); got == "readyForCodeRemoval" {
		t.Errorf("permanent flags are config, never readyForCodeRemoval, got %q", got)
	}
}

func TestRolledOut_RecentAndNotLaunched_IsNotRemovable(t *testing.T) {
	// Every relevant env serves variation 0, but it was flipped on a few days ago
	// and LD hasn't marked it launched — likely mid-rollout, so not yet removable.
	d := &FlagDetail{
		CodeRefs: 1,
		Environments: map[string]EnvStatus{
			"production": simpleEnv(20000, 0, false, fixedNow.AddDate(0, 0, -3)),
			"staging":    simpleEnv(15000, 0, false, fixedNow.AddDate(0, 0, -4)),
		},
	}
	if got := computeStaleStateAt(d, fixedNow); got == "readyForCodeRemoval" {
		t.Errorf("recently-flipped, not-yet-launched flag must not be removable, got %q", got)
	}
}

func TestRolledOut_EnvsServeDifferentVariations_IsNotRemovable(t *testing.T) {
	d := &FlagDetail{
		CodeRefs: 1,
		Environments: map[string]EnvStatus{
			"production": simpleEnv(20000, 0, true, fixedNow.AddDate(0, 0, -30)),
			"staging":    simpleEnv(15000, 1, true, fixedNow.AddDate(0, 0, -30)),
		},
	}
	if got := computeStaleStateAt(d, fixedNow); got == "readyForCodeRemoval" {
		t.Errorf("envs serving different variations are not a single hardcode, got %q", got)
	}
}

func TestRolledOut_RelevantEnvStillBranches_IsNotRemovable(t *testing.T) {
	d := &FlagDetail{
		CodeRefs: 1,
		Environments: map[string]EnvStatus{
			"production": branchingEnv(20000), // has a targeting rule
			"staging":    simpleEnv(15000, 0, true, fixedNow.AddDate(0, 0, -30)),
		},
	}
	if got := computeStaleStateAt(d, fixedNow); got == "readyForCodeRemoval" {
		t.Errorf("a relevant env that still branches blocks removal, got %q", got)
	}
}

func TestRolledOut_ZeroTrafficEnvIsIgnored(t *testing.T) {
	// Federal-style env has no traffic and complex targeting; it must not block
	// the otherwise-finished rollout.
	d := &FlagDetail{
		Temporary: true,
		CodeRefs:  1,
		Environments: map[string]EnvStatus{
			"production":           simpleEnv(20000, 0, true, fixedNow.AddDate(0, 0, -30)),
			"managed-federal-prod": branchingEnv(0), // zero evals => irrelevant
		},
	}
	if got := computeStaleStateAt(d, fixedNow); got != "readyForCodeRemoval" {
		t.Errorf("zero-traffic env should be ignored; expected readyForCodeRemoval, got %q", got)
	}
}

func TestFinalize_SetsRecommendedValueOnlyWhenRemovable(t *testing.T) {
	// Temporary, fully rolled out to variation 0 (=true) → removable, value set.
	removable := &FlagDetail{
		Temporary:  true,
		CodeRefs:   1,
		Variations: []Variation{{Value: true}, {Value: false}},
		Environments: map[string]EnvStatus{
			"production": simpleEnv(20000, 0, true, fixedNow.AddDate(0, 0, -30)),
			"staging":    simpleEnv(15000, 0, true, fixedNow.AddDate(0, 0, -30)),
		},
	}
	removable.finalize(fixedNow)
	if removable.StaleState != "readyForCodeRemoval" {
		t.Fatalf("expected readyForCodeRemoval, got %q", removable.StaleState)
	}
	if removable.RecommendedValue != true {
		t.Errorf("expected RecommendedValue true, got %v", removable.RecommendedValue)
	}

	// Permanent flag in the same shape → not removable, value must stay unset.
	permanent := &FlagDetail{
		Temporary:  false,
		CodeRefs:   1,
		Variations: []Variation{{Value: 11}, {Value: 0}},
		Environments: map[string]EnvStatus{
			"production": simpleEnv(20000, 0, true, fixedNow.AddDate(0, 0, -30)),
			"staging":    simpleEnv(15000, 0, true, fixedNow.AddDate(0, 0, -30)),
		},
	}
	permanent.finalize(fixedNow)
	if permanent.RecommendedValue != nil {
		t.Errorf("non-removable flag must not carry a recommendedValue, got %v", permanent.RecommendedValue)
	}
}

func TestRecommendedRemovalValue_OffServesOffVariation(t *testing.T) {
	// All relevant envs off → serves offVariation index 1 (=false).
	off := func(total7d int64) EnvStatus {
		return EnvStatus{
			On:           false,
			FlagStatus:   "active",
			LastModified: fixedNow.AddDate(0, 0, -30),
			Evaluations:  EvaluationCounts{Total7d: total7d},
			Targeting:    TargetingSummary{OffVariation: intp(1)},
		}
	}
	d := &FlagDetail{
		Temporary:  true,
		CodeRefs:   1,
		Variations: []Variation{{Value: true}, {Value: false}},
		Environments: map[string]EnvStatus{
			"production": off(9000),
			"staging":    off(8000),
		},
	}
	v, ok := recommendedRemovalValue(d)
	if !ok || v != false {
		t.Errorf("expected off flag to recommend offVariation value false, got %v (ok=%v)", v, ok)
	}
}

func TestRolledOut_NoCodeRefs_IsNotCodeRemoval(t *testing.T) {
	// Nothing in code to remove, even if fully rolled out.
	d := &FlagDetail{
		CodeRefs: 0,
		Environments: map[string]EnvStatus{
			"production": simpleEnv(20000, 0, true, fixedNow.AddDate(0, 0, -30)),
		},
	}
	if got := computeStaleStateAt(d, fixedNow); got == "readyForCodeRemoval" {
		t.Errorf("no code refs means nothing to remove, got %q", got)
	}
}

// The evaluation-counts endpoint is parameterized by a `from` timestamp.
// If that timestamp moves every run, the request URL — and therefore the
// response cache key — is unique each time and the cache never hits. These
// tests pin the day-aligned behavior that keeps repeated runs cacheable.

func TestEvalWindowStable_WithinSameDay(t *testing.T) {
	morning := time.Date(2026, 6, 16, 8, 30, 0, 0, time.UTC)
	evening := time.Date(2026, 6, 16, 23, 59, 59, 0, time.UTC)

	if evalWindowStart(morning, defaultEvalWindow) != evalWindowStart(evening, defaultEvalWindow) {
		t.Errorf("eval window must be identical across the same UTC day; got %d vs %d",
			evalWindowStart(morning, defaultEvalWindow), evalWindowStart(evening, defaultEvalWindow))
	}
}

func TestEvalWindowStable_IgnoresSubSecondJitter(t *testing.T) {
	a := time.Date(2026, 6, 16, 12, 0, 0, 123_000_000, time.UTC)
	b := time.Date(2026, 6, 16, 12, 0, 0, 456_000_000, time.UTC)

	if evalWindowStart(a, defaultEvalWindow) != evalWindowStart(b, defaultEvalWindow) {
		t.Error("millisecond jitter must not change the eval window (this was the cache-busting bug)")
	}
}

func TestEvalWindowChanges_AcrossDays(t *testing.T) {
	day1 := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)

	if evalWindowStart(day1, defaultEvalWindow) == evalWindowStart(day2, defaultEvalWindow) {
		t.Error("eval window should advance to a new value on a new UTC day")
	}
}

func TestEvalWindowIsSevenDaysBack_DayAligned(t *testing.T) {
	now := time.Date(2026, 6, 16, 15, 45, 0, 0, time.UTC)
	got := evalWindowStart(now, defaultEvalWindow)
	want := time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC).UnixMilli()
	if got != want {
		t.Errorf("expected window start %d (2026-06-09 00:00 UTC), got %d", want, got)
	}
}

// A short (<24h) window aligns to the HOUR, not the day, so a guarded-rollout-scale
// lookback still gets a cache-stable key while the API returns hourly buckets.
func TestEvalWindowShort_HourAligned(t *testing.T) {
	now := time.Date(2026, 6, 16, 15, 45, 30, 0, time.UTC)
	got := evalWindowStart(now, 6*time.Hour)
	want := time.Date(2026, 6, 16, 9, 0, 0, 0, time.UTC).UnixMilli() // 15:00 truncated, minus 6h
	if got != want {
		t.Errorf("expected 6h window start %d (2026-06-16 09:00 UTC), got %d", want, got)
	}
}

// Short-window key must stay stable within the same hour (cache property) but
// advance to a new hour — the same guarantee day-alignment gives the default.
func TestEvalWindowShort_StableWithinHour(t *testing.T) {
	a := time.Date(2026, 6, 16, 15, 5, 0, 0, time.UTC)
	b := time.Date(2026, 6, 16, 15, 55, 0, 0, time.UTC)
	c := time.Date(2026, 6, 16, 16, 5, 0, 0, time.UTC)
	if evalWindowStart(a, 6*time.Hour) != evalWindowStart(b, 6*time.Hour) {
		t.Error("6h window must be identical within the same UTC hour (cache stability)")
	}
	if evalWindowStart(a, 6*time.Hour) == evalWindowStart(c, 6*time.Hour) {
		t.Error("6h window should advance to a new value on a new UTC hour")
	}
}

// Guards the end-to-end property the bug violated: two enrichment runs on the
// same day must produce the identical cache key for the eval-counts request.
func TestEvalCacheKeyStableAcrossRuns(t *testing.T) {
	run := func(now time.Time) string {
		from := evalWindowStart(now, defaultEvalWindow)
		path := fmt.Sprintf("/api/v2/usage/evaluations/%s/%s/%s?from=%d",
			"default", "production", "my-flag", from)
		return cacheKey("GET", path, map[string]string{"LD-API-Version": "beta"}, nil)
	}

	first := run(time.Date(2026, 6, 16, 9, 0, 0, 0, time.UTC))
	second := run(time.Date(2026, 6, 16, 17, 30, 0, 0, time.UTC))

	if first != second {
		t.Error("eval-counts cache key must be stable across same-day runs")
	}
}
