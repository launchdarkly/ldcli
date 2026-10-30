package reference

import (
	"encoding/json"
	"fmt"
	"path"
	"strconv"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	python "github.com/tree-sitter/tree-sitter-python/bindings/go"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

type pythonAdapter struct{}

func (pythonAdapter) Format() string { return "python" }
func (pythonAdapter) Inspect(bundle SourceBundle) (Inspection, error) {
	root, err := parsePythonBundle(bundle)
	return Inspection{root: root}, err
}
func (pythonAdapter) Defaults(syncdomain.Kind) (string, map[string]string) { return "", nil }
func (pythonAdapter) Dependencies(document Document) ([]string, error) {
	tree, err := parseTree(document, sitter.NewLanguage(python.Language()))
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

func init() { mustRegister(pythonAdapter{}) }

func parsePythonBundle(bundle SourceBundle) (*sourceNode, error) {
	return parsePython(bundle.Primary, pythonImportedValues(bundle))
}

func parsePython(document Document, imported map[string]*sourceNode) (*sourceNode, error) {
	tree, err := parseTree(document, sitter.NewLanguage(python.Language()))
	if err != nil {
		return nil, err
	}
	defer tree.Close()

	aliases := pythonImports(tree.RootNode(), document.Data)
	instances := pythonInstances(tree.RootNode(), document.Data, aliases)
	constants, constantResources := pythonConstants(tree.RootNode(), document.Data)
	for name, value := range imported {
		constants[name] = value
	}
	collections := map[string][]*sourceNode{"agents": {}, "prompts": {}, "tools": {}, "constants": {}}
	collections["constants"] = constantResources
	walkTree(tree.RootNode(), func(node *sitter.Node) {
		if node.Kind() != "assignment" {
			return
		}
		name := node.ChildByFieldName("left")
		value := node.ChildByFieldName("right")
		if name == nil || value == nil || name.Kind() != "identifier" {
			return
		}
		id := pythonQualifiedID(node, nodeText(name, document.Data), document.Data)
		if collection, resource, ok := pythonFrameworkCall(
			value, id, aliases, instances, document.Data, constants,
		); ok {
			collections[collection] = append(collections[collection], resource)
		}
		for _, call := range pythonPromptCalls(value, aliases, document.Data) {
			fields := pythonPromptFields(call, aliases, document.Data, constants)
			collections["prompts"] = append(collections["prompts"], logicalResource(id, fields))
		}
	})
	walkTree(tree.RootNode(), func(node *sitter.Node) {
		if node.Kind() != "call" || pythonAssignedCall(node) {
			return
		}
		if collection, resource, ok := pythonFrameworkCall(
			node, "", aliases, instances, document.Data, constants,
		); ok {
			collections[collection] = append(collections[collection], resource)
		}
	})
	walkTree(tree.RootNode(), func(node *sitter.Node) {
		if node.Kind() != "decorated_definition" {
			return
		}
		collection := pythonDecoratedCollection(node, aliases, instances, document.Data)
		if collection == "" {
			return
		}
		var function *sitter.Node
		for _, child := range namedChildren(node) {
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
		id := nodeText(name, document.Data)
		fields := map[string]*sourceNode{}
		if body := function.ChildByFieldName("body"); body != nil && body.NamedChildCount() > 0 {
			first := body.NamedChild(0)
			if first.Kind() == "expression_statement" && first.NamedChildCount() == 1 {
				docstring := first.NamedChild(0)
				if docstring.Kind() == "string" {
					fields["description"] = pythonValue(docstring, document.Data, constants)
				}
			}
		}
		for _, child := range namedChildren(node) {
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
			fields["schema"] = readOnlyNode(
				int(parameters.StartByte()), int(parameters.EndByte()),
				"schema inferred from type hints is read-only",
			)
		}
		if collection == "prompts" {
			body := function.ChildByFieldName("body")
			fields["template"] = readOnlyNode(
				int(body.StartByte()), int(body.EndByte()),
				"prompt built by a function is read-only",
			)
		}
		collections[collection] = append(collections[collection], logicalResource(id, fields))
	})
	return logicalRoot(collections), nil
}

func pythonAssignedCall(call *sitter.Node) bool {
	for parent := call.Parent(); parent != nil; parent = parent.Parent() {
		if parent.Kind() == "assignment" {
			return true
		}
		if parent.Kind() == "expression_statement" || parent.Kind() == "return_statement" {
			return false
		}
	}
	return false
}

func pythonPromptCalls(node *sitter.Node, aliases map[string]string, source []byte) []*sitter.Node {
	var calls []*sitter.Node
	walkTree(node, func(candidate *sitter.Node) {
		if candidate.Kind() != "call" {
			return
		}
		callable := nodeText(candidate.ChildByFieldName("function"), source)
		owner, method, qualified := strings.Cut(callable, ".")
		if isPythonPromptKind(aliases[callable]) ||
			(qualified && isPythonPromptKind(aliases[owner]) && (method == "from_messages" || method == "from_template")) {
			calls = append(calls, candidate)
		}
	})
	return calls
}

func pythonPromptFields(
	call *sitter.Node,
	aliases map[string]string,
	source []byte,
	constants map[string]*sourceNode,
) map[string]*sourceNode {
	callable := nodeText(call.ChildByFieldName("function"), source)
	owner, method, qualified := strings.Cut(callable, ".")
	arguments := pythonPositionalArguments(call)
	if qualified && isPythonPromptKind(aliases[owner]) {
		if len(arguments) == 0 {
			return map[string]*sourceNode{}
		}
		if method == "from_messages" {
			return translateFieldPlaceholders(
				map[string]*sourceNode{"messages": pythonMessages(arguments[0], source, constants)},
				bracePlaceholder, "messages",
			)
		}
		if method == "from_template" {
			return translateFieldPlaceholders(
				map[string]*sourceNode{"template": pythonValue(arguments[0], source, constants)},
				bracePlaceholder, "template",
			)
		}
	}
	fields := pythonKeywordFields(call, source, constants)
	if len(arguments) != 0 {
		if aliases[callable] == "prompt-template" {
			fields["template"] = pythonValue(arguments[0], source, constants)
		} else if _, exists := fields["messages"]; !exists {
			fields["messages"] = pythonMessages(arguments[0], source, constants)
		}
	}
	return translateFieldPlaceholders(fields, bracePlaceholder, "template", "messages")
}

func isPythonPromptKind(kind string) bool {
	return kind == "prompt-template" || kind == "chat-prompt"
}

func pythonPositionalArguments(call *sitter.Node) []*sitter.Node {
	var result []*sitter.Node
	for _, argument := range namedChildren(call.ChildByFieldName("arguments")) {
		if argument.Kind() != "keyword_argument" {
			result = append(result, argument)
		}
	}
	return result
}

func pythonImports(root *sitter.Node, source []byte) map[string]string {
	aliases := map[string]string{}
	walkTree(root, func(node *sitter.Node) {
		if node.Kind() != "import_from_statement" {
			return
		}
		text := nodeText(node, source)
		from, imported, ok := strings.Cut(text, " import ")
		if !ok || !strings.HasPrefix(from, "from ") {
			return
		}
		module := strings.TrimSpace(strings.TrimPrefix(from, "from "))
		if allowed := pythonAllowedImports(module); allowed != nil {
			addPythonImportAliases(aliases, imported, allowed)
		}
	})
	return aliases
}

type pythonImportBinding struct {
	module string
	name   string
	alias  string
}

func pythonImportBindings(root *sitter.Node, source []byte) []pythonImportBinding {
	var bindings []pythonImportBinding
	walkTree(root, func(node *sitter.Node) {
		if node.Kind() != "import_from_statement" {
			return
		}
		text := nodeText(node, source)
		from, imported, ok := strings.Cut(text, " import ")
		if !ok || !strings.HasPrefix(from, "from ") {
			return
		}
		module := strings.TrimSpace(strings.TrimPrefix(from, "from "))
		if pythonAllowedImports(module) != nil {
			return
		}
		for _, item := range strings.Split(strings.Trim(imported, "()"), ",") {
			parts := strings.Fields(strings.TrimSpace(item))
			if len(parts) == 0 {
				continue
			}
			alias := parts[0]
			if len(parts) == 3 && parts[1] == "as" {
				alias = parts[2]
			}
			bindings = append(bindings, pythonImportBinding{
				module: module, name: parts[0], alias: alias,
			})
		}
	})
	return bindings
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

func pythonImportedValues(bundle SourceBundle) map[string]*sourceNode {
	result := map[string]*sourceNode{}
	tree, err := parseTree(bundle.Primary, sitter.NewLanguage(python.Language()))
	if err != nil {
		return result
	}
	defer tree.Close()
	cache := map[string]map[string]*sourceNode{}
	for _, binding := range pythonImportBindings(tree.RootNode(), bundle.Primary.Data) {
		candidates := pythonModulePaths(bundle.Primary.Path, binding.module)
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
			dependencyTree, parseErr := parseTree(document, sitter.NewLanguage(python.Language()))
			if parseErr != nil {
				result[binding.alias] = unresolvedImportNode(binding.module, binding.name, document.Path)
				continue
			}
			constants, _ = pythonConstants(dependencyTree.RootNode(), document.Data)
			dependencyTree.Close()
			for _, value := range constants {
				markSourceFile(value, document.Path)
			}
			cache[document.Path] = constants
		}
		value := constants[binding.name]
		if value == nil {
			value = unresolvedImportNode(binding.module, binding.name, document.Path)
		}
		result[binding.alias] = value
	}
	return result
}

func unresolvedImportNode(module, name, file string) *sourceNode {
	node := readOnlyNode(0, 0, fmt.Sprintf("imported value %s.%s cannot be resolved statically", module, name))
	node.code = syncdomain.ErrorImportUnresolved
	node.file = file
	return node
}

func addPythonImportAliases(target map[string]string, imported string, allowed map[string]string) {
	for _, item := range strings.Split(strings.Trim(imported, "()"), ",") {
		parts := strings.Fields(strings.TrimSpace(item))
		if len(parts) == 0 {
			continue
		}
		kind, ok := allowed[parts[0]]
		if !ok {
			continue
		}
		alias := parts[0]
		if len(parts) == 3 && parts[1] == "as" {
			alias = parts[2]
		}
		target[alias] = kind
	}
}

func pythonConstants(root *sitter.Node, source []byte) (map[string]*sourceNode, []*sourceNode) {
	counts := map[string]int{}
	values := map[string]*sourceNode{}
	var resources []*sourceNode
	walkTree(root, func(node *sitter.Node) {
		if node.Kind() != "assignment" {
			return
		}
		name := node.ChildByFieldName("left")
		valueNode := node.ChildByFieldName("right")
		if name == nil || valueNode == nil || name.Kind() != "identifier" {
			return
		}
		id := pythonQualifiedID(node, nodeText(name, source), source)
		value := pythonValue(valueNode, source, nil)
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
		case "assignment", "augmented_assignment", "named_expression":
			name := node.ChildByFieldName("left")
			if name == nil {
				name = node.ChildByFieldName("name")
			}
			if rootName := pythonMutationRoot(name, source); rootName != "" {
				if name.Kind() != "identifier" || node.Kind() != "assignment" {
					mutated[pythonQualifiedID(node, rootName, source)] = true
				}
			}
		case "call":
			function := node.ChildByFieldName("function")
			if function == nil {
				return
			}
			text := nodeText(function, source)
			rootName, method, ok := strings.Cut(text, ".")
			if ok && (method == "append" || method == "extend" || method == "update" ||
				method == "insert" || method == "pop" || method == "clear") {
				mutated[pythonQualifiedID(node, rootName, source)] = true
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

func pythonMutationRoot(node *sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	text := nodeText(node, source)
	if index := strings.IndexAny(text, ".["); index >= 0 {
		text = text[:index]
	}
	return strings.TrimSpace(text)
}

func pythonQualifiedID(node *sitter.Node, name string, source []byte) string {
	for parent := node.Parent(); parent != nil; parent = parent.Parent() {
		if parent.Kind() != "function_definition" && parent.Kind() != "class_definition" {
			continue
		}
		scope := parent.ChildByFieldName("name")
		if scope != nil {
			return nodeText(scope, source) + "." + name
		}
	}
	return name
}

func constantResource(id string, value *sourceNode) *sourceNode {
	fields := make(map[string]*sourceNode, len(value.fields)+1)
	for key, child := range value.fields {
		fields[key] = child
	}
	fields["id"] = &sourceNode{value: id}
	return &sourceNode{
		value: value.value, fields: fields, items: value.items, collection: value.collection,
		start: value.start, end: value.end, file: value.file, capability: value.capability,
		reason: value.reason, code: value.code, encode: value.encode,
	}
}

func pythonKeywordFields(call *sitter.Node, source []byte, constants map[string]*sourceNode) map[string]*sourceNode {
	fields := map[string]*sourceNode{}
	arguments := call.ChildByFieldName("arguments")
	for _, argument := range namedChildren(arguments) {
		if argument.Kind() != "keyword_argument" {
			continue
		}
		name := argument.ChildByFieldName("name")
		value := argument.ChildByFieldName("value")
		if name == nil || value == nil {
			continue
		}
		key := nodeText(name, source)
		if value.Kind() == "call" && key == "model_settings" &&
			strings.HasSuffix(nodeText(value.ChildByFieldName("function"), source), "ModelSettings") {
			fields[key] = &sourceNode{fields: pythonKeywordFields(value, source, constants)}
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

func pythonValue(node *sitter.Node, source []byte, constants map[string]*sourceNode) *sourceNode {
	if node == nil {
		return readOnlyNode(0, 0, "value is missing")
	}
	start, end := int(node.StartByte()), int(node.EndByte())
	switch node.Kind() {
	case "string":
		value, quote, err := decodePythonString(source[start:end])
		if err != nil {
			return readOnlyNode(start, end, err.Error())
		}
		return stringNode(value, start, end, func(text string) ([]byte, error) {
			return encodePythonString(text, quote), nil
		})
	case "integer", "float":
		value, err := strconv.ParseFloat(string(source[start:end]), 64)
		if err != nil {
			return readOnlyNode(start, end, "number is not representable")
		}
		return numberNode(value, start, end, func(number float64) ([]byte, error) {
			return []byte(strconv.FormatFloat(number, 'g', -1, 64)), nil
		})
	case "true", "false":
		value := node.Kind() == "true"
		return &sourceNode{
			value: value, start: start, end: end, capability: CapabilityDirect,
			encode: func(next any) ([]byte, error) {
				value, ok := next.(bool)
				if !ok {
					return nil, mapTypeError("source", "a boolean")
				}
				if value {
					return []byte("True"), nil
				}
				return []byte("False"), nil
			},
		}
	case "identifier":
		switch string(source[start:end]) {
		case "True", "False":
			value := string(source[start:end]) == "True"
			return &sourceNode{
				value: value, start: start, end: end, capability: CapabilityDirect,
				encode: func(next any) ([]byte, error) {
					value, ok := next.(bool)
					if !ok {
						return nil, mapTypeError("source", "a boolean")
					}
					if value {
						return []byte("True"), nil
					}
					return []byte("False"), nil
				},
			}
		}
		if constants != nil {
			if value := constants[string(source[start:end])]; value != nil && value.end < start {
				return value
			}
			if wildcard := constants["*"]; wildcard != nil {
				value := *wildcard
				value.reason = fmt.Sprintf("star import cannot resolve %s statically", nodeText(node, source))
				return &value
			}
		}
	case "dictionary":
		return pythonDictionary(node, source, constants)
	case "list", "tuple":
		return pythonSequence(node, source, constants)
	}
	return readOnlyNode(start, end, "dynamic expression has no safe inverse")
}

func pythonDictionary(node *sitter.Node, source []byte, constants map[string]*sourceNode) *sourceNode {
	start, end := int(node.StartByte()), int(node.EndByte())
	object := map[string]any{}
	fields := map[string]*sourceNode{}
	complete := true
	for _, item := range namedChildren(node) {
		if item.Kind() != "pair" {
			fields[nodeText(item, source)] = readOnlyNode(
				int(item.StartByte()), int(item.EndByte()), "dictionary entry is dynamic",
			)
			continue
		}
		keyNode := item.ChildByFieldName("key")
		valueNode := item.ChildByFieldName("value")
		key := pythonValue(keyNode, source, constants)
		text, ok := key.value.(string)
		if !ok {
			text = nodeText(keyNode, source)
		}
		value := pythonValue(valueNode, source, constants)
		fields[text] = value
		if value.value != nil {
			object[text] = value.value
		} else {
			complete = false
		}
	}
	result := &sourceNode{
		value: object, fields: fields, start: start, end: end, capability: CapabilityPatch,
		reason: "dictionary changes require patch review",
		encode: func(value any) ([]byte, error) { return json.Marshal(value) },
	}
	if !complete {
		result.value = nil
		result.capability = CapabilityReadOnly
		result.reason = "dictionary contains dynamic values"
		result.encode = nil
	}
	return result
}

func pythonSequence(node *sitter.Node, source []byte, constants map[string]*sourceNode) *sourceNode {
	start, end := int(node.StartByte()), int(node.EndByte())
	values := make([]any, 0, node.NamedChildCount())
	items := make([]*sourceNode, 0, node.NamedChildCount())
	complete := true
	for _, item := range namedChildren(node) {
		value := pythonValue(item, source, constants)
		items = append(items, value)
		values = append(values, value.value)
		if value.value == nil {
			complete = false
		}
	}
	result := &sourceNode{
		value: values, items: items, collection: true, start: start, end: end, capability: CapabilityPatch,
		reason: "collection changes require patch review",
		encode: func(value any) ([]byte, error) { return json.Marshal(value) },
	}
	if !complete {
		result.value = nil
		result.capability = CapabilityReadOnly
		result.reason = "collection contains dynamic values"
		result.encode = nil
	}
	return result
}

func pythonMessages(node *sitter.Node, source []byte, constants map[string]*sourceNode) *sourceNode {
	if node != nil && node.Kind() == "identifier" && constants != nil {
		if messages := messageListFromCollection(constants[nodeText(node, source)]); messages != nil {
			return messages
		}
	}
	if node == nil || node.Kind() != "list" && node.Kind() != "tuple" {
		if node == nil {
			return readOnlyNode(0, 0, "messages are missing")
		}
		return readOnlyNode(int(node.StartByte()), int(node.EndByte()), "messages are not a literal list")
	}
	start, end := int(node.StartByte()), int(node.EndByte())
	var items []*sourceNode
	var messages []syncdomain.Message
	complete := true
	for _, child := range namedChildren(node) {
		item := pythonMessage(child, source, constants)
		items = append(items, item)
		role, roleOK := item.fields["role"].value.(string)
		content, contentOK := item.fields["content"].value.(string)
		if !roleOK || !contentOK {
			complete = false
			continue
		}
		role = normalizeMessageRole(role)
		messages = append(messages, syncdomain.Message{Role: role, Content: content})
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

func normalizeMessageRole(role string) string {
	switch role {
	case "human":
		return syncdomain.RoleUser
	case "ai":
		return syncdomain.RoleAssistant
	default:
		return role
	}
}

func pythonMessage(node *sitter.Node, source []byte, constants map[string]*sourceNode) *sourceNode {
	fields := map[string]*sourceNode{}
	switch node.Kind() {
	case "tuple", "list":
		children := namedChildren(node)
		if len(children) == 2 {
			fields["role"] = pythonValue(children[0], source, constants)
			fields["content"] = pythonValue(children[1], source, constants)
		}
	case "dictionary":
		dictionary := pythonDictionary(node, source, constants)
		fields = dictionary.fields
	case "call":
		function := nodeText(node.ChildByFieldName("function"), source)
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
			fields["role"] = &sourceNode{value: role}
			arguments := pythonPositionalArguments(node)
			if len(arguments) != 0 {
				fields["content"] = pythonValue(arguments[0], source, constants)
			} else if content := pythonKeywordFields(node, source, constants)["content"]; content != nil {
				fields["content"] = content
			}
		}
	}
	if fields["role"] == nil {
		fields["role"] = readOnlyNode(int(node.StartByte()), int(node.EndByte()), "message role is not static")
	}
	if fields["content"] == nil {
		fields["content"] = readOnlyNode(int(node.StartByte()), int(node.EndByte()), "message content is not static")
	}
	return &sourceNode{fields: fields}
}

func pythonDecoratedCollection(
	node *sitter.Node,
	aliases, instances map[string]string,
	source []byte,
) string {
	for _, child := range namedChildren(node) {
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
		callable := nodeText(value, source)
		if kind := aliases[callable]; kind == "tool-decorator" || kind == "kernel-tool-decorator" {
			return "tools"
		}
		receiver, method, qualified := strings.Cut(callable, ".")
		if !qualified {
			continue
		}
		switch instances[receiver] {
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
