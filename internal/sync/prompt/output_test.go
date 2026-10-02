package prompt

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

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

func TestWritePlanRendersHumanFriendlyAttachmentDiff(t *testing.T) {
	server := testVariation("Support")
	local := server
	description := "This is a test"
	local.Tools = []syncdomain.AttachmentRef{{Key: "my-first-tool"}}
	local.Attachments = []syncdomain.Attachment{{
		Kind: syncdomain.AttachmentTool,
		Tool: &syncdomain.Tool{
			Key: "my-first-tool", Description: &description,
			Schema: map[string]any{"type": "object", "additionalProperties": false},
		},
	}}
	plan := Plan{Resources: []PlannedResource{{
		ID: testResourceID(), Action: ActionUpdateServer, Diff: variationDiff(&server, &local),
	}}}

	var output bytes.Buffer
	require.NoError(t, writePlanReview(&output, "plaintext", plan, 0))

	rendered := output.String()
	assert.Contains(t, rendered, "  Config: support")
	assert.Contains(t, rendered, "    Variation: default")
	assert.Contains(t, rendered, "      Action: Update LaunchDarkly from the local file")
	assert.Contains(t, rendered, `      Tool "my-first-tool" (added)`)
	assert.Contains(t, rendered, "(not attached)")
	assert.Contains(t, rendered, "my-first-tool")
	assert.Contains(t, rendered, "Description: This is a test")
	assert.Contains(t, rendered, "additionalProperties: false")
	assert.NotContains(t, rendered, "@@")
	assert.NotContains(t, rendered, "Variation content")
	assert.NotContains(t, rendered, `"key": "my-first-tool"`)
}

func TestWritePlanRendersEachSkillInItsOwnDiff(t *testing.T) {
	server := testVariation("Support")
	local := server
	local.Skills = []syncdomain.AttachmentRef{{Key: "first"}, {Key: "second"}}
	local.Attachments = []syncdomain.Attachment{
		{
			Kind:  syncdomain.AttachmentSkill,
			Skill: &syncdomain.Skill{Key: "first", Description: "First description", Markdown: "First skill"},
		},
		{
			Kind:  syncdomain.AttachmentSkill,
			Skill: &syncdomain.Skill{Key: "second", Markdown: "Second skill"},
		},
	}
	plan := Plan{Resources: []PlannedResource{{
		ID: testResourceID(), Action: ActionUpdateServer, Diff: variationDiff(&server, &local),
	}}}

	var output bytes.Buffer
	require.NoError(t, writePlanReview(&output, "plaintext", plan, 140))

	rendered := output.String()
	assert.Equal(t, 1, strings.Count(rendered, `Skill "first"`))
	assert.Equal(t, 1, strings.Count(rendered, `Skill "second"`))
	assert.Equal(t, 2, strings.Count(rendered, "(not attached)"))
	assert.Contains(t, rendered, "Description: First description")
	assert.NotContains(t, rendered, "Skills attached to this variation")
}

func TestWritePlanGroupsVariationsUnderTheirConfig(t *testing.T) {
	plan := Plan{Resources: []PlannedResource{
		{
			ID: ResourceID{
				Kind: syncdomain.KindVariation, ProjectKey: "production", LookupKey: "support/first",
			},
			Action: ActionInSync,
		},
		{
			ID: ResourceID{
				Kind: syncdomain.KindVariation, ProjectKey: "production", LookupKey: "support/second",
			},
			Action: ActionInSync,
		},
	}}

	var output bytes.Buffer
	require.NoError(t, writePlanReview(&output, "plaintext", plan, 0))

	rendered := output.String()
	assert.Equal(t, 1, strings.Count(rendered, "  Config: support"))
	assert.Contains(t, rendered, "    Variation: first")
	assert.Contains(t, rendered, "    Variation: second")
	assert.Contains(t, rendered, "      Action: No change (in sync)")
}

func TestUnifiedDiffKeepsOneUnchangedLineAroundChanges(t *testing.T) {
	lines, err := unifiedDiffLines(
		"first\nbefore\nold\nafter\nlast",
		"first\nbefore\nnew\nafter\nlast",
		"before",
		"after",
	)

	require.NoError(t, err)
	rendered := strings.Join(lines, "\n")
	assert.Contains(t, rendered, " before")
	assert.Contains(t, rendered, " after")
	assert.NotContains(t, rendered, " first")
	assert.NotContains(t, rendered, " last")
}
