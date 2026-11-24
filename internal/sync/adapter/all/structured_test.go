package all

import (
	"encoding/json"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	"github.com/launchdarkly/ldcli/internal/sync/adapter"
	"github.com/launchdarkly/ldcli/internal/sync/adapter/adaptertest"
	"github.com/launchdarkly/ldcli/internal/sync/adapter/yamltree"
)

func TestYAMLPlainScalarSpansCoverCommasAndBrackets(t *testing.T) {
	source := []byte("agents:\n  - name: support\n    prompt: Ask for an order ID, then the region [and date]\n    tags: [one, two]\n")
	root, err := adapter.ParseDocument(adapter.Document{Path: "agents.yaml", Data: source}, "yaml")
	require.NoError(t, err)

	prompt, err := root.Resolve(`agents[name="support"].prompt`)
	require.NoError(t, err)
	assert.Equal(t, "Ask for an order ID, then the region [and date]", string(source[prompt.Start:prompt.End]))
	assert.Equal(t, adapter.CapabilityDirect, prompt.Capability)

	tag, err := root.Resolve(`agents[name="support"].tags[0]`)
	require.NoError(t, err)
	assert.Equal(t, "one", string(source[tag.Start:tag.End]))

	folded := []byte("prompt: first line\n  second line\n")
	root, err = adapter.ParseDocument(adapter.Document{Path: "prompt.yaml", Data: folded}, "yaml")
	require.NoError(t, err)
	value, err := root.Resolve("prompt")
	require.NoError(t, err)
	assert.Equal(t, adapter.CapabilityReadOnly, value.Capability)
}

func TestJSONIndividualModelParameterMappingsRoundTrip(t *testing.T) {
	tests := []struct {
		name        string
		key         string
		source      any
		replacement any
	}{
		{name: "temperature", key: "temperature", source: 0.2, replacement: 0.7},
		{name: "top k", key: "top_k", source: 20, replacement: 40},
		{name: "top p", key: "top_p", source: 0.9, replacement: 0.8},
		{name: "max tokens", key: "max_tokens", source: 256, replacement: 512},
		{name: "future provider key", key: "Provider.Future-Key", source: "v1", replacement: "v2"},
		{name: "string", key: "response_style", source: "brief", replacement: "detailed"},
		{name: "boolean", key: "reasoning", source: true, replacement: false},
		{name: "null", key: "seed", source: nil, replacement: true},
		{
			name: "nested object", key: "provider",
			source:      map[string]any{"mode": "fast"},
			replacement: map[string]any{"mode": "careful", "retry": true},
		},
		{
			name: "array", key: "stop",
			source:      []any{"END"},
			replacement: []any{"DONE", map[string]any{"token": 2}},
		},
	}
	id := syncdomain.VariationID("project", "config", "default")
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source, err := json.Marshal(map[string]any{"prompt": "Help", "value": test.source})
			require.NoError(t, err)
			target := adaptertest.ModelParameterTarget(test.key)
			reference := syncdomain.Reference{
				File: "agent.json", Format: "json",
				Map: map[string]string{"prompt": "prompt", target: "value"},
			}

			projection, err := adapter.ParseSource(
				adaptertest.Bundle(reference.File, source), id, reference,
			)
			require.NoError(t, err)
			value, present := projection.Value(syncdomain.Field(target))
			assert.True(t, present)
			adaptertest.AssertJSONEqual(t, test.source, value)

			parameters := map[string]any{test.key: test.replacement}
			update, err := adapter.UpdateSource(
				adaptertest.Bundle(reference.File, source), id, reference,
				syncdomain.VariationProjection{
					Mode: syncdomain.VariationModeAgent, Parameters: &parameters,
					FieldStates: map[syncdomain.Field]syncdomain.FieldState{
						syncdomain.Field(target): {Capability: syncdomain.CapabilityDirect, Known: true},
					},
				},
			)
			require.NoError(t, err)
			require.Len(t, update.Fields[syncdomain.Field(target)].Edits, 1)
			updated := adaptertest.ApplyEdits(source, update.Fields[syncdomain.Field(target)].Edits...)
			reparsed, err := adapter.ParseSource(
				adaptertest.Bundle(reference.File, updated), id, reference,
			)
			require.NoError(t, err)
			value, present = reparsed.Value(syncdomain.Field(target))
			assert.True(t, present)
			adaptertest.AssertJSONEqual(t, test.replacement, value)
		})
	}
}

