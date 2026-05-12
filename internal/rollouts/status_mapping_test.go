package rollouts_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/rollouts"
)

// TestStatusMapping is the table-driven coverage for all 13 documented raw API statuses,
// including the sub-condition discrimination for `in_progress` (4 sub-cases) and
// `reverted` (3 sub-cases). Each entry asserts that MapStatus(r) returns the expected
// (kind, label) tuple plus raw passthrough for Status.
func TestStatusMapping(t *testing.T) {
	cases := []struct {
		name      string
		rollout   rollouts.Rollout
		wantKind  string
		wantLabel string
	}{
		{
			name: "not_started uses default rule produces Monitoring the default rule",
			rollout: rollouts.Rollout{
				Status:              rollouts.StatusBlock{Status: "not_started"},
				RuleIDOrFallthrough: "fallthrough",
			},
			wantKind:  "active",
			wantLabel: "Monitoring the default rule",
		},
		{
			name: "waiting uses named rule produces Monitoring rule <id>",
			rollout: rollouts.Rollout{
				Status:              rollouts.StatusBlock{Status: "waiting"},
				RuleIDOrFallthrough: "rule-abc",
			},
			wantKind:  "active",
			wantLabel: "Monitoring rule rule-abc",
		},
		{
			name: "in_progress progressive (no metric configurations) produces Monitoring {rule}",
			rollout: rollouts.Rollout{
				Status:              rollouts.StatusBlock{Status: "in_progress"},
				Kind:                "progressive",
				RuleIDOrFallthrough: "fallthrough",
				// no MetricConfigurations
			},
			wantKind:  "active",
			wantLabel: "Monitoring the default rule",
		},
		{
			name: "in_progress guarded with min sample NOT reached produces (not enough data)",
			rollout: rollouts.Rollout{
				Status:              rollouts.StatusBlock{Status: "in_progress"},
				Kind:                "guarded",
				RuleIDOrFallthrough: "fallthrough",
				MetricConfigurations: []rollouts.MetricConfiguration{
					{MetricKey: "latency-p99", MinSampleSize: 1000, Status: "not_enough_data"},
				},
			},
			wantKind:  "active",
			wantLabel: "Monitoring the default rule for regressions… (not enough data)",
		},
		{
			name: "in_progress guarded with min sample reached produces standard monitoring",
			rollout: rollouts.Rollout{
				Status:              rollouts.StatusBlock{Status: "in_progress"},
				Kind:                "guarded",
				RuleIDOrFallthrough: "fallthrough",
				MetricConfigurations: []rollouts.MetricConfiguration{
					{MetricKey: "latency-p99", MinSampleSize: 1000, Status: "ok"},
				},
			},
			wantKind:  "active",
			wantLabel: "Monitoring the default rule for regressions…",
		},
		{
			name: "in_progress guarded with extension active produces Monitoring extended by {duration}",
			rollout: rollouts.Rollout{
				Status:                  rollouts.StatusBlock{Status: "in_progress"},
				Kind:                    "guarded",
				RuleIDOrFallthrough:     "fallthrough",
				ExtensionDurationMillis: ptrInt64(900000), // 15m
				MetricConfigurations: []rollouts.MetricConfiguration{
					{MetricKey: "latency-p99", MinSampleSize: 1000, Status: "ok"},
				},
			},
			wantKind:  "active",
			wantLabel: "Monitoring extended by 15m0s",
		},
		{
			name: "monitoring_regressed produces Regressions detected on {rule} for {metric names}",
			rollout: rollouts.Rollout{
				Status:              rollouts.StatusBlock{Status: "monitoring_regressed"},
				Kind:                "guarded",
				RuleIDOrFallthrough: "fallthrough",
				MetricConfigurations: []rollouts.MetricConfiguration{
					{MetricKey: "latency-p99", Status: "regressed"},
					{MetricKey: "error-rate", Status: "regressed"},
				},
			},
			wantKind:  "regressed",
			wantLabel: "Regressions detected on the default rule for latency-p99, error-rate",
		},
		{
			name: "monitoring_stopped produces {rule} paused at {N}%: regressions detected for {metric names}",
			rollout: rollouts.Rollout{
				Status:              rollouts.StatusBlock{Status: "monitoring_stopped"},
				Kind:                "guarded",
				RuleIDOrFallthrough: "fallthrough",
				LatestStageIndex:    0,
				Stages: []rollouts.Stage{
					{StageIndex: 0, Allocation: 25000}, // 25%
				},
				MetricConfigurations: []rollouts.MetricConfiguration{
					{MetricKey: "latency-p99", Status: "regressed"},
				},
			},
			wantKind:  "paused",
			wantLabel: "the default rule paused at 25%: regressions detected for latency-p99",
		},
		{
			name: "srm_stopped produces {rule} paused at {N}%: sample ratio mismatch detected",
			rollout: rollouts.Rollout{
				Status:              rollouts.StatusBlock{Status: "srm_stopped"},
				Kind:                "guarded",
				RuleIDOrFallthrough: "fallthrough",
				LatestStageIndex:    1,
				Stages: []rollouts.Stage{
					{StageIndex: 0, Allocation: 25000},
					{StageIndex: 1, Allocation: 50000},
				},
			},
			wantKind:  "paused",
			wantLabel: "the default rule paused at 50%: sample ratio mismatch detected",
		},
		{
			name: "completed produces Monitoring completed on {rule}",
			rollout: rollouts.Rollout{
				Status:              rollouts.StatusBlock{Status: "completed"},
				Kind:                "guarded",
				RuleIDOrFallthrough: "fallthrough",
			},
			wantKind:  "completed",
			wantLabel: "Monitoring completed on the default rule",
		},
		{
			name: "manually_completed produces {rule} rolled forward manually",
			rollout: rollouts.Rollout{
				Status:              rollouts.StatusBlock{Status: "manually_completed"},
				Kind:                "guarded",
				RuleIDOrFallthrough: "fallthrough",
			},
			wantKind:  "completed",
			wantLabel: "the default rule rolled forward manually",
		},
		{
			name: "manually_reverted produces {rule} rolled back manually",
			rollout: rollouts.Rollout{
				Status:              rollouts.StatusBlock{Status: "manually_reverted"},
				Kind:                "guarded",
				RuleIDOrFallthrough: "fallthrough",
			},
			wantKind:  "reverted",
			wantLabel: "the default rule rolled back manually",
		},
		{
			name: "reverted (insufficient sample - no discriminating events) produces insufficient sample size label",
			rollout: rollouts.Rollout{
				Status:              rollouts.StatusBlock{Status: "reverted"},
				Kind:                "guarded",
				RuleIDOrFallthrough: "fallthrough",
				Events: []rollouts.Event{
					{Kind: "minimum_monitoring_window_expired"},
				},
			},
			wantKind:  "reverted",
			wantLabel: "the default rule rolled back due to insufficient sample size",
		},
		{
			name: "reverted (SRM event) produces rolled back automatically",
			rollout: rollouts.Rollout{
				Status:              rollouts.StatusBlock{Status: "reverted"},
				Kind:                "guarded",
				RuleIDOrFallthrough: "fallthrough",
				Events: []rollouts.Event{
					{Kind: "srm_detected"},
				},
			},
			wantKind:  "reverted",
			wantLabel: "the default rule rolled back automatically",
		},
		{
			name: "reverted (regression event) produces rolled back automatically after detecting a regression for {metric names}",
			rollout: rollouts.Rollout{
				Status:              rollouts.StatusBlock{Status: "reverted"},
				Kind:                "guarded",
				RuleIDOrFallthrough: "fallthrough",
				Events: []rollouts.Event{
					{Kind: "regression_detected", MetricKey: "latency-p99"},
				},
			},
			wantKind:  "reverted",
			wantLabel: "the default rule rolled back automatically after detecting a regression for latency-p99",
		},
		{
			name: "archived produces Monitoring of {rule} stopped early",
			rollout: rollouts.Rollout{
				Status:              rollouts.StatusBlock{Status: "archived"},
				Kind:                "guarded",
				RuleIDOrFallthrough: "fallthrough",
			},
			wantKind:  "paused",
			wantLabel: "Monitoring of the default rule stopped early",
		},
		{
			name: "unknown status falls back to active with unknown-status label",
			rollout: rollouts.Rollout{
				Status:              rollouts.StatusBlock{Status: "definitely_not_a_real_status"},
				Kind:                "guarded",
				RuleIDOrFallthrough: "fallthrough",
			},
			wantKind:  "active",
			wantLabel: "Monitoring (unknown status: definitely_not_a_real_status)",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := rollouts.MapStatus(&tc.rollout)
			require.Equal(t, tc.rollout.Status.Status, got.Status,
				"raw passthrough: MapStatus must echo Rollout.Status.Status into StatusBlock.Status")
			assert.Equal(t, tc.wantKind, got.Kind, "kind bucket")
			assert.Equal(t, tc.wantLabel, got.Label, "label string")
		})
	}
}

// TestDeriveStatusBlockMatchesMapStatus verifies that DeriveStatusBlock is the
// converter-facing alias of MapStatus and produces identical output.
func TestDeriveStatusBlockMatchesMapStatus(t *testing.T) {
	r := &rollouts.Rollout{
		Status:              rollouts.StatusBlock{Status: "monitoring_regressed"},
		Kind:                "guarded",
		RuleIDOrFallthrough: "fallthrough",
		MetricConfigurations: []rollouts.MetricConfiguration{
			{MetricKey: "latency-p99", Status: "regressed"},
		},
	}
	a := rollouts.MapStatus(r)
	b := rollouts.DeriveStatusBlock(r)
	assert.Equal(t, a, b)
}

func ptrInt64(v int64) *int64 { return &v }
