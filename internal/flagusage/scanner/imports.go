package scanner

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// importedWrappers scans a file's import declarations and returns a map of local
// binding name -> exported name for bindings that are flag accessors. A binding
// qualifies if it's imported from a named wrapper module, OR its exported name
// is a known wrapper definition (auto-discovery — so no -wrapper-modules flag is
// needed once the definitions are in scope).
// Handles: import { foo, bar as baz } from '@gonfalon/dogfood-flags'
func importedWrappers(root *sitter.Node, src []byte, wrapperModules map[string]bool, exportToFlagKey map[string]string) map[string]string {
	result := make(map[string]string)

	walkNodes(root, func(node *sitter.Node) {
		if node.Type() != "import_statement" {
			return
		}
		source := node.ChildByFieldName("source")
		if source == nil {
			return
		}
		moduleName := strings.Trim(source.Content(src), "\"'`")
		fromWrapperModule := wrapperModules[moduleName]

		specs := make(map[string]string)
		for i := 0; i < int(node.NamedChildCount()); i++ {
			child := node.NamedChild(i)
			if child.Type() == "import_clause" {
				extractNamedImports(child, src, specs)
			}
		}
		for local, exported := range specs {
			if fromWrapperModule || exportToFlagKey[exported] != "" {
				result[local] = exported
			}
		}
	})
	return result
}

func extractNamedImports(clause *sitter.Node, src []byte, result map[string]string) {
	// import_specifier nodes don't nest, so descending into one after recording it
	// is harmless — its children are the name/alias identifiers, not more specifiers.
	walkNodes(clause, func(node *sitter.Node) {
		if node.Type() != "import_specifier" {
			return
		}
		name := node.ChildByFieldName("name")
		alias := node.ChildByFieldName("alias")
		if name != nil {
			exportedName := name.Content(src)
			localName := exportedName
			if alias != nil {
				localName = alias.Content(src)
			}
			result[localName] = exportedName
		}
	})
}

// findWrapperCalls finds all call expressions where the callee is one of the
// imported wrapper functions (zero-arg calls like enableCommandPalette()).
// If exportToFlagKey is provided, it maps export names to flag keys.
// Otherwise, the export name itself is used as the flag key identifier.
func findWrapperCalls(root *sitter.Node, src []byte, filePath string, lines []string, localToExport map[string]string, exportToFlagKey map[string]string) []FlagReference {
	var refs []FlagReference

	walkNodes(root, func(node *sitter.Node) {
		if node.Type() != "call_expression" {
			return
		}
		fn := node.ChildByFieldName("function")
		if fn == nil || fn.Type() != "identifier" {
			return
		}
		localName := fn.Content(src)
		exportedName, isWrapper := localToExport[localName]
		if !isWrapper {
			return
		}

		flagKey := exportedName
		// "wrapper-call-unresolved" means we recognized the call but have no
		// definition mapping export name -> flag key, so the key is the raw export
		// name (a guess), NOT a real LD flag key. Enrichment must not look these up
		// as flags. Once ANY definition is in scope (exportToFlagKey non-empty), an
		// unknown accessor is dropped rather than guessed.
		kind := "wrapper-call-unresolved"
		if len(exportToFlagKey) > 0 {
			resolved, known := exportToFlagKey[exportedName]
			if !known {
				return
			}
			flagKey = resolved
			kind = "wrapper-call"
		}
		line := int(node.StartPoint().Row) + 1
		col := int(node.StartPoint().Column) + 1
		refs = append(refs, FlagReference{
			FlagKey:         flagKey,
			FilePath:        filePath,
			Line:            line,
			Column:          col,
			Kind:            kind,
			Method:          localName + "()",
			SurroundingCode: getSurroundingLines(lines, line-1, 1),
			WrapperName:     exportedName,
		})
	})
	return refs
}
