package symbols

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The header R8 8.13 writes, trimmed to what is read from it.
const testMapIDHeader = `# compiler: R8
# compiler_version: 8.13.19
# min_api: 23
# common_typos_disable
# {"id":"com.android.tools.r8.mapping","version":"2.2"}
# pg_map_id: 92d0222f1a7a3b92fca00ddc75fbcf893c89be03e02286414d51abcfd9b02063
# pg_map_hash: SHA-256 92d0222f1a7a3b92fca00ddc75fbcf893c89be03e02286414d51abcfd9b02063
`

const testMapID = "92d0222f1a7a3b92fca00ddc75fbcf893c89be03e02286414d51abcfd9b02063"

func TestAndroidMapIDReadsTheHeader(t *testing.T) {
	path := writeMappingFile(t, testMapIDHeader+testAndroidMapping)
	assert.Equal(t, testMapID, androidMapID(path))
}

// R8 shortened the id before AGP 8.12, and a build being retraced today may well
// have been produced by one of those.
func TestAndroidMapIDReadsAShortID(t *testing.T) {
	path := writeMappingFile(t, "# compiler: R8\n# pg_map_id: 92d0222\n"+testAndroidMapping)
	assert.Equal(t, "92d0222", androidMapID(path))
}

// Nothing here may fail an upload: an id that cannot be read leaves the mapping on
// the Version Lane, which is where it was before R8 recorded one.
func TestAndroidMapIDWithoutOne(t *testing.T) {
	cases := map[string]string{
		"no header at all":         testAndroidMapping,
		"a ProGuard mapping":       "com.example.app.CheckoutDemo -> a.b.c:\n",
		"an id that is not a hash": "# compiler: R8\n# pg_map_id: release-7\n" + testAndroidMapping,
		"an empty id":              "# compiler: R8\n# pg_map_id:\n" + testAndroidMapping,
	}
	for name, mapping := range cases {
		assert.Emptyf(t, androidMapID(writeMappingFile(t, mapping)), "androidMapID of %s", name)
	}

	assert.Empty(t, androidMapID(filepath.Join(t.TempDir(), "nothing-here.txt")))
}

// The header ends at the first class, and what follows is tens of megabytes of a
// build's own strings — including, in an app that has one, a class whose name would
// read as a header line to anything still looking.
func TestAndroidMapIDStopsAtTheFirstClass(t *testing.T) {
	path := writeMappingFile(t, testAndroidMapping+"# pg_map_id: "+testMapID+"\n")
	assert.Empty(t, androidMapID(path))
}

// The point of the id: a build that stamps nothing and is told nothing still uploads
// to the lane its crashes will arrive on, because R8 recorded which mapping this is
// and the shipped app reports the same thing on every frame.
func TestBuildAndroidObjectsKeysByTheMapID(t *testing.T) {
	path := writeMappingFile(t, testMapIDHeader+testAndroidMapping)

	objects, err := buildAndroidObjects(path, "", "", false, "")
	require.NoError(t, err)
	require.Len(t, objects, 1)
	assert.Equal(t, "_sym/android/id/"+testMapID+"/mapping.v1.index", objects[0].Key())
	assert.True(t, objects[0].keyProvesContent, "the key is derived from the mapping it stores")
}

// An id the caller gave, or one read out of the packaged app, is a build saying what
// it will report — which is the more specific answer, and the only one for a build
// old enough that R8 stamps nothing into the app itself.
func TestBuildAndroidObjectsPrefersAReportedID(t *testing.T) {
	path := writeMappingFile(t, testMapIDHeader+testAndroidMapping)

	objects, err := buildAndroidObjects(path, "", "deadbeef", false, "")
	require.NoError(t, err)
	require.Len(t, objects, 1)
	assert.Equal(t, "_sym/android/id/deadbeef/mapping.v1.index", objects[0].Key())
}
