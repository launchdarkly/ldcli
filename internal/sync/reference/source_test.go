package reference

import (
	"bytes"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

func TestParsePathGrammar(t *testing.T) {
	for _, path := range []string{
		"$",
		"field.child",
		`items[id="value"]`,
		`items[role="system",name="intro"]`,
		"items[0]",
		"items[0][1]",
		"items[2147483647]",
		`items[id="value"].messages[0].content`,
		`object["literal.key"]`,
		`["top level"]`,
	} {
		require.NoError(t, ParsePath(path), path)
	}
	for _, path := range []string{
		"",
		"$.field",
		"items[]",
		"items[-1]",
		"items[1.0]",
		"items[01]",
		"items[2147483648]",
		`items[0][id="value"]`,
		"items[*]",
		`items[id=value]`,
		`items[ id = "value" ]`,
		"items..id",
	} {
		err := ParsePath(path)
		problem, ok := syncdomain.ProblemFrom(err)
		require.True(t, ok, path)
		assert.Equal(t, syncdomain.ErrorInvalidPathSyntax, problem.Code, path)
	}
}

func TestResolvePathSupportsChainedAndMixedIndexes(t *testing.T) {
	root := &sourceNode{fields: map[string]*sourceNode{
		"agents": {items: []*sourceNode{
			{fields: map[string]*sourceNode{
				"id": {value: "support"},
				"messages": {items: []*sourceNode{
					{fields: map[string]*sourceNode{"content": {value: "First"}}},
					{fields: map[string]*sourceNode{"content": {value: "Second"}}},
				}, collection: true},
			}},
		}, collection: true},
	}}

	value, err := resolveSourcePath(root, `agents[id="support"].messages[1].content`)

	require.NoError(t, err)
	assert.Equal(t, "Second", value.value)
}

func TestResolvePathIndexErrorsUseStableCodes(t *testing.T) {
	root := &sourceNode{fields: map[string]*sourceNode{
		"items": {items: []*sourceNode{{value: "only"}}, collection: true},
		"value": {value: "text"},
	}}
	tests := []struct {
		path string
		code syncdomain.ErrorCode
	}{
		{path: "items[1]", code: syncdomain.ErrorIndexOutOfRange},
		{path: "value[0]", code: syncdomain.ErrorPathTypeMismatch},
	}
	for _, test := range tests {
		_, err := resolveSourcePath(root, test.path)
		problem, ok := syncdomain.ProblemFrom(err)
		require.True(t, ok)
		assert.Equal(t, test.code, problem.Code)
	}
}

func FuzzPathIndexNeverPanics(f *testing.F) {
	for _, path := range []string{
		"items[0]", "items[-1]", "items[1.5]", "items[]", "items[2147483648]",
		"items[0][1]", `items[id="support"].messages[0]`,
	} {
		f.Add(path)
	}
	f.Fuzz(func(t *testing.T, path string) {
		err := ParsePath(path)
		if err == nil {
			return
		}
		problem, ok := syncdomain.ProblemFrom(err)
		require.True(t, ok)
		require.Equal(t, syncdomain.ErrorInvalidPathSyntax, problem.Code)
	})
}

func TestResolvePathSupportsLiteralKeysAndRejectsAmbiguousSelectors(t *testing.T) {
	root := &sourceNode{fields: map[string]*sourceNode{
		"metadata": {fields: map[string]*sourceNode{"display.name": {value: "Support"}}},
		"agents": {
			items: []*sourceNode{
				{fields: map[string]*sourceNode{"id": {value: "support"}}},
				{fields: map[string]*sourceNode{"id": {value: "support"}}},
			},
		},
	}}
	value, err := resolveSourcePath(root, `metadata["display.name"]`)
	require.NoError(t, err)
	assert.Equal(t, "Support", value.value)

	_, err = resolveSourcePath(root, `agents[id="support"]`)
	problem, ok := syncdomain.ProblemFrom(err)
	require.True(t, ok)
	assert.Equal(t, syncdomain.ErrorPathAmbiguous, problem.Code)
}

func TestResolvePathMatchesEveryFilterCondition(t *testing.T) {
	root := &sourceNode{fields: map[string]*sourceNode{
		"items": {items: []*sourceNode{
			{fields: map[string]*sourceNode{
				"role": {value: "system"}, "name": {value: "intro"}, "content": {value: "First"},
			}},
			{fields: map[string]*sourceNode{
				"role": {value: "system"}, "name": {value: "outro"}, "content": {value: "Second"},
			}},
		}},
	}}

	value, err := resolveSourcePath(root, `items[role="system",name="intro"].content`)

	require.NoError(t, err)
	assert.Equal(t, "First", value.value)
}

func TestMarkdownSectionUpdatePreservesOtherBytes(t *testing.T) {
	source := []byte("# Guide\r\n\r\n## Support\r\n\r\nHelp with orders.\r\n\r\n## Build\r\n\r\nRun make.\r\n")
	id := syncdomain.VariationID("project", "support", "default")
	reference := syncdomain.Reference{
		File: "AGENTS.md", Format: "markdown", Select: `sections[heading="Support"]`,
		Map: map[string]string{"prompt": "body"},
	}
	projection, err := ParseSource(sourceBundle("AGENTS.md", source), id, syncdomain.VariationModeAgent, reference)
	require.NoError(t, err)
	assert.Equal(t, "Help with orders.", *projection.(syncdomain.VariationProjection).Instructions)

	next := "Help with orders and refunds."
	update, err := UpdateSource(
		sourceBundle("AGENTS.md", source),
		id,
		syncdomain.VariationModeAgent,
		reference,
		syncdomain.VariationProjection{Mode: syncdomain.VariationModeAgent, Instructions: &next},
	)
	require.NoError(t, err)
	edit := update.Fields[syncdomain.FieldPrompt].Edits[0]
	updated := applyTestEdits(source, edit)
	assert.Equal(t, "# Guide\r\n\r\n## Support\r\n\r\nHelp with orders and refunds.\r\n\r\n## Build\r\n\r\nRun make.\r\n", string(updated))
}

func TestMarkdownSectionsIgnoreCodeAndKeepHeadingHashes(t *testing.T) {
	source := []byte("    ## Not a heading\n\n````\n## Fenced\n````\n\n## C#\n\nLanguage guidance.\n")
	id := syncdomain.VariationID("project", "guide", "default")
	reference := syncdomain.Reference{
		File: "AGENTS.md", Format: "markdown", Select: `sections[heading="C#"]`,
		Map: map[string]string{"prompt": "body"},
	}

	projection, err := ParseSource(sourceBundle(reference.File, source), id, syncdomain.VariationModeAgent, reference)

	require.NoError(t, err)
	assert.Equal(t, "Language guidance.", *projection.(syncdomain.VariationProjection).Instructions)
}

func TestMarkdownRepresentsOneCompletionSystemMessage(t *testing.T) {
	source := []byte("Current system prompt.\n")
	id := syncdomain.VariationID("project", "completion", "default")
	reference := syncdomain.Reference{File: "prompt.md", Format: "markdown"}

	projection, err := ParseSource(sourceBundle(reference.File, source), id, syncdomain.VariationModeCompletion, reference)
	require.NoError(t, err)
	messages := *projection.(syncdomain.VariationProjection).Messages
	require.Equal(t, []syncdomain.Message{{Role: syncdomain.RoleSystem, Content: "Current system prompt."}}, messages)

	desired := []syncdomain.Message{{Role: syncdomain.RoleSystem, Content: "Updated system prompt."}}
	update, err := UpdateSource(
		sourceBundle(reference.File, source),
		id,
		syncdomain.VariationModeCompletion,
		reference,
		syncdomain.VariationProjection{Mode: syncdomain.VariationModeCompletion, Messages: &desired},
	)
	require.NoError(t, err)
	assert.Equal(t, CapabilityDirect, update.Fields[syncdomain.FieldPrompt].Capability)
}

func TestAgentSkillMapsDescriptionAndBody(t *testing.T) {
	source := []byte("---\nname: refunds\ndescription: Explain refunds.\nlicense: Apache-2.0\n---\n\n# Refunds\n\nAsk for the order ID.\n")
	id := syncdomain.ResourceID{Kind: syncdomain.KindSkill, ProjectKey: "project", LookupKey: "refunds"}
	reference := syncdomain.Reference{File: ".agents/skills/refunds/SKILL.md", Format: "agent-skill"}

	projection, err := ParseSource(sourceBundle(reference.File, source), id, "", reference)
	require.NoError(t, err)
	skill := projection.(syncdomain.SkillProjection)
	assert.Equal(t, "Explain refunds.", *skill.Description)
	assert.Equal(t, "# Refunds\n\nAsk for the order ID.\n", *skill.Markdown)

	description := "Explain refunds and escalation."
	markdown := "# Refunds\n\nEscalate large refunds.\n"
	update, err := UpdateSource(
		sourceBundle(reference.File, source),
		id,
		"",
		reference,
		syncdomain.SkillProjection{Description: &description, Markdown: &markdown},
	)
	require.NoError(t, err)
	assert.Equal(t, CapabilityDirect, update.Fields[syncdomain.FieldDescription].Capability)
	assert.Equal(t, CapabilityDirect, update.Fields[syncdomain.FieldMarkdown].Capability)
}

func TestAgentSkillDescriptionUpdatePreservesInlineComment(t *testing.T) {
	source := []byte("---\nname: refunds\ndescription: \"Explain refunds.\" # catalog text\n---\n\n# Refunds\n")
	id := syncdomain.ResourceID{Kind: syncdomain.KindSkill, ProjectKey: "project", LookupKey: "refunds"}
	reference := syncdomain.Reference{File: "SKILL.md", Format: "agent-skill"}
	description := "Explain refunds and escalation."

	update, err := UpdateSource(
		sourceBundle(reference.File, source),
		id,
		"",
		reference,
		syncdomain.SkillProjection{Description: &description},
	)

	require.NoError(t, err)
	updated := applyTestEdits(source, update.Fields[syncdomain.FieldDescription].Edits...)
	assert.Contains(t, string(updated), `description: "Explain refunds and escalation." # catalog text`)
}

func TestAgentSkillRejectsInvalidFiles(t *testing.T) {
	id := syncdomain.ResourceID{Kind: syncdomain.KindSkill, ProjectKey: "project", LookupKey: "refunds"}
	reference := syncdomain.Reference{File: "SKILL.md", Format: "agent-skill"}
	tests := []struct {
		name, source, message string
	}{
		{name: "missing front matter", source: "# Skill\n", message: "missing YAML front matter"},
		{name: "unclosed front matter", source: "---\ndescription: Help\n", message: "unclosed YAML front matter"},
		{name: "missing description", source: "---\nname: refunds\n---\n\n# Skill\n", message: "path_not_found"},
		{name: "missing body", source: "---\ndescription: Help\n---\n", message: "skill Markdown is required"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseSource(
				sourceBundle(reference.File, []byte(test.source)), id, "", reference,
			)
			require.Error(t, err)
			problem, ok := syncdomain.ProblemFrom(err)
			require.True(t, ok)
			if test.message == "path_not_found" {
				assert.Equal(t, syncdomain.ErrorPathNotFound, problem.Code)
			} else {
				assert.Equal(t, syncdomain.ErrorInvalidSource, problem.Code)
				assert.Contains(t, err.Error(), test.message)
			}
		})
	}
}

