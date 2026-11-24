package all

import (
	"strconv"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	"github.com/launchdarkly/ldcli/internal/sync/adapter"
	"github.com/launchdarkly/ldcli/internal/sync/adapter/adaptertest"
	"github.com/launchdarkly/ldcli/internal/sync/adapter/code"
)

func TestPythonQualifiedConstantsAndDictionaryMembers(t *testing.T) {
	source := fixtureFile(t, "python/settings.py")
	tests := []struct {
		name       string
		selectPath string
		fieldMap   map[string]string
		wantPrompt string
		wantModel  string
	}{
		{
			name: "class attribute", selectPath: `constants[id="SupportSettings.instructions"]`,
			fieldMap:   map[string]string{"prompt": "$"},
			wantPrompt: "Ask for an order ID.\nKeep the answer concise.",
		},
		{
			name: "function local", selectPath: `constants[id="build.prompt"]`,
			fieldMap:   map[string]string{"prompt": "$"},
			wantPrompt: "Function-local prompt.",
		},
		{
			name: "nested dictionary", selectPath: `constants[id="CONFIG"].agents.refunds`,
			fieldMap:   map[string]string{"prompt": "prompt", "model": "model"},
			wantPrompt: "Explain the refund policy.", wantModel: "gpt-5",
		},
		{
			name: "enum dictionary key", selectPath: `constants[id="PROMPTS"]["Language.EN"]`,
			fieldMap:   map[string]string{"prompt": "$"},
			wantPrompt: "Answer in English.",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			id := syncdomain.VariationID("project", "config", "default")
			reference := syncdomain.Reference{
				File: "settings.py", Format: "python", Select: test.selectPath, Map: test.fieldMap,
			}
			projection, err := adapter.ParseSource(adaptertest.Bundle(reference.File, source), id, reference)
			require.NoError(t, err)
			variation := projection.(syncdomain.VariationProjection)
			require.NotNil(t, variation.Instructions)
			assert.Equal(t, test.wantPrompt, *variation.Instructions)
			if test.wantModel != "" {
				require.NotNil(t, variation.Model)
				assert.Equal(t, test.wantModel, *variation.Model)
			}

			next := "Updated prompt 🌍"
			desired := syncdomain.VariationProjection{
				Mode: syncdomain.VariationModeAgent, Instructions: &next,
			}
			if variation.Model != nil {
				model := "gpt-5.1"
				desired.Model = &model
			}
			result, err := adapter.UpdateSource(
				adaptertest.Bundle(reference.File, source), id, reference, desired,
			)
			require.NoError(t, err)
			updated := adaptertest.ApplyChecked(t, source, adaptertest.DirectEdits(t, result))
			reparsed, err := adapter.ParseSource(
				adaptertest.Bundle(reference.File, updated), id, reference,
			)
			require.NoError(t, err)
			assert.Equal(t, next, *reparsed.(syncdomain.VariationProjection).Instructions)
		})
	}
}

func TestPythonConstantLiteralKinds(t *testing.T) {
	source := fixtureFile(t, "python/settings.py")
	root, err := adapter.ParseDocument(adapter.Document{Path: "settings.py", Data: source}, "python")
	require.NoError(t, err)
	tests := []struct {
		path string
		want any
	}{
		{path: `constants[id="SupportSettings.temperature"]`, want: 0.2},
		{path: `constants[id="SupportSettings.enabled"]`, want: true},
		{path: `constants[id="build.retries"]`, want: 3.0},
		{path: `constants[id="MODELS"]`, want: []any{"gpt-5", "claude-sonnet"}},
		{path: `constants[id="CONFIG"].agents.refunds.model`, want: "gpt-5"},
	}
	for _, test := range tests {
		node, err := root.Resolve(test.path)
		require.NoError(t, err)
		assert.Equal(t, test.want, node.Value)
	}
}

