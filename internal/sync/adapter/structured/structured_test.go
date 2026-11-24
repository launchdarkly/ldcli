package structured

import (
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	"github.com/launchdarkly/ldcli/internal/sync/adapter"
	"github.com/launchdarkly/ldcli/internal/sync/adapter/yamltree"
)

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
			_, err := parseJSONDocument(adapter.Document{Path: "value.json", Data: []byte(input)})
			require.Error(t, err)
		})
	}
	_, err := parseJSONDocument(adapter.Document{Path: "value.json", Data: []byte{0xff}})
	require.Error(t, err)

	root, err := parseJSONDocument(adapter.Document{Path: "value.json", Data: []byte("\ufeff {\"empty\":{},\"array\":[],\"false\":false,\"null\":null}")})
	require.NoError(t, err)
	assert.Equal(t, false, root.Fields["false"].Value)
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
			_, err := parseYAMLDocument(adapter.Document{Path: "value.yaml", Data: input})
			require.Error(t, err)
		})
	}

	root, err := parseYAMLDocument(adapter.Document{Path: "value.yaml", Data: []byte(`
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
	assert.Equal(t, adapter.CapabilityReadOnly, root.Capability)
	assert.Equal(t, adapter.CapabilityReadOnly, root.Fields["copy"].Capability)
	assert.Equal(t, adapter.CapabilityReadOnly, root.Fields["tagged"].Capability)
}

func TestTOMLAdapterHandlesSafetyForms(t *testing.T) {
	for name, input := range map[string][]byte{
		"invalid UTF-8":  {0xff},
		"invalid syntax": []byte("value = ["),
		"duplicate":      []byte("value = 1\nvalue = 2\n"),
		"value as table": []byte("value = 1\n[value]\nchild = 2\n"),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseTOMLDocument(adapter.Document{Path: "value.toml", Data: input})
			require.Error(t, err)
		})
	}

	root, err := parseTOMLDocument(adapter.Document{Path: "value.toml", Data: []byte("\ufeff" + `
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
	assert.Equal(t, "2026-10-30", root.Fields["date"].Value)
	assert.Len(t, root.Fields["tools"].Items, 2)

	assert.Equal(t, `"""next\"\"\"value"""`, mustTOMLString(t, `next"""value`, `"""old"""`))
	assert.Equal(t, "'''next'''", mustTOMLString(t, "next", "'''old'''"))
	_, err = encodeTOMLString("bad'''value", "'''old'''")
	require.Error(t, err)
	_, err = encodeTOMLString("bad\nvalue", "'old'")
	require.Error(t, err)
	assert.Equal(t, `"\b\t\n\f\r\u0001\u0080\"\\"`, mustTOMLString(t, "\b\t\n\f\r\x01\u0080\"\\", `"old"`))
}

func TestYAMLStringCodecRejectsUnsupportedCodePoint(t *testing.T) {
	_, err := yamltree.EncodeString("\uffff", "plain")
	require.ErrorContains(t, err, "unsupported character U+FFFF")
}

func mustTOMLString(t *testing.T, value, previous string) string {
	t.Helper()
	encoded, err := encodeTOMLString(value, previous)
	require.NoError(t, err)
	return string(encoded)
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
		root, err := adapter.ParseDocument(adapter.Document{Path: "value.toml", Data: source}, "toml")
		require.NoError(t, err)
		node, err := root.Resolve("value")
		require.NoError(t, err)
		require.Equal(t, value, node.Value)
	})
}

func TestYAMLTrailingDocumentMarkerKeepsTheDocumentWritable(t *testing.T) {
	root, err := parseYAMLDocument(adapter.Document{Path: "value.yaml", Data: []byte("name: one\n---\n")})
	require.NoError(t, err)
	assert.Equal(t, adapter.CapabilityDirect, root.Fields["name"].Capability)

	root, err = parseYAMLDocument(adapter.Document{Path: "value.yaml", Data: []byte("name: one\n---\nname: two\n")})
	require.NoError(t, err)
	assert.Equal(t, adapter.CapabilityReadOnly, root.Fields["name"].Capability, "a second document with content is read-only")
}