func TestPythonAgentProjectionAndLiteralRoundTrip(t *testing.T) {
	source := []byte(`from agents import Agent, ModelSettings

support = Agent(
    instructions='Ask for an order ID.',
    model="gpt-5",
    model_settings=ModelSettings(temperature=0.2),
)
`)
	id := syncdomain.VariationID("project", "support", "default")
	reference := syncdomain.Reference{
		File: "agents.py", Format: "python", Select: `agents[id="support"]`,
		Map: map[string]string{
			"prompt":                          "instructions",
			"model":                           "model",
			`model.parameters["temperature"]`: "model_settings.temperature",
		},
	}
	projection, err := ParseSource(sourceBundle(reference.File, source), id, syncdomain.VariationModeAgent, reference)
	require.NoError(t, err)
	variation := projection.(syncdomain.VariationProjection)
	assert.Equal(t, "Ask for an order ID.", *variation.Instructions)
	assert.Equal(t, "gpt-5", *variation.Model)
	assert.Equal(t, 0.2, (*variation.Parameters)["temperature"])

	prompt := "Ask for an order ID and \"region\"."
	parameters := map[string]any{"temperature": 0.4}
	update, err := UpdateSource(
		sourceBundle(reference.File, source),
		id,
		syncdomain.VariationModeAgent,
		reference,
		syncdomain.VariationProjection{
			Mode: syncdomain.VariationModeAgent, Instructions: &prompt,
			Model: variation.Model, Parameters: &parameters,
			FieldStates: map[syncdomain.Field]syncdomain.FieldState{
				`model.parameters["temperature"]`: {Capability: syncdomain.CapabilityDirect, Known: true},
			},
		},
	)
	require.NoError(t, err)
	assert.Equal(t, CapabilityDirect, update.Fields[syncdomain.FieldPrompt].Capability)
	assert.Equal(t, CapabilityDirect, update.Fields[`model.parameters["temperature"]`].Capability)
}

