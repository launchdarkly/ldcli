// Package python inspects Python sources. It finds agents, prompts, tools, and
// constants through an import allowlist and never runs Python.
package python

import (
	"cmp"
	"fmt"
	"maps"
	"math"
	"path"
	"slices"
	"strconv"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	python "github.com/tree-sitter/tree-sitter-python/bindings/go"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	"github.com/launchdarkly/ldcli/internal/sync/adapter"
	"github.com/launchdarkly/ldcli/internal/sync/adapter/code"
)

type pythonAdapter struct{}

func (pythonAdapter) Format() string   { return "python" }
func (pythonAdapter) Behavior() string { return "python/1" }

func (pythonAdapter) Inspect(bundle adapter.SourceBundle) (*adapter.Node, error) {
	return parsePythonBundle(bundle)
}

func (pythonAdapter) Defaults(syncdomain.Kind) (string, map[string]string) { return "", nil }

func (pythonAdapter) Dependencies(document adapter.Document) ([]string, error) {
	tree, err := code.ParseTree(document, sitter.NewLanguage(python.Language()))
	if err != nil {
		return nil, err
	}
	defer tree.Close()
	var dependencies []string
	for module := range pythonImportedModules(tree.RootNode(), document.Data) {
		dependencies = append(dependencies, pythonModulePaths(document.Path, module)...)
	}
	return dependencies, nil
}

func init() { adapter.MustRegister(pythonAdapter{}) }

func parsePythonBundle(bundle adapter.SourceBundle) (*adapter.Node, error) {
	return parsePython(bundle.Primary, pythonImportedValues(bundle))
}

func parsePython(document adapter.Document, imported map[string]*adapter.Node) (*adapter.Node, error) {
	tree, err := code.ParseTree(document, sitter.NewLanguage(python.Language()))
	if err != nil {
		return nil, err
	}
	defer tree.Close()

	aliases := pythonImports(tree.RootNode(), document.Data)
	instances := pythonInstances(tree.RootNode(), document.Data, aliases)
	constants, constantResources := pythonConstants(tree.RootNode(), document.Data, imported)
	for name, value := range imported {
		if _, shadowed := constants[name]; !shadowed {
			constants[name] = value
		}
	}
	collections := map[string][]*adapter.Node{"agents": {}, "prompts": {}, "tools": {}, "constants": {}}
	collections["constants"] = constantResources
	fallbacks := code.NewFallbackIDs()
	// A name that holds a resource more than once in its scope gives each
	// assignment an ordinal, so that no two resources share one id.
	assigned := pythonAssignmentCounts(tree.RootNode(), document.Data)
	ordinals := map[string]int{}
	code.Walk(tree.RootNode(), func(node *sitter.Node) {
		if node.Kind() != "assignment" && node.Kind() != "named_expression" {
			return
		}
		name, value := pythonAssignmentParts(node)
		if !pythonNamedTarget(name) || value == nil {
			return
		}
		id := pythonQualifiedID(node, code.Text(name, document.Data), document.Data)
		positional := assigned[id] > 1
		if positional {
			ordinals[id]++
			id = fmt.Sprintf("%s#%d", id, ordinals[id])
		}
		// The framework call of a chain takes its outermost variable name,
		// `agent` in `agent = self.backup = Agent()`. A chain of attributes
		// only, such as self.agent, leaves the call to the second walk, which
		// names it like an unassigned call.
		if name.Kind() == "identifier" && !pythonChainTarget(node, "identifier") {
			if collection, resource, ok := pythonFrameworkCall(
				value, id, aliases, instances, document.Data, constants,
			); ok {
				if positional {
					resource.Fields["id"] = adapter.PositionalID(id)
				}
				collections[collection] = append(collections[collection], resource)
			}
		}
		// The inner link of a chain, `b = Agent()` in `a = b = Agent()`, holds
		// the prompts that the outer name already collected. An outer link on
		// a subscript, `items[0] = b = Agent()`, names nothing, so the inner
		// name collects them.
		if pythonNamedAssignment(node.Parent()) != nil {
			return
		}
		// One prompt under the assignment, alone or piped into a chain, takes
		// the variable or attribute name. Several take the name with an
		// ordinal, so that each has a key of its own.
		calls := pythonPromptCalls(value, aliases, document.Data)
		for index, call := range calls {
			fields := pythonPromptFields(call, aliases, document.Data, constants)
			resource := adapter.Resource(id, fields)
			switch {
			case len(calls) > 1:
				// The ordinal is the position of the prompt in the chain.
				resource.Fields["id"] = adapter.PositionalID(fmt.Sprintf("%s#%d", id, index+1))
			case positional:
				resource.Fields["id"] = adapter.PositionalID(id)
			}
			collections["prompts"] = append(collections["prompts"], resource)
		}
	})
	code.Walk(tree.RootNode(), func(node *sitter.Node) {
		if node.Kind() != "call" {
			return
		}
		// The first walk named the call that is the whole value of an
		// assignment. A call nested in a list or in another call, such as an
		// agent in a crew, is a resource of its own.
		if !pythonAssignedValue(node) {
			if collection, resource, ok := pythonFrameworkCall(
				node, "", aliases, instances, document.Data, constants,
			); ok {
				callable := code.Text(node.ChildByFieldName("function"), document.Data)
				fallbacks.Distinguish(resource, pythonQualifiedCallID(node, callable, document.Data))
				collections[collection] = append(collections[collection], resource)
			}
		}
		// A prompt that a function returns, or builds in place, has no
		// variable name, so the enclosing function names it.
		if !pythonAssignedCall(node) && pythonPromptCall(node, aliases, document.Data) {
			callable := code.Text(node.ChildByFieldName("function"), document.Data)
			fields := pythonPromptFields(node, aliases, document.Data, constants)
			id := pythonQualifiedCallID(node, callable, document.Data)
			resource := adapter.Resource(id, fields)
			fallbacks.Distinguish(resource, id)
			collections["prompts"] = append(collections["prompts"], resource)
		}
	})
	code.Walk(tree.RootNode(), func(node *sitter.Node) {
		if node.Kind() != "decorated_definition" {
			return
		}
		collection := pythonDecoratedCollection(node, aliases, instances, document.Data)
		if collection == "" {
			return
		}
		var function *sitter.Node
		for _, child := range code.NamedChildren(node) {
			if child.Kind() == "function_definition" {
				function = child
				break
			}
		}
		if function == nil {
			return
		}
		name := function.ChildByFieldName("name")
		if name == nil {
			return
		}
		// The function name is scoped like every other resource, so two
		// decorated methods with one name in two classes keep distinct ids.
		id := pythonQualifiedID(function, code.Text(name, document.Data), document.Data)
		fields := map[string]*adapter.Node{}
		// The docstring is the first statement of the body. A comment before
		// it is not a statement.
		if statements := code.NamedChildren(function.ChildByFieldName("body")); len(statements) != 0 {
			first := statements[0]
			if first.Kind() == "expression_statement" && first.NamedChildCount() == 1 {
				docstring := first.NamedChild(0)
				if docstring.Kind() == "string" {
					fields["description"] = pythonValue(docstring, document.Data, constants)
				}
			}
		}
		for _, child := range code.NamedChildren(node) {
			if child.Kind() != "decorator" || child.NamedChildCount() == 0 {
				continue
			}
			decorator := child.NamedChild(0)
			if decorator.Kind() == "call" {
				for key, value := range pythonKeywordFields(decorator, document.Data, constants) {
					fields[key] = value
				}
			}
		}
		if parameters := function.ChildByFieldName("parameters"); parameters != nil && parameters.NamedChildCount() != 0 {
			fields["schema"] = adapter.ReadOnly(
				int(parameters.StartByte()), int(parameters.EndByte()),
				"schema inferred from type hints is read-only",
			)
		}
		if collection == "prompts" {
			body := function.ChildByFieldName("body")
			fields["template"] = adapter.ReadOnly(
				int(body.StartByte()), int(body.EndByte()),
				"prompt built by a function is read-only",
			)
		}
		// The function name is a stable identity, so the id is not positional.
		collections[collection] = append(collections[collection], adapter.Resource(id, fields))
	})
	for _, name := range []string{"agents", "prompts", "tools"} {
		code.DistinguishDuplicates(collections[name])
	}
	return adapter.Collections(collections), nil
}

