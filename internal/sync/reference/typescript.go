package reference

import (
	"encoding/json"
	"path"
	"regexp"
	"strconv"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

type ecmaScriptAdapter struct {
	format string
	typed  bool
}

func (adapter ecmaScriptAdapter) Format() string { return adapter.format }
func (adapter ecmaScriptAdapter) Inspect(bundle SourceBundle) (Inspection, error) {
	root, err := parseTypeScriptBundle(bundle, adapter.typed)
	return Inspection{root: root}, err
}
func (ecmaScriptAdapter) Defaults(syncdomain.Kind) (string, map[string]string) { return "", nil }
func (adapter ecmaScriptAdapter) Dependencies(document Document) ([]string, error) {
	language := sitter.NewLanguage(typescript.LanguageTypescript())
	if !adapter.typed {
		language = sitter.NewLanguage(javascript.Language())
	}
	tree, err := parseTree(document, language)
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
	mustRegister(ecmaScriptAdapter{format: "typescript", typed: true})
	mustRegister(ecmaScriptAdapter{format: "javascript"})
}

func parseTypeScriptBundle(bundle SourceBundle, typed bool) (*sourceNode, error) {
	return parseTypeScript(bundle.Primary, typed, typeScriptImportedValues(bundle, typed))
}

func parseTypeScript(document Document, typed bool, imported map[string]*sourceNode) (*sourceNode, error) {
	language := sitter.NewLanguage(typescript.LanguageTypescript())
	if !typed {
		language = sitter.NewLanguage(javascript.Language())
	}
	tree, err := parseTree(document, language)
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
	collections := map[string][]*sourceNode{"agents": {}, "prompts": {}, "tools": {}, "constants": {}}
	collections["constants"] = constantResources
	walkTree(tree.RootNode(), func(node *sitter.Node) {
		if node.Kind() != "call_expression" {
			return
		}
		function := node.ChildByFieldName("function")
		if function == nil || function.Kind() != "member_expression" {
			return
		}
		receiver := function.ChildByFieldName("object")
		method := function.ChildByFieldName("property")
		if receiver == nil || method == nil || !servers[nodeText(receiver, document.Data)] ||
			nodeText(method, document.Data) != "registerTool" {
			return
		}
		arguments := namedChildren(node.ChildByFieldName("arguments"))
		if len(arguments) < 2 {
			return
		}
		idNode := typeScriptValue(arguments[0], document.Data, constants)
		id, ok := idNode.value.(string)
		if !ok {
			return
		}
		fields := typeScriptObject(arguments[1], document.Data, constants)
		collections["tools"] = append(collections["tools"], logicalResource(id, fields.fields))
	})
	walkTree(tree.RootNode(), func(node *sitter.Node) {
		if node.Kind() != "variable_declarator" {
			return
		}
		name := node.ChildByFieldName("name")
		value := node.ChildByFieldName("value")
		if name == nil || value == nil {
			return
		}
		id := typeScriptQualifiedID(node, nodeText(name, document.Data), document.Data)
		for _, call := range typeScriptPromptCalls(value, imports, document.Data) {
			fields := typeScriptPromptFields(call, imports, document.Data, constants)
			collections["prompts"] = append(collections["prompts"], logicalResource(id, fields))
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
			for key, child := range typeScriptObject(value, document.Data, constants).fields {
				call := child.fields["call"]
				if call != nil && call.fields != nil {
					collections["tools"] = append(collections["tools"], logicalResource(key, call.fields))
				}
			}
		}
	})
	walkTree(tree.RootNode(), func(node *sitter.Node) {
		if node.Kind() != "call_expression" || typeScriptAssignedCall(node) {
			return
		}
		if collection, resource, ok := typeScriptFrameworkCall(
			node, "", imports, instances, document.Data, constants,
		); ok {
			collections[collection] = append(collections[collection], resource)
		}
	})
	return logicalRoot(collections), nil
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

func typeScriptConstants(root *sitter.Node, source []byte) (map[string]*sourceNode, []*sourceNode) {
	counts := map[string]int{}
	values := map[string]*sourceNode{}
	var resources []*sourceNode
	walkTree(root, func(node *sitter.Node) {
		if node.Kind() != "variable_declarator" {
			return
		}
		name := node.ChildByFieldName("name")
		valueNode := node.ChildByFieldName("value")
		if name == nil || valueNode == nil || name.Kind() != "identifier" {
			return
		}
		id := typeScriptQualifiedID(node, nodeText(name, source), source)
		value := typeScriptValue(valueNode, source, nil)
		if value.value == nil && value.fields == nil && value.items == nil {
			return
		}
		counts[id]++
		values[id] = value
		resources = append(resources, constantResource(id, value))
	})
	mutated := map[string]bool{}
	walkTree(root, func(node *sitter.Node) {
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
			text := nodeText(function, source)
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
		id := resource.fields["id"].value.(string)
		if mutated[id] {
			markReadOnly(resource, "mutated constants are read-only")
		}
	}
	return values, resources
}

func typeScriptMutationRoot(node *sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	text := nodeText(node, source)
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
				return nodeText(scope, source) + "." + name
			}
		}
	}
	return name
}

