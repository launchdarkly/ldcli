package yamltree

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/launchdarkly/ldcli/internal/sync/adapter"
)

func TestScalarSpanHelpers(t *testing.T) {
	_, _, _, _, err := ScalarSpan(Region{}, &yaml.Node{}, false)
	require.Error(t, err)
	assert.Equal(t, `"line\nnext"`, quoteYAML("line\nnext", "'old'"))
	assert.Equal(t, "'it''s'", quoteYAML("it's", "'old'"))
	_, err = EncodeString("bad\x01", "old")
	require.Error(t, err)

	end, closed := yamlQuotedEnd([]byte(`"unterminated`))
	assert.Equal(t, len(`"unterminated`), end)
	assert.False(t, closed)
	end, closed = yamlQuotedEnd([]byte(`'it''s'`))
	assert.Equal(t, len(`'it''s'`), end)
	assert.True(t, closed)
	assert.Equal(t, "value, next", string(yamlPlainScalar([]byte("value, next # comment"), false)))
	assert.Equal(t, "value", string(yamlPlainScalar([]byte("value, next]"), true)))
}

func TestQuotedScalarsThatSpanLinesAreReadOnly(t *testing.T) {
	data := []byte("prompt: \"first line\n  second line\"\nnext: value\n")
	var document yaml.Node
	require.NoError(t, yaml.Unmarshal(data, &document))
	root, err := Build(data, document.Content[0], 0, false)
	require.NoError(t, err)

	prompt := root.Fields["prompt"]
	assert.Equal(t, "first line second line", prompt.Value)
	assert.Equal(t, adapter.CapabilityReadOnly, prompt.Capability)
	assert.Equal(t, adapter.CapabilityDirect, root.Fields["next"].Capability)
}

func TestNumberSpellingsDecodeOrStayReadOnly(t *testing.T) {
	data := []byte("hex: 0x1A\noctal: 0o17\nseparated: 1_000\nflag: True\n")
	var document yaml.Node
	require.NoError(t, yaml.Unmarshal(data, &document))
	root, err := Build(data, document.Content[0], 0, false)
	require.NoError(t, err)

	assert.Equal(t, 26.0, root.Fields["hex"].Value)
	assert.Equal(t, 15.0, root.Fields["octal"].Value)
	assert.Equal(t, 1000.0, root.Fields["separated"].Value)
	assert.Equal(t, true, root.Fields["flag"].Value)
	assert.Equal(t, adapter.CapabilityReadOnly, unreadableScalar(&yaml.Node{Value: "odd"}, 0, 3).Capability)
}

func TestValidYAMLNumbersDoNotFailTheDocument(t *testing.T) {
	for _, value := range []string{"0x10", "0o17", ".inf", "-.inf", ".nan"} {
		t.Run(value, func(t *testing.T) {
			source := "limit: " + value + "\nname: kept\n"
			var document yaml.Node
			require.NoError(t, yaml.Unmarshal([]byte(source), &document))
			root, err := Build([]byte(source), document.Content[0], 0, false)
			require.NoError(t, err)
			assert.Equal(t, "kept", root.Fields["name"].Value)
			limit := root.Fields["limit"]
			if _, finite := limit.Value.(float64); finite {
				assert.Equal(t, adapter.CapabilityDirect, limit.Capability)
				return
			}
			// Infinity and NaN have no JSON form, so they stay read-only text.
			assert.Equal(t, value, limit.Value)
			assert.Equal(t, adapter.CapabilityReadOnly, limit.Capability)
		})
	}
}

func TestCircularAliasIsRejected(t *testing.T) {
	data := []byte("self: &self\n  again: *self\n")
	var document yaml.Node
	require.NoError(t, yaml.Unmarshal(data, &document))
	_, err := Build(data, document.Content[0], 0, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "*self refers to a node that contains it")

	data = []byte("base: &base\n  name: shared\ncopy: *base\nlist: [*base, *base]\n")
	document = yaml.Node{}
	require.NoError(t, yaml.Unmarshal(data, &document))
	root, err := Build(data, document.Content[0], 0, false)
	require.NoError(t, err)
	assert.Equal(t, "shared", root.Fields["copy"].Fields["name"].Value)
	assert.Len(t, root.Fields["list"].Items, 2)
}

func TestNullScalarsHaveNoValue(t *testing.T) {
	data := []byte("tilde: ~\nword: null\nempty:\n")
	var document yaml.Node
	require.NoError(t, yaml.Unmarshal(data, &document))
	root, err := Build(data, document.Content[0], 0, false)
	require.NoError(t, err)
	for _, key := range []string{"tilde", "word", "empty"} {
		assert.Nil(t, root.Fields[key].Value, key)
		assert.Equal(t, adapter.CapabilityReadOnly, root.Fields[key].Capability, key)
	}
}

func TestMergeKeysLetExplicitKeysWin(t *testing.T) {
	data := []byte("base: &base\n  name: shared\n  limit: 1\nother: &other\n  limit: 2\n  extra: true\nagent:\n  <<: *base\n  name: own\nlist:\n  limit: 9\n  <<: [*base, *other]\n")
	var document yaml.Node
	require.NoError(t, yaml.Unmarshal(data, &document))
	root, err := Build(data, document.Content[0], 0, false)
	require.NoError(t, err)

	agent := root.Fields["agent"]
	assert.Equal(t, "own", agent.Fields["name"].Value)
	assert.Equal(t, 1.0, agent.Fields["limit"].Value)
	assert.NotContains(t, agent.Fields, "<<")
	assert.Equal(t, adapter.CapabilityReadOnly, agent.Capability)

	list := root.Fields["list"]
	assert.Equal(t, 9.0, list.Fields["limit"].Value)
	assert.Equal(t, "shared", list.Fields["name"].Value)
	assert.Equal(t, true, list.Fields["extra"].Value)
	assert.Equal(t, map[string]any{"limit": 9.0, "name": "shared", "extra": true}, list.Value)

	for name, source := range map[string]string{
		"duplicate key":   "a: 1\na: 2\n",
		"duplicate merge": "b: &b\n  x: 1\nc:\n  <<: *b\n  <<: *b\n",
		"scalar merge":    "c:\n  <<: text\n",
		"scalar in list":  "b: &b\n  x: 1\nc:\n  <<: [*b, text]\n",
	} {
		t.Run(name, func(t *testing.T) {
			var document yaml.Node
			require.NoError(t, yaml.Unmarshal([]byte(source), &document))
			_, err := Build([]byte(source), document.Content[0], 0, false)
			require.Error(t, err)
		})
	}
}
