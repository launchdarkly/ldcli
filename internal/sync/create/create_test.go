package create

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

var (
	refundID = syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "demo", LookupKey: "refund"}
	triageID = syncdomain.ResourceID{Kind: syncdomain.KindSkill, ProjectKey: "demo", LookupKey: "incident-triage"}
)

func TestCheckNeedsTheFlagThatStartsTheCreate(t *testing.T) {
	tests := map[string]struct {
		kind  syncdomain.Kind
		input Input
		want  string
	}{
		"tool description": {
			kind:  syncdomain.KindTool,
			input: Input{Description: pointer("Issue a refund")},
			want:  "--description needs --json-schema. add creates a tool only when --json-schema is set.",
		},
		"skill name": {
			kind:  syncdomain.KindSkill,
			input: Input{Name: pointer("Triage")},
			want:  "--name needs --content. add creates a skill only when --content is set.",
		},
		"skill name and description": {
			kind:  syncdomain.KindSkill,
			input: Input{Name: pointer("Triage"), Description: pointer("")},
			want:  "--name and --description need --content. add creates a skill only when --content is set.",
		},
		"variation name and model config": {
			kind:  syncdomain.KindVariation,
			input: Input{Name: pointer("Escalation"), ModelConfigKey: pointer("claude")},
			want:  "--name and --model-config-key need --content. add creates a variation only when --content is set.",
		},
		"no flag": {
			kind: syncdomain.KindTool,
			want: "add creates a tool only when --json-schema is set",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require.EqualError(t, test.input.Check(test.kind), test.want)
		})
	}
}

func TestCheckDecodesTheJSONSchemaStrictly(t *testing.T) {
	const definition = `--json-schema is the input schema only. Pass the value of "inputSchema", ` +
		"and pass the description with --description."
	tests := map[string]struct {
		schema string
		want   string
	}{
		"object":                   {schema: `{"type": "object", "properties": {"schema": {"type": "string"}}}`},
		"empty object":             {schema: `{}`},
		"empty":                    {schema: ``, want: "--json-schema must be one JSON object: EOF"},
		"invalid":                  {schema: `{"type":`, want: "--json-schema must be one JSON object: unexpected EOF"},
		"array":                    {schema: `[{"type": "object"}]`, want: "--json-schema must be one JSON object"},
		"null":                     {schema: `null`, want: "--json-schema must be one JSON object"},
		"two values":               {schema: `{"type": "object"} {}`, want: "--json-schema must be one JSON object"},
		"MCP definition":           {schema: `{"name": "refund", "inputSchema": {"type": "object"}}`, want: definition},
		"tool file":                {schema: `{"formatVersion": 1, "key": "refund", "schema": {}}`, want: definition},
		"tool file without format": {schema: `{"key": "refund", "schema": {"type": "object"}}`, want: definition},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := Input{JSONSchema: pointer(test.schema)}.Check(syncdomain.KindTool)
			if test.want == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, test.want)
		})
	}
}

func TestCheckRejectsBlankSkillValues(t *testing.T) {
	require.EqualError(t, Input{Content: pointer(" \n")}.Check(syncdomain.KindSkill), "--content cannot be blank")
	require.EqualError(t, Input{Content: pointer("Triage."), Name: pointer(" ")}.Check(syncdomain.KindSkill),
		"--name cannot be blank")
	require.NoError(t, Input{Content: pointer("Triage."), Description: pointer("")}.Check(syncdomain.KindSkill))
}

func TestAttachmentBuildsATool(t *testing.T) {
	input := Input{JSONSchema: pointer(`{"type": "object", "required": ["orderId"]}`), Description: pointer("Issue a refund")}

	tool, err := input.Attachment(refundID)

	require.NoError(t, err)
	assert.Equal(t, syncdomain.Attachment{
		Kind:   syncdomain.AttachmentTool,
		Upsert: true,
		Tool: &syncdomain.Tool{
			Key: "refund", Description: pointer("Issue a refund"),
			Schema: map[string]any{"type": "object", "required": []any{"orderId"}},
		},
	}, tool)

	input.Description = pointer("")
	tool, err = input.Attachment(refundID)
	require.NoError(t, err)
	assert.Nil(t, tool.Tool.Description, "an empty description is no description, as in a tool file")
}

func TestAttachmentBuildsASkill(t *testing.T) {
	tests := map[string]struct {
		input Input
		want  syncdomain.Skill
	}{
		"name from the key": {
			input: Input{Content: pointer("Sort by severity.")},
			want:  syncdomain.Skill{Key: "incident-triage", Name: "Incident triage", Markdown: "Sort by severity.\n"},
		},
		"name and description from flags": {
			input: Input{Content: pointer("Sort by severity.\n"), Name: pointer("Triage"), Description: pointer("Sort incidents")},
			want: syncdomain.Skill{
				Key: "incident-triage", Name: "Triage", Description: "Sort incidents", Markdown: "Sort by severity.\n",
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			skill, err := test.input.Attachment(triageID)

			require.NoError(t, err)
			assert.Equal(t, syncdomain.Attachment{Kind: syncdomain.AttachmentSkill, Upsert: true, Skill: &test.want}, skill)
		})
	}
}

