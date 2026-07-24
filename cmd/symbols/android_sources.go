package symbols

import (
	"bufio"
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/launchdarkly/ldcli/internal/symbols/srcbundle"
)

// androidSourceBundleName is the object name of the source bundle uploaded beside
// an R8 mapping, so a build's mapping and its sources share one key prefix:
// _sym/android/id/<symbolsID>/mapping.txt and .../sources.srcbundle.
const androidSourceBundleName = "sources.srcbundle"

// androidSourceExtensions are the JVM source types R8 stack frames can point at.
var androidSourceExtensions = map[string]bool{
	".java": true,
	".kt":   true,
}

// androidSkipDirs are trees that never hold hand-written sources worth shipping:
// build output (including R8's own generated code), caches, and JS deps.
var androidSkipDirs = map[string]bool{
	"build":        true,
	".gradle":      true,
	".git":         true,
	".idea":        true,
	"node_modules": true,
}

// buildAndroidSourceBundle scans root for .java/.kt files and packs them keyed by
// their package-relative path (e.g. com/example/MainActivity.kt), which is what
// the backend reconstructs from a retraced frame's fully-qualified class name.
//
// The key comes from each file's own `package` declaration rather than its
// directory layout, because Kotlin does not require the two to agree. A file with
// no package declaration is keyed by its bare name (the JVM default package).
// Returns nil when nothing was found, so the caller can skip the upload.
func buildAndroidSourceBundle(root string) ([]byte, int, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read source path %s: %w", root, err)
	}
	if !info.IsDir() {
		return nil, 0, fmt.Errorf("source path %s is not a directory", root)
	}

	b := &srcbundle.Builder{}
	total := 0
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if androidSkipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !androidSourceExtensions[strings.ToLower(filepath.Ext(d.Name()))] {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil // unreadable: skip, same as Apple
		}
		if len(data) > maxSourceFileBytes || total+len(data) > maxSourceBundleBytes {
			return nil
		}
		total += len(data)
		b.Add(androidSourceKey(data, d.Name()), data)
		return nil
	})
	if err != nil {
		return nil, 0, fmt.Errorf("failed to scan sources in %s: %w", root, err)
	}
	if b.Len() == 0 {
		return nil, 0, nil
	}

	var buf bytes.Buffer
	if err := b.Encode(&buf); err != nil {
		return nil, 0, fmt.Errorf("failed to encode source bundle: %w", err)
	}
	return buf.Bytes(), b.Len(), nil
}

// androidSourceKey is the bundle key for one source file: its package as a path,
// plus the file name (com/example/MainActivity.kt).
func androidSourceKey(data []byte, fileName string) string {
	pkg := javaPackageOf(data)
	if pkg == "" {
		return fileName
	}
	return path.Join(strings.ReplaceAll(pkg, ".", "/"), fileName)
}

// javaPackageOf extracts the package name from Java or Kotlin source, or "" when
// there is none. It reads the leading declarations only: the package statement
// must precede any type declaration in both languages, so scanning stops at the
// first import (Kotlin allows a semicolon-less package line, hence the trim).
func javaPackageOf(data []byte) string {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "/*") ||
			strings.HasPrefix(line, "*") || strings.HasPrefix(line, "@") {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "package "); ok {
			pkg := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(rest), ";"))
			// Guard against a stray trailing comment ("package a.b; // note").
			if i := strings.IndexAny(pkg, " \t/"); i >= 0 {
				pkg = pkg[:i]
			}
			return strings.TrimSuffix(pkg, ";")
		}
		if strings.HasPrefix(line, "import ") {
			return "" // past the package slot
		}
		return "" // a real declaration: this file has no package
	}
	return ""
}
