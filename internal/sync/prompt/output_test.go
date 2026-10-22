package prompt

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

func TestWritePlanAndOutcomeJSON(t *testing.T) {
	id := testResourceID()
	plan := Plan{Resources: []PlannedResource{{ID: id, Action: ActionUpdateServer}}}

	var output bytes.Buffer
	require.NoError(t, writePlanOutput(&output, "json", plan, false))
	assert.JSONEq(t, `{"resources":[{"resourceKind":"variation","projectKey":"production","lookupKey":"support/default","action":"update_server"}]}`, output.String())

	output.Reset()
	require.NoError(t, writeOutcomeOutput(&output, "json", plan, []ResourceOutcome{{
		ID: id, Action: ActionUpdateServer, Status: OutcomeSucceeded,
	}}))
	assert.JSONEq(t, `{"resources":[{"resourceKind":"variation","projectKey":"production","lookupKey":"support/default","action":"update_server","status":"succeeded"}]}`, output.String())
}

// The JSON output is the stable format for scripts. The plain-text layout can
// change, but this output must not change by accident.
func TestPlanAndOutcomeJSONIsStable(t *testing.T) {
	server, local := testVariation("Support"), testVariation("Support")
	local.Instructions = "Help briefly"
	pushed, stale, broken := testResourceID(), testResourceID(), testResourceID()
	stale.LookupKey, broken.LookupKey = "support/stale", "support/broken"
	plan := Plan{Resources: []PlannedResource{
		{ID: pushed, Action: ActionUpdateServer, Diff: variationDiff(&server, &local)},
		{ID: stale, Action: ActionUpdateLocal, SyncedElsewhere: true},
		{ID: broken, Action: ActionError, Error: "local mode \"agent\" does not match config mode \"completion\""},
	}}

	var output bytes.Buffer
	require.NoError(t, writePlanOutput(&output, "json", plan, true))
	assert.Equal(t, `{
  "resources": [
    {
      "resourceKind": "variation",
      "projectKey": "production",
      "lookupKey": "support/default",
      "action": "update_server",
      "diff": {
        "variation": {
          "before": {
            "mode": "agent",
            "key": "default",
            "name": "Support",
            "instructions": "Help"
          },
          "after": {
            "mode": "agent",
            "key": "default",
            "name": "Support",
            "instructions": "Help briefly"
          }
        }
      }
    },
    {
      "resourceKind": "variation",
      "projectKey": "production",
      "lookupKey": "support/stale",
      "action": "update_local",
      "syncedElsewhere": true,
      "suggestion": "If the other working copy pushed its change to Git, run git pull before you sync. Then your sync.lock does not get a merge conflict."
    },
    {
      "resourceKind": "variation",
      "projectKey": "production",
      "lookupKey": "support/broken",
      "action": "error",
      "error": "local mode \"agent\" does not match config mode \"completion\""
    }
  ]
}
`, output.String())

	output.Reset()
	require.NoError(t, writeOutcomeOutput(&output, "json", plan, []ResourceOutcome{
		{ID: pushed, Action: ActionUpdateServer, Status: OutcomeSucceeded},
		{ID: stale, Action: ActionUpdateLocal, Status: OutcomeFailed, Error: "write failed"},
	}))
	assert.Equal(t, `{
  "resources": [
    {
      "resourceKind": "variation",
      "projectKey": "production",
      "lookupKey": "support/default",
      "action": "update_server",
      "status": "succeeded"
    },
    {
      "resourceKind": "variation",
      "projectKey": "production",
      "lookupKey": "support/stale",
      "action": "update_local",
      "status": "failed",
      "error": "write failed"
    }
  ]
}
`, output.String())
}

