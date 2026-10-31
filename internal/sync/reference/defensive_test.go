package reference

import (
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sitter "github.com/tree-sitter/go-tree-sitter"
	pythonlanguage "github.com/tree-sitter/tree-sitter-python/bindings/go"
	typescriptlanguage "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
	"gopkg.in/yaml.v3"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

func TestStringCodecsHandleSupportedEscapes(t *testing.T) {
	python := []byte(`"\\\'\"\n\r\t\b\f\v\x41\u0042\U0001F600"`)
	value, _, err := decodePythonString(python)
	require.NoError(t, err)
	assert.Equal(t, "\\'\"\n\r\t\b\f\vAB😀", value)

	typescript := []byte(`"\\\'\"\n\r\t\b\f\v\x41\u0042"`)
	value, _, err = decodeTypeScriptString(typescript)
	require.NoError(t, err)
	assert.Equal(t, "\\'\"\n\r\t\b\f\vAB", value)

	for name, input := range map[string][]byte{
		"short":               {'"'},
		"bad delimiter":       []byte(`x`),
		"template expression": []byte("`hello ${name}`"),
		"trailing slash":      []byte(`"hello\"`),
		"bad hex":             []byte(`"\xGG"`),
		"TypeScript U escape": []byte(`"\U00000041"`),
	} {
		t.Run(name, func(t *testing.T) {
			if input[0] == '`' || name == "TypeScript U escape" {
				_, _, err := decodeTypeScriptString(input)
				require.Error(t, err)
				return
			}
			_, _, err := decodePythonString(input)
			require.Error(t, err)
		})
	}
}

func TestStringCodecsPreserveQuotesControlsAndMultilineText(t *testing.T) {
	assert.Equal(t, "\"\"\"line\n\\\"\\\"\\\"end\"\"\"", string(encodePythonString("line\n\"\"\"end", `"""`)))
	encoded, err := encodeTypeScriptString("line\n\t\r\x01\"", '"')
	require.NoError(t, err)
	assert.Equal(t, `"line\n\t\r\u0001\""`, string(encoded))
	_, err = encodeTypeScriptString("unsafe ${value}", '`')
	require.Error(t, err)

	for name, input := range map[string][]byte{
		"raw prefix":          []byte(`r"text"`),
		"byte prefix":         []byte(`b"text"`),
		"formatted prefix":    []byte(`f"text"`),
		"unknown prefix":      []byte(`q"text"`),
		"unterminated triple": []byte(`"""text"`),
		"short hex":           []byte(`"\x1"`),
		"invalid Unicode":     []byte(`"\uD800"`),
		"large Unicode":       []byte(`"\UFFFFFFFF"`),
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := decodePythonString(input)
			require.Error(t, err)
		})
	}
}

func TestPlaceholderTranslationRejectsAmbiguousForms(t *testing.T) {
	tests := []struct {
		name  string
		value string
		style placeholderStyle
		from  bool
	}{
		{name: "brace escaped", value: "{{name}}", style: bracePlaceholder},
		{name: "brace residue", value: "literal }", style: bracePlaceholder},
		{name: "source unclosed", value: "{name", style: bracePlaceholder},
		{name: "source bad name", value: "{1name}", style: bracePlaceholder},
		{name: "dollar unclosed", value: "${name", style: dollarPlaceholder},
		{name: "target residue", value: "literal {", style: bracePlaceholder, from: true},
		{name: "target unclosed", value: "{{name", style: bracePlaceholder, from: true},
		{name: "target bad name", value: "{{1name}}", style: bracePlaceholder, from: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var err error
			if test.from {
				_, err = placeholdersFromLaunchDarkly(test.value, test.style)
			} else {
				_, err = placeholdersToLaunchDarkly(test.value, test.style)
			}
			require.Error(t, err)
		})
	}

	node := stringNode("hello {name}", 0, 14, func(text string) ([]byte, error) { return []byte(text), nil })
	translated := translatePlaceholderNode(node, bracePlaceholder)
	_, err := translated.encode(1)
	require.Error(t, err)
	_, err = translated.encode("hello {{1name}}")
	require.Error(t, err)
	assert.Nil(t, translatePlaceholderNode(nil, bracePlaceholder))

	dynamic := &sourceNode{value: true}
	assert.Same(t, dynamic, translatePlaceholderNode(dynamic, bracePlaceholder))
	assert.Same(t, dynamic, translateMessagePlaceholders(dynamic, bracePlaceholder))
}

func TestPathParserRejectsEveryUnsupportedShape(t *testing.T) {
	paths := []string{
		`[id="one"]`,
		`$["key"]`,
		`items[id`,
		`items[id!`,
		`items[id="one"][name="two"]`,
		`items[id="one",name=two]`,
		`items[id="one",name]`,
		`items[id="one",="two"]`,
		`items[id="one",name="two"`,
		`items[id="\uD800"]`,
		`items[id="\uDC00"]`,
		`items[id="\uD800\u0041"]`,
		`items[id="bad\q"]`,
		`["unclosed]`,
		`items[id="one"] trailing`,
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			err := ParsePath(path)
			require.Error(t, err)
			problem, ok := syncdomain.ProblemFrom(err)
			require.True(t, ok)
			assert.Equal(t, syncdomain.ErrorInvalidPathSyntax, problem.Code)
		})
	}
	require.NoError(t, ParsePath(`items[id="\uD83D\uDE00"]`))
	assert.Error(t, pathSyntaxError("x", 10, "test"))
}

