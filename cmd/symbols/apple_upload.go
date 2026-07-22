package symbols

import (
	"bytes"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/launchdarkly/ldcli/internal/symbols/apple"
)

// appleSymbolsIDPrefix is the storage segment for Apple symbol maps. Each map is
// keyed by its build UUID: _sym/apple/id/<UUID>. The backend derives the same
// key from the image_uuid the device reports for a crashing frame.
const appleSymbolsIDPrefix = "_sym/apple/id"

// appleSymbolMap is one architecture's compiled .ldsm ready to upload.
type appleSymbolMap struct {
	Key  string
	UUID string
	Arch string
	Data []byte
}

// uploadAppleDSYMs discovers .dSYM bundles under path, compiles each contained
// architecture to a .ldsm symbol map, and uploads one object per build UUID.
func uploadAppleDSYMs(apiKey, projectID, path, backendURL string) error {
	images, err := findDSYMImages(path)
	if err != nil {
		return fmt.Errorf("failed to find dSYM files: %w", err)
	}
	if len(images) == 0 {
		return fmt.Errorf("no .dSYM bundles found in %s, is this the correct path?", path)
	}

	maps, err := buildAppleMaps(images)
	if err != nil {
		return err
	}
	if len(maps) == 0 {
		return fmt.Errorf("no architectures found in the discovered dSYM files")
	}

	keys := make([]string, len(maps))
	for i, m := range maps {
		keys[i] = m.Key
	}

	uploadURLs, err := getSymbolUploadUrls(apiKey, projectID, keys, backendURL)
	if err != nil {
		return fmt.Errorf("failed to get upload URLs: %w", err)
	}
	// getSymbolUploadUrls returns one URL per requested key, in order; a short
	// list would misalign the pairing below, so require an exact match.
	if len(uploadURLs) != len(maps) {
		return fmt.Errorf("expected %d upload URLs but received %d", len(maps), len(uploadURLs))
	}

	for i, m := range maps {
		if err := uploadBytes(m.Data, uploadURLs[i], fmt.Sprintf("%s (%s)", m.UUID, m.Arch)); err != nil {
			return fmt.Errorf("failed to upload symbol map for %s: %w", m.UUID, err)
		}
	}

	fmt.Println("Successfully uploaded all symbols")
	return nil
}

// buildAppleMaps compiles every architecture of every dSYM image into a .ldsm,
// deduplicating by UUID (a universal binary and its per-arch slices can repeat).
func buildAppleMaps(images []string) ([]appleSymbolMap, error) {
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
		}
	}
	return maps, nil
}

func appleKey(uuid string) string {
	return fmt.Sprintf("%s/%s", appleSymbolsIDPrefix, uuid)
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

func uploadBytes(data []byte, uploadURL, name string) error {
	req, err := http.NewRequest("PUT", uploadURL, bytes.NewReader(data))
	if err != nil {
		return err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("upload failed with status code: %d", resp.StatusCode)
	}

	fmt.Printf("[LaunchDarkly] Uploaded symbol map %s\n", name)
	return nil
}
