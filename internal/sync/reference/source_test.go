package reference

import (
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

func TestPathParserReadsTwoFieldFilter(t *testing.T) {
	path, err := parsePath(`items[id="one",role="system"]`)
	require.NoError(t, err)
	require.Len(t, path.steps, 1)
	assert.Equal(t, "items", path.steps[0].key)
	assert.Equal(t, []pathFilter{{key: "id", value: "one"}, {key: "role", value: "system"}}, path.steps[0].filters)
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
}

func TestMarkdownSectionUpdatePreservesOtherSections(t *testing.T) {
	source := []byte("# Support\nOld prompt\n\n# Other\nKeep this.\n")
	id := syncdomain.VariationID("project", "config", "default")
	reference := syncdomain.Reference{
		File: "AGENTS.md", Format: "markdown",
		Select: `sections[heading="Support"]`, Map: map[string]string{"prompt": "body"},
	}
	prompt := "New prompt"
	result, err := UpdateSource(
		sourceBundle("AGENTS.md", source),
		id,
		syncdomain.VariationModeAgent,
		reference,
		syncdomain.VariationProjection{Instructions: &prompt},
	)
	require.NoError(t, err)
	updated := applyConformanceEdits(t, source, directEdits(t, result))
	assert.Contains(t, string(updated), "# Other\nKeep this.")
}

func FuzzPathParserNeverPanics(f *testing.F) {
	for _, seed := range []string{
		"$", "field.child", `items[id="one"]`, `object["literal.key"]`,
		"items[0]", "items[-1]", "items[1.5]", "items[]", "items[2147483648]",
		"items[0][1]", `items[id="support"].messages[0]`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, path string) {
		_ = ParsePath(path)
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
