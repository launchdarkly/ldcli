package srcbundle

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sampleSwift = "import Foundation\n" + // 1
	"\n" + // 2
	"struct Cart {\n" + // 3
	"    func total() -> Int {\n" + // 4
	"        let items = [1, 2, 3]\n" + // 5
	"        return items[9]\n" + // 6  <- crash line
	"    }\n" + // 7
	"}\n" // 8

func encode(t *testing.T, files map[string]string) *Bundle {
	t.Helper()
	b := &Builder{}
	for p, c := range files {
		b.Add(p, []byte(c))
	}
	var buf bytes.Buffer
	require.NoError(t, b.Encode(&buf))
	bundle, err := Open(buf.Bytes())
	require.NoError(t, err)
	return bundle
}

func TestRoundTrip(t *testing.T) {
	bundle := encode(t, map[string]string{
		"/src/Cart.swift":  sampleSwift,
		"/src/Other.swift": "let x = 1\n",
	})
	assert.Equal(t, 2, bundle.Len())

	got, ok := bundle.File("/src/Cart.swift")
	require.True(t, ok)
	assert.Equal(t, sampleSwift, string(got))

	_, ok = bundle.File("/src/Missing.swift")
	assert.False(t, ok)
}

func TestWindow(t *testing.T) {
	bundle := encode(t, map[string]string{"/src/Cart.swift": sampleSwift})

	before, content, after, ok := bundle.Window("/src/Cart.swift", 6, 2)
	require.True(t, ok)
	// Every line keeps its trailing newline, matching the JS source-map path.
	assert.Equal(t, "    func total() -> Int {\n        let items = [1, 2, 3]\n", before)
	assert.Equal(t, "        return items[9]\n", content)
	assert.Equal(t, "    }\n}\n", after)
}

func TestWindowClampsAtFileEdges(t *testing.T) {
	bundle := encode(t, map[string]string{"/src/Cart.swift": sampleSwift})

	before, content, _, ok := bundle.Window("/src/Cart.swift", 1, 5)
	require.True(t, ok)
	assert.Equal(t, "", before)
	assert.Equal(t, "import Foundation\n", content)

	_, content, after, ok := bundle.Window("/src/Cart.swift", 8, 5)
	require.True(t, ok)
	assert.Equal(t, "}\n", content)
	assert.Equal(t, "", after)
}

func TestWindowRejectsBadInput(t *testing.T) {
	bundle := encode(t, map[string]string{"/src/Cart.swift": sampleSwift})

	_, _, _, ok := bundle.Window("/src/Cart.swift", 0, 2)
	assert.False(t, ok, "lines are 1-based")

	_, _, _, ok = bundle.Window("/src/Cart.swift", 999, 2)
	assert.False(t, ok, "line past EOF")

	_, _, _, ok = bundle.Window("/src/Nope.swift", 1, 2)
	assert.False(t, ok, "unbundled file")
}

func TestFileWithoutTrailingNewline(t *testing.T) {
	bundle := encode(t, map[string]string{"/a.swift": "one\ntwo"})
	_, content, after, ok := bundle.Window("/a.swift", 2, 1)
	require.True(t, ok)
	assert.Equal(t, "two", content)
	assert.Equal(t, "", after)
}

func TestEmptyBundle(t *testing.T) {
	bundle := encode(t, nil)
	assert.Equal(t, 0, bundle.Len())
	_, ok := bundle.File("/anything")
	assert.False(t, ok)
}

func TestOpenRejectsGarbage(t *testing.T) {
	_, err := Open(nil)
	assert.Error(t, err)

	_, err = Open([]byte("not a bundle at all............."))
	assert.Error(t, err)

	// Right magic, wrong version.
	b := &Builder{}
	b.Add("/a.swift", []byte("x\n"))
	var buf bytes.Buffer
	require.NoError(t, b.Encode(&buf))
	raw := buf.Bytes()
	raw[4] = 0xFF
	_, err = Open(raw)
	assert.Error(t, err)
}

// A truncated bundle must fail to open rather than panic during lookup.
func TestOpenRejectsTruncated(t *testing.T) {
	b := &Builder{}
	b.Add("/a.swift", []byte(sampleSwift))
	var buf bytes.Buffer
	require.NoError(t, b.Encode(&buf))
	raw := buf.Bytes()

	for _, n := range []int{headerSize, headerSize + 4, len(raw) - 1} {
		_, err := Open(raw[:n])
		assert.Error(t, err, "truncated to %d bytes should not open", n)
	}
}

func TestManyFilesBinarySearch(t *testing.T) {
	files := map[string]string{}
	for _, p := range []string{"/z.swift", "/a.swift", "/m.swift", "/b.swift", "/y.swift"} {
		files[p] = "// " + p + "\n"
	}
	bundle := encode(t, files)
	assert.Equal(t, 5, bundle.Len())
	for p := range files {
		got, ok := bundle.File(p)
		require.True(t, ok, "expected %s", p)
		assert.Equal(t, "// "+p+"\n", string(got))
	}
}