func TestPythonToolDescriptionFromStaticDocstring(t *testing.T) {
	source := []byte(`from agents import function_tool

@function_tool
def lookup_order(order_id: str):
    """Look up one order."""
    return order_id
`)
	id := syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "lookup-order"}
	reference := syncdomain.Reference{
		File: "tools.py", Format: "python", Select: `tools[id="lookup_order"]`,
		Map: map[string]string{"description": "description"},
	}

	projection, err := ParseSource(sourceBundle(reference.File, source), id, "", reference)

	require.NoError(t, err)
	assert.Equal(t, "Look up one order.", *projection.(syncdomain.ToolProjection).Description)
}

func TestPythonPromptUsesImmutableSameFileConstant(t *testing.T) {
	source := []byte(`from langchain_core.prompts import PromptTemplate

TEMPLATE = """Summarize the case.
Keep the answer short."""
summary = PromptTemplate(template=TEMPLATE)
`)
	id := syncdomain.VariationID("project", "summary", "default")
	reference := syncdomain.Reference{
		File: "prompts.py", Format: "python", Select: `prompts[id="summary"]`,
		Map: map[string]string{"prompt": "template"},
	}

	projection, err := ParseSource(sourceBundle(reference.File, source), id, syncdomain.VariationModeAgent, reference)

	require.NoError(t, err)
	assert.Equal(t, "Summarize the case.\nKeep the answer short.",
		*projection.(syncdomain.VariationProjection).Instructions)
}

