package python

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/launchdarkly/ldcli/internal/sync/adapter"
	"github.com/launchdarkly/ldcli/internal/sync/adapter/code"
)

type pythonImportRule struct {
	module  string
	symbols map[string]string
}

var pythonFrameworkAllowlist = []pythonImportRule{
	{module: "agents", symbols: map[string]string{"Agent": "openai-agent", "function_tool": "tool-decorator"}},
	{module: "openai", symbols: map[string]string{"OpenAI": "openai-client"}},
	{module: "anthropic", symbols: map[string]string{"Anthropic": "anthropic-client"}},
	{module: "langchain_core.prompts", symbols: map[string]string{
		"PromptTemplate": "prompt-template", "ChatPromptTemplate": "chat-prompt",
	}},
	{module: "langchain_core.tools", symbols: map[string]string{
		"tool": "tool-decorator", "StructuredTool": "structured-tool",
	}},
	{module: "langgraph.prebuilt", symbols: map[string]string{"create_react_agent": "react-agent"}},
	{module: "crewai", symbols: map[string]string{"Agent": "crewai-agent", "Task": "crewai-task"}},
	{module: "pydantic_ai", symbols: map[string]string{"Agent": "pydantic-agent"}},
	{module: "google.adk.agents", symbols: map[string]string{"Agent": "adk-agent", "LlmAgent": "adk-agent"}},
	{module: "strands", symbols: map[string]string{"Agent": "strands-agent"}},
	{module: "strands.tools", symbols: map[string]string{"tool": "tool-decorator"}},
	{module: "autogen_agentchat.agents", symbols: map[string]string{"AssistantAgent": "autogen-agent"}},
	{module: "llama_index.core", symbols: map[string]string{"PromptTemplate": "prompt-template"}},
	{module: "llama_index.core.tools", symbols: map[string]string{"FunctionTool": "function-tool"}},
	{module: "llama_index.core.agent", symbols: map[string]string{"ReActAgent": "llama-react-agent"}},
	{module: "semantic_kernel.agents", symbols: map[string]string{"ChatCompletionAgent": "semantic-agent"}},
	{module: "semantic_kernel.functions", symbols: map[string]string{"kernel_function": "kernel-tool-decorator"}},
	{module: "mcp.server.fastmcp", symbols: map[string]string{"FastMCP": "fastmcp"}},
	{module: "fastmcp", symbols: map[string]string{"FastMCP": "fastmcp"}},
}

// pythonCallRule describes the calls of one framework kind. placeholders is
// the placeholder style that the framework fills at run time, so that sync
// translates it to LaunchDarkly placeholders. A framework that sends text as
// it is has none, so braces and JSON in its prompts stay literal. identity
// names the fields that identify a resource when the call is not assigned.
type pythonCallRule struct {
	collection   string
	positional   []string
	placeholders code.PlaceholderStyle
	identity     []string
}

var pythonCallRules = map[string]pythonCallRule{
	"openai-agent":      {collection: "agents", positional: []string{"name", "instructions"}},
	"crewai-agent":      {collection: "agents", positional: []string{"role", "goal", "backstory"}, placeholders: code.BracePlaceholders, identity: []string{"role"}},
	"crewai-task":       {collection: "prompts", positional: []string{"description", "expected_output"}, placeholders: code.BracePlaceholders},
	"pydantic-agent":    {collection: "agents", positional: []string{"model"}},
	"adk-agent":         {collection: "agents", positional: []string{"name", "model", "instruction", "description"}, placeholders: code.BracePlaceholders},
	"strands-agent":     {collection: "agents", positional: []string{"model", "messages", "tools", "system_prompt"}},
	"autogen-agent":     {collection: "agents", positional: []string{"name"}},
	"semantic-agent":    {collection: "agents", positional: []string{"name", "instructions"}},
	"react-agent":       {collection: "agents", positional: []string{"model", "tools", "prompt"}},
	"llama-react-agent": {collection: "agents", positional: []string{"tools"}},
	"prompt":            {collection: "prompts", positional: []string{"template"}},
	"function-tool":     {collection: "tools", positional: []string{"function"}},
	"structured-tool":   {collection: "tools", positional: []string{"function"}},
	"agent-as-tool":     {collection: "tools"},
	"openai-request":    {collection: "prompts"},
	"anthropic-request": {collection: "prompts"},
}