func TestJSONWholeModelParametersOwnsCanonicalMap(t *testing.T) {
	source := []byte(`{"prompt":"Help","parameters":{"temperature":0.2,"tools":[{"name":"search"}]}}`)
	id := syncdomain.VariationID("project", "config", "default")
	reference := syncdomain.Reference{
		File: "agent.json", Format: "json",
		Map: map[string]string{"prompt": "prompt", "model.parameters": "parameters"},
	}
	projection, err := adapter.ParseSource(
		adaptertest.Bundle(reference.File, source), id, reference,
	)
	require.NoError(t, err)
	parameters := projection.(syncdomain.VariationProjection).Parameters
	require.NotNil(t, parameters)
	assert.Equal(t, map[string]any{"temperature": 0.2}, *parameters)

	replacement := map[string]any{
		"top_k":  30,
		"nested": map[string]any{"enabled": true},
		"stop":   []any{"END"},
		"seed":   nil,
	}
	update, err := adapter.UpdateSource(
		adaptertest.Bundle(reference.File, source), id, reference,
		syncdomain.VariationProjection{
			Mode: syncdomain.VariationModeAgent, Parameters: &replacement,
			FieldStates: map[syncdomain.Field]syncdomain.FieldState{
				syncdomain.FieldModelParameters: {Capability: syncdomain.CapabilityDirect, Known: true},
			},
		},
	)
	require.NoError(t, err)
	require.Len(t, update.Fields[syncdomain.FieldModelParameters].Edits, 1)
	updated := adaptertest.ApplyEdits(source, update.Fields[syncdomain.FieldModelParameters].Edits...)
	assert.JSONEq(t, `{"prompt":"Help","parameters":{"top_k":30,"nested":{"enabled":true},"stop":["END"],"seed":null}}`, string(updated))
}

func TestStructuredPathsReadNestedListsAndLiteralKeys(t *testing.T) {
	tests := []struct {
		name   string
		format string
		source string
		path   string
		want   any
	}{
		{
			name: "JSON keyed list", format: "json",
			source: `{"items":[{"name":"one","enabled":false},{"name":"two","enabled":true}]}`,
			path:   `items[name="two"].enabled`, want: true,
		},
		{
			name: "YAML nested field", format: "yaml",
			source: "agent:\n  model:\n    temperature: 0.3\n",
			path:   "agent.model.temperature", want: 0.3,
		},
		{
			name: "TOML literal key", format: "toml",
			source: "[metadata]\n\"display.name\" = \"Support\"\n",
			path:   `metadata["display.name"]`, want: "Support",
		},
		{
			name: "TOML inline table list", format: "toml",
			source: "items = [{ name = \"one\", prompt = \"First\" }, { name = \"two\", prompt = \"Second\" }]\n",
			path:   `items[name="two"].prompt`, want: "Second",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, err := adapter.ParseDocument(adapter.Document{Path: "source", Data: []byte(test.source)}, test.format)
			require.NoError(t, err)
			value, err := root.Resolve(test.path)
			require.NoError(t, err)
			assert.Equal(t, test.want, value.Value)
		})
	}
}