func TestTypeScriptToolUsesImportAwareInspector(t *testing.T) {
	source := []byte(`import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
const server = new McpServer({ name: "orders", version: "1" });
server.registerTool("lookup", {
  description: "Look up an order.",
  inputSchema: { type: "object" },
}, handler);
`)
	id := syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "lookup"}
	reference := syncdomain.Reference{
		File: "tools.ts", Format: "typescript", Select: `tools[id="lookup"]`,
		Map: map[string]string{"description": "description", "schema": "inputSchema"},
	}
	projection, err := ParseSource(sourceBundle(reference.File, source), id, "", reference)
	require.NoError(t, err)
	tool := projection.(syncdomain.ToolProjection)
	assert.Equal(t, "Look up an order.", *tool.Description)
	assert.Equal(t, "object", (*tool.Schema)["type"])

	description := "Look up one order and its status."
	schema := map[string]any{"type": "object", "required": []any{"orderId"}}
	update, err := UpdateSource(
		sourceBundle(reference.File, source),
		id,
		"",
		reference,
		syncdomain.ToolProjection{Description: &description, Schema: &schema},
	)
	require.NoError(t, err)
	assert.Equal(t, CapabilityDirect, update.Fields[syncdomain.FieldDescription].Capability)
	assert.Equal(t, CapabilityDirect, update.Fields[syncdomain.FieldSchema].Capability)
}

type fakeAdapter struct{}

func (fakeAdapter) Format() string { return "fake-test" }
func (fakeAdapter) Inspect(bundle SourceBundle) (Inspection, error) {
	document := bundle.Primary
	return Inspection{root: stringNode(string(document.Data), 0, len(document.Data), func(text string) ([]byte, error) {
		return []byte(text), nil
	})}, nil
}
func (fakeAdapter) Defaults(kind syncdomain.Kind) (string, map[string]string) {
	return "$", map[string]string{string(syncdomain.FieldPrompt): "$"}
}

func TestRegisteredAdapterRunsThroughSharedCore(t *testing.T) {
	require.NoError(t, Register(fakeAdapter{}))
	id := syncdomain.VariationID("project", "config", "default")

	projection, err := ParseSource(
		sourceBundle("prompt.fake", []byte("Registered without a core change.")),
		id,
		syncdomain.VariationModeAgent,
		syncdomain.Reference{File: "prompt.fake", Format: "fake-test"},
	)

	require.NoError(t, err)
	assert.Equal(t, "Registered without a core change.", *projection.(syncdomain.VariationProjection).Instructions)
}

