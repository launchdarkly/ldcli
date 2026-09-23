package prompt

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
)

func TestConfirmApply(t *testing.T) {
	for _, test := range []struct {
		name      string
		input     string
		terminal  bool
		confirmed bool
		wantError string
	}{
		{"yes", "yes\n", true, true, ""},
		{"declined", "n\n", true, false, ""},
		{"non-terminal", "", false, false, "rerun with --yes"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var prompt bytes.Buffer
			confirmed, err := confirmApply(
				strings.NewReader(test.input),
				&prompt,
				func(_ io.Reader, _ io.Writer) bool {
					return test.terminal
				},
			)

			assert.Equal(t, test.confirmed, confirmed)
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
			} else {
				require.NoError(t, err)
				assert.Contains(t, prompt.String(), "Sync these changes?")
			}
		})
	}
}

func TestIsServerPull(t *testing.T) {
	for _, test := range []struct {
		name         string
		status       syncapi.ResourceStatus
		direction    syncapi.SyncDirection
		hasError     bool
		localDeleted bool
		want         bool
	}{
		{
			name:      "both",
			status:    syncapi.ResourceStatusServerChanged,
			direction: syncapi.SyncDirectionBoth,
			want:      true,
		},
		{
			name:      "server canonical",
			status:    syncapi.ResourceStatusServerChanged,
			direction: syncapi.SyncDirectionServerCanonical,
			want:      true,
		},
		{
			name:         "server canonical local change",
			status:       syncapi.ResourceStatusLocalChanged,
			direction:    syncapi.SyncDirectionServerCanonical,
			localDeleted: true,
			want:         true,
		},
		{
			name:         "server canonical conflict",
			status:       syncapi.ResourceStatusConflict,
			direction:    syncapi.SyncDirectionServerCanonical,
			localDeleted: true,
			want:         true,
		},
		{
			name:      "server canonical unrelated local change",
			status:    syncapi.ResourceStatusLocalChanged,
			direction: syncapi.SyncDirectionServerCanonical,
		},
		{
			name:      "code canonical",
			status:    syncapi.ResourceStatusServerChanged,
			direction: syncapi.SyncDirectionCodeCanonical,
		},
		{
			name:      "conflict",
			status:    syncapi.ResourceStatusConflict,
			direction: syncapi.SyncDirectionBoth,
		},
		{
			name:      "resource error",
			status:    syncapi.ResourceStatusServerChanged,
			direction: syncapi.SyncDirectionBoth,
			hasError:  true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			resource := syncapi.PlannedResource{
				ResourceKind:  syncdomain.KindVariation,
				Status:        test.status,
				SyncDirection: test.direction,
				LocalDeleted:  test.localDeleted,
			}
			if test.hasError {
				resource.Error = &syncapi.ResourceError{Message: "blocked"}
			}

			assert.Equal(t, test.want, isServerPull(resource))
		})
	}
}

func TestValidatePlansForApplyAllowsInSyncServerCanonicalResource(t *testing.T) {
	plans := []syncapi.ProjectPlan{{
		ProjectKey: "project",
		Resources: []syncapi.PlannedResource{{
			ResourceKind:  syncdomain.KindVariation,
			LookupKey:     "support/default",
			Status:        syncapi.ResourceStatusInSync,
			SyncDirection: syncapi.SyncDirectionServerCanonical,
		}},
	}}

	require.NoError(t, validatePlansForApply(plans))

	plans[0].Resources[0].Status = syncapi.ResourceStatusLocalChanged
	require.ErrorContains(t, validatePlansForApply(plans), "server-canonical")
}

func TestCodeCanonicalServerDeletionUsesLocalState(t *testing.T) {
	plan := syncapi.ProjectPlan{
		ProjectKey: "project",
		Resources: []syncapi.PlannedResource{
			{
				ResourceKind:  syncdomain.KindVariation,
				LookupKey:     "support/deleted-on-server",
				Status:        syncapi.ResourceStatusServerChanged,
				SyncDirection: syncapi.SyncDirectionCodeCanonical,
				ServerDeleted: true,
			},
		},
	}

	require.NoError(t, validatePlansForSync([]syncapi.ProjectPlan{plan}))
	require.NoError(t, validatePlansForApply([]syncapi.ProjectPlan{plan}))

	plan.Resources = append(plan.Resources, syncapi.PlannedResource{
		ResourceKind:  syncdomain.KindVariation,
		LookupKey:     "support/conflict",
		Status:        syncapi.ResourceStatusConflict,
		SyncDirection: syncapi.SyncDirectionCodeCanonical,
		LocalDeleted:  true,
	})
	require.ErrorContains(t, validatePlansForSync([]syncapi.ProjectPlan{plan}), "conflict")
	require.ErrorContains(t, validatePlansForApply([]syncapi.ProjectPlan{plan}), "conflict")
}

