package symbols

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/symbols/ldsm"
)

// fixtureDSYM is the checked-in universal dSYM shared with the apple package's
// golden test. It contains demo::outer with demo::inner inlined.
const fixtureDSYM = "../../internal/symbols/apple/testdata/symbolsdemo.dSYM"

var uuidHex = regexp.MustCompile(`^[0-9A-F]{32}$`)

func TestAppleKey(t *testing.T) {
	assert.Equal(t, "_sym/apple/id/ABC123", appleKey("ABC123"))
}

func TestFindDSYMImages_Bundle(t *testing.T) {
	images, err := findDSYMImages(fixtureDSYM)
	require.NoError(t, err)
	require.Len(t, images, 1, "the fixture bundle has one DWARF image")
	assert.True(t, strings.HasSuffix(images[0], "Contents/Resources/DWARF/symbolsdemo"))
	_, statErr := os.Stat(images[0])
	require.NoError(t, statErr)
}

func TestFindDSYMImages_TreeWalk(t *testing.T) {
	// Pointing at the parent directory should still discover the .dSYM bundle.
	parent := filepath.Dir(fixtureDSYM)
	images, err := findDSYMImages(parent)
	require.NoError(t, err)
	require.NotEmpty(t, images)
	assert.True(t, strings.HasSuffix(images[0], "Contents/Resources/DWARF/symbolsdemo"))
}

func TestFindDSYMImages_DirectFile(t *testing.T) {
	image := filepath.Join(fixtureDSYM, "Contents", "Resources", "DWARF", "symbolsdemo")
	images, err := findDSYMImages(image)
	require.NoError(t, err)
	assert.Equal(t, []string{image}, images)
}

func TestFindDSYMImages_Missing(t *testing.T) {
	_, err := findDSYMImages("testdata/nope")
	require.Error(t, err)
}

// TestBuildAppleMaps exercises the command's build/encode/dedupe step against
// the real fixture and confirms the produced bytes are valid, keyed .ldsm maps.
func TestBuildAppleMaps(t *testing.T) {
	image := filepath.Join(fixtureDSYM, "Contents", "Resources", "DWARF", "symbolsdemo")
	maps, err := buildAppleMaps([]string{image})
	require.NoError(t, err)
	require.Len(t, maps, 2, "universal fixture yields arm64 + x86_64 maps")

	keys := map[string]bool{}
	for _, m := range maps {
		assert.Regexp(t, uuidHex, m.UUID)
		assert.Equal(t, appleKey(m.UUID), m.Key)
		assert.True(t, strings.HasPrefix(m.Key, "_sym/apple/id/"))
		assert.False(t, keys[m.Key], "each arch gets a distinct key")
		keys[m.Key] = true

		// The uploaded bytes must decode as a valid .ldsm the backend can read.
		parsed, err := ldsm.Open(m.Data)
		require.NoError(t, err)
		assert.Equal(t, strings.ToUpper(m.UUID), keySuffixHex(m.Key))
		_ = parsed
	}
}

// TestBuildAppleMaps_DedupesUUID ensures the same image passed twice does not
// produce duplicate uploads.
func TestBuildAppleMaps_DedupesUUID(t *testing.T) {
	image := filepath.Join(fixtureDSYM, "Contents", "Resources", "DWARF", "symbolsdemo")
	maps, err := buildAppleMaps([]string{image, image})
	require.NoError(t, err)
	assert.Len(t, maps, 2, "duplicate images must be deduplicated by UUID")
}

func TestArchLabel(t *testing.T) {
	assert.Equal(t, "arm64", archLabel(0x0100000C))
	assert.Equal(t, "x86_64", archLabel(0x01000007))
	assert.Contains(t, archLabel(0xdeadbeef), "cpu-0x")
}

func keySuffixHex(key string) string {
	return key[strings.LastIndex(key, "/")+1:]
}