func TestRegistryRejectsInvalidAndDuplicateAdapters(t *testing.T) {
	require.Error(t, Register(nil))
	adapter := namedFakeAdapter{format: "duplicate-test"}
	require.NoError(t, Register(adapter))
	require.Error(t, Register(adapter))

	_, err := registeredAdapter("not-registered")
	problem, ok := syncdomain.ProblemFrom(err)
	require.True(t, ok)
	assert.Equal(t, syncdomain.ErrorUnsupportedFormat, problem.Code)
}

func TestReferenceValidationUsesStableProblem(t *testing.T) {
	id := syncdomain.VariationID("project", "config", "default")
	tests := []syncdomain.Reference{
		{Format: "python", Select: `agents[id="support"]`, Map: map[string]string{"prompt": "instructions"}},
		{File: "agents.py", Format: "python", Map: map[string]string{"prompt": "instructions"}},
		{File: "agents.py", Format: "python", Select: `agents[id="support"]`},
		{File: "agents.py", Format: "python", Select: `agents[id="support"]`, Map: map[string]string{"description": "instructions"}},
	}
	for _, reference := range tests {
		_, err := NormalizeReference(id, reference)
		problem, ok := syncdomain.ProblemFrom(err)
		require.True(t, ok)
		assert.Equal(t, syncdomain.ErrorInvalidReference, problem.Code)
		assert.Equal(t, id.Label(), problem.Resource)
	}
}

type namedFakeAdapter struct{ format string }

func (adapter namedFakeAdapter) Format() string { return adapter.format }
func (namedFakeAdapter) Inspect(bundle SourceBundle) (Inspection, error) {
	return fakeAdapter{}.Inspect(bundle)
}
func (namedFakeAdapter) Defaults(kind syncdomain.Kind) (string, map[string]string) {
	return fakeAdapter{}.Defaults(kind)
}

func TestTypeScriptAgentProjection(t *testing.T) {
	source := []byte(`import { Agent } from "@openai/agents";
const support = new Agent({
  instructions: "Help with orders.",
  model: "gpt-5",
});
`)
	id := syncdomain.VariationID("project", "support", "default")
	reference := syncdomain.Reference{
		File: "agents.ts", Format: "typescript", Select: `agents[id="support"]`,
		Map: map[string]string{"prompt": "instructions", "model": "model"},
	}

	projection, err := ParseSource(sourceBundle(reference.File, source), id, syncdomain.VariationModeAgent, reference)

	require.NoError(t, err)
	variation := projection.(syncdomain.VariationProjection)
	assert.Equal(t, "Help with orders.", *variation.Instructions)
	assert.Equal(t, "gpt-5", *variation.Model)
}

func TestTypeScriptPromptProjection(t *testing.T) {
	source := []byte(`import { PromptTemplate } from "@langchain/core/prompts";
const summary = new PromptTemplate({
  template: "Summarize the case.",
});
`)
	id := syncdomain.VariationID("project", "summary", "default")
	reference := syncdomain.Reference{
		File: "prompts.ts", Format: "typescript", Select: `prompts[id="summary"]`,
		Map: map[string]string{"prompt": "template"},
	}

	projection, err := ParseSource(sourceBundle(reference.File, source), id, syncdomain.VariationModeAgent, reference)

	require.NoError(t, err)
	assert.Equal(t, "Summarize the case.", *projection.(syncdomain.VariationProjection).Instructions)
}

func TestDynamicExpressionsAreReadOnly(t *testing.T) {
	source := []byte(`import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
const server = new McpServer({ name: "orders", version: "1" });
server.registerTool("lookup", {
  description: ` + "`Look up ${kind}.`" + `,
}, handler);
`)
	id := syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "lookup"}
	reference := syncdomain.Reference{
		File: "tools.ts", Format: "typescript", Select: `tools[id="lookup"]`,
		Map: map[string]string{"description": "description"},
	}
	description := "Look up one order."
	update, err := UpdateSource(
		sourceBundle(reference.File, source),
		id,
		"",
		reference,
		syncdomain.ToolProjection{Description: &description},
	)
	require.NoError(t, err)
	assert.Equal(t, CapabilityReadOnly, update.Fields[syncdomain.FieldDescription].Capability)
	assert.Empty(t, update.Fields[syncdomain.FieldDescription].Edits)
}