// pythonAllowedImports returns the symbols of an allowlisted framework
// module. Only the exact module matches, so a repository package that
// shares a prefix with a framework, such as agents.prompts, is followed as a
// repository import.
func pythonAllowedImports(module string) map[string]string {
	for _, rule := range pythonFrameworkAllowlist {
		if module == rule.module {
			return rule.symbols
		}
	}
	return nil
}

// pythonInstances maps each variable or attribute that holds a framework
// client or agent to its kind, by qualified id. A name that two different
// frameworks assign in one scope is ambiguous and has no kind.
func pythonInstances(root *sitter.Node, source []byte, aliases pythonAliases) map[string]string {
	instances := map[string]string{}
	ambiguous := map[string]bool{}
	code.Walk(root, func(node *sitter.Node) {
		var name, value *sitter.Node
		switch node.Kind() {
		case "assignment", "named_expression":
			name, value = pythonAssignmentParts(node)
		case "with_item":
			// `with OpenAI() as client` binds the name to the context value.
			if pattern := node.ChildByFieldName("value"); pattern != nil && pattern.Kind() == "as_pattern" {
				name, value = pattern.ChildByFieldName("alias"), pythonUnwrap(pattern.NamedChild(0))
				if name != nil && name.NamedChildCount() == 1 {
					name = name.NamedChild(0)
				}
			}
		default:
			return
		}
		if name == nil || value == nil {
			return
		}
		// A reset, such as `self.client = None` in a cleanup, does not
		// change what kind of client the name holds.
		if value.Kind() == "none" {
			return
		}
		// A binding of another value has no kind. It still has an entry, so
		// that a local name or a mock shadows an instance of the same name.
		kind := ""
		if value.Kind() == "call" {
			kind = aliases.at(node, source)[code.Text(value.ChildByFieldName("function"), source)]
		}
		id, ok := pythonInstanceID(node, name, source)
		if !ok {
			return
		}
		if previous, seen := instances[id]; seen && previous != kind {
			ambiguous[id] = true
		}
		instances[id] = kind
	})
	for id := range ambiguous {
		delete(instances, id)
	}
	return instances
}

// pythonInstanceID names the variable or attribute that an assignment binds.
// A variable is scoped like a constant. An attribute, such as self.client,
// belongs to the instance of the enclosing class, so every method of the
// class sees it under the class scope.
func pythonInstanceID(assignment, name *sitter.Node, source []byte) (string, bool) {
	switch name.Kind() {
	case "identifier":
		return pythonQualifiedID(assignment, code.Text(name, source), source), true
	case "attribute":
		return strings.Join(append(pythonClassScopes(assignment, source), code.Text(name, source)), "."), true
	}
	return "", false
}

// pythonClassScopes returns the scopes that enclose a node up to its
// innermost class, outermost first. A node outside every class keeps all of
// its scopes.
func pythonClassScopes(node *sitter.Node, source []byte) []string {
	var scopes []string
	inClass := false
	for parent := node.Parent(); parent != nil; parent = parent.Parent() {
		kind := parent.Kind()
		if kind != "function_definition" && kind != "class_definition" {
			continue
		}
		inClass = inClass || kind == "class_definition"
		if !inClass {
			continue
		}
		if scope := parent.ChildByFieldName("name"); scope != nil {
			scopes = append([]string{code.Text(scope, source)}, scopes...)
		}
	}
	if !inClass {
		return pythonScopes(node, source)
	}
	return scopes
}