func TestCodeIndexesSelectAndUpdateConstants(t *testing.T) {
	tests := []struct {
		name, format, source string
	}{
		{
			name: "Python", format: "python",
			source: "VALUES = [{\"name\": \"first\", \"prompt\": \"Keep\"}, {\"name\": \"second\", \"prompt\": \"Old\"}]\n",
		},
	}
	id := syncdomain.VariationID("project", "config", "default")
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reference := syncdomain.Reference{
				File: "values", Format: test.format, Select: `constants[id="VALUES"][1]`,
				Map: map[string]string{"prompt": "prompt"},
			}
			projection, selections, err := adapter.ParseSourceWithSelections(
				adaptertest.Bundle(reference.File, []byte(test.source)),
				id, reference,
			)
			require.NoError(t, err)
			assert.Equal(t, "Old", *projection.(syncdomain.VariationProjection).Instructions)
			require.Len(t, selections, 1)

			next := "Updated"
			result, err := adapter.UpdateSource(
				adaptertest.Bundle(reference.File, []byte(test.source)),
				id, reference,
				syncdomain.VariationProjection{Mode: syncdomain.VariationModeAgent, Instructions: &next},
			)
			require.NoError(t, err)
			updated := adaptertest.ApplyChecked(t, []byte(test.source), adaptertest.DirectEdits(t, result))
			assert.Contains(t, string(updated), "Keep")
			assert.Contains(t, string(updated), "Updated")
		})
	}
}

func TestDynamicDictionaryMemberDoesNotBlockLiteralModelEdit(t *testing.T) {
	source := fixtureFile(t, "python/settings.py")
	id := syncdomain.VariationID("project", "config", "default")
	reference := syncdomain.Reference{
		File: "settings.py", Format: "python", Select: `constants[id="CONFIG"].agents.support`,
		Map: map[string]string{"prompt": "prompt", "model": "model"},
	}

	projection, err := adapter.ParseSource(adaptertest.Bundle(reference.File, source), id, reference)
	require.NoError(t, err)
	state, ok := projection.States()[syncdomain.FieldPrompt]
	require.True(t, ok)
	assert.Equal(t, syncdomain.CapabilityReadOnly, state.Capability)
	assert.False(t, state.Known)

	model := "gpt-5.1"
	result, err := adapter.UpdateSource(
		adaptertest.Bundle(reference.File, source), id, reference,
		syncdomain.VariationProjection{Mode: syncdomain.VariationModeAgent, Model: &model},
	)
	require.NoError(t, err)
	update := result.Fields[syncdomain.FieldModel]
	assert.Equal(t, adapter.CapabilityDirect, update.Capability)
	assert.Len(t, update.Edits, 1)
	updated := adaptertest.ApplyChecked(t, source, update.Edits)
	assert.Contains(t, string(updated), `"model": "gpt-5.1"`)
	assert.Contains(t, string(updated), `f"Use {DEFAULT_MODEL} for support."`)
}

