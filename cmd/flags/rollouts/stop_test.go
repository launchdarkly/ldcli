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

// baseStopArgs returns the minimal required args for the stop command.
func baseStopArgs() []string {
	return []string{
		"flags", "rollouts-beta", "stop",
		"--access-token", "abcd1234",
		"--flag", "test-flag",
		"--project", "test-proj",
		"--environment", "test-env",
		"--to-variation", "v-uuid",
	}
}

// makeStopRollout is a small fixture helper local to the stop tests so each test can
// adjust just the fields it cares about without re-declaring the full struct.
func makeStopRollout(id, kind, envKey, statusKind, label string) rollouts.Rollout {
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

func TestStop_HappyPath_JSONOutput(t *testing.T) {
	mockClient := &rollouts.MockClient{}

	// Pre-read: List returns one non-terminal rollout.
	mockClient.On("List",
		"abcd1234",
		mock.Anything,
		"test-proj",
		"test-flag",
		mock.MatchedBy(func(opts rollouts.ListOpts) bool {
			return opts.Limit == 1 && opts.Environment == "test-env"
		}),
	).Return(&rollouts.RolloutList{Items: []rollouts.Rollout{
		makeStopRollout("r1", "progressive", "test-env", "active", "in progress"),
	}}, nil)

	// Stop returns a post-stop completed rollout.
	mockClient.On("Stop",
		"abcd1234",
		mock.Anything,
		"test-proj",
		"test-flag",
		"test-env",
		mock.MatchedBy(func(instr rollouts.StopInstruction) bool {
			return instr.Kind == "stopAutomatedRelease" && instr.FinalVariationID == "v-uuid"
		}),
	).Return(&rollouts.Rollout{
		ID:             "r1",
		Kind:           "progressive",
		EnvironmentKey: "test-env",
		Status: rollouts.StatusBlock{
			Status: "completed",
			Kind:   "completed",
			Label:  "Stopped to v-uuid",
		},
	}, nil)

	args := append(baseStopArgs(), "--output", "json")
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
	assert.Equal(t, "completed", r.Status.Kind)

	// SC#4: meta.uiURL must be non-empty and contain the flag key.
	require.NotNil(t, env.Meta)
	assert.NotEmpty(t, env.Meta.UIURL, "meta.uiURL must be populated per SC#4")
	assert.Contains(t, env.Meta.UIURL, "test-flag", "uiURL must include the flag key")

	// Both List AND Stop must have been called.
	mockClient.AssertExpectations(t)
}

func TestStop_HappyPath_PlaintextOutput(t *testing.T) {
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
		makeStopRollout("r1", "progressive", "test-env", "active", "in progress"),
	}}, nil)

	mockClient.On("Stop",
		"abcd1234",
		mock.Anything,
		"test-proj",
		"test-flag",
		"test-env",
		mock.MatchedBy(func(instr rollouts.StopInstruction) bool {
			return instr.Kind == "stopAutomatedRelease" && instr.FinalVariationID == "v-uuid"
		}),
	).Return(&rollouts.Rollout{
		ID:             "r1",
		Kind:           "progressive",
		EnvironmentKey: "test-env",
		Status: rollouts.StatusBlock{
			Status: "completed",
			Kind:   "completed",
			Label:  "Stopped",
		},
	}, nil)

	// No --output json: plaintext output.
	args := baseStopArgs()
	output, err := cmd.CallCmd(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
	require.NoError(t, err)

	s := string(output)
	assert.Contains(t, s, "Stopped rollout r1", "plaintext must contain 'Stopped rollout r1' header")
	assert.Contains(t, s, "Status: completed", "plaintext must show the post-stop status")

	// Envelope JSON must NOT appear in plaintext mode (no schema version leak).
	assert.NotContains(t, s, `"schemaVersion"`, "envelope must not leak into plaintext output")

	mockClient.AssertExpectations(t)
}

