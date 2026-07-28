package symbols

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/symbols/apple"
	"github.com/launchdarkly/ldcli/internal/symbols/srcbundle"
)

func TestAppleSourceKey(t *testing.T) {
	key := appleSourceKey("A5121984B70C3CA0BCC22FB671D75A20")
	assert.Equal(t, "_sym/apple/id/A5121984B70C3CA0BCC22FB671D75A20.srcbundle", key)
	// The source bundle sits beside the map under the same UUID.
	assert.Equal(t, strings.TrimSuffix(appleKey("A5121984B70C3CA0BCC22FB671D75A20"), appleSymbolExt),
		strings.TrimSuffix(key, appleSourceExt))
}

// buildAppleSourceBundle packs readable sources keyed exactly as the .dsymmap
// spells them, so a resolved frame's FileName is the lookup key.
func TestBuildAppleSourceBundle(t *testing.T) {
	dir := t.TempDir()
	swiftPath := filepath.Join(dir, "Cart.swift")
	require.NoError(t, os.WriteFile(swiftPath, []byte("import Foundation\nlet total = 1\n"), 0o644))

	a := apple.Arch{
		UUID:    "A5121984B70C3CA0BCC22FB671D75A20",
		Sources: map[string]string{"/build/Sources/Cart.swift": swiftPath},
	}
	data, nFiles, err := buildAppleSourceBundle(a)
	require.NoError(t, err)
	require.NotNil(t, data)
	assert.Equal(t, 1, nFiles)

	bundle, err := srcbundle.Open(data)
	require.NoError(t, err)
	// Keyed by the DWARF path, not the local path it was read from.
	_, content, _, ok := bundle.Window("/build/Sources/Cart.swift", 2, 2)
	require.True(t, ok)
	assert.Equal(t, "let total = 1\n", content)

	_, ok = bundle.File(swiftPath)
	assert.False(t, ok, "local path must not be a key")
}

func TestBuildAppleSourceBundleSkipsUnreadableAndNonSource(t *testing.T) {
	dir := t.TempDir()
	txtPath := filepath.Join(dir, "notes.txt")
	require.NoError(t, os.WriteFile(txtPath, []byte("not source\n"), 0o644))

	a := apple.Arch{
		UUID: "A5121984B70C3CA0BCC22FB671D75A20",
		Sources: map[string]string{
			"/build/notes.txt":          txtPath,                          // wrong extension
			"/build/Gone.swift":         filepath.Join(dir, "Gone.swift"), // not on this machine
			"/SDK/Vendor.swift":         filepath.Join(dir, "nope.swift"), // not on this machine
			"/build/Modules/module.map": filepath.Join(dir, "module.map"), // wrong extension
		},
	}
	data, nFiles, err := buildAppleSourceBundle(a)
	require.NoError(t, err)
	assert.Nil(t, data, "nothing bundleable yields no bundle")
	assert.Zero(t, nFiles)
}

func TestBuildAppleSourceBundleSkipsOversizeFile(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "Generated.swift")
	require.NoError(t, os.WriteFile(big, make([]byte, maxSourceFileBytes+1), 0o644))
	small := filepath.Join(dir, "Small.swift")
	require.NoError(t, os.WriteFile(small, []byte("let a = 1\n"), 0o644))

	a := apple.Arch{
		UUID: "A5121984B70C3CA0BCC22FB671D75A20",
		Sources: map[string]string{
			"/build/Generated.swift": big,
			"/build/Small.swift":     small,
		},
	}
	data, nFiles, err := buildAppleSourceBundle(a)
	require.NoError(t, err)
	require.NotNil(t, data)
	assert.Equal(t, 1, nFiles, "oversize file is skipped")

	bundle, err := srcbundle.Open(data)
	require.NoError(t, err)
	_, ok := bundle.File("/build/Small.swift")
	assert.True(t, ok)
	_, ok = bundle.File("/build/Generated.swift")
	assert.False(t, ok)
}

// The fixture dSYM's sources aren't checked out on this machine, so
// --include-sources must degrade to "map only" instead of failing.
func TestBuildAppleMapsWithSourcesDoesNotFail(t *testing.T) {
	image := filepath.Join(fixtureDSYM, "Contents", "Resources", "DWARF", "symbolsdemo")
	maps, err := buildAppleMaps([]string{image}, true)
	require.NoError(t, err)
	require.NotEmpty(t, maps)

	for _, m := range maps {
		if m.Kind == "sources" {
			assert.Equal(t, appleSourceKey(m.UUID), m.Key)
			_, err := srcbundle.Open(m.Data)
			require.NoError(t, err, "uploaded source bundle must decode")
		} else {
			assert.Equal(t, appleKey(m.UUID), m.Key)
		}
	}
}

