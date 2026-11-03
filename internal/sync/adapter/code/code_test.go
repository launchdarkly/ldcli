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
	assert.Equal(t, "\\'\"\n\r\t\b\f\vAB😀", value)

	typescript := []byte(`"\\\'\"\n\r\t\b\f\v\x41\u0042"`)
	value, _, err = DecodeJSString(typescript)
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