func TestFormatExpiration(t *testing.T) {
	assert.Equal(
		t,
		"December 14, 2026 at 7:00 AM EST",
		formatExpirationIn(
			"2026-12-14T12:00:00Z",
			time.FixedZone("EST", -5*60*60),
		),
	)
	assert.Equal(
		t,
		"unknown",
		formatExpirationIn("unknown", time.UTC),
	)
}

func TestWritePlanReviewExplainsWhichSideWillChange(t *testing.T) {
	var output bytes.Buffer
	err := writePlanReview(
		&output,
		"plaintext",
		[]syncapi.ProjectPlan{{
			ProjectKey: "project",
			Resources: []syncapi.PlannedResource{
				{
					ResourceKind:  syncdomain.KindVariation,
					LookupKey:     "support/server-change",
					Status:        syncapi.ResourceStatusServerChanged,
					SyncDirection: syncapi.SyncDirectionBoth,
					Diff: json.RawMessage(
						`{"name":{"before":"Server value","after":"Local value"}}`,
					),
				},
				{
					ResourceKind:  syncdomain.KindVariation,
					LookupKey:     "support/local-change",
					Status:        syncapi.ResourceStatusLocalChanged,
					SyncDirection: syncapi.SyncDirectionBoth,
					Diff: json.RawMessage(
						`{"name":{"before":"Server old","after":"Local new"}}`,
					),
				},
				{
					ResourceKind:  syncdomain.KindVariation,
					LookupKey:     "support/unchanged",
					Status:        syncapi.ResourceStatusInSync,
					SyncDirection: syncapi.SyncDirectionBoth,
				},
				{
					ResourceKind:  syncdomain.KindVariation,
					LookupKey:     "support/local-deleted",
					Status:        syncapi.ResourceStatusLocalChanged,
					SyncDirection: syncapi.SyncDirectionBoth,
					LocalDeleted:  true,
					Diff: json.RawMessage(
						`{"name":{"before":"Deleted locally"}}`,
					),
				},
				{
					ResourceKind:  syncdomain.KindVariation,
					LookupKey:     "support/server-deleted",
					Status:        syncapi.ResourceStatusServerChanged,
					SyncDirection: syncapi.SyncDirectionBoth,
					ServerDeleted: true,
					Diff: json.RawMessage(
						`{"name":{"after":"Deleted on server"}}`,
					),
				},
			},
		}},
		0,
	)

	require.NoError(t, err)
	rendered := output.String()
	assert.NotContains(t, rendered, "Direction:")
	assert.NotContains(t, rendered, "server_changed")
	assert.NotContains(t, rendered, "local_changed")

	assert.Contains(t, rendered, "Status: LaunchDarkly changes detected")
	assert.Contains(t, rendered, "Action: Update the local file from LaunchDarkly.")
	assert.Contains(t, rendered, "--- Local file now")
	assert.Contains(t, rendered, "+++ Local file after sync (from LaunchDarkly)")
	assert.Contains(t, rendered, `-"Local value"`)
	assert.Contains(t, rendered, `+"Server value"`)

	assert.Contains(t, rendered, "Status: Local changes detected")
	assert.Contains(t, rendered, "Action: Update LaunchDarkly from the local file.")
	assert.Contains(t, rendered, "--- LaunchDarkly now")
	assert.Contains(t, rendered, "+++ LaunchDarkly after sync (from local file)")
	assert.Contains(t, rendered, `-"Server old"`)
	assert.Contains(t, rendered, `+"Local new"`)
	assert.Contains(t, rendered, "Status: In sync")
	assert.Contains(t, rendered, "Status: Local file removed")
	assert.Contains(t, rendered, "Action: Delete the variation from LaunchDarkly.")
	assert.Contains(t, rendered, "Status: LaunchDarkly variation removed")
	assert.Contains(t, rendered, "Action: Delete the local file.")
}

func TestWriteApplyResultsShowsResourceOutcomes(t *testing.T) {
	var output bytes.Buffer
	err := writeApplyResults(
		&output,
		"plaintext",
		[]syncapi.ProjectApply{{
			ProjectKey: "project",
			PlanID:     "617c83f1-cd9a-4865-8f37-bb11f88e2147",
			Status:     syncapi.PlanStatusApplied,
			Resources: []syncapi.AppliedResource{
				{
					LookupKey: "support/unchanged",
					Outcome:   syncapi.ResourceApplyOutcomeApplied,
				},
				{
					LookupKey: "support/changed",
					Outcome:   syncapi.ResourceApplyOutcomeApplied,
				},
				{
					LookupKey: "support/failed",
					Outcome:   syncapi.ResourceApplyOutcomeFailed,
					Error: &syncapi.ResourceError{
						Code:    "manifest_changed",
						Message: "manifest changed",
					},
				},
			},
		}},
	)

	require.NoError(t, err)
	assert.Contains(t, output.String(), "support/unchanged  outcome=applied")
	assert.Contains(t, output.String(), "support/changed  outcome=applied")
	assert.Contains(t, output.String(), "support/failed  outcome=failed")
	assert.Contains(t, output.String(), "manifest_changed: manifest changed")
}