func TestSourcePathFailuresUseStableCodes(t *testing.T) {
	root := &sourceNode{fields: map[string]*sourceNode{
		"scalar": {value: "text"},
		"items": {items: []*sourceNode{
			{fields: map[string]*sourceNode{"id": {value: 10}}},
		}},
	}}
	tests := []struct {
		name, path string
		code       syncdomain.ErrorCode
	}{
		{name: "missing field", path: "missing", code: syncdomain.ErrorPathNotFound},
		{name: "field on scalar", path: "scalar.child", code: syncdomain.ErrorPathTypeMismatch},
		{name: "filter type", path: `items[id="10"]`, code: syncdomain.ErrorFilterTypeMismatch},
		{name: "no filter match", path: `items[name="missing"]`, code: syncdomain.ErrorPathNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := resolveSourcePath(root, test.path)
			problem, ok := syncdomain.ProblemFrom(err)
			require.True(t, ok)
			assert.Equal(t, test.code, problem.Code)
		})
	}
}

func TestProjectionValidationCoversEveryResourceKind(t *testing.T) {
	_, err := NormalizeReference(
		syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "tool"},
		syncdomain.Reference{File: "tool.json", Format: "json", Map: map[string]string{"prompt": "value"}},
	)
	problem, ok := syncdomain.ProblemFrom(err)
	require.True(t, ok)
	assert.Equal(t, syncdomain.ErrorInvalidReference, problem.Code)

	_, err = NormalizeReference(
		syncdomain.ResourceID{Kind: syncdomain.KindSkill, ProjectKey: "project", LookupKey: "skill"},
		syncdomain.Reference{File: "skill.toml", Format: "toml", Map: map[string]string{"schema": "value"}},
	)
	problem, ok = syncdomain.ProblemFrom(err)
	require.True(t, ok)
	assert.Equal(t, syncdomain.ErrorInvalidReference, problem.Code)
}

func TestUpdateSourceRejectsWrongProjectionAndMissingMap(t *testing.T) {
	id := syncdomain.VariationID("project", "config", "default")
	reference := syncdomain.Reference{
		File: "prompt.json", Format: "json", Map: map[string]string{"prompt": "missing"},
	}
	_, err := UpdateSource(
		sourceBundle(reference.File, []byte(`{"prompt":"Help"}`)),
		id, syncdomain.VariationModeAgent, reference,
		syncdomain.ToolProjection{},
	)
	require.ErrorContains(t, err, "projection kind")

	_, err = UpdateSource(
		sourceBundle(reference.File, []byte(`{"prompt":"Help"}`)),
		id, syncdomain.VariationModeAgent, reference,
		syncdomain.VariationProjection{Mode: syncdomain.VariationModeAgent, Instructions: stringPointerForReference("Next")},
	)
	problem, ok := syncdomain.ProblemFrom(err)
	require.True(t, ok)
	assert.Equal(t, syncdomain.ErrorPathNotFound, problem.Code)
}

func TestBuildProjectionAcceptsAllMappedFieldTypes(t *testing.T) {
	selected := &sourceNode{fields: map[string]*sourceNode{
		"prompt":      {value: "Help"},
		"messages":    {value: []syncdomain.Message{{Role: syncdomain.RoleSystem, Content: "Help"}}},
		"name":        {value: "assistant"},
		"model":       {value: "model"},
		"temperature": {value: int64(1)},
		"tools":       {value: []any{"search", "read"}},
		"skills":      {value: []string{"review"}},
	}}
	fieldMap := map[string]string{
		string(syncdomain.FieldPrompt):    "prompt",
		string(syncdomain.FieldName):      "name",
		string(syncdomain.FieldModel):     "model",
		`model.parameters["temperature"]`: "temperature",
		string(syncdomain.FieldTools):     "tools",
		string(syncdomain.FieldSkills):    "skills",
	}
	projection, err := buildProjection(syncdomain.KindVariation, "", selected, fieldMap)
	require.NoError(t, err)
	variation := projection.(syncdomain.VariationProjection)
	assert.Equal(t, syncdomain.VariationModeAgent, variation.Mode)
	assert.Equal(t, 1.0, (*variation.Parameters)["temperature"])
	assert.Equal(t, []string{"search", "read"}, *variation.Tools)

	projection, err = buildProjection(
		syncdomain.KindVariation, syncdomain.VariationModeCompletion, selected,
		map[string]string{string(syncdomain.FieldMessages): "messages"},
	)
	require.NoError(t, err)
	assert.NotNil(t, projection.(syncdomain.VariationProjection).Messages)

	selected = &sourceNode{fields: map[string]*sourceNode{
		"description": {value: "Search"},
		"schema":      {value: map[string]any{"type": "object"}},
		"parameters":  {value: map[string]any{"limit": 10}},
		"tags":        {value: []any{"search", "safe"}},
	}}
	projection, err = buildProjection(syncdomain.KindTool, "", selected, map[string]string{
		string(syncdomain.FieldDescription):      "description",
		string(syncdomain.FieldSchema):           "schema",
		string(syncdomain.FieldCustomParameters): "parameters",
		string(syncdomain.FieldTags):             "tags",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"search", "safe"}, *projection.(syncdomain.ToolProjection).Tags)

	selected = &sourceNode{fields: map[string]*sourceNode{
		"description": {value: "Review"},
		"markdown":    {value: "# Review"},
	}}
	projection, err = buildProjection(syncdomain.KindSkill, "", selected, map[string]string{
		string(syncdomain.FieldDescription): "description",
		string(syncdomain.FieldMarkdown):    "markdown",
	})
	require.NoError(t, err)
	assert.Equal(t, "# Review", *projection.(syncdomain.SkillProjection).Markdown)
}