// pythonUnwrap returns the expression inside await and parentheses, which
// change no value: `await client.create(...)` or `(Agent(...))`.
func pythonUnwrap(node *sitter.Node) *sitter.Node {
	for node != nil {
		switch node.Kind() {
		case "await":
			node = node.NamedChild(0)
		case "assignment":
			// `a = b = Agent()` nests the second assignment as the value of
			// the first, and both names hold the call.
			node = node.ChildByFieldName("right")
		case "named_expression":
			// `a = (b := Agent())` holds the call in the walrus.
			node = node.ChildByFieldName("value")
		case "parenthesized_expression":
			inner := code.NamedChildren(node)
			if len(inner) != 1 {
				return node
			}
			node = inner[0]
		default:
			return node
		}
	}
	return nil
}

// pythonAssignedValue reports whether a call is the whole value of an
// assignment, with or without await and parentheses around it, which the
// assignment walk names after its variable.
func pythonAssignedValue(call *sitter.Node) bool {
	for parent := call.Parent(); parent != nil; parent = parent.Parent() {
		switch parent.Kind() {
		case "await", "parenthesized_expression":
			continue
		case "assignment", "named_expression":
			left, right := pythonAssignmentParts(parent)
			if !code.SameNode(right, call) {
				return false
			}
			// The assignment walk names a call when a variable of the chain
			// holds it. A chain of attributes only, such as self.agent, is
			// named like an unassigned call.
			if left != nil && left.Kind() == "identifier" {
				return true
			}
		default:
			return false
		}
	}
	return false
}

// pythonAssignedCall reports whether a call sits under an assignment that
// names it: one with a variable or an attribute target. A call under a
// subscript or an unpacking target, such as `prompts["greet"] = ...` or
// `first, second = ...`, has no name, so the enclosing function names it.
func pythonAssignedCall(call *sitter.Node) bool {
	return pythonNamedAssignment(call.Parent()) != nil
}

// pythonNamedAssignment returns the nearest assignment that encloses node in
// its statement and has a variable or an attribute target, or nil.
func pythonNamedAssignment(node *sitter.Node) *sitter.Node {
	for ; node != nil; node = node.Parent() {
		switch node.Kind() {
		case "assignment", "named_expression":
			if name, _ := pythonAssignmentParts(node); pythonNamedTarget(name) {
				return node
			}
		case "expression_statement", "return_statement":
			return nil
		}
	}
	return nil
}

// pythonDeclaredVariable returns the variable and the value of an assignment
// or a walrus, `name = value` or `name := value`, or nil for another node or
// another target. A walrus in a comprehension binds in the enclosing scope,
// as the assignment does.
func pythonDeclaredVariable(node *sitter.Node) (name, value *sitter.Node) {
	switch node.Kind() {
	case "assignment":
		name, value = node.ChildByFieldName("left"), node.ChildByFieldName("right")
	case "named_expression":
		name, value = node.ChildByFieldName("name"), node.ChildByFieldName("value")
	default:
		return nil, nil
	}
	if name == nil || name.Kind() != "identifier" {
		return nil, nil
	}
	return name, value
}

// pythonAssignmentCounts counts the assignments to each variable or
// attribute by qualified id, so that a name assigned more than once in its
// scope is known before the resources are collected.
func pythonAssignmentCounts(root *sitter.Node, source []byte) map[string]int {
	counts := map[string]int{}
	code.Walk(root, func(node *sitter.Node) {
		if node.Kind() != "assignment" && node.Kind() != "named_expression" {
			return
		}
		if name, _ := pythonAssignmentParts(node); pythonNamedTarget(name) {
			counts[pythonQualifiedID(node, code.Text(name, source), source)]++
		}
	})
	return counts
}

// pythonAssignmentParts returns the target and the unwrapped value of an
// assignment, `name = value`, or of a walrus, `name := value`. Another node
// has neither.
func pythonAssignmentParts(node *sitter.Node) (name, value *sitter.Node) {
	switch node.Kind() {
	case "assignment":
		return node.ChildByFieldName("left"), pythonUnwrap(node.ChildByFieldName("right"))
	case "named_expression":
		return node.ChildByFieldName("name"), pythonUnwrap(node.ChildByFieldName("value"))
	}
	return nil, nil
}

// pythonChainTarget reports whether an outer link of the assignment chain
// of node has a target of the kind. A chain is `a = b = Agent()`, where each
// outer assignment holds the inner one as its whole value. A walrus inside a
// list or a call, as in `x = [a := Agent()]`, is not a link of a chain.
func pythonChainTarget(node *sitter.Node, kind string) bool {
	child := node
	for parent := node.Parent(); parent != nil; child, parent = parent, parent.Parent() {
		switch parent.Kind() {
		case "await", "parenthesized_expression":
			continue
		case "assignment", "named_expression":
			left, right := pythonAssignmentParts(parent)
			if !code.SameNode(right, pythonUnwrap(child)) {
				return false
			}
			if left != nil && left.Kind() == kind {
				return true
			}
		default:
			return false
		}
	}
	return false
}

// pythonNamedTarget reports whether an assignment target is a variable or
// an attribute, which names the value that it holds.
func pythonNamedTarget(name *sitter.Node) bool {
	return name != nil && (name.Kind() == "identifier" || name.Kind() == "attribute")
}

func pythonPromptCalls(node *sitter.Node, aliases pythonAliases, source []byte) []*sitter.Node {
	var calls []*sitter.Node
	code.Walk(node, func(candidate *sitter.Node) {
		if pythonPromptCall(candidate, aliases, source) {
			calls = append(calls, candidate)
		}
	})
	return calls
}

// pythonPromptCall reports whether a node builds a prompt template of an
// allowlisted framework.
func pythonPromptCall(node *sitter.Node, aliases pythonAliases, source []byte) bool {
	if node.Kind() != "call" {
		return false
	}
	callable := code.Text(node.ChildByFieldName("function"), source)
	owner, method, qualified := code.SplitMethod(callable)
	kinds := aliases.at(node, source)
	return isPythonPromptKind(kinds[callable]) ||
		(qualified && isPythonPromptKind(kinds[owner]) && (method == "from_messages" || method == "from_template"))
}

func pythonPromptFields(
	call *sitter.Node,
	aliases pythonAliases,
	source []byte,
	constants map[string]*adapter.Node,
) map[string]*adapter.Node {
	callable := code.Text(call.ChildByFieldName("function"), source)
	owner, method, qualified := code.SplitMethod(callable)
	arguments := pythonPositionalArguments(call)
	kinds := aliases.at(call, source)
	if qualified && isPythonPromptKind(kinds[owner]) {
		// The factory takes its one argument by position or by keyword.
		argument := func(name string) *sitter.Node {
			if len(arguments) != 0 {
				return arguments[0]
			}
			return pythonKeywordArgument(call, name, source)
		}
		switch {
		case method == "from_messages" && argument("messages") != nil:
			return code.TranslateFields(
				map[string]*adapter.Node{"messages": pythonMessages(argument("messages"), source, constants)},
				code.BracePlaceholders, "messages",
			)
		case method == "from_template" && argument("template") != nil:
			return code.TranslateFields(
				map[string]*adapter.Node{"template": pythonValue(argument("template"), source, constants)},
				code.BracePlaceholders, "template",
			)
		case len(arguments) == 0:
			return map[string]*adapter.Node{}
		}
	}
	fields := pythonKeywordFields(call, source, constants)
	if len(arguments) != 0 {
		if kinds[callable] == "prompt-template" {
			fields["template"] = pythonValue(arguments[0], source, constants)
		} else if _, exists := fields["messages"]; !exists {
			fields["messages"] = pythonMessages(arguments[0], source, constants)
		}
	}
	return code.TranslateFields(fields, code.BracePlaceholders, "template", "messages")
}

