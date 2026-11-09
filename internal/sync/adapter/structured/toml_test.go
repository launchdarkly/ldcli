package structured

import (
	"testing"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/sync/adapter"
)

func TestTOMLArrayTablesRejectAnIncompatibleKey(t *testing.T) {
	for name, input := range map[string]string{
		"value":        "tools = 1\n[[tools]]\nname = \"one\"\n",
		"table":        "[tools]\nname = \"one\"\n[[tools]]\nname = \"two\"\n",
		"inline table": "tools = { name = \"one\" }\n[[tools]]\nname = \"two\"\n",
		"plain array":  "tools = []\n[[tools]]\nname = \"one\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseTOMLDocument(adapter.Document{Path: "value.toml", Data: []byte(input)})
			require.ErrorContains(t, err, `TOML key "tools" has incompatible definitions`)
		})
	}
}

func TestTOMLSubtableOfAnArrayTableJoinsTheLastItem(t *testing.T) {
	root, err := parseTOMLDocument(adapter.Document{Path: "value.toml", Data: []byte(`
[[servers]]
name = "one"
[servers.limits]
max = 1
[[servers]]
name = "two"
`)})
	require.NoError(t, err)

	servers := root.Fields["servers"]
	require.Len(t, servers.Items, 2)
	assert.True(t, servers.Collection)
	assert.Equal(t, 1.0, servers.Items[0].Fields["limits"].Fields["max"].Value)
	assert.Nil(t, servers.Items[1].Fields["limits"])
	assert.Equal(t, []any{
		map[string]any{"name": "one", "limits": map[string]any{"max": 1.0}},
		map[string]any{"name": "two"},
	}, root.Value.(map[string]any)["servers"])
}

func TestTOMLStringEncodersRoundTrip(t *testing.T) {
	values := map[string]string{
		"escape":             `path C:\new\table`,
		"leading newline":    "\nfirst\nsecond",
		"trailing backslash": `ends with \`,
		"quotes":             `say """hi""" now"`,
		"control":            "tab\tbell\a",
		"crlf":               "one\r\ntwo",
		"unicode":            "caf\u00e9 \u2014 ok",
	}
	previous := []string{`"old"`, `"""old"""`, "\"\"\"\nold\"\"\"", `'old'`, `'''old'''`, "'''\nold'''"}
	for name, value := range values {
		for _, old := range previous {
			t.Run(name+" after "+old, func(t *testing.T) {
				encoded, err := encodeTOMLString(value, old)
				if err != nil {
					// A literal string has no escapes, so it cannot hold a
					// control character, and a single-line one no newline.
					require.True(t, old[0] == '\'', "basic strings hold every value")
					return
				}
				var decoded struct {
					Value string `toml:"value"`
				}
				require.NoError(t, toml.Unmarshal([]byte("value = "+string(encoded)+"\n"), &decoded), string(encoded))
				assert.Equal(t, value, decoded.Value)
			})
		}
	}
}

func TestTOMLHeadersDefineATableOnce(t *testing.T) {
	for name, input := range map[string]string{
		"table twice":            "[a]\nx = 1\n[a]\ny = 2\n",
		"table after array":      "[[a]]\nx = 1\n[a]\ny = 2\n",
		"table after inline":     "a = { x = 1 }\n[a]\ny = 2\n",
		"table after dotted key": "a.x = 1\n[a]\ny = 2\n",
		"dotted key after table": "[a]\nx = 1\n[b]\na.y = 2\n[a]\nz = 3\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseTOMLDocument(adapter.Document{Path: "value.toml", Data: []byte(input)})
			require.ErrorContains(t, err, `is defined more than once`)
		})
	}

	root, err := parseTOMLDocument(adapter.Document{Path: "value.toml", Data: []byte(`
[x.y.z.w]
deep = 1
[x]
top = 2
[x.y]
mid = 3
`)})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"x": map[string]any{
		"top": 2.0, "y": map[string]any{"mid": 3.0, "z": map[string]any{"w": map[string]any{"deep": 1.0}}},
	}}, root.Value)
}

func TestTOMLInfinityAndNaNAreReadOnly(t *testing.T) {
	root, err := parseTOMLDocument(adapter.Document{Path: "value.toml", Data: []byte("a = inf\nb = -inf\nc = nan\nd = 1.5\n")})
	require.NoError(t, err)
	for key, raw := range map[string]string{"a": "inf", "b": "-inf", "c": "nan"} {
		assert.Equal(t, adapter.CapabilityReadOnly, root.Fields[key].Capability, key)
		assert.Equal(t, raw, root.Fields[key].Value, key)
	}
	assert.Equal(t, 1.5, root.Fields["d"].Value)
	assert.Equal(t, adapter.CapabilityDirect, root.Fields["d"].Capability)
}

func TestTOMLClosedTablesCannotBeExtended(t *testing.T) {
	for name, input := range map[string]string{
		"dotted key into inline table": "a = { x = 1 }\na.y = 2\n",
		"header into inline table":     "a = { x = 1 }\n[a.b]\ny = 2\n",
		"dotted key into header table": "[a.b]\nx = 1\n[a]\nb.y = 2\n",
		"dotted key into array table":  "[[a]]\nx = 1\n[b]\n[c]\n",
	} {
		t.Run(name, func(t *testing.T) {
			if name == "dotted key into array table" {
				input = "[[a]]\nx = 1\n" // The root table is closed once a header opens.
				_, err := parseTOMLDocument(adapter.Document{Path: "value.toml", Data: []byte("a.y = 1\n[[a]]\nx = 1\n")})
				require.ErrorContains(t, err, "incompatible definitions")
				return
			}
			_, err := parseTOMLDocument(adapter.Document{Path: "value.toml", Data: []byte(input)})
			require.ErrorContains(t, err, "cannot be extended here")
		})
	}

	root, err := parseTOMLDocument(adapter.Document{Path: "value.toml", Data: []byte(`
a.b.c = 1
a.b.d = 2
[a.e]
f = 3
`)})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"a": map[string]any{
		"b": map[string]any{"c": 1.0, "d": 2.0}, "e": map[string]any{"f": 3.0},
	}}, root.Value)
}