func TestPythonLangChainPromptFormsAndDocstring(t *testing.T) {
	source := fixtureFile(t, "python/langchain_prompts.py")
	tests := []struct {
		name       string
		selectPath string
		mapPath    string
		mode       syncdomain.VariationMode
	}{
		{name: "from messages tuples", selectPath: `prompts[id="chat"]`, mapPath: "messages", mode: syncdomain.VariationModeCompletion},
		{name: "from messages constant", selectPath: `prompts[id="shared_chat"]`, mapPath: "messages", mode: syncdomain.VariationModeCompletion},
		{name: "constructor message objects", selectPath: `prompts[id="chat_constructor"]`, mapPath: "messages", mode: syncdomain.VariationModeCompletion},
		{name: "from template", selectPath: `prompts[id="simple"]`, mapPath: "template", mode: syncdomain.VariationModeAgent},
		{name: "template keyword", selectPath: `prompts[id="simple_keyword"]`, mapPath: "template", mode: syncdomain.VariationModeAgent},
		{name: "inline chain", selectPath: `prompts[id="chain"]`, mapPath: "messages", mode: syncdomain.VariationModeCompletion},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			id := syncdomain.VariationID("project", "config", "default")
			field := "prompt"
			if test.mode == syncdomain.VariationModeCompletion {
				field = "messages"
			}
			reference := syncdomain.Reference{
				File: "langchain_prompts.py", Format: "python", Select: test.selectPath,
				Map: map[string]string{field: test.mapPath},
			}
			projection, err := adapter.ParseSource(adaptertest.Bundle(reference.File, source), id, reference)
			require.NoError(t, err)
			desired := projection
			switch value := projection.(type) {
			case syncdomain.VariationProjection:
				if value.Messages != nil {
					messages := append([]syncdomain.Message{}, (*value.Messages)...)
					messages[len(messages)-1].Content = "Updated content."
					value.Messages = &messages
				} else {
					text := "Updated template."
					value.Instructions = &text
				}
				desired = value
			}
			result, err := adapter.UpdateSource(adaptertest.Bundle(reference.File, source), id, reference, desired)
			require.NoError(t, err)
			updated := adaptertest.ApplyChecked(t, source, adaptertest.DirectEdits(t, result))
			_, err = adapter.ParseSource(adaptertest.Bundle(reference.File, updated), id, reference)
			require.NoError(t, err)
		})
	}

	toolID := syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "lookup_order"}
	toolRef := syncdomain.Reference{
		File: "langchain_prompts.py", Format: "python", Select: `tools[id="lookup_order"]`,
		Map: map[string]string{"description": "description", "schema": "schema"},
	}
	projection, err := adapter.ParseSource(adaptertest.Bundle(toolRef.File, source), toolID, toolRef)
	require.NoError(t, err)
	tool := projection.(syncdomain.ToolProjection)
	assert.Equal(t, "Look up one order and return its status.", *tool.Description)
	schemaState, ok := tool.States()[syncdomain.FieldSchema]
	require.True(t, ok)
	assert.Equal(t, syncdomain.CapabilityReadOnly, schemaState.Capability)
	description := "Look up an order and its current status."
	result, err := adapter.UpdateSource(
		adaptertest.Bundle(toolRef.File, source), toolID, toolRef,
		syncdomain.ToolProjection{Description: &description},
	)
	require.NoError(t, err)
	updated := adaptertest.ApplyChecked(t, source, adaptertest.DirectEdits(t, result))
	assert.Contains(t, string(updated), `"""Look up an order and its current status."""`)
}

// A chain that holds two prompts names them with an ordinal, so the bare
// variable name matches nothing and each prompt has a key of its own.
func TestInlineChainWithTwoPromptsNumbersThem(t *testing.T) {
	source := fixtureFile(t, "python/langchain_prompts.py")
	id := syncdomain.VariationID("project", "config", "default")
	reference := syncdomain.Reference{
		File: "langchain_prompts.py", Format: "python", Select: `prompts[id="ambiguous"]`,
		Map: map[string]string{"messages": "messages"},
	}

	_, err := adapter.ParseSource(adaptertest.Bundle(reference.File, source), id, reference)

	problem, ok := syncdomain.ProblemFrom(err)
	require.True(t, ok)
	assert.Equal(t, syncdomain.ErrorPathNotFound, problem.Code)
	reference.Select = `prompts[id="ambiguous#1"]`
	_, err = adapter.ParseSource(adaptertest.Bundle(reference.File, source), id, reference)
	require.NoError(t, err)
}