func FuzzJSONStringCodec(f *testing.F) {
	for _, value := range []string{"plain", "quotes \" and slash \\", "line one\nline two", "Unicode 🌍"} {
		f.Add(value)
	}
	f.Fuzz(func(t *testing.T, value string) {
		if !utf8.ValidString(value) {
			t.Skip()
		}
		quoted, err := json.Marshal(value)
		require.NoError(t, err)
		source := append([]byte(`{"value":`), quoted...)
		source = append(source, '}')
		root, err := adapter.ParseDocument(adapter.Document{Path: "value.json", Data: source}, "json")
		require.NoError(t, err)
		node, err := root.Resolve("value")
		require.NoError(t, err)
		encoded, err := node.Encode(node.Value)
		require.NoError(t, err)
		var decoded string
		require.NoError(t, json.Unmarshal(encoded, &decoded))
		require.Equal(t, value, decoded)
	})
}

func FuzzYAMLStringCodec(f *testing.F) {
	for _, value := range []string{"plain", "quote ' and slash \\", "line one\nline two", "Unicode 🌍"} {
		f.Add(value)
	}
	f.Fuzz(func(t *testing.T, value string) {
		if !utf8.ValidString(value) {
			t.Skip()
		}
		for _, previous := range []string{"plain", "'plain'", `"plain"`} {
			encoded, err := yamltree.EncodeString(value, previous)
			if err != nil {
				t.Skip()
			}
			source := append([]byte("value: "), encoded...)
			source = append(source, '\n')
			root, err := adapter.ParseDocument(adapter.Document{Path: "value.yaml", Data: source}, "yaml")
			require.NoError(t, err)
			node, err := root.Resolve("value")
			require.NoError(t, err)
			require.Equal(t, value, node.Value)
		}
	})
}

func TestStructuredAdaptersRejectDuplicateKeys(t *testing.T) {
	tests := []struct {
		name   string
		format string
		source string
	}{
		{name: "JSON", format: "json", source: `{"name":"first","name":"second"}`},
		{name: "YAML", format: "yaml", source: "name: first\nname: second\n"},
		{name: "TOML", format: "toml", source: "name = \"first\"\nname = \"second\"\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := adapter.ParseDocument(adapter.Document{Path: "source", Data: []byte(test.source)}, test.format)
			require.Error(t, err)
			problem, ok := syncdomain.ProblemFrom(err)
			require.True(t, ok)
			assert.Equal(t, syncdomain.ErrorDuplicateKey, problem.Code)
		})
	}
}

func TestStructuredAdaptersRejectInvalidSyntax(t *testing.T) {
	tests := []struct {
		name, format, source string
	}{
		{name: "empty JSON", format: "json", source: ""},
		{name: "JSON trailing input", format: "json", source: `{"a":1} true`},
		{name: "JSON missing colon", format: "json", source: `{"a" 1}`},
		{name: "JSON missing comma", format: "json", source: `[1 2]`},
		{name: "JSON unclosed string", format: "json", source: `{"a":"value}`},
		{name: "invalid YAML", format: "yaml", source: "value: [\n"},
		{name: "empty YAML", format: "yaml", source: ""},
		{name: "invalid TOML", format: "toml", source: "value = [\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := adapter.ParseDocument(
				adapter.Document{Path: "source", Data: []byte(test.source)}, test.format,
			)
			require.Error(t, err)
			problem, ok := syncdomain.ProblemFrom(err)
			require.True(t, ok)
			assert.Equal(t, syncdomain.ErrorInvalidSource, problem.Code)
		})
	}
}

func TestYAMLUnsafeFormsAreReadOnly(t *testing.T) {
	tests := []struct {
		name   string
		source string
		path   string
	}{
		{name: "block scalar", source: "prompt: |\n  Help the user.\n", path: "prompt"},
		{name: "anchor", source: "prompt: &shared \"Help\"\n", path: "prompt"},
		{name: "alias", source: "base: &shared \"Help\"\nprompt: *shared\n", path: "prompt"},
		{name: "merge", source: "base: &base\n  prompt: Help\nagent:\n  <<: *base\n", path: "agent.prompt"},
		{name: "tag", source: "prompt: !custom Help\n", path: "prompt"},
		{name: "multiple documents", source: "prompt: First\n---\nprompt: Second\n", path: "prompt"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, err := adapter.ParseDocument(adapter.Document{Path: "agents.yaml", Data: []byte(test.source)}, "yaml")
			require.NoError(t, err)
			value, err := root.Resolve(test.path)
			require.NoError(t, err)
			assert.Equal(t, adapter.CapabilityReadOnly, value.Capability)
			assert.NotEmpty(t, value.Reason)
		})
	}
}

