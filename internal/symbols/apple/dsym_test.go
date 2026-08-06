package apple

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/blacktop/go-macho/pkg/swift"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/symbols/dsymmap"
)

// fixtureDWARF is the DWARF Mach-O inside the checked-in universal .dSYM.
// Regenerate with testdata/build.sh (UUIDs change on rebuild; the test asserts
// structure, not specific UUID values).
const fixtureDWARF = "testdata/symbolsdemo.dSYM/Contents/Resources/DWARF/symbolsdemo"

var uuidHex = regexp.MustCompile(`^[0-9A-F]{32}$`)

func TestBuildFromMachO_UniversalUUIDs(t *testing.T) {
	arches, err := BuildFromMachO(fixtureDWARF)
	require.NoError(t, err)
	require.Len(t, arches, 2, "fixture is a universal arm64+x86_64 binary")

	seen := map[string]bool{}
	for _, a := range arches {
		assert.Regexp(t, uuidHex, a.UUID, "UUID must be 32 uppercase hex chars, no dashes")
		assert.NotEqual(t, strings.Repeat("0", 32), a.UUID, "UUID must not be all zero")
		assert.False(t, seen[a.UUID], "each arch has a distinct UUID")
		seen[a.UUID] = true
		assert.NotZero(t, a.CPUType)
		require.NotNil(t, a.Builder)
	}
}

// TestBuildFromMachO_SymbolicatesInlineChain runs the full Stage 1 + Stage 2
// pipeline on every arch: build -> encode -> Open -> Lookup, asserting C++
// demangling and inline-frame recovery (demo::inner inlined into demo::outer).
func TestBuildFromMachO_SymbolicatesInlineChain(t *testing.T) {
	arches, err := BuildFromMachO(fixtureDWARF)
	require.NoError(t, err)

	for _, a := range arches {
		t.Run(archName(a.CPUType), func(t *testing.T) {
			outer := findFunc(a.Builder, "demo::outer")
			require.NotNil(t, outer, "expected a demangled demo::outer function")

			inl := findInline(outer, "demo::inner")
			require.NotNil(t, inl, "expected demo::inner inlined into demo::outer")

			var buf bytes.Buffer
			require.NoError(t, a.Builder.Encode(&buf))
			m, err := dsymmap.Open(buf.Bytes())
			require.NoError(t, err)
			assert.Equal(t, a.Builder.UUID, m.UUID())

			// Inside the inlined region: innermost demo::inner, then demo::outer.
			mid := inl.Start + (inl.End-inl.Start)/2
			frames := m.Lookup(mid)
			require.GreaterOrEqual(t, len(frames), 2, "inline lookup should expand to >= 2 frames")
			assert.Contains(t, frames[0].Function, "demo::inner")
			assert.Contains(t, frames[len(frames)-1].Function, "demo::outer")
			assert.True(t, strings.HasSuffix(frames[0].File, "symbolsdemo.cpp"),
				"innermost frame should carry a source file, got %q", frames[0].File)
			assert.NotZero(t, frames[0].Line)

			// The function entry is before the loop body, so no inline covers it.
			entry := m.Lookup(outer.Start)
			require.Len(t, entry, 1, "function entry should resolve to a single frame")
			assert.Contains(t, entry[0].Function, "demo::outer")

			// Far outside any function is a miss.
			assert.Nil(t, m.Lookup(0xFFFFFFF0))
		})
	}
}

func TestBuildFromMachO_MissingFile(t *testing.T) {
	_, err := BuildFromMachO("testdata/does-not-exist")
	require.Error(t, err)
}

// TestBestNameSwiftSimplified verifies that a private Swift declaration is
// rendered without its discriminator hash ("... in _<hash>"), which otherwise
// surfaces as a partial-looking symbol. Skips when no platform Swift demangler
// is available (the pure-Go engine passes symbols through unchanged).
func TestBestNameSwiftSimplified(t *testing.T) {
	const mangled = "$s12TestAppFruta17MainMenuViewModelC12runCatchable33_17034F2FACCE8EAB00EC5D8288C5BB0DLLyyAA13CrashScenarioOKFTf4nd_n"
	require.True(t, swift.IsMangled(mangled))

	simple, err := swift.DemangleSimple(mangled)
	if err != nil || simple == "" || simple == mangled {
		t.Skipf("swift demangler unavailable (engine=%s)", swift.EngineMode())
	}

	got := bestName("", mangled)
	assert.NotContains(t, got, "17034F2FACCE8EAB00EC5D8288C5BB0D", "private discriminator hash must be stripped")
	assert.Contains(t, got, "runCatchable")
}

func findFunc(b *dsymmap.Builder, nameSubstr string) *dsymmap.Function {
	for i := range b.Funcs {
		if strings.Contains(b.Funcs[i].Name, nameSubstr) {
			return &b.Funcs[i]
		}
	}
	return nil
}

func findInline(fn *dsymmap.Function, nameSubstr string) *dsymmap.Inline {
	for i := range fn.Inlines {
		if strings.Contains(fn.Inlines[i].Name, nameSubstr) {
			return &fn.Inlines[i]
		}
	}
	return nil
}

func archName(cpuType uint32) string {
	switch cpuType {
	case 0x0100000C:
		return "arm64"
	case 0x01000007:
		return "x86_64"
	default:
		return "cpu"
	}
}
