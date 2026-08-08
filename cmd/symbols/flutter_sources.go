package symbols

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/launchdarkly/ldcli/internal/symbols/flutter"
	"github.com/launchdarkly/ldcli/internal/symbols/srcbundle"
)

// flutterSourceBundleName is the object name of the source bundle uploaded beside a
// build's .dartmap, so a map and the sources behind it share one key prefix:
// _sym/flutter/id/<symbolsID>/app.dartmap and .../sources.srcbundle.
const flutterSourceBundleName = "sources.srcbundle"

// flutterSourceExtensions are the file types the UI can render as Dart source context.
var flutterSourceExtensions = map[string]bool{
	".dart": true,
}

// flutterVendorPathMarkers appear mid-path in Flutter/Dart SDK and pub-cache
// trees. Filtering by path (not readability) keeps SDK sources out of uploads
// the way Apple filters Xcode headers.
var flutterVendorPathMarkers = []string{
	"/.pub-cache/",
	"/pub-cache/",
	"/flutter/packages/flutter/",
	"/flutter/bin/cache/",
	"/flutter/packages/flutter_test/",
	"/flutter/packages/flutter_driver/",
	"/flutter/packages/flutter_localizations/",
	"/flutter/packages/flutter_web_plugins/",
	"/third_party/dart/",
	"/hosted/pub.dev/",
	"/hosted/pub.dartlang.org/",
}

// isFlutterVendorSource reports whether path belongs to the Flutter/Dart SDK or
// pub cache rather than to the project being uploaded.
func isFlutterVendorSource(path string) bool {
	lower := strings.ToLower(filepath.ToSlash(path))
	for _, marker := range flutterVendorPathMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// buildFlutterSourceBundle packs the project's .dart sources referenced by one
// (or more) .symbols images' DWARF into a .srcbundle. Keys are the exact DWARF
// strings stored in the .dartmap, so a resolved frame's FileName is the lookup
// key. SDK and pub-cache paths are excluded; files that aren't on this machine
// are skipped (or recovered from sourceRoot when given). Returns nil when
// nothing local was found, so the caller can skip the upload.
func buildFlutterSourceBundle(images []flutter.Image, sourceRoot string) ([]byte, int, error) {
	// Merge DWARF paths across arches: one release's sources are identical, and
	// the backend reads the bundle from whichever lane resolved the map.
	merged := make(map[string]string)
	for _, img := range images {
		for key, abs := range img.Sources {
			if _, ok := merged[key]; !ok {
				merged[key] = abs
			}
		}
	}

	byBase := indexDartFilesByBase(sourceRoot)

	b := &srcbundle.Builder{}
	total := 0
	for key, abs := range merged {
		if !flutterSourceExtensions[strings.ToLower(filepath.Ext(key))] {
			continue
		}
		if isFlutterVendorSource(key) || isFlutterVendorSource(abs) {
			continue
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			// DWARF recorded a build-machine path that isn't here; try the same
			// basename under --source-path (typically the project root).
			if alt := resolveFlutterSourceFallback(key, byBase); alt != "" {
				data, err = os.ReadFile(alt)
			}
		}
		if err != nil {
			continue
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

// indexDartFilesByBase maps basename → absolute paths under root. Empty when
// root is blank or unreadable; collisions keep every candidate so the caller can
// prefer a unique match.
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
			name := d.Name()
			if name == ".dart_tool" || name == "build" || name == ".git" || name == ".pub-cache" {
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
		base := d.Name()
		out[base] = append(out[base], p)
		return nil
	})
	return out
}

// resolveFlutterSourceFallback picks a --source-path file for a DWARF key whose
// recorded absolute path isn't readable here. Prefer a unique basename match;
// when several share the name, prefer one whose path ends with the DWARF key
// (handles package-relative keys like lib/main.dart).
func resolveFlutterSourceFallback(key string, byBase map[string][]string) string {
	base := filepath.Base(key)
	cands := byBase[base]
	if len(cands) == 0 {
		return ""
	}
	if len(cands) == 1 {
		return cands[0]
	}
	slashKey := filepath.ToSlash(key)
	for _, c := range cands {
		if strings.HasSuffix(filepath.ToSlash(c), slashKey) {
			return c
		}
	}
	return ""
}

// flutterSourceKeyBeside returns the storage key for a source bundle that sits
// next to a .dartmap key (same directory, sources.srcbundle name).
func flutterSourceKeyBeside(dartmapKey string) string {
	dir := filepath.ToSlash(filepath.Dir(dartmapKey))
	if dir == "." || dir == "" {
		return flutterSourceBundleName
	}
	return dir + "/" + flutterSourceBundleName
}