func TestBuildProjectionRejectsInvalidMappedFieldTypes(t *testing.T) {
	tests := []struct {
		name  string
		kind  syncdomain.Kind
		mode  syncdomain.VariationMode
		field syncdomain.Field
		value any
	}{
		{name: "agent prompt", kind: syncdomain.KindVariation, field: syncdomain.FieldPrompt, value: true},
		{name: "messages", kind: syncdomain.KindVariation, mode: syncdomain.VariationModeCompletion, field: syncdomain.FieldMessages, value: true},
		{name: "message role", kind: syncdomain.KindVariation, mode: syncdomain.VariationModeCompletion, field: syncdomain.FieldMessages, value: []syncdomain.Message{{Role: "invalid"}}},
		{name: "name", kind: syncdomain.KindVariation, field: syncdomain.FieldName, value: true},
		{name: "model", kind: syncdomain.KindVariation, field: syncdomain.FieldModel, value: true},
		{name: "parameters", kind: syncdomain.KindVariation, field: syncdomain.FieldModelParameters, value: "hot"},
		{name: "tools", kind: syncdomain.KindVariation, field: syncdomain.FieldTools, value: []any{"ok", 1}},
		{name: "skills", kind: syncdomain.KindVariation, field: syncdomain.FieldSkills, value: true},
		{name: "description", kind: syncdomain.KindTool, field: syncdomain.FieldDescription, value: true},
		{name: "schema", kind: syncdomain.KindTool, field: syncdomain.FieldSchema, value: true},
		{name: "parameters", kind: syncdomain.KindTool, field: syncdomain.FieldCustomParameters, value: true},
		{name: "tags", kind: syncdomain.KindTool, field: syncdomain.FieldTags, value: []any{"ok", 1}},
		{name: "skill", kind: syncdomain.KindSkill, field: syncdomain.FieldMarkdown, value: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			selected := &sourceNode{fields: map[string]*sourceNode{
				string(test.field): {value: test.value},
			}}
			fieldMap := map[string]string{string(test.field): string(test.field)}
			if test.kind == syncdomain.KindVariation && test.field != syncdomain.FieldPrompt && test.field != syncdomain.FieldMessages {
				selected.fields["prompt"] = &sourceNode{value: "Help"}
				fieldMap[string(syncdomain.FieldPrompt)] = "prompt"
			}
			_, err := buildProjection(test.kind, test.mode, selected, fieldMap)
			require.Error(t, err)
		})
	}

	_, err := buildProjection(syncdomain.KindVariation, "", &sourceNode{}, map[string]string{})
	require.Error(t, err)
	_, err = buildProjection(syncdomain.Kind("unknown"), "", &sourceNode{}, nil)
	require.Error(t, err)
}

func TestProjectionFieldReadsEveryOptionalField(t *testing.T) {
	text := "value"
	list := []string{"one"}
	messages := []syncdomain.Message{{Role: syncdomain.RoleSystem, Content: "Help"}}
	object := map[string]any{"type": "object"}
	parameters := map[string]any{"temperature": 0.5}
	projections := []syncdomain.Projection{
		syncdomain.VariationProjection{
			Instructions: &text, Name: &text, Model: &text, Parameters: &parameters, Tools: &list, Skills: &list,
		},
		syncdomain.VariationProjection{Messages: &messages},
		syncdomain.ToolProjection{
			Description: &text, Schema: &object, CustomParameters: &object, Tags: &list,
		},
		syncdomain.SkillProjection{Description: &text, Markdown: &text},
	}
	fields := [][]syncdomain.Field{
		{syncdomain.FieldPrompt, syncdomain.FieldName, syncdomain.FieldModel, syncdomain.FieldModelParameters, syncdomain.FieldTools, syncdomain.FieldSkills},
		{syncdomain.FieldMessages},
		{syncdomain.FieldDescription, syncdomain.FieldSchema, syncdomain.FieldCustomParameters, syncdomain.FieldTags},
		{syncdomain.FieldDescription, syncdomain.FieldMarkdown},
	}
	for index, projection := range projections {
		for _, field := range fields[index] {
			_, present, err := projectionField(projection, field)
			require.NoError(t, err)
			assert.True(t, present)
		}
	}
}

