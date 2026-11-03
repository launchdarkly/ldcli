package ecmascript

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sitter "github.com/tree-sitter/go-tree-sitter"
	typescriptlanguage "github.com/tree-sitter/tree-sitter-typescript/bindings/go"

	"github.com/launchdarkly/ldcli/internal/sync/adapter"
	"github.com/launchdarkly/ldcli/internal/sync/adapter/code"
)

func TestTypeScriptInspectorHandlesStaticAndDynamicLanguageForms(t *testing.T) {
	source := []byte(`
import { PromptTemplate, ChatPromptTemplate } from "@langchain/core/prompts";
import { tool } from "ai";
import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import textDefault, { TEXT, MISSING as missing } from "./values";
import * as values from "./values";
import external from "third-party";

const TRUE = true;
const FALSE = false;
const NOTHING = null;
const NUMBER = 1.5;
const OBJECT = { text: TEXT, "literal.key": "value", [TEXT]: "computed", 1: "number", ...values };
const DUPLICATE = { name: "one", name: "two" };
const DYNAMIC = [TEXT, makeValue()];
const MUTATED = ["one"];
MUTATED.push("two");
MUTATED[0] = "changed";

const empty = PromptTemplate.fromTemplate();
const chat = ChatPromptTemplate.fromMessages([["human", TEXT], { role: "ai", content: "Done" }, {}]);
const tools = { search: tool({ description: "Search", parameters: { type: "object" } }), dynamic: other };

const server = new Server({});
server.registerTool("lookup", { description: TEXT, inputSchema: { type: "object" } });
server.registerTool(dynamicName, {});
`)
	root, err := parseTypeScriptBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.ts", Data: source},
		Dependencies: map[string]adapter.Document{
			"src/values.ts": {Path: "src/values.ts", Data: []byte(`export const TEXT = "Hello ${name}";`)},
		},
	}, true)
	require.NoError(t, err)
	require.NotNil(t, root)
	assert.NotNil(t, root.Fields["tools"])
	assert.NotNil(t, root.Fields["prompts"])

	dependencies, err := (ecmaScriptAdapter{format: "typescript", typed: true}).Dependencies(
		adapter.Document{Path: "src/main.ts", Data: source},
	)
	require.NoError(t, err)
	assert.Contains(t, dependencies, "src/values.ts")

	jsRoot, err := parseTypeScriptBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.js", Data: []byte(`const value = {enabled: false};`)},
	}, false)
	require.NoError(t, err)
	require.NotNil(t, jsRoot)
}

func TestTypeScriptValueSafetyBranches(t *testing.T) {
	document := adapter.Document{Path: "main.ts", Data: []byte(`
const TRUE = true;
const OVERFLOW = 1e9999;
const TEMPLATE = ` + "`hello ${name}`" + `;
const OBJECT = { "name": "one", "name": "two", ...dynamic };
const DYNAMIC_OBJECT = { static: "value", ...dynamic };
const ARRAY = ["value", makeValue()];
const MESSAGE = {};
const NAMESPACE_VALUE = values.TEXT;
const IDENTIFIER_VALUE = STATIC_VALUE;
`)}
	tree, err := code.ParseTree(document, sitter.NewLanguage(typescriptlanguage.LanguageTypescript()))
	require.NoError(t, err)
	defer tree.Close()

	trueNode := code.FindNode(tree.RootNode(), document.Data, "true", "true")
	require.NotNil(t, trueNode)
	value := typeScriptValue(trueNode, document.Data, nil)
	encoded, err := value.Encode(false)
	require.NoError(t, err)
	assert.Equal(t, "false", string(encoded))
	_, err = value.Encode("false")
	require.Error(t, err)

	overflow := code.FindNode(tree.RootNode(), document.Data, "number", "1e9999")
	require.NotNil(t, overflow)
	assert.Equal(t, adapter.CapabilityReadOnly, typeScriptValue(overflow, document.Data, nil).Capability)
	template := code.FindNode(tree.RootNode(), document.Data, "template_string", "`hello ${name}`")
	require.NotNil(t, template)
	assert.Equal(t, adapter.CapabilityReadOnly, typeScriptValue(template, document.Data, nil).Capability)

	object := code.FindNode(tree.RootNode(), document.Data, "object", `{ "name": "one", "name": "two", ...dynamic }`)
	require.NotNil(t, object)
	assert.Equal(t, adapter.CapabilityReadOnly, typeScriptObject(object, document.Data, nil).Capability)
	dynamicObject := code.FindNode(tree.RootNode(), document.Data, "object", `{ static: "value", ...dynamic }`)
	require.NotNil(t, dynamicObject)
	assert.Equal(t, adapter.CapabilityReadOnly, typeScriptObject(dynamicObject, document.Data, nil).Capability)
	array := code.FindNode(tree.RootNode(), document.Data, "array", `["value", makeValue()]`)
	require.NotNil(t, array)
	assert.Equal(t, adapter.CapabilityReadOnly, typeScriptValue(array, document.Data, nil).Capability)

	member := code.FindNode(tree.RootNode(), document.Data, "member_expression", `values.TEXT`)
	require.NotNil(t, member)
	namespace := &adapter.Node{Fields: map[string]*adapter.Node{"TEXT": {Value: "resolved"}}}
	assert.Equal(t, "resolved", typeScriptValue(member, document.Data, map[string]*adapter.Node{"values": namespace}).Value)
	identifier := code.FindNode(tree.RootNode(), document.Data, "identifier", "STATIC_VALUE")
	require.NotNil(t, identifier)
	assert.Equal(t, "resolved", typeScriptValue(identifier, document.Data, map[string]*adapter.Node{
		"STATIC_VALUE": {Value: "resolved", End: 1},
	}).Value)

	assert.Equal(t, adapter.CapabilityReadOnly, typeScriptObject(nil, nil, nil).Capability)
	assert.Equal(t, adapter.CapabilityReadOnly, typeScriptMessages(nil, nil, nil).Capability)
	assert.Equal(t, adapter.CapabilityReadOnly, typeScriptMessages(object, document.Data, nil).Capability)
	messageObject := code.FindNode(tree.RootNode(), document.Data, "object", `{}`)
	require.NotNil(t, messageObject)
	message := typeScriptMessage(messageObject, document.Data, nil)
	assert.Equal(t, adapter.CapabilityReadOnly, message.Fields["role"].Capability)
	assert.Equal(t, adapter.CapabilityReadOnly, message.Fields["content"].Capability)
	_, ok := typeScriptPropertyName(nil, nil)
	assert.False(t, ok)
	assert.Empty(t, typeScriptMutationRoot(nil, nil))
}

func TestTypeScriptMCPServerRegistrationBranches(t *testing.T) {
	source := []byte(`
import { McpServer } from "@modelcontextprotocol/sdk";
const server = new McpServer({});
server.registerTool();
server.registerTool(dynamicName, {});
server.registerTool("lookup", { description: "Find data" });
`)
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "server.ts", Data: source}}, true)
	require.NoError(t, err)
	require.Len(t, root.Fields["tools"].Items, 1)
	assert.Equal(t, "lookup", root.Fields["tools"].Items[0].Fields["id"].Value)
}
