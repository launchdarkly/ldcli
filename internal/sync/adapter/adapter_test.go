package adapter

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

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
	root := &Node{Fields: map[string]*Node{
		"scalar": {Value: "text"},
		"items": {Items: []*Node{
			{Fields: map[string]*Node{"id": {Value: 10}}},
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
			_, err := root.Resolve(test.path)
			problem, ok := syncdomain.ProblemFrom(err)
			require.True(t, ok)
			assert.Equal(t, test.code, problem.Code)
		})
	}
}

func TestBuildProjectionAcceptsAllMappedFieldTypes(t *testing.T) {
	selected := &Node{Fields: map[string]*Node{
		"prompt":      {Value: "Help"},
		"messages":    {Value: []syncdomain.Message{{Role: syncdomain.RoleSystem, Content: "Help"}}},
		"name":        {Value: "assistant"},
		"model":       {Value: "model"},
		"temperature": {Value: int64(1)},
	}}
	fieldMap := map[string]string{
		string(syncdomain.FieldPrompt):    "prompt",
		string(syncdomain.FieldName):      "name",
		string(syncdomain.FieldModel):     "model",
		`model.parameters["temperature"]`: "temperature",
	}
	projection, err := buildProjection(syncdomain.KindVariation, selected, fieldMap)
	require.NoError(t, err)
	variation := projection.(syncdomain.VariationProjection)
	assert.Equal(t, syncdomain.VariationModeAgent, variation.Mode)
	assert.Equal(t, 1.0, (*variation.Parameters)["temperature"])

	projection, err = buildProjection(
		syncdomain.KindVariation, selected,
		map[string]string{string(syncdomain.FieldMessages): "messages"},
	)
	require.NoError(t, err)
	assert.NotNil(t, projection.(syncdomain.VariationProjection).Messages)

	_, err = buildProjection(syncdomain.KindVariation, selected, map[string]string{
		string(syncdomain.FieldPrompt): "prompt", string(syncdomain.FieldMessages): "messages",
	})
	require.ErrorContains(t, err, "cannot both be set")
	_, err = buildProjection(syncdomain.KindVariation, selected, map[string]string{string(syncdomain.FieldName): "name"})
	require.ErrorContains(t, err, "is required for a variation")

	selected = &Node{Fields: map[string]*Node{
		"description": {Value: "Search"},
		"schema":      {Value: map[string]any{"type": "object"}},
		"parameters":  {Value: map[string]any{"limit": 10}},
		"tags":        {Value: []any{"search", "safe"}},
	}}
	projection, err = buildProjection(syncdomain.KindTool, selected, map[string]string{
		string(syncdomain.FieldDescription):      "description",
		string(syncdomain.FieldSchema):           "schema",
		string(syncdomain.FieldCustomParameters): "parameters",
		string(syncdomain.FieldTags):             "tags",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"search", "safe"}, *projection.(syncdomain.ToolProjection).Tags)

	selected = &Node{Fields: map[string]*Node{
		"description": {Value: "Review"},
		"markdown":    {Value: "# Review"},
	}}
	projection, err = buildProjection(syncdomain.KindSkill, selected, map[string]string{
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
		{name: "description", kind: syncdomain.KindTool, field: syncdomain.FieldDescription, value: true},
		{name: "schema", kind: syncdomain.KindTool, field: syncdomain.FieldSchema, value: true},
		{name: "parameters", kind: syncdomain.KindTool, field: syncdomain.FieldCustomParameters, value: true},
		{name: "tags", kind: syncdomain.KindTool, field: syncdomain.FieldTags, value: []any{"ok", 1}},
		{name: "skill", kind: syncdomain.KindSkill, field: syncdomain.FieldMarkdown, value: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			selected := &Node{Fields: map[string]*Node{
				string(test.field): {Value: test.value},
			}}
			fieldMap := map[string]string{string(test.field): string(test.field)}
			if test.kind == syncdomain.KindVariation && test.field != syncdomain.FieldPrompt && test.field != syncdomain.FieldMessages {
				selected.Fields["prompt"] = &Node{Value: "Help"}
				fieldMap[string(syncdomain.FieldPrompt)] = "prompt"
			}
			_, err := buildProjection(test.kind, selected, fieldMap)
			require.Error(t, err)
		})
	}

	_, err := buildProjection(syncdomain.KindVariation, &Node{}, map[string]string{})
	require.Error(t, err)
	_, err = buildProjection(syncdomain.Kind("unknown"), &Node{}, nil)
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
			_, present := projection.Value(field)
			assert.True(t, present)
		}
	}
}

