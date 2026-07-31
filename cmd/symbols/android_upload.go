package symbols

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/launchdarkly/ldcli/internal/symbols/r8index"
)

// Android symbol uploads.
//
// R8 writes a mapping.txt: tens of megabytes of text recording, for one build, what
// each obfuscated class, method and line was called in the source. Symbolication needs
// a few dozen of those answers per crash, so what gets uploaded is not the text but an
// index over it (internal/symbols/r8index), built here — on the machine that just
// produced the mapping and can read it once — rather than by the backend on the first
// crash of every build that ever crashes.
//
// A build's objects go to as many as two lanes. The Symbols Id Lane is keyed by the id
// the shipped app reports, which is what a crash arrives with. The Version Lane is
// keyed by the app version, and is what symbolication falls back to for a crash that
// reports no id.

const (
	// androidIndexFileName is the object symbolication reads for an Android build.
	// The format version is part of the name so that changing the format means
	// writing a different object rather than teaching a reader to guess, and so that
	// an index can be skipped on existence: for one mapping and one version of this
	// format, the bytes can only be these.
	androidIndexFileName = "mapping.v1.index"
)

// androidLane is one storage prefix a build's objects are stored under.
type androidLane struct {
	prefix string
	label  string
	// keyProvesContent is true when the prefix is derived from the build the object
	// came from, so an object already at the key can only be these same bytes. It is
	// false for the Version Lane, where what is there is the last build's.
	keyProvesContent bool
}

func (l androidLane) key(name string) string {
	return fmt.Sprintf("%s/%s", l.prefix, name)
}

// androidObject is one artifact to store at one key.
type androidObject struct {
	Lane androidLane
	Name string
	Data []byte
	// keyProvesContent is the lane's rule narrowed to this object: a source bundle is
	// keyed by the mapping's id rather than its own contents, so even in the Id Lane
	// the key says nothing about which sources are stored under it.
	keyProvesContent bool
}

func (o androidObject) Key() string { return o.Lane.key(o.Name) }

func (o androidObject) Label() string { return fmt.Sprintf("%s (%s)", o.Name, o.Lane.label) }

// uploadAndroidSymbols indexes the R8 mapping for a build and uploads it, along with
// the app's sources when they were asked for.
func uploadAndroidSymbols(apiKey, projectID, path, appVersion, symbolsID, backendURL string, includeSources bool, sourceRoot string, skipExisting bool) error {
	fmt.Printf("Starting to upload %s symbols from %s\n", typeAndroid, path)

	objects, err := buildAndroidObjects(path, appVersion, symbolsID, includeSources, sourceRoot)
	if err != nil {
		return err
	}

	// Compressed before URLs are requested rather than at the point of sending,
	// because a digest has to describe the bytes that get stored. Bodies are shared
	// across lanes, so the same object is not compressed once per key. An index gzips
	// its own blocks, so compressBody declines to wrap it again.
	bodies := make(map[string]uploadBody, 2)
	keys := make([]string, len(objects))
	digests := make([]string, len(objects))
	for i, object := range objects {
		body, ok := bodies[object.Name]
		if !ok {
			body = compressBody(object.Data)
			bodies[object.Name] = body
		}
		keys[i] = object.Key()
		if !object.keyProvesContent {
			digests[i] = contentDigest(body.Data)
		}
	}

	uploadURLs, err := getSymbolUploadUrls(apiKey, projectID, keys, digests, backendURL, skipExisting)
	if err != nil {
		return fmt.Errorf("failed to get upload URLs: %w", err)
	}
	// One URL per requested key, in order; a short list would misalign the pairing.
	if len(uploadURLs) != len(objects) {
		return fmt.Errorf("expected %d upload URLs but received %d", len(objects), len(uploadURLs))
	}

	skipped := 0
	for i, object := range objects {
		if alreadyUploaded(uploadURLs[i]) {
			fmt.Printf("Skipping %s, already uploaded\n", object.Label())
			skipped++
			continue
		}
		if err := uploadBytes(bodies[object.Name], uploadURLs[i], object.Label()); err != nil {
			return fmt.Errorf("failed to upload %s: %w", object.Label(), err)
		}
	}

	reportUploadSummary(skipped)
	return nil
}