func TestWritePlanReviewShowsSummaryAndOneRowPerChange(t *testing.T) {
	var output bytes.Buffer
	require.NoError(t, writePlanReview(&output, "plaintext", reviewTestPlan(), 0, false))

	assert.Equal(t, `3 changes: 1 tool record, 1 prompt push, 1 prompt pull. 1 conflict. 1 in sync.

Tools
  record     production/lookup            same as LaunchDarkly
Prompts
  push       production/support/default   instructions changed                   +1 -1
  pull       production/support/second    instructions changed in LaunchDarkly   +1 -1
  conflict   production/support/third     changed on both sides                  +1 -1

      Variation content (changed)

        --- LaunchDarkly now
        +++ Local file now
        @@ -4,3 +4,3 @@
           "name": "Support",
        -  "instructions": "Help"
        +  "instructions": "Help briefly"
         }
`, output.String())
}

func TestWritePlanReviewDiffMatchesRowSize(t *testing.T) {
	var output bytes.Buffer
	require.NoError(t, writePlanReview(&output, "plaintext", reviewTestPlan(), 0, true))

	rendered := output.String()
	assert.Equal(t, 3, strings.Count(rendered, "Variation content (changed)"))
	var added, removed int
	for _, line := range strings.Split(rendered, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case isAddedDiffLine(line):
			added++
		case isRemovedDiffLine(line):
			removed++
		}
	}
	assert.Equal(t, 3, added)
	assert.Equal(t, 3, removed)
	assert.Equal(t, 3, strings.Count(rendered, "+1 -1"))
}

func TestWritePlanReviewMarkdownWritesOneTablePerKind(t *testing.T) {
	var output bytes.Buffer
	require.NoError(t, writePlanReview(&output, "markdown", reviewTestPlan(), 0, false))

	rendered := output.String()
	assert.Contains(t, rendered, "### Tools\n\n| Action | Resource | Reason | Size |\n| --- | --- | --- | --- |\n"+
		"| record | production/lookup | same as LaunchDarkly |  |\n")
	assert.Contains(t, rendered, "### Prompts\n\n| Action | Resource | Reason | Size |\n| --- | --- | --- | --- |\n"+
		"| push | production/support/default | instructions changed | +1 -1 |\n")
	assert.Contains(t, rendered, "\n#### `production/support/third`\n")
	assert.NotContains(t, rendered, "#### `production/support/default`")
}

func TestDiffSizeTextColorsEachPart(t *testing.T) {
	useColorTerminal(t, "")

	assert.Equal(t, "+3 -1", diffSizeText(3, 1, false))
	assert.Equal(t, "\x1b[92m+3\x1b[0m \x1b[91m-1\x1b[0m", diffSizeText(3, 1, true))
	assert.Equal(t, "\x1b[92m+2\x1b[0m", diffSizeText(2, 0, true))
	assert.Equal(t, "\x1b[91m-4\x1b[0m", diffSizeText(0, 4, true))
	assert.Empty(t, diffSizeText(0, 0, true))
}

func TestWritePlanReviewColorsSizesOnATerminal(t *testing.T) {
	var plain bytes.Buffer
	require.NoError(t, writePlanReview(&plain, "plaintext", reviewTestPlan(), 0, false))
	useColorTerminal(t, "")

	var output bytes.Buffer
	require.NoError(t, writePlanReview(&output, "plaintext", reviewTestPlan(), 80, false))

	rendered := output.String()
	assert.Contains(t, rendered, "  push       production/support/default   instructions changed                   "+
		"\x1b[92m+1\x1b[0m \x1b[91m-1\x1b[0m\n")
	assert.Equal(t, 3, strings.Count(rendered, "\x1b[92m+1\x1b[0m \x1b[91m-1\x1b[0m"))
	assert.Equal(t, plain.String(), ansi.Strip(rendered), "the colors must not change the column widths")
}

