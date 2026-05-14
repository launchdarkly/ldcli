package rollouts_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/cmd"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/rollouts"
)

// makeStatusRollout is a small fixture helper local to the status tests so each test can
// adjust just the fields it cares about without re-declaring the full struct.
func makeStatusRollout(id, kind, envKey, statusRaw, statusKind, label string) rollouts.Rollout {
	createdAt := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	return rollouts.Rollout{
		ID:             id,
		FlagKey:        "test-flag",
		Kind:           kind,
		EnvironmentKey: envKey,
		CreatedAt:      createdAt,
		Status: rollouts.StatusBlock{
			Status: statusRaw,
			Kind:   statusKind,
			Label:  label,
		},
	}
}

func TestStatus_MostRecentPath_JSONOutput(t *testing.T) {
	mockClient := &rollouts.MockClient{}
	r := makeStatusRollout("r1", "progressive", "prod", "in_progress", "active", "Monitoring")
	// Most-recent path: expect List with Limit:1; NOT Get.
	mockClient.On("List", "abcd1234", mock.Anything, "test-proj", "test-flag",
		mock.MatchedBy(func(opts rollouts.ListOpts) bool {
			return opts.Limit == 1 && !opts.All
		}),
	).Return(&rollouts.RolloutList{Items: []rollouts.Rollout{r}}, nil)

	args := []string{
		"flags", "rollouts-beta", "status",
		"--access-token", "abcd1234",
		"--flag", "test-flag",
		"--project", "test-proj",
		"--output", "json",
	}
	output, err := cmd.CallCmd(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
	require.NoError(t, err)

	var env rollouts.Envelope
	require.NoError(t, json.Unmarshal(output, &env), "expected JSON envelope, got: %s", string(output))
	assert.Equal(t, "rollouts.v1beta1", env.SchemaVersion)
	assert.Equal(t, "Rollout", env.Kind)

	rawData, err := json.Marshal(env.Data)
	require.NoError(t, err)
	var got rollouts.Rollout
	require.NoError(t, json.Unmarshal(rawData, &got))
	assert.Equal(t, "r1", got.ID)
	assert.Equal(t, "active", got.Status.Kind)
	assert.Equal(t, "in_progress", got.Status.Status)
	mockClient.AssertExpectations(t)
	// Most-recent path must NOT call Get.
	mockClient.AssertNotCalled(t, "Get",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestStatus_RolloutIdPath_JSONOutput(t *testing.T) {
	mockClient := &rollouts.MockClient{}
	r := makeStatusRollout("RID", "guarded", "test", "monitoring_regressed", "regressed", "Regression on m-latency")
	mockClient.On("Get", "abcd1234", mock.Anything, "test-proj", "test", "RID").
		Return(&r, nil)

	args := []string{
		"flags", "rollouts-beta", "status",
		"--access-token", "abcd1234",
		"--flag", "test-flag",
		"--project", "test-proj",
		"--rollout-id", "RID",
		"--environment", "test",
		"--output", "json",
	}
	output, err := cmd.CallCmd(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
	require.NoError(t, err)

	var env rollouts.Envelope
	require.NoError(t, json.Unmarshal(output, &env), "expected JSON envelope, got: %s", string(output))
	assert.Equal(t, "Rollout", env.Kind)
	rawData, err := json.Marshal(env.Data)
	require.NoError(t, err)
	var got rollouts.Rollout
	require.NoError(t, json.Unmarshal(rawData, &got))
	assert.Equal(t, "RID", got.ID)
	assert.Equal(t, "guarded", got.Kind)
	assert.Equal(t, "regressed", got.Status.Kind)

	mockClient.AssertExpectations(t)
	// --rollout-id path must NOT call List.
	mockClient.AssertNotCalled(t, "List",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestStatus_RolloutIdWithoutEnvironment_ValidationError(t *testing.T) {
	mockClient := &rollouts.MockClient{}
	// Neither Get nor List should fire because the validation guard runs first.
	args := []string{
		"flags", "rollouts-beta", "status",
		"--access-token", "abcd1234",
		"--flag", "test-flag",
		"--project", "test-proj",
		"--rollout-id", "RID",
		"--output", "json",
	}
	stdout, _, err := cmd.CallCmdWithStderr(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
	require.Error(t, err)

	var env rollouts.Envelope
	require.NoError(t, json.Unmarshal(stdout, &env), "expected JSON error envelope on stdout, got: %s", string(stdout))
	assert.Equal(t, "Error", env.Kind)
	require.NotNil(t, env.Error)
	assert.Equal(t, "bad_request", env.Error.Code)
	assert.Contains(t, env.Error.Message, "--environment")
	assert.Contains(t, env.Error.Message, "--rollout-id")

	mockClient.AssertNotCalled(t, "Get",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	mockClient.AssertNotCalled(t, "List",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestStatus_NoRolloutsFound_ErrorEnvelopeOnStdout(t *testing.T) {
	mockClient := &rollouts.MockClient{}
	mockClient.On("List", "abcd1234", mock.Anything, "test-proj", "test-flag", mock.Anything).
		Return(&rollouts.RolloutList{Items: []rollouts.Rollout{}}, nil)

	args := []string{
		"flags", "rollouts-beta", "status",
		"--access-token", "abcd1234",
		"--flag", "test-flag",
		"--project", "test-proj",
		"--output", "json",
	}
	stdout, stderr, err := cmd.CallCmdWithStderr(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
	require.Error(t, err)

	var env rollouts.Envelope
	require.NoError(t, json.Unmarshal(stdout, &env), "expected JSON error envelope on stdout, got: %s", string(stdout))
	assert.Equal(t, "Error", env.Kind)
	require.NotNil(t, env.Error)
	assert.Equal(t, "no_rollouts_found", env.Error.Code)
	assert.Contains(t, env.Error.Message, "test-flag")

	// AGENT-04 / D-07: envelope must NOT leak to stderr.
	assert.NotContains(t, string(stderr), `"kind": "Error"`,
		"envelope must not leak onto stderr in JSON mode (AGENT-04)")
	assert.NotContains(t, string(stderr), `"schemaVersion": "rollouts.v1beta1"`,
		"envelope must not leak onto stderr in JSON mode (AGENT-04)")
}

func TestStatus_NoRolloutsFound_NilList_ErrorEnvelope(t *testing.T) {
	mockClient := &rollouts.MockClient{}
	// Same as the empty-list case but the client returns (nil, nil) — proves the nil-guard
	// in resolveRollout.
	mockClient.On("List", "abcd1234", mock.Anything, "test-proj", "test-flag", mock.Anything).
		Return(nil, nil)

	args := []string{
		"flags", "rollouts-beta", "status",
		"--access-token", "abcd1234",
		"--flag", "test-flag",
		"--project", "test-proj",
		"--output", "json",
	}
	stdout, _, err := cmd.CallCmdWithStderr(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
	require.Error(t, err)

	var env rollouts.Envelope
	require.NoError(t, json.Unmarshal(stdout, &env))
	assert.Equal(t, "Error", env.Kind)
	require.NotNil(t, env.Error)
	assert.Equal(t, "no_rollouts_found", env.Error.Code)
	assert.Contains(t, env.Error.Message, "test-flag")
}

func TestStatus_PlaintextOutput_ContainsSectionHeaders(t *testing.T) {
	t.Setenv("FORCE_TTY", "1")
	mockClient := &rollouts.MockClient{}
	startedAt := time.Date(2026, 5, 14, 10, 30, 0, 0, time.UTC)
	r := rollouts.Rollout{
		ID:                  "r1",
		FlagKey:             "test-flag",
		Kind:                "guarded",
		EnvironmentKey:      "prod",
		OriginalVariationID: "var-orig",
		TargetVariationID:   "var-target",
		CreatedAt:           time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
		StartedAt:           &startedAt,
		LatestStageIndex:    1,
		Status: rollouts.StatusBlock{
			Status: "in_progress",
			Kind:   "active",
			Label:  "Stage 2 of 3",
		},
		Stages: []rollouts.Stage{
			{StageIndex: 0, Allocation: 25000, DurationMillis: 3600000, Duration: "1h0m0s"},
			{StageIndex: 1, Allocation: 50000, DurationMillis: 3600000, Duration: "1h0m0s"},
			{StageIndex: 2, Allocation: 100000, DurationMillis: 3600000, Duration: "1h0m0s"},
		},
		MetricConfigurations: []rollouts.MetricConfiguration{
			{MetricKey: "latency-p99", Status: "ok", AutoRollback: false},
		},
		Events: []rollouts.Event{
			{Kind: "rollout_started", CreatedAt: startedAt},
		},
	}
	mockClient.On("List", "abcd1234", mock.Anything, "test-proj", "test-flag", mock.Anything).
		Return(&rollouts.RolloutList{Items: []rollouts.Rollout{r}}, nil)

	args := []string{
		"flags", "rollouts-beta", "status",
		"--access-token", "abcd1234",
		"--flag", "test-flag",
		"--project", "test-proj",
	}
	output, err := cmd.CallCmd(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
	require.NoError(t, err)
	s := string(output)
	assert.Contains(t, s, "Rollout:", "expected Rollout: header in plaintext output")
	assert.Contains(t, s, "Stages:", "expected Stages: section header")
	assert.Contains(t, s, "Metrics:", "expected Metrics: section header")
	assert.Contains(t, s, "Events:", "expected Events: section header")
	assert.Contains(t, s, "r1")
	assert.Contains(t, s, "guarded")
	assert.Contains(t, s, "prod")
	assert.Contains(t, s, "latency-p99")
	assert.Contains(t, s, "rollout_started")
}

func TestStatus_ListClientError_ErrorEnvelope(t *testing.T) {
	mockClient := &rollouts.MockClient{}
	mockClient.On("List", "abcd1234", mock.Anything, "test-proj", "test-flag", mock.Anything).
		Return(nil, &rollouts.RolloutError{
			Code:       rollouts.ErrCodeNotFound,
			Message:    "Feature flag not found",
			NextAction: "Verify --flag value",
		})

	args := []string{
		"flags", "rollouts-beta", "status",
		"--access-token", "abcd1234",
		"--flag", "test-flag",
		"--project", "test-proj",
		"--output", "json",
	}
	stdout, _, err := cmd.CallCmdWithStderr(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
	require.Error(t, err)

	var env rollouts.Envelope
	require.NoError(t, json.Unmarshal(stdout, &env), "expected JSON envelope on stdout, got: %s", string(stdout))
	assert.Equal(t, "Error", env.Kind)
	require.NotNil(t, env.Error)
	assert.Equal(t, "not_found", env.Error.Code)
	assert.Equal(t, "Feature flag not found", env.Error.Message)
	assert.Equal(t, "Verify --flag value", env.Error.NextAction)

	// Sanity: err is the short sentinel, not the envelope JSON.
	assert.NotContains(t, err.Error(), `"kind"`,
		"err.Error() must not be the JSON envelope; it is a short sentinel")
	// Sanity: stdout starts with '{' (envelope opening brace), not other content.
	assert.True(t, strings.HasPrefix(strings.TrimSpace(string(stdout)), "{"),
		"expected stdout to begin with envelope JSON; got: %s", string(stdout))
}

func TestStatus_MetricResults_FetchedWhenEnvProvided(t *testing.T) {
	mockClient := &rollouts.MockClient{}
	r := rollouts.Rollout{
		ID:        "RID",
		FlagKey:   "test-flag",
		Kind:      "guarded",
		CreatedAt: time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
		Status:    rollouts.StatusBlock{Status: "in_progress", Kind: "active", Label: "Monitoring"},
		MetricConfigurations: []rollouts.MetricConfiguration{
			{MetricKey: "metric-a", Status: "ok"},
			{MetricKey: "metric-b", Status: "regressed"},
		},
	}
	mockClient.On("Get", "abcd1234", mock.Anything, "test-proj", "test", "RID").
		Return(&r, nil)
	prob := 0.42
	mockClient.On("GetMetricResult", "abcd1234", mock.Anything, "test-proj", "test-flag", "test", "RID", "metric-a").
		Return(&rollouts.MetricResult{
			MetricKey:     "metric-a",
			ControlResult: &rollouts.MetricResultEstimate{Value: 0.1, Exposures: 100, Conversions: 10},
		}, &prob, nil)
	mockClient.On("GetMetricResult", "abcd1234", mock.Anything, "test-proj", "test-flag", "test", "RID", "metric-b").
		Return(&rollouts.MetricResult{
			MetricKey:     "metric-b",
			ControlResult: &rollouts.MetricResultEstimate{Value: 0.2, Exposures: 100, Conversions: 20},
		}, &prob, nil)

	args := []string{
		"flags", "rollouts-beta", "status",
		"--access-token", "abcd1234",
		"--flag", "test-flag",
		"--project", "test-proj",
		"--rollout-id", "RID",
		"--environment", "test",
		"--output", "json",
	}
	output, err := cmd.CallCmd(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
	require.NoError(t, err)

	var env rollouts.Envelope
	require.NoError(t, json.Unmarshal(output, &env))
	rawData, _ := json.Marshal(env.Data)
	var got rollouts.Rollout
	require.NoError(t, json.Unmarshal(rawData, &got))
	require.Len(t, got.MetricResults, 2)
	assert.Equal(t, "metric-a", got.MetricResults[0].MetricKey)
	assert.Equal(t, "metric-b", got.MetricResults[1].MetricKey)
	require.NotNil(t, got.ProbabilityOfMismatch, "probabilityOfMismatch lifts to rollout root per PC-020")
	assert.InDelta(t, 0.42, *got.ProbabilityOfMismatch, 0.0001)
	mockClient.AssertExpectations(t)
}

func TestStatus_MetricResults_EnvRecoveredFromLinksSelf(t *testing.T) {
	mockClient := &rollouts.MockClient{}
	r := rollouts.Rollout{
		ID:        "RID",
		FlagKey:   "test-flag",
		Kind:      "guarded",
		CreatedAt: time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
		Status:    rollouts.StatusBlock{Status: "in_progress", Kind: "active", Label: "Monitoring"},
		MetricConfigurations: []rollouts.MetricConfiguration{
			{MetricKey: "metric-a", Status: "ok"},
		},
		Links: map[string]rollouts.Link{
			"self": {Href: "/internal/projects/test-proj/environments/staging/automated-releases/RID"},
		},
	}
	// Most-recent path (no --rollout-id, no --environment) — List returns the rollout.
	mockClient.On("List", "abcd1234", mock.Anything, "test-proj", "test-flag", mock.Anything).
		Return(&rollouts.RolloutList{Items: []rollouts.Rollout{r}}, nil)
	// envKeyFromLinks should recover "staging" from the _links.self.href.
	mockClient.On("GetMetricResult", "abcd1234", mock.Anything, "test-proj", "test-flag", "staging", "RID", "metric-a").
		Return(&rollouts.MetricResult{MetricKey: "metric-a"}, (*float64)(nil), nil)

	args := []string{
		"flags", "rollouts-beta", "status",
		"--access-token", "abcd1234",
		"--flag", "test-flag",
		"--project", "test-proj",
		"--output", "json",
	}
	output, err := cmd.CallCmd(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
	require.NoError(t, err)

	var env rollouts.Envelope
	require.NoError(t, json.Unmarshal(output, &env))
	rawData, _ := json.Marshal(env.Data)
	var got rollouts.Rollout
	require.NoError(t, json.Unmarshal(rawData, &got))
	assert.Len(t, got.MetricResults, 1)
	mockClient.AssertExpectations(t)
}

func TestStatus_MetricResults_SkippedWhenNoMetricConfigs(t *testing.T) {
	mockClient := &rollouts.MockClient{}
	r := rollouts.Rollout{
		ID:        "RID",
		FlagKey:   "test-flag",
		Kind:      "progressive",
		CreatedAt: time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
		Status:    rollouts.StatusBlock{Status: "in_progress", Kind: "active", Label: "Stage 1 of 3"},
		// No MetricConfigurations — progressive rollouts don't monitor metrics.
	}
	mockClient.On("List", "abcd1234", mock.Anything, "test-proj", "test-flag", mock.Anything).
		Return(&rollouts.RolloutList{Items: []rollouts.Rollout{r}}, nil)

	args := []string{
		"flags", "rollouts-beta", "status",
		"--access-token", "abcd1234",
		"--flag", "test-flag",
		"--project", "test-proj",
		"--output", "json",
	}
	_, err := cmd.CallCmd(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
	require.NoError(t, err)

	mockClient.AssertExpectations(t)
	mockClient.AssertNotCalled(t, "GetMetricResult",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}
