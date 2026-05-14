package rollouts_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/cmd"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/rollouts"
)

// baseDismissArgs returns the minimal required args for the dismiss-regression command.
func baseDismissArgs() []string {
	return []string{
		"flags", "rollouts-beta", "dismiss-regression",
		"--access-token", "abcd1234",
		"--flag", "test-flag",
		"--project", "test-proj",
		"--environment", "test-env",
	}
}

// makeDismissRollout is a small fixture helper local to the dismiss tests so each test
// can adjust just the fields it cares about without re-declaring the full struct.
func makeDismissRollout(id, kind, envKey, statusKind, label string) rollouts.Rollout {
	createdAt := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	return rollouts.Rollout{
		ID:             id,
		FlagKey:        "test-flag",
		Kind:           kind,
		EnvironmentKey: envKey,
		CreatedAt:      createdAt,
		Status: rollouts.StatusBlock{
			Status: statusKind,
			Kind:   statusKind,
			Label:  label,
		},
	}
}

func TestDismiss_HappyPath_JSONOutput(t *testing.T) {
	mockClient := &rollouts.MockClient{}

	// Pre-read: List returns one regressed rollout.
	mockClient.On("List",
		"abcd1234",
		mock.Anything,
		"test-proj",
		"test-flag",
		mock.MatchedBy(func(opts rollouts.ListOpts) bool {
			return opts.Limit == 1 && opts.Environment == "test-env"
		}),
	).Return(&rollouts.RolloutList{Items: []rollouts.Rollout{
		makeDismissRollout("r1", "guarded", "test-env", "regressed", "Regression on m-latency"),
	}}, nil)

	// DismissRegression returns a post-dismiss active rollout (no warnings — dismissal landed).
	mockClient.On("DismissRegression",
		"abcd1234",
		mock.Anything,
		"test-proj",
		"test-flag",
		"test-env",
		mock.MatchedBy(func(instr rollouts.DismissRegressionInstruction) bool {
			return instr.Kind == "dismissRegression"
		}),
	).Return(&rollouts.Rollout{
		ID:             "r1",
		Kind:           "guarded",
		EnvironmentKey: "test-env",
		Status: rollouts.StatusBlock{
			Status: "in_progress",
			Kind:   "active",
			Label:  "Resumed after dismissal",
		},
	}, []string(nil), nil)

	args := append(baseDismissArgs(), "--output", "json")
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
	assert.Equal(t, "r1", r.ID)
	assert.Equal(t, "active", r.Status.Kind, "post-dismiss state should be active")

	// SC#4: meta.uiURL must be non-empty.
	require.NotNil(t, env.Meta)
	assert.NotEmpty(t, env.Meta.UIURL, "meta.uiURL must be populated per SC#4")

	// No warnings when dismissal landed cleanly.
	assert.Empty(t, env.Meta.Warnings, "no warnings expected when dismissal landed immediately")

	// Both List AND DismissRegression must have been called.
	mockClient.AssertExpectations(t)
}