func TestCodeAdaptersRejectInvalidUTF8(t *testing.T) {
	id := syncdomain.VariationID("project", "support", "default")
	reference := syncdomain.Reference{
		File: "agents.py", Format: "python", Select: `agents[id="support"]`,
		Map: map[string]string{"prompt": "instructions"},
	}

	_, err := ParseSource(sourceBundle(reference.File, []byte{0xff}), id, syncdomain.VariationModeAgent, reference)

	require.ErrorContains(t, err, "valid UTF-8")
}

func sourceBundle(path string, data []byte) SourceBundle {
	return SourceBundle{Primary: Document{Path: path, Data: data}}
}

func applyTestEdits(source []byte, edits ...TextEdit) []byte {
	result := bytes.Clone(source)
	for index := len(edits) - 1; index >= 0; index-- {
		edit := edits[index]
		result = append(append(append([]byte{}, result[:edit.Start]...), edit.Replacement...), result[edit.End:]...)
	}
	return result
}

func FuzzPathParserNeverPanics(f *testing.F) {
	for _, value := range []string{
		"$", `agents[id="support"]`, `items[role="system",name="intro"]`,
		`metadata["display.name"]`, "items[",
	} {
		f.Add(value)
	}
	f.Fuzz(func(t *testing.T, value string) {
		_ = ParsePath(value)
	})
}

func FuzzPythonStringRoundTrip(f *testing.F) {
	for _, value := range []string{"plain", "quotes ' \" and slash \\", "line one\nline two", "Unicode 🌍"} {
		f.Add(value)
	}
	f.Fuzz(func(t *testing.T, value string) {
		if !utf8.ValidString(value) {
			t.Skip()
		}
		for _, quote := range []string{"'", `"`, `"""`} {
			encoded := encodePythonString(value, quote)
			decoded, _, err := decodePythonString(encoded)
			require.NoError(t, err)
			require.Equal(t, value, decoded)
		}
	})
}

func FuzzTypeScriptStringRoundTrip(f *testing.F) {
	for _, value := range []string{"plain", "quotes ' \" and slash \\", "line one\nline two", "Unicode 🌍"} {
		f.Add(value)
	}
	f.Fuzz(func(t *testing.T, value string) {
		if !utf8.ValidString(value) {
			t.Skip()
		}
		for _, quote := range []byte{'\'', '"'} {
			encoded, err := encodeTypeScriptString(value, quote)
			require.NoError(t, err)
			decoded, _, err := decodeTypeScriptString(encoded)
			require.NoError(t, err)
			require.Equal(t, value, decoded)
		}
	})
}

func TestIndexedSelectionIdentityUsesStableFieldsAndFallback(t *testing.T) {
	identified := &sourceNode{fields: map[string]*sourceNode{
		"id": {value: "support"},
		"n":  {value: 1},
	}}
	assert.Equal(t, map[string]any{"id": "support"}, directSelectionIdentity(identified))
	assert.Nil(t, directSelectionIdentity(nil))
	assert.True(t, scalarValue(uint64(2)))
	assert.False(t, scalarValue(nil))
	assert.False(t, sourceCollection(nil))
	assert.True(t, sourceCollection(&sourceNode{value: []string{"one"}}))
	assert.Equal(t,
		[]any{"one"},
		stableNodeValue(&sourceNode{items: []*sourceNode{{value: "one"}}, collection: true}, map[*sourceNode]bool{}),
	)
	assert.Nil(t, stableNodeValue(&sourceNode{}, map[*sourceNode]bool{}))

	cycle := &sourceNode{fields: map[string]*sourceNode{}}
	cycle.fields["self"] = cycle
	fallback := &sourceNode{fields: map[string]*sourceNode{"prompt": {value: "Help"}}}
	selections, err := indexedSelections(
		[]indexedResolution{
			{path: "items[0]", collectionLength: 1, item: identified},
			{path: "items[0].children[0]", collectionLength: 1, item: fallback},
			{path: "items[0].cycle[0]", collectionLength: 1, item: cycle},
		},
		fallback,
	)
	require.NoError(t, err)
	require.Len(t, selections, 3)
	for _, selection := range selections {
		assert.NotEmpty(t, selection.Fingerprint)
	}

	_, err = indexedSelections(
		[]indexedResolution{{path: "items[0]", collectionLength: 1, item: &sourceNode{value: make(chan int)}}},
		fallback,
	)
	require.ErrorContains(t, err, "encode indexed selection fingerprint")
}
