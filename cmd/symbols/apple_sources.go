package symbols

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/launchdarkly/ldcli/internal/symbols/apple"
	"github.com/launchdarkly/ldcli/internal/symbols/srcbundle"
)

// appleSourceExt is appended to the build UUID to form the source-bundle object
// name, so an image's map and its sources sit side by side under the same key:
// _sym/apple/id/<UUID>.dsymmap and _sym/apple/id/<UUID>.srcbundle.
const appleSourceExt = ".srcbundle"

const (
	// maxSourceFileBytes skips individual files too large to be worth shipping
	// (generated code, vendored blobs). Only a handful of lines around a frame is
	// ever rendered, so a huge file adds size for no benefit.
	maxSourceFileBytes = 2 << 20 // 2 MiB

	// maxSourceBundleBytes caps one image's uncompressed source total, so an
	// accidental `--include-sources` over a huge workspace can't produce an
	// unbounded upload.
	maxSourceBundleBytes = 256 << 20 // 256 MiB
)

// appleSourceExtensions are the file types the UI can render as source context.
// Restricting to these keeps unrelated DWARF-referenced paths (module maps,
// generated interfaces) out of the bundle.
var appleSourceExtensions = map[string]bool{
	".swift": true,
	".m":     true,
	".mm":    true,
	".h":     true,
	".hpp":   true,
	".hh":    true,
	".c":     true,
	".cc":    true,
	".cpp":   true,
	".cxx":   true,
}

// buildAppleSourceBundle packs the source files referenced by one architecture's
// DWARF into a .srcbundle. Files that aren't readable on this machine (SDK,
// system, or third-party code built elsewhere) are skipped — the backend simply
// renders those frames without source context. Returns nil when nothing local
// was found, so the caller can skip the upload entirely.
func buildAppleSourceBundle(a apple.Arch) ([]byte, int, error) {
	b := &srcbundle.Builder{}
	total := 0
	for key, path := range a.Sources {
		if !appleSourceExtensions[strings.ToLower(filepath.Ext(key))] {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue // not on this machine: expected for SDK/system sources
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
		return nil, 0, fmt.Errorf("failed to encode source bundle for %s: %w", a.UUID, err)
	}
	return buf.Bytes(), b.Len(), nil
}

// appleSourceKey is the storage key for an image's source bundle.
func appleSourceKey(uuid string) string {
	return fmt.Sprintf("%s/%s%s", appleSymbolsIDPrefix, uuid, appleSourceExt)
}