func TestCompletionMessagesEditContentButRejectStructure(t *testing.T) {
	source := fixtureFile(t, "python/messages.py")
	id := syncdomain.VariationID("project", "config", "default")
	reference := syncdomain.Reference{
		File: "messages.py", Format: "python", Select: `agents[id="completion"]`,
		Map: map[string]string{"messages": "messages"},
	}
	projection, err := adapter.ParseSource(adaptertest.Bundle(reference.File, source), id, reference)
	require.NoError(t, err)
	messages := append([]syncdomain.Message{}, (*projection.(syncdomain.VariationProjection).Messages)...)
	messages[0].Content = "Updated system prompt."
	messages[1].Content = "Updated user prompt."

	result, err := adapter.UpdateSource(
		adaptertest.Bundle(reference.File, source), id, reference,
		syncdomain.VariationProjection{Mode: syncdomain.VariationModeCompletion, Messages: &messages},
	)
	require.NoError(t, err)
	update := result.Fields[syncdomain.FieldMessages]
	assert.Equal(t, adapter.CapabilityDirect, update.Capability)
	assert.Len(t, update.Edits, 2)
	updated := adaptertest.ApplyChecked(t, source, update.Edits)
	assert.Contains(t, string(updated), `"content": "Updated system prompt."`)
	assert.Contains(t, string(updated), `"content": "Updated user prompt."`)

	messages = append(messages, syncdomain.Message{Role: syncdomain.RoleAssistant, Content: "Extra"})
	result, err = adapter.UpdateSource(
		adaptertest.Bundle(reference.File, source), id, reference,
		syncdomain.VariationProjection{Mode: syncdomain.VariationModeCompletion, Messages: &messages},
	)
	require.NoError(t, err)
	assert.Equal(t, adapter.CapabilityReadOnly, result.Fields[syncdomain.FieldMessages].Capability)
	assert.Contains(t, result.Fields[syncdomain.FieldMessages].Reason, "add or remove messages by hand")
}

func TestMutationAndSpreadStayLocalToTheirFields(t *testing.T) {
	pythonSource := []byte(`PROMPT = "Initial"
PROMPT += " changed"
CONFIG = {"prompt": "Initial"}
CONFIG["prompt"] = "changed"
`)
	pythonRoot, err := adapter.ParseDocument(adapter.Document{Path: "settings.py", Data: pythonSource}, "python")
	require.NoError(t, err)
	prompt, err := pythonRoot.Resolve(`constants[id="PROMPT"]`)
	require.NoError(t, err)
	assert.Equal(t, adapter.CapabilityReadOnly, prompt.Capability)
	assert.Contains(t, prompt.Reason, "mutated")
	config, err := pythonRoot.Resolve(`constants[id="CONFIG"]`)
	require.NoError(t, err)
	assert.Equal(t, adapter.CapabilityReadOnly, config.Capability)

	typeScriptSource := []byte(`const CONFIG = {
  description: "Static description",
  ...runtimeValues,
  model: helper(),
};
CONFIG.description = "changed";`)
	id := syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "config"}
	reference := syncdomain.Reference{
		File: "config.ts", Format: "typescript", Select: `constants[id="CONFIG"]`,
		Map: map[string]string{"description": "description", "schema": "model"},
	}
	projection, err := adapter.ParseSource(adaptertest.Bundle(reference.File, typeScriptSource), id, reference)
	require.NoError(t, err)
	tool := projection.(syncdomain.ToolProjection)
	assert.Equal(t, "Static description", *tool.Description)
	schemaState, ok := tool.States()[syncdomain.FieldSchema]
	require.True(t, ok)
	assert.Equal(t, syncdomain.CapabilityReadOnly, schemaState.Capability)
	root, err := adapter.ParseDocument(adapter.Document{Path: "config.ts", Data: typeScriptSource}, "typescript")
	require.NoError(t, err)
	config, err = root.Resolve(`constants[id="CONFIG"]`)
	require.NoError(t, err)
	assert.Equal(t, adapter.CapabilityReadOnly, config.Capability)
}

func TestLangChainPlaceholderTranslationRoundTrip(t *testing.T) {
	source := []byte(`from langchain_core.prompts import PromptTemplate
prompt = PromptTemplate.from_template("Hello {customer}.")
`)
	id := syncdomain.VariationID("project", "config", "default")
	reference := syncdomain.Reference{
		File: "prompt.py", Format: "python", Select: `prompts[id="prompt"]`,
		Map: map[string]string{"prompt": "template"},
	}
	projection, err := adapter.ParseSource(
		adaptertest.Bundle(reference.File, source), id, reference,
	)
	require.NoError(t, err)
	assert.Equal(t, "Hello {{customer}}.", *projection.(syncdomain.VariationProjection).Instructions)

	next := "Welcome {{customer_name}}."
	result, err := adapter.UpdateSource(
		adaptertest.Bundle(reference.File, source), id, reference,
		syncdomain.VariationProjection{Mode: syncdomain.VariationModeAgent, Instructions: &next},
	)
	require.NoError(t, err)
	updated := adaptertest.ApplyChecked(t, source, adaptertest.DirectEdits(t, result))
	assert.Contains(t, string(updated), `"Welcome {customer_name}."`)
}