func TestMessageUpdateSafetyBranches(t *testing.T) {
	have := []syncdomain.Message{{Role: syncdomain.RoleSystem, Content: "old"}}
	tests := []struct {
		name string
		node *Node
		want []syncdomain.Message
	}{
		{name: "different count", node: &Node{}, want: nil},
		{name: "different role", node: &Node{}, want: []syncdomain.Message{{Role: syncdomain.RoleUser, Content: "old"}}},
		{name: "missing content", node: &Node{}, want: []syncdomain.Message{{Role: syncdomain.RoleSystem, Content: "new"}}},
		{name: "read only content", node: &Node{Items: []*Node{{Fields: map[string]*Node{"content": {Reason: "dynamic"}}}}}, want: []syncdomain.Message{{Role: syncdomain.RoleSystem, Content: "new"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			update := updateMessageContents(SourceBundle{}, test.node, have, test.want)
			assert.Equal(t, CapabilityReadOnly, update.Capability)
		})
	}

	content := String("old", 0, 5, func(string) ([]byte, error) { return []byte(`"new"`), nil })
	content.File = "missing.py"
	update := updateMessageContents(
		SourceBundle{Primary: Document{Path: "main.py"}},
		&Node{Items: []*Node{{Fields: map[string]*Node{"content": content}}}},
		have, []syncdomain.Message{{Role: syncdomain.RoleSystem, Content: "new"}},
	)
	assert.Equal(t, CapabilityReadOnly, update.Capability)
}

func TestSourceValueHelpersRejectDynamicValues(t *testing.T) {
	assert.False(t, semanticEqual(make(chan int), make(chan int)))
	_, ok := AsNumber(math.NaN())
	assert.True(t, ok)
	_, ok = AsNumber("1")
	assert.False(t, ok)
	_, ok = stringList(true)
	assert.False(t, ok)
	_, ok = objectValue(true)
	assert.False(t, ok)

	stringValue := String("old", 0, 3, func(value string) ([]byte, error) { return []byte(value), nil })
	_, err := stringValue.Encode(1)
	require.Error(t, err)
	numberValue := Number(1, 0, 1, func(float64) ([]byte, error) { return []byte("1"), nil })
	_, err = numberValue.Encode("one")
	require.Error(t, err)
}

func TestRegistryContractAndDependencyErrors(t *testing.T) {
	require.Error(t, Register(nil))
	require.Error(t, Register(contractAdapter{}))
	require.NoError(t, Register(namedFakeAdapter{format: "contract-test"}))
	require.Error(t, Register(namedFakeAdapter{format: "contract-test"}))

	_, err := lookup("missing-format")
	require.Error(t, err)
	dependencies, err := DependencyPaths("contract-test", Document{})
	require.NoError(t, err)
	assert.Nil(t, dependencies)
	_, err = DependencyPaths("missing-format", Document{})
	require.Error(t, err)
}

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
	root := &Node{Fields: map[string]*Node{
		"agents": {Items: []*Node{
			{Fields: map[string]*Node{
				"id": {Value: "support"},
				"messages": {Items: []*Node{
					{Fields: map[string]*Node{"content": {Value: "First"}}},
					{Fields: map[string]*Node{"content": {Value: "Second"}}},
				}, Collection: true},
			}},
		}, Collection: true},
	}}

	value, err := root.Resolve(`agents[id="support"].messages[1].content`)

	require.NoError(t, err)
	assert.Equal(t, "Second", value.Value)
}

func TestResolvePathIndexErrorsUseStableCodes(t *testing.T) {
	root := &Node{Fields: map[string]*Node{
		"items": {Items: []*Node{{Value: "only"}}, Collection: true},
		"value": {Value: "text"},
	}}
	tests := []struct {
		path string
		code syncdomain.ErrorCode
	}{
		{path: "items[1]", code: syncdomain.ErrorIndexOutOfRange},
		{path: "value[0]", code: syncdomain.ErrorPathTypeMismatch},
	}
	for _, test := range tests {
		_, err := root.Resolve(test.path)
		problem, ok := syncdomain.ProblemFrom(err)
		require.True(t, ok)
		assert.Equal(t, test.code, problem.Code)
	}
}

func TestResolvePathSupportsLiteralKeysAndRejectsAmbiguousSelectors(t *testing.T) {
	root := &Node{Fields: map[string]*Node{
		"metadata": {Fields: map[string]*Node{"display.name": {Value: "Support"}}},
		"agents": {
			Items: []*Node{
				{Fields: map[string]*Node{"id": {Value: "support"}}},
				{Fields: map[string]*Node{"id": {Value: "support"}}},
			},
		},
	}}
	value, err := root.Resolve(`metadata["display.name"]`)
	require.NoError(t, err)
	assert.Equal(t, "Support", value.Value)

	_, err = root.Resolve(`agents[id="support"]`)
	problem, ok := syncdomain.ProblemFrom(err)
	require.True(t, ok)
	assert.Equal(t, syncdomain.ErrorPathAmbiguous, problem.Code)
}

func TestResolvePathMatchesEveryFilterCondition(t *testing.T) {
	root := &Node{Fields: map[string]*Node{
		"items": {Items: []*Node{
			{Fields: map[string]*Node{
				"role": {Value: "system"}, "name": {Value: "intro"}, "content": {Value: "First"},
			}},
			{Fields: map[string]*Node{
				"role": {Value: "system"}, "name": {Value: "outro"}, "content": {Value: "Second"},
			}},
		}},
	}}

	value, err := root.Resolve(`items[role="system",name="intro"].content`)

	require.NoError(t, err)
	assert.Equal(t, "First", value.Value)
}