func TestYAMLLaterDocumentsAfterAnEmptyOneAreMultiDocument(t *testing.T) {
	root, err := parseYAMLDocument(adapter.Document{Path: "value.yaml", Data: []byte("name: one\n---\n---\nname: two\n")})
	require.NoError(t, err)
	assert.Equal(t, adapter.CapabilityReadOnly, root.Fields["name"].Capability)
}

func TestJSONWritesKeepHTMLCharacters(t *testing.T) {
	root, err := parseJSONDocument(adapter.Document{Path: "value.json", Data: []byte(`{"prompt": "a", "schema": {"type": "object"}}`)})
	require.NoError(t, err)
	encoded, err := root.Fields["prompt"].Encode("Use <b>bold</b> & more")
	require.NoError(t, err)
	assert.Equal(t, `"Use <b>bold</b> & more"`, string(encoded))
	encoded, err = root.Fields["schema"].Encode(map[string]any{"description": "a < b"})
	require.NoError(t, err)
	assert.Equal(t, `{"description":"a < b"}`, string(encoded))
}

func TestEmptyArraysAreNotNull(t *testing.T) {
	jsonRoot, err := parseJSONDocument(adapter.Document{Path: "value.json", Data: []byte(`{"items": [], "none": null}`)})
	require.NoError(t, err)
	assert.Equal(t, []any{}, jsonRoot.Fields["items"].Value)
	assert.NotNil(t, jsonRoot.Fields["items"].Value, "an empty JSON array is a value, not null")
	assert.Nil(t, jsonRoot.Fields["none"].Value)
	assert.Equal(t, map[string]any{"items": []any{}, "none": nil}, jsonRoot.Value)

	tomlRoot, err := parseTOMLDocument(adapter.Document{Path: "value.toml", Data: []byte("items = []\n")})
	require.NoError(t, err)
	assert.Equal(t, []any{}, tomlRoot.Fields["items"].Value)
}

func TestJSONBooleanWritesCheckTheType(t *testing.T) {
	root, err := parseJSONDocument(adapter.Document{Path: "value.json", Data: []byte(`{"flag": true, "slot": null}`)})
	require.NoError(t, err)
	flag := root.Fields["flag"]
	assert.Equal(t, true, flag.Value)
	_, err = flag.Encode("yes")
	require.Error(t, err, "a boolean leaf rejects a string")
	encoded, err := flag.Encode(false)
	require.NoError(t, err)
	assert.Equal(t, "false", string(encoded))

	encoded, err = root.Fields["slot"].Encode("text")
	require.NoError(t, err, "a null slot takes any JSON value")
	assert.Equal(t, `"text"`, string(encoded))
}

func TestJSONIntegersBeyondExactFloatsAreReadOnly(t *testing.T) {
	root, err := parseJSONDocument(adapter.Document{Path: "value.json", Data: []byte(
		`{"exact": 9007199254740992, "big": 9007199254740993, "huge": 99999999999999999999, "negative": -9007199254740993, "ratio": 1.5, "exponent": 1e20}`,
	)})
	require.NoError(t, err)
	assert.Equal(t, float64(1<<53), root.Fields["exact"].Value, "the largest exact integer is a number")
	for _, field := range []string{"big", "huge", "negative"} {
		value := root.Fields[field]
		assert.Equal(t, adapter.CapabilityReadOnly, value.Capability, "%s has no exact float64", field)
		assert.Equal(t, "JSON integer is too large to represent exactly", value.Reason, field)
	}
	assert.Equal(t, "9007199254740993", root.Fields["big"].Value, "the text of the integer is kept")
	assert.Equal(t, 1.5, root.Fields["ratio"].Value)
	assert.Equal(t, 1e20, root.Fields["exponent"].Value, "a float literal keeps its float value")
	assert.Equal(t, adapter.CapabilityReadOnly, root.Capability, "the object that holds the integer is read-only as a whole")
	assert.Nil(t, root.Encode)

	nested, err := parseJSONDocument(adapter.Document{Path: "value.json", Data: []byte(`{"schema": {"limits": [9007199254740993]}, "name": "ok"}`)})
	require.NoError(t, err)
	assert.Equal(t, adapter.CapabilityReadOnly, nested.Fields["schema"].Capability, "a nested object that holds the integer is read-only")
	assert.Equal(t, adapter.CapabilityReadOnly, nested.Fields["schema"].Fields["limits"].Capability, "the array that holds the integer is read-only")
	assert.Equal(t, adapter.CapabilityDirect, nested.Fields["name"].Capability, "a sibling leaf stays writable")
}

