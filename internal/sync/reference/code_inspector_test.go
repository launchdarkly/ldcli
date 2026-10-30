package reference

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
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
			projection, err := ParseSource(sourceBundle(reference.File, source), id, syncdomain.VariationModeAgent, reference)
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
			result, err := UpdateSource(
				sourceBundle(reference.File, source), id, syncdomain.VariationModeAgent, reference, desired,
			)
			require.NoError(t, err)
			updated := applyConformanceEdits(t, source, directEdits(t, result))
			reparsed, err := ParseSource(
				sourceBundle(reference.File, updated), id, syncdomain.VariationModeAgent, reference,
			)
			require.NoError(t, err)
			assert.Equal(t, next, *reparsed.(syncdomain.VariationProjection).Instructions)
		})
	}
}

func TestPythonConstantLiteralKinds(t *testing.T) {
	source := fixtureFile(t, "python/settings.py")
	root, err := parseSourceDocument(Document{Path: "settings.py", Data: source}, "python")
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
		node, err := resolveSourcePath(root, test.path)
		require.NoError(t, err)
		assert.Equal(t, test.want, node.value)
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
			projection, selections, err := ParseSourceWithSelections(
				sourceBundle(reference.File, []byte(test.source)),
				id, syncdomain.VariationModeAgent, reference,
			)
			require.NoError(t, err)
			assert.Equal(t, "Old", *projection.(syncdomain.VariationProjection).Instructions)
			require.Len(t, selections, 1)

			next := "Updated"
			result, err := UpdateSource(
				sourceBundle(reference.File, []byte(test.source)),
				id, syncdomain.VariationModeAgent, reference,
				syncdomain.VariationProjection{Mode: syncdomain.VariationModeAgent, Instructions: &next},
			)
			require.NoError(t, err)
			updated := applyConformanceEdits(t, []byte(test.source), directEdits(t, result))
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

	projection, err := ParseSource(sourceBundle(reference.File, source), id, syncdomain.VariationModeAgent, reference)
	require.NoError(t, err)
	state, ok := syncdomain.ProjectionFieldState(projection, syncdomain.FieldPrompt)
	require.True(t, ok)
	assert.Equal(t, syncdomain.CapabilityReadOnly, state.Capability)
	assert.False(t, state.Known)

	model := "gpt-5.1"
	result, err := UpdateSource(
		sourceBundle(reference.File, source), id, syncdomain.VariationModeAgent, reference,
		syncdomain.VariationProjection{Mode: syncdomain.VariationModeAgent, Model: &model},
	)
	require.NoError(t, err)
	update := result.Fields[syncdomain.FieldModel]
	assert.Equal(t, CapabilityDirect, update.Capability)
	assert.Len(t, update.Edits, 1)
	updated := applyConformanceEdits(t, source, update.Edits)
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
			projection, err := ParseSource(sourceBundle(reference.File, source), id, test.mode, reference)
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
			result, err := UpdateSource(sourceBundle(reference.File, source), id, test.mode, reference, desired)
			require.NoError(t, err)
			updated := applyConformanceEdits(t, source, directEdits(t, result))
			_, err = ParseSource(sourceBundle(reference.File, updated), id, test.mode, reference)
			require.NoError(t, err)
		})
	}

	toolID := syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "lookup_order"}
	toolRef := syncdomain.Reference{
		File: "langchain_prompts.py", Format: "python", Select: `tools[id="lookup_order"]`,
		Map: map[string]string{"description": "description", "schema": "schema"},
	}
	projection, err := ParseSource(sourceBundle(toolRef.File, source), toolID, "", toolRef)
	require.NoError(t, err)
	tool := projection.(syncdomain.ToolProjection)
	assert.Equal(t, "Look up one order and return its status.", *tool.Description)
	schemaState, ok := syncdomain.ProjectionFieldState(tool, syncdomain.FieldSchema)
	require.True(t, ok)
	assert.Equal(t, syncdomain.CapabilityReadOnly, schemaState.Capability)
	description := "Look up an order and its current status."
	result, err := UpdateSource(
		sourceBundle(toolRef.File, source), toolID, "", toolRef,
		syncdomain.ToolProjection{Description: &description},
	)
	require.NoError(t, err)
	updated := applyConformanceEdits(t, source, directEdits(t, result))
	assert.Contains(t, string(updated), `"""Look up an order and its current status."""`)
}

func TestAmbiguousInlineChainWritesNothing(t *testing.T) {
	source := fixtureFile(t, "python/langchain_prompts.py")
	id := syncdomain.VariationID("project", "config", "default")
	reference := syncdomain.Reference{
		File: "langchain_prompts.py", Format: "python", Select: `prompts[id="ambiguous"]`,
		Map: map[string]string{"messages": "messages"},
	}

	_, err := ParseSource(sourceBundle(reference.File, source), id, syncdomain.VariationModeCompletion, reference)

	problem, ok := syncdomain.ProblemFrom(err)
	require.True(t, ok)
	assert.Equal(t, syncdomain.ErrorPathAmbiguous, problem.Code)
}

