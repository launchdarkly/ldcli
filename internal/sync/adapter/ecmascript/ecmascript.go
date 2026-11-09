// Package ecmascript inspects TypeScript and JavaScript sources. It finds
// agents, prompts, tools, and constants through an import allowlist and never
// runs the code.
package ecmascript

import (
	"fmt"
	"maps"
	"math"
	"path"
	"slices"
	"strconv"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	"github.com/launchdarkly/ldcli/internal/sync/adapter"
	"github.com/launchdarkly/ldcli/internal/sync/adapter/code"
)

type ecmaScriptAdapter struct {
	format string
	typed  bool
}

func (adapter ecmaScriptAdapter) Format() string   { return adapter.format }
func (adapter ecmaScriptAdapter) Behavior() string { return adapter.format + "/1" }

func (adapter ecmaScriptAdapter) Inspect(bundle adapter.SourceBundle) (*adapter.Node, error) {
	return parseTypeScriptBundle(bundle, adapter.typed)
}

func (ecmaScriptAdapter) Defaults(syncdomain.Kind) (string, map[string]string) { return "", nil }

func (adapter ecmaScriptAdapter) Dependencies(document adapter.Document) ([]string, error) {
	tree, err := code.ParseTree(document, typeScriptLanguage(document.Path, adapter.typed))
	if err != nil {
		return nil, err
	}
	defer tree.Close()
	var dependencies []string
	for module := range typeScriptImportedModules(tree.RootNode(), document.Data) {
		dependencies = append(dependencies, typeScriptModulePaths(document.Path, module)...)
	}
	return dependencies, nil
}

func init() {
	adapter.MustRegister(ecmaScriptAdapter{format: "typescript", typed: true})
	adapter.MustRegister(ecmaScriptAdapter{format: "javascript"})
}

func parseTypeScriptBundle(bundle adapter.SourceBundle, typed bool) (*adapter.Node, error) {
	return parseTypeScript(bundle.Primary, typed, typeScriptImportedValues(bundle, typed))
}

// typeScriptLanguage returns the grammar of a file. JSX is a syntax error in
// the TypeScript grammar, so a .tsx file uses the TSX grammar. A JavaScript
// file uses the JavaScript grammar, which includes JSX.
func typeScriptLanguage(file string, typed bool) *sitter.Language {
	switch strings.ToLower(path.Ext(file)) {
	case ".tsx":
		return sitter.NewLanguage(typescript.LanguageTSX())
	case ".ts", ".mts", ".cts":
		return sitter.NewLanguage(typescript.LanguageTypescript())
	case ".js", ".jsx", ".mjs", ".cjs":
		return sitter.NewLanguage(javascript.Language())
	}
	if typed {
		return sitter.NewLanguage(typescript.LanguageTypescript())
	}
	return sitter.NewLanguage(javascript.Language())
}

func parseTypeScript(document adapter.Document, typed bool, imported map[string]*adapter.Node) (*adapter.Node, error) {
	tree, err := code.ParseTree(document, typeScriptLanguage(document.Path, typed))
	if err != nil {
		return nil, err
	}
	defer tree.Close()

	imports := typeScriptImports(tree.RootNode(), document.Data)
	instances := typeScriptInstances(tree.RootNode(), document.Data, imports, typeScriptTypeImports(tree.RootNode(), document.Data))
	constants, constantResources := typeScriptConstants(tree.RootNode(), document.Data, imported)
	for name, value := range imported {
		if _, shadowed := constants[name]; !shadowed {
			constants[name] = value
		}
	}
	collections := map[string][]*adapter.Node{"agents": {}, "prompts": {}, "tools": {}, "constants": {}}
	collections["constants"] = constantResources
	fallbacks := code.NewFallbackIDs()
	code.Walk(tree.RootNode(), func(node *sitter.Node) {
		name, value := typeScriptDeclaration(node)
		if name == nil || value == nil {
			return
		}
		id := typeScriptQualifiedID(node, code.Text(name, document.Data), document.Data)
		// One prompt under the declaration, alone or piped into a chain, takes
		// the variable name. Several take the name with an ordinal, so that
		// each has a key of its own.
		calls := typeScriptPromptCalls(value, imports, document.Data)
		for index, call := range calls {
			fields := typeScriptPromptFields(call, imports, document.Data, constants)
			resource := adapter.Resource(id, fields)
			if len(calls) > 1 {
				// The ordinal is the position of the prompt in the chain.
				resource.Fields["id"] = adapter.PositionalID(fmt.Sprintf("%s#%d", id, index+1))
			}
			collections["prompts"] = append(collections["prompts"], resource)
		}
		if collection, resource, ok := typeScriptFrameworkCall(
			value, id, imports, instances, document.Data, constants,
		); ok {
			collections[collection] = append(collections[collection], resource)
		}
		if value.Kind() == "object" && (id == "tools" || strings.HasSuffix(id, ".tools")) {
			// `const tools = { search: tool({...}) }` of the Vercel AI SDK. A
			// tools object inside a function names its tools under the function.
			prefix := strings.TrimSuffix(id, "tools")
			collections["tools"] = append(collections["tools"], typeScriptVercelTools(value, prefix, imports, document.Data, constants)...)
		}
	})
	code.Walk(tree.RootNode(), func(node *sitter.Node) {
		// `generateText({ tools: { search: tool({...}) } })` holds its tools
		// inline. The enclosing scopes name them, as a tools constant does.
		if node.Kind() != "pair" {
			return
		}
		key, ok := typeScriptPropertyName(node.ChildByFieldName("key"), document.Data)
		value := typeScriptUnwrap(node.ChildByFieldName("value"))
		if !ok || key != "tools" || value == nil || value.Kind() != "object" {
			return
		}
		prefix := ""
		if scopes := typeScriptScopes(node, document.Data); len(scopes) != 0 {
			prefix = strings.Join(scopes, ".") + "."
		}
		collections["tools"] = append(collections["tools"], typeScriptVercelTools(value, prefix, imports, document.Data, constants)...)
	})
	code.Walk(tree.RootNode(), func(node *sitter.Node) {
		if node.Kind() != "call_expression" && node.Kind() != "new_expression" {
			return
		}
		// The declarator walk named the call that is the whole value of a
		// declaration, with or without await. A call nested in an array or
		// in another call, such as an agent in a team, is a resource of its own.
		if !typeScriptAssignedValue(node) {
			if collection, resource, ok := typeScriptFrameworkCall(
				node, "", imports, instances, document.Data, constants,
			); ok {
				fallbacks.Distinguish(resource, typeScriptQualifiedID(node, typeScriptCallable(node, document.Data), document.Data))
				collections[collection] = append(collections[collection], resource)
			}
		}
		// A prompt that a function returns, or builds in place, has no
		// variable name, so the enclosing function names it.
		if !typeScriptAssignedCall(node) && typeScriptPromptCall(node, imports, document.Data) {
			callable := typeScriptCallable(node, document.Data)
			fields := typeScriptPromptFields(node, imports, document.Data, constants)
			id := typeScriptQualifiedID(node, callable, document.Data)
			resource := adapter.Resource(id, fields)
			fallbacks.Distinguish(resource, id)
			collections["prompts"] = append(collections["prompts"], resource)
		}
	})
	// Two resources of one collection with one id, such as the tools of two
	// inline tools objects in one scope, get ordinals.
	for _, name := range []string{"agents", "prompts", "tools"} {
		code.DistinguishDuplicates(collections[name])
	}
	return adapter.Collections(collections), nil
}

// typeScriptDeclaration returns the name and the unwrapped value of a
// declaration that names its value: a variable declarator or a class field.
// An assignment to an existing name is not one, so the call walk names the
// calls under it.
func typeScriptDeclaration(node *sitter.Node) (*sitter.Node, *sitter.Node) {
	switch node.Kind() {
	case "variable_declarator", "public_field_definition":
		return node.ChildByFieldName("name"), typeScriptUnwrap(node.ChildByFieldName("value"))
	case "field_definition":
		return node.ChildByFieldName("property"), typeScriptUnwrap(node.ChildByFieldName("value"))
	}
	return nil, nil
}

// typeScriptVercelTools reads the Vercel AI SDK tools of a tools object: each
// entry whose value is a `tool({...})` call, named by its key under prefix.
func typeScriptVercelTools(
	object *sitter.Node,
	prefix string,
	imports typeScriptAliases,
	source []byte,
	constants map[string]*adapter.Node,
) []*adapter.Node {
	var tools []*adapter.Node
	for _, pair := range code.NamedChildren(object) {
		if pair.Kind() != "pair" {
			continue
		}
		key, ok := typeScriptPropertyName(pair.ChildByFieldName("key"), source)
		call := typeScriptUnwrap(pair.ChildByFieldName("value"))
		if !ok || call == nil || call.Kind() != "call_expression" ||
			imports.at(call, source)[typeScriptCallable(call, source)] != "vercel-tool" {
			continue
		}
		arguments := code.NamedChildren(call.ChildByFieldName("arguments"))
		if len(arguments) != 1 {
			continue
		}
		fields := typeScriptObject(arguments[0], source, constants).Fields
		tools = append(tools, adapter.Resource(prefix+key, fields))
	}
	return tools
}

// typeScriptAssignedValue reports whether a call is the whole value of a
// declaration, which the declaration walk names after its variable.
func typeScriptAssignedValue(call *sitter.Node) bool {
	for parent := call.Parent(); parent != nil; parent = parent.Parent() {
		if name, value := typeScriptDeclaration(parent); name != nil {
			return code.SameNode(value, call)
		}
		switch parent.Kind() {
		case "as_expression", "satisfies_expression", "non_null_expression", "parenthesized_expression",
			"await_expression", "type_assertion":
			continue
		default:
			return false
		}
	}
	return false
}

// typeScriptAssignedCall reports whether a call sits anywhere under a
// declaration. The declaration walk names every prompt under the value of a
// declaration, through the body of an assigned function as well, so the
// call walk leaves them out.
func typeScriptAssignedCall(call *sitter.Node) bool {
	for parent := call.Parent(); parent != nil; parent = parent.Parent() {
		if name, value := typeScriptDeclaration(parent); name != nil {
			// A call in a function or a class nested in the value has a
			// scope of its own, so the call walk names it.
			return value != nil && !typeScriptInnerBinding(call, value)
		}
	}
	return false
}