func TestYAMLIntegersBeyondExactFloatsAreReadOnly(t *testing.T) {
	root, err := parseYAMLDocument(adapter.Document{Path: "value.yaml", Data: []byte(
		"exact: 9007199254740992\nbig: 9007199254740993\nhuge: 99999999999999999999\nnegative: -9007199254740993\nratio: 1.5\n",
	)})
	require.NoError(t, err)
	assert.Equal(t, float64(1<<53), root.Fields["exact"].Value, "the largest exact integer is a number")
	for _, field := range []string{"big", "huge", "negative"} {
		value := root.Fields[field]
		assert.Equal(t, adapter.CapabilityReadOnly, value.Capability, "%s has no exact float64", field)
		assert.Equal(t, "YAML integer is too large to represent exactly", value.Reason, field)
	}
	assert.Equal(t, "9007199254740993", root.Fields["big"].Value, "the text of the integer is kept")
	assert.Equal(t, 1.5, root.Fields["ratio"].Value)

	encoded, err := root.Fields["exact"].Encode(float64(1000000))
	require.NoError(t, err)
	assert.Equal(t, "1000000", string(encoded), "an integer slot writes an integer, not scientific notation")
	_, err = root.Fields["exact"].Encode(1.5)
	require.Error(t, err, "an integer slot rejects a fraction")
	encoded, err = root.Fields["ratio"].Encode(float64(1000000))
	require.NoError(t, err)
	assert.Equal(t, "1e+06", string(encoded), "a float slot keeps the float spelling")
	encoded, err = root.Fields["ratio"].Encode(float64(2))
	require.NoError(t, err)
	assert.Equal(t, "2.0", string(encoded), "a whole number in a float slot keeps a fraction")
}

func TestYAMLWritesKeepHTMLCharacters(t *testing.T) {
	root, err := parseYAMLDocument(adapter.Document{Path: "value.yaml", Data: []byte("prompt: \"a < b\"\nplain: text\n")})
	require.NoError(t, err)
	encoded, err := root.Fields["prompt"].Encode("x > y & z")
	require.NoError(t, err)
	assert.Equal(t, `"x > y & z"`, string(encoded), "a quoted YAML string keeps <, >, and &")
	encoded, err = root.Fields["plain"].Encode("a: b & c")
	require.NoError(t, err)
	assert.Equal(t, `"a: b & c"`, string(encoded))
}

func TestJSONProblemsNameTheFile(t *testing.T) {
	for name, data := range map[string]string{
		"syntax":    `{"a": 1,`,
		"duplicate": `{"a": 1, "a": 2}`,
		"trailing":  `{} x`,
	} {
		_, err := parseJSONDocument(adapter.Document{Path: "config.json", Data: []byte(data)})
		require.Error(t, err, name)
		problem, ok := syncdomain.ProblemFrom(err)
		require.True(t, ok, name)
		assert.Equal(t, "config.json", problem.File, "the %s problem names the file", name)
	}
}

func TestYAMLAndTOMLProblemsNameTheFile(t *testing.T) {
	cases := map[string]func() (*adapter.Node, error){
		"yaml empty": func() (*adapter.Node, error) {
			return parseYAMLDocument(adapter.Document{Path: "config.yaml", Data: []byte("")})
		},
		"yaml duplicate": func() (*adapter.Node, error) {
			return parseYAMLDocument(adapter.Document{Path: "config.yaml", Data: []byte("a: 1\na: 2\n")})
		},
		"toml duplicate": func() (*adapter.Node, error) {
			return parseTOMLDocument(adapter.Document{Path: "config.yaml", Data: []byte("a = 1\na = 2\n")})
		},
	}
	for name, parse := range cases {
		_, err := parse()
		require.Error(t, err, name)
		problem, ok := syncdomain.ProblemFrom(err)
		require.True(t, ok, name)
		assert.Equal(t, "config.yaml", problem.File, "the %s problem names the file", name)
	}
}
