package sync

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

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
				assert.Contains(t, prompt.String(), "Apply these plans?")
			}
		})
	}
}

func TestRenderVariationDiffSortsFieldsAndUsesStackedFallback(t *testing.T) {
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
	)

	require.NoError(t, err)
	assert.Less(t, strings.Index(rendered, "instructions"), strings.Index(rendered, "model"))
	assert.Contains(t, rendered, "instructions (added)")
	assert.Contains(t, rendered, "Before:\n      (not present)")
	assert.Contains(t, rendered, `"temperature": 0.2`)
	assert.Contains(t, rendered, `"temperature": 0.3`)
}

func TestRenderVariationDiffUsesSideBySidePanelsForWideTerminal(t *testing.T) {
	rendered, err := renderVariationDiff(
		json.RawMessage(`{"name":{"before":"Old","after":"New"}}`),
		"plaintext",
		120,
	)

	require.NoError(t, err)
	lines := strings.Split(rendered, "\n")
	require.Greater(t, len(lines), 3)
	assert.Contains(t, rendered, "name (changed)")
	assert.Contains(t, rendered, "Before")
	assert.Contains(t, rendered, "After")
	assert.Contains(t, rendered, "Old")
	assert.Contains(t, rendered, "New")
	assert.Contains(t, rendered, "╭")
}

func TestRenderVariationDiffUsesMarkdownFallback(t *testing.T) {
	rendered, err := renderVariationDiff(
		json.RawMessage(`{"name":{"before":"Old","after":"New"}}`),
		"markdown",
		120,
	)

	require.NoError(t, err)
	assert.Contains(t, rendered, "#### name (changed)")
	assert.Contains(t, rendered, "**Before**")
	assert.Contains(t, rendered, "```json")
	assert.NotContains(t, rendered, "╭")
}

func TestWriteSyncOutputJSONIncludesPartialApplyOutcomes(t *testing.T) {
	var output bytes.Buffer
	err := writeSyncOutput(
		&output,
		"json",
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
				ApplyStatus:  syncapi.ResourceApplyStatusReconciliationRequired,
				ApplyError: &syncapi.ResourceError{
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
		syncapi.ResourceApplyStatusReconciliationRequired,
		decoded.Applies[0].Resources[0].ApplyStatus,
	)
}