// typeScriptConstants returns the literal constants of a file by qualified
// id, and the constant resources that constants[id="NAME"] selects. imported
// holds the values of the static imports. A literal can use an imported
// value or a constant of the same file that is declared before it.
func typeScriptConstants(root *sitter.Node, source []byte, imported map[string]*adapter.Node) (map[string]*adapter.Node, []*adapter.Node) {
	type declaration struct {
		id    string
		value *sitter.Node
	}
	var declarations []declaration
	counts := map[string]int{}
	code.Walk(root, func(node *sitter.Node) {
		// `exports.TEXT = "Hello"`, an entry of `module.exports` with a
		// literal, and `export = "Hello"` declare the constant that a
		// require of the module binds.
		for _, export := range typeScriptModuleExports(node, source) {
			if export.value != nil {
				id := typeScriptQualifiedID(node, export.local, source)
				declarations = append(declarations, declaration{id: id, value: export.value})
				counts[id]++
			}
		}
		// `export default value` and `export { TEXT as default }` are the
		// constant that a default import binds.
		if node.Kind() == "export_statement" {
			if valueNode := typeScriptDefaultExport(node, source); valueNode != nil {
				declarations = append(declarations, declaration{id: "default", value: valueNode})
				counts["default"]++
			}
			return
		}
		// A parameter, a catch parameter, or a local function or class hides
		// a constant of the same name in its function, so it has an entry
		// without a value.
		if pattern := typeScriptLocalBinding(node); pattern != nil {
			for _, bound := range typeScriptBoundNames(pattern, source) {
				id := typeScriptBindingID(node, bound, source)
				if counts[id] == 0 {
					declarations = append(declarations, declaration{id: id, value: nil})
					counts[id]++
				}
			}
			return
		}
		// A variable declarator or a class field with a literal value is a
		// constant. A require binds an import, which the imports hold.
		if len(typeScriptRequireBindings(node, source)) != 0 {
			return
		}
		// A destructuring, an uninitialized let, and a for-of target bind
		// their names without a literal, so they hide an outer constant of
		// the same name as a parameter does.
		hide := func(pattern *sitter.Node, id func(string) string) {
			for _, bound := range typeScriptBoundNames(pattern, source) {
				if bound := id(bound); counts[bound] == 0 {
					declarations = append(declarations, declaration{id: bound, value: nil})
					counts[bound]++
				}
			}
		}
		if node.Kind() == "for_in_statement" {
			if left := node.ChildByFieldName("left"); left != nil {
				hide(left, func(bound string) string {
					return typeScriptBlockID(typeScriptQualifiedID(node, bound, source), node)
				})
			}
			return
		}
		name, valueNode := typeScriptDeclaration(node)
		if name == nil {
			return
		}
		if node.Kind() == "variable_declarator" && (valueNode == nil || name.Kind() != "identifier") {
			hide(name, func(bound string) string { return typeScriptBindingID(node, bound, source) })
			return
		}
		if valueNode == nil || name.Kind() != "identifier" && name.Kind() != "property_identifier" {
			return
		}
		id := typeScriptBindingID(node, code.Text(name, source), source)
		declarations = append(declarations, declaration{id: id, value: valueNode})
		counts[id]++
	})
	declared := func(id string) bool { return counts[id] != 0 }
	// A namespace import holds the exports of a module, so a write through
	// it names one export.
	namespace := func(name string) bool {
		value := imported[name]
		return value != nil && value.Fields != nil && value.Value == nil && counts[name] == 0
	}
	mutated := map[string]bool{}
	code.Walk(root, func(node *sitter.Node) {
		switch node.Kind() {
		case "assignment_expression", "augmented_assignment_expression", "update_expression":
			left := node.ChildByFieldName("left")
			if node.Kind() == "update_expression" {
				// `count++` and `--this.limit` change their target.
				left = node.ChildByFieldName("argument")
			}
			if field := typeScriptThisField(left, source); field != "" {
				// `this.items = ...` changes the class field.
				mutated[strings.Join(append(typeScriptMemberScopes(node, source), field), ".")] = true
			} else if rootName := typeScriptMutationRoot(left, source); rootName != "" {
				_, field := typeScriptChainParts(code.Text(typeScriptUnwrap(left), source))
				mutated[typeScriptMutatedName(node, rootName, field, source, declared, namespace)] = true
			}
		case "call_expression":
			owner, method, ok := code.SplitMethod(typeScriptCallable(node, source))
			if !ok || method != "push" && method != "splice" && method != "pop" &&
				method != "shift" && method != "unshift" && method != "sort" && method != "reverse" {
				return
			}
			// The call mutates the root of the receiver chain, LIST in
			// `LIST[0].push(...)` and CONFIG in `CONFIG["items"].push(...)`.
			rootName, field := typeScriptChainParts(owner)
			if rootName == "this" && field != "" {
				// `this.items.push(...)` changes the class field.
				mutated[strings.Join(append(typeScriptMemberScopes(node, source), field), ".")] = true
			} else if rootName != "" {
				mutated[typeScriptMutatedName(node, rootName, field, source, declared, namespace)] = true
			}
		}
	})

	// Each literal is read in source order with the imports and every earlier
	// constant that is declared once and not mutated in scope, so a chain of
	// aliases resolves. A declaration that is not a literal keeps a nil
	// entry, so that it still shadows an import or a module-level constant
	// of the same name.
	environment := maps.Clone(imported)
	if environment == nil {
		environment = map[string]*adapter.Node{}
	}
	values := map[string]*adapter.Node{}
	// A named import and a field of a namespace import of one export are one
	// value, so a mutation through either name freezes both.
	typeScriptSpreadImportMutations(mutated, imported)
	// An import that this file mutates is read-only here, as a local
	// constant is, and the read-only copy shadows the import. A namespace
	// import freezes only the exports that the file mutates.
	for name, value := range imported {
		if value == nil {
			continue
		}
		if mutated[name] {
			frozen := value.Clone()
			frozen.MarkReadOnly("mutated constants are read-only")
			environment[name], values[name] = frozen, frozen
			continue
		}
		var frozen *adapter.Node
		for field := range value.Fields {
			if !mutated[name+"."+field] {
				continue
			}
			if frozen == nil {
				frozen = value.Clone()
			}
			frozen.Fields[field].MarkReadOnly("mutated constants are read-only")
		}
		if frozen != nil {
			environment[name], values[name] = frozen, frozen
		}
	}
	var resources []*adapter.Node
	fallbacks := code.NewFallbackIDs()
	for _, item := range declarations {
		if item.value == nil {
			// A parameter has no value and hides an outer name.
			values[item.id], environment[item.id] = nil, nil
			continue
		}
		// A module-level declaration of an imported name shadows the import
		// from here on. The import stays visible to the uses before it.
		if !strings.Contains(item.id, ".") && !typeScriptBlockScoped(item.id) {
			code.RecordShadowedImport(values, item.id, imported[item.id], int(item.value.StartByte()))
		}
		value := typeScriptValue(item.value, source, environment)
		if !code.IsLiteral(value) {
			// The nil entries shadow an import of the same name here and in
			// every later literal.
			values[item.id], environment[item.id] = nil, nil
			continue
		}
		if counts[item.id] == 1 && !mutated[item.id] {
			environment[item.id] = value
		}
		if item.value.Kind() == "identifier" && counts[item.id] == 1 {
			// `const ALSO = ITEMS` binds the object of ITEMS, so an import
			// of either name binds one value.
			origin := typeScriptScopedName(item.value, code.Text(item.value, source), source, declared)
			if origin != item.id && declared(origin) {
				code.RecordAlias(values, item.id, origin)
			}
		}
		values[item.id] = value
		code.RecordDeclaration(values, item.id, int(item.value.EndByte()))
		resource := code.Constant(item.id, value)
		// A binding of a nested block resolves the uses in its block, and
		// is not a constant of the file.
		if !typeScriptBlockScoped(item.id) {
			resources = append(resources, resource)
		}
		// A name that is assigned more than once has no one value, so each
		// of its assignments is a read-only constant with an id of its own.
		if counts[item.id] != 1 {
			values[item.id] = nil
			resource.MarkReadOnly("reassigned constants are read-only")
		}
		if mutated[item.id] {
			values[item.id] = nil
			resource.MarkReadOnly("mutated constants are read-only")
		}
		if counts[item.id] != 1 || mutated[item.id] {
			// An import of the name binds its first literal as read-only.
			frozen := value.Clone()
			frozen.MarkReadOnly(resource.Reason)
			code.RecordFrozen(values, item.id, frozen)
		}
		// The positional id comes last, so that no read-only mark replaces it.
		if counts[item.id] != 1 {
			fallbacks.Distinguish(resource, item.id)
		}
	}
	return values, resources
}

// typeScriptThisField returns the class field that a property chain on this
// starts with, such as items in `this.items[0]`, or an empty string.
func typeScriptThisField(node *sitter.Node, source []byte) string {
	field := ""
	for node = typeScriptUnwrap(node); node != nil; node = typeScriptUnwrap(node) {
		switch node.Kind() {
		case "member_expression":
			field = code.Text(node.ChildByFieldName("property"), source)
			node = node.ChildByFieldName("object")
		case "subscript_expression":
			field = ""
			node = node.ChildByFieldName("object")
		case "this":
			return field
		default:
			return ""
		}
	}
	return ""
}

// typeScriptExportedNames maps each exported name of a module to the local
// name that it exports: `export const X` exports X as X, and
// `export { A, B as C }` exports A as A and B as C.
func typeScriptExportedNames(root *sitter.Node, source []byte) map[string]string {
	exported := map[string]string{}
	code.Walk(root, func(node *sitter.Node) {
		// `module.exports = { TEXT }` and `exports.TEXT = ...` are the
		// CommonJS exports.
		for _, export := range typeScriptModuleExports(node, source) {
			exported[export.name] = export.local
		}
		// `export type { TEXT }` exports a type that TypeScript erases, so
		// the value is not importable. `export { TEXT } from "./other"`
		// exports a value of another module, not a constant of this file.
		if node.Kind() != "export_statement" || typeScriptHasTypeKeyword(node, source) || typeScriptReExport(node) {
			return
		}
		if declaration := node.ChildByFieldName("declaration"); declaration != nil {
			for _, declarator := range code.NamedChildren(declaration) {
				if declarator.Kind() != "variable_declarator" {
					continue
				}
				if name := declarator.ChildByFieldName("name"); name != nil {
					for _, bound := range typeScriptBoundNames(name, source) {
						exported[bound] = bound
					}
				}
			}
		}
		for _, clause := range code.NamedChildren(node) {
			if clause.Kind() != "export_clause" {
				continue
			}
			for _, specifier := range code.NamedChildren(clause) {
				name := specifier.ChildByFieldName("name")
				if specifier.Kind() != "export_specifier" || name == nil || typeScriptHasTypeKeyword(specifier, source) {
					continue
				}
				local := code.Text(name, source)
				as := local
				if alias := specifier.ChildByFieldName("alias"); alias != nil {
					as = code.Text(alias, source)
				}
				exported[as] = local
			}
		}
	})
	return exported
}

