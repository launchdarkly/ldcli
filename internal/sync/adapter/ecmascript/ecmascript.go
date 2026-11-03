// Package ecmascript inspects TypeScript and JavaScript sources. It finds
// agents, prompts, tools, and constants through an import allowlist and never
// runs the code.
package ecmascript

import (
	"path"
	"regexp"
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
	language := sitter.NewLanguage(typescript.LanguageTypescript())
	if !adapter.typed {
		language = sitter.NewLanguage(javascript.Language())
	}
	tree, err := code.ParseTree(document, language)
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

func parseTypeScript(document adapter.Document, typed bool, imported map[string]*adapter.Node) (*adapter.Node, error) {
	language := sitter.NewLanguage(typescript.LanguageTypescript())
	if !typed {
		language = sitter.NewLanguage(javascript.Language())
	}
	tree, err := code.ParseTree(document, language)
	if err != nil {
		return nil, err
	}
	defer tree.Close()

	imports := typeScriptImports(tree.RootNode(), document.Data)
	servers := typeScriptServers(tree.RootNode(), document.Data, imports)
	instances := typeScriptInstances(tree.RootNode(), document.Data, imports)
	constants, constantResources := typeScriptConstants(tree.RootNode(), document.Data)
	for name, value := range imported {
		constants[name] = value
	}
	collections := map[string][]*adapter.Node{"agents": {}, "prompts": {}, "tools": {}, "constants": {}}
	collections["constants"] = constantResources
	code.Walk(tree.RootNode(), func(node *sitter.Node) {
		if node.Kind() != "call_expression" {
			return
		}
		function := node.ChildByFieldName("function")
		if function == nil || function.Kind() != "member_expression" {
			return
		}
		receiver := function.ChildByFieldName("object")
		method := function.ChildByFieldName("property")
		if receiver == nil || method == nil || !servers[code.Text(receiver, document.Data)] ||
			code.Text(method, document.Data) != "registerTool" {
			return
		}
		arguments := code.NamedChildren(node.ChildByFieldName("arguments"))
		if len(arguments) < 2 {
			return
		}
		idNode := typeScriptValue(arguments[0], document.Data, constants)
		id, ok := idNode.Value.(string)
		if !ok {
			return
		}
		fields := typeScriptObject(arguments[1], document.Data, constants)
		collections["tools"] = append(collections["tools"], adapter.Resource(id, fields.Fields))
	})
	code.Walk(tree.RootNode(), func(node *sitter.Node) {
		if node.Kind() != "variable_declarator" {
			return
		}
		name := node.ChildByFieldName("name")
		value := node.ChildByFieldName("value")
		if name == nil || value == nil {
			return
		}
		id := typeScriptQualifiedID(node, code.Text(name, document.Data), document.Data)
		for _, call := range typeScriptPromptCalls(value, imports, document.Data) {
			fields := typeScriptPromptFields(call, imports, document.Data, constants)
			collections["prompts"] = append(collections["prompts"], adapter.Resource(id, fields))
		}
		if collection, resource, ok := typeScriptFrameworkCall(
			value, id, imports, instances, document.Data, constants,
		); ok {
			collections[collection] = append(collections[collection], resource)
		}
		switch value.Kind() {
		case "object":
			if id != "tools" || imports["tool"] != "vercel-tool" {
				return
			}
			for key, child := range typeScriptObject(value, document.Data, constants).Fields {
				call := child.Fields["call"]
				if call != nil && call.Fields != nil {
					collections["tools"] = append(collections["tools"], adapter.Resource(key, call.Fields))
				}
			}
		}
	})
	code.Walk(tree.RootNode(), func(node *sitter.Node) {
		if node.Kind() != "call_expression" || typeScriptAssignedCall(node) {
			return
		}
		if collection, resource, ok := typeScriptFrameworkCall(
			node, "", imports, instances, document.Data, constants,
		); ok {
			collections[collection] = append(collections[collection], resource)
		}
	})
	return adapter.Collections(collections), nil
}

func typeScriptAssignedCall(call *sitter.Node) bool {
	for parent := call.Parent(); parent != nil; parent = parent.Parent() {
		if parent.Kind() == "variable_declarator" {
			return true
		}
		if parent.Kind() == "expression_statement" || parent.Kind() == "return_statement" {
			return false
		}
	}
	return false
}

func typeScriptConstants(root *sitter.Node, source []byte) (map[string]*adapter.Node, []*adapter.Node) {
	counts := map[string]int{}
	values := map[string]*adapter.Node{}
	var resources []*adapter.Node
	code.Walk(root, func(node *sitter.Node) {
		if node.Kind() != "variable_declarator" {
			return
		}
		name := node.ChildByFieldName("name")
		valueNode := node.ChildByFieldName("value")
		if name == nil || valueNode == nil || name.Kind() != "identifier" {
			return
		}
		id := typeScriptQualifiedID(node, code.Text(name, source), source)
		value := typeScriptValue(valueNode, source, nil)
		if value.Value == nil && value.Fields == nil && value.Items == nil {
			return
		}
		counts[id]++
		values[id] = value
		resources = append(resources, code.Constant(id, value))
	})
	mutated := map[string]bool{}
	code.Walk(root, func(node *sitter.Node) {
		switch node.Kind() {
		case "assignment_expression", "augmented_assignment_expression":
			left := node.ChildByFieldName("left")
			if rootName := typeScriptMutationRoot(left, source); rootName != "" {
				mutated[typeScriptQualifiedID(node, rootName, source)] = true
			}
		case "call_expression":
			function := node.ChildByFieldName("function")
			if function == nil {
				return
			}
			text := code.Text(function, source)
			rootName, method, ok := strings.Cut(text, ".")
			if ok && (method == "push" || method == "splice" || method == "pop" ||
				method == "shift" || method == "unshift" || method == "sort" || method == "reverse") {
				mutated[typeScriptQualifiedID(node, rootName, source)] = true
			}
		}
	})
	for id, count := range counts {
		if count != 1 || mutated[id] {
			values[id] = nil
		}
	}
	for _, resource := range resources {
		id := resource.Fields["id"].Value.(string)
		if mutated[id] {
			resource.MarkReadOnly("mutated constants are read-only")
		}
	}
	return values, resources
}

func typeScriptMutationRoot(node *sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	text := code.Text(node, source)
	if index := strings.IndexAny(text, ".["); index >= 0 {
		text = text[:index]
	}
	return strings.TrimSpace(text)
}

func typeScriptQualifiedID(node *sitter.Node, name string, source []byte) string {
	for parent := node.Parent(); parent != nil; parent = parent.Parent() {
		switch parent.Kind() {
		case "function_declaration", "method_definition", "class_declaration":
			scope := parent.ChildByFieldName("name")
			if scope != nil {
				return code.Text(scope, source) + "." + name
			}
		}
	}
	return name
}

func typeScriptPromptCalls(node *sitter.Node, imports map[string]string, source []byte) []*sitter.Node {
	var calls []*sitter.Node
	code.Walk(node, func(candidate *sitter.Node) {
		if candidate.Kind() != "call_expression" && candidate.Kind() != "new_expression" {
			return
		}
		function := candidate.ChildByFieldName("function")
		if function == nil {
			function = candidate.ChildByFieldName("constructor")
		}
		callable := code.Text(function, source)
		owner, method, qualified := strings.Cut(callable, ".")
		if imports[callable] == "prompt" ||
			(qualified && imports[owner] == "prompt" && (method == "fromMessages" || method == "fromTemplate")) {
			calls = append(calls, candidate)
		}
	})
	return calls
}

func typeScriptPromptFields(
	call *sitter.Node,
	imports map[string]string,
	source []byte,
	constants map[string]*adapter.Node,
) map[string]*adapter.Node {
	function := call.ChildByFieldName("function")
	if function == nil {
		function = call.ChildByFieldName("constructor")
	}
	callable := code.Text(function, source)
	owner, method, qualified := strings.Cut(callable, ".")
	arguments := code.NamedChildren(call.ChildByFieldName("arguments"))
	if qualified && imports[owner] == "prompt" && len(arguments) != 0 {
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
	if arguments[0].Kind() == "array" {
		return code.TranslateFields(
			map[string]*adapter.Node{"messages": typeScriptMessages(arguments[0], source, constants)},
			code.BracePlaceholders, "messages",
		)
	}
	return code.TranslateFields(
		typeScriptObject(arguments[0], source, constants).Fields,
		code.BracePlaceholders, "template", "messages",
	)
}

func typeScriptImports(root *sitter.Node, source []byte) map[string]string {
	imports := map[string]string{}
	namedPattern := regexp.MustCompile(`(?s)import\s*\{([^}]*)\}\s*from\s*["']([^"']+)["']`)
	defaultPattern := regexp.MustCompile(`(?s)import\s+([A-Za-z_$][A-Za-z0-9_$]*)\s+from\s*["']([^"']+)["']`)
	code.Walk(root, func(node *sitter.Node) {
		if node.Kind() != "import_statement" {
			return
		}
		text := code.Text(node, source)
		if match := defaultPattern.FindStringSubmatch(text); len(match) == 3 {
			if allowed := typeScriptAllowedImports(match[2]); allowed != nil {
				if kind := allowed["default"]; kind != "" {
					imports[match[1]] = kind
				}
			}
		}
		match := namedPattern.FindStringSubmatch(text)
		if len(match) != 3 {
			return
		}
		module := match[2]
		allowed := typeScriptAllowedImports(module)
		if allowed == nil {
			return
		}
		for _, item := range strings.Split(match[1], ",") {
			parts := strings.Fields(strings.TrimSpace(item))
			if len(parts) == 0 {
				continue
			}
			alias := parts[0]
			if len(parts) == 3 && parts[1] == "as" {
				alias = parts[2]
			}
			if kind := allowed[parts[0]]; kind != "" {
				imports[alias] = kind
			}
		}
	})
	return imports
}

type typeScriptImportBinding struct {
	module string
	name   string
	alias  string
}

func typeScriptImportBindings(root *sitter.Node, source []byte) []typeScriptImportBinding {
	namedPattern := regexp.MustCompile(`(?s)import\s*\{([^}]*)\}\s*from\s*["']([^"']+)["']`)
	defaultPattern := regexp.MustCompile(`(?s)import\s+([A-Za-z_$][A-Za-z0-9_$]*)\s+from\s*["']([^"']+)["']`)
	namespacePattern := regexp.MustCompile(`(?s)import\s*\*\s*as\s+([A-Za-z_$][A-Za-z0-9_$]*)\s+from\s*["']([^"']+)["']`)
	var bindings []typeScriptImportBinding
	code.Walk(root, func(node *sitter.Node) {
		if node.Kind() != "import_statement" {
			return
		}
		text := code.Text(node, source)
		if match := namespacePattern.FindStringSubmatch(text); len(match) == 3 &&
			typeScriptAllowedImports(match[2]) == nil {
			bindings = append(bindings, typeScriptImportBinding{
				module: match[2], name: "*", alias: match[1],
			})
		}
		if match := defaultPattern.FindStringSubmatch(text); len(match) == 3 &&
			typeScriptAllowedImports(match[2]) == nil {
			bindings = append(bindings, typeScriptImportBinding{
				module: match[2], name: "default", alias: match[1],
			})
		}
		match := namedPattern.FindStringSubmatch(text)
		if len(match) != 3 || typeScriptAllowedImports(match[2]) != nil {
			return
		}
		for _, item := range strings.Split(match[1], ",") {
			parts := strings.Fields(strings.TrimSpace(item))
			if len(parts) == 0 {
				continue
			}
			alias := parts[0]
			if len(parts) == 3 && parts[1] == "as" {
				alias = parts[2]
			}
			bindings = append(bindings, typeScriptImportBinding{
				module: match[2], name: parts[0], alias: alias,
			})
		}
	})
	return bindings
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

func typeScriptModulePaths(primary, module string) []string {
	base := path.Clean(path.Join(path.Dir(primary), module))
	for _, extension := range []string{".js", ".jsx", ".ts", ".tsx"} {
		base = strings.TrimSuffix(base, extension)
	}
	return []string{
		base + ".ts", base + ".tsx", base + ".js", base + ".jsx",
		path.Join(base, "index.ts"), path.Join(base, "index.js"),
	}
}

func typeScriptImportedValues(bundle adapter.SourceBundle, typed bool) map[string]*adapter.Node {
	result := map[string]*adapter.Node{}
	language := sitter.NewLanguage(typescript.LanguageTypescript())
	if !typed {
		language = sitter.NewLanguage(javascript.Language())
	}
	tree, err := code.ParseTree(bundle.Primary, language)
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
			dependencyLanguage := sitter.NewLanguage(typescript.LanguageTypescript())
			if strings.HasSuffix(document.Path, ".js") || strings.HasSuffix(document.Path, ".jsx") {
				dependencyLanguage = sitter.NewLanguage(javascript.Language())
			}
			dependencyTree, parseErr := code.ParseTree(document, dependencyLanguage)
			if parseErr != nil {
				result[binding.alias] = code.UnresolvedImport(binding.module, binding.name, document.Path)
				continue
			}
			constants, _ = typeScriptConstants(dependencyTree.RootNode(), document.Data)
			dependencyTree.Close()
			for _, value := range constants {
				value.SetFile(document.Path)
			}
			cache[document.Path] = constants
		}
		var value *adapter.Node
		if binding.name == "*" {
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

func typeScriptServers(root *sitter.Node, source []byte, imports map[string]string) map[string]bool {
	servers := map[string]bool{}
	code.Walk(root, func(node *sitter.Node) {
		if node.Kind() != "variable_declarator" {
			return
		}
		name := node.ChildByFieldName("name")
		value := node.ChildByFieldName("value")
		if name == nil || value == nil || value.Kind() != "new_expression" {
			return
		}
		constructor := value.ChildByFieldName("constructor")
		if constructor != nil && imports[code.Text(constructor, source)] == "mcp-server" {
			servers[code.Text(name, source)] = true
		}
	})
	return servers
}

func typeScriptObject(node *sitter.Node, source []byte, constants map[string]*adapter.Node) *adapter.Node {
	if node == nil || node.Kind() != "object" {
		if node == nil {
			return adapter.ReadOnly(0, 0, "object is missing")
		}
		return adapter.ReadOnly(int(node.StartByte()), int(node.EndByte()), "dynamic expression is not a literal object")
	}
	start, end := int(node.StartByte()), int(node.EndByte())
	fields := map[string]*adapter.Node{}
	object := map[string]any{}
	complete := true
	for _, pair := range code.NamedChildren(node) {
		if pair.Kind() != "pair" {
			fields[code.Text(pair, source)] = adapter.ReadOnly(
				int(pair.StartByte()), int(pair.EndByte()), "object entry is dynamic",
			)
			complete = false
			continue
		}
		keyNode := pair.ChildByFieldName("key")
		valueNode := pair.ChildByFieldName("value")
		key, ok := typeScriptPropertyName(keyNode, source)
		if !ok {
			key = code.Text(keyNode, source)
		}
		if _, duplicate := fields[key]; duplicate {
			return adapter.ReadOnly(start, end, "object contains a duplicate property")
		}
		value := typeScriptValue(valueNode, source, constants)
		if key == "messages" && valueNode.Kind() == "array" {
			value = typeScriptMessages(valueNode, source, constants)
		}
		if valueNode.Kind() == "call_expression" {
			function := valueNode.ChildByFieldName("function")
			arguments := code.NamedChildren(valueNode.ChildByFieldName("arguments"))
			if function != nil && code.Text(function, source) == "tool" && len(arguments) == 1 {
				call := typeScriptObject(arguments[0], source, constants)
				value = &adapter.Node{Fields: map[string]*adapter.Node{"call": call}}
			}
		}
		fields[key] = value
		if value.Value != nil {
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
			return code.EncodeJSString(text, quote)
		})
	case "number":
		value, err := strconv.ParseFloat(string(source[start:end]), 64)
		if err != nil {
			return adapter.ReadOnly(start, end, "number is not representable")
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
			if value.Value == nil {
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
		if constants != nil {
			object := node.ChildByFieldName("object")
			property := node.ChildByFieldName("property")
			if object != nil && property != nil {
				if namespace := constants[code.Text(object, source)]; namespace != nil {
					if value := namespace.Fields[code.Text(property, source)]; value != nil {
						return value
					}
				}
			}
		}
	case "identifier":
		if constants != nil {
			if value := constants[code.Text(node, source)]; value != nil && value.End < start {
				return value
			}
		}
	}
	return adapter.ReadOnly(start, end, "dynamic expression has no safe inverse")
}

func typeScriptMessages(node *sitter.Node, source []byte, constants map[string]*adapter.Node) *adapter.Node {
	if node != nil && node.Kind() == "identifier" && constants != nil {
		if messages := code.MessageList(constants[code.Text(node, source)]); messages != nil {
			return messages
		}
	}
	if node == nil || node.Kind() != "array" {
		if node == nil {
			return adapter.ReadOnly(0, 0, "messages are missing")
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
		if !roleOK || !contentOK {
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
	fields := map[string]*adapter.Node{}
	switch node.Kind() {
	case "array":
		children := code.NamedChildren(node)
		if len(children) == 2 {
			fields["role"] = typeScriptValue(children[0], source, constants)
			fields["content"] = typeScriptValue(children[1], source, constants)
		}
	case "object":
		fields = typeScriptObject(node, source, constants).Fields
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
		return strings.TrimSpace(strings.Trim(code.Text(node, source), "[]")), true
	case "number":
		return code.Text(node, source), true
	default:
		return "", false
	}
}
