package symbols

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/launchdarkly/ldcli/internal/symbols/flutter"
)

const (
	// flutterSymbolsIDPrefix is the Id-lane storage segment for Flutter/Dart
	// symbol maps. Each map is keyed by the Dart snapshot build id (symbols_id):
	// _sym/flutter/id/<symbolsID>/app.dartmap. The backend derives the same key
	// from the symbols_id an obfuscated crash reports.
	flutterSymbolsIDPrefix = "_sym/flutter/id"

	// flutterSymbolExt is the object extension for a compiled Flutter symbol map
	// (the shared dsymmap binary format under a Flutter-specific name).
	flutterSymbolExt = ".dartmap"

	// flutterSymbolMapName is the Id-lane object name. A symbols_id is unique per
	// (build, arch), so it fully identifies one map and no platform token is
	// needed in the Id-lane key.
	flutterSymbolMapName = "app" + flutterSymbolExt

	// flutterSymbolFileSuffix is the Flutter debug-symbols file `ldcli` discovers
	// for --type flutter (e.g. app.android-arm64.symbols).
	flutterSymbolFileSuffix = ".symbols"
)

// flutterUpload is one .dartmap object to store at one key. A map is uploaded to
// the Id lane always, and to the Version lane too when --app-version is given
// (same bytes, two keys).
type flutterUpload struct {
	Data  []byte
	Key   string
	Label string
}

// uploadFlutterSymbols discovers app.*.symbols files under path, compiles each
// to a .dartmap, and uploads it to the Id lane (and the Version lane when
// appVersion is set).
func uploadFlutterSymbols(apiKey, projectID, path, appVersion, backendURL string) error {
	uploads, err := buildFlutterMaps(path, appVersion)
	if err != nil {
		return err
	}

	keys := make([]string, len(uploads))
	for i, u := range uploads {
		keys[i] = u.Key
	}

	uploadURLs, err := getSymbolUploadUrls(apiKey, projectID, keys, backendURL)
	if err != nil {
		return fmt.Errorf("failed to get upload URLs: %w", err)
	}
	// One URL per requested key, in order; a short list would misalign the pairing.
	if len(uploadURLs) != len(uploads) {
		return fmt.Errorf("expected %d upload URLs but received %d", len(uploads), len(uploadURLs))
	}

	for i, u := range uploads {
		if err := uploadBytes(u.Data, uploadURLs[i], u.Label); err != nil {
			return fmt.Errorf("failed to upload symbol map %s: %w", u.Label, err)
		}
	}

	fmt.Println("Successfully uploaded all symbols")
	return nil
}

// buildFlutterMaps compiles every discovered app.*.symbols to a .dartmap and
// returns the objects to store, deduplicating by symbols_id (the same build can
// be discovered more than once). Each map yields an Id-lane upload, plus a
// Version-lane upload when appVersion and a platform token are both available.
func buildFlutterMaps(path, appVersion string) ([]flutterUpload, error) {
	files, err := findFlutterSymbolFiles(path)
	if err != nil {
		return nil, fmt.Errorf("failed to find Flutter symbol files: %w", err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no Flutter symbol files found in %s (looked for app.*.symbols). Build with `flutter build ... --obfuscate --split-debug-info=<dir>`", path)
	}

	var uploads []flutterUpload
	seen := make(map[string]bool)
	for _, file := range files {
		img, err := flutter.BuildFromELF(file)
		if err != nil {
			return nil, fmt.Errorf("failed to process %s: %w", file, err)
		}
		if img.SymbolsID == "" {
			return nil, fmt.Errorf("no build id found in %s", file)
		}
		if seen[img.SymbolsID] {
			continue
		}
		seen[img.SymbolsID] = true

		var buf bytes.Buffer
		if err := img.Builder.Encode(&buf); err != nil {
			return nil, fmt.Errorf("failed to encode symbol map for %s: %w", img.SymbolsID, err)
		}
		data := buf.Bytes()

		uploads = append(uploads, flutterUpload{
			Data:  data,
			Key:   flutterIDKey(img.SymbolsID),
			Label: fmt.Sprintf("%s (%s, Id Lane)", img.SymbolsID, img.Platform),
		})
		if appVersion != "" && img.Platform != "" {
			uploads = append(uploads, flutterUpload{
				Data:  data,
				Key:   flutterVersionKey(appVersion, img.Platform),
				Label: fmt.Sprintf("%s (%s, Version Lane)", img.SymbolsID, img.Platform),
			})
		}
		fmt.Printf("Built symbol map for %s (%s, %d bytes)\n", img.SymbolsID, img.Platform, len(data))
	}
	return uploads, nil
}

// flutterIDKey is the Id-lane storage key for a symbols_id:
// _sym/flutter/id/<symbolsID>/app.dartmap.
func flutterIDKey(symbolsID string) string {
	return fmt.Sprintf("%s/%s/%s", flutterSymbolsIDPrefix, symbolsID, flutterSymbolMapName)
}

// flutterVersionKey is the Version-lane storage key for a platform token:
// <version>/app.<platform>.dartmap. The platform token (e.g. "android-arm64")
// disambiguates the per-arch maps that share one app version, and matches the
// "<os>-<arch>" the backend builds from the crash header.
func flutterVersionKey(version, platform string) string {
	return fmt.Sprintf("%s/app.%s%s", version, platform, flutterSymbolExt)
}

// findFlutterSymbolFiles resolves path to the app.*.symbols files to compile.
// path may be a single .symbols file or a directory tree (e.g. the
// --split-debug-info output folder).
func findFlutterSymbolFiles(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		if !isFlutterSymbolFile(path) {
			return nil, fmt.Errorf("file %s is not a Flutter symbol file (expected app.*.symbols)", path)
		}
		return []string{path}, nil
	}

	var out []string
	err = filepath.WalkDir(path, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !d.IsDir() && isFlutterSymbolFile(p) {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func isFlutterSymbolFile(name string) bool {
	base := filepath.Base(name)
	return strings.HasPrefix(base, "app.") && strings.HasSuffix(base, flutterSymbolFileSuffix)
}