func TestMessageUpdateSafetyBranches(t *testing.T) {
	have := []syncdomain.Message{{Role: syncdomain.RoleSystem, Content: "old"}}
	tests := []struct {
		name string
		node *sourceNode
		want []syncdomain.Message
	}{
		{name: "different count", node: &sourceNode{}, want: nil},
		{name: "different role", node: &sourceNode{}, want: []syncdomain.Message{{Role: syncdomain.RoleUser, Content: "old"}}},
		{name: "missing content", node: &sourceNode{}, want: []syncdomain.Message{{Role: syncdomain.RoleSystem, Content: "new"}}},
		{name: "read only content", node: &sourceNode{items: []*sourceNode{{fields: map[string]*sourceNode{"content": {reason: "dynamic"}}}}}, want: []syncdomain.Message{{Role: syncdomain.RoleSystem, Content: "new"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			update := updateMessageContents(SourceBundle{}, test.node, have, test.want)
			assert.Equal(t, CapabilityReadOnly, update.Capability)
		})
	}

	content := stringNode("old", 0, 5, func(string) ([]byte, error) { return []byte(`"new"`), nil })
	content.file = "missing.py"
	update := updateMessageContents(
		SourceBundle{Primary: Document{Path: "main.py"}},
		&sourceNode{items: []*sourceNode{{fields: map[string]*sourceNode{"content": content}}}},
		have, []syncdomain.Message{{Role: syncdomain.RoleSystem, Content: "new"}},
	)
	assert.Equal(t, CapabilityReadOnly, update.Capability)
}

func TestSourceValueHelpersRejectDynamicValues(t *testing.T) {
	assert.False(t, semanticEqual(make(chan int), make(chan int)))
	_, ok := sourceNumber(math.NaN())
	assert.True(t, ok)
	_, ok = sourceNumber("1")
	assert.False(t, ok)
	_, ok = stringList(true)
	assert.False(t, ok)
	_, ok = objectValue(true)
	assert.False(t, ok)

	stringValue := stringNode("old", 0, 3, func(value string) ([]byte, error) { return []byte(value), nil })
	_, err := stringValue.encode(1)
	require.Error(t, err)
	numberValue := numberNode(1, 0, 1, func(float64) ([]byte, error) { return []byte("1"), nil })
	_, err = numberValue.encode("one")
	require.Error(t, err)
}

func TestPythonInspectorHandlesStaticAndDynamicLanguageForms(t *testing.T) {
	source := []byte(`
from langchain_core.prompts import PromptTemplate, ChatPromptTemplate
from langchain_core.tools import tool
from fastmcp import FastMCP
from .values import TEXT, MISSING as missing

TRUE = True
FALSE = False
NUMBER = 1.5
TUPLE = ("system", TEXT)
OBJECT = {"text": TEXT, 1: "number", **{"extra": "value"}}
DYNAMIC = [TEXT, make_value()]
MUTATED = ["one"]
MUTATED.append("two")
MUTATED[0] = "changed"

empty = PromptTemplate.from_template()
chat = ChatPromptTemplate.from_messages([("human", TEXT), ("ai", "Done"), {"role": "system"}])

@tool
def search(query: str):
    """Search docs."""
    return query

mcp = FastMCP("server")
@mcp.tool(description="Run a task")
def run_task(value: str):
    """Run."""
    return value

@mcp.prompt()
def dynamic_prompt(name: str):
    """Prompt docs."""
    return f"Hello {name}"

class Scope:
    VALUE = "scoped"
`)
	root, err := parsePythonBundle(SourceBundle{
		Primary: Document{Path: "src/main.py", Data: source},
		Dependencies: map[string]Document{
			"src/values.py": {Path: "src/values.py", Data: []byte(`TEXT = "Hello {name}"`)},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, root)
	assert.NotNil(t, root.fields["tools"])
	assert.NotNil(t, root.fields["prompts"])

	dependencies, err := pythonAdapter{}.Dependencies(Document{Path: "src/main.py", Data: source})
	require.NoError(t, err)
	assert.Contains(t, dependencies, "src/values.py")
}

func TestTypeScriptInspectorHandlesStaticAndDynamicLanguageForms(t *testing.T) {
	source := []byte(`
import { PromptTemplate, ChatPromptTemplate } from "@langchain/core/prompts";
import { tool } from "ai";
import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import textDefault, { TEXT, MISSING as missing } from "./values";
import * as values from "./values";
import external from "third-party";

const TRUE = true;
const FALSE = false;
const NOTHING = null;
const NUMBER = 1.5;
const OBJECT = { text: TEXT, "literal.key": "value", [TEXT]: "computed", 1: "number", ...values };
const DUPLICATE = { name: "one", name: "two" };
const DYNAMIC = [TEXT, makeValue()];
const MUTATED = ["one"];
MUTATED.push("two");
MUTATED[0] = "changed";

const empty = PromptTemplate.fromTemplate();
const chat = ChatPromptTemplate.fromMessages([["human", TEXT], { role: "ai", content: "Done" }, {}]);
const tools = { search: tool({ description: "Search", parameters: { type: "object" } }), dynamic: other };

const server = new Server({});
server.registerTool("lookup", { description: TEXT, inputSchema: { type: "object" } });
server.registerTool(dynamicName, {});
`)
	root, err := parseTypeScriptBundle(SourceBundle{
		Primary: Document{Path: "src/main.ts", Data: source},
		Dependencies: map[string]Document{
			"src/values.ts": {Path: "src/values.ts", Data: []byte(`export const TEXT = "Hello ${name}";`)},
		},
	}, true)
	require.NoError(t, err)
	require.NotNil(t, root)
	assert.NotNil(t, root.fields["tools"])
	assert.NotNil(t, root.fields["prompts"])

	dependencies, err := (ecmaScriptAdapter{format: "typescript", typed: true}).Dependencies(
		Document{Path: "src/main.ts", Data: source},
	)
	require.NoError(t, err)
	assert.Contains(t, dependencies, "src/values.ts")

	jsRoot, err := parseTypeScriptBundle(SourceBundle{
		Primary: Document{Path: "src/main.js", Data: []byte(`const value = {enabled: false};`)},
	}, false)
	require.NoError(t, err)
	require.NotNil(t, jsRoot)
}

func TestJSONAdapterRejectsMalformedSources(t *testing.T) {
	inputs := []string{
		"",
		`{"key" "value"}`,
		`{"key": true "next": false}`,
		`{"key": 1, "key": 2}`,
		`[1 2]`,
		`{"key":}`,
		`{"key":"unterminated}`,
		"{\"key\":\"control\x01\"}",
		`truth`,
		`+`,
		`1e`,
		`true trailing`,
	}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			_, err := parseJSONDocument(Document{Path: "value.json", Data: []byte(input)})
			require.Error(t, err)
		})
	}
	_, err := parseJSONDocument(Document{Path: "value.json", Data: []byte{0xff}})
	require.Error(t, err)

	root, err := parseJSONDocument(Document{Path: "value.json", Data: []byte("\ufeff {\"empty\":{},\"array\":[],\"false\":false,\"null\":null}")})
	require.NoError(t, err)
	assert.Equal(t, false, root.fields["false"].value)
}

