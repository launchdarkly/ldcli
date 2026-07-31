package symbols

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Android build discovery.
//
// The Android Gradle Plugin already writes everything an upload needs, in
// well-known places: R8's mapping at
// <module>/build/outputs/mapping/<variant>/mapping.txt, and the version that
// shipped in <module>/build/outputs/{apk,bundle}/**/output-metadata.json. Reading
// both here means `ldcli symbols upload --type android` works from an Android
// project root with no --path, no --app-version, and no build script staging
// files for it.

const (
	// androidOutputMetadataName is the JSON manifest AGP writes beside each
	// packaged APK/AAB, recording the variant and the versionName/versionCode it
	// was built with.
	androidOutputMetadataName = "output-metadata.json"

	// androidMappingGlobs are the paths R8 writes a mapping to, relative to the
	// directory being searched: a module's own build dir, then one and two levels
	// of nesting so a project root or a grouped module (features/foo) both match.
	// Deliberately a fixed set of shapes rather than a recursive walk, so an
	// unrelated mapping.txt somewhere in the tree can't be mistaken for a build.
	androidMappingGlob1 = "build/outputs/mapping/*/" + androidMappingFileName
	androidMappingGlob2 = "*/build/outputs/mapping/*/" + androidMappingFileName
	androidMappingGlob3 = "*/*/build/outputs/mapping/*/" + androidMappingFileName
)

// resolveAndroidBuild fills in from the build itself whatever the command line
// left out: the mapping to upload, and the version to key it by. Both are
// returned unchanged when path is not an AGP output tree, or when the caller
// already said which mapping and version it means.
func resolveAndroidBuild(path, appVersion string) (string, string, error) {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		// A path naming one file is already an answer; a path naming nothing is
		// reported by the ordinary search, whose error says what was looked for.
		return path, appVersion, nil
	}

	build, err := discoverAndroidBuild(path)
	if err != nil || build == nil {
		return path, appVersion, err
	}

	fmt.Printf("Found the %s mapping at %s\n", build.Variant, build.MappingPath)
	if appVersion == "" {
		if version := build.AppVersion(); version != "" {
			appVersion = version
			fmt.Printf("Using app version %s, as packaged for %s\n", version, build.Variant)
		}
	}
	return build.MappingPath, appVersion, nil
}

// androidBuild is one obfuscated variant found in an AGP output tree.
type androidBuild struct {
	// MappingPath is the mapping.txt R8 produced for the variant.
	MappingPath string
	// Variant is the AGP variant name (composeRelease), which is both the
	// directory the mapping sits in and how output-metadata.json identifies a build.
	Variant string
	// BuildDir is the module's build directory, the root of the outputs tree the
	// mapping and the packaged app share.
	BuildDir string
}

// discoverAndroidBuild finds the obfuscated build under root, so a mapping never
// has to be pointed at or copied somewhere for upload.
//
// Returns nil when root is not an AGP output tree, which leaves the caller on its
// ordinary search: this only adds a shortcut for the conventional layout, it does
// not take away the ability to upload a mapping from anywhere. Several variants
// is an error rather than a guess, since each is a different app build and only
// one of them is the one being shipped.
func discoverAndroidBuild(root string) (*androidBuild, error) {
	builds, err := findAndroidBuilds(root)
	if err != nil || len(builds) == 0 {
		return nil, err
	}
	if len(builds) > 1 {
		paths := make([]string, 0, len(builds))
		for _, b := range builds {
			paths = append(paths, fmt.Sprintf("  %s (%s)", b.MappingPath, b.Variant))
		}
		return nil, fmt.Errorf(
			"found %d obfuscated Android variants under %s; pass --%s to pick the one you are shipping:\n%s",
			len(builds), root, pathFlag, strings.Join(paths, "\n"),
		)
	}
	return builds[0], nil
}

// findAndroidBuilds returns every variant with a non-empty mapping under root,
// ordered by path so the ambiguity error reads the same on every run. An empty
// mapping is skipped: AGP writes one for a variant R8 left unobfuscated, and it
// would retrace nothing.
func findAndroidBuilds(root string) ([]*androidBuild, error) {
	var matches []string
	for _, glob := range []string{androidMappingGlob1, androidMappingGlob2, androidMappingGlob3} {
		found, err := filepath.Glob(filepath.Join(root, glob))
		if err != nil {
			// The globs are constants, so the only error the pattern can raise is
			// impossible here; surfacing it keeps that assumption honest.
			return nil, err
		}
		matches = append(matches, found...)
	}
	sort.Strings(matches)

	builds := make([]*androidBuild, 0, len(matches))
	for _, mappingPath := range matches {
		info, err := os.Stat(mappingPath)
		if err != nil || info.IsDir() || info.Size() == 0 {
			continue
		}
		variantDir := filepath.Dir(mappingPath)
		builds = append(builds, &androidBuild{
			MappingPath: mappingPath,
			Variant:     filepath.Base(variantDir),
			// <build>/outputs/mapping/<variant>/mapping.txt, so the build dir is
			// three levels up from the variant directory.
			BuildDir: filepath.Dir(filepath.Dir(filepath.Dir(variantDir))),
		})
	}
	return builds, nil
}

// AppVersion returns the versionName the variant was packaged with, or "" when
// the app has not been packaged (mapping but no APK/AAB) or AGP recorded no
// version. It is the same string the app reports at runtime, which is what makes
// it usable as the Version Lane key.
//
// Note this is the app's version, and the SDK only reports it when the app passes
// it as the observability service version; a build that leaves that at its default
// reports the SDK's version instead, and needs a symbols id to be symbolicated.
func (b *androidBuild) AppVersion() string {
	for _, metadataPath := range b.outputMetadataPaths() {
		content, err := os.ReadFile(metadataPath)
		if err != nil {
			continue
		}
		var metadata struct {
			VariantName string `json:"variantName"`
			Elements    []struct {
				VersionName string `json:"versionName"`
			} `json:"elements"`
		}
		if err := json.Unmarshal(content, &metadata); err != nil {
			continue
		}
		// A module builds many variants into one outputs tree, so the metadata has
		// to name this one: taking another variant's version would key the upload
		// to a build these symbols do not describe.
		if metadata.VariantName != b.Variant {
			continue
		}
		for _, element := range metadata.Elements {
			if element.VersionName != "" {
				return element.VersionName
			}
		}
	}
	return ""
}

// outputMetadataPaths are where AGP writes packaging metadata, which is under the
// APK's flavor/buildType directories rather than the variant directory the mapping
// uses — hence the wildcards, with the variant identified by the file's contents.
func (b *androidBuild) outputMetadataPaths() []string {
	var paths []string
	for _, glob := range []string{
		filepath.Join("outputs", "apk", "*", "*", androidOutputMetadataName),
		filepath.Join("outputs", "apk", "*", androidOutputMetadataName),
		filepath.Join("outputs", "bundle", "*", androidOutputMetadataName),
	} {
		found, err := filepath.Glob(filepath.Join(b.BuildDir, glob))
		if err != nil {
			continue
		}
		paths = append(paths, found...)
	}
	sort.Strings(paths)
	return paths
}
