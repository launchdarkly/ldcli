package symbols

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// receivedUpload is what a presigned PUT looked like on the wire.
type receivedUpload struct {
	Encoding string
	Length   int64
	Body     []byte
}

// uploadTestServer stands in for the presigned URL, recording the one PUT made to it.
func uploadTestServer(t *testing.T) (*httptest.Server, *receivedUpload) {
	t.Helper()
	var got receivedUpload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.Encoding = r.Header.Get("Content-Encoding")
		got.Length = r.ContentLength
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		got.Body = body
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func ungzip(t *testing.T, data []byte) []byte {
	t.Helper()
	reader, err := gzip.NewReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer reader.Close()
	out, err := io.ReadAll(reader)
	require.NoError(t, err)
	return out
}

func TestUploadFileSendsGzip(t *testing.T) {
	mapping := strings.Repeat("com.example.app.Class -> a.b.c:\n    12:12:void method() -> a\n", 500)
	path := filepath.Join(t.TempDir(), androidMappingFileName)
	require.NoError(t, os.WriteFile(path, []byte(mapping), 0o644))

	srv, got := uploadTestServer(t)
	stdout, _ := captureOutput(t, func() {
		require.NoError(t, uploadFile(path, srv.URL, androidMappingFileName))
	})

	assert.Equal(t, gzipEncoding, got.Encoding)
	assert.Equal(t, mapping, string(ungzip(t, got.Body)), "the artifact has to survive the round trip")
	assert.Less(t, len(got.Body), len(mapping)/10, "a mapping should compress by at least 10x")
	// S3 will not take a chunked upload, so the length has to be known up front.
	assert.Equal(t, int64(len(got.Body)), got.Length)
	assert.Contains(t, stdout, "gzipped to")
}

// An artifact that is already compressed can come out of gzip bigger. Storing that
// costs bytes and puts a reader a step further from the artifact, for nothing.
func TestUploadFileSendsIncompressibleFileAsIs(t *testing.T) {
	incompressible := make([]byte, 8192)
	_, err := rand.Read(incompressible)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "sources.srcbundle")
	require.NoError(t, os.WriteFile(path, incompressible, 0o644))

	srv, got := uploadTestServer(t)
	_, _ = captureOutput(t, func() {
		require.NoError(t, uploadFile(path, srv.URL, "sources.srcbundle"))
	})

	assert.Empty(t, got.Encoding)
	assert.Equal(t, incompressible, got.Body)
	assert.Equal(t, int64(len(incompressible)), got.Length)
}

func TestUploadFileReportsAMissingFile(t *testing.T) {
	srv, _ := uploadTestServer(t)
	assert.Error(t, uploadFile(filepath.Join(t.TempDir(), "absent.txt"), srv.URL, "absent.txt"))
}

// The digest that proves an object is already stored is compared against the ETag of
// the stored bytes, so it has to be taken after compression. Hashing the artifact
// instead would never match, and every source bundle would be re-sent forever.
func TestUploadBytesDigestDescribesTheStoredBytes(t *testing.T) {
	body := compressBody([]byte(strings.Repeat("package com.example;\n", 500)))
	require.Equal(t, gzipEncoding, body.Encoding, "this fixture is meant to compress")

	srv, got := uploadTestServer(t)
	_, _ = captureOutput(t, func() {
		require.NoError(t, uploadBytes(body, srv.URL, androidSourceBundleName))
	})

	assert.Equal(t, gzipEncoding, got.Encoding)
	assert.Equal(t, contentDigest(body.Data), contentDigest(got.Body))
}

func TestCompressBody(t *testing.T) {
	data := []byte(strings.Repeat("com.example.App -> a.a:\n", 500))

	body := compressBody(data)
	assert.Equal(t, gzipEncoding, body.Encoding)
	assert.Equal(t, len(data), body.RawSize)
	assert.Equal(t, data, ungzip(t, body.Data))
}

// A .srcbundle gzips its own entries, so there is nothing left for another pass to
// take out and the artifact is sent as it is.
func TestCompressBodyKeepsIncompressibleData(t *testing.T) {
	data := make([]byte, 4096)
	_, err := rand.Read(data)
	require.NoError(t, err)

	body := compressBody(data)
	assert.Empty(t, body.Encoding)
	assert.Equal(t, data, body.Data)
}

func TestCompressBodyOnEmptyData(t *testing.T) {
	body := compressBody(nil)
	assert.Empty(t, body.Encoding)
	assert.Empty(t, body.Data)
}

func TestByteSize(t *testing.T) {
	assert.Equal(t, "0 B", byteSize(0))
	assert.Equal(t, "512 B", byteSize(512))
	assert.Equal(t, "1.0 KB", byteSize(1024))
	assert.Equal(t, "1.5 MB", byteSize(1024*1024*3/2))
	assert.Equal(t, "61.3 MB", byteSize(64266122))
}