func isPythonPromptKind(kind string) bool {
	return kind == "prompt-template" || kind == "chat-prompt"
}

// pythonKeywordArgument returns the value of a keyword argument, or nil.
func pythonKeywordArgument(call *sitter.Node, name string, source []byte) *sitter.Node {
	for _, argument := range code.NamedChildren(call.ChildByFieldName("arguments")) {
		if argument.Kind() == "keyword_argument" && code.Text(argument.ChildByFieldName("name"), source) == name {
			return argument.ChildByFieldName("value")
		}
	}
	return nil
}

// pythonPositionalArguments returns the arguments that a call passes by
// position. A splat, such as *args or **options, fills no positional field.
func pythonPositionalArguments(call *sitter.Node) []*sitter.Node {
	var result []*sitter.Node
	for _, argument := range code.NamedChildren(call.ChildByFieldName("arguments")) {
		switch argument.Kind() {
		case "keyword_argument", "list_splat", "dictionary_splat":
			continue
		}
		result = append(result, argument)
	}
	return result
}

// pythonAliases are the framework symbols that the imports of a file bind,
// such as Agent to "openai-agent", with the position and the scope of each
// import. Two frameworks can export one name, so a use sees the binding
// that Python sees at that point.
type pythonAliases struct {
	bindings []pythonAliasBinding
}

// pythonAliasBinding is one framework symbol that an import binds.
type pythonAliasBinding struct {
	alias, kind string
	// scopes are the functions and classes that enclose the import.
	scopes []string
	end    int
	// class reports that the innermost scope is a class body, which binds
	// the name on the class only.
	class bool
	// fallback is one more than the start of the try statement whose body
	// or except clause holds the import, or zero without one. Two imports
	// of one name in one try statement bind either one, as in an import
	// with a fallback.
	fallback int
}

// pythonImports returns the framework symbols that the imports of a file
// bind.
func pythonImports(root *sitter.Node, source []byte) pythonAliases {
	var bindings []pythonAliasBinding
	add := func(node *sitter.Node, alias, kind string) {
		chain := pythonScopeChain(node, source)
		bindings = append(bindings, pythonAliasBinding{
			alias: alias, kind: kind, scopes: pythonScopes(node, source), end: int(node.EndByte()),
			class:    len(chain) != 0 && chain[len(chain)-1].class,
			fallback: pythonTryStatement(node),
		})
	}
	for _, site := range pythonImportSites(root, source) {
		allowed := pythonAllowedImports(site.binding.module)
		if allowed == nil {
			continue
		}
		if site.binding.name == "*" {
			// `from agents import *` binds every symbol of the framework.
			for _, name := range slices.Sorted(maps.Keys(allowed)) {
				add(site.node, name, allowed[name])
			}
			continue
		}
		if kind, ok := allowed[site.binding.name]; ok {
			add(site.node, site.binding.alias, kind)
		}
	}
	// `import agents as oa` makes every symbol reachable as oa.Agent.
	for _, site := range pythonModuleAliasSites(root, source) {
		allowed := pythonAllowedImports(site.module)
		for _, name := range slices.Sorted(maps.Keys(allowed)) {
			add(site.node, site.alias+"."+name, allowed[name])
		}
	}
	slices.SortStableFunc(bindings, func(left, right pythonAliasBinding) int { return cmp.Compare(left.end, right.end) })
	return pythonAliases{bindings: bindings}
}

// at returns the framework symbols that a use at node sees: every
// module-level import when the use is inside a function or a class, the
// module-level imports before the use when it is module-level code, and the
// imports of the enclosing functions before the use. A binding of a nearer
// scope wins over one of an outer scope, and a later binding wins over an
// earlier one of the same scope.
func (aliases pythonAliases) at(node *sitter.Node, source []byte) map[string]string {
	use := int(node.StartByte())
	chain := pythonScopeChain(node, source)
	scopes := make([]string, len(chain))
	// A function body runs later, so it sees every module-level import. A
	// class body runs where it is, like module-level code.
	inFunction := pythonInLambda(node)
	for index, scope := range chain {
		scopes[index] = scope.name
		inFunction = inFunction || !scope.class
	}
	kinds := map[string]string{}
	depths := map[string]int{}
	fallbacks := map[string]int{}
	ambiguous := map[string]bool{}
	for _, binding := range aliases.bindings {
		if len(binding.scopes) == 0 {
			if !inFunction && binding.end > use {
				continue
			}
		} else if len(scopes) < len(binding.scopes) || !slices.Equal(scopes[:len(binding.scopes)], binding.scopes) || binding.end > use {
			continue
		} else if binding.class && (len(scopes) != len(binding.scopes) || pythonInExpressionScope(node)) {
			// A class body binds a name on the class only. A method, a
			// lambda, or a comprehension inside the class resolves a bare
			// name through the enclosing functions and the module.
			continue
		}
		if depth, seen := depths[binding.alias]; seen && depth > len(binding.scopes) {
			continue
		}
		// Two imports of one name in one try statement, as in an import
		// with a fallback, bind either framework, so the name is ambiguous.
		if previous, seen := fallbacks[binding.alias]; seen && previous != 0 && previous == binding.fallback && kinds[binding.alias] != binding.kind {
			ambiguous[binding.alias] = true
		}
		kinds[binding.alias], depths[binding.alias], fallbacks[binding.alias] = binding.kind, len(binding.scopes), binding.fallback
	}
	for alias := range ambiguous {
		delete(kinds, alias)
	}
	return kinds
}

// pythonTryStatement returns one more than the start of the try statement
// whose body or except clause holds a node within its scope, or zero without
// one. The offset keeps a try statement at the start of the file apart from
// no try statement.
func pythonTryStatement(node *sitter.Node) int {
	for parent := node.Parent(); parent != nil; parent = parent.Parent() {
		switch parent.Kind() {
		case "function_definition", "class_definition":
			return 0
		case "try_statement":
			return int(parent.StartByte()) + 1
		}
	}
	return 0
}

// pythonImportBinding is one name that a from-import creates.
type pythonImportBinding struct {
	module string
	name   string
	alias  string
}

// pythonImportBindings returns the bindings of the modules that are not
// allowlisted frameworks, so that imported constants can resolve.
func pythonImportBindings(root *sitter.Node, source []byte) []pythonImportBinding {
	var bindings []pythonImportBinding
	for _, binding := range pythonImportClauses(root, source) {
		if pythonAllowedImports(binding.module) == nil {
			bindings = append(bindings, binding)
		}
	}
	return bindings
}

// pythonImportClauses returns every binding of every from-import. It reads
// the syntax tree, so a comment or a line break inside the import does not
// change the names.
func pythonImportClauses(root *sitter.Node, source []byte) []pythonImportBinding {
	sites := pythonImportSites(root, source)
	bindings := make([]pythonImportBinding, len(sites))
	for index, site := range sites {
		bindings[index] = site.binding
	}
	return bindings
}

// pythonImportSite is one binding of a from-import with its statement.
type pythonImportSite struct {
	binding pythonImportBinding
	node    *sitter.Node
}

// pythonImportSites returns every binding of every from-import with the
// import statement that creates it.
func pythonImportSites(root *sitter.Node, source []byte) []pythonImportSite {
	var bindings []pythonImportSite
	code.Walk(root, func(node *sitter.Node) {
		if node.Kind() != "import_from_statement" {
			return
		}
		module := code.Text(node.ChildByFieldName("module_name"), source)
		if module == "" {
			return
		}
		for index := uint(0); index < node.ChildCount(); index++ {
			child := node.Child(index)
			if node.FieldNameForChild(uint32(index)) != "name" {
				continue
			}
			binding := pythonImportBinding{module: module}
			switch child.Kind() {
			case "dotted_name":
				binding.name = code.Text(child, source)
				binding.alias = binding.name
			case "aliased_import":
				binding.name = code.Text(child.ChildByFieldName("name"), source)
				binding.alias = code.Text(child.ChildByFieldName("alias"), source)
			default:
				continue
			}
			bindings = append(bindings, pythonImportSite{binding: binding, node: node})
		}
		// `from .values import *` binds every name of the module. The names
		// are not known statically, so the binding is the wildcard itself.
		for _, child := range code.NamedChildren(node) {
			if child.Kind() == "wildcard_import" {
				bindings = append(bindings, pythonImportSite{
					binding: pythonImportBinding{module: module, name: "*", alias: "*"}, node: node,
				})
			}
		}
	})
	return bindings
}