func TestWritePlanSizesHaveNoColorOffATerminal(t *testing.T) {
	tests := map[string]struct {
		noColor string
		write   func(io.Writer) error
	}{
		"piped plain text": {write: func(out io.Writer) error {
			return writePlanOutput(out, "plaintext", reviewTestPlan(), false)
		}},
		"Markdown on a terminal": {write: func(out io.Writer) error {
			return writePlanReview(out, "markdown", reviewTestPlan(), 80, false)
		}},
		"JSON": {write: func(out io.Writer) error {
			return writePlanOutput(out, "json", reviewTestPlan(), true)
		}},
		"NO_COLOR on a terminal": {noColor: "1", write: func(out io.Writer) error {
			return writePlanReview(out, "plaintext", reviewTestPlan(), 80, false)
		}},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			useColorTerminal(t, test.noColor)

			var output bytes.Buffer
			require.NoError(t, test.write(&output))
			assert.NotContains(t, output.String(), "\x1b[")
		})
	}
}

func TestWritePlanReviewSummarizesPlansWithoutChanges(t *testing.T) {
	var output bytes.Buffer
	require.NoError(t, writePlanReview(&output, "plaintext", Plan{}, 0, false))
	assert.Equal(t, "No prompt variations are synced in this repository. To start, run ldcli sync prompts add.\n", output.String())

	output.Reset()
	inSync := Plan{Resources: []PlannedResource{
		{ID: testResourceID(), Action: ActionInSync},
		{ID: testResourceID(), Action: ActionInSync},
	}}
	require.NoError(t, writePlanReview(&output, "plaintext", inSync, 0, false))
	assert.Equal(t, "No changes. 2 in sync.\n", output.String())
}

func TestPlanReasonNamesTheChange(t *testing.T) {
	variation := testVariation("Support")
	tests := map[string]struct {
		resource PlannedResource
		want     string
	}{
		"create":  {PlannedResource{Action: ActionCreateServer}, "new"},
		"restore": {PlannedResource{Action: ActionUpdateLocal}, "local file missing"},
		"delete":  {PlannedResource{Action: ActionDeleteLocal}, "deleted in LaunchDarkly"},
		"forget":  {PlannedResource{Action: ActionRemoveManifest}, "gone on both sides"},
		"error": {
			PlannedResource{Action: ActionError, Error: "variation does not exist in LaunchDarkly; set upsert: true to create it"},
			"variation does not exist in LaunchDarkly",
		},
		"tools": {
			PlannedResource{ID: testResourceID(), Action: ActionUpdateLocal, Local: &variation, Diff: variationDiffFields{"tools": {}}},
			"tools changed in LaunchDarkly",
		},
		"tool fields": {
			PlannedResource{
				ID: ResourceID{Kind: syncdomain.KindTool, ProjectKey: "production", LookupKey: "search"}, Action: ActionUpdateServer,
				Diff: attachmentDiff(
					&syncdomain.Attachment{Kind: syncdomain.AttachmentTool, Tool: &syncdomain.Tool{Key: "search", Schema: map[string]any{}}},
					&syncdomain.Attachment{Kind: syncdomain.AttachmentTool, Tool: &syncdomain.Tool{
						Key: "search", Description: pointerTo("Search"), Schema: map[string]any{"type": "object"},
					}},
				),
			},
			"description, schema changed",
		},
		"repin": {
			PlannedResource{
				Action: ActionUpdateServer, ServerHasStaleAttachmentPins: true,
				Diff: variationDiffFields{"attachment versions": {
					After: json.RawMessage(`{"skills":{"summarize":2},"tools":{"search":4}}`),
				}},
			},
			"search to v4, summarize to v2",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, test.want, planReason(test.resource))
		})
	}
}

func TestPlanSummaryCountsRepinsSeparately(t *testing.T) {
	plan := Plan{Resources: []PlannedResource{
		{ID: testResourceID(), Action: ActionUpdateServer},
		{ID: testResourceID(), Action: ActionUpdateServer, ServerHasStaleAttachmentPins: true},
		{ID: testResourceID(), Action: ActionError},
	}}

	assert.Equal(t, "2 changes: 1 prompt push, 1 prompt repin. 1 error.", planSummary(plan))
}