// typeScriptModuleExport is one CommonJS export: the exported name, the
// local name that holds its value, and the literal value when the export
// has no local name, as in `exports.TEXT = "Hello"`.
type typeScriptModuleExport struct {
	name, local string
	value       *sitter.Node
}

// typeScriptModuleExports returns the CommonJS exports of a node: each entry
// of `module.exports = { TEXT, OTHER: LOCAL, INLINE: "text" }`, the one
// export of `exports.TEXT = value` and `module.exports.TEXT = value`, or the
// whole module of `module.exports = value`. The TypeScript form `export =
// value` exports as `module.exports = value` does. Another node exports
// nothing.
func typeScriptModuleExports(node *sitter.Node, source []byte) []typeScriptModuleExport {
	var target string
	var right *sitter.Node
	switch node.Kind() {
	case "assignment_expression":
		left := typeScriptUnwrap(node.ChildByFieldName("left"))
		if left == nil {
			return nil
		}
		target, right = code.Text(left, source), typeScriptUnwrap(node.ChildByFieldName("right"))
	case "export_statement":
		target, right = "module.exports", typeScriptExportEquals(node)
	}
	if right == nil {
		return nil
	}
	export := func(name string, value *sitter.Node) typeScriptModuleExport {
		if value != nil && value.Kind() == "identifier" {
			return typeScriptModuleExport{name: name, local: code.Text(value, source)}
		}
		return typeScriptModuleExport{name: name, local: name, value: value}
	}
	switch {
	case target == "module.exports" && right.Kind() == "object":
		// The object is the module, so a default import binds the exports
		// as one object. An entry named default replaces this marker.
		exports := []typeScriptModuleExport{{name: "default", local: "*"}}
		for _, entry := range code.NamedChildren(right) {
			switch entry.Kind() {
			case "shorthand_property_identifier":
				name := code.Text(entry, source)
				exports = append(exports, typeScriptModuleExport{name: name, local: name})
			case "pair":
				if key, ok := typeScriptPropertyName(entry.ChildByFieldName("key"), source); ok {
					exports = append(exports, export(key, typeScriptUnwrap(entry.ChildByFieldName("value"))))
				}
			}
		}
		return exports
	case target == "module.exports":
		// `module.exports = TEXT` or `module.exports = "Help"` makes one
		// value the module, which a require binds whole and a default
		// import binds as the default. A literal declares the default.
		whole := export("default", right)
		whole.name = "*"
		return []typeScriptModuleExport{whole}
	case strings.HasPrefix(target, "exports.") || strings.HasPrefix(target, "module.exports."):
		name := target[strings.LastIndex(target, ".")+1:]
		if strings.Contains(name, "[") || name == "" {
			return nil
		}
		return []typeScriptModuleExport{export(name, right)}
	}
	return nil
}

// typeScriptExportEquals returns the value of `export = value`, the
// TypeScript form of `module.exports = value`, or nil for another statement.
func typeScriptExportEquals(node *sitter.Node) *sitter.Node {
	if node.Kind() != "export_statement" || node.ChildCount() < 3 || node.Child(1).Kind() != "=" {
		return nil
	}
	return typeScriptUnwrap(node.NamedChild(0))
}

// typeScriptReExport reports whether an export statement exports the values
// of another module, as `export { TEXT } from "./other"` does.
func typeScriptReExport(node *sitter.Node) bool {
	return node.ChildByFieldName("source") != nil
}

// typeScriptDefaultExport returns the value that an export statement exports
// as the default, or nil: the expression of `export default value`, or the
// name in `export { TEXT as default }`.
func typeScriptDefaultExport(node *sitter.Node, source []byte) *sitter.Node {
	if typeScriptReExport(node) {
		// `export { default } from "./other"` is a value of another module.
		return nil
	}
	for index := uint(0); index < node.ChildCount(); index++ {
		child := node.Child(index)
		if !child.IsNamed() && code.Text(child, source) == "default" {
			return node.ChildByFieldName("value")
		}
	}
	for _, clause := range code.NamedChildren(node) {
		if clause.Kind() != "export_clause" {
			continue
		}
		for _, specifier := range code.NamedChildren(clause) {
			alias := specifier.ChildByFieldName("alias")
			if specifier.Kind() == "export_specifier" && alias != nil && code.Text(alias, source) == "default" {
				return specifier.ChildByFieldName("name")
			}
		}
	}
	return nil
}

// typeScriptMutationRoot returns the name at the root of an assignment
// target, such as CONFIG in `CONFIG!.prompt` or `(CONFIG as T).list[0]`.
// A wrapper on a segment changes no runtime value, so it is left out.
func typeScriptMutationRoot(node *sitter.Node, source []byte) string {
	for node = typeScriptUnwrap(node); node != nil; node = typeScriptUnwrap(node) {
		switch node.Kind() {
		case "member_expression", "subscript_expression":
			node = node.ChildByFieldName("object")
		case "identifier":
			return code.Text(node, source)
		default:
			return ""
		}
	}
	return ""
}

// typeScriptChainParts splits a receiver chain into its root and the first
// segment after it: LIST and "" for `LIST[0]`, CONFIG and items for
// `CONFIG.items[0]`, and Bot and items for `Bot.items.push`.
func typeScriptChainParts(chain string) (root, field string) {
	index := strings.IndexAny(chain, ".[")
	if index < 0 {
		return chain, ""
	}
	root, rest := chain[:index], chain[index:]
	if !strings.HasPrefix(rest, ".") {
		return root, ""
	}
	rest = rest[1:]
	if end := strings.IndexAny(rest, ".[("); end >= 0 {
		rest = rest[:end]
	}
	return root, rest
}

// typeScriptMutatedName returns the id that a write through a receiver chain
// mutates: the static field of a class when the chain starts with the class
// name, as in `Bot.items.push(...)`, one export of a namespace import, as in
// `values.LIST.push(...)`, else the constant that the root names.
func typeScriptMutatedName(
	node *sitter.Node,
	rootName, field string,
	source []byte,
	declared, namespace func(string) bool,
) string {
	if field != "" {
		if candidate := typeScriptScopedName(node, rootName+".static."+field, source, declared); declared(candidate) {
			return candidate
		}
		if namespace(rootName) && typeScriptScopedName(node, rootName, source, declared) == rootName {
			return rootName + "." + field
		}
	}
	return typeScriptScopedName(node, rootName, source, declared)
}

// typeScriptConstant returns a copy of the constant that a use of name at a
// byte offset refers to, or nil when none is in scope. The copy keeps a later
// change to one use, such as a read-only mark, away from the other uses.
func typeScriptConstant(node *sitter.Node, name string, use int, source []byte, constants map[string]*adapter.Node) *adapter.Node {
	if constants == nil {
		return nil
	}
	declared := func(id string) bool { _, ok := constants[id]; return ok }
	scoped := typeScriptScopedName(node, name, source, declared)
	value := constants[scoped]
	if code.DeclaredAt(constants, scoped, value, use) || value != nil && typeScriptDeferredUse(node, scoped, source) {
		return value.Clone()
	}
	if scoped == name {
		return code.ShadowedImport(constants, name, use)
	}
	return nil
}

// typeScriptDeferredUse reports whether a use runs after the scope of the
// constant finished: a function body between the scope of the constant and
// the use runs later, so it sees a constant that the file declares after it.
func typeScriptDeferredUse(node *sitter.Node, scoped string, source []byte) bool {
	// An instance field initializer runs in the constructor, after the
	// module loads. A static field runs with the class declaration.
	for parent := node.Parent(); parent != nil && parent.Kind() != "class_body"; parent = parent.Parent() {
		if (parent.Kind() == "public_field_definition" || parent.Kind() == "field_definition") &&
			!typeScriptStaticField(parent, source) {
			return true
		}
	}
	chain := typeScriptScopeChain(node, source)
	for _, scope := range chain[min(strings.Count(scoped, "."), len(chain)):] {
		if !scope.class {
			return true
		}
	}
	return false
}

// typeScriptSpreadImportMutations marks every import name that shares a value
// with a mutated one: `LIST` of `import { LIST }` and `values.LIST` of
// `import * as values` bind one export, so a write through one of them
// mutates the other.
func typeScriptSpreadImportMutations(mutated map[string]bool, imported map[string]*adapter.Node) {
	shared := map[*adapter.Node][]string{}
	for name, value := range imported {
		if value == nil || strings.Contains(name, " ") {
			continue
		}
		shared[value] = append(shared[value], name)
		for field, entry := range value.Fields {
			if entry != nil {
				shared[entry] = append(shared[entry], name+"."+field)
			}
		}
	}
	for _, names := range shared {
		if !slices.ContainsFunc(names, func(name string) bool { return mutated[name] }) {
			continue
		}
		for _, name := range names {
			mutated[name] = true
		}
	}
}

// typeScriptStaticScope reports whether a scope node is a static member of
// its class: a static method, or a function or class that initializes a
// static field.
func typeScriptStaticScope(node *sitter.Node, source []byte) bool {
	switch node.Kind() {
	case "method_definition":
		return typeScriptStaticField(node, source)
	case "arrow_function", "function_expression", "function", "generator_function", "class":
		parent := node.Parent()
		if parent == nil || parent.Kind() != "public_field_definition" && parent.Kind() != "field_definition" {
			return false
		}
		return typeScriptStaticField(parent, source)
	}
	return false
}

// typeScriptStaticField reports whether a class field has the static
// modifier.
func typeScriptStaticField(field *sitter.Node, source []byte) bool {
	for index := uint(0); index < field.ChildCount(); index++ {
		child := field.Child(index)
		if !child.IsNamed() && code.Text(child, source) == "static" {
			return true
		}
	}
	return false
}

// typeScriptScopedName returns the id that a use of name refers to: the
// constant of the nearest enclosing function or class that declares it, else
// the module-level name. declared reports whether an id has a constant. A
// bare name inside a method does not see the fields of the class, because
// JavaScript resolves it through the enclosing functions and then the
// module. A property chain, such as this.client, belongs to the class.
func typeScriptScopedName(node *sitter.Node, name string, source []byte, declared func(string) bool) string {
	scopes := typeScriptScopeChain(node, source)
	bare := !strings.Contains(name, ".")
	// A let or const of a nested block binds its name in that block, so the
	// nearest block around the use that declares the name wins.
	if bare {
		for parent := node.Parent(); parent != nil; parent = parent.Parent() {
			if !typeScriptNestedBlock(parent) {
				continue
			}
			if candidate := typeScriptBlockID(typeScriptQualifiedID(parent, name, source), parent); declared(candidate) {
				return candidate
			}
		}
	}
	for end := len(scopes); end > 0; end-- {
		if bare && end < len(scopes) && scopes[end-1].class {
			continue
		}
		names := make([]string, 0, end+1)
		for _, scope := range scopes[:end] {
			names = append(names, scope.name)
		}
		if candidate := strings.Join(append(names, name), "."); declared(candidate) {
			return candidate
		}
	}
	return name
}