// pythonModuleAliases maps each name that an import statement binds to its
// module, such as "oa" for `import agents as oa`.
// pythonModuleAliasSite is one module that an import statement binds, as
// `import agents` or `import agents as oa`.
type pythonModuleAliasSite struct {
	alias, module string
	node          *sitter.Node
}

func pythonModuleAliasSites(root *sitter.Node, source []byte) []pythonModuleAliasSite {
	var sites []pythonModuleAliasSite
	code.Walk(root, func(node *sitter.Node) {
		if node.Kind() != "import_statement" {
			return
		}
		for index := uint(0); index < node.ChildCount(); index++ {
			child := node.Child(index)
			if node.FieldNameForChild(uint32(index)) != "name" {
				continue
			}
			switch child.Kind() {
			case "dotted_name":
				module := code.Text(child, source)
				sites = append(sites, pythonModuleAliasSite{alias: module, module: module, node: node})
			case "aliased_import":
				sites = append(sites, pythonModuleAliasSite{
					alias:  code.Text(child.ChildByFieldName("alias"), source),
					module: code.Text(child.ChildByFieldName("name"), source),
					node:   node,
				})
			}
		}
	})
	return sites
}

func pythonImportedModules(root *sitter.Node, source []byte) map[string]struct{} {
	modules := map[string]struct{}{}
	for _, binding := range pythonImportBindings(root, source) {
		modules[binding.module] = struct{}{}
	}
	return modules
}

func pythonModulePaths(primary, module string) []string {
	directory := path.Dir(primary)
	dots := len(module) - len(strings.TrimLeft(module, "."))
	module = strings.TrimLeft(module, ".")
	if dots == 0 {
		directory = "."
	} else {
		for level := 1; level < dots; level++ {
			directory = path.Dir(directory)
		}
	}
	modulePath := path.Join(directory, strings.ReplaceAll(module, ".", "/"))
	return []string{modulePath + ".py", path.Join(modulePath, "__init__.py")}
}

func pythonImportedValues(bundle adapter.SourceBundle) map[string]*adapter.Node {
	result := map[string]*adapter.Node{}
	tree, err := code.ParseTree(bundle.Primary, sitter.NewLanguage(python.Language()))
	if err != nil {
		return result
	}
	defer tree.Close()
	cache := map[string]map[string]*adapter.Node{}
	for _, site := range pythonImportSites(tree.RootNode(), bundle.Primary.Data) {
		binding := site.binding
		if pythonAllowedImports(binding.module) != nil {
			continue
		}
		// An import inside a function binds its name in that function, so
		// the value is keyed like a constant of that scope, and from the
		// import statement on.
		key := pythonQualifiedID(site.node, binding.alias, bundle.Primary.Data)
		code.RecordImportSite(result, key, int(site.node.EndByte()))
		if previous, duplicate := result[key]; duplicate {
			// Two imports bind one name, as in a try/except fallback, so no
			// one module is its value.
			result[key] = code.AmbiguousImport(binding.alias, previous.File)
			continue
		}
		candidates := pythonModulePaths(bundle.Primary.Path, binding.module)
		var document adapter.Document
		for _, candidate := range candidates {
			if value, ok := bundle.Dependencies[candidate]; ok {
				document = value
				break
			}
		}
		if document.Path == "" {
			result[key] = code.UnresolvedImport(binding.module, binding.name, candidates[0])
			continue
		}
		constants := cache[document.Path]
		if constants == nil {
			dependencyTree, parseErr := code.ParseTree(document, sitter.NewLanguage(python.Language()))
			if parseErr != nil {
				result[key] = code.UnresolvedImport(binding.module, binding.name, document.Path)
				continue
			}
			constants, _ = pythonConstants(dependencyTree.RootNode(), document.Data, nil)
			dependencyTree.Close()
			for _, value := range constants {
				value.SetFile(document.Path)
			}
			cache[document.Path] = constants
		}
		value := constants[binding.name]
		if value == nil {
			value = code.UnresolvedImport(binding.module, binding.name, document.Path)
		}
		result[key] = value
	}
	return result
}

// pythonDeclaration is one binding of a name in the file: an assignment
// with its value, or a parameter or target without one.
type pythonDeclaration struct {
	node  *sitter.Node
	id    string
	value *sitter.Node
}

// pythonConstants returns the literal constants of a file by qualified id,
// and the constant resources that constants[id="NAME"] selects. imported
// holds the values of the static imports. A literal can use an imported
// value or a constant of the same file that is declared before it.
func pythonConstants(root *sitter.Node, source []byte, imported map[string]*adapter.Node) (map[string]*adapter.Node, []*adapter.Node) {
	type declaration = pythonDeclaration
	var declarations []declaration
	counts := map[string]int{}
	code.Walk(root, func(node *sitter.Node) {
		name, valueNode := pythonDeclaredVariable(node)
		if name == nil || valueNode == nil {
			return
		}
		id := pythonBindingID(node, code.Text(name, source), source)
		declarations = append(declarations, declaration{node: node, id: id, value: valueNode})
		counts[id]++
	})
	// A parameter hides a constant of the same name in its function, so it
	// has an entry without a value.
	code.Walk(root, func(node *sitter.Node) {
		if node.Kind() != "parameters" {
			return
		}
		for _, parameter := range code.NamedChildren(node) {
			if name := pythonParameterName(parameter, source); name != "" {
				id := pythonQualifiedID(parameter, name, source)
				if counts[id] == 0 {
					declarations = append(declarations, declaration{node: parameter, id: id, value: nil})
					counts[id]++
				}
			}
		}
	})
	// A loop, with, except, or unpacking target binds its name in the
	// enclosing scope, so it hides an outer constant from there on, and a
	// constant that it rebinds is read-only.
	code.Walk(root, func(node *sitter.Node) {
		for _, target := range pythonBindingTargets(node) {
			for _, name := range pythonBoundNames(target, source) {
				id := pythonBindingID(target, name, source)
				declarations = append(declarations, declaration{node: target, id: id, value: nil})
				counts[id]++
			}
		}
	})
	// The declarations are read in source order, so a parameter hides a name
	// before the body of its function reads it.
	slices.SortStableFunc(declarations, func(left, right declaration) int {
		return cmp.Compare(left.node.StartByte(), right.node.StartByte())
	})
	// An import binds a name in its scope as a declaration does, so a
	// mutation of it is recorded under the same scoped id.
	declared := func(id string) bool { return counts[id] != 0 || imported[id] != nil }
	mutated := map[string]bool{}
	code.Walk(root, func(node *sitter.Node) {
		switch node.Kind() {
		case "assignment", "augmented_assignment", "named_expression":
			// An assignment or a walrus to a variable is a declaration,
			// which counts records. Every other write mutates a receiver
			// chain.
			if declared, _ := pythonDeclaredVariable(node); declared != nil {
				return
			}
			name := node.ChildByFieldName("left")
			if name == nil {
				return
			}
			for _, target := range pythonWriteTargets(name) {
				// A variable of an unpacking is bound, not mutated.
				if target.Kind() == "identifier" && node.Kind() == "assignment" {
					continue
				}
				if id := pythonMutatedName(node, code.Text(target, source), source, declared); id != "" {
					mutated[id] = true
				}
			}
		case "call":
			function := node.ChildByFieldName("function")
			if function == nil {
				return
			}
			owner, method, ok := code.SplitMethod(code.Text(function, source))
			if !ok || !pythonMutatingMethods[method] {
				return
			}
			if id := pythonMutatedName(node, owner, source, declared); id != "" {
				mutated[id] = true
			}
		case "delete_statement":
			// `del ITEMS[0]` and `del CONFIG.key` mutate the collection, and
			// `del NAME` unbinds the name, so the constant has no one value.
			for _, target := range code.NamedChildren(node) {
				for _, item := range pythonDeleteTargets(target) {
					if id := pythonMutatedName(node, code.Text(item, source), source, declared); id != "" {
						mutated[id] = true
					}
				}
			}
		}
	})
	// An alias shares its object with its origin: `ALSO = ITEMS` binds the
	// same list, and two imports of one value bind the same object. A
	// mutation through any name of the object freezes every name.
	pythonSpreadMutations(mutated, declarations, imported, source, declared)

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
	// An import that this file mutates is read-only here, as a local
	// constant is, and the read-only copy shadows the import.
	for name, value := range imported {
		if mutated[name] && value != nil {
			frozen := value.Clone()
			frozen.MarkReadOnly("mutated constants are read-only")
			environment[name], values[name] = frozen, frozen
		}
	}
	// A name that a function binds anywhere is local in the whole function,
	// so a use of it before the binding is unbound, not the outer constant.
	// An entry without a value hides the outer name until the binding gives
	// it one. A class body and the module run in order, so they keep the
	// outer name until the binding.
	for _, item := range declarations {
		if _, bound := environment[item.id]; !bound && pythonFunctionLocal(item.node, source) {
			environment[item.id] = nil
		}
	}
	var resources []*adapter.Node
	fallbacks := code.NewFallbackIDs()
	for _, item := range declarations {
		// A declaration of an imported name shadows the import from here on,
		// and the import stays visible to the uses before it: a module-level
		// or a function-local import of the same scope, or the module-level
		// import that a class body sees until it binds the name itself. A
		// loop, with, except, or unpacking target shadows it the same way.
		shadowed := imported[item.id]
		if shadowed == nil && pythonInClassBody(item.node) {
			if bare := item.id[strings.LastIndex(item.id, ".")+1:]; bare != item.id {
				shadowed = imported[bare]
			}
		}
		code.RecordShadowedImport(values, item.id, shadowed, int(item.node.StartByte()))
		if item.value == nil {
			// A parameter or a target has no value and hides an outer name.
			values[item.id], environment[item.id] = nil, nil
			continue
		}
		value := pythonValue(item.value, source, environment)
		if !code.IsLiteral(value) {
			// The nil entries shadow an import of the same name here and in
			// every later literal.
			values[item.id], environment[item.id] = nil, nil
			continue
		}
		if counts[item.id] == 1 && !mutated[item.id] {
			environment[item.id] = value
		}
		values[item.id] = value
		code.RecordDeclaration(values, item.id, int(item.node.EndByte()))
		resource := code.Constant(item.id, value)
		resources = append(resources, resource)
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
		// The positional id comes last, so that no read-only mark replaces it.
		if counts[item.id] != 1 {
			fallbacks.Distinguish(resource, item.id)
		}
	}
	return values, resources
}

