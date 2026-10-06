package r8index

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// assertIndexMatchesMapping is what the format has to be judged on: for every
// question the enhancer can ask, an index answers what the parsed mapping answers.
// Both resolve through the same class.retrace, so what is really under test is
// whether a class survives the round trip intact.
func assertIndexMatchesMapping(t *testing.T, m *Mapping) {
	t.Helper()

	raw, err := Encode(m)
	require.NoError(t, err)
	ix, err := Open(raw)
	require.NoError(t, err)

	for obf, c := range m.classes {
		gotName, gotOK := ix.RetraceClass(obf)
		wantName, wantOK := m.RetraceClass(obf)
		assert.Equal(t, wantOK, gotOK, "RetraceClass(%q)", obf)
		assert.Equal(t, wantName, gotName, "RetraceClass(%q)", obf)

		for method, members := range c.members {
			for _, line := range probeLines(members) {
				assert.Equal(t, m.Retrace(obf, method, line), ix.Retrace(obf, method, line),
					"Retrace(%q, %q, %d)", obf, method, line)
			}
		}
		// A name the class does not remap still deobfuscates the class.
		assert.Equal(t, m.Retrace(obf, "notAMethod", 1), ix.Retrace(obf, "notAMethod", 1),
			"Retrace(%q, unmapped)", obf)
	}

	for original, file := range m.sourceFiles {
		assert.Equal(t, file, ix.SourceFile(original), "SourceFile(%q)", original)
	}

	// A class outside the mapping reads as absent, which is how an unobfuscated
	// frame passes through untouched.
	assert.Nil(t, ix.Retrace("not.In.Mapping", "a", 1))
	_, ok := ix.RetraceClass("not.In.Mapping")
	assert.False(t, ok)
	assert.Empty(t, ix.SourceFile("not.In.Mapping"))
}

// probeLines covers each range's edges and the lines outside it, since which entry a
// line falls in is what decides the frame.
func probeLines(members []member) []int {
	lines := []int{0, 1, 7, 1000}
	for _, mem := range members {
		if mem.hasRange {
			lines = append(lines,
				mem.minStart-1, mem.minStart,
				(mem.minStart+mem.minEnd)/2,
				mem.minEnd, mem.minEnd+1)
		}
	}
	return lines
}

func TestIndexMatchesParsedMapping(t *testing.T) {
	assertIndexMatchesMapping(t, parsedSampleMapping(t))
}

// The synthetic lambda mapping is the hard case: a synthesized class, a marker R8
// writes only on a member's first occurrence, and members prefixed with the class's
// own residual name.
func TestIndexMatchesSyntheticLambdaMapping(t *testing.T) {
	assertIndexMatchesMapping(t, Parse([]byte(syntheticLambdaMapping)))
}

func TestIndexMatchesMappingWithSourceFiles(t *testing.T) {
	assertIndexMatchesMapping(t, Parse([]byte(`com.example.app.Checkout -> a.b.c:
# {"id":"sourceFile","fileName":"Checkout.kt"}
    1:3:void run():2:2 -> a
    1:3:void com.example.app.Helper.help():2:2 -> a
com.example.app.Helper -> a.b.d:
# {"id":"sourceFile","fileName":"Helpers.kt"}
    1:1:void help():2:2 -> a
`)))
}

// One name being a prefix of another is where a search that compares in place gets it
// wrong, and source files are looked up by original name, where nesting makes shared
// prefixes ordinary.
func TestIndexSourceFileNamePrefixes(t *testing.T) {
	raw, err := Encode(Parse([]byte(`com.example.Cart -> a:
# {"id":"sourceFile","fileName":"Cart.kt"}
    1:1:void a():1:1 -> a
com.example.CartItem -> b:
# {"id":"sourceFile","fileName":"CartItem.kt"}
    1:1:void a():1:1 -> a
com.example.Cart$Line -> c:
# {"id":"sourceFile","fileName":"Cart.kt"}
    1:1:void a():1:1 -> a
`)))
	require.NoError(t, err)
	ix, err := Open(raw)
	require.NoError(t, err)

	assert.Equal(t, "Cart.kt", ix.SourceFile("com.example.Cart"))
	assert.Equal(t, "CartItem.kt", ix.SourceFile("com.example.CartItem"))
	assert.Equal(t, "Cart.kt", ix.SourceFile("com.example.Cart$Line"))
	assert.Empty(t, ix.SourceFile("com.example.Car"), "a shorter name is not a match")
	assert.Empty(t, ix.SourceFile("com.example.CartItems"), "a longer name is not a match")
	assert.Empty(t, ix.SourceFile(""))
}

