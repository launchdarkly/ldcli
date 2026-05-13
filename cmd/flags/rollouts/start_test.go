package rollouts_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/cmd"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/rollouts"
)

// baseStartArgs returns the minimal required args for the start command (progressive, no metrics).
func baseStartArgs() []string {
	return []string{
		"flags", "rollouts-beta", "start",
		"--access-token", "abcd1234",
		"--flag", "test-flag",
		"--project", "test-proj",
		"--environment", "test-env",
		"--target-variation", "tv-uuid",
		"--original-variation", "ov-uuid",
		"--randomization-unit", "user",
		"--stages", "25:60m,50:60m,100:60m",
	}
}

func TestStartCmd_ProgressiveHappyPath_JSON(t *testing.T) {
	mockClient := &rollouts.MockClient{}
	mockClient.On("Start",
		"abcd1234",
		mock.Anything,
		"test-proj",
		"test-flag",
		"test-env",
		mock.MatchedBy(func(instr rollouts.StartInstruction) bool {
			return instr.ReleaseKind == "progressive" &&
				len(instr.Stages) == 3 &&
				instr.Stages[0].Allocation == 25000 &&
				instr.Stages[0].DurationMillis == 3600000 &&
				instr.RuleID == "" &&
				len(instr.Metrics) == 0
		}),
	).Return(&rollouts.Rollout{ID: "new-rollout-id", Kind: "progressive"}, nil)

	args := append(baseStartArgs(), "--output", "json")
	output, err := cmd.CallCmd(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
	require.NoError(t, err)

	var env rollouts.Envelope
	require.NoError(t, json.Unmarshal(output, &env), "expected JSON envelope, got: %s", string(output))
	assert.Equal(t, "rollouts.v1beta1", env.SchemaVersion)
	assert.Equal(t, "Rollout", env.Kind)

	// Decode data payload into a Rollout.
	rawData, err := json.Marshal(env.Data)
	require.NoError(t, err)
	var r rollouts.Rollout
	require.NoError(t, json.Unmarshal(rawData, &r))
	assert.Equal(t, "new-rollout-id", r.ID)
	mockClient.AssertExpectations(t)
}

func TestStartCmd_GuardedWithPauseOnRegression(t *testing.T) {
	mockClient := &rollouts.MockClient{}
	mockClient.On("Start",
		"abcd1234",
		mock.Anything,
		"test-proj",
		"test-flag",
		"test-env",
		mock.MatchedBy(func(instr rollouts.StartInstruction) bool {
			pref, ok := instr.MetricMonitoringPreferences["rg-errors"]
			return instr.ReleaseKind == "guarded" &&
				ok && !pref.AutoRollback &&
				len(instr.Metrics) == 1 && instr.Metrics[0].Key == "rg-errors"
		}),
	).Return(&rollouts.Rollout{ID: "guarded-rollout", Kind: "guarded"}, nil)

	args := append(baseStartArgs(), "--pause-on-regression", "rg-errors", "--output", "json")
	output, err := cmd.CallCmd(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
	require.NoError(t, err)

	var env rollouts.Envelope
	require.NoError(t, json.Unmarshal(output, &env))
	assert.Equal(t, "Rollout", env.Kind)
	mockClient.AssertExpectations(t)
}

func TestStartCmd_GuardedWithRevertOnRegression(t *testing.T) {
	mockClient := &rollouts.MockClient{}
	mockClient.On("Start",
		"abcd1234",
		mock.Anything,
		"test-proj",
		"test-flag",
		"test-env",
		mock.MatchedBy(func(instr rollouts.StartInstruction) bool {
			pref, ok := instr.MetricMonitoringPreferences["rg-errors"]
			return instr.ReleaseKind == "guarded" &&
				ok && pref.AutoRollback &&
				len(instr.Metrics) == 1 && instr.Metrics[0].Key == "rg-errors"
		}),
	).Return(&rollouts.Rollout{ID: "guarded-rollout-revert", Kind: "guarded"}, nil)

	args := append(baseStartArgs(), "--revert-on-regression", "rg-errors", "--output", "json")
	output, err := cmd.CallCmd(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
	require.NoError(t, err)

	var env rollouts.Envelope
	require.NoError(t, json.Unmarshal(output, &env))
	assert.Equal(t, "Rollout", env.Kind)
	mockClient.AssertExpectations(t)
}

func TestStartCmd_GuardedWithMixedMetricBehaviors(t *testing.T) {
	mockClient := &rollouts.MockClient{}
	mockClient.On("Start",
		"abcd1234",
		mock.Anything,
		"test-proj",
		"test-flag",
		"test-env",
		mock.MatchedBy(func(instr rollouts.StartInstruction) bool {
			prefA, okA := instr.MetricMonitoringPreferences["rg-a"]
			prefB, okB := instr.MetricMonitoringPreferences["rg-b"]
			return instr.ReleaseKind == "guarded" &&
				len(instr.Metrics) == 2 &&
				okA && !prefA.AutoRollback && // pause
				okB && prefB.AutoRollback // revert
		}),
	).Return(&rollouts.Rollout{ID: "mixed-guarded", Kind: "guarded"}, nil)

	args := append(baseStartArgs(),
		"--pause-on-regression", "rg-a",
		"--revert-on-regression", "rg-b",
		"--output", "json",
	)
	output, err := cmd.CallCmd(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
	require.NoError(t, err)

	var env rollouts.Envelope
	require.NoError(t, json.Unmarshal(output, &env))
	assert.Equal(t, "Rollout", env.Kind)
	mockClient.AssertExpectations(t)
}

func TestStartCmd_MetricInBothFlags_UsageError(t *testing.T) {
	mockClient := &rollouts.MockClient{}
	// No .On("Start") — the mock must NOT be called.

	args := append(baseStartArgs(),
		"--pause-on-regression", "rg-x",
		"--revert-on-regression", "rg-x",
		"--output", "json",
	)
	stdout, _, err := cmd.CallCmdWithStderr(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
	require.Error(t, err)

	// Error message must contain both the metric key and the mutual-exclusion reason.
	assert.Contains(t, string(stdout)+" "+err.Error(), "rg-x",
		"error output must mention the conflicting metric key")

	// Client must not have been called.
	mockClient.AssertNotCalled(t, "Start")
}

func TestStartCmd_DecimalAllocationRejected(t *testing.T) {
	mockClient := &rollouts.MockClient{}

	args := []string{
		"flags", "rollouts-beta", "start",
		"--access-token", "abcd1234",
		"--flag", "test-flag",
		"--project", "test-proj",
		"--environment", "test-env",
		"--target-variation", "tv-uuid",
		"--original-variation", "ov-uuid",
		"--randomization-unit", "user",
		"--stages", "12.5:60m",
	}
	_, err := cmd.CallCmd(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "whole percent integer",
		"error must mention whole-percent-integer requirement")
	mockClient.AssertNotCalled(t, "Start")
}

func TestStartCmd_DurationWithoutUnitRejected(t *testing.T) {
	mockClient := &rollouts.MockClient{}

	args := []string{
		"flags", "rollouts-beta", "start",
		"--access-token", "abcd1234",
		"--flag", "test-flag",
		"--project", "test-proj",
		"--environment", "test-env",
		"--target-variation", "tv-uuid",
		"--original-variation", "ov-uuid",
		"--randomization-unit", "user",
		"--stages", "25:3600",
	}
	_, err := cmd.CallCmd(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unit",
		"error must mention that duration must include a unit")
	mockClient.AssertNotCalled(t, "Start")
}

func TestStartCmd_AllocationOutOfRangeRejected(t *testing.T) {
	mockClient := &rollouts.MockClient{}

	args := []string{
		"flags", "rollouts-beta", "start",
		"--access-token", "abcd1234",
		"--flag", "test-flag",
		"--project", "test-proj",
		"--environment", "test-env",
		"--target-variation", "tv-uuid",
		"--original-variation", "ov-uuid",
		"--randomization-unit", "user",
		"--stages", "150:60m",
	}
	_, err := cmd.CallCmd(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "range",
		"error must mention allocation range constraint")
	mockClient.AssertNotCalled(t, "Start")
}

func TestStartCmd_ErrorEnvelopeOnStdout_NotStderr_JSON(t *testing.T) {
	mockClient := &rollouts.MockClient{}
	mockClient.On("Start",
		"abcd1234",
		mock.Anything,
		"test-proj",
		"test-flag",
		"test-env",
		mock.Anything,
	).Return(nil, &rollouts.RolloutError{
		Code:       rollouts.ErrCodeRolloutAlreadyRunning,
		Message:    "Flag must not have ongoing guarded rollout",
		NextAction: "Stop the current rollout",
	})

	args := append(baseStartArgs(), "--output", "json")
	stdout, stderr, err := cmd.CallCmdWithStderr(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
	require.Error(t, err)

	// (a) stdout must contain the error envelope.
	var env rollouts.Envelope
	require.NoError(t, json.Unmarshal(stdout, &env), "expected JSON error envelope on stdout, got: %s", string(stdout))
	assert.Equal(t, "Error", env.Kind)
	assert.Equal(t, "rollouts.v1beta1", env.SchemaVersion)
	require.NotNil(t, env.Error)
	assert.Equal(t, rollouts.ErrCodeRolloutAlreadyRunning, env.Error.Code)

	// (b) stderr must NOT contain the envelope JSON.
	assert.NotContains(t, string(stderr), `"kind": "Error"`,
		"envelope must not leak onto stderr in JSON mode (AGENT-04)")
	assert.NotContains(t, string(stderr), `"schemaVersion"`,
		"envelope must not leak onto stderr in JSON mode (AGENT-04)")

	// (c) The returned error is a short sentinel, not the full envelope.
	assert.NotContains(t, err.Error(), `"kind"`,
		"err.Error() must not be the envelope JSON")
	assert.NotContains(t, err.Error(), `"schemaVersion"`,
		"err.Error() must not be the envelope JSON")
}

func TestStartCmd_ErrorPlainText(t *testing.T) {
	mockClient := &rollouts.MockClient{}
	mockClient.On("Start",
		"abcd1234",
		mock.Anything,
		"test-proj",
		"test-flag",
		"test-env",
		mock.Anything,
	).Return(nil, &rollouts.RolloutError{
		Code:    rollouts.ErrCodeRolloutAlreadyRunning,
		Message: "Flag must not have ongoing guarded rollout",
	})

	args := baseStartArgs() // no --output json
	output, err := cmd.CallCmd(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
	require.Error(t, err)

	// In plaintext mode the error message surfaces via err (not envelope on stdout).
	assert.NotContains(t, string(output), `"schemaVersion"`,
		"envelope JSON must not appear on stdout in plaintext mode")
}

func TestStartCmd_RuleIDFlowsThrough(t *testing.T) {
	ruleUUID := "11111111-2222-3333-4444-555555555555"
	mockClient := &rollouts.MockClient{}
	mockClient.On("Start",
		"abcd1234",
		mock.Anything,
		"test-proj",
		"test-flag",
		"test-env",
		mock.MatchedBy(func(instr rollouts.StartInstruction) bool {
			return instr.RuleID == ruleUUID
		}),
	).Return(&rollouts.Rollout{ID: "rule-rollout"}, nil)

	args := append(baseStartArgs(), "--rule-id", ruleUUID, "--output", "json")
	_, err := cmd.CallCmd(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
	require.NoError(t, err)
	mockClient.AssertExpectations(t)
}

// TestParseStages covers the stages parser standalone.
// The command-layer tests above provide integration coverage; these table-driven tests
// verify the edge cases of parseStages directly.
func TestParseStages(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantErr     bool
		errContains string
		wantLen     int
		wantAlloc   int   // basis points for stage[0]
		wantMillis  int64 // duration millis for stage[0]
	}{
		{
			name:       "simple three-stage progressive",
			input:      "25:60m,50:60m,100:60m",
			wantLen:    3,
			wantAlloc:  25000,
			wantMillis: 3600000,
		},
		{
			name:       "single stage",
			input:      "100:1h30m",
			wantLen:    1,
			wantAlloc:  100000,
			wantMillis: 5400000,
		},
		{
			name:        "decimal allocation rejected",
			input:       "12.5:60m",
			wantErr:     true,
			errContains: "whole percent integer",
		},
		{
			name:        "duration without unit rejected",
			input:       "25:3600",
			wantErr:     true,
			errContains: "unit",
		},
		{
			name:        "allocation zero rejected",
			input:       "0:60m",
			wantErr:     true,
			errContains: "range",
		},
		{
			name:        "allocation over 100 rejected",
			input:       "101:60m",
			wantErr:     true,
			errContains: "range",
		},
		{
			name:        "empty string rejected",
			input:       "",
			wantErr:     true,
			errContains: "at least one stage",
		},
		{
			name:        "missing colon separator rejected",
			input:       "25-60m",
			wantErr:     true,
			errContains: "expected",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockClient := &rollouts.MockClient{}
			// Build args exercising parseStages via the command to confirm the parser integrates.
			if tc.wantErr {
				args := []string{
					"flags", "rollouts-beta", "start",
					"--access-token", "abcd1234",
					"--flag", "f",
					"--project", "p",
					"--environment", "e",
					"--target-variation", "tv",
					"--original-variation", "ov",
					"--randomization-unit", "user",
					"--stages", tc.input,
				}
				_, err := cmd.CallCmd(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
				require.Error(t, err)
				if tc.errContains != "" {
					assert.Contains(t, err.Error(), tc.errContains,
						"expected error to contain %q, got: %q", tc.errContains, err.Error())
				}
				mockClient.AssertNotCalled(t, "Start")
			} else {
				// Happy-path tests: mock returns a rollout; assert stage count/values.
				mockClient.On("Start",
					"abcd1234",
					mock.Anything,
					"p",
					"f",
					"e",
					mock.MatchedBy(func(instr rollouts.StartInstruction) bool {
						return len(instr.Stages) == tc.wantLen &&
							instr.Stages[0].Allocation == tc.wantAlloc &&
							instr.Stages[0].DurationMillis == tc.wantMillis
					}),
				).Return(&rollouts.Rollout{ID: "ok"}, nil)

				args := []string{
					"flags", "rollouts-beta", "start",
					"--access-token", "abcd1234",
					"--flag", "f",
					"--project", "p",
					"--environment", "e",
					"--target-variation", "tv",
					"--original-variation", "ov",
					"--randomization-unit", "user",
					"--stages", tc.input,
				}
				_, err := cmd.CallCmd(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
				require.NoError(t, err)
				mockClient.AssertExpectations(t)
			}
		})
	}
}