func TestTOMLArrayTableFieldsAreReadOnly(t *testing.T) {
	source := `[[tool.plugins]]
name = "audit"
enabled = true
`
	root, err := adapter.ParseDocument(adapter.Document{Path: "pyproject.toml", Data: []byte(source)}, "toml")
	require.NoError(t, err)

	value, err := root.Resolve(`tool.plugins[name="audit"].enabled`)

	require.NoError(t, err)
	assert.Equal(t, adapter.CapabilityReadOnly, value.Capability)
}

func TestStructuredEditsPreserveBOMAndCRLFOutsideSpan(t *testing.T) {
	source := []byte("\ufeff{\r\n  \"description\": \"Old\",\r\n  \"enabled\": true\r\n}\r\n")
	id := syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "tool"}
	reference := syncdomain.Reference{
		File: "tool.json", Format: "json", Select: "$",
		Map: map[string]string{"description": "description"},
	}
	description := "New 🌍"

	result, err := adapter.UpdateSource(
		adaptertest.Bundle(reference.File, source), id, reference,
		syncdomain.ToolProjection{Description: &description},
	)

	require.NoError(t, err)
	updated := adaptertest.ApplyChecked(t, source, adaptertest.DirectEdits(t, result))
	assert.Contains(t, string(updated), "\ufeff{\r\n")
	assert.Contains(t, string(updated), "\"enabled\": true\r\n")
	assert.Contains(t, string(updated), `"description": "New 🌍"`)
}

func TestStructuredIndexWriteRoundTripsPreserveOtherItems(t *testing.T) {
	tests := []struct {
		name, format, source, unchanged string
	}{
		{
			name: "JSON", format: "json",
			source:    "{\n  \"tools\": [{\"name\":\"first\",\"description\":\"Keep\"}, {\"name\":\"second\",\"description\":\"Old\"}]\n}\n",
			unchanged: `{"name":"first","description":"Keep"}`,
		},
		{
			name: "YAML", format: "yaml",
			source:    "tools:\n  - {name: first, description: Keep}\n  - {name: second, description: Old}\n",
			unchanged: "{name: first, description: Keep}",
		},
		{
			name: "TOML", format: "toml",
			source:    "tools = [{ name = \"first\", description = \"Keep\" }, { name = \"second\", description = \"Old\" }]\n",
			unchanged: `{ name = "first", description = "Keep" }`,
		},
	}
	id := syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "second"}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := []byte(test.source)
			reference := syncdomain.Reference{
				File: "tools." + test.format, Format: test.format, Select: "tools[1]",
				Map: map[string]string{"description": "description"},
			}
			projection, selections, err := adapter.ParseSourceWithSelections(adaptertest.Bundle(reference.File, source), id, reference)
			require.NoError(t, err)
			assert.Equal(t, "Old", *projection.(syncdomain.ToolProjection).Description)
			require.Len(t, selections, 1)
			assert.Equal(t, 2, selections[0].CollectionLength)

			description := "Updated"
			update, err := adapter.UpdateSource(
				adaptertest.Bundle(reference.File, source), id, reference,
				syncdomain.ToolProjection{Description: &description},
			)
			require.NoError(t, err)
			updated := adaptertest.ApplyChecked(t, source, adaptertest.DirectEdits(t, update))
			assert.Contains(t, string(updated), test.unchanged)
			assert.Contains(t, string(updated), "Updated")

			roundTrip, _, err := adapter.ParseSourceWithSelections(
				adaptertest.Bundle(reference.File, updated), id, reference,
			)
			require.NoError(t, err)
			assert.Equal(t, description, *roundTrip.(syncdomain.ToolProjection).Description)
		})
	}
}

