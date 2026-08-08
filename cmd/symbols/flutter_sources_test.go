package symbols

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/launchdarkly/ldcli/internal/symbols/flutter"
	"github.com/launchdarkly/ldcli/internal/symbols/srcbundle"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildFlutterSourceBundleFromDWARFPaths(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "lib", "main.dart")
	require.NoError(t, os.MkdirAll(filepath.Dir(mainPath), 0o755))
	require.NoError(t, os.WriteFile(mainPath, []byte("void main() {}\n"), 0o644))

	images := []flutter.Image{{
		Sources: map[string]string{
			mainPath: mainPath,
			"/Users/dev/.pub-cache/hosted/pub.dev/http-1.0.0/lib/http.dart": "/Users/dev/.pub-cache/hosted/pub.dev/http-1.0.0/lib/http.dart",
		},
	}}

	raw, n, err := buildFlutterSourceBundle(images, "")
	require.NoError(t, err)
	require.NotNil(t, raw)
	assert.Equal(t, 1, n)

	bundle, err := srcbundle.Open(raw)
	require.NoError(t, err)
	_, ok := bundle.File(mainPath)
	assert.True(t, ok, "project dart file should be in the bundle")
	_, ok = bundle.File("/Users/dev/.pub-cache/hosted/pub.dev/http-1.0.0/lib/http.dart")
	assert.False(t, ok, "pub-cache paths must be excluded")
}

func TestBuildFlutterSourceBundleSourcePathFallback(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "lib", "main.dart")
	require.NoError(t, os.MkdirAll(filepath.Dir(mainPath), 0o755))
	body := []byte("class Cart {}\n")
	require.NoError(t, os.WriteFile(mainPath, body, 0o644))

	// DWARF recorded a build-machine absolute path that isn't here.
	images := []flutter.Image{{
		Sources: map[string]string{
			"/ci/checkout/lib/main.dart": "/ci/checkout/lib/main.dart",
		},
	}}

	raw, n, err := buildFlutterSourceBundle(images, root)
	require.NoError(t, err)
	require.NotNil(t, raw)
	assert.Equal(t, 1, n)

	bundle, err := srcbundle.Open(raw)
	require.NoError(t, err)
	got, ok := bundle.File("/ci/checkout/lib/main.dart")
	require.True(t, ok)
	assert.Equal(t, body, got)
}

func TestFlutterSourceKeyBeside(t *testing.T) {
	assert.Equal(t,
		"_sym/flutter/id/abc123/sources.srcbundle",
		flutterSourceKeyBeside("_sym/flutter/id/abc123/app.dartmap"),
	)
	assert.Equal(t,
		"1.2.3/sources.srcbundle",
		flutterSourceKeyBeside("1.2.3/app.android-arm64.dartmap"),
	)
}

func TestIsFlutterVendorSource(t *testing.T) {
	assert.True(t, isFlutterVendorSource("/Users/dev/.pub-cache/hosted/pub.dev/foo/lib/a.dart"))
	assert.True(t, isFlutterVendorSource("/sdk/flutter/packages/flutter/lib/material.dart"))
	assert.False(t, isFlutterVendorSource("/Users/dev/myapp/lib/main.dart"))
}