func TestAmbiguousPlaceholdersAreReadOnly(t *testing.T) {
	tests := []struct {
		name, template string
	}{
		{name: "escaped braces", template: `Use {{literal}} braces.`},
		{name: "JSON example", template: `Return {"status":"ok"}.`},
		{name: "format conversion", template: `Show {value!r}.`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := []byte("from langchain_core.prompts import PromptTemplate\nprompt = PromptTemplate.from_template(" +
				strconv.Quote(test.template) + ")\n")
			id := syncdomain.VariationID("project", "config", "default")
			reference := syncdomain.Reference{
				File: "prompt.py", Format: "python", Select: `prompts[id="prompt"]`,
				Map: map[string]string{"prompt": "template"},
			}
			projection, err := adapter.ParseSource(
				adaptertest.Bundle(reference.File, source), id, reference,
			)
			require.NoError(t, err)
			state, ok := projection.States()[syncdomain.FieldPrompt]
			require.True(t, ok)
			assert.Equal(t, syncdomain.CapabilityReadOnly, state.Capability)
			assert.Equal(t, syncdomain.ErrorPlaceholderAmbiguous, state.Code)
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
	projection, err := adapter.ParseSource(adaptertest.Bundle(reference.File, source), id, reference)
	require.NoError(t, err)
	variation := projection.(syncdomain.VariationProjection)
	assert.Equal(t, "Ask for an order ID.", *variation.Instructions)
	assert.Equal(t, "gpt-5", *variation.Model)
	assert.Equal(t, 0.2, (*variation.Parameters)["temperature"])

	prompt := "Ask for an order ID and \"region\"."
	parameters := map[string]any{"temperature": 0.4}
	update, err := adapter.UpdateSource(
		adaptertest.Bundle(reference.File, source), id, reference,
		syncdomain.VariationProjection{
			Mode: syncdomain.VariationModeAgent, Instructions: &prompt,
			Model: variation.Model, Parameters: &parameters,
			FieldStates: map[syncdomain.Field]syncdomain.FieldState{
				`model.parameters["temperature"]`: {Capability: syncdomain.CapabilityDirect, Known: true},
			},
		},
	)
	require.NoError(t, err)
	assert.Equal(t, adapter.CapabilityDirect, update.Fields[syncdomain.FieldPrompt].Capability)
	assert.Equal(t, adapter.CapabilityDirect, update.Fields[`model.parameters["temperature"]`].Capability)
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

	projection, err := adapter.ParseSource(adaptertest.Bundle(reference.File, source), id, reference)

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

	projection, err := adapter.ParseSource(adaptertest.Bundle(reference.File, source), id, reference)

	require.NoError(t, err)
	assert.Equal(t, "Summarize the case.\nKeep the answer short.",
		*projection.(syncdomain.VariationProjection).Instructions)
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
			encoded := code.EncodePythonString(value, quote)
			decoded, _, err := code.DecodePythonString(encoded)
			require.NoError(t, err)
			require.Equal(t, value, decoded)
		}
	})
}

func TestDecoratedResourcesHaveStableIDs(t *testing.T) {
	source := []byte(`
from langchain_core.tools import tool

@tool
def lookup(order_id: str):
    """Look up an order."""
    return order_id

@tool
def search(query: str):
    """Search the docs."""
    return query
`)
	id := syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "search"}
	reference := syncdomain.Reference{
		File: "tools.py", Format: "python", Select: `tools[id="search"]`,
		Map: map[string]string{"description": "description"},
	}

	projection, selections, err := adapter.ParseSourceWithSelections(adaptertest.Bundle("tools.py", source), id, reference)

	require.NoError(t, err)
	assert.Equal(t, "Search the docs.", *projection.(syncdomain.ToolProjection).Description)
	assert.Empty(t, selections, "a function name is a stable identity, not a position")
}
