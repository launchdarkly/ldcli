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

// flutterProject writes a minimal Flutter project (pubspec + lib file) and
// returns its root.
func flutterProject(t *testing.T, pkg, libRelPath, body string) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "pubspec.yaml"),
		[]byte("name: "+pkg+"\ndescription: test\n"), 0o644))
	full := filepath.Join(root, "lib", filepath.FromSlash(libRelPath))
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(body), 0o644))
	return root
}

// The app's own "package:" URIs resolve through pubspec.yaml to lib/, and the
// bundle is keyed by the URI the .dartmap stores rather than by a path.
func TestBuildFlutterSourceBundleResolvesAppPackageURIs(t *testing.T) {
	root := flutterProject(t, "my_app", "src/cart.dart", "class Cart {}\n")

	images := []flutter.Image{{Sources: map[string]string{
		"package:my_app/src/cart.dart": "package:my_app/src/cart.dart",
	}}}

	raw, n, err := buildFlutterSourceBundle(images, root)
	require.NoError(t, err)
	require.NotNil(t, raw)
	assert.Equal(t, 1, n)

	bundle, err := srcbundle.Open(raw)
	require.NoError(t, err)
	got, ok := bundle.File("package:my_app/src/cart.dart")
	require.True(t, ok, "the app's own package URI should be packed under that URI")
	assert.Equal(t, "class Cart {}\n", string(got))
}

// The SDK and dependency URIs must be left out entirely — never satisfied by a
// same-named file in the project, which would show the wrong code behind a real
// frame.
func TestBuildFlutterSourceBundleExcludesSDKAndDependencyURIs(t *testing.T) {
	root := flutterProject(t, "my_app", "ink_well.dart", "// the app's own file\n")

	images := []flutter.Image{{Sources: map[string]string{
		"package:flutter/src/material/ink_well.dart": "package:flutter/src/material/ink_well.dart",
		"package:http/src/client.dart":               "package:http/src/client.dart",
		"dart:async":                                 "dart:async",
		"org-dartlang-sdk:///sdk/lib/core/list.dart": "org-dartlang-sdk:///sdk/lib/core/list.dart",
	}}}

	raw, n, err := buildFlutterSourceBundle(images, root)
	require.Nil(t, raw, "nothing in this image belongs to the app")
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

// A file:// URI names a real path once its scheme is stripped, which is what the
// DWARF reader hands over.
func TestBuildFlutterSourceBundleReadsFileURIs(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "lib", "main.dart")
	require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, os.WriteFile(src, []byte("void main() {}\n"), 0o644))

	key := "file://" + filepath.ToSlash(src)
	images := []flutter.Image{{Sources: map[string]string{key: src}}}

	raw, n, err := buildFlutterSourceBundle(images, "")
	require.NoError(t, err)
	require.NotNil(t, raw)
	assert.Equal(t, 1, n)

	bundle, err := srcbundle.Open(raw)
	require.NoError(t, err)
	_, ok := bundle.File(key)
	assert.True(t, ok, "the bundle is keyed by the URI the map stores")
}

// A path recorded on the build machine is recovered from the local checkout.
func TestBuildFlutterSourceBundleSourcePathFallback(t *testing.T) {
	root := t.TempDir()
	local := filepath.Join(root, "lib", "main.dart")
	require.NoError(t, os.MkdirAll(filepath.Dir(local), 0o755))
	body := []byte("class Cart {}\n")
	require.NoError(t, os.WriteFile(local, body, 0o644))

	images := []flutter.Image{{Sources: map[string]string{
		"/ci/checkout/lib/main.dart": "/ci/checkout/lib/main.dart",
	}}}

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

// Without a readable pubspec there is no way to tell the app's package from a
// dependency's, so package URIs are left unresolved rather than guessed at.
func TestBuildFlutterSourceBundleWithoutPubspecSkipsPackageURIs(t *testing.T) {
	root := t.TempDir()
	lib := filepath.Join(root, "lib", "main.dart")
	require.NoError(t, os.MkdirAll(filepath.Dir(lib), 0o755))
	require.NoError(t, os.WriteFile(lib, []byte("void main() {}\n"), 0o644))

	images := []flutter.Image{{Sources: map[string]string{
		"package:my_app/main.dart": "package:my_app/main.dart",
	}}}

	raw, _, err := buildFlutterSourceBundle(images, root)
	require.NoError(t, err)
	assert.Nil(t, raw)
}

func TestFlutterAppPackage(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "pubspec.yaml"), []byte(
		"name: my_app # the app\ndescription: x\ndependencies:\n  http:\n    name: not_this\n",
	), 0o644))
	assert.Equal(t, "my_app", flutterAppPackage(root))

	assert.Equal(t, "", flutterAppPackage(t.TempDir()), "no pubspec, no package name")
	assert.Equal(t, "", flutterAppPackage(""))
}

func TestFlutterURISchemeTreatsWindowsPathsAsPaths(t *testing.T) {
	assert.Equal(t, "package", flutterURIScheme("package:my_app/main.dart"))
	assert.Equal(t, "dart", flutterURIScheme("dart:async"))
	assert.Equal(t, "file", flutterURIScheme("file:///a/b.dart"))
	assert.Equal(t, "", flutterURIScheme(`C:\src\main.dart`))
	assert.Equal(t, "", flutterURIScheme("lib/main.dart"))
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

// Every lane a map is stored on gets the bundle, because the backend reads it
// from whichever lane resolved the map, and each arch has its own Id lane.
func TestBuildFlutterMapsAttachesSourcesToEveryLane(t *testing.T) {
	assert.Equal(t, "_sym/flutter/id/a/sources.srcbundle", flutterSourceKeyBeside(flutterIDKey("a")))
	assert.Equal(t, "9/sources.srcbundle", flutterSourceKeyBeside(flutterVersionKey("9", "android-arm64")))
}
