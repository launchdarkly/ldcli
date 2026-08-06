package symbols

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/symbols/r8index"
)

const testAndroidMapping = `# compiler: R8
com.example.app.CheckoutDemo -> a.b.c:
# {"id":"sourceFile","fileName":"SymbolicationDemo.kt"}
    12:14:int startCheckout(java.lang.String):40:42 -> a
`

// writeAndroidMapping puts a mapping on disk and returns its path, which is the input
// an Android upload starts from.
func writeMappingFile(t *testing.T, mapping string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, androidMappingFileName)
	require.NoError(t, os.WriteFile(path, []byte(mapping), 0o644))
	return path
}

// What the backend reads is the index, on both lanes: a build's own id, and the app
// version a crash without an id falls back to. The mapping text itself is never stored,
// because nothing reads it.
func TestBuildAndroidObjectsIndexesBothLanes(t *testing.T) {
	path := writeMappingFile(t, testAndroidMapping)

	objects, err := buildAndroidObjects(path, "1.2.3", "deadbeef", false, "")
	require.NoError(t, err)
	require.Len(t, objects, 2)

	assert.Equal(t, "_sym/android/id/deadbeef/mapping.v1.index", objects[0].Key())
	assert.Equal(t, "1.2.3/mapping.v1.index", objects[1].Key())
	assert.Equal(t, objects[0].Data, objects[1].Data, "one build, one index")

	for _, object := range objects {
		assert.NotContains(t, object.Key(), androidMappingFileName)
	}

	// An object is only worth storing if it answers what symbolication asks.
	ix, err := r8index.Open(objects[0].Data)
	require.NoError(t, err)
	frames := ix.Retrace("a.b.c", "a", 13)
	require.Len(t, frames, 1)
	assert.Equal(t, "com.example.app.CheckoutDemo", frames[0].Class)
	assert.Equal(t, "startCheckout", frames[0].Method)
	assert.Equal(t, 41, frames[0].Line)
	assert.Equal(t, "SymbolicationDemo.kt", ix.SourceFile("com.example.app.CheckoutDemo"))
}

// The Id Lane is the one symbolication tries first, so its key is the one that proves
// what is stored under it. A Version Lane object is the last build's.
func TestBuildAndroidObjectsOnlyIdLaneKeyProvesContent(t *testing.T) {
	objects, err := buildAndroidObjects(writeMappingFile(t, testAndroidMapping), "1.2.3", "deadbeef", false, "")
	require.NoError(t, err)
	require.Len(t, objects, 2)

	assert.True(t, objects[0].keyProvesContent)
	assert.False(t, objects[1].keyProvesContent)
}

// Either lane on its own is a complete upload: an app that reports an id needs no
// version, and a build with no id is still symbolicated by the version it shipped.
func TestBuildAndroidObjectsOneLane(t *testing.T) {
	path := writeMappingFile(t, testAndroidMapping)

	idOnly, err := buildAndroidObjects(path, "", "deadbeef", false, "")
	require.NoError(t, err)
	require.Len(t, idOnly, 1)
	assert.Equal(t, "_sym/android/id/deadbeef/mapping.v1.index", idOnly[0].Key())

	versionOnly, err := buildAndroidObjects(path, "1.2.3", "", false, "")
	require.NoError(t, err)
	require.Len(t, versionOnly, 1)
	assert.Equal(t, "1.2.3/mapping.v1.index", versionOnly[0].Key())
}

// With neither an id nor a version there is no key a crash could ever look under, so
// the upload is refused rather than written somewhere nothing reads.
func TestBuildAndroidObjectsRequiresALane(t *testing.T) {
	_, err := buildAndroidObjects(writeMappingFile(t, testAndroidMapping), "", "", false, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), appVersionFlag)
}

// A mapping that yields no index is reported here. Uploading nothing usable would
// leave the build looking symbolicated while every crash arrives obfuscated.
func TestBuildAndroidObjectsRejectsUnusableMapping(t *testing.T) {
	path := writeMappingFile(t, "this is not a mapping\n")

	_, err := buildAndroidObjects(path, "1.2.3", "deadbeef", false, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), androidMappingFileName)
	assert.Contains(t, err.Error(), "minified")
}

// Sources go on the lane symbolication resolves first, since that is where it looks
// for them; the fallback lane getting an index without sources still retraces.
func TestBuildAndroidObjectsPlacesSourcesOnTheFirstLane(t *testing.T) {
	sourceRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(sourceRoot, "main/java/com/example/app"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(sourceRoot, "main/java/com/example/app/CheckoutDemo.kt"),
		[]byte("package com.example.app\n\nobject CheckoutDemo\n"), 0o644))

	objects, err := buildAndroidObjects(writeMappingFile(t, testAndroidMapping), "1.2.3", "deadbeef", true, sourceRoot)
	require.NoError(t, err)
	require.Len(t, objects, 3)

	assert.Equal(t, "_sym/android/id/deadbeef/mapping.v1.index", objects[0].Key())
	assert.Equal(t, "_sym/android/id/deadbeef/"+androidSourceBundleName, objects[1].Key())
	assert.Equal(t, "1.2.3/mapping.v1.index", objects[2].Key())

	// The bundle is keyed by the mapping's id rather than its own contents, so its
	// key cannot stand in for its bytes even in the Id Lane.
	assert.False(t, objects[1].keyProvesContent)
}