// typeScriptScope is one class or function that encloses a node.
type typeScriptScope struct {
	name  string
	class bool
}

// typeScriptScopeChain returns the classes and functions that enclose a
// node, outermost first.
func typeScriptScopeChain(node *sitter.Node, source []byte) []typeScriptScope {
	var scopes []typeScriptScope
	for parent := node.Parent(); parent != nil; parent = parent.Parent() {
		name := typeScriptScopeName(parent, source)
		if name == "" {
			continue
		}
		class := false
		switch parent.Kind() {
		case "class_declaration", "abstract_class_declaration", "class":
			class = true
		}
		entry := []typeScriptScope{{name: name, class: class}}
		if typeScriptStaticScope(parent, source) {
			// A static method or a static field initializer belongs to the
			// class, not to its instances, so its scope carries the static
			// marker, as a static field does, and an instance member of the
			// same name is another scope. The marker counts as a class
			// scope, so a bare name does not reach the static fields
			// through it.
			entry = append([]typeScriptScope{{name: "static", class: true}}, entry...)
		}
		scopes = append(entry, scopes...)
	}
	return scopes
}

// typeScriptScopes returns the names of the classes and functions that
// enclose a node, outermost first.
func typeScriptScopes(node *sitter.Node, source []byte) []string {
	chain := typeScriptScopeChain(node, source)
	scopes := make([]string, len(chain))
	for index, scope := range chain {
		scopes[index] = scope.name
	}
	return scopes
}

// typeScriptUnwrap returns the expression inside type wrappers and
// parentheses, such as `{...} as const`, `value satisfies T`, `value!`,
// `<T>value`, and `(value)`. The wrappers change no runtime value.
func typeScriptUnwrap(node *sitter.Node) *sitter.Node {
	for node != nil {
		switch node.Kind() {
		case "as_expression", "satisfies_expression", "non_null_expression", "parenthesized_expression", "await_expression":
			node = node.NamedChild(0)
		case "type_assertion":
			node = node.NamedChild(node.NamedChildCount() - 1)
		default:
			return node
		}
	}
	return nil
}

// typeScriptCallable returns the name of the function or constructor of a
// call, such as client.chat.completions.create. A type wrapper or
// parentheses on a segment, as in `client!.chat` or `(server as McpServer)`,
// change no runtime value, so the name leaves them out.
func typeScriptCallable(call *sitter.Node, source []byte) string {
	function := call.ChildByFieldName("function")
	if function == nil {
		function = call.ChildByFieldName("constructor")
	}
	return typeScriptCallableName(function, source)
}

// typeScriptCallableName returns the dotted name of a callee expression
// without the wrappers on its segments.
func typeScriptCallableName(node *sitter.Node, source []byte) string {
	node = typeScriptUnwrap(node)
	if node == nil {
		return ""
	}
	if node.Kind() == "member_expression" {
		property := code.Text(node.ChildByFieldName("property"), source)
		if object := typeScriptCallableName(node.ChildByFieldName("object"), source); object != "" {
			return object + "." + property
		}
		return property
	}
	return code.Text(node, source)
}

// typeScriptQualifiedID prefixes a name with every enclosing class and
// function, outermost first, so that two methods with one name in two classes
// keep their locals apart.
func typeScriptQualifiedID(node *sitter.Node, name string, source []byte) string {
	return strings.Join(append(typeScriptScopes(node, source), name), ".")
}

// typeScriptScopeName returns the name of a function or class scope, or an
// empty string for a node that opens no named scope. A function expression
// or an arrow function takes the name of the variable, property, or class
// field that holds it. A callback that is only an argument has no name.
func typeScriptScopeName(node *sitter.Node, source []byte) string {
	name := ""
	switch node.Kind() {
	case "function_declaration", "generator_function_declaration", "method_definition",
		"class_declaration", "abstract_class_declaration":
		name = code.Text(node.ChildByFieldName("name"), source)
	case "arrow_function", "function_expression", "function", "generator_function", "class":
		parent := node.Parent()
		if parent == nil {
			break
		}
		switch parent.Kind() {
		case "variable_declarator", "public_field_definition":
			name = code.Text(parent.ChildByFieldName("name"), source)
		case "field_definition":
			name = code.Text(parent.ChildByFieldName("property"), source)
		case "pair":
			name, _ = typeScriptPropertyName(parent.ChildByFieldName("key"), source)
		case "assignment_expression":
			name = code.Text(parent.ChildByFieldName("left"), source)
		}
	}
	if name == "" && typeScriptClassKind(node.Kind()) {
		// An anonymous class, as `export default class {}` or the class of
		// a mixin factory, is a scope of its own, so its members and its
		// methods share one name, taken from its position.
		return fmt.Sprintf("class@%d", node.StartByte())
	}
	return name
}

// typeScriptClassKind reports whether a node kind is a class.
func typeScriptClassKind(kind string) bool {
	return kind == "class_declaration" || kind == "abstract_class_declaration" || kind == "class"
}

// typeScriptPromptCalls returns the prompt calls that a declaration value
// holds. A call that a declaration or an assignment inside the value holds,
// such as a constant in an arrow function body, belongs to that inner
// binding, which names it on its own.
func typeScriptPromptCalls(node *sitter.Node, imports typeScriptAliases, source []byte) []*sitter.Node {
	var calls []*sitter.Node
	code.Walk(node, func(candidate *sitter.Node) {
		if typeScriptPromptCall(candidate, imports, source) && !typeScriptInnerBinding(candidate, node) {
			calls = append(calls, candidate)
		}
	})
	return calls
}

// typeScriptInnerBinding reports whether a declaration between a call and
// the value that holds it names the call itself. The value itself, or a call
// that only wrappers separate from it, has no inner declaration.
func typeScriptInnerBinding(call, value *sitter.Node) bool {
	if code.SameNode(call, value) {
		return false
	}
	for parent := call.Parent(); parent != nil && !code.SameNode(parent, value); parent = parent.Parent() {
		if name, _ := typeScriptDeclaration(parent); name != nil {
			return true
		}
		// A function or a class nested in the value is a scope of its own,
		// so a prompt in it belongs to that scope and not to the variable.
		if typeScriptFunctionKind(parent.Kind()) || parent.Kind() == "class" || parent.Kind() == "class_declaration" {
			return true
		}
	}
	return false
}

// typeScriptPromptCall reports whether a node builds a prompt template of an
// allowlisted framework.
func typeScriptPromptCall(node *sitter.Node, imports typeScriptAliases, source []byte) bool {
	if node.Kind() != "call_expression" && node.Kind() != "new_expression" {
		return false
	}
	callable := typeScriptCallable(node, source)
	owner, method, qualified := code.SplitMethod(callable)
	kinds := imports.at(node, source)
	return kinds[callable] == "prompt" ||
		(qualified && kinds[owner] == "prompt" && (method == "fromMessages" || method == "fromTemplate"))
}

func typeScriptPromptFields(
	call *sitter.Node,
	imports typeScriptAliases,
	source []byte,
	constants map[string]*adapter.Node,
) map[string]*adapter.Node {
	callable := typeScriptCallable(call, source)
	owner, method, qualified := code.SplitMethod(callable)
	arguments := code.NamedChildren(call.ChildByFieldName("arguments"))
	if qualified && imports.at(call, source)[owner] == "prompt" && len(arguments) != 0 {
		if method == "fromMessages" {
			return code.TranslateFields(
				map[string]*adapter.Node{"messages": typeScriptMessages(arguments[0], source, constants)},
				code.BracePlaceholders, "messages",
			)
		}
		if method == "fromTemplate" {
			return code.TranslateFields(
				map[string]*adapter.Node{"template": typeScriptValue(arguments[0], source, constants)},
				code.BracePlaceholders, "template",
			)
		}
	}
	if len(arguments) == 0 {
		return map[string]*adapter.Node{}
	}
	first := typeScriptUnwrap(arguments[0])
	if first.Kind() == "array" || code.MessageList(typeScriptValue(first, source, constants)) != nil {
		return code.TranslateFields(
			map[string]*adapter.Node{"messages": typeScriptMessages(first, source, constants)},
			code.BracePlaceholders, "messages",
		)
	}
	return code.TranslateFields(
		typeScriptObject(arguments[0], source, constants).Fields,
		code.BracePlaceholders, "template", "messages",
	)
}

// typeScriptImports maps each imported name of an allowlisted framework to
// the kind of symbol that it is, such as "prompt" or "mcp-server".
// typeScriptAliases are the framework symbols that the imports of a file
// bind, such as Agent to "openai-agent", with the scope of each binding and
// the local names that hide a binding. An import or a module-level require
// is visible in the whole file, a require inside a function in that
// function, and a local declaration of the same name hides them both.
type typeScriptAliases struct {
	bindings []typeScriptAliasBinding
	shadows  []typeScriptShadow
}

// typeScriptAliasBinding is one framework symbol that an import binds.
type typeScriptAliasBinding struct {
	alias, kind string
	// scopes are the functions and classes that enclose the binding.
	scopes []string
	end    int
	// block is the byte range of the block that declares the binding: the
	// program for an import, or the block that holds a require.
	block [2]int
}

// typeScriptShadow is a local declaration whose name hides an alias in the
// block that holds it: the function body, or a nested block such as the
// body of a loop or a condition.
type typeScriptShadow struct {
	name       string
	start, end int
}

// hiddenBy reports whether a shadow hides the binding at a use inside the
// shadow. The nearer declaration wins: a binding of the block of the shadow,
// such as the require that the shadow records, or of a block inside it,
// stays. A binding of an outer block, or of a block that does not hold the
// use, is hidden.
func (binding typeScriptAliasBinding) hiddenBy(shadow typeScriptShadow, use int) bool {
	if use < binding.block[0] || use >= binding.block[1] {
		return true
	}
	return shadow.start > binding.block[0] || shadow.end < binding.block[1]
}

// typeScriptImports returns the framework symbols that the imports and the
// requires of a file bind.
func typeScriptImports(root *sitter.Node, source []byte) typeScriptAliases {
	return typeScriptImportsFrom(root, source, false)
}

// typeScriptTypeImports returns the framework symbols that the imports bind
// as values or as types. A type annotation names a client kind through a
// type-only import too, as `import type OpenAI from "openai"` does, while
// the value imports leave such a binding out.
func typeScriptTypeImports(root *sitter.Node, source []byte) typeScriptAliases {
	return typeScriptImportsFrom(root, source, true)
}

