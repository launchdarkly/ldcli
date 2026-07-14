package scanner

import (
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
)

var LangGo = Language{
	Name:         "go",
	Extensions:   []string{".go"},
	GetParser:    golang.GetLanguage,
	ScanFile:     scanFileGo,
	ResolveCalls: resolveCallsGo,
}

// flagfn factory functions register a generated flag accessor. In gonfalon's
// generated raw_flags_auto.go, each flag produces a registration like
//
//	rawFlagFnsDogfood.EnableFoo = flagfn.NewBool(EnableFooFlagKey, false, ...)
//
// mapping a struct method name (EnableFoo) to a flag-key const. This is the Go
// analog of TS's createFlagFunction wrapper factory: the method name is the
// accessor, resolved at call sites like `get.Flags(ctx).EnableFoo()`. The value
// is the variation type the factory produces.
var goWrapperFactories = map[string]string{
	"NewBool":    "bool",
	"NewInt":     "int",
	"NewFloat64": "float64",
	"NewString":  "string",
	"NewJson":    "json",
	"NewJSON":    "json",
}

// Go SDK variation methods on *LDClient.
// Key is always the first string arg for non-Ctx methods,
// and the second arg (after context.Context) for *Ctx methods.
var goVariationMethods = map[string]VariationMethod{
	// Bool
	"BoolVariation":          {FlagKeyArg: 0, DefaultArg: 2},
	"BoolVariationCtx":       {FlagKeyArg: 1, DefaultArg: 3},
	"BoolVariationDetail":    {FlagKeyArg: 0, DefaultArg: 2},
	"BoolVariationDetailCtx": {FlagKeyArg: 1, DefaultArg: 3},
	// Int
	"IntVariation":          {FlagKeyArg: 0, DefaultArg: 2},
	"IntVariationCtx":       {FlagKeyArg: 1, DefaultArg: 3},
	"IntVariationDetail":    {FlagKeyArg: 0, DefaultArg: 2},
	"IntVariationDetailCtx": {FlagKeyArg: 1, DefaultArg: 3},
	// Float64
	"Float64Variation":          {FlagKeyArg: 0, DefaultArg: 2},
	"Float64VariationCtx":       {FlagKeyArg: 1, DefaultArg: 3},
	"Float64VariationDetail":    {FlagKeyArg: 0, DefaultArg: 2},
	"Float64VariationDetailCtx": {FlagKeyArg: 1, DefaultArg: 3},
	// String
	"StringVariation":          {FlagKeyArg: 0, DefaultArg: 2},
	"StringVariationCtx":       {FlagKeyArg: 1, DefaultArg: 3},
	"StringVariationDetail":    {FlagKeyArg: 0, DefaultArg: 2},
	"StringVariationDetailCtx": {FlagKeyArg: 1, DefaultArg: 3},
	// JSON
	"JSONVariation":          {FlagKeyArg: 0, DefaultArg: 2},
	"JSONVariationCtx":       {FlagKeyArg: 1, DefaultArg: 3},
	"JSONVariationDetail":    {FlagKeyArg: 0, DefaultArg: 2},
	"JSONVariationDetailCtx": {FlagKeyArg: 1, DefaultArg: 3},
	// Migration
	"MigrationVariation":    {FlagKeyArg: 0, DefaultArg: 2},
	"MigrationVariationCtx": {FlagKeyArg: 1, DefaultArg: 3},
}

// Dogfood wrapper patterns — foundation's dogfood package wraps the SDK.
// dogfood.BoolVariation(key, user, default) — key at arg 0
// dogfood.BoolVariation2(ctx, key, ldCtx, default) — key at arg 1
var goDogfoodMethods = map[string]VariationMethod{
	"BoolVariation2":    {FlagKeyArg: 1, DefaultArg: 3},
	"IntVariation2":     {FlagKeyArg: 1, DefaultArg: 3},
	"Float64Variation2": {FlagKeyArg: 1, DefaultArg: 3},
	"StringVariation2":  {FlagKeyArg: 1, DefaultArg: 3},
	"JsonVariation2":    {FlagKeyArg: 1, DefaultArg: 3},
	"JSONVariation2":    {FlagKeyArg: 1, DefaultArg: 3},
}