// Without --include-sources nothing but the index is stored.
func TestBuildAndroidObjectsWithoutSources(t *testing.T) {
	objects, err := buildAndroidObjects(writeMappingFile(t, testAndroidMapping), "1.2.3", "deadbeef", false, t.TempDir())
	require.NoError(t, err)
	for _, object := range objects {
		assert.NotContains(t, object.Key(), androidSourceBundleName)
	}
}

// Two mappings cannot be from one build, and the keys describe one build, so the
// ambiguity is reported rather than resolved by picking.
func TestFindAndroidMappingRefusesSeveral(t *testing.T) {
	dir := t.TempDir()
	for _, variant := range []string{"release", "composeRelease"} {
		sub := filepath.Join(dir, variant)
		require.NoError(t, os.MkdirAll(sub, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(sub, androidMappingFileName), []byte(testAndroidMapping), 0o644))
	}

	_, err := findAndroidMapping(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), pathFlag)
}

// androidUploadServer answers the URL request with a presigned URL per key pointing at
// itself, and records what is PUT to each. skip names the keys to answer with "",
// which is how the backend says it already has one.
type androidUploadServer struct {
	*httptest.Server
	requested []capturedRequest
	stored    map[string][]byte
	encodings map[string]string
}

func newAndroidUploadServer(t *testing.T, skip ...string) *androidUploadServer {
	t.Helper()
	srv := &androidUploadServer{stored: map[string][]byte{}, encodings: map[string]string{}}
	skipped := make(map[string]bool, len(skip))
	for _, key := range skip {
		skipped[key] = true
	}

	srv.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			key := strings.TrimPrefix(r.URL.Path, "/put/")
			srv.stored[key] = body
			srv.encodings[key] = r.Header.Get("Content-Encoding")
			return
		}

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var captured capturedRequest
		require.NoError(t, json.Unmarshal(body, &captured))
		srv.requested = append(srv.requested, captured)

		paths, ok := captured.Variables["paths"].([]interface{})
		require.True(t, ok, "a URL request always names the keys it wants")
		urls := make([]string, 0, len(paths))
		for _, path := range paths {
			key := path.(string)
			if skipped[key] {
				urls = append(urls, "")
				continue
			}
			urls = append(urls, srv.URL+"/put/"+key)
		}

		answer, err := json.Marshal(map[string]interface{}{
			"data": map[string]interface{}{"get_symbol_upload_urls_ld": urls},
		})
		require.NoError(t, err)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(answer)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// End to end over a fake backend: the index reaches both lanes, and the Version Lane
// key — which proves nothing about its contents — carries a digest of exactly the bytes
// that were sent.
func TestUploadAndroidSymbolsSendsIndexToBothLanes(t *testing.T) {
	srv := newAndroidUploadServer(t)
	path := writeMappingFile(t, testAndroidMapping)

	_, _ = captureOutput(t, func() {
		require.NoError(t, uploadAndroidSymbols("key", "proj", path, "1.2.3", "deadbeef", srv.URL, false, "", true))
	})

	idKey := "_sym/android/id/deadbeef/mapping.v1.index"
	versionKey := "1.2.3/mapping.v1.index"
	require.Contains(t, srv.stored, idKey)
	require.Contains(t, srv.stored, versionKey)
	assert.Equal(t, srv.stored[idKey], srv.stored[versionKey])

	// The stored bytes are the index, whatever the transport did to them.
	stored := srv.stored[idKey]
	if srv.encodings[idKey] == gzipEncoding {
		stored = gunzip(t, stored)
	}
	_, err := r8index.Open(stored)
	require.NoError(t, err)

	require.Len(t, srv.requested, 1)
	digests, ok := srv.requested[0].Variables[digestsArgument].([]interface{})
	require.True(t, ok, "the Version Lane copy needs a digest to be settled by")
	assert.Equal(t, "", digests[0], "the Id Lane key already proves what is under it")
	assert.Equal(t, contentDigest(srv.stored[versionKey]), digests[1],
		"the digest has to describe the bytes that get stored")
}

// A build re-uploaded unchanged skips the Id Lane copy by key alone, and the Version
// Lane copy still goes: that key is a version, and only the digest could have settled
// it.
func TestUploadAndroidSymbolsSkipsWhatTheBackendHas(t *testing.T) {
	idKey := "_sym/android/id/deadbeef/mapping.v1.index"
	srv := newAndroidUploadServer(t, idKey)
	path := writeMappingFile(t, testAndroidMapping)

	stdout, _ := captureOutput(t, func() {
		require.NoError(t, uploadAndroidSymbols("key", "proj", path, "1.2.3", "deadbeef", srv.URL, false, "", true))
	})

	assert.NotContains(t, srv.stored, idKey)
	assert.Contains(t, srv.stored, "1.2.3/mapping.v1.index")
	assert.Contains(t, stdout, "Skipping")
}

func gunzip(t *testing.T, data []byte) []byte {
	t.Helper()
	reader, err := gzip.NewReader(bytes.NewReader(data))
	require.NoError(t, err)
	out, err := io.ReadAll(reader)
	require.NoError(t, err)
	return out
}