func typeScriptImportsFrom(root *sitter.Node, source []byte, includeTypes bool) typeScriptAliases {
	var aliases typeScriptAliases
	add := func(node *sitter.Node, alias, kind string) {
		// A var require binds its names in the whole function.
		block := typeScriptBindingBlock(node)
		aliases.bindings = append(aliases.bindings, typeScriptAliasBinding{
			alias: alias, kind: kind, scopes: typeScriptScopes(node, source), end: int(node.EndByte()),
			block: [2]int{int(block.StartByte()), int(block.EndByte())},
		})
	}
	for _, site := range typeScriptImportSitesFrom(root, source, includeTypes) {
		allowed := typeScriptAllowedImports(site.binding.module)
		if allowed == nil {
			continue
		}
		if site.binding.name == "*" {
			// `import * as agents` makes every symbol reachable as agents.Agent.
			for _, name := range slices.Sorted(maps.Keys(allowed)) {
				if name != "default" {
					add(site.node, site.binding.alias+"."+name, allowed[name])
				}
			}
			continue
		}
		if kind := allowed[site.binding.name]; kind != "" {
			add(site.node, site.binding.alias, kind)
		}
	}
	code.Walk(root, func(node *sitter.Node) {
		// A class field is a property of the instance, not a name of the
		// class body, so it hides no import in the methods.
		if node.Kind() == "public_field_definition" || node.Kind() == "field_definition" {
			return
		}
		name, _ := typeScriptDeclaration(node)
		if name == nil {
			// A parameter binds its name in the whole function, and a local
			// function, class, or catch parameter binds it in its block.
			name = typeScriptLocalBinding(node)
		}
		if name == nil {
			return
		}
		block := typeScriptBindingBlock(node)
		if block.Kind() == "program" {
			// A module-level declaration is a constant, not a shadow. A
			// declaration in a block at module level is a shadow of the
			// block.
			return
		}
		for _, bound := range typeScriptBoundNames(name, source) {
			aliases.shadows = append(aliases.shadows, typeScriptShadow{
				name: bound, start: int(block.StartByte()), end: int(block.EndByte()),
			})
		}
	})
	return aliases
}

// typeScriptBindingBlock returns the block in which a node binds its names:
// the catch clause for its parameter, the function or the program for a
// var, else the block around the node.
func typeScriptBindingBlock(node *sitter.Node) *sitter.Node {
	switch {
	case node.Kind() == "catch_clause":
		return node
	case typeScriptVarDeclarator(node):
		// JavaScript hoists a var to its function, so the var binds its
		// name in the whole function, even from a branch that never runs.
		for parent := node.Parent(); parent != nil; parent = parent.Parent() {
			if typeScriptFunctionKind(parent.Kind()) || parent.Kind() == "program" {
				return parent
			}
		}
	}
	return typeScriptBlock(node)
}

// typeScriptVarDeclarator reports whether a node is the declarator of a var.
func typeScriptVarDeclarator(node *sitter.Node) bool {
	parent := node.Parent()
	return node.Kind() == "variable_declarator" && parent != nil && parent.Kind() == "variable_declaration"
}

// typeScriptFunctionKind reports whether a node kind is a function, whose
// body is the scope of its parameters and its var declarations.
func typeScriptFunctionKind(kind string) bool {
	switch kind {
	case "arrow_function", "function_expression", "function_declaration", "method_definition",
		"function", "generator_function", "generator_function_declaration":
		return true
	}
	return false
}

// typeScriptNestedBlock reports whether a block is nested in its function or
// in the module: a loop, a switch body, a catch clause, or a statement block
// that is not the body of a function. A let or const binds its name in such
// a block only.
func typeScriptNestedBlock(block *sitter.Node) bool {
	switch block.Kind() {
	case "for_statement", "for_in_statement", "switch_body", "catch_clause":
		return true
	case "statement_block":
		parent := block.Parent()
		return parent != nil && !typeScriptFunctionKind(parent.Kind())
	}
	return false
}

// typeScriptBindingID returns the id of a declaration: its qualified name,
// with the start of its block when a let, a const, a catch parameter, or a
// local function or class binds the name in a nested block only. A var binds
// its name in the whole function.
func typeScriptBindingID(node *sitter.Node, name string, source []byte) string {
	if (node.Kind() == "public_field_definition" || node.Kind() == "field_definition") && typeScriptStaticField(node, source) {
		// A static field belongs to the class, not to its instances.
		return strings.Join(append(typeScriptScopes(node, source), "static", name), ".")
	}
	id := typeScriptQualifiedID(node, name, source)
	if block := typeScriptBindingBlock(node); typeScriptNestedBlock(block) {
		return typeScriptBlockID(id, block)
	}
	return id
}

// typeScriptBlockID returns the id of a name in a nested block.
func typeScriptBlockID(id string, block *sitter.Node) string {
	return fmt.Sprintf("%s@%d", id, block.StartByte())
}

// typeScriptBlockScoped reports whether an id names a binding of a nested
// block, which is not a constant of the file.
func typeScriptBlockScoped(id string) bool {
	return strings.Contains(id, "@")
}

// typeScriptLocalBinding returns the pattern that a node binds in its
// block besides a variable: a parameter, a catch parameter, or the name of a
// function or class declaration. It returns nil for another node.
func typeScriptLocalBinding(node *sitter.Node) *sitter.Node {
	switch node.Kind() {
	case "catch_clause":
		return node.ChildByFieldName("parameter")
	case "function_declaration", "generator_function_declaration", "class_declaration", "abstract_class_declaration":
		return node.ChildByFieldName("name")
	}
	return typeScriptParameterPattern(node)
}

// typeScriptParameterPattern returns the pattern that a function parameter
// binds, or nil for another node.
func typeScriptParameterPattern(node *sitter.Node) *sitter.Node {
	parent := node.Parent()
	if parent == nil {
		return nil
	}
	// `x => x` has its one parameter without parentheses.
	if parent.Kind() == "arrow_function" && node.Kind() == "identifier" &&
		code.SameNode(parent.ChildByFieldName("parameter"), node) {
		return node
	}
	if parent.Kind() != "formal_parameters" {
		return nil
	}
	switch node.Kind() {
	case "required_parameter", "optional_parameter", "rest_parameter", "rest_pattern":
		// TypeScript wraps a parameter; a JavaScript rest parameter is a
		// rest_pattern around its name.
		if pattern := node.ChildByFieldName("pattern"); pattern != nil {
			return pattern
		}
		return node.NamedChild(0)
	case "identifier", "object_pattern", "array_pattern", "assignment_pattern":
		if node.Kind() == "assignment_pattern" {
			return node.ChildByFieldName("left")
		}
		return node
	}
	return nil
}

// typeScriptBlock returns the block that a declaration binds its names in:
// the nearest statement block, loop, class body, or the program.
func typeScriptBlock(node *sitter.Node) *sitter.Node {
	for parent := node.Parent(); parent != nil; parent = parent.Parent() {
		switch parent.Kind() {
		case "statement_block", "for_statement", "for_in_statement", "switch_body", "class_body", "program",
			"catch_clause", "arrow_function", "function_expression", "function_declaration", "method_definition",
			"function", "generator_function", "generator_function_declaration":
			return parent
		}
	}
	return node
}

// typeScriptBoundNames returns the names that a declaration pattern binds:
// the identifier, or every identifier of an object or array pattern.
func typeScriptBoundNames(pattern *sitter.Node, source []byte) []string {
	switch pattern.Kind() {
	case "identifier", "property_identifier", "shorthand_property_identifier_pattern", "type_identifier":
		// A class declaration names its class with a type identifier.
		return []string{code.Text(pattern, source)}
	case "pair_pattern":
		if value := pattern.ChildByFieldName("value"); value != nil {
			return typeScriptBoundNames(value, source)
		}
		return nil
	case "assignment_pattern", "object_assignment_pattern":
		// The default on the right is a value, not a binding.
		if left := pattern.ChildByFieldName("left"); left != nil {
			return typeScriptBoundNames(left, source)
		}
		return nil
	}
	var names []string
	for _, child := range code.NamedChildren(pattern) {
		names = append(names, typeScriptBoundNames(child, source)...)
	}
	return names
}

// at returns the framework symbols that a use at node sees. A binding of a
// nearer scope wins over one of an outer scope, and a local declaration of
// a name hides the bindings of that name in its block.
func (aliases typeScriptAliases) at(node *sitter.Node, source []byte) map[string]string {
	use := int(node.StartByte())
	scopes := typeScriptScopes(node, source)
	encloses := func(outer []string) bool {
		return len(scopes) >= len(outer) && slices.Equal(scopes[:len(outer)], outer)
	}
	visible := map[string]typeScriptAliasBinding{}
	for _, binding := range aliases.bindings {
		if len(binding.scopes) != 0 && (!encloses(binding.scopes) || binding.end > use) {
			continue
		}
		// A require binds its names in its block only.
		if use < binding.block[0] || use >= binding.block[1] {
			continue
		}
		if seen, ok := visible[binding.alias]; ok && len(seen.scopes) > len(binding.scopes) {
			continue
		}
		visible[binding.alias] = binding
	}
	for _, shadow := range aliases.shadows {
		if use < shadow.start || use >= shadow.end {
			continue
		}
		for alias, binding := range visible {
			if root, _, _ := strings.Cut(alias, "."); root == shadow.name && binding.hiddenBy(shadow, use) {
				delete(visible, alias)
			}
		}
	}
	kinds := make(map[string]string, len(visible))
	for alias, binding := range visible {
		kinds[alias] = binding.kind
	}
	return kinds
}

// typeScriptImportBinding is one name that an import statement creates. name
// is "default" for a default import and "*" for a namespace import.
type typeScriptImportBinding struct {
	module string
	name   string
	alias  string
}

// typeScriptImportBindings returns the bindings of the modules that are not
// allowlisted frameworks, so that imported constants can resolve.
func typeScriptImportBindings(root *sitter.Node, source []byte) []typeScriptImportBinding {
	var bindings []typeScriptImportBinding
	for _, binding := range typeScriptImportClauses(root, source) {
		if typeScriptAllowedImports(binding.module) == nil {
			bindings = append(bindings, binding)
		}
	}
	return bindings
}

// typeScriptImportClauses returns every binding of every import statement.
// One statement can bind a default import, a namespace, and named imports at
// once. A type-only import binds no value.
func typeScriptImportClauses(root *sitter.Node, source []byte) []typeScriptImportBinding {
	var bindings []typeScriptImportBinding
	for _, site := range typeScriptImportSites(root, source) {
		// A require inside a function binds a local name, not a symbol of
		// the file, so the repository modules of a file come from its
		// module-level imports and requires.
		if len(typeScriptScopes(site.node, source)) == 0 {
			bindings = append(bindings, site.binding)
		}
	}
	return bindings
}