func TestWriteOutcomeOutputMirrorsThePlan(t *testing.T) {
	plan := reviewTestPlan()
	outcomes := []ResourceOutcome{
		{ID: plan.Resources[0].ID, Action: ActionUpdateManifest, Status: OutcomeSucceeded},
		{ID: plan.Resources[1].ID, Action: ActionUpdateServer, Status: OutcomeSucceeded},
		{ID: plan.Resources[2].ID, Action: ActionUpdateLocal, Status: OutcomeFailed, Error: "write failed"},
		{ID: plan.Resources[4].ID, Action: ActionInSync, Status: OutcomeSucceeded},
	}

	var output bytes.Buffer
	require.NoError(t, writeOutcomeOutput(&output, "plaintext", plan, outcomes))

	assert.Equal(t, `Tools
  recorded   production/lookup            same as LaunchDarkly
Prompts
  pushed     production/support/default   instructions changed

Failed: variation production/support/second: write failed
2 applied, 1 failed. 1 in sync.
`, output.String())
}

func TestWritePlanDescribesMissingLaunchDarklyVariation(t *testing.T) {
	variation := syncdomain.Variation{Mode: syncdomain.VariationModeCompletion, Key: "default", Name: "Default"}
	plan := Plan{Resources: []PlannedResource{{
		ID:     testResourceID(),
		Action: ActionCreateServer,
		Diff:   variationDiff(nil, &variation),
	}}}

	var output bytes.Buffer
	require.NoError(t, writePlanOutput(&output, "plaintext", plan, true))

	assert.Contains(t, output.String(), "  create   production/support/default   new   +5\n")
	assert.Contains(t, output.String(), "(does not exist in LaunchDarkly)")
	assert.NotContains(t, output.String(), `"variation": {`)
	assert.NotContains(t, output.String(), "-null")
}

func TestWritePlanNotesResourceSyncedByAnotherWorkingCopy(t *testing.T) {
	plan := Plan{Resources: []PlannedResource{{ID: testResourceID(), Action: ActionUpdateLocal, SyncedElsewhere: true}}}

	var notes, data bytes.Buffer
	writePlanNotes(&notes, plan)
	require.NoError(t, writePlanOutput(&data, "json", plan, false))

	assert.Equal(t, "Note: Another working copy synced variation production/support/default after your sync.lock. "+
		"If the other working copy pushed its change to Git, run git pull before you sync. "+
		"Then your sync.lock does not get a merge conflict.\n", notes.String())
	assert.Contains(t, data.String(), `"syncedElsewhere": true`)
	assert.Contains(t, data.String(), `"suggestion": "If the other working copy pushed its change to Git`)
}

func TestStaleSuggestionMatchesTheAction(t *testing.T) {
	assert.Empty(t, staleSuggestion(PlannedResource{Action: ActionUpdateLocal}))
	assert.Contains(t, staleSuggestion(PlannedResource{Action: ActionUpdateLocal, SyncedElsewhere: true}), "run git pull before you sync")
	assert.Contains(t, staleSuggestion(PlannedResource{Action: ActionConflict, SyncedElsewhere: true}), "--conflict or --resolve")
	assert.Contains(t, staleSuggestion(PlannedResource{Action: ActionInSync, SyncedElsewhere: true}), "run sync again")
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
	require.NoError(t, writePlanReview(&output, "plaintext", plan, 0, true))

	rendered := output.String()
	assert.Contains(t, rendered, "  push   production/support/default   tools changed   +4\n")
	assert.Contains(t, rendered, `      Tool "my-first-tool" (added)`)
	assert.Contains(t, rendered, "(not attached)")
	assert.Contains(t, rendered, "Description: This is a test")
	assert.Contains(t, rendered, "additionalProperties: false")
	assert.NotContains(t, rendered, "@@")
	assert.NotContains(t, rendered, "Variation content")
	assert.NotContains(t, rendered, `"key": "my-first-tool"`)
}