// pythonDeleteTargets returns each target of a delete statement: the target
// itself, or each item of a tuple or list of targets.
func pythonDeleteTargets(target *sitter.Node) []*sitter.Node {
	switch target.Kind() {
	case "expression_list", "tuple", "list", "parenthesized_expression":
		var targets []*sitter.Node
		for _, child := range code.NamedChildren(target) {
			targets = append(targets, pythonDeleteTargets(child)...)
		}
		return targets
	}
	return []*sitter.Node{target}
}

// pythonSpreadMutations marks every name that shares an object with a
// mutated name: a declaration that binds a bare name to another constant,
// and the imports that bind one value of another file.
func pythonSpreadMutations(
	mutated map[string]bool,
	declarations []pythonDeclaration,
	imported map[string]*adapter.Node,
	source []byte,
	declared func(string) bool,
) {
	shared := map[string][]string{}
	link := func(left, right string) {
		shared[left] = append(shared[left], right)
		shared[right] = append(shared[right], left)
	}
	for _, item := range declarations {
		if item.value == nil || item.value.Kind() != "identifier" {
			continue
		}
		origin := pythonScopedName(item.value, code.Text(item.value, source), source, declared)
		if origin != item.id && declared(origin) {
			link(item.id, origin)
		}
	}
	byValue := map[*adapter.Node][]string{}
	for name, value := range imported {
		if value != nil && value.Code == "" {
			byValue[value] = append(byValue[value], name)
		}
	}
	for _, names := range byValue {
		for _, name := range names[1:] {
			link(names[0], name)
		}
	}
	queue := slices.Collect(maps.Keys(mutated))
	for len(queue) != 0 {
		name := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		for _, other := range shared[name] {
			if !mutated[other] {
				mutated[other] = true
				queue = append(queue, other)
			}
		}
	}
}

// pythonLocalBinding reports whether a name is a parameter of a lambda
// around it, or a target of a comprehension around it. Both bind the name
// for the expression only, so the name is a value of the call there.
func pythonLocalBinding(node *sitter.Node, name string, source []byte) bool {
	for parent := node.Parent(); parent != nil; parent = parent.Parent() {
		switch parent.Kind() {
		case "function_definition", "class_definition":
			return false
		case "lambda":
			for _, parameter := range code.NamedChildren(parent.ChildByFieldName("parameters")) {
				if pythonParameterName(parameter, source) == name {
					return true
				}
			}
		case "list_comprehension", "set_comprehension", "dictionary_comprehension", "generator_expression":
			for _, clause := range code.NamedChildren(parent) {
				if clause.Kind() == "for_in_clause" && slices.Contains(pythonBoundNames(clause.ChildByFieldName("left"), source), name) {
					return true
				}
			}
		}
	}
	return false
}

// pythonFunctionLocal reports whether the innermost scope of a node is a
// function, where a binding is local in the whole function.
func pythonFunctionLocal(node *sitter.Node, source []byte) bool {
	chain := pythonScopeChain(node, source)
	return len(chain) != 0 && !chain[len(chain)-1].class
}

// pythonInExpressionScope reports whether a lambda or a comprehension
// encloses a node within its function or class, or within the module.
func pythonInExpressionScope(node *sitter.Node) bool {
	for parent := node.Parent(); parent != nil; parent = parent.Parent() {
		switch parent.Kind() {
		case "function_definition", "class_definition":
			return false
		case "lambda", "list_comprehension", "set_comprehension", "dictionary_comprehension", "generator_expression":
			return true
		}
	}
	return false
}

// pythonInLambda reports whether a lambda encloses a node within its
// function or class, or within the module. The body of a lambda runs when
// the lambda is called, after the code around it.
func pythonInLambda(node *sitter.Node) bool {
	for parent := node.Parent(); parent != nil; parent = parent.Parent() {
		switch parent.Kind() {
		case "function_definition", "class_definition":
			return false
		case "lambda":
			// A default of the lambda runs with the definition.
			if !pythonInParameterDefault(node, parent) {
				return true
			}
		}
	}
	return false
}

// pythonBindingTargets returns the patterns that a statement binds in its
// scope besides an assignment to one variable: the target of a for loop or
// of an unpacking assignment, the alias of a with item or an except clause,
// and the captures of a case pattern.
func pythonBindingTargets(node *sitter.Node) []*sitter.Node {
	switch node.Kind() {
	case "for_statement":
		if left := node.ChildByFieldName("left"); left != nil {
			return []*sitter.Node{left}
		}
	case "assignment":
		if left := node.ChildByFieldName("left"); left != nil && left.Kind() != "identifier" {
			return []*sitter.Node{left}
		}
	case "with_item", "except_clause":
		if value := node.ChildByFieldName("value"); value != nil && value.Kind() == "as_pattern" {
			if alias := value.ChildByFieldName("alias"); alias != nil {
				return []*sitter.Node{alias}
			}
		}
	case "case_clause":
		// A case pattern captures names for the clause and the scope.
		var patterns []*sitter.Node
		for _, child := range code.NamedChildren(node) {
			if child.Kind() == "case_pattern" {
				patterns = append(patterns, child)
			}
		}
		return patterns
	}
	return nil
}