func TestCompletionMessagesEditContentButRejectStructure(t *testing.T) {
	source := fixtureFile(t, "python/messages.py")
	id := syncdomain.VariationID("project", "config", "default")
	reference := syncdomain.Reference{
		File: "messages.py", Format: "python", Select: `agents[id="completion"]`,
		Map: map[string]string{"messages": "messages"},
	}
	projection, err := ParseSource(sourceBundle(reference.File, source), id, syncdomain.VariationModeCompletion, reference)
	require.NoError(t, err)
	messages := append([]syncdomain.Message{}, (*projection.(syncdomain.VariationProjection).Messages)...)
	messages[0].Content = "Updated system prompt."
	messages[1].Content = "Updated user prompt."

	result, err := UpdateSource(
		sourceBundle(reference.File, source), id, syncdomain.VariationModeCompletion, reference,
		syncdomain.VariationProjection{Mode: syncdomain.VariationModeCompletion, Messages: &messages},
	)
	require.NoError(t, err)
	update := result.Fields[syncdomain.FieldMessages]
	assert.Equal(t, CapabilityDirect, update.Capability)
	assert.Len(t, update.Edits, 2)
	updated := applyConformanceEdits(t, source, update.Edits)
	assert.Contains(t, string(updated), `"content": "Updated system prompt."`)
	assert.Contains(t, string(updated), `"content": "Updated user prompt."`)

	messages = append(messages, syncdomain.Message{Role: syncdomain.RoleAssistant, Content: "Extra"})
	result, err = UpdateSource(
		sourceBundle(reference.File, source), id, syncdomain.VariationModeCompletion, reference,
		syncdomain.VariationProjection{Mode: syncdomain.VariationModeCompletion, Messages: &messages},
	)
	require.NoError(t, err)
	assert.Equal(t, CapabilityReadOnly, result.Fields[syncdomain.FieldMessages].Capability)
	assert.Contains(t, result.Fields[syncdomain.FieldMessages].Reason, "add or remove messages by hand")
}

func TestTypeScriptLangChainAndQualifiedConstant(t *testing.T) {
	source := fixtureFile(t, "typescript/langchain.ts")
	id := syncdomain.VariationID("project", "config", "default")
	tests := []struct {
		name, selectPath, field, mapPath string
		mode                             syncdomain.VariationMode
	}{
		{name: "messages", selectPath: `prompts[id="chat"]`, field: "messages", mapPath: "messages", mode: syncdomain.VariationModeCompletion},
		{name: "constant messages", selectPath: `prompts[id="sharedChat"]`, field: "messages", mapPath: "messages", mode: syncdomain.VariationModeCompletion},
		{name: "template", selectPath: `prompts[id="simple"]`, field: "prompt", mapPath: "template", mode: syncdomain.VariationModeAgent},
		{name: "constructor", selectPath: `prompts[id="constructor"]`, field: "messages", mapPath: "messages", mode: syncdomain.VariationModeCompletion},
		{name: "function constant", selectPath: `constants[id="build.prompt"]`, field: "prompt", mapPath: "$", mode: syncdomain.VariationModeAgent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reference := syncdomain.Reference{
				File: "langchain.ts", Format: "typescript", Select: test.selectPath,
				Map: map[string]string{test.field: test.mapPath},
			}
			projection, err := ParseSource(sourceBundle(reference.File, source), id, test.mode, reference)
			require.NoError(t, err)
			assert.NotNil(t, projection)
		})
	}
}

func TestMutationAndSpreadStayLocalToTheirFields(t *testing.T) {
	pythonSource := []byte(`PROMPT = "Initial"
PROMPT += " changed"
CONFIG = {"prompt": "Initial"}
CONFIG["prompt"] = "changed"
`)
	pythonRoot, err := parseSourceDocument(Document{Path: "settings.py", Data: pythonSource}, "python")
	require.NoError(t, err)
	prompt, err := resolveSourcePath(pythonRoot, `constants[id="PROMPT"]`)
	require.NoError(t, err)
	assert.Equal(t, CapabilityReadOnly, prompt.capability)
	assert.Contains(t, prompt.reason, "mutated")
	config, err := resolveSourcePath(pythonRoot, `constants[id="CONFIG"]`)
	require.NoError(t, err)
	assert.Equal(t, CapabilityReadOnly, config.capability)

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
	projection, err := ParseSource(sourceBundle(reference.File, typeScriptSource), id, "", reference)
	require.NoError(t, err)
	tool := projection.(syncdomain.ToolProjection)
	assert.Equal(t, "Static description", *tool.Description)
	schemaState, ok := syncdomain.ProjectionFieldState(tool, syncdomain.FieldSchema)
	require.True(t, ok)
	assert.Equal(t, syncdomain.CapabilityReadOnly, schemaState.Capability)
	root, err := parseSourceDocument(Document{Path: "config.ts", Data: typeScriptSource}, "typescript")
	require.NoError(t, err)
	config, err = resolveSourcePath(root, `constants[id="CONFIG"]`)
	require.NoError(t, err)
	assert.Equal(t, CapabilityReadOnly, config.capability)
}

func TestTypeScriptAndJavaScriptConstantLiteralKinds(t *testing.T) {
	source := []byte(`const CONFIG = {
  single: 'one',
  double: "two",
  template: ` + "`three`" + `,
  retries: 3,
  enabled: true,
  models: ["gpt-5", "claude-sonnet"],
  nested: { prompt: "Help" },
};`)
	for _, format := range []string{"typescript", "javascript"} {
		t.Run(format, func(t *testing.T) {
			root, err := parseSourceDocument(Document{Path: "config.js", Data: source}, format)
			require.NoError(t, err)
			tests := []struct {
				path string
				want any
			}{
				{path: `constants[id="CONFIG"].single`, want: "one"},
				{path: `constants[id="CONFIG"].double`, want: "two"},
				{path: `constants[id="CONFIG"].template`, want: "three"},
				{path: `constants[id="CONFIG"].retries`, want: 3.0},
				{path: `constants[id="CONFIG"].enabled`, want: true},
				{path: `constants[id="CONFIG"].models`, want: []any{"gpt-5", "claude-sonnet"}},
				{path: `constants[id="CONFIG"].nested.prompt`, want: "Help"},
			}
			for _, test := range tests {
				node, err := resolveSourcePath(root, test.path)
				require.NoError(t, err)
				assert.Equal(t, test.want, node.value)
			}
		})
	}
}
