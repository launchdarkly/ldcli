package prompt

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

func TestConfirmApply(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		terminal  bool
		confirmed bool
		wantError string
	}{
		{name: "yes", input: "yes\n", terminal: true, confirmed: true},
		{name: "declined", input: "n\n", terminal: true},
		{name: "non-terminal", wantError: "rerun with --yes"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var prompt bytes.Buffer
			confirmed, err := confirmApply(strings.NewReader(test.input), &prompt, test.terminal)

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

func TestWritePlanAndOutcomeJSON(t *testing.T) {
	id := testResourceID()
	plan := Plan{Resources: []PlannedResource{{ID: id, Action: ActionUpdateServer}}}

	var output bytes.Buffer
	require.NoError(t, writePlanOutput(&output, "json", plan))
	assert.JSONEq(t, `{"resources":[{"resourceKind":"variation","projectKey":"production","lookupKey":"support/default","action":"update_server"}]}`, output.String())

	output.Reset()
	require.NoError(t, writeOutcomeOutput(&output, "json", []ResourceOutcome{{
		ID: id, Action: ActionUpdateServer, Status: OutcomeSucceeded,
	}}))
	assert.JSONEq(t, `{"resources":[{"resourceKind":"variation","projectKey":"production","lookupKey":"support/default","action":"update_server","status":"succeeded"}]}`, output.String())
}

func TestWritePlanDescribesMissingLaunchDarklyVariation(t *testing.T) {
	variation := syncdomain.Variation{Mode: syncdomain.VariationModeCompletion, Key: "default", Name: "Default"}
	plan := Plan{Resources: []PlannedResource{{
		ID:     testResourceID(),
		Action: ActionCreateServer,
		Diff:   variationDiff(nil, &variation),
	}}}

	var output bytes.Buffer
	require.NoError(t, writePlanOutput(&output, "plaintext", plan))

	assert.Contains(t, output.String(), "(does not exist in LaunchDarkly)")
	assert.NotContains(t, output.String(), `"variation": {`)
	assert.NotContains(t, output.String(), "-null")
}

func TestWritePlanDescribesArchivedLaunchDarklyVariation(t *testing.T) {
	variation := syncdomain.Variation{Mode: syncdomain.VariationModeCompletion, Key: "default", Name: "Default"}
	plan := Plan{Resources: []PlannedResource{{
		ID:     testResourceID(),
		Action: ActionArchiveServer,
		Diff:   variationDiff(&variation, nil),
	}}}

	var output bytes.Buffer
	require.NoError(t, writePlanOutput(&output, "plaintext", plan))

	assert.Contains(t, output.String(), "Action: Archive the variation in LaunchDarkly")
	assert.Contains(t, output.String(), "(archived in LaunchDarkly)")
}