// The DWARF walk must record the source paths the map references.
func TestBuildFromMachOCollectsSources(t *testing.T) {
	image := filepath.Join(fixtureDSYM, "Contents", "Resources", "DWARF", "symbolsdemo")
	arches, err := apple.BuildFromMachO(image)
	require.NoError(t, err)
	require.NotEmpty(t, arches)

	found := false
	for _, a := range arches {
		if len(a.Sources) > 0 {
			found = true
			for key, abs := range a.Sources {
				assert.NotEmpty(t, key)
				assert.True(t, filepath.IsAbs(abs), "%s should resolve to an absolute path", key)
			}
		}
	}
	assert.True(t, found, "fixture DWARF should reference at least one source file")
}

// The bug this guards: readability is not ownership. On any machine with Xcode
// the SDK headers a build referenced are present and read fine, so relying on a
// failed read to exclude them uploads Apple's code. Observed on a real dSYM,
// where all five referenced SDK headers were readable and every app source was
// not — the bundle would have contained nothing but Apple's headers.
func TestBuildAppleSourceBundleExcludesVendorSources(t *testing.T) {
	dir := t.TempDir()
	// Stand-ins for readable toolchain files, and one real app source.
	sdkHeader := filepath.Join(dir, "_ctype.h")
	toolchainSwift := filepath.Join(dir, "Concurrency.swift")
	appSwift := filepath.Join(dir, "Cart.swift")
	for _, p := range []string{sdkHeader, toolchainSwift, appSwift} {
		require.NoError(t, os.WriteFile(p, []byte("let x = 1\n"), 0o644))
	}

	a := apple.Arch{
		UUID: "A5121984B70C3CA0BCC22FB671D75A20",
		Sources: map[string]string{
			// Exactly as observed in a dSYM's DWARF.
			"/Applications/Xcode26.2.app/Contents/Developer/Platforms/iPhoneSimulator.platform/Developer/SDKs/iPhoneSimulator.sdk/usr/include/_ctype.h": sdkHeader,
			"/Applications/Xcode.app/Contents/Developer/Toolchains/XcodeDefault.xctoolchain/usr/lib/swift/Concurrency.swift":                            toolchainSwift,
			"/usr/include/stdio.h":                         sdkHeader,
			"/Library/Developer/CommandLineTools/SDKs/x.h": sdkHeader,
			"/Users/dev/MyApp/Sources/Cart.swift":          appSwift,
		},
	}

	data, nFiles, err := buildAppleSourceBundle(a)
	require.NoError(t, err)
	require.NotNil(t, data)
	assert.Equal(t, 1, nFiles, "only the app's own source is packed")

	bundle, err := srcbundle.Open(data)
	require.NoError(t, err)
	_, ok := bundle.File("/Users/dev/MyApp/Sources/Cart.swift")
	assert.True(t, ok)
	for _, vendor := range []string{
		"/Applications/Xcode26.2.app/Contents/Developer/Platforms/iPhoneSimulator.platform/Developer/SDKs/iPhoneSimulator.sdk/usr/include/_ctype.h",
		"/Applications/Xcode.app/Contents/Developer/Toolchains/XcodeDefault.xctoolchain/usr/lib/swift/Concurrency.swift",
		"/usr/include/stdio.h",
		"/Library/Developer/CommandLineTools/SDKs/x.h",
	} {
		_, ok := bundle.File(vendor)
		assert.False(t, ok, "vendor source must not be uploaded: %s", vendor)
	}
}

func TestIsAppleVendorSource(t *testing.T) {
	vendor := []string{
		"/Applications/Xcode26.2.app/Contents/Developer/Platforms/iPhoneSimulator.platform/Developer/SDKs/iPhoneSimulator.sdk/usr/include/_ctype.h",
		"/Applications/Xcode-beta.app/Contents/Developer/Toolchains/XcodeDefault.xctoolchain/usr/lib/swift/shims/x.h",
		"/Library/Developer/CommandLineTools/usr/include/c++/v1/vector",
		"/System/Library/Frameworks/Foundation.framework/Headers/NSObject.h",
		"/usr/include/stdio.h",
		"/usr/lib/swift/shims/HeapObject.h",
		"/opt/homebrew/include/zlib.h",
	}
	for _, p := range vendor {
		assert.True(t, isAppleVendorSource(p), "expected vendor: %s", p)
	}

	own := []string{
		"/Users/dev/MyApp/Sources/Cart.swift",
		"/Users/dev/MyApp/Pods/Alamofire/Source/Request.swift",
		"/Users/dev/Library/Developer/Xcode/DerivedData/App-abc/SourcePackages/checkouts/swift-log/Logging.swift",
		"/builds/ci/project/App/AppDelegate.swift",
		"/Users/dev/System Design/App.swift", // not the /System root
	}
	for _, p := range own {
		assert.False(t, isAppleVendorSource(p), "expected project source: %s", p)
	}
}