func TestIndexedSelectionsDetectNestedReorders(t *testing.T) {
	id := syncdomain.VariationID("project", "completion", "default")
	reference := syncdomain.Reference{
		File: "agents.json", Format: "json", Select: "agents[0]",
		Map: map[string]string{"prompt": "messages[0].content"},
	}
	before := []byte(`{"agents":[
		{"id":"support","messages":[{"role":"system","content":"Help"},{"role":"user","content":"Example"}]},
		{"id":"other","messages":[{"role":"system","content":"Other"}]}
	]}`)
	after := []byte(`{"agents":[
		{"id":"other","messages":[{"role":"system","content":"Other"}]},
		{"id":"support","messages":[{"role":"user","content":"Example"},{"role":"system","content":"Help"}]}
	]}`)

	_, baseline, err := adapter.ParseSourceWithSelections(
		adaptertest.Bundle(reference.File, before), id, reference,
	)
	require.NoError(t, err)
	_, reordered, err := adapter.ParseSourceWithSelections(
		adaptertest.Bundle(reference.File, after), id, reference,
	)
	require.NoError(t, err)

	require.Len(t, baseline, 2)
	require.Len(t, reordered, 2)
	assert.NotEqual(t, baseline, reordered)
}

func TestYAMLUnicodeColumnsAndFlowScalarsUseExactSpans(t *testing.T) {
	source := []byte("étiquette: {prompt: ancien, enabled: true}\n")
	id := syncdomain.VariationID("project", "config", "default")
	reference := syncdomain.Reference{
		File: "agents.yaml", Format: "yaml", Select: `["étiquette"]`,
		Map: map[string]string{"prompt": "prompt"},
	}
	prompt := "nouveau"

	result, err := adapter.UpdateSource(
		adaptertest.Bundle(reference.File, source), id, reference,
		syncdomain.VariationProjection{Mode: syncdomain.VariationModeAgent, Instructions: &prompt},
	)

	require.NoError(t, err)
	updated := adaptertest.ApplyChecked(t, source, adaptertest.DirectEdits(t, result))
	assert.Equal(t, "étiquette: {prompt: nouveau, enabled: true}\n", string(updated))
}

func TestStructuredValueFormsRoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		format string
		source string
		path   string
		want   any
		next   any
	}{
		{name: "JSON escaped Unicode string", format: "json", source: `{"value":"Hello \u4e16\u754c"}`, path: "value", want: "Hello 世界", next: "Hello 🌍"},
		{name: "JSON number", format: "json", source: `{"value":1.25}`, path: "value", want: 1.25, next: 2.5},
		{name: "JSON boolean", format: "json", source: `{"value":true}`, path: "value", want: true, next: false},
		{name: "JSON object", format: "json", source: `{"value":{"enabled":true}}`, path: "value", want: map[string]any{"enabled": true}, next: map[string]any{"enabled": false}},
		{name: "YAML plain string", format: "yaml", source: "value: hello\n", path: "value", want: "hello", next: "updated"},
		{name: "YAML single quoted string", format: "yaml", source: "value: 'hello'\n", path: "value", want: "hello", next: "updated"},
		{name: "YAML double quoted string", format: "yaml", source: "value: \"hello\\nworld\"\n", path: "value", want: "hello\nworld", next: "updated\nworld"},
		{name: "YAML number", format: "yaml", source: "value: 1.25\n", path: "value", want: 1.25, next: 2.5},
		{name: "YAML boolean", format: "yaml", source: "value: true\n", path: "value", want: true, next: false},
		{name: "TOML basic string", format: "toml", source: "value = \"hello\"\n", path: "value", want: "hello", next: "updated"},
		{name: "TOML literal string", format: "toml", source: "value = 'hello'\n", path: "value", want: "hello", next: "updated"},
		{name: "TOML multiline string", format: "toml", source: "value = \"\"\"hello\nworld\"\"\"\n", path: "value", want: "hello\nworld", next: "updated\nworld"},
		{name: "TOML number", format: "toml", source: "value = 1.25\n", path: "value", want: 1.25, next: 2.5},
		{name: "TOML boolean", format: "toml", source: "value = true\n", path: "value", want: true, next: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := []byte(test.source)
			root, err := adapter.ParseDocument(adapter.Document{Path: "source", Data: source}, test.format)
			require.NoError(t, err)
			node, err := root.Resolve(test.path)
			require.NoError(t, err)
			assert.Equal(t, test.want, node.Value)
			require.Equal(t, adapter.CapabilityDirect, node.Capability)
			replacement, err := node.Encode(test.next)
			require.NoError(t, err)
			updated := append(append(append([]byte{}, source[:node.Start]...), replacement...), source[node.End:]...)
			root, err = adapter.ParseDocument(adapter.Document{Path: "source", Data: updated}, test.format)
			require.NoError(t, err)
			node, err = root.Resolve(test.path)
			require.NoError(t, err)
			assert.Equal(t, test.next, node.Value)
		})
	}
}

