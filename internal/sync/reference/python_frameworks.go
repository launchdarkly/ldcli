package reference

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
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
}

type pythonCallRule struct {
	collection string
	positional []string
}

var pythonCallRules = map[string]pythonCallRule{
	"openai-agent":      {collection: "agents", positional: []string{"name"}},
	"crewai-agent":      {collection: "agents", positional: []string{"role", "goal", "backstory"}},
	"crewai-task":       {collection: "prompts", positional: []string{"description", "expected_output"}},
	"pydantic-agent":    {collection: "agents", positional: []string{"model"}},
	"adk-agent":         {collection: "agents", positional: []string{"name", "model", "instruction", "description"}},
	"strands-agent":     {collection: "agents", positional: []string{"system_prompt", "tools"}},
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

func pythonAllowedImports(module string) map[string]string {
	for _, rule := range pythonFrameworkAllowlist {
		if module == rule.module {
			return rule.symbols
		}
	}
	for _, rule := range pythonFrameworkAllowlist {
		if strings.HasPrefix(module, rule.module+".") {
			return rule.symbols
		}
	}
	return nil
}

func pythonInstances(root *sitter.Node, source []byte, aliases map[string]string) map[string]string {
	instances := map[string]string{}
	walkTree(root, func(node *sitter.Node) {
		if node.Kind() != "assignment" {
			return
		}
		name := node.ChildByFieldName("left")
		value := node.ChildByFieldName("right")
		if name == nil || value == nil || name.Kind() != "identifier" || value.Kind() != "call" {
			return
		}
		if kind := aliases[nodeText(value.ChildByFieldName("function"), source)]; kind != "" {
			instances[nodeText(name, source)] = kind
		}
	})
	return instances
}

func pythonFrameworkCall(
	call *sitter.Node,
	id string,
	aliases, instances map[string]string,
	source []byte,
	constants map[string]*sourceNode,
) (collection string, resource *sourceNode, ok bool) {
	if call == nil || call.Kind() != "call" {
		return "", nil, false
	}
	callable := nodeText(call.ChildByFieldName("function"), source)
	kind := aliases[callable]
	receiver, method, qualified := strings.Cut(callable, ".")
	if qualified {
		ownerKind := instances[receiver]
		if ownerKind == "" {
			ownerKind = aliases[receiver]
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
		if index < len(positional) {
			fields[name] = pythonValue(positional[index], source, constants)
		}
	}
	switch kind {
	case "openai-request":
		rule.collection = "prompts"
	case "anthropic-request":
		rule.collection = "prompts"
	case "agent-as-tool", "function-tool", "structured-tool":
		rule.collection = "tools"
	}
	if rule.collection == "" {
		return "", nil, false
	}
	switch rule.collection {
	case "agents":
		fields = translateFieldPlaceholders(
			fields, bracePlaceholder,
			"instructions", "instruction", "prompt", "system_prompt", "system_message", "goal", "backstory",
		)
	case "prompts":
		fields = translateFieldPlaceholders(
			fields, bracePlaceholder,
			"instructions", "prompt", "system", "system_prompt", "description", "messages",
		)
	}
	if name := staticSourceString(fields["name"]); name != "" && id == "" {
		id = name
	}
	if name := staticSourceString(fields["tool_name"]); name != "" && id == "" {
		id = name
	}
	if id == "" {
		id = pythonQualifiedCallID(call, callable, source)
	}
	return rule.collection, logicalResource(id, fields), true
}

func pythonQualifiedCallID(call *sitter.Node, callable string, source []byte) string {
	for parent := call.Parent(); parent != nil; parent = parent.Parent() {
		if parent.Kind() == "function_definition" {
			if name := parent.ChildByFieldName("name"); name != nil {
				return nodeText(name, source) + "." + callable
			}
		}
	}
	return callable
}

func staticSourceString(node *sourceNode) string {
	if node == nil {
		return ""
	}
	value, _ := node.value.(string)
	return value
}