func TestStop_ToVariationMissing_UsageError(t *testing.T) {
	mockClient := &rollouts.MockClient{}
	// Cobra required-flag enforcement fires BEFORE RunE — no client calls expected.

	args := []string{
		"flags", "rollouts-beta", "stop",
		"--access-token", "abcd1234",
		"--flag", "test-flag",
		"--project", "test-proj",
		"--environment", "test-env",
		// --to-variation intentionally omitted
	}
	_, stderr, err := cmd.CallCmdWithStderr(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
	require.Error(t, err)

	// Cobra error message mentions "to-variation".
	errMsg := err.Error() + " " + string(stderr)
	assert.Contains(t, errMsg, "to-variation", "error must mention the missing required flag name")

	// Neither List nor Stop should have been called.
	mockClient.AssertNotCalled(t, "List",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	mockClient.AssertNotCalled(t, "Stop",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestStop_AlreadyTerminal_Completed_RefusalEnvelope(t *testing.T) {
	mockClient := &rollouts.MockClient{}

	// Pre-read: returns a completed (terminal) rollout.
	mockClient.On("List",
		"abcd1234",
		mock.Anything,
		"test-proj",
		"test-flag",
		mock.MatchedBy(func(opts rollouts.ListOpts) bool {
			return opts.Limit == 1 && opts.Environment == "test-env"
		}),
	).Return(&rollouts.RolloutList{Items: []rollouts.Rollout{
		makeStopRollout("r-done", "progressive", "test-env", "completed", "Done"),
	}}, nil)

	// Stop must NOT be called.

	args := append(baseStopArgs(), "--output", "json")
	stdout, _, err := cmd.CallCmdWithStderr(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
	require.Error(t, err)

	var env rollouts.Envelope
	require.NoError(t, json.Unmarshal(stdout, &env), "expected JSON error envelope on stdout, got: %s", string(stdout))
	assert.Equal(t, "Error", env.Kind)
	require.NotNil(t, env.Error)

	// LITERAL string assertion — catches value drift between constant and wire emission.
	assert.Equal(t, "rollout_already_terminal", env.Error.Code)
	assert.Contains(t, env.Error.Message, "r-done", "error message must name the rollout ID")
	assert.Contains(t, env.Error.Message, "completed", "error message must name the current state")
	assert.Contains(t, env.Error.NextAction, "list", "nextAction must hint at list command for recovery")

	// Prove the pre-read guard short-circuited — Stop must not have been called.
	mockClient.AssertNotCalled(t, "Stop",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestStop_AlreadyTerminal_Reverted_RefusalEnvelope(t *testing.T) {
	mockClient := &rollouts.MockClient{}

	// Pre-read: returns a reverted (terminal) rollout.
	mockClient.On("List",
		"abcd1234",
		mock.Anything,
		"test-proj",
		"test-flag",
		mock.MatchedBy(func(opts rollouts.ListOpts) bool {
			return opts.Limit == 1 && opts.Environment == "test-env"
		}),
	).Return(&rollouts.RolloutList{Items: []rollouts.Rollout{
		makeStopRollout("r-rev", "guarded", "test-env", "reverted", "Reverted due to regression"),
	}}, nil)

	// Stop must NOT be called.

	args := append(baseStopArgs(), "--output", "json")
	stdout, _, err := cmd.CallCmdWithStderr(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
	require.Error(t, err)

	var env rollouts.Envelope
	require.NoError(t, json.Unmarshal(stdout, &env), "expected JSON error envelope on stdout, got: %s", string(stdout))
	assert.Equal(t, "Error", env.Kind)
	require.NotNil(t, env.Error)

	// LITERAL string assertion — verifies BOTH terminal values trigger the refusal.
	assert.Equal(t, "rollout_already_terminal", env.Error.Code)
	assert.Contains(t, env.Error.Message, "r-rev", "error message must name the rollout ID")
	assert.Contains(t, env.Error.Message, "reverted", "error message must name the current state")

	// Prove the pre-read guard short-circuited.
	mockClient.AssertNotCalled(t, "Stop",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestStop_NoRolloutsFound_ErrorEnvelopeOnStdout(t *testing.T) {
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

	// Stop must NOT be called.

	args := append(baseStopArgs(), "--output", "json")
	stdout, _, err := cmd.CallCmdWithStderr(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
	require.Error(t, err)

	var env rollouts.Envelope
	require.NoError(t, json.Unmarshal(stdout, &env), "expected JSON error envelope on stdout, got: %s", string(stdout))
	assert.Equal(t, "Error", env.Kind)
	require.NotNil(t, env.Error)

	// LITERAL string assertion — reuse of Phase 3 constant.
	assert.Equal(t, "no_rollouts_found", env.Error.Code)
	assert.Contains(t, env.Error.Message, "test-flag", "error message must mention the flag key")
	assert.Contains(t, env.Error.Message, "test-env", "error message must mention the environment key")
	assert.Contains(t, env.Error.NextAction, "ldcli flags rollouts-beta list", "nextAction must point at list command")

	// Prove the pre-read guard short-circuited.
	mockClient.AssertNotCalled(t, "Stop",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestStop_UpstreamInvalidVariation_PassesThroughExistingMapping(t *testing.T) {
	mockClient := &rollouts.MockClient{}

	// Pre-read passes (non-terminal rollout).
	mockClient.On("List",
		"abcd1234",
		mock.Anything,
		"test-proj",
		"test-flag",
		mock.MatchedBy(func(opts rollouts.ListOpts) bool {
			return opts.Limit == 1 && opts.Environment == "test-env"
		}),
	).Return(&rollouts.RolloutList{Items: []rollouts.Rollout{
		makeStopRollout("r1", "progressive", "test-env", "active", "in progress"),
	}}, nil)

	// Stop returns an invalid-variation error (simulates a bad UUID being passed).
	mockClient.On("Stop",
		"abcd1234",
		mock.Anything,
		"test-proj",
		"test-flag",
		"test-env",
		mock.Anything,
	).Return(nil, &rollouts.RolloutError{
		Code:       rollouts.ErrCodeInvalidVariation,
		Message:    "originalVariationId must be a valid variation id 'v-uuid'",
		NextAction: "Pass the variation UUID (_id) ...",
	})

	args := append(baseStopArgs(), "--output", "json")
	stdout, _, err := cmd.CallCmdWithStderr(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
	require.Error(t, err)

	var env rollouts.Envelope
	require.NoError(t, json.Unmarshal(stdout, &env), "expected JSON error envelope on stdout, got: %s", string(stdout))
	assert.Equal(t, "Error", env.Kind)
	require.NotNil(t, env.Error)

	// LITERAL string assertion — proves Phase 2 mapAPIError mapping reused verbatim.
	assert.Equal(t, "invalid_variation", env.Error.Code)
	assert.Contains(t, env.Error.Message, "originalVariationId must be a valid variation id", "error message must match upstream verbatim")

	// Both List AND Stop must have been called.
	mockClient.AssertExpectations(t)
}
