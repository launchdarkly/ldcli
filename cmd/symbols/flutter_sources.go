package symbols

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/launchdarkly/ldcli/internal/symbols/flutter"
	"github.com/launchdarkly/ldcli/internal/symbols/srcbundle"
)

// Flutter source bundling.
//
// Dart names a compilation unit by its script URI, so what a .dartmap stores as a
// frame's file — and therefore what the bundle has to be keyed by — is rarely a
// path. It is "package:my_app/main.dart" for the app's own code, "dart:async" or
// "org-dartlang-sdk:///..." for the SDK, "package:<dep>/..." for a dependency,
// and only sometimes a plain or file:// path.
//
// Keys are always the URI exactly as the map spells it, because that is what the
// backend looks a frame up by. Which file on this machine backs a key is a
// separate question, answered here: a package URI resolves through the app's
// pubspec name to lib/, and anything belonging to the SDK or to a dependency is
// left out rather than guessed at.

// flutterSourceBundleName is the object name of the source bundle uploaded beside a
// build's .dartmap, so a map and the sources behind it share one key prefix:
// _sym/flutter/id/<symbolsID>/app.dartmap and .../sources.srcbundle.
const flutterSourceBundleName = "sources.srcbundle"

// flutterSourceExtensions are the file types the UI can render as Dart source context.
var flutterSourceExtensions = map[string]bool{
	".dart": true,
}

// flutterVendorSchemes are the URI schemes naming code that ships with Dart or
// Flutter rather than being written by the developer.
var flutterVendorSchemes = map[string]bool{
	"dart":                            true,
	"org-dartlang-sdk":                true,
	"org-dartlang-app":                true,
	"org-dartlang-untranslatable-uri": true,
}

// flutterVendorPathMarkers appear mid-path in Flutter/Dart SDK and pub-cache
// trees, for the builds whose DWARF records real paths instead of package URIs.
var flutterVendorPathMarkers = []string{
	"/.pub-cache/",
	"/pub-cache/",
	"/flutter/packages/flutter/",
	"/flutter/packages/flutter_test/",
	"/flutter/packages/flutter_driver/",
	"/flutter/packages/flutter_localizations/",
	"/flutter/packages/flutter_web_plugins/",
	"/flutter/bin/cache/",
	"/third_party/dart/",
	"/hosted/pub.dev/",
	"/hosted/pub.dartlang.org/",
}