// Equal mappings must encode to equal bytes, so an index can be addressed by the
// content id of the mapping it was built from.
func TestEncodingIsDeterministic(t *testing.T) {
	first, err := Encode(Parse([]byte(sampleMapping)))
	require.NoError(t, err)
	second, err := Encode(Parse([]byte(sampleMapping)))
	require.NoError(t, err)
	assert.Equal(t, first, second)
}

func TestEncodeRejectsNilMapping(t *testing.T) {
	_, err := Encode(nil)
	assert.Error(t, err)
}

// A block is bytes from storage, so every truncation of one has to read as unusable
// rather than panic or invent a class.
func TestDecodeClassRejectsTruncatedBlocks(t *testing.T) {
	m := Parse([]byte(sampleMapping))
	block := encodeClass(m.classes["a.b.c"])

	full, ok := decodeClass(block)
	require.True(t, ok)
	require.Equal(t, "com.example.app.UserService", full.originalName)

	for i := 0; i < len(block); i++ {
		c, ok := decodeClass(block[:i])
		if ok {
			// A prefix that happens to decode must still be a coherent class, not
			// one carrying members it never got the bytes for.
			assert.NotNil(t, c)
			continue
		}
		assert.Nil(t, c, "truncated at %d", i)
	}
}

// A count is the one field that sizes an allocation, so it is bounded by the bytes
// that are left rather than trusted.
func TestDecodeClassRejectsImplausibleCounts(t *testing.T) {
	// "" original name, no flags, then a method count of 2^32.
	block := append(appendString(nil, ""), 0)
	block = append(block, 0xff, 0xff, 0xff, 0xff, 0x0f)

	c, ok := decodeClass(block)
	assert.False(t, ok)
	assert.Nil(t, c)
}

func TestOpenRejectsGarbage(t *testing.T) {
	for _, raw := range [][]byte{nil, {}, []byte("SRCB"), []byte("not a bundle at all")} {
		_, err := Open(raw)
		assert.Error(t, err)
	}
}

// A block that survives the bundle's checks but decodes to nothing usable reads as a
// missing class, and is remembered as one.
func TestUnreadableBlockIsAMiss(t *testing.T) {
	raw, err := Encode(Parse([]byte(sampleMapping)))
	require.NoError(t, err)
	ix, err := Open(raw)
	require.NoError(t, err)

	assert.Nil(t, ix.Retrace("a.b.missing", "a", 1))
	assert.Nil(t, ix.Retrace("a.b.missing", "a", 1), "a miss is memoized, not re-read")
	assert.NotNil(t, ix.Retrace("a.b.c", "a", 2), "other classes still resolve")
}

// An index queried for every class must not end up holding every class, or it would
// have become the structure it exists to avoid.
func TestMemoIsBounded(t *testing.T) {
	var mapping []byte
	for i := 0; i < memoLimit*2; i++ {
		mapping = append(mapping, fmt.Sprintf("com.example.C%d -> a%d:\n    1:1:void run():2:2 -> a\n", i, i)...)
	}
	raw, err := Encode(Parse(mapping))
	require.NoError(t, err)
	ix, err := Open(raw)
	require.NoError(t, err)

	for i := 0; i < memoLimit*2; i++ {
		frames := ix.Retrace(fmt.Sprintf("a%d", i), "a", 1)
		require.Len(t, frames, 1, "class %d", i)
		assert.Equal(t, fmt.Sprintf("com.example.C%d", i), frames[0].Class)
	}
	assert.LessOrEqual(t, len(ix.classes), memoLimit)
}

// The lookups have to be safe on an index that was never opened, since that is what a
// build with no symbols uploaded looks like.
func TestNilIndexAnswersNothing(t *testing.T) {
	var ix *Index
	assert.Nil(t, ix.Retrace("a", "b", 1))
	_, ok := ix.RetraceClass("a")
	assert.False(t, ok)
	assert.Empty(t, ix.SourceFile("a"))
}

