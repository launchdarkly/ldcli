package scanner

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// wrapperFactoryPatterns are byte patterns that indicate a file might contain
// wrapper definitions. Used as a fast pre-filter before tree-sitter parsing.
var wrapperFactoryPatterns = [][]byte{
	[]byte("createFlagFunction"),
}

func init() {
	for name := range tsWrapperFactories {
		p := []byte(name)
		found := false
		for _, existing := range wrapperFactoryPatterns {
			if string(existing) == string(p) {
				found = true
				break
			}
		}
		if !found {
			wrapperFactoryPatterns = append(wrapperFactoryPatterns, p)
		}
	}
}

// dirContainsWrapperFactory does a fast byte scan of files in dir to check
// if any contain a wrapper factory pattern. Returns true on first match.
func dirContainsWrapperFactory(dir string) bool {
	files, err := collectFiles(dir)
	if err != nil {
		return false
	}
	for _, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, pattern := range wrapperFactoryPatterns {
			if bytes.Contains(src, pattern) {
				return true
			}
		}
	}
	return false
}

// resolveModuleSourceDir resolves a bare npm module specifier (e.g.
// "@gonfalon/dogfood-flags") to its source directory by walking up from
// startDir looking for node_modules/<module>. If found, it reads the
// package's package.json to locate the source entry point and returns
// the directory containing it. Falls back to the package root if no
// entry point is found.
func resolveModuleSourceDir(module, startDir string) string {
	pkgDir := findNodeModulesPackage(module, startDir)
	if pkgDir == "" {
		return ""
	}

	entryDir := sourceEntryDir(pkgDir)
	if entryDir != "" {
		return entryDir
	}
	return pkgDir
}

// findNodeModulesPackage walks up from dir looking for node_modules/<module>.
func findNodeModulesPackage(module, dir string) string {
	dir, _ = filepath.Abs(dir)
	for {
		candidate := filepath.Join(dir, "node_modules", module)
		resolved, err := filepath.EvalSymlinks(candidate)
		if err == nil {
			info, err := os.Stat(resolved)
			if err == nil && info.IsDir() {
				return resolved
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// sourceEntryDir reads a package's package.json and returns the directory
// containing the main source entry point. Checks exports["."], then main.
func sourceEntryDir(pkgDir string) string {
	data, err := os.ReadFile(filepath.Join(pkgDir, "package.json"))
	if err != nil {
		return ""
	}

	var pkg struct {
		Main    string          `json:"main"`
		Exports json.RawMessage `json:"exports"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return ""
	}

	if entry := parseExportsDot(pkg.Exports); entry != "" {
		dir := filepath.Dir(filepath.Join(pkgDir, entry))
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}

	if pkg.Main != "" {
		dir := filepath.Dir(filepath.Join(pkgDir, pkg.Main))
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}

	return ""
}

// parseExportsDot extracts the "." entry from a package.json exports field.
// Handles both `exports: { ".": "./src/index.ts" }` and `exports: { ".": { import: "...", default: "..." } }`.
func parseExportsDot(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return ""
	}

	dot, ok := obj["."]
	if !ok {
		return ""
	}

	var s string
	if json.Unmarshal(dot, &s) == nil {
		return s
	}

	var nested map[string]string
	if json.Unmarshal(dot, &nested) == nil {
		for _, key := range []string{"import", "default", "require"} {
			if v, ok := nested[key]; ok {
				return v
			}
		}
	}

	return ""
}

// collectWrapperCandidateModules finds bare modules that have at least one
// import used as a zero-arg function call — the wrapper call-site pattern.
// This narrows the search from 90+ imported modules to typically 1-2.
func collectWrapperCandidateModules(parsed []scanParsedFile) map[string]bool {
	candidates := make(map[string]bool)
	for _, pf := range parsed {
		if pf.lang.Name != "typescript" {
			continue
		}
		root, src := pf.tree.RootNode(), pf.src
		calledNames := collectCalledIdentifiers(root, src)
		if len(calledNames) == 0 {
			continue
		}
		forEachImport(root, src, func(mod, localName string) {
			if calledNames[localName] {
				candidates[mod] = true
			}
		})
	}
	return candidates
}

// forEachImport calls fn(module, localName) for every named import from a bare module.
func forEachImport(root *sitter.Node, src []byte, fn func(mod, localName string)) {
	walkNodes(root, func(node *sitter.Node) {
		if node.Type() != "import_statement" {
			return
		}
		source := node.ChildByFieldName("source")
		if source == nil {
			return
		}
		mod := strings.Trim(source.Content(src), "\"'`")
		if mod == "" || strings.HasPrefix(mod, ".") {
			return
		}
		specs := make(map[string]string)
		for i := 0; i < int(node.NamedChildCount()); i++ {
			child := node.NamedChild(i)
			if child.Type() == "import_clause" {
				extractNamedImports(child, src, specs)
			}
		}
		for local := range specs {
			fn(mod, local)
		}
	})
}

// collectCalledIdentifiers returns identifiers used as zero-arg function calls.
func collectCalledIdentifiers(root *sitter.Node, src []byte) map[string]bool {
	result := make(map[string]bool)
	walkNodes(root, func(node *sitter.Node) {
		if node.Type() != "call_expression" {
			return
		}
		fn := node.ChildByFieldName("function")
		if fn == nil || fn.Type() != "identifier" {
			return
		}
		args := node.ChildByFieldName("arguments")
		if args != nil && args.NamedChildCount() == 0 {
			result[fn.Content(src)] = true
		}
	})
	return result
}
