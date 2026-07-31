package symbols

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/launchdarkly/ldcli/internal/symbols/apple"
)

// appleSymbolsIDPrefix is the storage segment for Apple symbol maps. Each map is
// keyed by its build UUID: _sym/apple/id/<UUID>.dsymmap. The backend derives the
// same key from the image_uuid the device reports for a crashing frame.
const appleSymbolsIDPrefix = "_sym/apple/id"

// appleSymbolExt is appended to the build UUID to form the object name, so
// uploaded artifacts carry a human-recognizable file extension. It has no role
// in lookup (maps are keyed by UUID); the backend appends the same extension.
const appleSymbolExt = ".dsymmap"

// kindSources marks an appleSymbolMap that carries packed sources rather than a
// symbol map.
const kindSources = "sources"

// appleSymbolMap is one compiled artifact ready to upload: an architecture's
// .dsymmap symbol map, or (with --include-sources) its .srcbundle sources.
type appleSymbolMap struct {
	Key  string
	UUID string
	Arch string
	Data []byte
	// Kind labels the artifact in progress output ("" for a symbol map).
	Kind string
}

// label describes the artifact for upload logs.
func (m appleSymbolMap) label() string {
	if m.Kind == "" {
		return fmt.Sprintf("%s (%s)", m.UUID, m.Arch)
	}
	return fmt.Sprintf("%s (%s, %s)", m.UUID, m.Arch, m.Kind)
}

// uploadAppleDSYMs discovers .dSYM bundles under path, compiles each contained
// architecture to a .dsymmap symbol map, and uploads one object per build UUID.
// When includeSources is set, each image's referenced source files are packed
// into a .srcbundle and uploaded alongside its map so the UI can show source
// context around native frames.
//
// With skipExisting, an unchanged binary keeps its UUID, so re-running this sends
// nothing. Source bundles borrow that UUID rather than being keyed by their own
// contents — sources unreadable on the machine that uploaded first must still be able
// to overwrite — so those are skipped only when their digest matches what is stored.
func uploadAppleDSYMs(apiKey, projectID, path, backendURL string, includeSources, skipExisting bool) error {
	images, err := findDSYMImages(path)
	if err != nil {
		return fmt.Errorf("failed to find dSYM files: %w", err)
	}
	if len(images) == 0 {
		return fmt.Errorf("no .dSYM bundles found in %s, is this the correct path?", path)
	}

	maps, err := buildAppleMaps(images, includeSources)
	if err != nil {
		return err
	}
	if len(maps) == 0 {
		return fmt.Errorf("no architectures found in the discovered dSYM files")
	}

	keys := make([]string, len(maps))
	digests := make([]string, len(maps))
	bodies := make([]uploadBody, len(maps))
	for i, m := range maps {
		keys[i] = m.Key
		// Compressed here rather than at the point of sending, so a digest below
		// describes the bytes that get stored.
		bodies[i] = compressBody(m.Data)
		if m.Kind == kindSources {
			// Sources are keyed by their image's UUID rather than by their own
			// contents, so only a digest can show that re-sending them is a no-op.
			digests[i] = contentDigest(bodies[i].Data)
		}
	}

	uploadURLs, err := getSymbolUploadUrls(apiKey, projectID, keys, digests, backendURL, skipExisting)
	if err != nil {
		return fmt.Errorf("failed to get upload URLs: %w", err)
	}
	// getSymbolUploadUrls returns one URL per requested key, in order; a short
	// list would misalign the pairing below, so require an exact match.
	if len(uploadURLs) != len(maps) {
		return fmt.Errorf("expected %d upload URLs but received %d", len(maps), len(uploadURLs))
	}

	skipped := 0
	for i, m := range maps {
		if alreadyUploaded(uploadURLs[i]) {
			fmt.Printf("Skipping %s, already uploaded\n", m.label())
			skipped++
			continue
		}
		if err := uploadBytes(bodies[i], uploadURLs[i], m.label()); err != nil {
			return fmt.Errorf("failed to upload symbol map for %s: %w", m.UUID, err)
		}
	}

	reportUploadSummary(skipped)
	return nil
}