func TestFormatToolDetailsRendersNestedSchemaValues(t *testing.T) {
	rendered := formatToolDetails(syncdomain.Tool{
		Key: "search",
		Schema: map[string]any{
			"anyOf": []any{
				map[string]any{"type": "string"},
				nil,
			},
		},
	})

	assert.Contains(t, rendered, "anyOf:\n    -\n      type: string\n    - null")
	assert.NotContains(t, rendered, "map[")
	assert.NotContains(t, rendered, "<nil>")
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
	require.NoError(t, writePlanReview(&output, "plaintext", plan, 140, true))

	rendered := output.String()
	assert.Equal(t, 1, strings.Count(rendered, `Skill "first"`))
	assert.Equal(t, 1, strings.Count(rendered, `Skill "second"`))
	assert.Equal(t, 2, strings.Count(rendered, "(not attached)"))
	assert.Contains(t, rendered, "Description: First description")
	assert.NotContains(t, rendered, "Skills attached to this variation")
}

func TestWritePlanMarkdownProtectsSkillCodeFences(t *testing.T) {
	server := testVariation("Support")
	server.Skills = []syncdomain.AttachmentRef{{Key: "example"}}
	server.Attachments = []syncdomain.Attachment{{
		Kind: syncdomain.AttachmentSkill,
		Skill: &syncdomain.Skill{
			Key:      "example",
			Markdown: "Example:\n```go\nsame\n```\nOld ending",
		},
	}}
	local := server
	localSkill := *server.Attachments[0].Skill
	localSkill.Markdown = "Example:\n```go\nsame\n```\nNew ending"
	local.Attachments = []syncdomain.Attachment{{
		Kind:  syncdomain.AttachmentSkill,
		Skill: &localSkill,
	}}
	plan := Plan{Resources: []PlannedResource{{
		ID: testResourceID(), Action: ActionUpdateServer, Diff: variationDiff(&server, &local),
	}}}

	var output bytes.Buffer
	require.NoError(t, writePlanReview(&output, "markdown", plan, 0, true))

	rendered := output.String()
	assert.Contains(t, rendered, "````diff\n")
	assert.Equal(t, 2, strings.Count(rendered, "````"))
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

// useColorTerminal makes lipgloss render as it does for a color terminal with
// the given NO_COLOR value, until the test ends.
func useColorTerminal(t *testing.T, noColor string) {
	t.Helper()
	t.Setenv("NO_COLOR", noColor)
	t.Setenv("CLICOLOR_FORCE", "1")
	previous := lipgloss.DefaultRenderer()
	lipgloss.SetDefaultRenderer(lipgloss.NewRenderer(os.Stdout))
	t.Cleanup(func() { lipgloss.SetDefaultRenderer(previous) })
}

// reviewTestPlan has one row of each common kind of change, in plan order.
func reviewTestPlan() Plan {
	server, local := testVariation("Support"), testVariation("Support")
	local.Instructions = "Help briefly"
	diff := variationDiff(&server, &local)
	variationID := func(lookupKey string) ResourceID {
		return ResourceID{Kind: syncdomain.KindVariation, ProjectKey: "production", LookupKey: lookupKey}
	}
	return Plan{Resources: []PlannedResource{
		{ID: ResourceID{Kind: syncdomain.KindTool, ProjectKey: "production", LookupKey: "lookup"}, Action: ActionUpdateManifest},
		{ID: variationID("support/default"), Action: ActionUpdateServer, Local: &local, Server: &server, Diff: diff},
		{ID: variationID("support/second"), Action: ActionUpdateLocal, Local: &local, Server: &server, Diff: diff},
		{ID: variationID("support/third"), Action: ActionConflict, Local: &local, Server: &server, Diff: diff},
		{ID: variationID("support/fourth"), Action: ActionInSync},
	}}
}

func pointerTo[T any](value T) *T {
	return &value
}
