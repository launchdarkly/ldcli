package r8index

import (
	"bytes"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Streaming is an optimisation, so it has to be invisible: the same mapping has to
// encode to the same bytes whichever way it was read. Anything else would make the
// index no longer addressable by the mapping's content id.
func TestEncodeFromMatchesEncode(t *testing.T) {
	for name, mapping := range map[string]string{
		"sample":           sampleMapping,
		"synthetic lambda": syntheticLambdaMapping,
		"exception types":  exceptionTypeMapping,
		"crlf line ends":   strings.ReplaceAll(sampleMapping, "\n", "\r\n"),
		"no trailing newline": strings.TrimSuffix(`com.example.Cart -> a:
# {"id":"sourceFile","fileName":"Cart.kt"}
    1:1:void a():1:1 -> a
`, "\n"),
	} {
		t.Run(name, func(t *testing.T) {
			want, err := Encode(Parse([]byte(mapping)))
			require.NoError(t, err)

			got, err := EncodeFrom(strings.NewReader(mapping))
			require.NoError(t, err)

			assert.Equal(t, want, got)
		})
	}
}

// A file that is not a mapping has to be refused rather than turned into an index of
// nothing: the upload would otherwise succeed and the build would look symbolicated.
func TestEncodeFromRejectsInputWithoutClasses(t *testing.T) {
	for name, in := range map[string]string{
		"empty":        "",
		"prose":        "this is not a mapping\n",
		"header only":  "# compiler: R8\n# compiler_version: 8.5.35\n",
		"members only": "    1:3:void run():2:2 -> a\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := EncodeFrom(strings.NewReader(in))
			assert.Error(t, err)
		})
	}
}

// A line longer than bufio's default must not fail the upload, since how long a
// member line gets is up to the signatures in somebody's build.
func TestEncodeFromReadsVeryLongLines(t *testing.T) {
	args := strings.Repeat("java.lang.String,", 8*1024)
	mapping := fmt.Sprintf("com.example.Wide -> a:\n    1:1:void run(%s):2:2 -> a\n", args)
	require.Greater(t, len(mapping), 64*1024)

	raw, err := EncodeFrom(strings.NewReader(mapping))
	require.NoError(t, err)

	ix, err := Open(raw)
	require.NoError(t, err)
	frames := ix.Retrace("a", "a", 1)
	require.Len(t, frames, 1)
	assert.Equal(t, "com.example.Wide", frames[0].Class)
	assert.Equal(t, "run", frames[0].Method)
}

func TestEncodeFromReportsReadFailure(t *testing.T) {
	_, err := EncodeFrom(failingReader{})
	assert.Error(t, err)
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, assert.AnError }

// The point of streaming: what it costs to write an index should be the index rather
// than the mapping. What matters is the high-water mark, not what is left at the end
// — both ways end holding the same index — so this samples the live heap while the
// work runs instead of measuring it afterwards.
//
//	R8_MAPPING=/path/to/mapping.txt go test ./backend/stacktraces/r8index/ -run RealMappingStream -v
func TestRealMappingStreamFootprint(t *testing.T) {
	data := realMapping(t)

	var viaParse, viaStream []byte
	parseAndEncode := peakHeapMB(func() {
		var err error
		viaParse, err = Encode(Parse(data))
		require.NoError(t, err)
	})
	stream := peakHeapMB(func() {
		var err error
		viaStream, err = EncodeFrom(bytes.NewReader(data))
		require.NoError(t, err)
	})

	require.Equal(t, viaParse, viaStream, "streaming must not change the bytes")

	fmt.Printf("\nmapping.txt              : %6.1f MB\npeak, parse then encode  : %6.1f MB\npeak, streaming          : %6.1f MB\n",
		float64(len(data))/(1<<20), parseAndEncode, stream)
	assert.Less(t, stream, parseAndEncode, "streaming should peak below the parse it replaces")
}

// peakHeapMB is the highest live heap seen while f ran, over the bytes already
// allocated when it started. Sampled rather than read at the end, because the whole
// question is what the middle of the run needed.
func peakHeapMB(f func()) float64 {
	runtime.GC()
	var start runtime.MemStats
	runtime.ReadMemStats(&start)

	var peak atomic.Uint64
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			var ms runtime.MemStats
			runtime.ReadMemStats(&ms)
			if ms.HeapAlloc > peak.Load() {
				peak.Store(ms.HeapAlloc)
			}
			select {
			case <-done:
				return
			case <-time.After(time.Millisecond):
			}
		}
	}()

	f()
	close(done)
	<-stopped

	if peak.Load() < start.HeapAlloc {
		return 0
	}
	return float64(peak.Load()-start.HeapAlloc) / (1 << 20)
}

func TestEncodeFromRealMappingIsReadable(t *testing.T) {
	path := os.Getenv("R8_MAPPING")
	if path == "" {
		t.Skip("set R8_MAPPING to a release mapping.txt")
	}
	file, err := os.Open(path)
	require.NoError(t, err)
	defer file.Close()

	raw, err := EncodeFrom(file)
	require.NoError(t, err)

	ix, err := Open(raw)
	require.NoError(t, err)

	// Every class the parser found has to be there, answering the same thing.
	m := Parse(realMapping(t))
	for obf := range m.classes {
		wantName, wantOK := m.RetraceClass(obf)
		gotName, gotOK := ix.RetraceClass(obf)
		require.Equal(t, wantOK, gotOK, obf)
		require.Equal(t, wantName, gotName, obf)
	}
}