func TestYAMLAdapterHandlesSafetyForms(t *testing.T) {
	for name, input := range map[string][]byte{
		"invalid UTF-8":  {0xff},
		"invalid syntax": []byte("value: ["),
		"empty":          nil,
		"non-string key": []byte("1: value"),
		"duplicate":      []byte("key: one\nkey: two\n"),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseYAMLDocument(Document{Path: "value.yaml", Data: input})
			require.Error(t, err)
		})
	}

	root, err := parseYAMLDocument(Document{Path: "value.yaml", Data: []byte(`
defaults: &defaults
  enabled: true
  count: 1_000
copy:
  <<: *defaults
tagged: !custom value
block: |
  content
sequence: &items [one, two]
null_value: null
---
second: document
`)})
	require.NoError(t, err)
	assert.Equal(t, CapabilityReadOnly, root.capability)
	assert.Equal(t, CapabilityReadOnly, root.fields["copy"].capability)
	assert.Equal(t, CapabilityReadOnly, root.fields["tagged"].capability)
}

func TestTOMLAdapterHandlesSafetyForms(t *testing.T) {
	for name, input := range map[string][]byte{
		"invalid UTF-8":  {0xff},
		"invalid syntax": []byte("value = ["),
		"duplicate":      []byte("value = 1\nvalue = 2\n"),
		"value as table": []byte("value = 1\n[value]\nchild = 2\n"),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseTOMLDocument(Document{Path: "value.toml", Data: input})
			require.Error(t, err)
		})
	}

	root, err := parseTOMLDocument(Document{Path: "value.toml", Data: []byte("\ufeff" + `
basic = "text"
literal = 'text'
multiline_basic = """text"""
multiline_literal = '''text'''
enabled = true
integer = 10
float = 1.5
date = 2026-10-30
array = [1, # comment
  2]
inline = { name = "value" }
[[tools]]
name = "one"
[[tools]]
name = "two"
`)})
	require.NoError(t, err)
	assert.Equal(t, "2026-10-30", root.fields["date"].value)
	assert.Len(t, root.fields["tools"].items, 2)

	assert.Equal(t, `"""next\"""value"""`, mustTOMLString(t, `next"""value`, `"""old"""`))
	assert.Equal(t, "'''next'''", mustTOMLString(t, "next", "'''old'''"))
	_, err = encodeTOMLString("bad'''value", "'''old'''")
	require.Error(t, err)
	_, err = encodeTOMLString("bad\nvalue", "'old'")
	require.Error(t, err)
	encoded := encodeTOMLBasicString("\b\t\n\f\r\x01\u0080\"\\")
	assert.True(t, strings.HasPrefix(encoded, `"`))
}

func TestMarkdownProfilesRejectUnsafeMarkersAndFrontMatter(t *testing.T) {
	inputs := []string{
		"<!-- launchdarkly:begin one -->\n<!-- launchdarkly:begin two -->",
		"<!-- launchdarkly:end one -->",
		"<!-- launchdarkly:begin one -->",
	}
	for _, input := range inputs {
		_, err := parseMarkdownProfile(Document{Path: "AGENTS.md", Data: []byte(input)}, false)
		require.Error(t, err)
	}
	_, err := parseMarkdownProfile(Document{Path: "rule.md", Data: []byte("---\ninvalid: [\n---\nbody")}, true)
	require.Error(t, err)

	root, err := parseMarkdownProfile(Document{Path: "AGENTS.md", Data: []byte("\ufeff# Instructions")}, false)
	require.NoError(t, err)
	assert.Equal(t, "# Instructions", root.fields["body"].value)
}