func TestAttachmentAppliesTheCreateRules(t *testing.T) {
	skill := Input{Content: pointer("Sort by severity.")}

	_, err := skill.Attachment(syncdomain.ResourceID{Kind: syncdomain.KindSkill, ProjectKey: "demo", LookupKey: "Triage"})
	require.EqualError(t, err,
		`skill key "Triage" must use only lowercase letters, digits, and hyphens, and start with a letter or digit`)

	skill.Description = pointer(strings.Repeat("d", 1025))
	_, err = skill.Attachment(triageID)
	require.EqualError(t, err, "skill description must be at most 1024 characters")

	skill.Description, skill.Name = nil, pointer(strings.Repeat("n", 257))
	_, err = skill.Attachment(triageID)
	require.EqualError(t, err, "skill name must be at most 256 characters")

	tool := Input{JSONSchema: pointer(`{}`), Description: pointer(strings.Repeat("d", 1025))}
	_, err = tool.Attachment(refundID)
	require.EqualError(t, err, "tool description must be at most 1024 characters")
}

func TestMatchesComparesOnlyThePassedFields(t *testing.T) {
	tagged := syncdomain.Attachment{Kind: syncdomain.AttachmentTool, Version: 3, Tool: &syncdomain.Tool{
		Key: "refund", Description: pointer("Issue a refund"), Schema: map[string]any{"type": "object"},
		Tags: []string{"billing"}, CustomParameters: map[string]any{"timeout": float64(5)},
	}}
	named := syncdomain.Attachment{Kind: syncdomain.AttachmentSkill, Version: 2, Skill: &syncdomain.Skill{
		Key: "incident-triage", Name: "On-call triage", Description: "Sort incidents", Markdown: "Sort by severity.\n",
	}}
	tests := map[string]struct {
		input    Input
		id       syncdomain.ResourceID
		existing syncdomain.Attachment
		want     bool
	}{
		"tool without --description": {
			input: Input{JSONSchema: pointer(`{"type": "object"}`)}, id: refundID, existing: tagged, want: true,
		},
		"tool with the same description": {
			input: Input{JSONSchema: pointer(`{"type":"object"}`), Description: pointer("Issue a refund")},
			id:    refundID, existing: tagged, want: true,
		},
		"tool with another description": {
			input: Input{JSONSchema: pointer(`{"type": "object"}`), Description: pointer("Refund")}, id: refundID, existing: tagged,
		},
		"tool with an empty description": {
			input: Input{JSONSchema: pointer(`{"type": "object"}`), Description: pointer("")}, id: refundID, existing: tagged,
		},
		"tool with another schema": {
			input: Input{JSONSchema: pointer(`{"type": "object", "required": ["orderId"]}`)}, id: refundID, existing: tagged,
		},
		"skill with another name": {
			input: Input{Content: pointer("Sort by severity."), Name: pointer("Triage"), Description: pointer("Sort incidents")},
			id:    triageID, existing: named, want: true,
		},
		"skill with another description": {
			input: Input{Content: pointer("Sort by severity."), Description: pointer("")}, id: triageID, existing: named,
		},
		"skill with other Markdown": {
			input: Input{Content: pointer("Sort by age.")}, id: triageID, existing: named,
		},
		"other kind": {
			input: Input{Content: pointer("Sort by severity.")}, id: triageID, existing: tagged,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			created, err := test.input.Attachment(test.id)
			require.NoError(t, err)

			assert.Equal(t, test.want, test.input.Matches(created, test.existing))
		})
	}
}

func TestTargetWithoutQuestionsNamesEveryMissingFlag(t *testing.T) {
	_, _, err := Target(syncdomain.KindTool, "", "", Questions{})
	require.EqualError(t, err, "tool creation requires --project and --key without interactive input")

	_, _, err = Target(syncdomain.KindSkill, "demo", "", Questions{})
	require.EqualError(t, err, "skill creation requires --key without interactive input")

	id, canceled, err := Target(syncdomain.KindTool, "demo", "refund", Questions{})
	require.NoError(t, err)
	assert.False(t, canceled)
	assert.Equal(t, refundID, id)
}

func TestAskWithoutQuestionsKeepsTheInput(t *testing.T) {
	input := Input{Content: pointer("Sort by severity.")}

	asked, canceled, err := input.Ask(triageID, Questions{})

	require.NoError(t, err)
	assert.False(t, canceled)
	assert.Equal(t, input, asked, "the name comes from the key, and the description is empty")
}

func TestKeyValidatorAppliesTheCreateRules(t *testing.T) {
	require.EqualError(t, keyValidator(syncdomain.KindTool)(""), "tool key must not be empty")
	require.EqualError(t, keyValidator(syncdomain.KindSkill)("a/b"), "skill key must be a single path segment")
	require.EqualError(t, keyValidator(syncdomain.KindSkill)("Triage"),
		`skill key "Triage" must use only lowercase letters, digits, and hyphens, and start with a letter or digit`)
	require.NoError(t, keyValidator(syncdomain.KindTool)("Refund_v2"))
}

func TestRequiredValueRejectsBlankInput(t *testing.T) {
	require.Error(t, RequiredValue("variation name")("  "))
	require.NoError(t, RequiredValue("variation name")("Custom name"))
}

func TestJoinFlags(t *testing.T) {
	assert.Equal(t, "--key", joinFlags([]string{"--key"}))
	assert.Equal(t, "--project and --key", joinFlags([]string{"--project", "--key"}))
	assert.Equal(t, "--project, --config-key, and --key", joinFlags([]string{"--project", "--config-key", "--key"}))
}

func pointer[T any](value T) *T {
	return &value
}