func scanFileGo(root *sitter.Node, src []byte, filePath string) []FlagReference {
	var refs []FlagReference
	lines := splitLines(src)

	// Collect file-local string constants first so flag keys referenced through a
	// const (e.g. flagfn.NewBool(EnableFooFlagKey, ...) or
	// client.BoolVariation(fooKey, ...)) resolve. Genuinely dynamic keys stay "".
	consts := collectGoConsts(root, src)

	walkNodes(root, func(node *sitter.Node) {
		if node.Type() == "call_expression" {
			if found := checkCallExpressionGo(node, src, filePath, lines, consts); found != nil {
				refs = append(refs, *found)
			}
		}
	})
	return refs
}

func checkCallExpressionGo(node *sitter.Node, src []byte, filePath string, lines []string, consts map[string]string) *FlagReference {
	fn := node.ChildByFieldName("function")
	if fn == nil {
		return nil
	}
	args := node.ChildByFieldName("arguments")
	if args == nil {
		return nil
	}

	fnName, receiver := extractFunctionNameGo(fn, src)
	if fnName == "" {
		return nil
	}

	if method, ok := goVariationMethods[fnName]; ok && receiver != "" {
		return extractGoVariationCall(node, args, src, filePath, lines, fnName, "variation", method, consts)
	}

	if method, ok := goDogfoodMethods[fnName]; ok && receiver != "" {
		return extractGoVariationCall(node, args, src, filePath, lines, fnName, "variation", method, consts)
	}

	if varType, ok := goWrapperFactories[fnName]; ok && receiver != "" {
		return extractGoWrapperDefinition(node, args, src, filePath, lines, fnName, varType, consts)
	}

	return nil
}

// Go uses selector_expression for method calls: receiver.Method(...)
func extractFunctionNameGo(fn *sitter.Node, src []byte) (name string, receiver string) {
	if fn.Type() == "selector_expression" {
		operand := fn.ChildByFieldName("operand")
		field := fn.ChildByFieldName("field")
		if field != nil {
			fieldName := field.Content(src)
			receiverName := ""
			if operand != nil {
				receiverName = operand.Content(src)
			}
			return fieldName, receiverName
		}
	}
	if fn.Type() == "identifier" {
		return fn.Content(src), ""
	}
	return "", ""
}