// typeScriptImportSite is one binding with the statement that creates it.
type typeScriptImportSite struct {
	binding typeScriptImportBinding
	node    *sitter.Node
}

// typeScriptImportSites returns every binding of every import statement and
// every require, with the node that creates it.
func typeScriptImportSites(root *sitter.Node, source []byte) []typeScriptImportSite {
	return typeScriptImportSitesFrom(root, source, false)
}

// typeScriptImportSitesFrom returns the import sites, with the type-only
// imports when includeTypes is true.
func typeScriptImportSitesFrom(root *sitter.Node, source []byte, includeTypes bool) []typeScriptImportSite {
	var sites []typeScriptImportSite
	add := func(node *sitter.Node, bindings ...typeScriptImportBinding) {
		for _, binding := range bindings {
			sites = append(sites, typeScriptImportSite{binding: binding, node: node})
		}
	}
	code.Walk(root, func(node *sitter.Node) {
		if node.Kind() == "variable_declarator" {
			add(node, typeScriptRequireBindings(node, source)...)
			return
		}
		if node.Kind() != "import_statement" || !includeTypes && typeScriptHasTypeKeyword(node, source) {
			return
		}
		for _, clause := range code.NamedChildren(node) {
			// `import OpenAI = require("openai")` binds the module as a
			// `const OpenAI = require("openai")` does.
			if clause.Kind() == "import_require_clause" {
				add(node, typeScriptImportRequire(clause, source)...)
				return
			}
		}
		module, ok := code.StringValue(typeScriptValue(node.ChildByFieldName("source"), source, nil))
		if !ok {
			return
		}
		for _, clause := range code.NamedChildren(node) {
			if clause.Kind() != "import_clause" {
				continue
			}
			for _, child := range code.NamedChildren(clause) {
				switch child.Kind() {
				case "identifier":
					add(node, typeScriptImportBinding{module: module, name: "default", alias: code.Text(child, source)})
				case "namespace_import":
					if names := code.NamedChildren(child); len(names) == 1 {
						add(node, typeScriptImportBinding{module: module, name: "*", alias: code.Text(names[0], source)})
					}
				case "named_imports":
					add(node, typeScriptNamedImports(child, module, source, includeTypes)...)
				}
			}
		}
	})
	return sites
}

// typeScriptImportRequire reads the TypeScript CommonJS form
// `import OpenAI = require("openai")`.
func typeScriptImportRequire(clause *sitter.Node, source []byte) []typeScriptImportBinding {
	var name, module *sitter.Node
	for _, child := range code.NamedChildren(clause) {
		switch child.Kind() {
		case "identifier":
			name = child
		case "string":
			module = child
		}
	}
	if name == nil || module == nil {
		return nil
	}
	text, ok := code.StringValue(typeScriptValue(module, source, nil))
	if !ok {
		return nil
	}
	// The name holds the module, as a namespace import does, and a package
	// that exports one class is that class, as a default import. The
	// namespace binding comes last, so that a required repository module
	// reads as its exports.
	alias := code.Text(name, source)
	return []typeScriptImportBinding{{module: text, name: "default", alias: alias}, {module: text, name: "*", alias: alias}}
}

// typeScriptRequireBindings returns the bindings of a CommonJS require:
// `const OpenAI = require("openai")` binds the default export, and
// `const { Agent, tool as t } = require("@openai/agents")` binds named exports.
func typeScriptRequireBindings(declarator *sitter.Node, source []byte) []typeScriptImportBinding {
	name := declarator.ChildByFieldName("name")
	call := typeScriptUnwrap(declarator.ChildByFieldName("value"))
	// `require("pkg").Agent` binds one export of the module.
	member := ""
	if call != nil && call.Kind() == "member_expression" {
		if property := call.ChildByFieldName("property"); property != nil {
			member = code.Text(property, source)
		}
		call = typeScriptUnwrap(call.ChildByFieldName("object"))
	}
	if name == nil || call == nil || call.Kind() != "call_expression" || typeScriptCallable(call, source) != "require" {
		return nil
	}
	arguments := code.NamedChildren(call.ChildByFieldName("arguments"))
	if len(arguments) != 1 {
		return nil
	}
	module, ok := code.StringValue(typeScriptValue(arguments[0], source, nil))
	if !ok {
		return nil
	}
	if member != "" {
		if name.Kind() != "identifier" {
			return nil
		}
		return []typeScriptImportBinding{{module: module, name: member, alias: code.Text(name, source)}}
	}
	switch name.Kind() {
	case "identifier":
		// `const agents = require("@openai/agents")` holds the module, so
		// every export is reachable as agents.Agent, as a namespace import,
		// and a package that exports one class is that class, as a default
		// import. The namespace binding comes last, so that a required
		// repository module reads as its exports.
		alias := code.Text(name, source)
		return []typeScriptImportBinding{{module: module, name: "default", alias: alias}, {module: module, name: "*", alias: alias}}
	case "object_pattern":
		var bindings []typeScriptImportBinding
		for _, property := range code.NamedChildren(name) {
			switch property.Kind() {
			case "shorthand_property_identifier_pattern":
				text := code.Text(property, source)
				bindings = append(bindings, typeScriptImportBinding{module: module, name: text, alias: text})
			case "object_assignment_pattern":
				// `{ Agent = Fallback }` binds Agent with a default.
				if left := property.ChildByFieldName("left"); left != nil && left.Kind() == "shorthand_property_identifier_pattern" {
					text := code.Text(left, source)
					bindings = append(bindings, typeScriptImportBinding{module: module, name: text, alias: text})
				}
			case "pair_pattern":
				key, value := property.ChildByFieldName("key"), property.ChildByFieldName("value")
				if value != nil && value.Kind() == "assignment_pattern" {
					// `{ Tool: T = Fallback }` binds T with a default.
					value = value.ChildByFieldName("left")
				}
				if key != nil && value != nil && value.Kind() == "identifier" {
					bindings = append(bindings, typeScriptImportBinding{
						module: module, name: code.Text(key, source), alias: code.Text(value, source),
					})
				}
			}
		}
		return bindings
	}
	return nil
}

func typeScriptNamedImports(node *sitter.Node, module string, source []byte, includeTypes bool) []typeScriptImportBinding {
	var bindings []typeScriptImportBinding
	for _, specifier := range code.NamedChildren(node) {
		name := specifier.ChildByFieldName("name")
		if specifier.Kind() != "import_specifier" || name == nil || !includeTypes && typeScriptHasTypeKeyword(specifier, source) {
			continue
		}
		binding := typeScriptImportBinding{module: module, name: code.Text(name, source), alias: code.Text(name, source)}
		if alias := specifier.ChildByFieldName("alias"); alias != nil {
			binding.alias = code.Text(alias, source)
		}
		bindings = append(bindings, binding)
	}
	return bindings
}

// typeScriptHasTypeKeyword reports whether an import statement or an import
// specifier has the type keyword, which imports only a type.
func typeScriptHasTypeKeyword(node *sitter.Node, source []byte) bool {
	for index := uint(0); index < node.ChildCount(); index++ {
		child := node.Child(index)
		if !child.IsNamed() && code.Text(child, source) == "type" {
			return true
		}
	}
	return false
}

func typeScriptImportedModules(root *sitter.Node, source []byte) map[string]struct{} {
	modules := map[string]struct{}{}
	for _, binding := range typeScriptImportBindings(root, source) {
		if strings.HasPrefix(binding.module, ".") {
			modules[binding.module] = struct{}{}
		}
	}
	return modules
}

// typeScriptSourceExtensions are the file extensions of a module, in the
// order that a resolver tries them.
var typeScriptSourceExtensions = []string{".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs"}

func typeScriptModulePaths(primary, module string) []string {
	base := path.Clean(path.Join(path.Dir(primary), module))
	for _, extension := range typeScriptSourceExtensions {
		base = strings.TrimSuffix(base, extension)
	}
	paths := make([]string, 0, 2*len(typeScriptSourceExtensions))
	for _, extension := range typeScriptSourceExtensions {
		paths = append(paths, base+extension)
	}
	for _, extension := range typeScriptSourceExtensions {
		paths = append(paths, path.Join(base, "index"+extension))
	}
	return paths
}

func typeScriptImportedValues(bundle adapter.SourceBundle, typed bool) map[string]*adapter.Node {
	result := map[string]*adapter.Node{}
	tree, err := code.ParseTree(bundle.Primary, typeScriptLanguage(bundle.Primary.Path, typed))
	if err != nil {
		return result
	}
	defer tree.Close()
	cache := map[string]map[string]*adapter.Node{}
	for _, binding := range typeScriptImportBindings(tree.RootNode(), bundle.Primary.Data) {
		if !strings.HasPrefix(binding.module, ".") {
			result[binding.alias] = code.UnresolvedImport(binding.module, binding.name, binding.module)
			continue
		}
		candidates := typeScriptModulePaths(bundle.Primary.Path, binding.module)
		var document adapter.Document
		for _, candidate := range candidates {
			if value, ok := bundle.Dependencies[candidate]; ok {
				document = value
				break
			}
		}
		if document.Path == "" {
			result[binding.alias] = code.UnresolvedImport(binding.module, binding.name, candidates[0])
			continue
		}
		constants := cache[document.Path]
		if constants == nil {
			dependencyTree, parseErr := code.ParseTree(document, typeScriptLanguage(document.Path, typed))
			if parseErr != nil {
				result[binding.alias] = code.UnresolvedImport(binding.module, binding.name, document.Path)
				continue
			}
			constants, _ = typeScriptConstants(dependencyTree.RootNode(), document.Data, nil)
			// A module exports only the names that it declares exported, so
			// a private constant is not importable.
			exported := typeScriptExportedNames(dependencyTree.RootNode(), document.Data)
			dependencyTree.Close()
			code.LocateConstants(constants, document)
			visible := map[string]*adapter.Node{}
			for name, local := range exported {
				// An alias in the module binds the object of its origin.
				local = code.AliasOrigin(constants, local)
				constant, ok := constants[local]
				if ok && constant == nil {
					// A name that the module reassigns or mutates exports
					// its first literal as read-only, so the import
					// resolves but blocks a write.
					constant = code.Frozen(constants, local)
				}
				if constant != nil {
					visible[name] = constant
				}
			}
			// `export default value` declares the default itself. An export
			// clause maps the default to its constant above, which also
			// covers a constant that the clause names before its declaration.
			if constant := constants["default"]; constant != nil {
				visible["default"] = constant
			}
			// `module.exports = { ... }` makes the object the module, so a
			// default import binds the named exports as one object, as a
			// require does.
			if exported["default"] == "*" && visible["*"] == nil && visible["default"] == nil {
				whole := &adapter.Node{Fields: maps.Clone(visible)}
				visible["*"] = whole
			}
			// `module.exports = value` makes the value the module, so a
			// default import binds it too.
			if visible["default"] == nil && visible["*"] != nil {
				visible["default"] = visible["*"]
			}
			constants = visible
			cache[document.Path] = constants
		}
		var value *adapter.Node
		if whole := constants["*"]; binding.name == "*" && whole != nil {
			value = whole
		} else if binding.name == "*" {
			value = &adapter.Node{Fields: map[string]*adapter.Node{}}
			for name, constant := range constants {
				value.Fields[name] = constant
			}
		} else {
			value = constants[binding.name]
		}
		if value == nil {
			value = code.UnresolvedImport(binding.module, binding.name, document.Path)
		}
		result[binding.alias] = value
	}
	return result
}