// pythonWriteTargets returns the targets that an assignment writes: the
// target itself, or each target of a tuple or list pattern.
func pythonWriteTargets(pattern *sitter.Node) []*sitter.Node {
	switch pattern.Kind() {
	case "pattern_list", "tuple_pattern", "list_pattern", "parenthesized_expression":
		var targets []*sitter.Node
		for _, child := range code.NamedChildren(pattern) {
			targets = append(targets, pythonWriteTargets(child)...)
		}
		return targets
	}
	return []*sitter.Node{pattern}
}

// pythonBoundNames returns the names that a target pattern binds: the
// identifier, or every identifier of a tuple or list pattern. An attribute
// or a subscript target binds no name.
func pythonBoundNames(pattern *sitter.Node, source []byte) []string {
	if pattern == nil {
		return nil
	}
	children := code.NamedChildren(pattern)
	switch pattern.Kind() {
	case "identifier":
		return []string{code.Text(pattern, source)}
	case "pattern_list", "tuple_pattern", "list_pattern", "tuple", "list", "as_pattern_target",
		"list_splat_pattern", "parenthesized_expression",
		"case_pattern", "as_pattern", "splat_pattern", "union_pattern":
		var names []string
		for _, child := range children {
			names = append(names, pythonBoundNames(child, source)...)
		}
		return names
	case "dotted_name":
		// A bare name in a case pattern captures the subject. A dotted
		// name, or the wildcard, matches a value and binds nothing.
		if len(children) == 1 && children[0].Kind() == "identifier" && code.Text(children[0], source) != "_" {
			return []string{code.Text(children[0], source)}
		}
		return nil
	case "class_pattern", "keyword_pattern":
		// The first child names the class or the keyword, not a capture.
		var names []string
		for _, child := range children[min(1, len(children)):] {
			names = append(names, pythonBoundNames(child, source)...)
		}
		return names
	case "dict_pattern":
		var names []string
		for _, child := range children {
			if child.Kind() == "case_pattern" || child.Kind() == "splat_pattern" {
				names = append(names, pythonBoundNames(child, source)...)
			}
		}
		return names
	}
	return nil
}

// pythonParameterName returns the name that a function parameter binds, or
// an empty string for self and other forms without a name.
func pythonParameterName(parameter *sitter.Node, source []byte) string {
	switch parameter.Kind() {
	case "identifier":
		return code.Text(parameter, source)
	case "default_parameter", "typed_default_parameter":
		if name := parameter.ChildByFieldName("name"); name != nil && name.Kind() == "identifier" {
			return code.Text(name, source)
		}
	case "typed_parameter", "list_splat_pattern", "dictionary_splat_pattern":
		name := parameter.NamedChild(0)
		switch {
		case name == nil:
			return ""
		case name.Kind() == "identifier":
			return code.Text(name, source)
		case name.Kind() == "list_splat_pattern" || name.Kind() == "dictionary_splat_pattern":
			// `*args: T` and `**kwargs: T` wrap the splat in the type.
			return pythonParameterName(name, source)
		}
	}
	return ""
}

// pythonMutatingMethods are the list, dictionary, and set methods that
// change their receiver in place.
var pythonMutatingMethods = map[string]bool{
	"append": true, "extend": true, "insert": true, "remove": true, "pop": true, "clear": true,
	"sort": true, "reverse": true, "update": true, "setdefault": true, "popitem": true,
	"add": true, "discard": true,
}

// pythonChainRoot splits a receiver chain at its first attribute or
// subscript, so that SETTINGS["values"].update gives SETTINGS and
// self.items[0] gives self and items[0].
func pythonChainRoot(chain string) (root, rest string) {
	index := strings.IndexAny(chain, ".[")
	if index < 0 {
		return chain, ""
	}
	rest = chain[index:]
	if rest[0] == '.' {
		rest = rest[1:]
	}
	return chain[:index], rest
}

// pythonMutatedName returns the id of the constant that a write through a
// receiver chain mutates: the root of items.append, SETTINGS["values"].update,
// or items[0] = x, and the class attribute of self.items.append or
// self.items[0] = x. The chain is the receiver of a call or the target of an
// assignment. An empty chain names nothing.
func pythonMutatedName(node *sitter.Node, chain string, source []byte, declared func(string) bool) string {
	rootName, rest := pythonChainRoot(strings.TrimSpace(chain))
	if rootName == "" {
		return ""
	}
	if attribute, _ := pythonChainRoot(rest); attribute != "" {
		if rootName == "self" || rootName == "cls" {
			return strings.Join(append(pythonClassScopes(node, source), attribute), ".")
		}
		// A write through the class name, as in Bot.items.append(...),
		// mutates the class attribute when the file declares it.
		if candidate := pythonScopedName(node, rootName+"."+attribute, source, declared); declared(candidate) {
			return candidate
		}
	}
	return pythonScopedName(node, rootName, source, declared)
}

// pythonConstant returns a copy of the constant that a use of name at a byte
// offset refers to, or nil when none is in scope. The copy keeps a later
// change to one use, such as a read-only mark, away from the other uses.
func pythonConstant(node *sitter.Node, name string, use int, source []byte, constants map[string]*adapter.Node) *adapter.Node {
	if constants == nil {
		return nil
	}
	declared := func(id string) bool { _, ok := constants[id]; return ok }
	scoped := pythonScopedName(node, name, source, declared)
	value := constants[scoped]
	if value == nil {
		return code.ShadowedImport(constants, scoped, use)
	}
	deferred := pythonDeferredUse(node, scoped, source)
	// An import binds its name from its statement on, so a use above it in
	// the same scope is unbound. A function body runs later and sees it.
	if value.File != "" && !deferred && !code.ImportBound(constants, scoped, use) {
		return nil
	}
	if code.DeclaredAt(constants, scoped, value, use) || deferred {
		return value.Clone()
	}
	return code.ShadowedImport(constants, scoped, use)
}

// pythonDeferredUse reports whether a use runs after the scope of the
// constant finished: a function body or a lambda between the scope of the
// constant and the use runs later, so it sees a constant that the file
// declares after it.
func pythonDeferredUse(node *sitter.Node, scoped string, source []byte) bool {
	// The scopes between the scope of the constant and the use.
	remaining := len(pythonScopeChain(node, source)) - strings.Count(scoped, ".")
	for parent := node.Parent(); parent != nil; parent = parent.Parent() {
		switch parent.Kind() {
		case "lambda":
			if pythonInParameterDefault(node, parent) {
				// A default of a lambda runs with the definition too.
				continue
			}
			return true
		case "function_definition", "class_definition":
			if parent.Kind() == "function_definition" && pythonInParameterDefault(node, parent) {
				// A default runs with the definition, not with the body.
				continue
			}
			if remaining <= 0 {
				return false
			}
			if parent.Kind() == "function_definition" {
				return true
			}
			remaining--
		}
	}
	return false
}

// pythonInParameterDefault reports whether a node sits in the default value
// of a parameter of the function or the lambda.
func pythonInParameterDefault(node, function *sitter.Node) bool {
	for _, parameter := range code.NamedChildren(function.ChildByFieldName("parameters")) {
		if parameter.Kind() != "default_parameter" && parameter.Kind() != "typed_default_parameter" {
			continue
		}
		value := parameter.ChildByFieldName("value")
		if value != nil && node.StartByte() >= value.StartByte() && node.EndByte() <= value.EndByte() {
			return true
		}
	}
	return false
}

// pythonInClassBody reports whether a node sits directly in a class body,
// where code runs in order like module-level code.
func pythonInClassBody(node *sitter.Node) bool {
	for parent := node.Parent(); parent != nil; parent = parent.Parent() {
		switch parent.Kind() {
		case "class_definition":
			return true
		case "function_definition":
			return false
		}
	}
	return false
}