func TestMarkdownAndAgentSkillSafetyForms(t *testing.T) {
	selectPath, fieldMap := markdownAdapter{}.Defaults(syncdomain.KindTool)
	assert.Equal(t, "$", selectPath)
	assert.Nil(t, fieldMap)
	selectPath, fieldMap = agentSkillAdapter{}.Defaults(syncdomain.KindVariation)
	assert.Equal(t, "$", selectPath)
	assert.Nil(t, fieldMap)

	_, err := parseMarkdown(Document{Path: "README.md", Data: []byte{0xff}})
	require.Error(t, err)
	_, err = parseAgentSkill(Document{Path: "SKILL.md", Data: []byte{0xff}})
	require.Error(t, err)

	for name, input := range map[string]string{
		"missing front matter":  "# Body",
		"unclosed front matter": "---\ndescription: value",
		"invalid YAML":          "---\ndescription: [\n---\nBody",
		"front matter scalar":   "---\nvalue\n---\nBody",
		"missing description":   "---\nname: skill\n---\nBody",
		"description type":      "---\ndescription: [value]\n---\nBody",
		"empty body":            "---\ndescription: value\n---\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseAgentSkill(Document{Path: "SKILL.md", Data: []byte(input)})
			require.Error(t, err)
		})
	}

	root, err := parseAgentSkill(Document{Path: "SKILL.md", Data: []byte("---\ndescription: |\n  long text\n---\nBody\n")})
	require.NoError(t, err)
	assert.Equal(t, CapabilityReadOnly, root.fields["frontmatter"].fields["description"].capability)

	sections := markdownSections([]byte("    # indented\n````\n# hidden\n````\n## Visible ##\nbody\n"))
	require.Len(t, sections, 1)
	assert.Equal(t, "Visible", sections[0].fields["heading"].value)

	_, _, _, err = yamlScalarSpan(frontMatterRegion{}, &yaml.Node{})
	require.Error(t, err)
	assert.Equal(t, 0, yamlTokenEnd(nil, 0))
	assert.Equal(t, `"line\nnext"`, quoteYAML("line\nnext", "'old'"))
	assert.Equal(t, "'it''s'", quoteYAML("it's", "'old'"))
	_, err = encodeYAMLString("bad\x01", "old")
	require.Error(t, err)

	exact := exactMarkdownNode([]byte("body\n"), 0, 5)
	encoded, err := exact.encode("next")
	require.NoError(t, err)
	assert.Equal(t, "next\n", string(encoded))

	region, bodyStart, err := splitFrontMatter([]byte("\ufeff---\r\ndescription: value\r\n---\r\n\r\nBody"))
	require.NoError(t, err)
	assert.NotEmpty(t, region.data)
	assert.Greater(t, bodyStart, 0)
	assert.Equal(t, len(`"unterminated`), yamlTokenEnd([]byte(`"unterminated`), 0))
	assert.Equal(t, len(`'it''s'`), yamlTokenEnd([]byte(`'it''s'`), 0))
	assert.Equal(t, 5, yamlTokenEnd([]byte("value, next"), 0))
}

func TestRegistryContractAndDependencyErrors(t *testing.T) {
	assert.Nil(t, NewInspection(nil).root)
	inspection := NewInspection(&Node{
		Value:  "root",
		Fields: map[string]*Node{"child": {Value: "value"}},
		Items:  []*Node{{Value: "item"}},
	})
	require.Equal(t, "value", inspection.root.fields["child"].value)
	require.Len(t, inspection.root.items, 1)

	require.Error(t, Register(nil))
	require.Error(t, Register(contractAdapter{}))
	require.Error(t, Register(jsonAdapter{}))

	_, err := registeredAdapter("missing-format")
	require.Error(t, err)
	dependencies, err := DependencyPaths("json", Document{})
	require.NoError(t, err)
	assert.Nil(t, dependencies)
	_, err = DependencyPaths("missing-format", Document{})
	require.Error(t, err)
}

func TestCodeParsersRejectUnsafeInput(t *testing.T) {
	_, err := pythonAdapter{}.Dependencies(Document{Path: "main.py", Data: []byte{0xff}})
	require.Error(t, err)
	_, err = pythonAdapter{}.Dependencies(Document{Path: "main.py", Data: make([]byte, maxSourceBytes+1)})
	require.Error(t, err)
	_, err = pythonAdapter{}.Dependencies(Document{Path: "main.py", Data: []byte("def broken(")})
	require.Error(t, err)

	result := messageListFromCollection(&sourceNode{items: []*sourceNode{
		{items: []*sourceNode{{value: "human"}, {value: "Hello"}}},
		{fields: map[string]*sourceNode{"role": {value: "system"}, "content": {value: true}}},
	}})
	assert.Equal(t, CapabilityReadOnly, result.capability)
	assert.Nil(t, messageListFromCollection(nil))

	_, _, err = decodeTypeScriptString([]byte("xx"))
	require.Error(t, err)
	_, _, err = decodeTypeScriptString([]byte(`"x'`))
	require.Error(t, err)
	_, err = decodeEscapes("\\\nnext", false)
	require.NoError(t, err)
	_, err = decodeEscapes(`\q`, false)
	require.Error(t, err)
}

func TestPythonValueSafetyBranches(t *testing.T) {
	document := Document{Path: "main.py", Data: []byte(`
TRUE = True
FALSE = False
OVERFLOW = 1e9999
DICT = {"static": "value", "dynamic": make_value()}
LIST = ["value", make_value()]
message = AIMessage(content="done")
later = UNKNOWN
`)}
	tree, err := parseTree(document, sitter.NewLanguage(pythonlanguage.Language()))
	require.NoError(t, err)
	defer tree.Close()

	trueNode := findNodeByText(tree.RootNode(), document.Data, "true", "True")
	require.NotNil(t, trueNode)
	value := pythonValue(trueNode, document.Data, nil)
	encoded, err := value.encode(false)
	require.NoError(t, err)
	assert.Equal(t, "False", string(encoded))
	_, err = value.encode("false")
	require.Error(t, err)

	overflow := findNodeByText(tree.RootNode(), document.Data, "float", "1e9999")
	require.NotNil(t, overflow)
	assert.Equal(t, CapabilityReadOnly, pythonValue(overflow, document.Data, nil).capability)

	dictionary := findNodeByText(tree.RootNode(), document.Data, "dictionary", `{"static": "value", "dynamic": make_value()}`)
	require.NotNil(t, dictionary)
	assert.Equal(t, CapabilityReadOnly, pythonDictionary(dictionary, document.Data, nil).capability)
	list := findNodeByText(tree.RootNode(), document.Data, "list", `["value", make_value()]`)
	require.NotNil(t, list)
	assert.Equal(t, CapabilityReadOnly, pythonSequence(list, document.Data, nil).capability)

	call := findNodeByText(tree.RootNode(), document.Data, "call", `AIMessage(content="done")`)
	require.NotNil(t, call)
	message := pythonMessage(call, document.Data, nil)
	assert.Equal(t, syncdomain.RoleAssistant, message.fields["role"].value)
	assert.Equal(t, "done", message.fields["content"].value)

	identifier := findNodeByText(tree.RootNode(), document.Data, "identifier", "UNKNOWN")
	require.NotNil(t, identifier)
	wildcard := &sourceNode{reason: "unresolved", code: syncdomain.ErrorImportUnresolved}
	resolved := pythonValue(identifier, document.Data, map[string]*sourceNode{"*": wildcard})
	assert.Contains(t, resolved.reason, "star import")

	assert.Equal(t, "module.py", pythonModulePaths("main.py", "module")[0])
	assert.Empty(t, pythonMutationRoot(nil, nil))
	assert.Equal(t, CapabilityReadOnly, pythonMessages(nil, nil, nil).capability)
}

