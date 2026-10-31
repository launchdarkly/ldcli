package reference

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

type typeScriptImportRule struct {
	module  string
	symbols map[string]string
}

var typeScriptFrameworkAllowlist = []typeScriptImportRule{
	{module: "@modelcontextprotocol/sdk", symbols: map[string]string{"McpServer": "mcp-server"}},
	{module: "openai", symbols: map[string]string{"default": "openai-client", "OpenAI": "openai-client"}},
	{module: "@anthropic-ai/sdk", symbols: map[string]string{"default": "anthropic-client", "Anthropic": "anthropic-client"}},
	{module: "ai", symbols: map[string]string{
		"generateText": "ai-prompt", "streamText": "ai-prompt", "tool": "vercel-tool",
	}},
	{module: "@openai/agents", symbols: map[string]string{"Agent": "openai-agent", "tool": "openai-tool"}},
	{module: "@langchain/core/prompts", symbols: map[string]string{
		"PromptTemplate": "prompt", "ChatPromptTemplate": "prompt",
	}},
	{module: "@langchain/core/tools", symbols: map[string]string{"tool": "langchain-tool"}},
	{module: "@mastra/core/agent", symbols: map[string]string{"Agent": "mastra-agent"}},
	{module: "@mastra/core/tools", symbols: map[string]string{"createTool": "mastra-tool"}},
}

type typeScriptCallRule struct {
	collection string
	objectArg  int
}

var typeScriptCallRules = map[string]typeScriptCallRule{
	"openai-agent":      {collection: "agents", objectArg: 0},
	"mastra-agent":      {collection: "agents", objectArg: 0},
	"ai-prompt":         {collection: "prompts", objectArg: 0},
	"vercel-tool":       {collection: "tools", objectArg: 0},
	"openai-tool":       {collection: "tools", objectArg: 0},
	"langchain-tool":    {collection: "tools", objectArg: 1},
	"mastra-tool":       {collection: "tools", objectArg: 0},
	"openai-request":    {collection: "prompts", objectArg: 0},
	"anthropic-request": {collection: "prompts", objectArg: 0},
}

func typeScriptAllowedImports(module string) map[string]string {
	for _, rule := range typeScriptFrameworkAllowlist {
		if module == rule.module || strings.HasPrefix(module, rule.module+"/") {
			return rule.symbols
		}
	}
	return nil
}

func typeScriptInstances(root *sitter.Node, source []byte, imports map[string]string) map[string]string {
	instances := map[string]string{}
	walkTree(root, func(node *sitter.Node) {
		if node.Kind() != "variable_declarator" {
			return
		}
		name := node.ChildByFieldName("name")
		value := node.ChildByFieldName("value")
		if name == nil || value == nil || value.Kind() != "new_expression" && value.Kind() != "call_expression" {
			return
		}
		function := value.ChildByFieldName("constructor")
		if function == nil {
			function = value.ChildByFieldName("function")
		}
		if kind := imports[nodeText(function, source)]; kind != "" {
			instances[nodeText(name, source)] = kind
		}
	})
	return instances
}

func typeScriptFrameworkCall(
	call *sitter.Node,
	id string,
	imports, instances map[string]string,
	source []byte,
	constants map[string]*sourceNode,
) (collection string, resource *sourceNode, ok bool) {
	if call == nil || call.Kind() != "call_expression" && call.Kind() != "new_expression" {
		return "", nil, false
	}
	function := call.ChildByFieldName("function")
	if function == nil {
		function = call.ChildByFieldName("constructor")
	}
	callable := nodeText(function, source)
	kind := imports[callable]
	receiver, method, qualified := strings.Cut(callable, ".")
	if qualified {
		switch instances[receiver] {
		case "openai-client":
			if method == "chat.completions.create" || method == "responses.create" {
				kind = "openai-request"
			}
		case "anthropic-client":
			if method == "messages.create" {
				kind = "anthropic-request"
			}
		case "mcp-server":
			if method == "tool" {
				kind = "mcp-tool"
			}
			if method == "registerPrompt" {
				kind = "mcp-prompt"
			}
		}
	}
	rule, exists := typeScriptCallRules[kind]
	arguments := namedChildren(call.ChildByFieldName("arguments"))
	if !exists {
		if kind == "mcp-tool" {
			return typeScriptMCPTool(arguments, id, source, constants)
		}
		if kind == "mcp-prompt" {
			return typeScriptMCPPrompt(arguments, id, source, constants)
		}
		return "", nil, false
	}
	if rule.objectArg >= len(arguments) || arguments[rule.objectArg].Kind() != "object" {
		return "", nil, false
	}
	fields := typeScriptObject(arguments[rule.objectArg], source, constants).fields
	switch rule.collection {
	case "agents":
		fields = translateFieldPlaceholders(
			fields, dollarPlaceholder, "instructions", "prompt", "system",
		)
	case "prompts":
		fields = translateFieldPlaceholders(
			fields, dollarPlaceholder, "instructions", "prompt", "system", "messages",
		)
	}
	if id == "" {
		id = staticSourceString(fields["name"])
	}
	if id == "" {
		id = staticSourceString(fields["id"])
	}
	if id == "" {
		id = typeScriptQualifiedCallID(call, callable, source)
	}
	return rule.collection, logicalResource(id, fields), true
}

func typeScriptMCPTool(
	arguments []*sitter.Node,
	id string,
	source []byte,
	constants map[string]*sourceNode,
) (string, *sourceNode, bool) {
	if len(arguments) < 3 {
		return "", nil, false
	}
	name := typeScriptValue(arguments[0], source, constants)
	if id == "" {
		id, _ = name.value.(string)
	}
	fields := map[string]*sourceNode{
		"name":        name,
		"description": typeScriptValue(arguments[1], source, constants),
		"schema":      typeScriptValue(arguments[2], source, constants),
	}
	return "tools", logicalResource(id, fields), id != ""
}

func typeScriptMCPPrompt(
	arguments []*sitter.Node,
	id string,
	source []byte,
	constants map[string]*sourceNode,
) (string, *sourceNode, bool) {
	if len(arguments) == 0 {
		return "", nil, false
	}
	name := typeScriptValue(arguments[0], source, constants)
	if id == "" {
		id, _ = name.value.(string)
	}
	fields := map[string]*sourceNode{"name": name}
	if len(arguments) > 1 && arguments[1].Kind() == "object" {
		for key, value := range typeScriptObject(arguments[1], source, constants).fields {
			fields[key] = value
		}
	}
	fields["template"] = readOnlyNode(
		int(callStart(arguments[0])), int(callEnd(arguments[len(arguments)-1])),
		"prompt built by a handler is read-only",
	)
	return "prompts", logicalResource(id, fields), id != ""
}

func typeScriptQualifiedCallID(call *sitter.Node, callable string, source []byte) string {
	for parent := call.Parent(); parent != nil; parent = parent.Parent() {
		if parent.Kind() == "function_declaration" || parent.Kind() == "method_definition" {
			if name := parent.ChildByFieldName("name"); name != nil {
				return nodeText(name, source) + "." + callable
			}
		}
	}
	return callable
}

func callStart(node *sitter.Node) uint { return node.StartByte() }
func callEnd(node *sitter.Node) uint   { return node.EndByte() }