func TestDismiss_HappyPath_PlaintextOutput(t *testing.T) {
	t.Setenv("FORCE_TTY", "1")
	mockClient := &rollouts.MockClient{}

	mockClient.On("List",
		"abcd1234",
		mock.Anything,
		"test-proj",
		"test-flag",
		mock.MatchedBy(func(opts rollouts.ListOpts) bool {
			return opts.Limit == 1 && opts.Environment == "test-env"
		}),
	).Return(&rollouts.RolloutList{Items: []rollouts.Rollout{
		makeDismissRollout("r1", "guarded", "test-env", "regressed", "Regression on m-latency"),
	}}, nil)

	mockClient.On("DismissRegression",
		"abcd1234",
		mock.Anything,
		"test-proj",
		"test-flag",
		"test-env",
		mock.MatchedBy(func(instr rollouts.DismissRegressionInstruction) bool {
			return instr.Kind == "dismissRegression"
		}),
	).Return(&rollouts.Rollout{
		ID:             "r1",
		Kind:           "guarded",
		EnvironmentKey: "test-env",
		Status: rollouts.StatusBlock{
			Status: "in_progress",
			Kind:   "active",
			Label:  "Resumed after dismissal",
		},
	}, []string(nil), nil)

	// No --output json: plaintext output.
	args := baseDismissArgs()
	output, err := cmd.CallCmd(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
	require.NoError(t, err)

	s := string(output)
	assert.Contains(t, s, "Dismissed regression on rollout r1", "plaintext must contain the dismiss header")
	assert.Contains(t, s, "Status: active", "plaintext must show the post-dismiss status")

	// Envelope JSON must NOT appear in plaintext mode.
	assert.NotContains(t, s, `"schemaVersion"`, "envelope must not leak into plaintext output")

	mockClient.AssertExpectations(t)
}

func TestDismiss_NoActiveRegression_RefusalEnvelope(t *testing.T) {
	mockClient := &rollouts.MockClient{}

	// Pre-read: rollout is in "active" state — not regressed.
	mockClient.On("List",
		"abcd1234",
		mock.Anything,
		"test-proj",
		"test-flag",
		mock.MatchedBy(func(opts rollouts.ListOpts) bool {
			return opts.Limit == 1 && opts.Environment == "test-env"
		}),
	).Return(&rollouts.RolloutList{Items: []rollouts.Rollout{
		makeDismissRollout("r1", "guarded", "test-env", "active", "Monitoring"),
	}}, nil)

	// DismissRegression must NOT be called.

	args := append(baseDismissArgs(), "--output", "json")
	stdout, _, err := cmd.CallCmdWithStderr(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
	require.Error(t, err)

	var env rollouts.Envelope
	require.NoError(t, json.Unmarshal(stdout, &env), "expected JSON error envelope on stdout, got: %s", string(stdout))
	assert.Equal(t, "Error", env.Kind)
	require.NotNil(t, env.Error)

	// LITERAL string assertion — catches typos and value drift.
	assert.Equal(t, "no_active_regression", env.Error.Code)
	assert.Contains(t, env.Error.Message, "r1", "error message must name the rollout ID")
	assert.Contains(t, env.Error.Message, "active", "error message must name the current state")
	assert.Contains(t, env.Error.NextAction, "status", "nextAction must hint at status command for recovery")

	// Prove the pre-read guard short-circuited — DismissRegression must not have been called.
	mockClient.AssertNotCalled(t, "DismissRegression",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestDismiss_NoActiveRegression_PausedState_RefusalEnvelope(t *testing.T) {
	mockClient := &rollouts.MockClient{}

	// Pre-read: rollout is in "paused" state — also not regressed.
	mockClient.On("List",
		"abcd1234",
		mock.Anything,
		"test-proj",
		"test-flag",
		mock.MatchedBy(func(opts rollouts.ListOpts) bool {
			return opts.Limit == 1 && opts.Environment == "test-env"
		}),
	).Return(&rollouts.RolloutList{Items: []rollouts.Rollout{
		makeDismissRollout("r2", "guarded", "test-env", "paused", "Paused by operator"),
	}}, nil)

	// DismissRegression must NOT be called.

	args := append(baseDismissArgs(), "--output", "json")
	stdout, _, err := cmd.CallCmdWithStderr(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
	require.Error(t, err)

	var env rollouts.Envelope
	require.NoError(t, json.Unmarshal(stdout, &env), "expected JSON error envelope on stdout, got: %s", string(stdout))
	assert.Equal(t, "Error", env.Kind)
	require.NotNil(t, env.Error)

	// LITERAL string assertion — verifies ANY non-regressed state triggers the refusal.
	assert.Equal(t, "no_active_regression", env.Error.Code)
	assert.Contains(t, env.Error.Message, "r2", "error message must name the rollout ID")
	assert.Contains(t, env.Error.Message, "paused", "error message must name the current state")
	assert.Contains(t, env.Error.NextAction, "status", "nextAction must hint at status command for recovery")

	// Prove the pre-read guard short-circuited.
	mockClient.AssertNotCalled(t, "DismissRegression",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestDismiss_NoRolloutsFound_ErrorEnvelopeOnStdout(t *testing.T) {
	mockClient := &rollouts.MockClient{}

	// Pre-read: empty list, no error.
	mockClient.On("List",
		"abcd1234",
		mock.Anything,
		"test-proj",
		"test-flag",
		mock.MatchedBy(func(opts rollouts.ListOpts) bool {
			return opts.Limit == 1 && opts.Environment == "test-env"
		}),
	).Return(&rollouts.RolloutList{Items: []rollouts.Rollout{}}, nil)

	// DismissRegression must NOT be called.

	args := append(baseDismissArgs(), "--output", "json")
	stdout, _, err := cmd.CallCmdWithStderr(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
	require.Error(t, err)

	var env rollouts.Envelope
	require.NoError(t, json.Unmarshal(stdout, &env), "expected JSON error envelope on stdout, got: %s", string(stdout))
	assert.Equal(t, "Error", env.Kind)
	require.NotNil(t, env.Error)

	// LITERAL string assertion — reuse of Phase 3 constant (no new constant added).
	assert.Equal(t, "no_rollouts_found", env.Error.Code)
	assert.Contains(t, env.Error.Message, "test-flag", "error message must mention the flag key")
	assert.Contains(t, env.Error.Message, "test-env", "error message must mention the environment key")
	assert.Contains(t, env.Error.NextAction, "list", "nextAction must point at list command")

	// Prove the pre-read guard short-circuited.
	mockClient.AssertNotCalled(t, "DismissRegression",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestDismiss_EventualConsistencyTimeout_WarningInEnvelope is the critical SC#2 test —
// it verifies the bounded-backoff timeout path produces a SUCCESS envelope with a
// meta.warnings entry (not an error) when the post-dismiss state stays regressed within
// the polling budget. The mock returns the stale rollout + warnings slice, simulating
// what Client.DismissRegression returns when all three backoff polls see "regressed".
func TestDismiss_EventualConsistencyTimeout_WarningInEnvelope(t *testing.T) {
	mockClient := &rollouts.MockClient{}

	// Pre-read: rollout is regressed — pre-read guard passes.
	mockClient.On("List",
		"abcd1234",
		mock.Anything,
		"test-proj",
		"test-flag",
		mock.MatchedBy(func(opts rollouts.ListOpts) bool {
			return opts.Limit == 1 && opts.Environment == "test-env"
		}),
	).Return(&rollouts.RolloutList{Items: []rollouts.Rollout{
		makeDismissRollout("r1", "guarded", "test-env", "regressed", "Regression still present"),
	}}, nil)

	// DismissRegression returns the still-regressed rollout + the eventual-consistency warning.
	// This simulates the bounded-backoff loop timing out without the state clearing (PC-007).
	mockClient.On("DismissRegression",
		"abcd1234",
		mock.Anything,
		"test-proj",
		"test-flag",
		"test-env",
		mock.MatchedBy(func(instr rollouts.DismissRegressionInstruction) bool {
			return instr.Kind == "dismissRegression"
		}),
	).Return(&rollouts.Rollout{
		ID:             "r1",
		Kind:           "guarded",
		EnvironmentKey: "test-env",
		Status: rollouts.StatusBlock{
			Status: "monitoring_regressed",
			Kind:   "regressed",
			Label:  "Regression still present",
		},
	}, []string{
		"Dismissal patch succeeded but the rollout's regressed state did not clear within the polling budget (~9s); see API-PAPERCUTS.md PC-007 for the upstream eventual-consistency context. Re-invoke `status` to confirm propagation.",
	}, nil)

	args := append(baseDismissArgs(), "--output", "json")
	output, err := cmd.CallCmd(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
	// exit 0 — PATCH succeeded; the warning is informational only.
	require.NoError(t, err)

	var env rollouts.Envelope
	require.NoError(t, json.Unmarshal(output, &env), "expected JSON envelope, got: %s", string(output))

	// kind must be "Rollout" (NOT "Error") — the PATCH succeeded.
	assert.Equal(t, "Rollout", env.Kind)

	// data.status.kind still shows "regressed" (API-passthrough of the stale post-timeout state).
	rawData, err := json.Marshal(env.Data)
	require.NoError(t, err)
	var r rollouts.Rollout
	require.NoError(t, json.Unmarshal(rawData, &r))
	assert.Equal(t, "regressed", r.Status.Kind, "stale post-timeout state must be surfaced verbatim")

	// meta.warnings must contain the PC-007 reference (the key anchor for operators/agents).
	require.NotNil(t, env.Meta)
	require.NotEmpty(t, env.Meta.Warnings, "meta.warnings must be non-empty on eventual-consistency timeout")
	assert.Contains(t, env.Meta.Warnings[0], "PC-007", "warning must reference the PC-007 papercut anchor")

	// meta.uiURL still populated on the timeout path.
	assert.NotEmpty(t, env.Meta.UIURL, "meta.uiURL must be populated even on timeout path")

	mockClient.AssertExpectations(t)
}

// TestDismiss_UpstreamForbidden_PassesThroughExistingMapping verifies the existing
// mapAPIError pipeline carries over to DismissRegression (i.e., the pre-read passes but
// the PATCH returns 403, which the mock simulates as a RolloutError with ErrCodeForbidden).
func TestDismiss_UpstreamForbidden_PassesThroughExistingMapping(t *testing.T) {
	mockClient := &rollouts.MockClient{}

	// Pre-read passes (regressed rollout).
	mockClient.On("List",
		"abcd1234",
		mock.Anything,
		"test-proj",
		"test-flag",
		mock.MatchedBy(func(opts rollouts.ListOpts) bool {
			return opts.Limit == 1 && opts.Environment == "test-env"
		}),
	).Return(&rollouts.RolloutList{Items: []rollouts.Rollout{
		makeDismissRollout("r1", "guarded", "test-env", "regressed", "Regression on latency"),
	}}, nil)

	// DismissRegression returns a forbidden error (simulates 403 from the upstream).
	mockClient.On("DismissRegression",
		"abcd1234",
		mock.Anything,
		"test-proj",
		"test-flag",
		"test-env",
		mock.Anything,
	).Return(nil, []string(nil), &rollouts.RolloutError{
		Code:       rollouts.ErrCodeForbidden,
		Message:    "Access denied; token may lack required scope",
		NextAction: "Verify your access token's role includes the required permission/scope on the target project",
	})

	args := append(baseDismissArgs(), "--output", "json")
	stdout, _, err := cmd.CallCmdWithStderr(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
	require.Error(t, err)

	var env rollouts.Envelope
	require.NoError(t, json.Unmarshal(stdout, &env), "expected JSON error envelope on stdout, got: %s", string(stdout))
	assert.Equal(t, "Error", env.Kind)
	require.NotNil(t, env.Error)

	// LITERAL string assertion — proves Phase 1 mapAPIError reused verbatim.
	assert.Equal(t, "forbidden", env.Error.Code)
	assert.Contains(t, env.Error.Message, "Access denied", "error message must reflect upstream message")

	// Both List AND DismissRegression must have been called.
	mockClient.AssertExpectations(t)
}
