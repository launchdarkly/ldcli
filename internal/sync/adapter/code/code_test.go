package code

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/sync/adapter"
)

func TestStringCodecsHandleSupportedEscapes(t *testing.T) {
	python := []byte(`"\\\'\"\n\r\t\b\f\v\x41\u0042\U0001F600"`)
	value, _, err := DecodePythonString(python)
	require.NoError(t, err)
	// A carriage return escape decodes to the one line ending.
	assert.Equal(t, "\\'\"\n\n\t\b\f\vAB😀", value)

	typescript := []byte(`"\\\'\"\n\r\t\b\f\v\x41\u0042"`)
	value, _, err = DecodeJSString(typescript)
	require.NoError(t, err)
	assert.Equal(t, "\\'\"\n\n\t\b\f\vAB", value)

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
				_, _, err := DecodeJSString(input)
				require.Error(t, err)
				return
			}
			_, _, err := DecodePythonString(input)
			require.Error(t, err)
		})
	}
}

func TestStringCodecsPreserveQuotesControlsAndMultilineText(t *testing.T) {
	assert.Equal(t, "\"\"\"line\n\\\"\\\"\\\"end\"\"\"", string(EncodePythonString("line\n\"\"\"end", `"""`)))
	encoded, err := EncodeJSString("line\n\t\r\x01\"", '"')
	require.NoError(t, err)
	assert.Equal(t, `"line\n\t\r\u0001\""`, string(encoded))
	_, err = EncodeJSString("unsafe ${value}", '`')
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
			_, _, err := DecodePythonString(input)
			require.Error(t, err)
		})
	}
}

func TestPlaceholderTranslationRejectsAmbiguousForms(t *testing.T) {
	tests := []struct {
		name  string
		value string
		style PlaceholderStyle
		from  bool
	}{
		{name: "brace escaped", value: "{{name}}", style: BracePlaceholders},
		{name: "brace residue", value: "literal }", style: BracePlaceholders},
		{name: "source unclosed", value: "{name", style: BracePlaceholders},
		{name: "source bad name", value: "{1name}", style: BracePlaceholders},
		{name: "dollar unclosed", value: "${name", style: DollarPlaceholders},
		{name: "target residue", value: "literal {", style: BracePlaceholders, from: true},
		{name: "target residue before placeholder", value: "literal { {{name}}", style: BracePlaceholders, from: true},
		{name: "target unclosed", value: "{{name", style: BracePlaceholders, from: true},
		{name: "target bad name", value: "{{1name}}", style: BracePlaceholders, from: true},
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

	node := adapter.String("hello {name}", 0, 14, func(text string) ([]byte, error) { return []byte(text), nil })
	translated := TranslatePlaceholders(node, BracePlaceholders)
	_, err := translated.Encode(1)
	require.Error(t, err)
	_, err = translated.Encode("hello {{1name}}")
	require.Error(t, err)
	assert.Nil(t, TranslatePlaceholders(nil, BracePlaceholders))

	dynamic := &adapter.Node{Value: true}
	assert.Same(t, dynamic, TranslatePlaceholders(dynamic, BracePlaceholders))
	assert.Same(t, dynamic, translateMessagePlaceholders(dynamic, BracePlaceholders))
}

func TestJSStringCodecRejectsMalformedInput(t *testing.T) {
	_, _, err := DecodeJSString([]byte("xx"))
	require.Error(t, err)
	_, _, err = DecodeJSString([]byte(`"x'`))
	require.Error(t, err)
	_, err = decodeEscapes("\\\nnext", false)
	require.NoError(t, err)
	_, err = decodeEscapes(`\q`, false)
	require.Error(t, err)
}

func TestLineContinuationsJoinWindowsLineBreaks(t *testing.T) {
	assert.Equal(t, "a\nb\nc", NormalizeLineEndings("a\r\nb\rc"), "Windows and classic Mac line endings become one")
	value, _, err := DecodePythonString([]byte("\"one \\\r\ntwo\""))
	require.NoError(t, err)
	assert.Equal(t, "one two", value, "a backslash before CRLF joins the lines as Python does")

	value, _, err = DecodeJSString([]byte("'one \\\r\ntwo'"))
	require.NoError(t, err)
	assert.Equal(t, "one two", value)

	value, _, err = DecodePythonString([]byte("\"one \\\ntwo\""))
	require.NoError(t, err)
	assert.Equal(t, "one two", value)
}

func TestMessageListKeepsTheFileOfItsSource(t *testing.T) {
	collection := &adapter.Node{
		File: "src/values.py", Start: 4, End: 40, Collection: true,
		Items: []*adapter.Node{{Fields: map[string]*adapter.Node{
			"role": {Value: "user"}, "content": {Value: "Hi"},
		}}},
	}

	messages := MessageList(collection)

	require.NotNil(t, messages)
	assert.Equal(t, "src/values.py", messages.File, "an imported message list keeps the dependency file")
	assert.Equal(t, 4, messages.Start)
}

func TestConstantIDIsTheVariableName(t *testing.T) {
	value := &adapter.Node{
		Value: map[string]any{"id": "abc", "prompt": "Help"},
		Fields: map[string]*adapter.Node{
			"id":     {Value: "abc", Capability: adapter.CapabilityDirect},
			"prompt": {Value: "Help", Capability: adapter.CapabilityDirect},
		},
		Capability: adapter.CapabilityDirect,
	}

	constant := Constant("CONFIG", value)

	assert.Equal(t, "CONFIG", constant.Fields["id"].Value, "the resource is selected by its variable name")
	assert.Equal(t, adapter.CapabilityReadOnly, constant.Fields["id"].Capability, "a map of the id says that it is the variable name")
	assert.Contains(t, constant.Fields["id"].Reason, "variable name")
	assert.Equal(t, "Help", constant.Fields["prompt"].Value)
	assert.Equal(t, "abc", constant.Value.(map[string]any)["id"], "a write of the whole object keeps the entry")
}

func TestPythonEscapesCoverOctalBellAndNamedCharacters(t *testing.T) {
	value, _, err := DecodePythonString([]byte(`"\101\x41\a\7"`))
	require.NoError(t, err)
	assert.Equal(t, "AA\a\a", value, "octal escapes and the bell escape decode as Python does")

	_, _, err = DecodePythonString([]byte(`"\N{BULLET}"`))
	require.Error(t, err, "a named character is read-only instead of wrong")
}

func TestPythonOctalEscapesStopAtThreeSevenSeven(t *testing.T) {
	_, _, err := DecodePythonString([]byte(`"\400"`))
	require.Error(t, err, "an octal escape above \\377 is read-only instead of wrong")
	value, _, err := DecodePythonString([]byte(`"\377"`))
	require.NoError(t, err)
	assert.Equal(t, "\u00ff", value)
}
