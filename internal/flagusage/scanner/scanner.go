package scanner

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

var languages = []Language{
	LangTypeScript,
	LangGo,
}

// extToLang maps file extensions to their Language config.
var extToLang map[string]*Language

func init() {
	extToLang = make(map[string]*Language)
	for i := range languages {
		for _, ext := range languages[i].Extensions {
			extToLang[ext] = &languages[i]
		}
	}
}

type ScanOptions struct {
	// Module paths that export wrapper functions (e.g. "@gonfalon/dogfood-flags").
	// If set, the scanner will resolve imports from these modules and track call sites.
	WrapperModules []string

	// DefinitionsDir is an optional separate directory containing wrapper definition files
	// (e.g. the dogfood-flags package). If set, definitions are loaded from here first
	// to build the exportName→flagKey mapping, even if the main scan dir doesn't contain them.
	DefinitionsDir string
}

func Scan(rootDir string, opts ...ScanOptions) (*ScanResult, error) {
	files, err := collectFiles(rootDir)
	if err != nil {
		return nil, err
	}

	var opt ScanOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	result := &ScanResult{
		Stats: ScanStats{ByKind: make(map[string]int)},
	}

	// Build per-language parsers lazily.
	parsers := make(map[string]*sitter.Parser)
	getParser := func(lang *Language) *sitter.Parser {
		if p, ok := parsers[lang.Name]; ok {
			return p
		}
		p := sitter.NewParser()
		p.SetLanguage(lang.GetParser())
		parsers[lang.Name] = p
		return p
	}

	type parsedFile = scanParsedFile

	// Pre-load definitions from a separate directory if specified.
	exportToFlagKey := make(map[string]string)
	if opt.DefinitionsDir != "" {
		defFiles, err := collectFiles(opt.DefinitionsDir)
		if err != nil {
			return nil, err
		}
		for _, path := range defFiles {
			lang := langForFile(path)
			if lang == nil {
				continue
			}
			src, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			tree, err := getParser(lang).ParseCtx(context.Background(), nil, src)
			if err != nil {
				continue
			}
			for _, ref := range lang.ScanFile(tree.RootNode(), src, path) {
				if ref.Kind == "wrapper-definition" && ref.WrapperName != "" {
					exportToFlagKey[ref.WrapperName] = ref.FlagKey
					result.Wrappers = append(result.Wrappers, WrapperMapping{
						ExportName:   ref.WrapperName,
						FlagKey:      ref.FlagKey,
						DefaultValue: ref.DefaultValue,
						FilePath:     ref.FilePath,
						Line:         ref.Line,
					})
				}
			}
		}
	}

	var parsed []parsedFile

	// Pass 1: parse all files, collect direct SDK refs and wrapper definitions
	for _, path := range files {
		lang := langForFile(path)
		if lang == nil {
			continue
		}

		src, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		tree, err := getParser(lang).ParseCtx(context.Background(), nil, src)
		if err != nil {
			continue
		}

		refs := lang.ScanFile(tree.RootNode(), src, path)
		result.References = append(result.References, refs...)
		result.Stats.FilesScanned++

		parsed = append(parsed, parsedFile{path: path, src: src, tree: tree, lang: lang})
	}

	// Also collect definitions from the main scan (for single-dir mode)
	for _, ref := range result.References {
		if ref.Kind == "wrapper-definition" && ref.WrapperName != "" {
			if _, exists := exportToFlagKey[ref.WrapperName]; !exists {
				exportToFlagKey[ref.WrapperName] = ref.FlagKey
				result.Wrappers = append(result.Wrappers, WrapperMapping{
					ExportName:   ref.WrapperName,
					FlagKey:      ref.FlagKey,
					DefaultValue: ref.DefaultValue,
					FilePath:     ref.FilePath,
					Line:         ref.Line,
				})
			}
		}
	}

	// Auto-discover wrapper definitions from imported modules. This ALWAYS runs and
	// only *adds* mappings the explicit sources didn't already provide (existing
	// keys from -definitions / in-tree win via the !exists checks below). Running it
	// unconditionally means an explicit -definitions pointing at the wrong directory
	// can no longer SUPPRESS resolution — at worst it contributes nothing and
	// node_modules discovery still resolves the wrappers.
	{
		modules := collectWrapperCandidateModules(parsed)
		for mod := range modules {
			srcDir := resolveModuleSourceDir(mod, rootDir)
			if srcDir == "" {
				continue
			}
			if !dirContainsWrapperFactory(srcDir) {
				continue
			}
			defFiles, err := collectFiles(srcDir)
			if err != nil {
				continue
			}
			for _, path := range defFiles {
				lang := langForFile(path)
				if lang == nil {
					continue
				}
				src, err := os.ReadFile(path)
				if err != nil {
					continue
				}
				tree, err := getParser(lang).ParseCtx(context.Background(), nil, src)
				if err != nil {
					continue
				}
				for _, ref := range lang.ScanFile(tree.RootNode(), src, path) {
					if ref.Kind == "wrapper-definition" && ref.WrapperName != "" {
						if _, exists := exportToFlagKey[ref.WrapperName]; !exists {
							exportToFlagKey[ref.WrapperName] = ref.FlagKey
							result.Wrappers = append(result.Wrappers, WrapperMapping{
								ExportName:   ref.WrapperName,
								FlagKey:      ref.FlagKey,
								DefaultValue: ref.DefaultValue,
								FilePath:     ref.FilePath,
								Line:         ref.Line,
							})
						}
					}
				}
			}
		}
	}

	// Pass 2: resolve accessor call sites. Each language with a ResolveCalls hook
	// gets the shared accessor-name→flag-key map (definitions discovered in-tree,
	// loaded via -definitions, or auto-discovered) plus any explicitly named
	// wrapper modules. Runs whenever we have something to map against — once
	// definitions are in scope, call sites resolve with no -wrapper-modules flag.
	if len(opt.WrapperModules) > 0 || len(exportToFlagKey) > 0 {
		wrapperModuleSet := make(map[string]bool, len(opt.WrapperModules))
		for _, m := range opt.WrapperModules {
			wrapperModuleSet[m] = true
		}

		for _, pf := range parsed {
			if pf.lang.ResolveCalls == nil {
				continue
			}
			refs := pf.lang.ResolveCalls(ResolveContext{
				Root:            pf.tree.RootNode(),
				Src:             pf.src,
				FilePath:        pf.path,
				Lines:           strings.Split(string(pf.src), "\n"),
				ExportToFlagKey: exportToFlagKey,
				WrapperModules:  wrapperModuleSet,
			})
			result.References = append(result.References, refs...)
		}
	}

	flagKeys := make(map[string]bool)
	for _, ref := range result.References {
		flagKeys[ref.FlagKey] = true
		result.Stats.ByKind[ref.Kind]++
	}
	result.Stats.ReferencesFound = len(result.References)
	result.Stats.UniqueFlags = len(flagKeys)

	return result, nil
}

