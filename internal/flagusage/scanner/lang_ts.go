package scanner

import (
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
)

var LangTypeScript = Language{
	Name:         "typescript",
	Extensions:   []string{".ts", ".tsx", ".js", ".jsx"},
	GetParser:    typescript.GetLanguage,
	ScanFile:     scanFileTS,
	ResolveCalls: resolveCallsTS,
}

// resolveCallsTS is the TS pass-2: a wrapper accessor is a bare function imported
// from a wrapper module (or whose export name is a known definition) and invoked
// as `enableFoo()`. See importedWrappers / findWrapperCalls.
func resolveCallsTS(rc ResolveContext) []FlagReference {
	localToExport := importedWrappers(rc.Root, rc.Src, rc.WrapperModules, rc.ExportToFlagKey)
	if len(localToExport) == 0 {
		return nil
	}
	return findWrapperCalls(rc.Root, rc.Src, rc.FilePath, rc.Lines, localToExport, rc.ExportToFlagKey)
}

// SDK method names that take a flag key as the first argument.
var tsVariationMethods = map[string]bool{
	"variation":             true,
	"boolVariation":         true,
	"stringVariation":       true,
	"intVariation":          true,
	"floatVariation":        true,
	"numberVariation":       true,
	"jsonVariation":         true,
	"doubleVariation":       true,
	"variationDetail":       true,
	"boolVariationDetail":   true,
	"stringVariationDetail": true,
	"intVariationDetail":    true,
	"floatVariationDetail":  true,
	"numberVariationDetail": true,
	"jsonVariationDetail":   true,
}

// React hook names that take a flag key as the first argument.
var tsVariationHooks = map[string]bool{
	"useVariation":       true,
	"useBoolVariation":   true,
	"useStringVariation": true,
	"useNumberVariation": true,
	"useJsonVariation":   true,
	"useVariationDetail": true,
}

// Hooks that return a flags object where property accesses are flag keys.
var tsFlagsObjectHooks = map[string]bool{
	"useFlags":   true,
	"useLDFlags": true,
}

// Known wrapper factory function names (like gonfalon's createFlagFunction).
var tsWrapperFactories = map[string]bool{
	"createFlagFunction": true,
}

func scanFileTS(root *sitter.Node, src []byte, filePath string) []FlagReference {
	var refs []FlagReference
	lines := splitLines(src)

	// Collect file-local string constants and enum members first, so flag-key
	// arguments referenced indirectly (const FOO = 'foo'; variation(FOO)) resolve.
	consts := collectTSConsts(root, src)

	walkNodes(root, func(node *sitter.Node) {
		if node.Type() != "call_expression" {
			return
		}
		found := checkCallExpressionTS(node, src, filePath, lines, consts)
		if found == nil {
			return
		}
		refs = append(refs, *found)
		// If this variation/hook call is the *entire body* of a named function,
		// that function is a thin wrapper — register it so its remote call sites
		// (which carry no literal key) resolve too.
		if found.Kind == "variation" || found.Kind == "hook" {
			if name := thinWrapperName(node, src); name != "" {
				wd := *found
				wd.Kind = "wrapper-definition"
				wd.WrapperName = name
				refs = append(refs, wd)
			}
		}
	})
	return refs
}

func checkCallExpressionTS(node *sitter.Node, src []byte, filePath string, lines []string, consts map[string]string) *FlagReference {
	fn := node.ChildByFieldName("function")
	if fn == nil {
		return nil
	}
	args := node.ChildByFieldName("arguments")
	if args == nil {
		return nil
	}

	fnName, receiver := extractFunctionNameTS(fn, src)
	if fnName == "" {
		return nil
	}

	if tsVariationMethods[fnName] && receiver != "" {
		return extractVariationCall(node, args, src, filePath, lines, fnName, "variation", 0, 1, consts)
	}

	if tsVariationHooks[fnName] {
		return extractVariationCall(node, args, src, filePath, lines, fnName, "hook", 0, 1, consts)
	}

	if tsWrapperFactories[fnName] {
		return extractWrapperDefinition(node, args, src, filePath, lines, fnName, consts)
	}

	return nil
}

// collectTSConsts builds a map of identifiers and enum members that are bound to
// a string literal, so flag keys referenced through a constant can be resolved.
// Handles `const FOO = 'foo'` (identifier -> literal) and
// `enum E { Member = 'foo' }` (E.Member -> literal).
func collectTSConsts(root *sitter.Node, src []byte) map[string]string {
	consts := make(map[string]string)

	var walk func(node *sitter.Node, enumName string)
	walk = func(node *sitter.Node, enumName string) {
		if node == nil {
			return
		}

		switch node.Type() {
		case "variable_declarator":
			name := node.ChildByFieldName("name")
			value := node.ChildByFieldName("value")
			if name != nil && name.Type() == "identifier" && value != nil {
				if lit := extractStringLiteral(value, src); lit != "" {
					consts[name.Content(src)] = lit
				}
			}
		case "enum_declaration":
			en := ""
			if nm := node.ChildByFieldName("name"); nm != nil {
				en = nm.Content(src)
			}
			for i := 0; i < int(node.ChildCount()); i++ {
				walk(node.Child(i), en)
			}
			return
		case "enum_assignment", "property_signature", "pair":
			// `Member = 'literal'` inside an enum body.
			if enumName != "" {
				nm := node.ChildByFieldName("name")
				val := node.ChildByFieldName("value")
				if nm != nil && val != nil {
					if lit := extractStringLiteral(val, src); lit != "" {
						consts[enumName+"."+nm.Content(src)] = lit
					}
				}
			}
		}

		for i := 0; i < int(node.ChildCount()); i++ {
			walk(node.Child(i), enumName)
		}
	}

	walk(root, "")
	return consts
}

// thinWrapperName returns the bound name if `call` is the *entire body* of a
// thin wrapper function — an arrow whose expression body is the call, or a
// function/arrow whose only statement returns the call. Anything with other
// statements (e.g. a component that uses a flag and returns JSX) yields "" and
// is NOT treated as a wrapper.
func thinWrapperName(call *sitter.Node, src []byte) string {
	parent := call.Parent()
	if parent == nil {
		return ""
	}
	switch parent.Type() {
	case "arrow_function":
		if parent.ChildByFieldName("body") != call {
			return "" // call is an argument/sub-expression, not the whole body
		}
		return bindingName(parent, src)
	case "return_statement":
		block := parent.Parent()
		if block == nil || block.Type() != "statement_block" || block.NamedChildCount() != 1 {
			return "" // wrapper body must be a sole `return <call>`
		}
		return bindingName(block.Parent(), src)
	}
	return ""
}

// bindingName returns the name a function/arrow node is bound to:
//
//	export function NAME() {...}    -> function_declaration name field
//	export const NAME = () => ...   -> enclosing variable_declarator
func bindingName(fnNode *sitter.Node, src []byte) string {
	if fnNode == nil {
		return ""
	}
	if fnNode.Type() == "function_declaration" {
		if nm := fnNode.ChildByFieldName("name"); nm != nil {
			return nm.Content(src)
		}
		return ""
	}
	return findExportName(fnNode, src)
}

func extractFunctionNameTS(fn *sitter.Node, src []byte) (name string, receiver string) {
	switch fn.Type() {
	case "identifier":
		return fn.Content(src), ""
	case "member_expression":
		obj := fn.ChildByFieldName("object")
		prop := fn.ChildByFieldName("property")
		if prop != nil {
			propName := prop.Content(src)
			objName := ""
			if obj != nil {
				objName = obj.Content(src)
			}
			return propName, objName
		}
	}
	return "", ""
}