func TestRenderVariationDiffSortsFieldsAndUsesUnifiedFallback(t *testing.T) {
	rendered, err := renderVariationDiff(
		json.RawMessage(`{
			"model": {
				"before": {"parameters": {"temperature": 0.2}},
				"after": {"parameters": {"temperature": 0.3}}
			},
			"instructions": {"after": "Be helpful"}
		}`),
		"plaintext",
		0,
		variationDiffPresentation{beforeLabel: "Before", afterLabel: "After"},
	)

	require.NoError(t, err)
	assert.Less(t, strings.Index(rendered, "instructions"), strings.Index(rendered, "model"))
	assert.Contains(t, rendered, "instructions (added)")
	assert.Contains(t, rendered, "\n  instructions (added)")
	assert.Contains(t, rendered, "\n\n  model (changed)")
	assert.Contains(t, rendered, "--- Before")
	assert.Contains(t, rendered, "+++ After")
	assert.Contains(t, rendered, "-(not present)")
	assert.Contains(t, rendered, `+"Be helpful"`)
	assert.Contains(t, rendered, `-    "temperature": 0.2`)
	assert.Contains(t, rendered, `+    "temperature": 0.3`)
	assert.NotContains(t, rendered, "\x1b[")
}

func TestRenderVariationDiffUsesSideBySideOutputForWideTerminal(t *testing.T) {
	rendered, err := renderVariationDiff(
		json.RawMessage(`{"name":{"before":"Old","after":"New"}}`),
		"plaintext",
		120,
		variationDiffPresentation{beforeLabel: "Before", afterLabel: "After"},
	)

	require.NoError(t, err)
	assert.Contains(t, rendered, "name (changed)")
	assert.Contains(t, rendered, "Before")
	assert.Contains(t, rendered, "After")
	assert.Contains(t, rendered, "Old")
	assert.Contains(t, rendered, "New")
	assert.NotContains(t, rendered, "╭")
	assert.True(t, containsLineWith(rendered, "Before", "After"))
	assert.True(t, containsLineWith(rendered, "Old", "New"))
}

func TestRenderVariationDiffUsesStackedOutputForNarrowTerminal(t *testing.T) {
	rendered, err := renderVariationDiff(
		json.RawMessage(`{"name":{"before":"Old","after":"New"}}`),
		"plaintext",
		80,
		variationDiffPresentation{beforeLabel: "Before", afterLabel: "After"},
	)

	require.NoError(t, err)
	assert.Contains(t, rendered, "Before")
	assert.Contains(t, rendered, "After")
	assert.False(t, containsLineWith(rendered, "Before", "After"))
	assert.False(t, containsLineWith(rendered, "Old", "New"))
}

func TestRenderVariationDiffWrapsWideColumnsWithoutLosingContent(t *testing.T) {
	rendered, err := renderVariationDiff(
		json.RawMessage(`{
			"instructions": {
				"before": "A long instruction value that ends with oldtailmarker",
				"after": "A long instruction value that ends with newtailmarker"
			}
		}`),
		"plaintext",
		100,
		variationDiffPresentation{beforeLabel: "Before", afterLabel: "After"},
	)

	require.NoError(t, err)
	assert.Contains(t, rendered, "oldtailmarker")
	assert.Contains(t, rendered, "newtailmarker")
}

func TestRenderVariationDiffUsesMarkdownFallback(t *testing.T) {
	rendered, err := renderVariationDiff(
		json.RawMessage(`{"name":{"before":"Old","after":"New"}}`),
		"markdown",
		120,
		variationDiffPresentation{beforeLabel: "Before", afterLabel: "After"},
	)

	require.NoError(t, err)
	assert.Contains(t, rendered, "#### name (changed)")
	assert.Contains(t, rendered, "```diff")
	assert.Contains(t, rendered, `-"Old"`)
	assert.Contains(t, rendered, `+"New"`)
	assert.NotContains(t, rendered, "╭")
	assert.NotContains(t, rendered, "\x1b[")
}