// buildAppleMaps compiles every architecture of every dSYM image into a .dsymmap,
// deduplicating by UUID (a universal binary and its per-arch slices can repeat).
// With includeSources it also emits a .srcbundle per image, keyed by the same
// UUID.
func buildAppleMaps(images []string, includeSources bool) ([]appleSymbolMap, error) {
	var maps []appleSymbolMap
	seen := make(map[string]bool)

	for _, image := range images {
		arches, err := apple.BuildFromMachO(image)
		if err != nil {
			return nil, fmt.Errorf("failed to process %s: %w", image, err)
		}
		for _, a := range arches {
			if seen[a.UUID] {
				continue
			}
			seen[a.UUID] = true

			var buf bytes.Buffer
			if err := a.Builder.Encode(&buf); err != nil {
				return nil, fmt.Errorf("failed to encode symbol map for %s: %w", a.UUID, err)
			}
			arch := archLabel(a.CPUType)
			maps = append(maps, appleSymbolMap{
				Key:  appleKey(a.UUID),
				UUID: a.UUID,
				Arch: arch,
				Data: buf.Bytes(),
			})
			fmt.Printf("Built symbol map for %s (%s, %d bytes)\n", a.UUID, arch, buf.Len())

			if !includeSources {
				continue
			}
			srcData, nFiles, err := buildAppleSourceBundle(a)
			if err != nil {
				return nil, err
			}
			if srcData == nil {
				// None of the DWARF-referenced sources are readable here (e.g.
				// uploading a dSYM archived on another machine). Not fatal: the map
				// alone still symbolicates, just without source context.
				fmt.Printf("No local sources found for %s (%s); skipping source bundle\n", a.UUID, arch)
				continue
			}
			maps = append(maps, appleSymbolMap{
				Key:  appleSourceKey(a.UUID),
				UUID: a.UUID,
				Arch: arch,
				Data: srcData,
				Kind: kindSources,
			})
			fmt.Printf("Built source bundle for %s (%s, %d files, %d bytes)\n", a.UUID, arch, nFiles, len(srcData))
		}
	}
	return maps, nil
}

func appleKey(uuid string) string {
	return fmt.Sprintf("%s/%s%s", appleSymbolsIDPrefix, uuid, appleSymbolExt)
}

// findDSYMImages resolves path to the DWARF Mach-O images to symbolicate. path
// may be a single .dSYM bundle, a directory tree containing .dSYM bundles, or a
// DWARF Mach-O file directly.
func findDSYMImages(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	if !info.IsDir() {
		// A file is treated as a DWARF Mach-O image (e.g. the inner dSYM file).
		return []string{path}, nil
	}

	if strings.HasSuffix(path, ".dSYM") {
		return dwarfImagesIn(path)
	}

	var images []string
	err = filepath.WalkDir(path, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() && strings.HasSuffix(d.Name(), ".dSYM") {
			found, ferr := dwarfImagesIn(p)
			if ferr != nil {
				return ferr
			}
			images = append(images, found...)
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return images, nil
}

// dwarfImagesIn returns the Mach-O images inside a .dSYM bundle's
// Contents/Resources/DWARF directory.
func dwarfImagesIn(bundle string) ([]string, error) {
	dwarfDir := filepath.Join(bundle, "Contents", "Resources", "DWARF")
	entries, err := os.ReadDir(dwarfDir)
	if err != nil {
		return nil, fmt.Errorf("dSYM %s has no DWARF resources: %w", bundle, err)
	}
	var images []string
	for _, entry := range entries {
		if !entry.IsDir() {
			images = append(images, filepath.Join(dwarfDir, entry.Name()))
		}
	}
	return images, nil
}

// archLabel maps a mach cputype to a human label for logs (best-effort).
func archLabel(cpuType uint32) string {
	switch cpuType {
	case 0x0100000C:
		return "arm64"
	case 0x0200000C:
		return "arm64_32"
	case 0x01000007:
		return "x86_64"
	case 0x00000007:
		return "i386"
	case 0x0000000C:
		return "arm"
	default:
		return fmt.Sprintf("cpu-0x%x", cpuType)
	}
}

// uploadBytes sends an artifact built in memory. It takes an already-compressed
// body rather than raw bytes so that a caller which sends a digest hashes exactly
// what gets stored; see compress.go.
func uploadBytes(body uploadBody, uploadURL, name string) error {
	if err := putObject(uploadURL, bytes.NewReader(body.Data), int64(len(body.Data)), body.Encoding); err != nil {
		return err
	}

	if body.Encoding == gzipEncoding {
		fmt.Printf("[LaunchDarkly] Uploaded symbol map %s (%s gzipped to %s)\n",
			name, byteSize(int64(body.RawSize)), byteSize(int64(len(body.Data))))
		return nil
	}
	fmt.Printf("[LaunchDarkly] Uploaded symbol map %s (%s)\n", name, byteSize(int64(len(body.Data))))
	return nil
}