// pythonInstanceKind returns the kind of the instance that a receiver names
// at a use: the one of the enclosing scope when it exists, else the
// module-level one. self.client in a method also names a class attribute
// `client = OpenAI()` of the class body, as Python resolves it.
func pythonInstanceKind(instances map[string]string, node *sitter.Node, receiver string, source []byte) string {
	declared := func(id string) bool { _, ok := instances[id]; return ok }
	// A binding in scope decides, with or without a kind, so a local mock
	// or a `self.client = mock()` shadows the instance that it hides.
	if id := pythonScopedName(node, receiver, source, declared); declared(id) {
		return instances[id]
	}
	for _, prefix := range []string{"self.", "cls."} {
		if attribute, ok := strings.CutPrefix(receiver, prefix); ok {
			return instances[strings.Join(append(pythonClassScopes(node, source), attribute), ".")]
		}
	}
	return ""
}

func pythonFrameworkCall(
	call *sitter.Node,
	id string,
	aliases pythonAliases,
	instances map[string]string,
	source []byte,
	constants map[string]*adapter.Node,
) (collection string, resource *adapter.Node, ok bool) {
	if call == nil || call.Kind() != "call" {
		return "", nil, false
	}
	callable := code.Text(call.ChildByFieldName("function"), source)
	kinds := aliases.at(call, source)
	kind := kinds[callable]
	if receiver, rest, qualified := strings.Cut(callable, "."); qualified {
		ownerKind, method := code.Receiver(callable, func(receiver string) string {
			return pythonInstanceKind(instances, call, receiver, source)
		})
		if ownerKind == "" {
			ownerKind, method = kinds[receiver], rest
		}
		if ownerKind == "" {
			// A module-qualified class, such as tools.StructuredTool.from_function,
			// has its kind on the part before the last dot.
			if prefix, last, ok := code.SplitMethod(callable); ok && kinds[prefix] != "" {
				ownerKind, method = kinds[prefix], last
			}
		}
		switch ownerKind {
		case "openai-client":
			if method == "chat.completions.create" || method == "responses.create" {
				kind = "openai-request"
			}
		case "anthropic-client":
			if method == "messages.create" {
				kind = "anthropic-request"
			}
		case "openai-agent", "pydantic-agent":
			if method == "as_tool" {
				kind = "agent-as-tool"
			}
		case "function-tool":
			if method == "from_defaults" {
				kind = "function-tool"
			}
		case "structured-tool":
			if method == "from_function" {
				kind = "structured-tool"
			}
		case "llama-react-agent":
			if method == "from_tools" {
				kind = "llama-react-agent"
			}
		}
	}
	if kind == "" || isPythonPromptKind(kind) || strings.Contains(kind, "decorator") ||
		kind == "openai-client" || kind == "anthropic-client" || kind == "fastmcp" {
		return "", nil, false
	}
	fields := pythonKeywordFields(call, source, constants)
	positional := pythonPositionalArguments(call)
	rule := pythonCallRules[kind]
	for index, name := range rule.positional {
		if index >= len(positional) {
			break
		}
		// A positional messages list is read as the keyword one is.
		if name == "messages" {
			fields[name] = pythonMessages(positional[index], source, constants)
			continue
		}
		fields[name] = pythonValue(positional[index], source, constants)
	}
	if rule.collection == "" {
		return "", nil, false
	}
	if rule.placeholders != "" {
		switch rule.collection {
		case "agents":
			fields = code.TranslateFields(
				fields, rule.placeholders,
				"instructions", "instruction", "prompt", "system_prompt", "system_message", "goal", "backstory",
			)
		case "prompts":
			fields = code.TranslateFields(
				fields, rule.placeholders,
				"instructions", "prompt", "system", "system_prompt", "description", "expected_output", "messages",
			)
		}
	}
	for _, field := range append([]string{"name", "tool_name"}, rule.identity...) {
		if name := code.StaticString(fields[field]); name != "" && id == "" {
			id = name
		}
	}
	if id == "" {
		id = pythonQualifiedCallID(call, callable, source)
	}
	return rule.collection, adapter.Resource(id, fields), true
}

// pythonQualifiedCallID names an unassigned call after its callable and every
// enclosing class and function.
func pythonQualifiedCallID(call *sitter.Node, callable string, source []byte) string {
	return pythonQualifiedID(call, callable, source)
}
