package structured

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

	assert.Equal(t, `"""next\"""value"""`, mustTOMLString(t, `next"""value`, `"""old"""`))
	assert.Equal(t, "'''next'''", mustTOMLString(t, "next", "'''old'''"))
	_, err = encodeTOMLString("bad'''value", "'''old'''")
	require.Error(t, err)
	_, err = encodeTOMLString("bad\nvalue", "'old'")
	require.Error(t, err)
	encoded := encodeTOMLBasicString("\b\t\n\f\r\x01\u0080\"\\")
	assert.True(t, strings.HasPrefix(encoded, `"`))
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