func TestRenderVariationDiffCollapsesWholeVariationChanges(t *testing.T) {
	for _, test := range []struct {
		name          string
		payload       json.RawMessage
		expectedKind  string
		unexpectedKey string
	}{
		{
			name: "added",
			payload: json.RawMessage(`{
				"key": {"after": "new"},
				"mode": {"after": "completion"},
				"name": {"after": "New"}
			}`),
			expectedKind:  "variation (added)",
			unexpectedKey: "key (added)",
		},
		{
			name: "removed",
			payload: json.RawMessage(`{
				"key": {"before": "old"},
				"mode": {"before": "completion"},
				"name": {"before": "Old"}
			}`),
			expectedKind:  "variation (removed)",
			unexpectedKey: "key (removed)",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			rendered, err := renderVariationDiff(
				test.payload,
				"plaintext",
				0,
				variationDiffPresentation{
					beforeLabel: "Before",
					afterLabel:  "After",
				},
			)

			require.NoError(t, err)
			assert.Contains(t, rendered, test.expectedKind)
			assert.NotContains(t, rendered, test.unexpectedKey)
			assert.Equal(t, 1, strings.Count(rendered, "--- Before"))
			assert.Equal(t, 1, strings.Count(rendered, "+++ After"))
			assert.Contains(t, rendered, `"key":`)
			assert.Contains(t, rendered, `"mode":`)
			assert.Contains(t, rendered, `"name":`)
		})
	}
}

func TestChangedPartsHighlightsOnlyChangedRunes(t *testing.T) {
	prefix, removed, added, suffix := changedParts(
		`    "temperature": 0.2`,
		`    "temperature": 0.3`,
	)

	assert.Equal(t, `    "temperature": 0.`, prefix)
	assert.Equal(t, "2", removed)
	assert.Equal(t, "3", added)
	assert.Empty(t, suffix)
}

func containsLineWith(value string, fragments ...string) bool {
	for _, line := range strings.Split(value, "\n") {
		matches := true
		for _, fragment := range fragments {
			if !strings.Contains(line, fragment) {
				matches = false
				break
			}
		}
		if matches {
			return true
		}
	}
	return false
}

func TestWriteSyncOutputJSONIncludesPartialApplyOutcomes(t *testing.T) {
	var output bytes.Buffer
	err := writeSyncOutput(
		&output,
		"json",
		nil,
		[]syncapi.ProjectPlan{{
			ProjectKey: "project",
			PlanID:     "617c83f1-cd9a-4865-8f37-bb11f88e2147",
		}},
		[]syncapi.ProjectApply{{
			ProjectKey: "project",
			PlanID:     "617c83f1-cd9a-4865-8f37-bb11f88e2147",
			Status:     syncapi.PlanStatusFailed,
			Resources: []syncapi.AppliedResource{{
				ResourceKind: syncdomain.KindVariation,
				LookupKey:    "support/default",
				Outcome:      syncapi.ResourceApplyOutcomeFailed,
				Error: &syncapi.ResourceError{
					Code:    "verification_failed",
					Message: "post-write verification failed",
				},
			}},
		}},
	)

	require.NoError(t, err)
	var decoded syncOutput
	require.NoError(t, json.Unmarshal(output.Bytes(), &decoded))
	require.Len(t, decoded.Plans, 1)
	require.Len(t, decoded.Applies, 1)
	require.Len(t, decoded.Applies[0].Resources, 1)
	assert.Equal(
		t,
		syncapi.ResourceApplyOutcomeFailed,
		decoded.Applies[0].Resources[0].Outcome,
	)
}

func TestWriteSyncOutputIncludesPulledResources(t *testing.T) {
	pulls := []serverPull{{
		ProjectKey: "project",
		LookupKey:  "support/default",
		Path:       "project/configs/support/default.prompt.md",
	}}

	t.Run("plaintext", func(t *testing.T) {
		var output bytes.Buffer
		err := writeSyncOutput(&output, "plaintext", pulls, nil, nil)

		require.NoError(t, err)
		assert.Contains(t, output.String(), "Pulled server changes:")
		assert.Contains(
			t,
			output.String(),
			"project/support/default -> .launchdarkly/project/configs/support/default.prompt.md",
		)
	})

	t.Run("json", func(t *testing.T) {
		var output bytes.Buffer
		err := writeSyncOutput(&output, "json", pulls, nil, nil)

		require.NoError(t, err)
		var decoded syncOutput
		require.NoError(t, json.Unmarshal(output.Bytes(), &decoded))
		require.Len(t, decoded.Pulls, 1)
		assert.Equal(t, "project", decoded.Pulls[0].ProjectKey)
		assert.Equal(t, "support/default", decoded.Pulls[0].LookupKey)
		assert.Equal(
			t,
			"project/configs/support/default.prompt.md",
			decoded.Pulls[0].Path,
		)
	})
}