// typeScriptBinding returns the name and the unwrapped value of a binding: a
// declaration, an assignment to a plain name such as `client = new OpenAI()`,
// a class field, or an assignment to a property of this.
func typeScriptBinding(node *sitter.Node) (*sitter.Node, *sitter.Node) {
	switch node.Kind() {
	case "variable_declarator", "public_field_definition":
		return node.ChildByFieldName("name"), typeScriptUnwrap(node.ChildByFieldName("value"))
	case "required_parameter", "optional_parameter":
		// `constructor(private client = new OpenAI())` declares and sets a
		// property of the instance, and `run(client = new OpenAI())` binds
		// a client in its function.
		if typeScriptParameterProperty(node) || node.ChildByFieldName("value") != nil {
			return node.ChildByFieldName("pattern"), typeScriptUnwrap(node.ChildByFieldName("value"))
		}
	case "assignment_pattern":
		// A JavaScript parameter with a default is the pattern itself.
		if parent := node.Parent(); parent != nil && parent.Kind() == "formal_parameters" {
			return node.ChildByFieldName("left"), typeScriptUnwrap(node.ChildByFieldName("right"))
		}
	case "field_definition":
		return node.ChildByFieldName("property"), typeScriptUnwrap(node.ChildByFieldName("value"))
	case "assignment_expression":
		left := node.ChildByFieldName("left")
		if left != nil && (left.Kind() == "identifier" || typeScriptThisProperty(left)) {
			return left, typeScriptUnwrap(node.ChildByFieldName("right"))
		}
	}
	return nil, nil
}

// typeScriptParameterProperty reports whether a parameter has an
// accessibility modifier or readonly, which makes it a property of the
// instance in a TypeScript constructor.
func typeScriptParameterProperty(parameter *sitter.Node) bool {
	for index := uint(0); index < parameter.ChildCount(); index++ {
		child := parameter.Child(index)
		if child.Kind() == "accessibility_modifier" || !child.IsNamed() && child.Kind() == "readonly" {
			return true
		}
	}
	return false
}

// typeScriptParameterOf returns the parameter of the nearest enclosing
// function that binds name, or false when the function has none.
func typeScriptParameterOf(node *sitter.Node, name string, source []byte) (*sitter.Node, bool) {
	for parent := node.Parent(); parent != nil; parent = parent.Parent() {
		switch parent.Kind() {
		case "method_definition", "function_declaration", "generator_function_declaration",
			"function_expression", "function", "generator_function", "arrow_function":
		default:
			continue
		}
		for _, parameter := range code.NamedChildren(parent.ChildByFieldName("parameters")) {
			bound := parameter
			switch parameter.Kind() {
			case "required_parameter", "optional_parameter":
				bound = parameter.ChildByFieldName("pattern")
			case "assignment_pattern":
				bound = parameter.ChildByFieldName("left")
			}
			if bound != nil && bound.Kind() == "identifier" && code.Text(bound, source) == name {
				return parameter, true
			}
		}
		return nil, false
	}
	return nil, false
}

// typeScriptParameterKind returns the framework kind that the type annotation
// of a parameter names, such as `client: OpenAI`, or an empty string.
func typeScriptParameterKind(parameter, use *sitter.Node, imports typeScriptAliases, source []byte) string {
	annotation := parameter.ChildByFieldName("type")
	if annotation == nil {
		return ""
	}
	kinds := imports.at(use, source)
	kind := ""
	// `client: OpenAI | undefined` and `client?: OpenAI` name the client
	// through the one type of the annotation that an import binds.
	code.Walk(annotation, func(node *sitter.Node) {
		if kind != "" || node.Kind() != "type_identifier" && node.Kind() != "identifier" {
			return
		}
		kind = kinds[code.Text(node, source)]
	})
	return kind
}

// typeScriptThisProperty reports whether a node is a property chain on this,
// such as this.client.
func typeScriptThisProperty(node *sitter.Node) bool {
	for node != nil && node.Kind() == "member_expression" {
		node = typeScriptUnwrap(node.ChildByFieldName("object"))
	}
	return node != nil && node.Kind() == "this"
}

// typeScriptInstanceID names the variable, field, or property that a binding
// sets. A variable is scoped like a constant. A property of this belongs to
// the instance of the enclosing class, so every method of the class sees it
// under the class scope.
func typeScriptInstanceID(binding, name *sitter.Node, source []byte) string {
	if typeScriptThisProperty(name) {
		// `this!.client` and `this.client` name one property, the same one
		// that a class field `client = ...` declares and that a use looks up
		// as a member of the class.
		property := strings.TrimPrefix(typeScriptCallableName(name, source), "this.")
		return strings.Join(append(typeScriptMemberScopes(binding, source), property), ".")
	}
	if typeScriptParameterProperty(binding) {
		// A parameter property is a property of the instance, as this.client is.
		return strings.Join(append(typeScriptMemberScopes(binding, source), code.Text(name, source)), ".")
	}
	return typeScriptBindingID(binding, code.Text(name, source), source)
}

// typeScriptClassScopes returns the scopes that enclose a node up to its
// innermost class, outermost first. A node outside every class keeps all of
// its scopes.
func typeScriptClassScopes(node *sitter.Node, source []byte) []string {
	var scopes []string
	inClass := false
	for parent := node.Parent(); parent != nil; parent = parent.Parent() {
		name := typeScriptScopeName(parent, source)
		if name == "" {
			continue
		}
		switch parent.Kind() {
		case "class_declaration", "abstract_class_declaration", "class":
			inClass = true
		}
		if inClass {
			scopes = append([]string{name}, scopes...)
		}
	}
	if !inClass {
		return typeScriptScopes(node, source)
	}
	return scopes
}

// typeScriptMemberScopes returns the scopes of a class member that a node
// reads or sets: the class scopes, with a static marker when the node sits
// in a static field or a static method, because instance code does not see
// the static members and static code does not see the instance members.
func typeScriptMemberScopes(node *sitter.Node, source []byte) []string {
	scopes := typeScriptClassScopes(node, source)
	for parent := node.Parent(); parent != nil && parent.Kind() != "class_body"; parent = parent.Parent() {
		switch parent.Kind() {
		case "class_static_block":
			// `static { ... }` runs with the class, so this is the class.
			return append(scopes, "static")
		case "public_field_definition", "field_definition", "method_definition":
			if typeScriptStaticField(parent, source) {
				return append(scopes, "static")
			}
			return scopes
		}
	}
	return scopes
}

// typeScriptInstanceKind returns the kind of the instance that a receiver
// names at a use: the one of the enclosing scope when it exists, else the
// module-level one. this.client in a method also names a class field
// `client = new OpenAI()`.
func typeScriptInstanceKind(instances map[string]string, node *sitter.Node, receiver string, source []byte) string {
	declared := func(id string) bool { _, ok := instances[id]; return ok }
	// `Bot.client` reads the static field of the class, so the class name
	// reaches the static marker first, as a constant read through it does.
	// The instance field of the same name is `this.client`, not `Bot.client`.
	if root, field, ok := strings.Cut(receiver, "."); ok && root != "this" {
		if id := typeScriptScopedName(node, root+".static."+field, source, declared); declared(id) {
			return instances[id]
		}
	}
	// A binding in scope decides, with or without a kind, so a local mock
	// or a `this.client = mock()` shadows the instance that it hides.
	if id := typeScriptScopedName(node, receiver, source, declared); declared(id) {
		return instances[id]
	}
	if field, ok := strings.CutPrefix(receiver, "this."); ok {
		return instances[strings.Join(append(typeScriptMemberScopes(node, source), field), ".")]
	}
	return ""
}