// pythonScopedName returns the id that a use of name refers to: the constant
// of the nearest enclosing function or class that declares it, else the
// module-level name. declared reports whether an id has a constant. A bare
// name inside a method does not see the attributes of the class, because
// Python resolves it through the enclosing functions and then the module.
// An attribute chain, such as self.client, belongs to the class.
func pythonScopedName(node *sitter.Node, name string, source []byte, declared func(string) bool) string {
	scopes := pythonScopeChain(node, source)
	bare := !strings.Contains(name, ".")
	// A lambda or a comprehension is a scope of its own, so the class body
	// around it is not the innermost scope of a bare name inside it.
	nested := bare && pythonInExpressionScope(node)
	for end := len(scopes); end > 0; end-- {
		if bare && (end < len(scopes) || nested) && scopes[end-1].class {
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

// pythonBindingID returns the qualified id of the scope that a binding of
// name at node writes. A `global` statement in the function moves the
// binding to the module, and a `nonlocal` statement moves it to the nearest
// enclosing function.
func pythonBindingID(node *sitter.Node, name string, source []byte) string {
	scopes := pythonScopeChain(node, source)
	depth := len(scopes)
	nonlocal := false
	for parent := node.Parent(); parent != nil && depth != 0; parent = parent.Parent() {
		if parent.Kind() != "function_definition" && parent.Kind() != "class_definition" {
			continue
		}
		if parent.Kind() == "function_definition" && pythonInParameterDefault(node, parent) {
			continue
		}
		depth--
		if parent.Kind() == "class_definition" {
			if nonlocal {
				// A nonlocal name skips the class bodies between the
				// functions.
				continue
			}
		} else {
			switch pythonScopeStatement(parent, name, source) {
			case "global_statement":
				return name
			case "nonlocal_statement":
				nonlocal = true
				continue
			}
		}
		names := make([]string, 0, depth+2)
		for _, scope := range scopes[:depth+1] {
			names = append(names, scope.name)
		}
		return strings.Join(append(names, name), ".")
	}
	return name
}

// pythonScopeStatement returns the kind of the global or nonlocal statement
// of a function that names name, or an empty string. A statement in a
// nested function, class, or lambda belongs to that scope.
func pythonScopeStatement(function *sitter.Node, name string, source []byte) string {
	kind := ""
	var visit func(node *sitter.Node)
	visit = func(node *sitter.Node) {
		if kind != "" {
			return
		}
		switch node.Kind() {
		case "function_definition", "class_definition", "lambda":
			return
		case "global_statement", "nonlocal_statement":
			for _, child := range code.NamedChildren(node) {
				if child.Kind() == "identifier" && code.Text(child, source) == name {
					kind = node.Kind()
					return
				}
			}
			return
		}
		for _, child := range code.NamedChildren(node) {
			visit(child)
		}
	}
	visit(function.ChildByFieldName("body"))
	return kind
}

// pythonScope is one class or function that encloses a node.
type pythonScope struct {
	name  string
	class bool
}

// pythonScopeChain returns the classes and functions that enclose a node,
// outermost first.
func pythonScopeChain(node *sitter.Node, source []byte) []pythonScope {
	var scopes []pythonScope
	for parent := node.Parent(); parent != nil; parent = parent.Parent() {
		if parent.Kind() != "function_definition" && parent.Kind() != "class_definition" {
			continue
		}
		// A parameter default runs when the function is defined, in the
		// enclosing scope, so the function is not one of its scopes.
		if parent.Kind() == "function_definition" && pythonInParameterDefault(node, parent) {
			continue
		}
		if scope := parent.ChildByFieldName("name"); scope != nil {
			scopes = append([]pythonScope{{
				name: code.Text(scope, source), class: parent.Kind() == "class_definition",
			}}, scopes...)
		}
	}
	return scopes
}

// pythonQualifiedID prefixes a name with every enclosing class and function,
// outermost first, so that two methods with one name in two classes keep
// their locals apart.
func pythonQualifiedID(node *sitter.Node, name string, source []byte) string {
	return strings.Join(append(pythonScopes(node, source), name), ".")
}

// pythonScopes returns the names of the classes and functions that enclose
// a node, outermost first.
func pythonScopes(node *sitter.Node, source []byte) []string {
	chain := pythonScopeChain(node, source)
	scopes := make([]string, len(chain))
	for index, scope := range chain {
		scopes[index] = scope.name
	}
	return scopes
}

func pythonKeywordFields(call *sitter.Node, source []byte, constants map[string]*adapter.Node) map[string]*adapter.Node {
	fields := map[string]*adapter.Node{}
	arguments := call.ChildByFieldName("arguments")
	for _, argument := range code.NamedChildren(arguments) {
		if argument.Kind() != "keyword_argument" {
			continue
		}
		name := argument.ChildByFieldName("name")
		value := argument.ChildByFieldName("value")
		if name == nil || value == nil {
			continue
		}
		key := code.Text(name, source)
		if value.Kind() == "call" && key == "model_settings" &&
			strings.HasSuffix(code.Text(value.ChildByFieldName("function"), source), "ModelSettings") {
			fields[key] = &adapter.Node{Fields: pythonKeywordFields(value, source, constants)}
			continue
		}
		if key == "messages" {
			fields[key] = pythonMessages(value, source, constants)
			continue
		}
		fields[key] = pythonValue(value, source, constants)
	}
	return fields
}

func pythonValue(node *sitter.Node, source []byte, constants map[string]*adapter.Node) *adapter.Node {
	if node == nil {
		return adapter.ReadOnly(0, 0, "value is missing")
	}
	start, end := int(node.StartByte()), int(node.EndByte())
	switch node.Kind() {
	case "parenthesized_expression":
		if inner := code.NamedChildren(node); len(inner) == 1 {
			return pythonValue(inner[0], source, constants)
		}
	case "assignment":
		// `a = b = "text"` nests the second assignment as the value of the first.
		if right := node.ChildByFieldName("right"); right != nil {
			return pythonValue(right, source, constants)
		}
	case "string":
		value, quote, err := code.DecodePythonString(source[start:end])
		if err != nil {
			return adapter.ReadOnly(start, end, err.Error())
		}
		return adapter.String(value, start, end, func(text string) ([]byte, error) {
			return code.MatchLineEndings(code.EncodePythonString(text, quote), source[start:end]), nil
		})
	case "concatenated_string":
		// Adjacent literals are one string. A write replaces them all with
		// one literal in the quote of the first part.
		var value, quote string
		for _, part := range code.NamedChildren(node) {
			text, partQuote, err := code.DecodePythonString([]byte(code.Text(part, source)))
			if part.Kind() != "string" || err != nil {
				return adapter.ReadOnly(start, end, "string concatenation is not static")
			}
			if quote == "" {
				quote = partQuote
			}
			value += text
		}
		return adapter.String(value, start, end, func(text string) ([]byte, error) {
			return code.MatchLineEndings(code.EncodePythonString(text, quote), source[start:end]), nil
		})
	case "integer", "float", "unary_operator":
		value, reason := pythonNumber(node, source)
		if reason != "" {
			return adapter.ReadOnly(start, end, reason)
		}
		return adapter.Number(value, start, end, func(number float64) ([]byte, error) {
			return []byte(strconv.FormatFloat(number, 'g', -1, 64)), nil
		})
	case "true", "false":
		return adapter.Bool(node.Kind() == "true", start, end, encodePythonBool)
	case "identifier":
		switch string(source[start:end]) {
		case "True", "False":
			return adapter.Bool(string(source[start:end]) == "True", start, end, encodePythonBool)
		}
		if constants != nil {
			// A lambda parameter or a comprehension target is a value of
			// the call, not a constant.
			if pythonLocalBinding(node, string(source[start:end]), source) {
				return adapter.ReadOnly(start, end, "dynamic expression has no safe inverse")
			}
			if value := pythonConstant(node, string(source[start:end]), start, source, constants); value != nil {
				return value
			}
			declared := func(id string) bool { _, ok := constants[id]; return ok }
			if wildcard := constants[pythonScopedName(node, "*", source, declared)]; wildcard != nil {
				value := *wildcard
				value.Reason = fmt.Sprintf("star import cannot resolve %s statically", code.Text(node, source))
				return &value
			}
		}
	case "none":
		return &adapter.Node{Start: start, End: end, Capability: adapter.CapabilityDirect, Encode: code.PythonLiteral}
	case "dictionary":
		return pythonDictionary(node, source, constants)
	case "list", "tuple":
		return pythonSequence(node, source, constants)
	}
	return adapter.ReadOnly(start, end, "dynamic expression has no safe inverse")
}

// pythonNumber reads an integer or float literal, with a sign, digit
// separators, and a base prefix, as a float64. A value that it cannot read
// comes with the read-only reason: a sign on something other than a literal
// is a dynamic expression, and an integer beyond 53 bits has no exact
// float64, so it is not representable.
func pythonNumber(node *sitter.Node, source []byte) (float64, string) {
	const dynamic, notRepresentable = "dynamic expression has no safe inverse", "number is not representable"
	literal := node
	sign := 1.0
	if node.Kind() == "unary_operator" {
		operand := node.ChildByFieldName("argument")
		if operand == nil || operand.Kind() != "integer" && operand.Kind() != "float" {
			return 0, dynamic
		}
		text := code.Text(node, source)
		if strings.HasPrefix(text, "-") {
			sign = -1
		} else if !strings.HasPrefix(text, "+") {
			return 0, dynamic
		}
		literal = operand
	}
	text := strings.ReplaceAll(code.Text(literal, source), "_", "")
	if literal.Kind() == "integer" {
		integer, err := strconv.ParseInt(strings.ToLower(text), 0, 64)
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

func pythonDictionary(node *sitter.Node, source []byte, constants map[string]*adapter.Node) *adapter.Node {
	start, end := int(node.StartByte()), int(node.EndByte())
	object := map[string]any{}
	fields := map[string]*adapter.Node{}
	complete := true
	for _, item := range code.NamedChildren(node) {
		// A splat or a comprehension has no static entry.
		if item.Kind() != "pair" {
			fields[code.Text(item, source)] = adapter.ReadOnly(
				int(item.StartByte()), int(item.EndByte()), "dictionary entry is dynamic",
			)
			complete = false
			continue
		}
		keyNode := item.ChildByFieldName("key")
		valueNode := item.ChildByFieldName("value")
		// A write rewrites the dictionary as a literal with string keys, so a
		// computed or non-string key makes it read-only.
		text, ok := code.StringValue(pythonValue(keyNode, source, constants))
		if !ok {
			text = code.Text(keyNode, source)
			complete = false
		}
		value := pythonValue(valueNode, source, constants)
		fields[text] = value
		if code.Writable(value) {
			object[text] = value.Value
		} else {
			complete = false
		}
	}
	result := &adapter.Node{
		Value: object, Fields: fields, Start: start, End: end, Capability: adapter.CapabilityDirect,
		Encode: code.PythonLiteral,
	}
	if !complete {
		result.Value = nil
		result.Capability = adapter.CapabilityReadOnly
		result.Reason = "dictionary contains a dynamic entry"
		result.Encode = nil
	}
	return result
}

func pythonSequence(node *sitter.Node, source []byte, constants map[string]*adapter.Node) *adapter.Node {
	start, end := int(node.StartByte()), int(node.EndByte())
	values := make([]any, 0, node.NamedChildCount())
	items := make([]*adapter.Node, 0, node.NamedChildCount())
	complete := true
	for _, item := range code.NamedChildren(node) {
		value := pythonValue(item, source, constants)
		items = append(items, value)
		values = append(values, value.Value)
		if !code.Writable(value) {
			complete = false
		}
	}
	result := &adapter.Node{
		Value: values, Items: items, Collection: true, Start: start, End: end, Capability: adapter.CapabilityDirect,
		Encode: code.PythonLiteral,
	}
	if node.Kind() == "tuple" {
		// A tuple is written back as a tuple, so its type does not change.
		result.Encode = code.PythonTuple
	}
	if !complete {
		result.Value = nil
		result.Capability = adapter.CapabilityReadOnly
		result.Reason = "collection contains dynamic values"
		result.Encode = nil
	}
	return result
}

func pythonMessages(node *sitter.Node, source []byte, constants map[string]*adapter.Node) *adapter.Node {
	node = pythonUnwrap(node)
	if node != nil && node.Kind() == "identifier" {
		constant := pythonConstant(node, code.Text(node, source), int(node.StartByte()), source, constants)
		if messages := code.MessageList(constant); messages != nil {
			return messages
		}
	}
	if node == nil || node.Kind() != "list" && node.Kind() != "tuple" {
		if node == nil {
			return adapter.ReadOnly(0, 0, "messages are missing")
		}
		return adapter.ReadOnly(int(node.StartByte()), int(node.EndByte()), "messages are not a literal list")
	}
	start, end := int(node.StartByte()), int(node.EndByte())
	var items []*adapter.Node
	var messages []syncdomain.Message
	complete := true
	for _, child := range code.NamedChildren(node) {
		item := pythonMessage(child, source, constants)
		items = append(items, item)
		role, roleOK := item.Fields["role"].Value.(string)
		content, contentOK := item.Fields["content"].Value.(string)
		if !roleOK || !contentOK || !code.Writable(item.Fields["content"]) {
			complete = false
			continue
		}
		role = code.NormalizeRole(role)
		messages = append(messages, syncdomain.Message{Role: role, Content: content})
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

func pythonMessage(node *sitter.Node, source []byte, constants map[string]*adapter.Node) *adapter.Node {
	fields := map[string]*adapter.Node{}
	switch node.Kind() {
	case "tuple", "list":
		children := code.NamedChildren(node)
		if len(children) == 2 {
			fields["role"] = pythonValue(children[0], source, constants)
			fields["content"] = pythonValue(children[1], source, constants)
		}
	case "dictionary":
		dictionary := pythonDictionary(node, source, constants)
		fields = dictionary.Fields
	case "call":
		function := code.Text(node.ChildByFieldName("function"), source)
		role := ""
		switch {
		case strings.HasSuffix(function, "SystemMessage"):
			role = syncdomain.RoleSystem
		case strings.HasSuffix(function, "HumanMessage"):
			role = syncdomain.RoleUser
		case strings.HasSuffix(function, "AIMessage"):
			role = syncdomain.RoleAssistant
		}
		if role != "" {
			fields["role"] = &adapter.Node{Value: role}
			arguments := pythonPositionalArguments(node)
			if len(arguments) != 0 {
				fields["content"] = pythonValue(arguments[0], source, constants)
			} else if content := pythonKeywordFields(node, source, constants)["content"]; content != nil {
				fields["content"] = content
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

func pythonDecoratedCollection(
	node *sitter.Node,
	aliases pythonAliases,
	instances map[string]string,
	source []byte,
) string {
	kinds := aliases.at(node, source)
	for _, child := range code.NamedChildren(node) {
		if child.Kind() != "decorator" || child.NamedChildCount() == 0 {
			continue
		}
		value := child.NamedChild(0)
		if value.Kind() == "call" {
			value = value.ChildByFieldName("function")
		}
		if value == nil {
			continue
		}
		callable := code.Text(value, source)
		if kind := kinds[callable]; kind == "tool-decorator" || kind == "kernel-tool-decorator" {
			return "tools"
		}
		kind, method := code.Receiver(callable, func(receiver string) string {
			return pythonInstanceKind(instances, node, receiver, source)
		})
		switch kind {
		case "fastmcp":
			if method == "tool" {
				return "tools"
			}
			if method == "prompt" {
				return "prompts"
			}
		case "pydantic-agent":
			if method == "tool" {
				return "tools"
			}
			if method == "system_prompt" || method == "instructions" {
				return "prompts"
			}
		}
	}
	return ""
}

func encodePythonBool(value bool) ([]byte, error) {
	if value {
		return []byte("True"), nil
	}
	return []byte("False"), nil
}