// isFlutterVendorSource reports whether a filesystem path belongs to the
// Flutter/Dart SDK or the pub cache rather than to the project being uploaded.
func isFlutterVendorSource(p string) bool {
	lower := strings.ToLower(filepath.ToSlash(p))
	for _, marker := range flutterVendorPathMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// buildFlutterSourceBundle packs the project's .dart sources referenced by the
// images' DWARF into a .srcbundle, keyed by the URI each one is recorded under so
// a resolved frame's FileName is the lookup key.
//
// Only the app's own code is packed. The SDK and every dependency are excluded,
// which matters for more than upload size: a bundle that answered
// "package:flutter/src/material/ink_well.dart" with whatever local file happened
// to share its name would put the wrong code behind a real frame.
//
// Returns nil when nothing was found, so the caller can skip the upload.
func buildFlutterSourceBundle(images []flutter.Image, sourceRoot string) ([]byte, int, error) {
	// Merge the arches: one release's Dart sources are identical across them, and
	// a crash can arrive from any, so every lane gets the same bundle.
	merged := make(map[string]string)
	for _, img := range images {
		for key, resolved := range img.Sources {
			if _, ok := merged[key]; !ok {
				merged[key] = resolved
			}
		}
	}

	appPkg := flutterAppPackage(sourceRoot)
	byBase := indexDartFilesByBase(sourceRoot)

	// Sorted so the same build always produces the same bundle, including which
	// files are dropped if the size budget runs out.
	keys := make([]string, 0, len(merged))
	for key := range merged {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	b := &srcbundle.Builder{}
	total := 0
	for _, key := range keys {
		if !flutterSourceExtensions[strings.ToLower(path.Ext(flutterURIPath(key)))] {
			continue
		}
		local, ok := flutterSourceFile(key, merged[key], appPkg, sourceRoot, byBase)
		if !ok {
			continue
		}
		data, err := os.ReadFile(local)
		if err != nil {
			continue // built elsewhere: the backend renders the frame without source
		}
		if len(data) > maxSourceFileBytes || total+len(data) > maxSourceBundleBytes {
			continue
		}
		total += len(data)
		b.Add(key, data)
	}
	if b.Len() == 0 {
		return nil, 0, nil
	}

	var buf bytes.Buffer
	if err := b.Encode(&buf); err != nil {
		return nil, 0, fmt.Errorf("failed to encode Flutter source bundle: %w", err)
	}
	return buf.Bytes(), b.Len(), nil
}

// flutterSourceFile returns the file on this machine to pack under key, or
// ok=false when key names code that is not the project's or cannot be found.
//
// resolved is what the DWARF reader made of the name: a path for the plain and
// file:// forms, the URI itself otherwise.
func flutterSourceFile(key, resolved, appPkg, sourceRoot string, byBase map[string][]string) (string, bool) {
	scheme, rest, isURI := strings.Cut(key, ":")
	if isURI && flutterURIScheme(key) != "" {
		if flutterVendorSchemes[strings.ToLower(scheme)] {
			return "", false
		}
		if strings.EqualFold(scheme, "package") {
			// package:<pkg>/<path> is <pkg>'s lib/<path>. Only the app's own
			// package can be resolved from the project root; a dependency's
			// lives in the pub cache and is not ours to upload.
			pkg, within, found := strings.Cut(strings.TrimPrefix(rest, "//"), "/")
			if !found || appPkg == "" || !strings.EqualFold(pkg, appPkg) {
				return "", false
			}
			return filepath.Join(sourceRoot, "lib", filepath.FromSlash(within)), true
		}
		if !strings.EqualFold(scheme, "file") {
			return "", false // an unknown scheme is not a path to guess at
		}
	}

	// A path: either as the build machine spelled it, or the same file in this
	// checkout. Both are checked against the vendor markers, since the resolved
	// path is what establishes whose code it is.
	if isFlutterVendorSource(key) || isFlutterVendorSource(resolved) {
		return "", false
	}
	if _, err := os.Stat(resolved); err == nil {
		return resolved, true
	}
	if alt := resolveFlutterSourceFallback(resolved, byBase); alt != "" {
		return alt, true
	}
	return "", false
}

// flutterURIScheme returns the scheme of a Dart script URI, or "" when the name
// is a filesystem path. A Windows drive letter is a path, not a scheme.
func flutterURIScheme(name string) string {
	i := strings.Index(name, ":")
	if i <= 1 {
		return ""
	}
	for j := 0; j < i; j++ {
		c := name[j]
		if !(c >= 'a' && c <= 'z') && !(c >= 'A' && c <= 'Z') && !(c >= '0' && c <= '9') && c != '-' && c != '+' && c != '.' {
			return ""
		}
	}
	return name[:i]
}

// flutterURIPath strips a scheme so the extension can be read off either form.
func flutterURIPath(name string) string {
	if scheme := flutterURIScheme(name); scheme != "" {
		return strings.TrimPrefix(name[len(scheme)+1:], "//")
	}
	return name
}

// flutterAppPackage reads the package name out of the project's pubspec.yaml,
// which is what its own "package:" URIs are keyed by. Returns "" when there is no
// readable pubspec, in which case package URIs are left unresolved rather than
// guessed at.
func flutterAppPackage(sourceRoot string) string {
	if sourceRoot == "" {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(sourceRoot, "pubspec.yaml"))
	if err != nil {
		return ""
	}

	for line := range strings.SplitSeq(string(raw), "\n") {
		line = strings.TrimSuffix(line, "\r")
		// Top level only: a "name:" indented under dependencies belongs to
		// something else.
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			continue
		}
		rest, ok := strings.CutPrefix(line, "name:")
		if !ok {
			continue
		}
		name := strings.TrimSpace(rest)
		name = strings.Trim(name, `"'`)
		if i := strings.Index(name, "#"); i >= 0 {
			name = strings.TrimSpace(name[:i])
		}
		return name
	}
	return ""
}

// indexDartFilesByBase maps basename → paths under root, for recovering a file
// whose recorded path belongs to the machine that built it. Empty when root is
// blank or unreadable.
func indexDartFilesByBase(root string) map[string][]string {
	out := make(map[string][]string)
	if root == "" {
		return out
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return out
	}
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".dart_tool", "build", ".git", ".pub-cache":
				return filepath.SkipDir
			}
			return nil
		}
		if !flutterSourceExtensions[strings.ToLower(filepath.Ext(d.Name()))] {
			return nil
		}
		if isFlutterVendorSource(p) {
			return nil
		}
		out[d.Name()] = append(out[d.Name()], p)
		return nil
	})
	return out
}

// resolveFlutterSourceFallback picks a file from the local checkout for a
// recorded path that is not readable here. A unique basename match is taken; when
// several files share the name, only one whose path ends with the recorded path
// is, since anything else would be a coin flip between same-named files.
func resolveFlutterSourceFallback(recorded string, byBase map[string][]string) string {
	base := filepath.Base(recorded)
	candidates := byBase[base]
	if len(candidates) == 0 {
		return ""
	}
	if len(candidates) == 1 {
		return candidates[0]
	}
	suffix := filepath.ToSlash(recorded)
	for _, c := range candidates {
		if strings.HasSuffix(filepath.ToSlash(c), suffix) {
			return c
		}
	}
	return ""
}

// flutterSourceKeyBeside returns the storage key for the source bundle that sits
// next to a .dartmap key.
func flutterSourceKeyBeside(dartmapKey string) string {
	dir := path.Dir(filepath.ToSlash(dartmapKey))
	if dir == "." || dir == "" {
		return flutterSourceBundleName
	}
	return dir + "/" + flutterSourceBundleName
}
