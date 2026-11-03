// Package python inspects Python sources. It finds agents, prompts, tools, and
// constants through an import allowlist and never runs Python.
package python

import (
	"fmt"
	"path"
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
	constants, constantResources := pythonConstants(tree.RootNode(), document.Data)
	for name, value := range imported {
		constants[name] = value
	}
	collections := map[string][]*adapter.Node{"agents": {}, "prompts": {}, "tools": {}, "constants": {}}
	collections["constants"] = constantResources
	code.Walk(tree.RootNode(), func(node *sitter.Node) {
		if node.Kind() != "assignment" {
			return
		}
		name := node.ChildByFieldName("left")
		value := node.ChildByFieldName("right")
		if name == nil || value == nil || name.Kind() != "identifier" {
			return
		}
		id := pythonQualifiedID(node, code.Text(name, document.Data), document.Data)
		if collection, resource, ok := pythonFrameworkCall(
			value, id, aliases, instances, document.Data, constants,
		); ok {
			collections[collection] = append(collections[collection], resource)
		}
		for _, call := range pythonPromptCalls(value, aliases, document.Data) {
			fields := pythonPromptFields(call, aliases, document.Data, constants)
			collections["prompts"] = append(collections["prompts"], adapter.Resource(id, fields))
		}
	})
	code.Walk(tree.RootNode(), func(node *sitter.Node) {
		if node.Kind() != "call" || pythonAssignedCall(node) {
			return
		}
		if collection, resource, ok := pythonFrameworkCall(
			node, "", aliases, instances, document.Data, constants,
		); ok {
			collections[collection] = append(collections[collection], resource)
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
		id := code.Text(name, document.Data)
		fields := map[string]*adapter.Node{}
		if body := function.ChildByFieldName("body"); body != nil && body.NamedChildCount() > 0 {
			first := body.NamedChild(0)
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
		collections[collection] = append(collections[collection], adapter.Resource(id, fields))
	})
	return adapter.Collections(collections), nil
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
	code.Walk(node, func(candidate *sitter.Node) {
		if candidate.Kind() != "call" {
			return
		}
		callable := code.Text(candidate.ChildByFieldName("function"), source)
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
	constants map[string]*adapter.Node,
) map[string]*adapter.Node {
	callable := code.Text(call.ChildByFieldName("function"), source)
	owner, method, qualified := strings.Cut(callable, ".")
	arguments := pythonPositionalArguments(call)
	if qualified && isPythonPromptKind(aliases[owner]) {
		if len(arguments) == 0 {
			return map[string]*adapter.Node{}
		}
		if method == "from_messages" {
			return code.TranslateFields(
				map[string]*adapter.Node{"messages": pythonMessages(arguments[0], source, constants)},
				code.BracePlaceholders, "messages",
			)
		}
		if method == "from_template" {
			return code.TranslateFields(
				map[string]*adapter.Node{"template": pythonValue(arguments[0], source, constants)},
				code.BracePlaceholders, "template",
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
	return code.TranslateFields(fields, code.BracePlaceholders, "template", "messages")
}

func isPythonPromptKind(kind string) bool {
	return kind == "prompt-template" || kind == "chat-prompt"
}

func pythonPositionalArguments(call *sitter.Node) []*sitter.Node {
	var result []*sitter.Node
	for _, argument := range code.NamedChildren(call.ChildByFieldName("arguments")) {
		if argument.Kind() != "keyword_argument" {
			result = append(result, argument)
		}
	}
	return result
}

func pythonImports(root *sitter.Node, source []byte) map[string]string {
	aliases := map[string]string{}
	code.Walk(root, func(node *sitter.Node) {
		if node.Kind() != "import_from_statement" {
			return
		}
		text := code.Text(node, source)
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
	code.Walk(root, func(node *sitter.Node) {
		if node.Kind() != "import_from_statement" {
			return
		}
		text := code.Text(node, source)
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

func pythonImportedValues(bundle adapter.SourceBundle) map[string]*adapter.Node {
	result := map[string]*adapter.Node{}
	tree, err := code.ParseTree(bundle.Primary, sitter.NewLanguage(python.Language()))
	if err != nil {
		return result
	}
	defer tree.Close()
	cache := map[string]map[string]*adapter.Node{}
	for _, binding := range pythonImportBindings(tree.RootNode(), bundle.Primary.Data) {
		candidates := pythonModulePaths(bundle.Primary.Path, binding.module)
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
			dependencyTree, parseErr := code.ParseTree(document, sitter.NewLanguage(python.Language()))
			if parseErr != nil {
				result[binding.alias] = code.UnresolvedImport(binding.module, binding.name, document.Path)
				continue
			}
			constants, _ = pythonConstants(dependencyTree.RootNode(), document.Data)
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
		result[binding.alias] = value
	}
	return result
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

func pythonConstants(root *sitter.Node, source []byte) (map[string]*adapter.Node, []*adapter.Node) {
	counts := map[string]int{}
	values := map[string]*adapter.Node{}
	var resources []*adapter.Node
	code.Walk(root, func(node *sitter.Node) {
		if node.Kind() != "assignment" {
			return
		}
		name := node.ChildByFieldName("left")
		valueNode := node.ChildByFieldName("right")
		if name == nil || valueNode == nil || name.Kind() != "identifier" {
			return
		}
		id := pythonQualifiedID(node, code.Text(name, source), source)
		value := pythonValue(valueNode, source, nil)
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
			text := code.Text(function, source)
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
		id := resource.Fields["id"].Value.(string)
		if mutated[id] {
			resource.MarkReadOnly("mutated constants are read-only")
		}
	}
	return values, resources
}

func pythonMutationRoot(node *sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	text := code.Text(node, source)
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
			return code.Text(scope, source) + "." + name
		}
	}
	return name
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
	case "string":
		value, quote, err := code.DecodePythonString(source[start:end])
		if err != nil {
			return adapter.ReadOnly(start, end, err.Error())
		}
		return adapter.String(value, start, end, func(text string) ([]byte, error) {
			return code.EncodePythonString(text, quote), nil
		})
	case "integer", "float":
		value, err := strconv.ParseFloat(string(source[start:end]), 64)
		if err != nil {
			return adapter.ReadOnly(start, end, "number is not representable")
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
			if value := constants[string(source[start:end])]; value != nil && value.End < start {
				return value
			}
			if wildcard := constants["*"]; wildcard != nil {
				value := *wildcard
				value.Reason = fmt.Sprintf("star import cannot resolve %s statically", code.Text(node, source))
				return &value
			}
		}
	case "dictionary":
		return pythonDictionary(node, source, constants)
	case "list", "tuple":
		return pythonSequence(node, source, constants)
	}
	return adapter.ReadOnly(start, end, "dynamic expression has no safe inverse")
}

func pythonDictionary(node *sitter.Node, source []byte, constants map[string]*adapter.Node) *adapter.Node {
	start, end := int(node.StartByte()), int(node.EndByte())
	object := map[string]any{}
	fields := map[string]*adapter.Node{}
	complete := true
	for _, item := range code.NamedChildren(node) {
		if item.Kind() != "pair" {
			fields[code.Text(item, source)] = adapter.ReadOnly(
				int(item.StartByte()), int(item.EndByte()), "dictionary entry is dynamic",
			)
			continue
		}
		keyNode := item.ChildByFieldName("key")
		valueNode := item.ChildByFieldName("value")
		key := pythonValue(keyNode, source, constants)
		text, ok := key.Value.(string)
		if !ok {
			text = code.Text(keyNode, source)
		}
		value := pythonValue(valueNode, source, constants)
		fields[text] = value
		if value.Value != nil {
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
		result.Reason = "dictionary contains dynamic values"
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
		if value.Value == nil {
			complete = false
		}
	}
	result := &adapter.Node{
		Value: values, Items: items, Collection: true, Start: start, End: end, Capability: adapter.CapabilityDirect,
		Encode: code.PythonLiteral,
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
	if node != nil && node.Kind() == "identifier" && constants != nil {
		if messages := code.MessageList(constants[code.Text(node, source)]); messages != nil {
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
		if !roleOK || !contentOK {
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
	aliases, instances map[string]string,
	source []byte,
) string {
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

func encodePythonBool(value bool) ([]byte, error) {
	if value {
		return []byte("True"), nil
	}
	return []byte("False"), nil
}