func TestTOMLSubtablesOfArrayItemsAreReadOnly(t *testing.T) {
	root, err := parseTOMLDocument(adapter.Document{Path: "value.toml", Data: []byte("[[servers]]\nname = \"one\"\n[servers.limits]\nmax = 1\n[servers.limits.deep]\nvalue = \"x\"\n[other]\nkept = true\n")})
	require.NoError(t, err)
	item := root.Fields["servers"].Items[0]
	limits := item.Fields["limits"]
	assert.Equal(t, adapter.CapabilityReadOnly, limits.Capability)
	assert.Equal(t, adapter.CapabilityReadOnly, limits.Fields["max"].Capability)
	assert.Equal(t, adapter.CapabilityReadOnly, limits.Fields["deep"].Fields["value"].Capability)
	assert.Equal(t, adapter.CapabilityDirect, root.Fields["other"].Fields["kept"].Capability)
}

func TestTOMLTablesAreStructurallyReadOnlyWithWritableValues(t *testing.T) {
	root, err := parseTOMLDocument(adapter.Document{Path: "value.toml", Data: []byte(
		"[model]\nname = \"gpt\"\nlimits = { max = 1 }\n[model.parameters]\ntemperature = 0.2\n",
	)})
	require.NoError(t, err)
	model := root.Fields["model"]
	for name, table := range map[string]*adapter.Node{
		"root": root, "header table": model, "sub-table": model.Fields["parameters"], "inline table": model.Fields["limits"],
	} {
		assert.Equal(t, adapter.CapabilityReadOnly, table.Capability, name)
		assert.Equal(t, tomlStructureReason, table.Reason, name)
		assert.Nil(t, table.Encode, name)
	}
	assert.Equal(t, adapter.CapabilityDirect, model.Fields["name"].Capability)
	assert.Equal(t, adapter.CapabilityDirect, model.Fields["limits"].Fields["max"].Capability)
	assert.Equal(t, adapter.CapabilityDirect, model.Fields["parameters"].Fields["temperature"].Capability)
}

func TestTOMLNestedInlineTablesCannotBeExtended(t *testing.T) {
	_, err := parseTOMLDocument(adapter.Document{Path: "value.toml", Data: []byte("a = { b = { x = 1 }, b.y = 2 }\n")})
	require.ErrorContains(t, err, "cannot be extended here", "a dotted key cannot add to a nested inline table")

	root, err := parseTOMLDocument(adapter.Document{Path: "value.toml", Data: []byte("a = { b.x = 1, b.y = 2 }\n")})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"a": map[string]any{"b": map[string]any{"x": 1.0, "y": 2.0}}}, root.Value)
}

func TestTOMLImplicitHeaderTablesAreClosedToDottedKeys(t *testing.T) {
	_, err := parseTOMLDocument(adapter.Document{Path: "value.toml", Data: []byte("[a.b.c]\nx = 1\n[a]\nb.y = 2\n")})
	require.ErrorContains(t, err, "cannot be extended here", "a table that a header path created cannot take a dotted key")

	root, err := parseTOMLDocument(adapter.Document{Path: "value.toml", Data: []byte("[a.b.c]\nx = 1\n[a.b]\ny = 2\n")})
	require.NoError(t, err, "a later header defines an implicit table once")
	assert.Equal(t, map[string]any{"a": map[string]any{"b": map[string]any{"y": 2.0, "c": map[string]any{"x": 1.0}}}}, root.Value)
}

func TestTOMLNumbersKeepTheirTypeAndPrecision(t *testing.T) {
	root, err := parseTOMLDocument(adapter.Document{Path: "value.toml", Data: []byte("big = 9007199254740993\nratio = 1.0\ncount = 3\n")})
	require.NoError(t, err)
	big := root.Fields["big"]
	assert.Equal(t, adapter.CapabilityReadOnly, big.Capability, "an integer beyond 53 bits is read-only text")
	assert.Equal(t, "9007199254740993", big.Value)

	huge, err := parseTOMLDocument(adapter.Document{Path: "value.toml", Data: []byte("huge = 99999999999999999999\nname = \"ok\"\n")})
	require.NoError(t, err, "an integer beyond int64 does not fail the document")
	assert.Equal(t, adapter.CapabilityReadOnly, huge.Fields["huge"].Capability)
	assert.Equal(t, "99999999999999999999", huge.Fields["huge"].Value)
	assert.Equal(t, adapter.CapabilityDirect, huge.Fields["name"].Capability, "the other fields stay usable")

	encoded, err := root.Fields["ratio"].Encode(2.0)
	require.NoError(t, err)
	assert.Equal(t, "2.0", string(encoded), "a float slot keeps a float form")
	encoded, err = root.Fields["count"].Encode(4.0)
	require.NoError(t, err)
	assert.Equal(t, "4", string(encoded), "an integer slot writes an integer")
	_, err = root.Fields["count"].Encode(4.5)
	require.Error(t, err, "an integer slot does not take a fraction")
}