func TestTypeScriptValueSafetyBranches(t *testing.T) {
	document := Document{Path: "main.ts", Data: []byte(`
const TRUE = true;
const OVERFLOW = 1e9999;
const TEMPLATE = ` + "`hello ${name}`" + `;
const OBJECT = { "name": "one", "name": "two", ...dynamic };
const DYNAMIC_OBJECT = { static: "value", ...dynamic };
const ARRAY = ["value", makeValue()];
const MESSAGE = {};
const NAMESPACE_VALUE = values.TEXT;
const IDENTIFIER_VALUE = STATIC_VALUE;
`)}
	tree, err := parseTree(document, sitter.NewLanguage(typescriptlanguage.LanguageTypescript()))
	require.NoError(t, err)
	defer tree.Close()

	trueNode := findNodeByText(tree.RootNode(), document.Data, "true", "true")
	require.NotNil(t, trueNode)
	value := typeScriptValue(trueNode, document.Data, nil)
	encoded, err := value.encode(false)
	require.NoError(t, err)
	assert.Equal(t, "false", string(encoded))
	_, err = value.encode("false")
	require.Error(t, err)

	overflow := findNodeByText(tree.RootNode(), document.Data, "number", "1e9999")
	require.NotNil(t, overflow)
	assert.Equal(t, CapabilityReadOnly, typeScriptValue(overflow, document.Data, nil).capability)
	template := findNodeByText(tree.RootNode(), document.Data, "template_string", "`hello ${name}`")
	require.NotNil(t, template)
	assert.Equal(t, CapabilityReadOnly, typeScriptValue(template, document.Data, nil).capability)

	object := findNodeByText(tree.RootNode(), document.Data, "object", `{ "name": "one", "name": "two", ...dynamic }`)
	require.NotNil(t, object)
	assert.Equal(t, CapabilityReadOnly, typeScriptObject(object, document.Data, nil).capability)
	dynamicObject := findNodeByText(tree.RootNode(), document.Data, "object", `{ static: "value", ...dynamic }`)
	require.NotNil(t, dynamicObject)
	assert.Equal(t, CapabilityReadOnly, typeScriptObject(dynamicObject, document.Data, nil).capability)
	array := findNodeByText(tree.RootNode(), document.Data, "array", `["value", makeValue()]`)
	require.NotNil(t, array)
	assert.Equal(t, CapabilityReadOnly, typeScriptValue(array, document.Data, nil).capability)

	member := findNodeByText(tree.RootNode(), document.Data, "member_expression", `values.TEXT`)
	require.NotNil(t, member)
	namespace := &sourceNode{fields: map[string]*sourceNode{"TEXT": {value: "resolved"}}}
	assert.Equal(t, "resolved", typeScriptValue(member, document.Data, map[string]*sourceNode{"values": namespace}).value)
	identifier := findNodeByText(tree.RootNode(), document.Data, "identifier", "STATIC_VALUE")
	require.NotNil(t, identifier)
	assert.Equal(t, "resolved", typeScriptValue(identifier, document.Data, map[string]*sourceNode{
		"STATIC_VALUE": {value: "resolved", end: 1},
	}).value)

	assert.Equal(t, CapabilityReadOnly, typeScriptObject(nil, nil, nil).capability)
	assert.Equal(t, CapabilityReadOnly, typeScriptMessages(nil, nil, nil).capability)
	assert.Equal(t, CapabilityReadOnly, typeScriptMessages(object, document.Data, nil).capability)
	messageObject := findNodeByText(tree.RootNode(), document.Data, "object", `{}`)
	require.NotNil(t, messageObject)
	message := typeScriptMessage(messageObject, document.Data, nil)
	assert.Equal(t, CapabilityReadOnly, message.fields["role"].capability)
	assert.Equal(t, CapabilityReadOnly, message.fields["content"].capability)
	_, ok := typeScriptPropertyName(nil, nil)
	assert.False(t, ok)
	assert.Empty(t, typeScriptMutationRoot(nil, nil))
}