func typeScriptPromptCalls(node *sitter.Node, imports map[string]string, source []byte) []*sitter.Node {
	var calls []*sitter.Node
	walkTree(node, func(candidate *sitter.Node) {
		if candidate.Kind() != "call_expression" && candidate.Kind() != "new_expression" {
			return
		}
		function := candidate.ChildByFieldName("function")
		if function == nil {
			function = candidate.ChildByFieldName("constructor")
		}
		callable := nodeText(function, source)
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
	constants map[string]*sourceNode,
) map[string]*sourceNode {
	function := call.ChildByFieldName("function")
	if function == nil {
		function = call.ChildByFieldName("constructor")
	}
	callable := nodeText(function, source)
	owner, method, qualified := strings.Cut(callable, ".")
	arguments := namedChildren(call.ChildByFieldName("arguments"))
	if qualified && imports[owner] == "prompt" && len(arguments) != 0 {
		if method == "fromMessages" {
			return translateFieldPlaceholders(
				map[string]*sourceNode{"messages": typeScriptMessages(arguments[0], source, constants)},
				bracePlaceholder, "messages",
			)
		}
		if method == "fromTemplate" {
			return translateFieldPlaceholders(
				map[string]*sourceNode{"template": typeScriptValue(arguments[0], source, constants)},
				bracePlaceholder, "template",
			)
		}
	}
	if len(arguments) == 0 {
		return map[string]*sourceNode{}
	}
	if arguments[0].Kind() == "array" {
		return translateFieldPlaceholders(
			map[string]*sourceNode{"messages": typeScriptMessages(arguments[0], source, constants)},
			bracePlaceholder, "messages",
		)
	}
	return translateFieldPlaceholders(
		typeScriptObject(arguments[0], source, constants).fields,
		bracePlaceholder, "template", "messages",
	)
}

func typeScriptImports(root *sitter.Node, source []byte) map[string]string {
	imports := map[string]string{}
	namedPattern := regexp.MustCompile(`(?s)import\s*\{([^}]*)\}\s*from\s*["']([^"']+)["']`)
	defaultPattern := regexp.MustCompile(`(?s)import\s+([A-Za-z_$][A-Za-z0-9_$]*)\s+from\s*["']([^"']+)["']`)
	walkTree(root, func(node *sitter.Node) {
		if node.Kind() != "import_statement" {
			return
		}
		text := nodeText(node, source)
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
	walkTree(root, func(node *sitter.Node) {
		if node.Kind() != "import_statement" {
			return
		}
		text := nodeText(node, source)
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

func typeScriptImportedValues(bundle SourceBundle, typed bool) map[string]*sourceNode {
	result := map[string]*sourceNode{}
	language := sitter.NewLanguage(typescript.LanguageTypescript())
	if !typed {
		language = sitter.NewLanguage(javascript.Language())
	}
	tree, err := parseTree(bundle.Primary, language)
	if err != nil {
		return result
	}
	defer tree.Close()
	cache := map[string]map[string]*sourceNode{}
	for _, binding := range typeScriptImportBindings(tree.RootNode(), bundle.Primary.Data) {
		if !strings.HasPrefix(binding.module, ".") {
			result[binding.alias] = unresolvedImportNode(binding.module, binding.name, binding.module)
			continue
		}
		candidates := typeScriptModulePaths(bundle.Primary.Path, binding.module)
		var document Document
		for _, candidate := range candidates {
			if value, ok := bundle.Dependencies[candidate]; ok {
				document = value
				break
			}
		}
		if document.Path == "" {
			result[binding.alias] = unresolvedImportNode(binding.module, binding.name, candidates[0])
			continue
		}
		constants := cache[document.Path]
		if constants == nil {
			dependencyLanguage := sitter.NewLanguage(typescript.LanguageTypescript())
			if strings.HasSuffix(document.Path, ".js") || strings.HasSuffix(document.Path, ".jsx") {
				dependencyLanguage = sitter.NewLanguage(javascript.Language())
			}
			dependencyTree, parseErr := parseTree(document, dependencyLanguage)
			if parseErr != nil {
				result[binding.alias] = unresolvedImportNode(binding.module, binding.name, document.Path)
				continue
			}
			constants, _ = typeScriptConstants(dependencyTree.RootNode(), document.Data)
			dependencyTree.Close()
			for _, value := range constants {
				markSourceFile(value, document.Path)
			}
			cache[document.Path] = constants
		}
		var value *sourceNode
		if binding.name == "*" {
			value = &sourceNode{fields: map[string]*sourceNode{}}
			for name, constant := range constants {
				value.fields[name] = constant
			}
		} else {
			value = constants[binding.name]
		}
		if value == nil {
			value = unresolvedImportNode(binding.module, binding.name, document.Path)
		}
		result[binding.alias] = value
	}
	return result
}

func typeScriptServers(root *sitter.Node, source []byte, imports map[string]string) map[string]bool {
	servers := map[string]bool{}
	walkTree(root, func(node *sitter.Node) {
		if node.Kind() != "variable_declarator" {
			return
		}
		name := node.ChildByFieldName("name")
		value := node.ChildByFieldName("value")
		if name == nil || value == nil || value.Kind() != "new_expression" {
			return
		}
		constructor := value.ChildByFieldName("constructor")
		if constructor != nil && imports[nodeText(constructor, source)] == "mcp-server" {
			servers[nodeText(name, source)] = true
		}
	})
	return servers
}

func typeScriptObject(node *sitter.Node, source []byte, constants map[string]*sourceNode) *sourceNode {
	if node == nil || node.Kind() != "object" {
		if node == nil {
			return readOnlyNode(0, 0, "object is missing")
		}
		return readOnlyNode(int(node.StartByte()), int(node.EndByte()), "dynamic expression is not a literal object")
	}
	start, end := int(node.StartByte()), int(node.EndByte())
	fields := map[string]*sourceNode{}
	object := map[string]any{}
	complete := true
	for _, pair := range namedChildren(node) {
		if pair.Kind() != "pair" {
			fields[nodeText(pair, source)] = readOnlyNode(
				int(pair.StartByte()), int(pair.EndByte()), "object entry is dynamic",
			)
			complete = false
			continue
		}
		keyNode := pair.ChildByFieldName("key")
		valueNode := pair.ChildByFieldName("value")
		key, ok := typeScriptPropertyName(keyNode, source)
		if !ok {
			key = nodeText(keyNode, source)
		}
		if _, duplicate := fields[key]; duplicate {
			return readOnlyNode(start, end, "object contains a duplicate property")
		}
		value := typeScriptValue(valueNode, source, constants)
		if key == "messages" && valueNode.Kind() == "array" {
			value = typeScriptMessages(valueNode, source, constants)
		}
		if valueNode.Kind() == "call_expression" {
			function := valueNode.ChildByFieldName("function")
			arguments := namedChildren(valueNode.ChildByFieldName("arguments"))
			if function != nil && nodeText(function, source) == "tool" && len(arguments) == 1 {
				call := typeScriptObject(arguments[0], source, constants)
				value = &sourceNode{fields: map[string]*sourceNode{"call": call}}
			}
		}
		fields[key] = value
		if value.value != nil {
			object[key] = value.value
		} else {
			complete = false
		}
	}
	if !complete {
		return &sourceNode{
			fields: fields, start: start, end: end, capability: CapabilityReadOnly,
			reason: "object contains a dynamic value",
		}
	}
	return &sourceNode{
		value: object, fields: fields, start: start, end: end, capability: CapabilityDirect,
		encode: func(value any) ([]byte, error) { return json.Marshal(value) },
	}
}

func typeScriptValue(node *sitter.Node, source []byte, constants map[string]*sourceNode) *sourceNode {
	if node == nil {
		return readOnlyNode(0, 0, "value is missing")
	}
	start, end := int(node.StartByte()), int(node.EndByte())
	switch node.Kind() {
	case "string", "template_string":
		value, quote, err := decodeTypeScriptString(source[start:end])
		if err != nil {
			return readOnlyNode(start, end, err.Error())
		}
		return stringNode(value, start, end, func(text string) ([]byte, error) {
			return encodeTypeScriptString(text, quote)
		})
	case "number":
		value, err := strconv.ParseFloat(string(source[start:end]), 64)
		if err != nil {
			return readOnlyNode(start, end, "number is not representable")
		}
		return numberNode(value, start, end, func(number float64) ([]byte, error) {
			return []byte(strconv.FormatFloat(number, 'g', -1, 64)), nil
		})
	case "object":
		return typeScriptObject(node, source, constants)
	case "array":
		values := make([]any, 0, node.NamedChildCount())
		items := make([]*sourceNode, 0, node.NamedChildCount())
		complete := true
		for _, child := range namedChildren(node) {
			value := typeScriptValue(child, source, constants)
			items = append(items, value)
			values = append(values, value.value)
			if value.value == nil {
				complete = false
			}
		}
		result := &sourceNode{
			value: values, items: items, collection: true, start: start, end: end, capability: CapabilityPatch,
			reason: "array changes require patch review",
			encode: func(value any) ([]byte, error) { return json.Marshal(value) },
		}
		if !complete {
			result.value = nil
			result.capability = CapabilityReadOnly
			result.reason = "array contains dynamic values"
			result.encode = nil
		}
		return result
	case "true":
		return typeScriptBooleanNode(true, start, end)
	case "false":
		return typeScriptBooleanNode(false, start, end)
	case "null":
		return &sourceNode{value: nil, start: start, end: end, capability: CapabilityPatch}
	case "member_expression":
		if constants != nil {
			object := node.ChildByFieldName("object")
			property := node.ChildByFieldName("property")
			if object != nil && property != nil {
				if namespace := constants[nodeText(object, source)]; namespace != nil {
					if value := namespace.fields[nodeText(property, source)]; value != nil {
						return value
					}
				}
			}
		}
	case "identifier":
		if constants != nil {
			if value := constants[nodeText(node, source)]; value != nil && value.end < start {
				return value
			}
		}
	}
	return readOnlyNode(start, end, "dynamic expression has no safe inverse")
}

func typeScriptBooleanNode(value bool, start, end int) *sourceNode {
	return &sourceNode{
		value: value, start: start, end: end, capability: CapabilityDirect,
		encode: func(next any) ([]byte, error) {
			value, ok := next.(bool)
			if !ok {
				return nil, mapTypeError("source", "a boolean")
			}
			return []byte(strconv.FormatBool(value)), nil
		},
	}
}

func typeScriptMessages(node *sitter.Node, source []byte, constants map[string]*sourceNode) *sourceNode {
	if node != nil && node.Kind() == "identifier" && constants != nil {
		if messages := messageListFromCollection(constants[nodeText(node, source)]); messages != nil {
			return messages
		}
	}
	if node == nil || node.Kind() != "array" {
		if node == nil {
			return readOnlyNode(0, 0, "messages are missing")
		}
		return readOnlyNode(int(node.StartByte()), int(node.EndByte()), "messages are not a literal array")
	}
	start, end := int(node.StartByte()), int(node.EndByte())
	var items []*sourceNode
	var messages []syncdomain.Message
	complete := true
	for _, child := range namedChildren(node) {
		item := typeScriptMessage(child, source, constants)
		items = append(items, item)
		role, roleOK := item.fields["role"].value.(string)
		content, contentOK := item.fields["content"].value.(string)
		if !roleOK || !contentOK {
			complete = false
			continue
		}
		messages = append(messages, syncdomain.Message{
			Role: normalizeMessageRole(role), Content: content,
		})
	}
	result := &sourceNode{
		items: items, collection: true, start: start, end: end, capability: CapabilityDirect,
		reason: "message structure is read-only",
	}
	if complete {
		result.value = messages
	} else {
		result.capability = CapabilityReadOnly
		result.reason = "dynamic message content is read-only"
	}
	return result
}

func typeScriptMessage(node *sitter.Node, source []byte, constants map[string]*sourceNode) *sourceNode {
	fields := map[string]*sourceNode{}
	switch node.Kind() {
	case "array":
		children := namedChildren(node)
		if len(children) == 2 {
			fields["role"] = typeScriptValue(children[0], source, constants)
			fields["content"] = typeScriptValue(children[1], source, constants)
		}
	case "object":
		fields = typeScriptObject(node, source, constants).fields
	}
	if fields["role"] == nil {
		fields["role"] = readOnlyNode(int(node.StartByte()), int(node.EndByte()), "message role is not static")
	}
	if fields["content"] == nil {
		fields["content"] = readOnlyNode(int(node.StartByte()), int(node.EndByte()), "message content is not static")
	}
	return &sourceNode{fields: fields}
}

func typeScriptPropertyName(node *sitter.Node, source []byte) (string, bool) {
	if node == nil {
		return "", false
	}
	switch node.Kind() {
	case "property_identifier", "identifier":
		return nodeText(node, source), true
	case "string":
		value := typeScriptValue(node, source, nil)
		text, ok := value.value.(string)
		return text, ok
	case "computed_property_name":
		return strings.TrimSpace(strings.Trim(nodeText(node, source), "[]")), true
	case "number":
		return nodeText(node, source), true
	default:
		return "", false
	}
}
