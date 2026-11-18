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

// flutterUpload is one object to store at one key — a .dartmap, or the optional
// sources.srcbundle that sits beside it. A map is uploaded to the Id lane always,
// and to the Version lane too when --app-version is given (same bytes, two keys).
type flutterUpload struct {
	Data  []byte
	Key   string
	Label string
}

// uploadFlutterSymbols discovers app.*.symbols files under path, compiles each
// to a .dartmap, and uploads it to the Id lane (and the Version lane when
// appVersion is set). With includeSources it also packs the project's .dart
// files into a sources.srcbundle beside each map.
//
// With skipExisting only the Id-lane copy can be skipped: a rebuild under the same
// --app-version must still replace what that version resolves to.
func uploadFlutterSymbols(apiKey, projectID, path, appVersion, backendURL string, includeSources bool, sourceRoot string, skipExisting bool) error {
	uploads, err := buildFlutterMaps(path, appVersion, includeSources, sourceRoot)
	if err != nil {
		return err
	}

	keys := make([]string, len(uploads))
	for i, u := range uploads {
		keys[i] = u.Key
	}

	// No digests: every key here is either the dartmap's own build id, a Version
	// Lane copy, or a source bundle that borrows that id — which the backend
	// re-presigns so it can overwrite.
	uploadURLs, err := getSymbolUploadUrls(apiKey, projectID, keys, nil, backendURL, skipExisting)
	if err != nil {
		return fmt.Errorf("failed to get upload URLs: %w", err)
	}
	// One URL per requested key, in order; a short list would misalign the pairing.
	if len(uploadURLs) != len(uploads) {
		return fmt.Errorf("expected %d upload URLs but received %d", len(uploads), len(uploadURLs))
	}

	skipped := 0
	for i, u := range uploads {
		if alreadyUploaded(uploadURLs[i]) {
			fmt.Printf("Skipping %s, already uploaded\n", u.Label)
			skipped++
			continue
		}
		// Nothing here carries a digest, so unlike the Apple uploader this can wait
		// until a map is known to be going, and skip the work for one that isn't.
		if err := uploadBytes(compressBody(u.Data), uploadURLs[i], u.Label); err != nil {
			return fmt.Errorf("failed to upload %s: %w", u.Label, err)
		}
	}

	reportUploadSummary(skipped)
	return nil
}