func langForFile(path string) *Language {
	ext := filepath.Ext(path)
	return extToLang[ext]
}

func collectFiles(root string) ([]string, error) {
	var files []string
	skipDirs := map[string]bool{
		"node_modules": true, "dist": true, "build": true,
		".git": true, ".next": true, "coverage": true,
		"__generated__": true, ".cache": true, "vendor": true,
	}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] || (strings.HasPrefix(d.Name(), ".") && d.Name() != ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if langForFile(path) != nil {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

// splitLines is a shared helper for all languages.
func splitLines(src []byte) []string {
	return strings.Split(string(src), "\n")
}

// walkNodes performs a pre-order depth-first traversal of the tree rooted at
// node, invoking visit on every node. It is the shared traversal primitive for
// every language's pass-1/pass-2 scan, so detection code declares *what* to match
// (the visit body) rather than re-implementing the recursion. Visitors that must
// short-circuit descent or thread contextual state down the tree (e.g.
// collectTSConsts' enum name) keep their own recursion instead.
func walkNodes(node *sitter.Node, visit func(*sitter.Node)) {
	if node == nil {
		return
	}
	visit(node)
	for i := 0; i < int(node.ChildCount()); i++ {
		walkNodes(node.Child(i), visit)
	}
}

func extractVariationCall(callNode, args *sitter.Node, src []byte, filePath string, lines []string, method, kind string, flagKeyArg, defaultArg int, consts map[string]string) *FlagReference {
	flagKeyNode := nthNamedChild(args, flagKeyArg)
	if flagKeyNode == nil {
		return nil
	}

	flagKey := resolveFlagKeyNode(flagKeyNode, src, consts)
	if flagKey == "" {
		return nil
	}

	line := int(callNode.StartPoint().Row) + 1
	col := int(callNode.StartPoint().Column) + 1

	defaultVal := ""
	if defaultArg >= 0 {
		defaultNode := nthNamedChild(args, defaultArg)
		if defaultNode != nil {
			defaultVal = defaultNode.Content(src)
		}
	}

	return &FlagReference{
		FlagKey:         flagKey,
		FilePath:        filePath,
		Line:            line,
		Column:          col,
		Kind:            kind,
		Method:          method,
		DefaultValue:    truncate(defaultVal, 80),
		SurroundingCode: getSurroundingLines(lines, line-1, 1),
	}
}

func extractWrapperDefinition(callNode, args *sitter.Node, src []byte, filePath string, lines []string, method string, consts map[string]string) *FlagReference {
	firstArg := nthNamedChild(args, 0)
	if firstArg == nil {
		return nil
	}

	flagKey := resolveFlagKeyNode(firstArg, src, consts)
	if flagKey == "" {
		return nil
	}

	line := int(callNode.StartPoint().Row) + 1
	col := int(callNode.StartPoint().Column) + 1

	defaultVal := ""
	secondArg := nthNamedChild(args, 1)
	if secondArg != nil {
		defaultVal = secondArg.Content(src)
	}

	exportName := findExportName(callNode, src)

	return &FlagReference{
		FlagKey:         flagKey,
		FilePath:        filePath,
		Line:            line,
		Column:          col,
		Kind:            "wrapper-definition",
		Method:          method,
		DefaultValue:    truncate(defaultVal, 80),
		SurroundingCode: getSurroundingLines(lines, line-1, 0),
		WrapperName:     exportName,
	}
}

// findExportName walks up from a call expression to find the variable declaration name.
// Handles: export const foo = createFlagFunction(...)
//
//	const foo = createFlagFunction(...)
func findExportName(node *sitter.Node, src []byte) string {
	current := node
	for current != nil {
		if current.Type() == "variable_declarator" {
			nameNode := current.ChildByFieldName("name")
			if nameNode != nil {
				return nameNode.Content(src)
			}
		}
		if current.Type() == "lexical_declaration" || current.Type() == "export_statement" {
			break
		}
		current = current.Parent()
	}
	return ""
}

// resolveFlagKeyNode resolves a flag-key argument to a string. It accepts a
// direct string/template literal, or — following flag-key constants — an
// identifier or member expression (e.g. an enum member) that maps to a string
// literal in `consts`. A genuinely dynamic expression (function param, call,
// concatenation) resolves to "" and is intentionally skipped.
func resolveFlagKeyNode(node *sitter.Node, src []byte, consts map[string]string) string {
	if lit := extractStringLiteral(node, src); lit != "" {
		return lit
	}
	switch node.Type() {
	case "identifier", "member_expression":
		return consts[node.Content(src)]
	}
	return ""
}

func extractStringLiteral(node *sitter.Node, src []byte) string {
	if node.Type() == "string" {
		raw := node.Content(src)
		return strings.Trim(raw, "\"'`")
	}
	// template_string with no interpolation
	if node.Type() == "template_string" {
		for i := 0; i < int(node.NamedChildCount()); i++ {
			if node.NamedChild(i).Type() == "template_substitution" {
				return ""
			}
		}
		raw := node.Content(src)
		return strings.Trim(raw, "`")
	}
	return ""
}

func getSurroundingLines(lines []string, centerIdx, context int) string {
	start := centerIdx - context
	if start < 0 {
		start = 0
	}
	end := centerIdx + context + 1
	if end > len(lines) {
		end = len(lines)
	}
	selected := lines[start:end]
	for i := range selected {
		selected[i] = strings.TrimRight(selected[i], " \t\r")
	}
	return strings.Join(selected, "\n")
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
