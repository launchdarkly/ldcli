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

// appleVendorPathPrefixes are absolute roots holding platform code rather than
// anything the developer wrote.
var appleVendorPathPrefixes = []string{
	"/system/",
	"/usr/include/",
	"/usr/local/include/",
	"/usr/lib/swift/",
	"/library/developer/commandlinetools/",
	"/opt/homebrew/", // Homebrew
	"/opt/local/",    // MacPorts
}

// appleVendorPathMarkers appear mid-path in an Xcode install, wherever it lives:
// "/Applications/Xcode26.2.app/...", a beta, or a versioned side-by-side copy.
var appleVendorPathMarkers = []string{
	".sdk/",                    // any platform SDK's headers
	".xctoolchain/",            // bundled clang/Swift toolchain
	".platform/developer/",     // platform support code
	".app/contents/developer/", // the Xcode install itself
}

// isAppleVendorSource reports whether path belongs to the toolchain, an SDK, or
// the OS rather than to the project being uploaded.
//
// This has to be decided by path, not by whether the file opens. Readability
// says nothing about ownership: on any machine with Xcode the SDK headers a
// build referenced are sitting right there and read fine, while the app's own
// sources may not (a CI build, a cleaned DerivedData, a dSYM from a colleague).
// Filtering on readability alone therefore does the opposite of what's wanted —
// it can drop every app file and keep only Apple's.
func isAppleVendorSource(path string) bool {
	// DWARF records the compiler's paths, whose case need not match the disk on
	// a case-insensitive volume.
	lower := strings.ToLower(path)
	for _, prefix := range appleVendorPathPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	for _, marker := range appleVendorPathMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// buildAppleSourceBundle packs the source files referenced by one architecture's
// DWARF into a .srcbundle. Only the project's own sources are packed: toolchain,
// SDK and OS files are excluded by path (see isAppleVendorSource) so a build
// machine with Xcode installed can't ship Apple's headers to LaunchDarkly, and
// files that aren't on this machine at all are skipped. The backend renders those
// frames without source context. Returns nil when nothing local was found, so the
// caller can skip the upload entirely.
func buildAppleSourceBundle(a apple.Arch) ([]byte, int, error) {
	b := &srcbundle.Builder{}
	total := 0
	for key, path := range a.Sources {
		if !appleSourceExtensions[strings.ToLower(filepath.Ext(key))] {
			continue
		}
		// The DWARF key records where the compiler read the file from, which is
		// what establishes provenance; the resolved path is checked too, in case a
		// relative or rewritten key lands in the toolchain anyway.
		if isAppleVendorSource(key) || isAppleVendorSource(path) {
			continue // not the developer's code to upload
		}
		data, err := os.ReadFile(path)
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
		return nil, 0, fmt.Errorf("failed to encode source bundle for %s: %w", a.UUID, err)
	}
	return buf.Bytes(), b.Len(), nil
}

// appleSourceKey is the storage key for an image's source bundle.
func appleSourceKey(uuid string) string {
	return fmt.Sprintf("%s/%s%s", appleSymbolsIDPrefix, uuid, appleSourceExt)
}
