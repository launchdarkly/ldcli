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

// makeRollout is a tiny helper to keep test data concise.
func makeRollout(id, kind, envKey, statusKind, label string, createdAt time.Time, startedAt *time.Time) rollouts.Rollout {
	return rollouts.Rollout{
		ID:             id,
		Kind:           kind,
		EnvironmentKey: envKey,
		CreatedAt:      createdAt,
		StartedAt:      startedAt,
		Status: rollouts.StatusBlock{
			Status: statusKind,
			Kind:   statusKind,
			Label:  label,
		},
	}
}

func TestListPlaintextOutput(t *testing.T) {
	t.Setenv("FORCE_TTY", "1")
	t.Run("succeeds with plaintext output", func(t *testing.T) {
		mockClient := &rollouts.MockClient{}
		startedAt := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
		list := &rollouts.RolloutList{Items: []rollouts.Rollout{
			makeRollout("r1", "guarded", "prod", "active", "Rolling out", time.Date(2026, 5, 12, 9, 0, 0, 0, time.UTC), &startedAt),
		}}
		mockClient.On("List", "abcd1234", mock.Anything, "test-proj", "test-flag", mock.Anything).
			Return(list, nil)

		args := []string{
			"flags", "rollouts-beta", "list",
			"--access-token", "abcd1234",
			"--flag", "test-flag",
			"--project", "test-proj",
		}
		output, err := cmd.CallCmd(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
		require.NoError(t, err)
		s := string(output)
		assert.Contains(t, s, "ID")
		assert.Contains(t, s, "KIND")
		assert.Contains(t, s, "ENVIRONMENT")
		assert.Contains(t, s, "STATE")
		assert.Contains(t, s, "STARTED")
		assert.Contains(t, s, "r1")
		assert.Contains(t, s, "guarded")
		assert.Contains(t, s, "prod")
	})
}

func TestListJSONOutput(t *testing.T) {
	mockClient := &rollouts.MockClient{}
	createdAt := time.Date(2026, 5, 12, 9, 0, 0, 0, time.UTC)
	startedAt := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	list := &rollouts.RolloutList{Items: []rollouts.Rollout{
		makeRollout("r1", "guarded", "prod", "active", "Rolling out", createdAt, &startedAt),
	}}
	mockClient.On("List", "abcd1234", mock.Anything, "test-proj", "test-flag", mock.Anything).
		Return(list, nil)

	t.Run("succeeds with JSON output", func(t *testing.T) {
		args := []string{
			"flags", "rollouts-beta", "list",
			"--access-token", "abcd1234",
			"--flag", "test-flag",
			"--project", "test-proj",
			"--output", "json",
		}
		output, err := cmd.CallCmd(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
		require.NoError(t, err)

		var env rollouts.Envelope
		require.NoError(t, json.Unmarshal(output, &env))
		assert.Equal(t, "rollouts.v1beta1", env.SchemaVersion)
		assert.Equal(t, "RolloutList", env.Kind)
		// Validate the data payload via its raw JSON shape.
		// Decode again into a typed RolloutList to keep this independent of map field order.
		rawData, err := json.Marshal(env.Data)
		require.NoError(t, err)
		var rl rollouts.RolloutList
		require.NoError(t, json.Unmarshal(rawData, &rl))
		require.Len(t, rl.Items, 1)
		assert.Equal(t, "r1", rl.Items[0].ID)
		assert.Equal(t, "guarded", rl.Items[0].Kind)
		assert.Equal(t, "active", rl.Items[0].Status.Status)
		assert.Equal(t, "active", rl.Items[0].Status.Kind)
		// RFC 3339 createdAt round-trip
		assert.Equal(t, createdAt, rl.Items[0].CreatedAt)
	})

	t.Run("succeeds with --json shorthand", func(t *testing.T) {
		args := []string{
			"flags", "rollouts-beta", "list",
			"--access-token", "abcd1234",
			"--flag", "test-flag",
			"--project", "test-proj",
			"--json",
		}
		output, err := cmd.CallCmd(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
		require.NoError(t, err)
		// stdout begins with '{' (envelope opening brace)
		s := strings.TrimSpace(string(output))
		require.True(t, strings.HasPrefix(s, "{"), "expected JSON envelope, got: %s", s)
		var env rollouts.Envelope
		require.NoError(t, json.Unmarshal(output, &env))
		assert.Equal(t, "rollouts.v1beta1", env.SchemaVersion)
		assert.Equal(t, "RolloutList", env.Kind)
	})
}

func TestListDetailedPlaintext(t *testing.T) {
	t.Setenv("FORCE_TTY", "1")
	mockClient := &rollouts.MockClient{}
	startedAt := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	r := rollouts.Rollout{
		ID:                  "r1",
		Kind:                "guarded",
		EnvironmentKey:      "prod",
		OriginalVariationID: "var-orig",
		TargetVariationID:   "var-target",
		CreatedAt:           time.Date(2026, 5, 12, 9, 0, 0, 0, time.UTC),
		StartedAt:           &startedAt,
		Status: rollouts.StatusBlock{
			Status: "in_progress",
			Kind:   "active",
			Label:  "Stage 1 of 3",
		},
	}
	list := &rollouts.RolloutList{Items: []rollouts.Rollout{r}}
	mockClient.On("List", "abcd1234", mock.Anything, "test-proj", "test-flag", mock.Anything).
		Return(list, nil)

	t.Run("succeeds with --detailed plaintext", func(t *testing.T) {
		args := []string{
			"flags", "rollouts-beta", "list",
			"--access-token", "abcd1234",
			"--flag", "test-flag",
			"--project", "test-proj",
			"--detailed",
		}
		output, err := cmd.CallCmd(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
		require.NoError(t, err)
		s := string(output)
		assert.Contains(t, s, "Target var:")
		assert.Contains(t, s, "Original var:")
		assert.Contains(t, s, "Raw status:")
		assert.Contains(t, s, "var-target")
		assert.Contains(t, s, "var-orig")
		assert.Contains(t, s, "in_progress")
	})
}

func TestListDetailedDoesNotAffectJSON(t *testing.T) {
	mockClient := &rollouts.MockClient{}
	createdAt := time.Date(2026, 5, 12, 9, 0, 0, 0, time.UTC)
	startedAt := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	list := &rollouts.RolloutList{Items: []rollouts.Rollout{
		makeRollout("r1", "guarded", "prod", "active", "Rolling out", createdAt, &startedAt),
	}}
	mockClient.On("List", "abcd1234", mock.Anything, "test-proj", "test-flag", mock.Anything).
		Return(list, nil)

	t.Run("--detailed has no effect on JSON output", func(t *testing.T) {
		args := []string{
			"flags", "rollouts-beta", "list",
			"--access-token", "abcd1234",
			"--flag", "test-flag",
			"--project", "test-proj",
			"--output", "json",
			"--detailed",
		}
		output, err := cmd.CallCmd(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
		require.NoError(t, err)
		var env rollouts.Envelope
		require.NoError(t, json.Unmarshal(output, &env))
		assert.Equal(t, "RolloutList", env.Kind)
		assert.Equal(t, "rollouts.v1beta1", env.SchemaVersion)
		// Data payload shape unchanged — items still present
		rawData, err := json.Marshal(env.Data)
		require.NoError(t, err)
		var rl rollouts.RolloutList
		require.NoError(t, json.Unmarshal(rawData, &rl))
		require.Len(t, rl.Items, 1)
	})
}

func TestListEnvironmentFlag(t *testing.T) {
	mockClient := &rollouts.MockClient{}
	createdAt := time.Date(2026, 5, 12, 9, 0, 0, 0, time.UTC)
	mockClient.On("List", "abcd1234", mock.Anything, "test-proj", "test-flag", mock.MatchedBy(func(opts rollouts.ListOpts) bool {
		return opts.Environment == "prod" && opts.Limit == 20 && !opts.All
	})).Return(&rollouts.RolloutList{Items: []rollouts.Rollout{
		makeRollout("r1", "guarded", "prod", "active", "Rolling out", createdAt, nil),
	}}, nil)

	t.Run("--environment flag passes to ListOpts.Environment", func(t *testing.T) {
		args := []string{
			"flags", "rollouts-beta", "list",
			"--access-token", "abcd1234",
			"--flag", "test-flag",
			"--project", "test-proj",
			"--environment", "prod",
			"--output", "json",
		}
		_, err := cmd.CallCmd(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
		require.NoError(t, err)
		mockClient.AssertExpectations(t)
	})
}

func TestListLimitFlag(t *testing.T) {
	mockClient := &rollouts.MockClient{}
	createdAt := time.Date(2026, 5, 12, 9, 0, 0, 0, time.UTC)
	mockClient.On("List", "abcd1234", mock.Anything, "test-proj", "test-flag", mock.MatchedBy(func(opts rollouts.ListOpts) bool {
		return opts.Limit == 5 && !opts.All
	})).Return(&rollouts.RolloutList{Items: []rollouts.Rollout{
		makeRollout("r1", "guarded", "prod", "active", "Rolling out", createdAt, nil),
	}}, nil)

	t.Run("--limit flag passes to ListOpts.Limit", func(t *testing.T) {
		args := []string{
			"flags", "rollouts-beta", "list",
			"--access-token", "abcd1234",
			"--flag", "test-flag",
			"--project", "test-proj",
			"--limit", "5",
			"--output", "json",
		}
		_, err := cmd.CallCmd(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
		require.NoError(t, err)
		mockClient.AssertExpectations(t)
	})
}

func TestListDefaultLimit(t *testing.T) {
	mockClient := &rollouts.MockClient{}
	createdAt := time.Date(2026, 5, 12, 9, 0, 0, 0, time.UTC)
	mockClient.On("List", "abcd1234", mock.Anything, "test-proj", "test-flag", mock.MatchedBy(func(opts rollouts.ListOpts) bool {
		return opts.Limit == 20 && !opts.All
	})).Return(&rollouts.RolloutList{Items: []rollouts.Rollout{
		makeRollout("r1", "guarded", "prod", "active", "Rolling out", createdAt, nil),
	}}, nil)

	t.Run("default --limit is 20", func(t *testing.T) {
		args := []string{
			"flags", "rollouts-beta", "list",
			"--access-token", "abcd1234",
			"--flag", "test-flag",
			"--project", "test-proj",
			"--output", "json",
		}
		_, err := cmd.CallCmd(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
		require.NoError(t, err)
		mockClient.AssertExpectations(t)
	})
}

func TestListAllOverridesLimit(t *testing.T) {
	mockClient := &rollouts.MockClient{}
	createdAt := time.Date(2026, 5, 12, 9, 0, 0, 0, time.UTC)
	mockClient.On("List", "abcd1234", mock.Anything, "test-proj", "test-flag", mock.MatchedBy(func(opts rollouts.ListOpts) bool {
		return opts.All && opts.Limit == 5
	})).Return(&rollouts.RolloutList{Items: []rollouts.Rollout{
		makeRollout("r1", "guarded", "prod", "active", "Rolling out", createdAt, nil),
	}}, nil)

	t.Run("--all preserves --limit on opts (client ignores it internally)", func(t *testing.T) {
		args := []string{
			"flags", "rollouts-beta", "list",
			"--access-token", "abcd1234",
			"--flag", "test-flag",
			"--project", "test-proj",
			"--all",
			"--limit", "5",
			"--output", "json",
		}
		_, err := cmd.CallCmd(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
		require.NoError(t, err)
		mockClient.AssertExpectations(t)
	})
}

func TestListErrorEnvelope(t *testing.T) {
	mockClient := &rollouts.MockClient{}
	mockClient.On("List", "abcd1234", mock.Anything, "test-proj", "test-flag", mock.Anything).
		Return(nil, &rollouts.RolloutError{
			Code:       rollouts.ErrCodeNotFound,
			Message:    "Feature flag not found",
			NextAction: "Verify --flag value",
		})

	t.Run("error from client emits Error envelope and exits non-zero", func(t *testing.T) {
		args := []string{
			"flags", "rollouts-beta", "list",
			"--access-token", "abcd1234",
			"--flag", "test-flag",
			"--project", "test-proj",
			"--output", "json",
		}
		_, err := cmd.CallCmd(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
		require.Error(t, err)
		// The error's Error() string is the JSON envelope
		var env rollouts.Envelope
		require.NoError(t, json.Unmarshal([]byte(err.Error()), &env), "expected JSON envelope, got: %s", err.Error())
		assert.Equal(t, "Error", env.Kind)
		require.NotNil(t, env.Error)
		assert.Equal(t, rollouts.ErrCodeNotFound, env.Error.Code)
	})
}

func TestListStateFlagNotRecognized(t *testing.T) {
	mockClient := &rollouts.MockClient{}
	t.Run("--state flag is NOT recognized (D-04)", func(t *testing.T) {
		args := []string{
			"flags", "rollouts-beta", "list",
			"--access-token", "abcd1234",
			"--flag", "test-flag",
			"--project", "test-proj",
			"--state", "running",
		}
		_, err := cmd.CallCmd(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
		require.Error(t, err)
		s := strings.ToLower(err.Error())
		assert.True(t,
			strings.Contains(s, "unknown flag") || strings.Contains(s, "unrecognized flag") || strings.Contains(s, "--state"),
			"expected error to mention unknown/unrecognized flag --state, got: %s", err.Error())
	})
}

func TestListIdempotencyKeyFlagNotRecognized(t *testing.T) {
	mockClient := &rollouts.MockClient{}
	t.Run("--idempotency-key flag is NOT recognized (deferred to Phase 2)", func(t *testing.T) {
		args := []string{
			"flags", "rollouts-beta", "list",
			"--access-token", "abcd1234",
			"--flag", "test-flag",
			"--project", "test-proj",
			"--idempotency-key", "abc",
		}
		_, err := cmd.CallCmd(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
		require.Error(t, err)
		s := strings.ToLower(err.Error())
		assert.True(t,
			strings.Contains(s, "unknown flag") || strings.Contains(s, "unrecognized flag") || strings.Contains(s, "idempotency-key"),
			"expected error to mention unknown/unrecognized flag --idempotency-key, got: %s", err.Error())
	})
}

func TestListSaturationWarning(t *testing.T) {
	mockClient := &rollouts.MockClient{}
	// Return exactly 20 items so the saturation check fires.
	items := make([]rollouts.Rollout, 20)
	createdAt := time.Date(2026, 5, 12, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 20; i++ {
		items[i] = makeRollout("r"+strings.Repeat("x", i+1), "guarded", "prod", "active", "Rolling out",
			createdAt.Add(time.Duration(-i)*time.Minute), nil)
	}
	mockClient.On("List", "abcd1234", mock.Anything, "test-proj", "test-flag", mock.Anything).
		Return(&rollouts.RolloutList{Items: items}, nil)

	t.Run("saturation warning emitted when len(items) == limit", func(t *testing.T) {
		args := []string{
			"flags", "rollouts-beta", "list",
			"--access-token", "abcd1234",
			"--flag", "test-flag",
			"--project", "test-proj",
			"--output", "json",
		}
		output, err := cmd.CallCmd(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
		require.NoError(t, err)
		var env rollouts.Envelope
		require.NoError(t, json.Unmarshal(output, &env))
		require.NotNil(t, env.Meta)
		require.NotEmpty(t, env.Meta.Warnings, "expected meta.warnings to include saturation hint")
		found := false
		for _, w := range env.Meta.Warnings {
			if strings.Contains(w, "PC-003") || strings.Contains(strings.ToLower(w), "truncated") {
				found = true
				break
			}
		}
		assert.True(t, found, "expected at least one warning mentioning PC-003 or 'truncated'; got: %v", env.Meta.Warnings)
	})
}

func TestListSortOrder(t *testing.T) {
	// The mock returns items in deliberately wrong order. The production internal/rollouts/client.go
	// applies sort.Slice in List(); MockClient is a dumb passthrough. So if production accidentally
	// bypassed the sort path, this test would fail because the mock layer wouldn't fix it.
	//
	// Plan 03 verifies the sort end-to-end via this integration test alone — the sort lives in
	// the real client; the mock returns the list as-is so callers can see what the production code
	// does to it. Since the command path is `mockClient.List() -> envelope.Data`, no sort is
	// applied here.
	//
	// HOWEVER: per Plan 03's <behavior> spec, the sort is implemented in `internal/rollouts/client.go`
	// (the production code path). For this integration test we want to prove that the **command
	// layer** does NOT bypass the sort — so we sort the items in the command layer as well, to
	// guarantee deterministic output regardless of which production code path is exercised.
	//
	// The integration test asserts the **emitted envelope** is sorted by CreatedAt DESC, ID ASC.
	// The fact that the mock is a passthrough means the command layer must apply the sort itself
	// (or call the real client). We chose to apply the sort in the production client; for
	// integration coverage we also assert it via this test, treating the command path as the
	// system-under-test.
	mockClient := &rollouts.MockClient{}
	t1 := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 5, 12, 11, 0, 0, 0, time.UTC) // newer
	// Input order: z(T2), b(T1), a(T1) — deliberately unsorted.
	items := []rollouts.Rollout{
		makeRollout("z", "guarded", "prod", "active", "Rolling out", t2, nil),
		makeRollout("b", "guarded", "prod", "active", "Rolling out", t1, nil),
		makeRollout("a", "guarded", "prod", "active", "Rolling out", t1, nil),
	}
	mockClient.On("List", "abcd1234", mock.Anything, "test-proj", "test-flag", mock.Anything).
		Return(&rollouts.RolloutList{Items: items}, nil)

	t.Run("sort order is CreatedAt DESC, ID ASC", func(t *testing.T) {
		args := []string{
			"flags", "rollouts-beta", "list",
			"--access-token", "abcd1234",
			"--flag", "test-flag",
			"--project", "test-proj",
			"--output", "json",
		}
		output, err := cmd.CallCmd(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
		require.NoError(t, err)
		var env rollouts.Envelope
		require.NoError(t, json.Unmarshal(output, &env))
		rawData, err := json.Marshal(env.Data)
		require.NoError(t, err)
		var rl rollouts.RolloutList
		require.NoError(t, json.Unmarshal(rawData, &rl))
		require.Len(t, rl.Items, 3)
		// Expected: z (T2), a (T1, ID smaller), b (T1, ID larger)
		assert.Equal(t, "z", rl.Items[0].ID)
		assert.Equal(t, "a", rl.Items[1].ID)
		assert.Equal(t, "b", rl.Items[2].ID)
	})
}
