package ecmascript

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/launchdarkly/ldcli/internal/sync/adapter"
	"github.com/launchdarkly/ldcli/internal/sync/adapter/code"
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

// typeScriptCallRule describes the calls of one framework kind. placeholders
// is the placeholder style that the framework fills at run time. The
// frameworks here send their text as it is, so none translates, and braces
// or template syntax in a prompt stay literal.
type typeScriptCallRule struct {
	collection   string
	objectArg    int
	placeholders code.PlaceholderStyle
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

// typeScriptInstances maps each variable that holds a framework client or
// agent to its kind, by qualified id. A variable that two different
// frameworks assign in one scope is ambiguous and has no kind.
func typeScriptInstances(root *sitter.Node, source []byte, imports, typeImports typeScriptAliases) map[string]string {
	instances := map[string]string{}
	ambiguous := map[string]bool{}
	// unset holds the names that a binding without a value declares, such
	// as `let client;` or an untyped parameter. The entry shadows an outer
	// instance of the name, and a later assignment gives the name its kind
	// without making it ambiguous.
	unset := map[string]bool{}
	record := func(id, kind string) {
		if previous, seen := instances[id]; seen && !unset[id] && previous != kind {
			ambiguous[id] = true
		}
		delete(unset, id)
		instances[id] = kind
	}
	declare := func(id string) {
		if _, seen := instances[id]; !seen {
			instances[id], unset[id] = "", true
		}
	}
	code.Walk(root, func(node *sitter.Node) {
		// A binding without a value shadows an outer client of the same
		// name: an uninitialized let, a destructuring, a for-of target, a
		// catch parameter, a local function or class, and a parameter. A
		// typed parameter, as `function run(client: OpenAI)` or
		// `constructor(private client: OpenAI)`, takes the kind of its
		// type. A parameter with a default is a binding of that value below.
		switch {
		case node.Kind() == "for_in_statement":
			for _, bound := range typeScriptBoundNames(node.ChildByFieldName("left"), source) {
				declare(typeScriptBlockID(typeScriptQualifiedID(node, bound, source), node))
			}
			return
		case node.Kind() == "variable_declarator" &&
			(node.ChildByFieldName("value") == nil || node.ChildByFieldName("name").Kind() != "identifier"):
			// A destructuring binds its names to parts of a value, which
			// have no kind of their own.
			for _, bound := range typeScriptBoundNames(node.ChildByFieldName("name"), source) {
				declare(typeScriptBindingID(node, bound, source))
			}
			return
		case node.Kind() != "assignment_pattern" && node.ChildByFieldName("value") == nil:
			pattern := typeScriptLocalBinding(node)
			if pattern == nil {
				break
			}
			kind := ""
			if node.Kind() == "required_parameter" || node.Kind() == "optional_parameter" {
				kind = typeScriptParameterKind(node, node, typeImports, source)
			}
			if typeScriptParameterProperty(node) {
				if kind != "" {
					record(typeScriptInstanceID(node, pattern, source), kind)
				}
				return
			}
			for _, bound := range typeScriptBoundNames(pattern, source) {
				if kind != "" {
					record(typeScriptBindingID(node, bound, source), kind)
				} else {
					declare(typeScriptBindingID(node, bound, source))
				}
			}
			return
		}
		name, value := typeScriptBinding(node)
		if name == nil || value == nil {
			return
		}
		// A binding of another value has no kind. It still has an entry, so
		// that a local name shadows an outer instance of the same name.
		kind := ""
		if value.Kind() == "new_expression" || value.Kind() == "call_expression" {
			kind = imports.at(node, source)[typeScriptCallable(value, source)]
		}
		// A reset, such as `this.client = null` in a cleanup, does not
		// change what kind of client the name holds.
		if value.Kind() == "null" || value.Kind() == "undefined" || value.Kind() == "identifier" && code.Text(value, source) == "undefined" {
			return
		}
		// `this.client = client` stores the value of another name. The
		// type of a parameter or the kind of the name decides. A name
		// without a kind does not change the kind that a property holds,
		// because the parameter or the name can hold any client, while a
		// local bound to such a name shadows an outer client.
		if value.Kind() == "identifier" {
			other := code.Text(value, source)
			if parameter, ok := typeScriptParameterOf(node, other, source); ok {
				kind = typeScriptParameterKind(parameter, node, typeImports, source)
			} else {
				kind = typeScriptInstanceKind(instances, node, other, source)
			}
			if kind == "" {
				if typeScriptThisProperty(name) {
					return
				}
				id := typeScriptInstanceID(node, name, source)
				if _, seen := instances[id]; seen {
					// A name that held a client holds an unknown value now.
					record(id, "")
				} else {
					declare(id)
				}
				return
			}
		}
		record(typeScriptInstanceID(node, name, source), kind)
	})
	for id := range ambiguous {
		delete(instances, id)
	}
	return instances
}

func typeScriptFrameworkCall(
	call *sitter.Node,
	id string,
	imports typeScriptAliases,
	instances map[string]string,
	source []byte,
	constants map[string]*adapter.Node,
) (collection string, resource *adapter.Node, ok bool) {
	call = typeScriptUnwrap(call)
	if call == nil || call.Kind() != "call_expression" && call.Kind() != "new_expression" {
		return "", nil, false
	}
	callable := typeScriptCallable(call, source)
	kind := imports.at(call, source)[callable]
	if strings.Contains(callable, ".") {
		ownerKind, method := code.Receiver(callable, func(receiver string) string {
			return typeScriptInstanceKind(instances, call, receiver, source)
		})
		switch ownerKind {
		case "openai-client":
			if method == "chat.completions.create" || method == "responses.create" {
				kind = "openai-request"
			}
		case "anthropic-client":
			if method == "messages.create" {
				kind = "anthropic-request"
			}
		case "mcp-server":
			switch method {
			case "tool":
				kind = "mcp-tool"
			case "registerTool":
				kind = "mcp-registered-tool"
			case "registerPrompt", "prompt":
				kind = "mcp-prompt"
			}
		}
	}
	// A Vercel AI SDK tool is named by its key in the tools object, which the
	// tools collector reads, so a call without a key is not a resource.
	if kind == "vercel-tool" && id == "" {
		return "", nil, false
	}
	rule, exists := typeScriptCallRules[kind]
	arguments := code.NamedChildren(call.ChildByFieldName("arguments"))
	for index := range arguments {
		arguments[index] = typeScriptUnwrap(arguments[index])
	}
	if !exists {
		switch kind {
		case "mcp-tool":
			return typeScriptMCPTool(arguments, id, source, constants)
		case "mcp-registered-tool":
			return typeScriptMCPRegisteredTool(arguments, id, source, constants)
		case "mcp-prompt":
			return typeScriptMCPPrompt(arguments, id, source, constants)
		}
		return "", nil, false
	}
	if rule.objectArg >= len(arguments) {
		return "", nil, false
	}
	// The config can be a literal object or a constant that holds one. A
	// literal object that sync cannot read, such as one with a duplicate
	// key, still names the resource, so the plan can report it.
	config := typeScriptObject(arguments[rule.objectArg], source, constants)
	if config.Fields == nil && arguments[rule.objectArg].Kind() != "object" {
		return "", nil, false
	}
	fields := config.Fields
	if rule.placeholders != "" {
		switch rule.collection {
		case "agents":
			fields = code.TranslateFields(
				fields, rule.placeholders, "instructions", "prompt", "system",
			)
		case "prompts":
			fields = code.TranslateFields(
				fields, rule.placeholders, "instructions", "prompt", "system", "messages",
			)
		}
	}
	if id == "" {
		id = code.StaticString(fields["name"])
	}
	if id == "" {
		id = code.StaticString(fields["id"])
	}
	if id == "" {
		id = typeScriptQualifiedID(call, callable, source)
	}
	return rule.collection, adapter.Resource(id, fields), true
}

// typeScriptMCPRegisteredTool reads `server.registerTool(name, config,
// handler)`. The MCP name identifies the tool, and the config object holds
// its fields. A name that is not static falls back to the variable, as for
// the other MCP methods.
func typeScriptMCPRegisteredTool(
	arguments []*sitter.Node,
	id string,
	source []byte,
	constants map[string]*adapter.Node,
) (string, *adapter.Node, bool) {
	if len(arguments) < 2 {
		return "", nil, false
	}
	if static := code.StaticString(typeScriptValue(arguments[0], source, constants)); static != "" {
		id = static
	}
	fields := typeScriptObject(arguments[1], source, constants).Fields
	return "tools", adapter.Resource(id, fields), id != ""
}

func typeScriptMCPTool(
	arguments []*sitter.Node,
	id string,
	source []byte,
	constants map[string]*adapter.Node,
) (string, *adapter.Node, bool) {
	// server.tool takes a name, an optional description, an optional
	// schema, and the handler last. A string after the name is the
	// description, and an object is the schema.
	if len(arguments) < 2 {
		return "", nil, false
	}
	name := typeScriptValue(arguments[0], source, constants)
	// The MCP name identifies the tool, as it does for registerTool, so a
	// call assigned to a variable keeps the same id as a bare call. A name
	// that is not static falls back to the variable.
	if static := code.StaticString(name); static != "" {
		id = static
	}
	fields := map[string]*adapter.Node{"name": name}
	options := arguments[1 : len(arguments)-1]
	if len(options) != 0 {
		candidate := typeScriptValue(options[0], source, constants)
		if _, isString := candidate.Value.(string); isString ||
			options[0].Kind() == "string" || options[0].Kind() == "template_string" {
			fields["description"] = candidate
			options = options[1:]
		}
	}
	if len(options) != 0 {
		fields["schema"] = typeScriptValue(options[0], source, constants)
	}
	return "tools", adapter.Resource(id, fields), id != ""
}

func typeScriptMCPPrompt(
	arguments []*sitter.Node,
	id string,
	source []byte,
	constants map[string]*adapter.Node,
) (string, *adapter.Node, bool) {
	if len(arguments) == 0 {
		return "", nil, false
	}
	name := typeScriptValue(arguments[0], source, constants)
	// The MCP name identifies the prompt, assigned or not, as for a tool.
	if static := code.StaticString(name); static != "" {
		id = static
	}
	fields := map[string]*adapter.Node{"name": name}
	if len(arguments) > 1 {
		config := typeScriptValue(arguments[1], source, constants)
		if _, isString := config.Value.(string); isString ||
			arguments[1].Kind() == "string" || arguments[1].Kind() == "template_string" {
			// `server.prompt(name, description, ...)` of the older API
			// takes the description as a string.
			fields["description"] = config
		} else {
			// The config of registerPrompt can be a literal object or a
			// constant that holds one.
			for key, value := range typeScriptObject(arguments[1], source, constants).Fields {
				fields[key] = value
			}
		}
	}
	fields["template"] = adapter.ReadOnly(
		int(arguments[0].StartByte()), int(arguments[len(arguments)-1].EndByte()),
		"prompt built by a handler is read-only",
	)
	return "prompts", adapter.Resource(id, fields), id != ""
}