// buildFlutterMaps compiles every discovered app.*.symbols to a .dartmap and
// returns the objects to store, deduplicating by symbols_id (the same build can
// be discovered more than once). Each map yields an Id-lane upload, plus a
// Version-lane upload when appVersion and a platform token are both available.
// With includeSources one sources.srcbundle is attached beside every distinct
// storage prefix the maps occupy, since symbolication reads it from the lane the
// map came from.
func buildFlutterMaps(path, appVersion string, includeSources bool, sourceRoot string) ([]flutterUpload, error) {
	files, err := findFlutterSymbolFiles(path)
	if err != nil {
		return nil, fmt.Errorf("failed to find Flutter symbol files: %w", err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no Flutter symbol files found in %s (looked for app.*.symbols). Build with `flutter build ... --obfuscate --split-debug-info=<dir>`", path)
	}

	var uploads []flutterUpload
	var images []flutter.Image
	seenID := make(map[string]bool)
	seenVersionKey := make(map[string]bool)
	var noBuildID []string
	var missingAppVersion, missingPlatform int
	for _, file := range files {
		img, err := flutter.BuildFromELF(file)
		if err != nil {
			return nil, fmt.Errorf("failed to process %s: %w", file, err)
		}
		images = append(images, img)

		var buf bytes.Buffer
		if err := img.Builder.Encode(&buf); err != nil {
			return nil, fmt.Errorf("failed to encode symbol map for %s: %w", file, err)
		}
		data := buf.Bytes()

		// iOS/macOS .symbols files carry no Dart build id (see readBuildID), so the
		// Id lane can't be keyed from the file. Fall back to the Version lane, which
		// the backend also tries for Flutter crashes (keyed by app version +
		// platform). This requires --app-version and a platform token.
		if img.SymbolsID == "" {
			if appVersion == "" || img.Platform == "" {
				noBuildID = append(noBuildID, file)
				if appVersion == "" {
					missingAppVersion++
					fmt.Printf("Skipping %s: no build id in file (e.g. iOS .symbols). Re-run with --app-version to upload it to the Version lane.\n", filepath.Base(file))
				} else {
					missingPlatform++
					fmt.Printf("Skipping %s: no build id in file and no platform token could be parsed from the filename (expected app.<platform>.symbols), so the Version lane can't be keyed.\n", filepath.Base(file))
				}
				continue
			}
			vKey := flutterVersionKey(appVersion, img.Platform)
			if seenVersionKey[vKey] {
				continue
			}
			seenVersionKey[vKey] = true
			uploads = append(uploads, flutterUpload{
				Data:  data,
				Key:   vKey,
				Label: fmt.Sprintf("%s (Version Lane, no build id)", img.Platform),
			})
			fmt.Printf("Built symbol map for %s (Version lane only, no build id, %d bytes)\n", img.Platform, len(data))
			continue
		}

		if seenID[img.SymbolsID] {
			continue
		}
		seenID[img.SymbolsID] = true

		uploads = append(uploads, flutterUpload{
			Data:  data,
			Key:   flutterIDKey(img.SymbolsID),
			Label: fmt.Sprintf("%s (%s, Id Lane)", img.SymbolsID, img.Platform),
		})
		if appVersion != "" && img.Platform != "" {
			vKey := flutterVersionKey(appVersion, img.Platform)
			if !seenVersionKey[vKey] {
				seenVersionKey[vKey] = true
				uploads = append(uploads, flutterUpload{
					Data:  data,
					Key:   vKey,
					Label: fmt.Sprintf("%s (%s, Version Lane)", img.SymbolsID, img.Platform),
				})
			}
		}
		fmt.Printf("Built symbol map for %s (%s, %d bytes)\n", img.SymbolsID, img.Platform, len(data))
	}

	if len(uploads) == 0 {
		if len(noBuildID) > 0 {
			switch {
			case missingAppVersion > 0 && missingPlatform > 0:
				return nil, fmt.Errorf("found %d Flutter symbol file(s) with no build id (e.g. iOS .symbols) that could not be uploaded: %d were missing --app-version, and %d had no platform token parsable from the filename (expected app.<platform>.symbols). Re-run with --app-version <app-version> and ensure filenames include a platform token to use the Version lane", len(noBuildID), missingAppVersion, missingPlatform)
			case missingPlatform > 0:
				return nil, fmt.Errorf("found %d Flutter symbol file(s) with no build id (e.g. iOS .symbols) and no platform token parsable from the filename (expected app.<platform>.symbols), so the Version lane could not be keyed and none could be uploaded", len(noBuildID))
			default:
				return nil, fmt.Errorf("found %d Flutter symbol file(s) with no build id (e.g. iOS .symbols) and no --app-version was given, so none could be uploaded. Re-run with --app-version <app-version> to use the Version lane", len(noBuildID))
			}
		}
		return nil, fmt.Errorf("no Flutter symbol maps could be built from %s", path)
	}

	if includeSources {
		sources, n, err := buildFlutterSourceBundle(images, sourceRoot)
		if err != nil {
			return nil, err
		}
		if sources == nil {
			fmt.Printf("No project .dart sources could be read for --%s (--%s %q is not a Flutter project root, or its sources are not the ones this build was compiled from); continuing with symbol maps only\n", includeSourcesFlag, sourcePathFlag, sourceRoot)
			return uploads, nil
		}

		// Every lane gets its own copy. Symbolication reads the bundle from
		// whichever lane resolved the map, and each arch has a lane of its own, so
		// a single copy would leave crashes from the other arches without source.
		srcKeys := make([]string, 0, len(uploads))
		seenSrc := make(map[string]bool)
		for _, u := range uploads {
			srcKey := flutterSourceKeyBeside(u.Key)
			if seenSrc[srcKey] {
				continue
			}
			seenSrc[srcKey] = true
			srcKeys = append(srcKeys, srcKey)
		}
		for _, srcKey := range srcKeys {
			uploads = append(uploads, flutterUpload{
				Data:  sources,
				Key:   srcKey,
				Label: fmt.Sprintf("%s (%d files)", flutterSourceBundleName, n),
			})
		}
		fmt.Printf("Built source bundle (%d files, %d bytes) for %d lane(s)\n", n, len(sources), len(srcKeys))
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