func extractGoVariationCall(callNode, args *sitter.Node, src []byte, filePath string, lines []string, method, kind string, vm VariationMethod, consts map[string]string) *FlagReference {
	flagKeyNode := nthNamedChild(args, vm.FlagKeyArg)
	if flagKeyNode == nil {
		return nil
	}

	flagKey := resolveGoFlagKeyNode(flagKeyNode, src, consts)
	if flagKey == "" {
		return nil
	}

	line := int(callNode.StartPoint().Row) + 1
	col := int(callNode.StartPoint().Column) + 1

	defaultVal := ""
	if vm.DefaultArg >= 0 {
		defaultNode := nthNamedChild(args, vm.DefaultArg)
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

// extractGoWrapperDefinition turns a flagfn.New* registration into a
// wrapper-definition: the accessor (method) name comes from the assignment
// target (`rawFlagFnsDogfood.EnableFoo = ...`), the flag key from the first
// argument resolved through file-local consts.
func extractGoWrapperDefinition(callNode, args *sitter.Node, src []byte, filePath string, lines []string, method, varType string, consts map[string]string) *FlagReference {
	firstArg := nthNamedChild(args, 0)
	if firstArg == nil {
		return nil
	}

	flagKey := resolveGoFlagKeyNode(firstArg, src, consts)
	if flagKey == "" {
		return nil
	}

	exportName := goAssignmentTargetName(callNode, src)
	if exportName == "" {
		return nil
	}

	line := int(callNode.StartPoint().Row) + 1
	col := int(callNode.StartPoint().Column) + 1

	defaultVal := ""
	if secondArg := nthNamedChild(args, 1); secondArg != nil {
		defaultVal = secondArg.Content(src)
	}

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
		VariationType:   varType,
	}
}

// goAssignmentTargetName walks up from a factory call to the assignment that
// stores it and returns the accessor name — the field of a selector target
// (`x.EnableFoo = ...` -> "EnableFoo") or a bare identifier target.
func goAssignmentTargetName(node *sitter.Node, src []byte) string {
	current := node
	for current != nil {
		if current.Type() == "assignment_statement" || current.Type() == "short_var_declaration" {
			left := current.ChildByFieldName("left")
			if left == nil {
				return ""
			}
			target := left.NamedChild(0)
			if target == nil {
				return ""
			}
			switch target.Type() {
			case "selector_expression":
				if field := target.ChildByFieldName("field"); field != nil {
					return field.Content(src)
				}
			case "identifier":
				return target.Content(src)
			}
			return ""
		}
		current = current.Parent()
	}
	return ""
}

// resolveCallsGo is the Go pass-2: a generated flag accessor is invoked as a
// zero-arg method whose name is a known accessor — `get.Flags(ctx).EnableFoo()`
// or, via a stored receiver, `flags.EnableFoo()`. The accessor names are
// flag-specific generated identifiers, so matching the method name against the
// definition map (and requiring zero args) resolves them without data-flow.
func resolveCallsGo(rc ResolveContext) []FlagReference {
	if len(rc.ExportToFlagKey) == 0 {
		return nil
	}

	var refs []FlagReference
	walkNodes(rc.Root, func(node *sitter.Node) {
		if node.Type() != "call_expression" {
			return
		}
		fn := node.ChildByFieldName("function")
		args := node.ChildByFieldName("arguments")
		if fn == nil || fn.Type() != "selector_expression" || args == nil || args.NamedChildCount() != 0 {
			return
		}
		field := fn.ChildByFieldName("field")
		if field == nil {
			return
		}
		name := field.Content(rc.Src)
		flagKey, ok := rc.ExportToFlagKey[name]
		if !ok {
			return
		}
		line := int(node.StartPoint().Row) + 1
		col := int(node.StartPoint().Column) + 1
		refs = append(refs, FlagReference{
			FlagKey:         flagKey,
			FilePath:        rc.FilePath,
			Line:            line,
			Column:          col,
			Kind:            "wrapper-call",
			Method:          name + "()",
			SurroundingCode: getSurroundingLines(rc.Lines, line-1, 1),
			WrapperName:     name,
		})
	})
	return refs
}

// collectGoConsts maps const names bound to a string literal to their value, so
// flag keys referenced through a const resolve. Handles single-name const specs
// (`Foo = "foo"`), including grouped `const ( ... )` blocks.
func collectGoConsts(root *sitter.Node, src []byte) map[string]string {
	consts := make(map[string]string)

	walkNodes(root, func(node *sitter.Node) {
		if node.Type() != "const_spec" {
			return
		}
		var name string
		var lit string
		for i := 0; i < int(node.NamedChildCount()); i++ {
			child := node.NamedChild(i)
			switch child.Type() {
			case "identifier":
				if name == "" {
					name = child.Content(src)
				}
			case "expression_list":
				if v := child.NamedChild(0); v != nil {
					lit = extractGoStringLiteral(v, src)
				}
			}
		}
		if name != "" && lit != "" {
			consts[name] = lit
		}
	})
	return consts
}

// resolveGoFlagKeyNode resolves a flag-key argument to a string: a direct string
// literal, or an identifier/selector that maps to one via file-local consts. A
// genuinely dynamic expression resolves to "" and is intentionally skipped.
func resolveGoFlagKeyNode(node *sitter.Node, src []byte, consts map[string]string) string {
	if lit := extractGoStringLiteral(node, src); lit != "" {
		return lit
	}
	switch node.Type() {
	case "identifier", "selector_expression":
		return consts[node.Content(src)]
	}
	return ""
}

func extractGoStringLiteral(node *sitter.Node, src []byte) string {
	// Go interpreted string literal: "flag-key"
	if node.Type() == "interpreted_string_literal" {
		raw := node.Content(src)
		if len(raw) >= 2 {
			return raw[1 : len(raw)-1]
		}
	}
	// Go raw string literal: `flag-key`
	if node.Type() == "raw_string_literal" {
		raw := node.Content(src)
		if len(raw) >= 2 {
			return raw[1 : len(raw)-1]
		}
	}
	return ""
}

func nthNamedChild(node *sitter.Node, n int) *sitter.Node {
	count := int(node.NamedChildCount())
	if n < count {
		return node.NamedChild(n)
	}
	return nil
}
