package reference

import (
	"encoding/json"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

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
			root, err := parseSourceDocument(Document{Path: "source", Data: []byte(test.source)}, test.format)
			require.NoError(t, err)
			value, err := resolveSourcePath(root, test.path)
			require.NoError(t, err)
			assert.Equal(t, test.want, value.value)
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
		root, err := parseSourceDocument(Document{Path: "value.json", Data: source}, "json")
		require.NoError(t, err)
		node, err := resolveSourcePath(root, "value")
		require.NoError(t, err)
		encoded, err := node.encode(node.value)
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
			encoded, err := encodeYAMLString(value, previous)
			if err != nil {
				t.Skip()
			}
			source := append([]byte("value: "), encoded...)
			source = append(source, '\n')
			root, err := parseSourceDocument(Document{Path: "value.yaml", Data: source}, "yaml")
			require.NoError(t, err)
			node, err := resolveSourcePath(root, "value")
			require.NoError(t, err)
			require.Equal(t, value, node.value)
		}
	})
}

func TestYAMLStringCodecRejectsUnsupportedCodePoint(t *testing.T) {
	_, err := encodeYAMLString("\uffff", "plain")
	require.ErrorContains(t, err, "unsupported character U+FFFF")
}

func FuzzTOMLStringCodec(f *testing.F) {
	for _, value := range []string{"plain", "quotes \" and slash \\", "line one\nline two", "Unicode 🌍"} {
		f.Add(value)
	}
	f.Fuzz(func(t *testing.T, value string) {
		if !utf8.ValidString(value) {
			t.Skip()
		}
		encoded, err := encodeTOMLString(value, `"plain"`)
		require.NoError(t, err)
		source := append([]byte("value = "), encoded...)
		source = append(source, '\n')
		root, err := parseSourceDocument(Document{Path: "value.toml", Data: source}, "toml")
		require.NoError(t, err)
		node, err := resolveSourcePath(root, "value")
		require.NoError(t, err)
		require.Equal(t, value, node.value)
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
			_, err := parseSourceDocument(Document{Path: "source", Data: []byte(test.source)}, test.format)
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
			_, err := parseSourceDocument(
				Document{Path: "source", Data: []byte(test.source)}, test.format,
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
			root, err := parseSourceDocument(Document{Path: "agents.yaml", Data: []byte(test.source)}, "yaml")
			require.NoError(t, err)
			value, err := resolveSourcePath(root, test.path)
			require.NoError(t, err)
			assert.Equal(t, CapabilityReadOnly, value.capability)
			assert.NotEmpty(t, value.reason)
		})
	}
}

func TestTOMLArrayTableFieldsAreReadOnly(t *testing.T) {
	source := `[[tool.plugins]]
name = "audit"
enabled = true
`
	root, err := parseSourceDocument(Document{Path: "pyproject.toml", Data: []byte(source)}, "toml")
	require.NoError(t, err)

	value, err := resolveSourcePath(root, `tool.plugins[name="audit"].enabled`)

	require.NoError(t, err)
	assert.Equal(t, CapabilityReadOnly, value.capability)
}

func TestStructuredEditsPreserveBOMAndCRLFOutsideSpan(t *testing.T) {
	source := []byte("\ufeff{\r\n  \"description\": \"Old\",\r\n  \"enabled\": true\r\n}\r\n")
	id := syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "tool"}
	reference := syncdomain.Reference{
		File: "tool.json", Format: "json", Select: "$",
		Map: map[string]string{"description": "description"},
	}
	description := "New 🌍"

	result, err := UpdateSource(
		sourceBundle(reference.File, source), id, "", reference,
		syncdomain.ToolProjection{Description: &description},
	)

	require.NoError(t, err)
	updated := applyConformanceEdits(t, source, directEdits(t, result))
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
			projection, selections, err := ParseSourceWithSelections(sourceBundle(reference.File, source), id, "", reference)
			require.NoError(t, err)
			assert.Equal(t, "Old", *projection.(syncdomain.ToolProjection).Description)
			require.Len(t, selections, 1)
			assert.Equal(t, 2, selections[0].CollectionLength)

			description := "Updated"
			update, err := UpdateSource(
				sourceBundle(reference.File, source), id, "", reference,
				syncdomain.ToolProjection{Description: &description},
			)
			require.NoError(t, err)
			updated := applyConformanceEdits(t, source, directEdits(t, update))
			assert.Contains(t, string(updated), test.unchanged)
			assert.Contains(t, string(updated), "Updated")

			roundTrip, _, err := ParseSourceWithSelections(
				sourceBundle(reference.File, updated), id, "", reference,
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

	_, baseline, err := ParseSourceWithSelections(
		sourceBundle(reference.File, before), id, syncdomain.VariationModeCompletion, reference,
	)
	require.NoError(t, err)
	_, reordered, err := ParseSourceWithSelections(
		sourceBundle(reference.File, after), id, syncdomain.VariationModeCompletion, reference,
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

	result, err := UpdateSource(
		sourceBundle(reference.File, source), id, syncdomain.VariationModeAgent, reference,
		syncdomain.VariationProjection{Mode: syncdomain.VariationModeAgent, Instructions: &prompt},
	)

	require.NoError(t, err)
	updated := applyConformanceEdits(t, source, directEdits(t, result))
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
			root, err := parseSourceDocument(Document{Path: "source", Data: source}, test.format)
			require.NoError(t, err)
			node, err := resolveSourcePath(root, test.path)
			require.NoError(t, err)
			assert.Equal(t, test.want, node.value)
			require.Equal(t, CapabilityDirect, node.capability)
			replacement, err := node.encode(test.next)
			require.NoError(t, err)
			updated := append(append(append([]byte{}, source[:node.start]...), replacement...), source[node.end:]...)
			root, err = parseSourceDocument(Document{Path: "source", Data: updated}, test.format)
			require.NoError(t, err)
			node, err = resolveSourcePath(root, test.path)
			require.NoError(t, err)
			assert.Equal(t, test.next, node.value)
		})
	}
}

func TestStructuredProjectionCollectionsAndObjects(t *testing.T) {
	variationSource := []byte(`{
  "prompt": "Help",
  "tools": ["lookup"],
  "skills": ["refunds"]
}`)
	variationID := syncdomain.VariationID("project", "config", "default")
	variationRef := syncdomain.Reference{
		File: "variation.json", Format: "json", Select: "$",
		Map: map[string]string{"prompt": "prompt", "tools": "tools", "skills": "skills"},
	}
	projection, err := ParseSource(
		sourceBundle(variationRef.File, variationSource), variationID,
		syncdomain.VariationModeAgent, variationRef,
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
	projection, err = ParseSource(sourceBundle(toolRef.File, toolSource), toolID, "", toolRef)
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

	_, err := ParseSource(
		sourceBundle(reference.File, []byte(`{"prompt":42}`)),
		id, syncdomain.VariationModeAgent, reference,
	)

	problem, ok := syncdomain.ProblemFrom(err)
	require.True(t, ok)
	assert.Equal(t, syncdomain.ErrorMapTypeMismatch, problem.Code)
}