func TestSourceEntryPointsPreserveStructuredFailures(t *testing.T) {
	id := syncdomain.VariationID("project", "config", "default")
	_, err := NormalizeReference(id, syncdomain.Reference{
		File: "value.txt", Format: "missing", Select: "$",
		Map: map[string]string{string(syncdomain.FieldPrompt): "$"},
	})
	require.Error(t, err)
	_, err = NormalizeReference(id, syncdomain.Reference{
		File: "value.json", Format: "json", Select: "$", Map: map[string]string{},
	})
	require.Error(t, err)
	_, err = NormalizeReference(id, syncdomain.Reference{
		File: "value.json", Format: "json", Select: "$",
		Map: map[string]string{string(syncdomain.FieldPrompt): "invalid["},
	})
	require.Error(t, err)
	_, err = NormalizeReference(id, syncdomain.Reference{
		File: "main.py", Format: "python",
		Map: map[string]string{string(syncdomain.FieldPrompt): "template"},
	})
	require.Error(t, err)
	_, err = ParseSource(SourceBundle{}, id, syncdomain.VariationModeAgent, syncdomain.Reference{
		File: "value.json", Format: "missing", Select: "$",
		Map: map[string]string{string(syncdomain.FieldPrompt): "$"},
	})
	require.Error(t, err)
	_, err = ParseSource(sourceBundle("value.json", []byte(`{"prompt":"Help"}`)), id, syncdomain.VariationModeAgent, syncdomain.Reference{
		File: "value.json", Format: "json", Select: "missing",
		Map: map[string]string{string(syncdomain.FieldPrompt): "prompt"},
	})
	require.Error(t, err)
	_, err = ParseSource(sourceBundle("value.json", []byte(`{"prompt":`)), id, syncdomain.VariationModeAgent, syncdomain.Reference{
		File: "value.json", Format: "json", Select: "$",
		Map: map[string]string{string(syncdomain.FieldPrompt): "prompt"},
	})
	require.Error(t, err)
	_, err = UpdateSource(
		SourceBundle{}, id, syncdomain.VariationModeAgent,
		syncdomain.Reference{
			File: "value.json", Format: "missing", Select: "$",
			Map: map[string]string{string(syncdomain.FieldPrompt): "$"},
		},
		syncdomain.VariationProjection{},
	)
	require.Error(t, err)
	_, err = UpdateSource(
		sourceBundle("value.json", []byte(`{"prompt":`)), id, syncdomain.VariationModeAgent,
		syncdomain.Reference{
			File: "value.json", Format: "json", Select: "$",
			Map: map[string]string{string(syncdomain.FieldPrompt): "prompt"},
		},
		syncdomain.VariationProjection{},
	)
	require.Error(t, err)
	_, err = UpdateSource(
		sourceBundle("value.json", []byte(`{"prompt":"Help"}`)), id, syncdomain.VariationModeAgent,
		syncdomain.Reference{
			File: "value.json", Format: "json", Select: "missing",
			Map: map[string]string{string(syncdomain.FieldPrompt): "prompt"},
		},
		syncdomain.VariationProjection{},
	)
	require.Error(t, err)

	messages := []syncdomain.Message{
		{Role: syncdomain.RoleSystem, Content: "one"},
		{Role: syncdomain.RoleUser, Content: "two"},
	}
	result, err := UpdateSource(
		sourceBundle("value.json", []byte(`{"prompt":"Help"}`)),
		id, syncdomain.VariationModeAgent,
		syncdomain.Reference{
			File: "value.json", Format: "json", Select: "$",
			Map: map[string]string{string(syncdomain.FieldPrompt): "prompt"},
		},
		syncdomain.VariationProjection{Messages: &messages},
	)
	require.NoError(t, err)
	assert.Equal(t, CapabilityReadOnly, result.Fields[syncdomain.FieldPrompt].Capability)

	assert.False(t, fieldAllowed(syncdomain.Kind("unknown"), syncdomain.FieldPrompt))
	_, err = parseSourceDocument(Document{}, "missing")
	require.Error(t, err)
	number, ok := sourceNumber(1)
	assert.True(t, ok)
	assert.Equal(t, 1.0, number)

	content := stringNode("old", 0, 3, func(string) ([]byte, error) { return nil, assert.AnError })
	update := updateMessageContents(
		sourceBundle("value.json", []byte("old")),
		&sourceNode{items: []*sourceNode{{fields: map[string]*sourceNode{"content": content}}}},
		[]syncdomain.Message{{Role: syncdomain.RoleSystem, Content: "old"}},
		[]syncdomain.Message{{Role: syncdomain.RoleSystem, Content: "new"}},
	)
	assert.Equal(t, CapabilityReadOnly, update.Capability)
}

func TestTypeScriptMCPServerRegistrationBranches(t *testing.T) {
	source := []byte(`
import { McpServer } from "@modelcontextprotocol/sdk";
const server = new McpServer({});
server.registerTool();
server.registerTool(dynamicName, {});
server.registerTool("lookup", { description: "Find data" });
`)
	root, err := parseTypeScriptBundle(SourceBundle{Primary: Document{Path: "server.ts", Data: source}}, true)
	require.NoError(t, err)
	require.Len(t, root.fields["tools"].items, 1)
	assert.Equal(t, "lookup", root.fields["tools"].items[0].fields["id"].value)
}

func findNodeByText(root *sitter.Node, source []byte, kind, text string) *sitter.Node {
	var found *sitter.Node
	walkTree(root, func(node *sitter.Node) {
		if found == nil && node.Kind() == kind && nodeText(node, source) == text {
			found = node
		}
	})
	return found
}

func mustTOMLString(t *testing.T, value, previous string) string {
	t.Helper()
	encoded, err := encodeTOMLString(value, previous)
	require.NoError(t, err)
	return string(encoded)
}

type contractAdapter struct{}

func (contractAdapter) Format() string { return "" }
func (contractAdapter) Inspect(SourceBundle) (Inspection, error) {
	return NewInspection(nil), nil
}
func (contractAdapter) Defaults(syncdomain.Kind) (string, map[string]string) { return "$", nil }

func stringPointerForReference(value string) *string { return &value }