func TestRegisteredAdapterRunsThroughSharedCore(t *testing.T) {
	require.NoError(t, Register(fakeAdapter{}))
	id := syncdomain.VariationID("project", "config", "default")

	projection, err := ParseSource(
		bundle("prompt.fake", []byte("Registered without a core change.")),
		id, syncdomain.Reference{File: "prompt.fake", Format: "fake-test"},
	)

	require.NoError(t, err)
	assert.Equal(t, "Registered without a core change.", *projection.(syncdomain.VariationProjection).Instructions)
}

func TestRegistryRejectsInvalidAndDuplicateAdapters(t *testing.T) {
	require.Error(t, Register(nil))
	adapter := namedFakeAdapter{format: "duplicate-test"}
	require.NoError(t, Register(adapter))
	require.Error(t, Register(adapter))

	_, err := lookup("not-registered")
	problem, ok := syncdomain.ProblemFrom(err)
	require.True(t, ok)
	assert.Equal(t, syncdomain.ErrorUnsupportedFormat, problem.Code)
}

func TestIndexedSelectionIdentityUsesStableFieldsAndFallback(t *testing.T) {
	identified := &Node{Fields: map[string]*Node{
		"id": {Value: "support"},
		"n":  {Value: 1},
	}}
	assert.Equal(t, map[string]any{"id": "support"}, directSelectionIdentity(identified))
	assert.Nil(t, directSelectionIdentity(nil))
	assert.True(t, scalarValue(uint64(2)))
	assert.False(t, scalarValue(nil))
	assert.False(t, (*Node)(nil).IsCollection())
	assert.True(t, (&Node{Value: []string{"one"}}).IsCollection())
	assert.Equal(t,
		[]any{"one"},
		stableNodeValue(&Node{Items: []*Node{{Value: "one"}}, Collection: true}, map[*Node]bool{}),
	)
	assert.Nil(t, stableNodeValue(&Node{}, map[*Node]bool{}))

	cycle := &Node{Fields: map[string]*Node{}}
	cycle.Fields["self"] = cycle
	fallback := &Node{Fields: map[string]*Node{"prompt": {Value: "Help"}}}
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
		[]indexedResolution{{path: "items[0]", collectionLength: 1, item: &Node{Value: make(chan int)}}},
		fallback,
	)
	require.ErrorContains(t, err, "encode indexed selection fingerprint")
}

type fakeAdapter struct{}

func (fakeAdapter) Format() string   { return "fake-test" }
func (fakeAdapter) Behavior() string { return "fake-test/1" }
func (fakeAdapter) Inspect(bundle SourceBundle) (*Node, error) {
	document := bundle.Primary
	return String(string(document.Data), 0, len(document.Data), func(text string) ([]byte, error) {
		return []byte(text), nil
	}), nil
}
func (fakeAdapter) Defaults(kind syncdomain.Kind) (string, map[string]string) {
	return "$", map[string]string{string(syncdomain.FieldPrompt): "$"}
}

type namedFakeAdapter struct{ format string }

func (adapter namedFakeAdapter) Format() string { return adapter.format }
func (namedFakeAdapter) Behavior() string       { return "fake-test/1" }
func (namedFakeAdapter) Inspect(bundle SourceBundle) (*Node, error) {
	return fakeAdapter{}.Inspect(bundle)
}
func (namedFakeAdapter) Defaults(kind syncdomain.Kind) (string, map[string]string) {
	return fakeAdapter{}.Defaults(kind)
}

type contractAdapter struct{}

func (contractAdapter) Format() string                                       { return "" }
func (contractAdapter) Behavior() string                                     { return "" }
func (contractAdapter) Inspect(SourceBundle) (*Node, error)                  { return nil, nil }
func (contractAdapter) Defaults(syncdomain.Kind) (string, map[string]string) { return "$", nil }

func bundle(path string, data []byte) SourceBundle {
	return SourceBundle{Primary: Document{Path: path, Data: data}}
}

func TestCoreHelpersRejectUnknownKindsAndFailedEncoders(t *testing.T) {
	assert.False(t, fieldAllowed(syncdomain.Kind("unknown"), syncdomain.FieldPrompt))
	_, err := ParseDocument(Document{}, "missing")
	require.Error(t, err)
	number, ok := AsNumber(1)
	assert.True(t, ok)
	assert.Equal(t, 1.0, number)

	content := String("old", 0, 3, func(string) ([]byte, error) { return nil, assert.AnError })
	update := updateMessageContents(
		bundle("value.json", []byte("old")),
		&Node{Items: []*Node{{Fields: map[string]*Node{"content": content}}}},
		[]syncdomain.Message{{Role: syncdomain.RoleSystem, Content: "old"}},
		[]syncdomain.Message{{Role: syncdomain.RoleSystem, Content: "new"}},
	)
	assert.Equal(t, CapabilityReadOnly, update.Capability)
}
