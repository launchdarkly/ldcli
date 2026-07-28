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

// androidSource is one file competing for a bundle key, kept with the rank of the
// source set it came from so a later, better copy can displace it.
type androidSource struct {
	data []byte
	rank int
}

// buildAndroidSourceBundle scans root for .java/.kt files and packs them keyed by
// their package-relative path (e.g. com/example/MainActivity.kt), which is what
// the backend reconstructs from a retraced frame's fully-qualified class name.
//
// The key comes from each file's own `package` declaration rather than its
// directory layout, because Kotlin does not require the two to agree. A file with
// no package declaration is keyed by its bare name (the JVM default package).
// Returns nil when nothing was found, so the caller can skip the upload.
//
// A key can be claimed by more than one file, because defining a class per build
// variant is ordinary Gradle practice: src/debug and src/main can each hold
// com/example/Config.kt. The winner is chosen by source set (see
// androidSourceRank) rather than by whichever the walk reached first, which would
// hand the key to src/debug purely because "debug" sorts before "main".
func buildAndroidSourceBundle(root string) ([]byte, int, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read source path %s: %w", root, err)
	}
	if !info.IsDir() {
		return nil, 0, fmt.Errorf("source path %s is not a directory", root)
	}

	chosen := make(map[string]androidSource)
	total := 0
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if androidSkipDirs[d.Name()] {
				return filepath.SkipDir
			}
			// Pruned rather than filtered per file: nothing under a test source set
			// is wanted. Skipping root itself is the exception, since a --source-path
			// aimed straight at one is an explicit request for it.
			if p != root && isAndroidTestSourceSet(d.Name()) && filepath.Base(filepath.Dir(p)) == "src" {
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
		if len(data) > maxSourceFileBytes {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			rel = p
		}
		key := androidSourceKey(data, d.Name())
		rank := androidSourceRank(androidSourceSetOf(rel))
		prev, seen := chosen[key]
		if seen && rank >= prev.rank {
			return nil // an equal or better copy already holds the key
		}
		// Displacing an earlier pick returns what it was holding to the budget
		// (len is 0 for the zero value, i.e. a key seen for the first time).
		next := total + len(data) - len(prev.data)
		if next > maxSourceBundleBytes {
			return nil
		}
		total = next
		chosen[key] = androidSource{data: data, rank: rank}
		return nil
	})
	if err != nil {
		return nil, 0, fmt.Errorf("failed to scan sources in %s: %w", root, err)
	}
	if len(chosen) == 0 {
		return nil, 0, nil
	}

	b := &srcbundle.Builder{}
	for key, src := range chosen {
		b.Add(key, src.data)
	}

	var buf bytes.Buffer
	if err := b.Encode(&buf); err != nil {
		return nil, 0, fmt.Errorf("failed to encode source bundle: %w", err)
	}
	return buf.Bytes(), b.Len(), nil
}

// androidSourceSetOf returns the Gradle source set a file belongs to — "main",
// "debug", a flavor name — read from the src/<set>/ segment of its root-relative
// path, or "" when the file is not laid out that way. Modules nest freely
// (app/src/main, features/login/src/main), so the innermost "src" is the one that
// names the set.
func androidSourceSetOf(rel string) string {
	segs := strings.Split(filepath.ToSlash(rel), "/")
	// Starts below the file name and the set directory: "src" itself must have a
	// directory after it for there to be a set.
	for i := len(segs) - 3; i >= 0; i-- {
		if segs[i] == "src" {
			return segs[i+1]
		}
	}
	return ""
}

// androidSourceRank orders files claiming one bundle key, lower winning. A
// variant's own copy of a class is compiled only into that variant, while the
// main source set is in every one of them, so main is the copy a frame from an
// arbitrary build is likeliest to have come from. Files outside the src/<set>/
// layout rank with the variants: nothing says they are overrides, and they are
// often the only copy there is.
//
// Ranks tie between variants (src/free and src/paid both overriding the same
// class), and the walk's lexical order settles those, so a given tree always
// bundles the same file.
func androidSourceRank(sourceSet string) int {
	if sourceSet == "main" {
		return 0
	}
	return 1
}

// androidTestSourceSets are the source sets holding test code. Both unit tests
// and instrumented tests are excluded: neither is compiled into the app whose
// mapping.txt is being uploaded, so no retraced frame can point at them, and a
// test-only class sharing a production class's package and file name would
// otherwise compete for its bundle key.
var androidTestSourceSets = []string{"test", "androidTest"}

// isAndroidTestSourceSet reports whether a source set name is a test one. Gradle
// appends the variant in camel case for the variant-specific sets ("testDebug",
// "androidTestFreeRelease"), and testFixtures is caught by the same rule.
func isAndroidTestSourceSet(sourceSet string) bool {
	for _, name := range androidTestSourceSets {
		if sourceSet == name {
			return true
		}
		if rest, ok := strings.CutPrefix(sourceSet, name); ok && rest != "" && rest[0] >= 'A' && rest[0] <= 'Z' {
			return true
		}
	}
	return false
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