// buildAndroidObjects compiles the build's mapping into an index and pairs it with
// every key it has to be stored under, which is also what `symbols generate` writes.
func buildAndroidObjects(path, appVersion, symbolsID string, includeSources bool, sourceRoot string) ([]androidObject, error) {
	// An Android project already knows where R8 put the mapping, what version it
	// shipped, and which symbols id the app reports, so read all three out of the
	// build instead of asking for them.
	build, err := resolveAndroidBuild(androidUpload{Path: path, AppVersion: appVersion, SymbolsID: symbolsID})
	if err != nil {
		return nil, err
	}

	lanes := androidLanes(build)
	if len(lanes) == 0 {
		return nil, fmt.Errorf("this build reports no symbols id and no app version, so there is no key a crash could be symbolicated under. Apply the LaunchDarkly Gradle plugin so the shipped app records its symbols id, or re-run with --%s <version>", appVersionFlag)
	}

	mapping, err := findAndroidMapping(build.Path)
	if err != nil {
		return nil, err
	}
	index, err := buildAndroidIndex(mapping)
	if err != nil {
		return nil, err
	}

	sources, err := androidSources(includeSources, sourceRoot)
	if err != nil {
		return nil, err
	}

	var objects []androidObject
	for i, lane := range lanes {
		objects = append(objects, androidObject{
			Lane:             lane,
			Name:             androidIndexFileName,
			Data:             index,
			keyProvesContent: lane.keyProvesContent,
		})

		// Sources go on the lane symbolication resolves first, because that is the
		// lane it reads them from: the index a frame was retraced with and the source
		// shown behind it have to come from one upload, or a frame would carry this
		// build's line numbers under another build's code.
		if sources != nil && i == 0 {
			objects = append(objects, androidObject{Lane: lane, Name: androidSourceBundleName, Data: sources})
		}
	}
	return objects, nil
}

// androidLanes returns the lanes to store a build's objects under, in the order
// symbolication tries them.
func androidLanes(build androidUpload) []androidLane {
	var lanes []androidLane
	if build.SymbolsID != "" {
		lanes = append(lanes, androidLane{
			prefix:           fmt.Sprintf("%s/%s", androidSymbolsIDPrefix, build.SymbolsID),
			label:            "Symbols Id Lane",
			keyProvesContent: true,
		})
	}
	if build.AppVersion != "" {
		lanes = append(lanes, androidLane{prefix: build.AppVersion, label: "Version Lane"})
	}
	return lanes
}

// androidSources packs the app's sources into the bundle stored beside the index, so
// the errors page can show the code around each retraced frame. Off unless asked for:
// it ships source to LaunchDarkly. Finding none is not fatal — the index alone still
// retraces, just without the code behind a frame.
func androidSources(includeSources bool, sourceRoot string) ([]byte, error) {
	if !includeSources {
		return nil, nil
	}

	data, count, err := buildAndroidSourceBundle(sourceRoot)
	if err != nil {
		return nil, err
	}
	if data == nil {
		fmt.Printf("No .java/.kt sources found under %s; skipping source bundle\n", sourceRoot)
		return nil, nil
	}

	fmt.Printf("Built source bundle from %s (%d files, %s)\n", sourceRoot, count, byteSize(int64(len(data))))
	return data, nil
}

// findAndroidMapping resolves a path to the one mapping to index. More than one is
// refused rather than chosen between: a build has a single mapping, and the keys above
// can only describe one of them.
func findAndroidMapping(path string) (string, error) {
	files, err := getAllSymbolFiles(path, typeAndroid)
	if err != nil {
		return "", fmt.Errorf("failed to find symbol files: %w", err)
	}
	if len(files) > 1 {
		found := make([]string, len(files))
		for i, file := range files {
			found[i] = file.Path
		}
		return "", fmt.Errorf("found %d mappings under %s (%s), which cannot all be from one build; point --%s at the one that shipped", len(files), path, strings.Join(found, ", "), pathFlag)
	}
	return files[0].Path, nil
}

// buildAndroidIndex compiles a mapping.txt into the index symbolication reads,
// streaming it off disk so that a release mapping is never held in memory whole.
func buildAndroidIndex(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}
	defer file.Close()

	index, err := r8index.EncodeFrom(file)
	if err != nil {
		// Reported here rather than fallen back from: symbolication reads only the
		// index, so storing anything else would leave a build looking like it has
		// symbols while every crash arrives obfuscated.
		return nil, fmt.Errorf("failed to index %s: %w. Check that this is an R8/ProGuard mapping from a minified build", path, err)
	}

	size := int64(0)
	if info, err := file.Stat(); err == nil {
		size = info.Size()
	}
	fmt.Printf("Indexed %s (%s of mapping into a %s index)\n",
		filepath.Base(path), byteSize(size), byteSize(int64(len(index))))
	return index, nil
}