// The checks against a real build are opt-in, since they need one:
//
//	go test ./backend/stacktraces/r8index/ -run RealMapping -v -mapping /path/to/mapping.txt
//
// Flags rather than environment variables, so that `go test -args -h` lists them, and
// because os.Getenv is banned throughout the backend.
var (
	realMappingPath = flag.String("mapping", "", "path to a release mapping.txt, for the opt-in checks against a real build")
	realIndexPath   = flag.String("index", "", "path to a mapping.v1.index the CLI built from -mapping")
)

// realMappingFile names the mapping to check against, skipping when there is none. For
// the tests that read it themselves, either to time the reading or to stream it.
func realMappingFile(t *testing.T) string {
	t.Helper()
	if *realMappingPath == "" {
		t.Skip("pass -mapping <release mapping.txt> to run this")
	}
	return *realMappingPath
}

func realMapping(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(realMappingFile(t))
	require.NoError(t, err)
	return data
}

func heapAllocMB() float64 {
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return float64(ms.HeapAlloc) / (1 << 20)
}

// TestRealMapping checks every class in a real build decodes to what the parser
// produced. Two lines per method name rather than every line of every entry: the
// exhaustive comparison is done on the small mappings above, and a real mapping has
// 480k entries whose blocks would be inflated once per probe.
func TestRealMapping(t *testing.T) {
	data := realMapping(t)
	m := Parse(data)
	require.NotZero(t, m.Classes())

	raw, err := Encode(m)
	require.NoError(t, err)
	fmt.Printf("\nmapping.txt : %.1f MB\nindex       : %.1f MB (%.0f%% of the mapping)\nclasses     : %d\n",
		float64(len(data))/(1<<20), float64(len(raw))/(1<<20),
		100*float64(len(raw))/float64(len(data)), m.Classes())

	ix, err := Open(raw)
	require.NoError(t, err)

	lookups := 0
	for obf, c := range m.classes {
		gotName, gotOK := ix.RetraceClass(obf)
		wantName, wantOK := m.RetraceClass(obf)
		require.Equal(t, wantOK, gotOK, obf)
		require.Equal(t, wantName, gotName, obf)

		for method, members := range c.members {
			line := 1
			if len(members) > 0 && members[0].hasRange {
				line = members[0].minStart
			}
			for _, probe := range []int{line, line + 1} {
				require.Equal(t, m.Retrace(obf, method, probe), ix.Retrace(obf, method, probe),
					"Retrace(%q, %q, %d)", obf, method, probe)
				lookups++
			}
		}
	}
	for original, file := range m.sourceFiles {
		require.Equal(t, file, ix.SourceFile(original), original)
	}
	fmt.Printf("compared    : %d lookups over %d classes\n", lookups, m.Classes())
}

// TestRealMappingFootprint is the number the format exists for: what has to stay in
// memory to answer lookups for one build. Each half takes its own baseline before it
// allocates anything, so neither is measured against the other's leftovers.
func TestRealMappingFootprint(t *testing.T) {
	path := realMappingFile(t)

	var withMapping, withIndex float64

	// The parsed mapping and the bytes it was built from, which is what a parse
	// holds: the names are slices of that text rather than copies of it.
	t.Run("parsed mapping", func(t *testing.T) {
		base := heapAllocMB()
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		m := Parse(data)
		require.NotZero(t, m.Classes())

		withMapping = heapAllocMB() - base
		runtime.KeepAlive(m)
		runtime.KeepAlive(data)
	})

	// An index, with the mapping it was built from already collected.
	t.Run("index", func(t *testing.T) {
		base := heapAllocMB()
		raw := func() []byte {
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			raw, err := Encode(Parse(data))
			require.NoError(t, err)
			return raw
		}()
		ix, err := Open(raw)
		require.NoError(t, err)

		withIndex = heapAllocMB() - base
		runtime.KeepAlive(ix)
	})

	fmt.Printf("\nretained, parsed mapping + its bytes : %6.1f MB\nretained, index                     : %6.1f MB\nratio                               : %6.1fx\n",
		withMapping, withIndex, withMapping/withIndex)
}