// typeScriptNumber reads a number literal, with a sign, numeric separators,
// a base prefix, and a BigInt suffix, as a float64. A value that it cannot
// read comes with the read-only reason: a sign on something other than a
// literal is a dynamic expression, and an integer beyond 53 bits has no exact
// float64, so it is not representable.
func typeScriptNumber(node *sitter.Node, source []byte) (float64, string) {
	const dynamic, notRepresentable = "dynamic expression has no safe inverse", "number is not representable"
	text := code.Text(node, source)
	sign := 1.0
	if node.Kind() == "unary_expression" {
		operand := node.ChildByFieldName("argument")
		if operand == nil || operand.Kind() != "number" {
			return 0, dynamic
		}
		switch {
		case strings.HasPrefix(text, "-"):
			sign = -1
		case strings.HasPrefix(text, "+"):
		default:
			return 0, dynamic
		}
		text = code.Text(operand, source)
	}
	text = strings.ToLower(strings.ReplaceAll(strings.TrimSuffix(text, "n"), "_", ""))
	// A literal with a base prefix, or without a fraction and an exponent, is
	// an integer.
	if strings.HasPrefix(text, "0x") || strings.HasPrefix(text, "0o") || strings.HasPrefix(text, "0b") ||
		!strings.ContainsAny(text, ".e") {
		integer, err := strconv.ParseInt(text, 0, 64)
		if err != nil || !adapter.ExactInteger(integer) {
			return 0, notRepresentable
		}
		return sign * float64(integer), ""
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsInf(value, 0) {
		return 0, notRepresentable
	}
	return sign * value, ""
}

func typeScriptObject(node *sitter.Node, source []byte, constants map[string]*adapter.Node) *adapter.Node {
	node = typeScriptUnwrap(node)
	if node == nil {
		return adapter.ReadOnly(0, 0, "object is missing")
	}
	if node.Kind() != "object" {
		if value := typeScriptValue(node, source, constants); value.Fields != nil {
			return value
		}
		return adapter.ReadOnly(int(node.StartByte()), int(node.EndByte()), "dynamic expression is not a literal object")
	}
	start, end := int(node.StartByte()), int(node.EndByte())
	fields := map[string]*adapter.Node{}
	object := map[string]any{}
	complete := true
	for _, pair := range code.NamedChildren(node) {
		if pair.Kind() == "shorthand_property_identifier" {
			// `{ instructions }` is `{ instructions: instructions }`.
			key := code.Text(pair, source)
			if _, duplicate := fields[key]; duplicate {
				return adapter.ReadOnly(start, end, "object contains a duplicate property")
			}
			value := typeScriptConstant(pair, key, int(pair.StartByte()), source, constants)
			if value == nil {
				value = adapter.ReadOnly(int(pair.StartByte()), int(pair.EndByte()), "dynamic expression has no safe inverse")
			}
			// A shorthand messages constant is a message list, as a literal
			// messages array is.
			if key == "messages" {
				if messages := code.MessageList(value); messages != nil {
					value = messages
				} else {
					value = adapter.ReadOnly(int(pair.StartByte()), int(pair.EndByte()), "messages are not a literal array")
				}
			}
			fields[key] = value
			if code.Writable(value) {
				object[key] = value.Value
			} else {
				complete = false
			}
			continue
		}
		if pair.Kind() != "pair" {
			fields[code.Text(pair, source)] = adapter.ReadOnly(
				int(pair.StartByte()), int(pair.EndByte()), "object entry is dynamic",
			)
			complete = false
			continue
		}
		keyNode := pair.ChildByFieldName("key")
		valueNode := typeScriptUnwrap(pair.ChildByFieldName("value"))
		key, ok := typeScriptPropertyName(keyNode, source)
		if !ok {
			// A write rewrites the object as JSON with literal keys, so a
			// computed key makes it read-only.
			key = code.Text(keyNode, source)
			complete = false
		}
		if _, duplicate := fields[key]; duplicate {
			return adapter.ReadOnly(start, end, "object contains a duplicate property")
		}
		value := typeScriptValue(valueNode, source, constants)
		if key == "messages" {
			value = typeScriptMessages(valueNode, source, constants)
		}
		fields[key] = value
		if code.Writable(value) {
			object[key] = value.Value
		} else {
			complete = false
		}
	}
	if !complete {
		return &adapter.Node{
			Fields: fields, Start: start, End: end, Capability: adapter.CapabilityReadOnly,
			Reason: "object contains a dynamic value",
		}
	}
	return &adapter.Node{
		Value: object, Fields: fields, Start: start, End: end, Capability: adapter.CapabilityDirect,
		Encode: adapter.JSONEncoder(source[start:end]),
	}
}

func typeScriptValue(node *sitter.Node, source []byte, constants map[string]*adapter.Node) *adapter.Node {
	node = typeScriptUnwrap(node)
	if node == nil {
		return adapter.ReadOnly(0, 0, "value is missing")
	}
	start, end := int(node.StartByte()), int(node.EndByte())
	switch node.Kind() {
	case "string", "template_string":
		value, quote, err := code.DecodeJSString(source[start:end])
		if err != nil {
			return adapter.ReadOnly(start, end, err.Error())
		}
		return adapter.String(value, start, end, func(text string) ([]byte, error) {
			encoded, err := code.EncodeJSString(text, quote)
			return code.MatchLineEndings(encoded, source[start:end]), err
		})
	case "number", "unary_expression":
		value, reason := typeScriptNumber(node, source)
		if reason != "" {
			return adapter.ReadOnly(start, end, reason)
		}
		return adapter.Number(value, start, end, func(number float64) ([]byte, error) {
			return []byte(strconv.FormatFloat(number, 'g', -1, 64)), nil
		})
	case "object":
		return typeScriptObject(node, source, constants)
	case "array":
		values := make([]any, 0, node.NamedChildCount())
		items := make([]*adapter.Node, 0, node.NamedChildCount())
		complete := true
		for _, child := range code.NamedChildren(node) {
			value := typeScriptValue(child, source, constants)
			items = append(items, value)
			values = append(values, value.Value)
			if !code.Writable(value) {
				complete = false
			}
		}
		result := &adapter.Node{
			Value: values, Items: items, Collection: true, Start: start, End: end, Capability: adapter.CapabilityDirect,
			Encode: adapter.JSONEncoder(source[start:end]),
		}
		if !complete {
			result.Value = nil
			result.Capability = adapter.CapabilityReadOnly
			result.Reason = "array contains dynamic values"
			result.Encode = nil
		}
		return result
	case "true":
		return adapter.Bool(true, start, end, nil)
	case "false":
		return adapter.Bool(false, start, end, nil)
	case "null":
		return &adapter.Node{Start: start, End: end, Capability: adapter.CapabilityDirect, Encode: adapter.JSONEncoder(nil)}
	case "member_expression":
		// Each segment of values.CONFIG.instructions is a static object. A
		// wrapper on a segment, such as values.CONFIG!.prompt, changes nothing.
		object := typeScriptUnwrap(node.ChildByFieldName("object"))
		property := node.ChildByFieldName("property")
		if object != nil && property != nil && object.Kind() == "identifier" && constants != nil {
			// Prompts.system reads the static field of the class.
			declared := func(id string) bool { _, ok := constants[id]; return ok }
			name := code.Text(object, source) + ".static." + code.Text(property, source)
			if id := typeScriptScopedName(node, name, source, declared); declared(id) {
				if value := constants[id]; value != nil {
					return value.Clone()
				}
			}
		}
		if object != nil && property != nil && object.Kind() == "this" && constants != nil {
			// this.instructions in a method reads the class field constant.
			id := strings.Join(append(typeScriptMemberScopes(node, source), code.Text(property, source)), ".")
			if value := constants[id]; value != nil {
				return value.Clone()
			}
		}
		if object != nil && property != nil &&
			(object.Kind() == "identifier" || object.Kind() == "member_expression" || object.Kind() == "subscript_expression") {
			if base := typeScriptValue(object, source, constants); base.Fields != nil {
				if value := base.Fields[code.Text(property, source)]; value != nil {
					return value
				}
			}
		}
	case "subscript_expression":
		// CONFIG["prompt"] and LIST[1] read a static object or array.
		object := typeScriptUnwrap(node.ChildByFieldName("object"))
		index := typeScriptUnwrap(node.ChildByFieldName("index"))
		if object != nil && index != nil {
			base := typeScriptValue(object, source, constants)
			switch key := typeScriptValue(index, source, nil).Value.(type) {
			case string:
				if value := base.Fields[key]; value != nil {
					return value
				}
			case float64:
				if position := int(key); float64(position) == key && position >= 0 && position < len(base.Items) {
					return base.Items[position]
				}
			}
		}
	case "identifier":
		if value := typeScriptConstant(node, code.Text(node, source), start, source, constants); value != nil {
			return value
		}
	}
	return adapter.ReadOnly(start, end, "dynamic expression has no safe inverse")
}

// typeScriptMessages reads a message list: a literal array, or a constant or
// namespace member that holds one.
func typeScriptMessages(node *sitter.Node, source []byte, constants map[string]*adapter.Node) *adapter.Node {
	node = typeScriptUnwrap(node)
	if node == nil {
		return adapter.ReadOnly(0, 0, "messages are missing")
	}
	if node.Kind() != "array" {
		if messages := code.MessageList(typeScriptValue(node, source, constants)); messages != nil {
			return messages
		}
		return adapter.ReadOnly(int(node.StartByte()), int(node.EndByte()), "messages are not a literal array")
	}
	start, end := int(node.StartByte()), int(node.EndByte())
	var items []*adapter.Node
	var messages []syncdomain.Message
	complete := true
	for _, child := range code.NamedChildren(node) {
		item := typeScriptMessage(child, source, constants)
		items = append(items, item)
		role, roleOK := item.Fields["role"].Value.(string)
		content, contentOK := item.Fields["content"].Value.(string)
		if !roleOK || !contentOK || !code.Writable(item.Fields["content"]) {
			complete = false
			continue
		}
		messages = append(messages, syncdomain.Message{
			Role: code.NormalizeRole(role), Content: content,
		})
	}
	result := &adapter.Node{
		Items: items, Collection: true, Start: start, End: end, Capability: adapter.CapabilityDirect,
		Reason: "message structure is read-only",
	}
	if complete {
		result.Value = messages
	} else {
		result.Capability = adapter.CapabilityReadOnly
		result.Reason = "dynamic message content is read-only"
	}
	return result
}

func typeScriptMessage(node *sitter.Node, source []byte, constants map[string]*adapter.Node) *adapter.Node {
	node = typeScriptUnwrap(node)
	fields := map[string]*adapter.Node{}
	switch node.Kind() {
	case "array":
		children := code.NamedChildren(node)
		if len(children) == 2 {
			fields["role"] = typeScriptValue(children[0], source, constants)
			fields["content"] = typeScriptValue(children[1], source, constants)
		}
	case "object":
		if object := typeScriptObject(node, source, constants); object.Fields != nil {
			fields = object.Fields
		}
	case "new_expression":
		// `new SystemMessage("...")` and the other LangChain message classes
		// carry their role in the class name and their content first.
		role := ""
		switch constructor := typeScriptCallable(node, source); {
		case strings.HasSuffix(constructor, "SystemMessage"):
			role = syncdomain.RoleSystem
		case strings.HasSuffix(constructor, "HumanMessage"):
			role = syncdomain.RoleUser
		case strings.HasSuffix(constructor, "AIMessage"):
			role = syncdomain.RoleAssistant
		}
		if arguments := code.NamedChildren(node.ChildByFieldName("arguments")); role != "" && len(arguments) != 0 {
			fields["role"] = &adapter.Node{Value: role}
			argument := typeScriptUnwrap(arguments[0])
			if argument != nil && argument.Kind() == "object" {
				fields["content"] = typeScriptObject(argument, source, constants).Fields["content"]
			} else {
				fields["content"] = typeScriptValue(argument, source, constants)
			}
		}
	}
	if fields["role"] == nil {
		fields["role"] = adapter.ReadOnly(int(node.StartByte()), int(node.EndByte()), "message role is not static")
	}
	if fields["content"] == nil {
		fields["content"] = adapter.ReadOnly(int(node.StartByte()), int(node.EndByte()), "message content is not static")
	}
	return &adapter.Node{Fields: fields}
}

func typeScriptPropertyName(node *sitter.Node, source []byte) (string, bool) {
	if node == nil {
		return "", false
	}
	switch node.Kind() {
	case "property_identifier", "identifier":
		return code.Text(node, source), true
	case "string":
		value := typeScriptValue(node, source, nil)
		text, ok := value.Value.(string)
		return text, ok
	case "computed_property_name":
		if inner := typeScriptUnwrap(node.NamedChild(0)); inner != nil && inner.Kind() == "string" {
			return typeScriptPropertyName(inner, source)
		}
		return code.Text(node, source), false
	case "number":
		return code.Text(node, source), true
	default:
		return "", false
	}
}