func TestStructuredProjectionCollectionsAndObjects(t *testing.T) {
	variationSource := []byte(`{
  "prompt": "Help",
  "tools": ["source-tool"],
  "skills": ["source-skill"]
}`)
	variationID := syncdomain.VariationID("project", "config", "default")
	tools := []syncdomain.AttachmentRef{{Key: "lookup"}}
	skills := []syncdomain.AttachmentRef{{Key: "refunds"}}
	variationRef := syncdomain.Reference{
		File: "variation.json", Format: "json", Select: "$",
		Map:   map[string]string{"prompt": "prompt"},
		Tools: &tools, Skills: &skills,
	}
	projection, err := adapter.ParseSource(
		adaptertest.Bundle(variationRef.File, variationSource), variationID, variationRef,
	)
	require.NoError(t, err)
	variation := projection.(syncdomain.VariationProjection)
	assert.Equal(t, []string{"lookup"}, *variation.Tools)
	assert.Equal(t, []string{"refunds"}, *variation.Skills)

	toolSource := []byte(`{
  "description": "Lookup",
  "schema": {"type": "object"},
  "customParameters": {"timeout": 5},
  "tags": ["orders", "support"]
}`)
	toolID := syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "lookup"}
	toolRef := syncdomain.Reference{
		File: "tool.json", Format: "json", Select: "$",
		Map: map[string]string{
			"description": "description", "schema": "schema",
			"customParameters": "customParameters", "tags": "tags",
		},
	}
	projection, err = adapter.ParseSource(adaptertest.Bundle(toolRef.File, toolSource), toolID, toolRef)
	require.NoError(t, err)
	tool := projection.(syncdomain.ToolProjection)
	assert.Equal(t, "object", (*tool.Schema)["type"])
	assert.Equal(t, float64(5), (*tool.CustomParameters)["timeout"])
	assert.Equal(t, []string{"orders", "support"}, *tool.Tags)
}

func TestMapTypeMismatchUsesStableCode(t *testing.T) {
	id := syncdomain.VariationID("project", "config", "default")
	reference := syncdomain.Reference{
		File: "value.json", Format: "json", Select: "$",
		Map: map[string]string{"prompt": "prompt"},
	}

	_, err := adapter.ParseSource(
		adaptertest.Bundle(reference.File, []byte(`{"prompt":42}`)),
		id, reference,
	)

	problem, ok := syncdomain.ProblemFrom(err)
	require.True(t, ok)
	assert.Equal(t, syncdomain.ErrorMapTypeMismatch, problem.Code)
}
