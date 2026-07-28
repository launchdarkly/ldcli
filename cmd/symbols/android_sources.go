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

// androidPackageScanLimit bounds how much of a file is examined for its package
// statement. The statement must precede every declaration, so it is always near
// the top; the limit only keeps a pathological file from being scanned in full.
const androidPackageScanLimit = 64 * 1024

// javaPackageOf extracts the package name from Java or Kotlin source, or "" when
// there is none. It reads the leading declarations only: the package statement
// must precede any type declaration in both languages, so scanning stops at the
// first import (Kotlin allows a semicolon-less package line, hence the trim).
//
// Comments are removed before scanning rather than skipped line by line. A
// per-line rule can't see that a line belongs to a block comment: a license
// header written without a leading "*" on each line looks exactly like source,
// and would be read as a declaration proving the file has no package — losing
// the package for every such file, and with it the bundle key the backend
// rebuilds from a retraced class name.
func javaPackageOf(data []byte) string {
	if len(data) > androidPackageScanLimit {
		data = data[:androidPackageScanLimit]
	}
	scanner := bufio.NewScanner(bytes.NewReader(stripJVMComments(data)))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// File annotations (@file:JvmName, and Java's package annotations) may
		// precede the package statement.
		if line == "" || strings.HasPrefix(line, "@") {
			continue
		}
		if rest, ok := cutJVMKeyword(line, "package"); ok {
			pkg := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(rest), ";"))
			// Guard against anything following on the same line ("package a.b; class C").
			if i := strings.IndexAny(pkg, " \t"); i >= 0 {
				pkg = pkg[:i]
			}
			return strings.TrimSuffix(pkg, ";")
		}
		if _, ok := cutJVMKeyword(line, "import"); ok {
			return "" // past the package slot
		}
		return "" // a real declaration: this file has no package
	}
	return ""
}

// cutJVMKeyword reports whether line opens with keyword as a whole word,
// returning the remainder. Matching the bare prefix would also accept an
// identifier that merely starts with it ("packageName" is not "package").
func cutJVMKeyword(line, keyword string) (string, bool) {
	rest, ok := strings.CutPrefix(line, keyword)
	if !ok || rest == "" {
		return "", false
	}
	if rest[0] != ' ' && rest[0] != '\t' {
		return "", false
	}
	return rest, true
}

// stripJVMComments replaces Java/Kotlin comments with a space, preserving
// newlines so the result stays line-addressable. String and character literals
// are honoured so a "//" or "/*" inside one (an annotation argument, the only
// literal that can appear before a package statement) does not open a comment.
//
// Nested block comments are Kotlin-legal, so nesting is tracked; in Java the
// first "*/" closes, but a Java file with "/*" inside a block comment is
// vanishingly rare next to a Kotlin file that nests deliberately.
func stripJVMComments(data []byte) []byte {
	out := make([]byte, 0, len(data))
	depth := 0
	for i := 0; i < len(data); i++ {
		c := data[i]
		if depth > 0 {
			switch {
			case c == '/' && i+1 < len(data) && data[i+1] == '*':
				depth++
				i++
			case c == '*' && i+1 < len(data) && data[i+1] == '/':
				depth--
				i++
			case c == '\n':
				out = append(out, '\n')
			}
			continue
		}
		switch {
		case c == '/' && i+1 < len(data) && data[i+1] == '*':
			depth++
			i++
			out = append(out, ' ')
		case c == '/' && i+1 < len(data) && data[i+1] == '/':
			for i < len(data) && data[i] != '\n' {
				i++
			}
			out = append(out, ' ')
			if i < len(data) {
				out = append(out, '\n')
			}
		case c == '"' || c == '\'':
			quote := c
			out = append(out, c)
			i++
			for i < len(data) && data[i] != quote && data[i] != '\n' {
				if data[i] == '\\' && i+1 < len(data) {
					out = append(out, data[i])
					i++
				}
				out = append(out, data[i])
				i++
			}
			if i < len(data) {
				out = append(out, data[i])
			}
		default:
			out = append(out, c)
		}
	}
	return out
}
