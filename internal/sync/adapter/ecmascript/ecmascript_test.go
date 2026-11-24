package ecmascript

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sitter "github.com/tree-sitter/go-tree-sitter"
	javascriptlanguage "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	typescriptlanguage "github.com/tree-sitter/tree-sitter-typescript/bindings/go"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
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

func TestTypeScriptImportClausesBindEveryForm(t *testing.T) {
	document := adapter.Document{Path: "main.ts", Data: []byte(`
import OpenAI, { toFile as tf, type Foo } from "openai";
import type { Bar } from "./types";
import * as values from "./values";
import def, { TEXT as text } from "./values";
import "side-effect";
`)}
	tree, err := code.ParseTree(document, sitter.NewLanguage(typescriptlanguage.LanguageTypescript()))
	require.NoError(t, err)
	defer tree.Close()

	assert.Equal(t, map[string]string{"OpenAI": "openai-client"}, typeScriptImports(tree.RootNode(), document.Data).at(tree.RootNode(), document.Data))
	assert.Equal(t, []typeScriptImportBinding{
		{module: "./values", name: "*", alias: "values"},
		{module: "./values", name: "default", alias: "def"},
		{module: "./values", name: "TEXT", alias: "text"},
	}, typeScriptImportBindings(tree.RootNode(), document.Data))
}

func TestTypeScriptNullKeepsObjectsAndArraysWritable(t *testing.T) {
	document := adapter.Document{Path: "main.ts", Data: []byte(`const CONFIG = { stop: null, items: [null, 1] };`)}
	tree, err := code.ParseTree(document, sitter.NewLanguage(typescriptlanguage.LanguageTypescript()))
	require.NoError(t, err)
	defer tree.Close()

	object := code.FindNode(tree.RootNode(), document.Data, "object", `{ stop: null, items: [null, 1] }`)
	require.NotNil(t, object)
	value := typeScriptObject(object, document.Data, nil)
	assert.Equal(t, adapter.CapabilityDirect, value.Capability)
	assert.Equal(t, map[string]any{"stop": nil, "items": []any{nil, 1.0}}, value.Value)
	encoded, err := value.Encode(map[string]any{"stop": nil, "items": []any{nil, 2.0}})
	require.NoError(t, err)
	assert.Contains(t, string(encoded), "null")
}

func TestTypeScriptFunctionExpressionsQualifyLocalConstants(t *testing.T) {
	document := adapter.Document{Path: "main.ts", Data: []byte(`
const first = async () => { const prompt = "a"; };
const second = function () { const prompt = "b"; };
const handlers = { third: () => { const prompt = "c"; } };
app.get("/", () => { const prompt = "d"; });
app.get("/x", () => { const prompt = "e"; });
`)}
	tree, err := code.ParseTree(document, sitter.NewLanguage(typescriptlanguage.LanguageTypescript()))
	require.NoError(t, err)
	defer tree.Close()

	constants, _ := typeScriptConstants(tree.RootNode(), document.Data, nil)
	assert.Equal(t, "a", constants["first.prompt"].Value)
	assert.Equal(t, "b", constants["second.prompt"].Value)
	assert.Equal(t, "c", constants["third.prompt"].Value)
	// Two callbacks that are only arguments share one unqualified id, so
	// neither constant is usable.
	assert.Nil(t, constants["prompt"])
}

func TestTypeScriptImportedConstantsResolveRegardlessOfPosition(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.ts", Data: []byte(`
import { Agent } from "@openai/agents";
import { TEXT } from "./values";
const support = new Agent({ name: "support", instructions: TEXT });
`)},
		Dependencies: map[string]adapter.Document{
			// The constant ends after the use in the primary file.
			"src/values.ts": {Path: "src/values.ts", Data: []byte("// " + strings.Repeat("x", 300) + "\nexport const TEXT = \"Hello\";\n")},
		},
	}, true)
	require.NoError(t, err)
	require.Len(t, root.Fields["agents"].Items, 1)
	instructions := root.Fields["agents"].Items[0].Fields["instructions"]
	assert.Equal(t, "Hello", instructions.Value)
	assert.Equal(t, "src/values.ts", instructions.File)
}

func TestTypeScriptTypeWrappersKeepStaticValues(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { Agent } from "@openai/agents";
const CONFIG = { stop: ["a"] as const, name: ("x")!, deep: <T>({ ok: true }) } satisfies Config;
const support = new Agent({ name: "support", instructions: "Help" } as const);
`)}}, true)
	require.NoError(t, err)

	constants := root.Fields["constants"].Items
	require.Len(t, constants, 1)
	assert.Equal(t, []any{"a"}, constants[0].Fields["stop"].Value)
	assert.Equal(t, "x", constants[0].Fields["name"].Value)
	assert.Equal(t, map[string]any{"ok": true}, constants[0].Fields["deep"].Value)
	require.Len(t, root.Fields["agents"].Items, 1)
	assert.Equal(t, "Help", root.Fields["agents"].Items[0].Fields["instructions"].Value)
}

func TestTypeScriptComputedKeysMakeObjectsReadOnly(t *testing.T) {
	document := adapter.Document{Path: "main.ts", Data: []byte(`const A = { [key]: 1 };` + "\n" + `const B = { ["lit-key"]: 2 };`)}
	tree, err := code.ParseTree(document, sitter.NewLanguage(typescriptlanguage.LanguageTypescript()))
	require.NoError(t, err)
	defer tree.Close()

	computed := code.FindNode(tree.RootNode(), document.Data, "object", `{ [key]: 1 }`)
	require.NotNil(t, computed)
	assert.Equal(t, adapter.CapabilityReadOnly, typeScriptObject(computed, document.Data, nil).Capability)
	literal := code.FindNode(tree.RootNode(), document.Data, "object", `{ ["lit-key"]: 2 }`)
	require.NotNil(t, literal)
	value := typeScriptObject(literal, document.Data, nil)
	assert.Equal(t, adapter.CapabilityDirect, value.Capability)
	assert.Equal(t, map[string]any{"lit-key": 2.0}, value.Value)
}

func TestTypeScriptScopesResolveUsesAndMutationsAlike(t *testing.T) {
	for _, language := range []struct {
		name   string
		typed  bool
		source string
	}{
		{"typescript", true, `
import { TEXT } from "./values";
const LIST = ["one"];
const MESSAGES = [["human", TEXT]];
class K { field = () => { const prompt = "class"; return prompt; }; }
function build() {
  const prompt = "local";
  LIST.push("two");
  return { prompt, list: LIST, imported: TEXT };
}
`},
		{"javascript", false, `
import { TEXT } from "./values";
const LIST = ["one"];
const MESSAGES = [["human", TEXT]];
class K { field = () => { const prompt = "class"; return prompt; }; }
function build() {
  const prompt = "local";
  LIST.push("two");
  return { prompt, list: LIST, imported: TEXT };
}
`},
	} {
		t.Run(language.name, func(t *testing.T) {
			extension := ".ts"
			if !language.typed {
				extension = ".js"
			}
			bundle := adapter.SourceBundle{
				Primary: adapter.Document{Path: "src/main" + extension, Data: []byte(language.source)},
				Dependencies: map[string]adapter.Document{
					"src/values" + extension: {Path: "src/values" + extension, Data: []byte(`export const TEXT = "Hello";`)},
				},
			}
			imported := typeScriptImportedValues(bundle, language.typed)
			tree, err := code.ParseTree(bundle.Primary, bundleLanguage(language.typed))
			require.NoError(t, err)
			defer tree.Close()

			constants, _ := typeScriptConstants(tree.RootNode(), bundle.Primary.Data, imported)
			assert.Nil(t, constants["LIST"], "a mutation inside a function marks the module-level list")
			assert.Equal(t, "local", constants["build.prompt"].Value)
			assert.Equal(t, "class", constants["K.field.prompt"].Value, "a class field arrow names the class and the field in both languages")
			messages := code.MessageList(constants["MESSAGES"])
			require.NotNil(t, messages)
			assert.Equal(t, adapter.CapabilityDirect, messages.Capability, "an imported value inside a constant resolves")

			use := code.FindNode(tree.RootNode(), bundle.Primary.Data, "shorthand_property_identifier", "prompt")
			require.NotNil(t, use)
			for name, value := range imported {
				constants[name] = value
			}
			declared := func(id string) bool { _, ok := constants[id]; return ok }
			assert.Equal(t, "build.prompt", typeScriptScopedName(use, "prompt", bundle.Primary.Data, declared))
		})
	}
}

func TestTypeScriptReturnedPromptCallsAreCollected(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { PromptTemplate } from "@langchain/core/prompts";
function build() {
  return PromptTemplate.fromTemplate("Hello {name}");
}
`)}}, true)
	require.NoError(t, err)
	require.Len(t, root.Fields["prompts"].Items, 1)
	prompt := root.Fields["prompts"].Items[0]
	assert.Equal(t, "build.PromptTemplate.fromTemplate", prompt.Fields["id"].Value)
	assert.Equal(t, "Hello {{name}}", prompt.Fields["template"].Value)
}

func bundleLanguage(typed bool) *sitter.Language {
	if typed {
		return sitter.NewLanguage(typescriptlanguage.LanguageTypescript())
	}
	return sitter.NewLanguage(javascriptlanguage.Language())
}

func TestTypeScriptNonObjectConfigsDoNotPanic(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { McpServer } from "@modelcontextprotocol/sdk";
import { Agent } from "@openai/agents";
import { ChatPromptTemplate } from "@langchain/core/prompts";
const CONFIG = { description: "From a constant" };
const server = new McpServer({});
server.registerTool("constant", CONFIG);
server.registerTool("dynamic", buildConfig());
server.registerTool("duplicate", { description: "one", description: "two" });
const support = new Agent({ name: "support", name: "again" });
const chat = ChatPromptTemplate.fromMessages([{ role: "human", role: "ai", content: "x" }]);
`)}}, true)
	require.NoError(t, err)

	tools := map[string]*adapter.Node{}
	for _, tool := range root.Fields["tools"].Items {
		tools[tool.Fields["id"].Value.(string)] = tool
	}
	assert.Equal(t, "From a constant", tools["constant"].Fields["description"].Value, "a constant object resolves")
	assert.Len(t, tools["dynamic"].Fields, 1, "a dynamic config has only the id")
	assert.Len(t, tools["duplicate"].Fields, 1)
	assert.Len(t, root.Fields["agents"].Items, 1)
	assert.Len(t, root.Fields["prompts"].Items, 1)
}

func TestTypeScriptMutationOfACollectionLeavesTheImportedValueWritable(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.ts", Data: []byte(`
import { Agent } from "@openai/agents";
import { TEXT } from "./values";
const SYSTEM = "You help.";
const MESSAGES = [["system", SYSTEM], ["human", TEXT]];
const LIST = [TEXT];
LIST.push("more");
const support = new Agent({ name: "support", instructions: TEXT });
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.ts": {Path: "src/values.ts", Data: []byte(`export const TEXT = "Hello";`)},
		},
	}, true)
	require.NoError(t, err)
	instructions := root.Fields["agents"].Items[0].Fields["instructions"]
	assert.Equal(t, adapter.CapabilityDirect, instructions.Capability, "the mutated list does not mark the import")
	for _, constant := range root.Fields["constants"].Items {
		if constant.Fields["id"].Value == "MESSAGES" {
			messages := code.MessageList(constant)
			require.NotNil(t, messages)
			assert.Equal(t, adapter.CapabilityDirect, messages.Capability, "a local system prompt resolves")
		}
	}
}

func TestTypeScriptFrameworkCallsAcceptConstantConfigsAndMessageLists(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.ts", Data: []byte(`
import { Agent } from "@openai/agents";
import { generateText } from "ai";
import { ChatPromptTemplate } from "@langchain/core/prompts";
import * as values from "./values";
const CONFIG = { name: "support", instructions: "Help" };
const SHARED = [["human", "Hi"]];
const support = new Agent(CONFIG);
const text = generateText({ messages: SHARED });
const chat = ChatPromptTemplate.fromMessages(values.MESSAGES);
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.ts": {Path: "src/values.ts", Data: []byte(`export const MESSAGES = [{ role: "system", content: "Be kind." }];`)},
		},
	}, true)
	require.NoError(t, err)

	require.Len(t, root.Fields["agents"].Items, 1)
	assert.Equal(t, "Help", root.Fields["agents"].Items[0].Fields["instructions"].Value)
	prompts := map[string]*adapter.Node{}
	for _, prompt := range root.Fields["prompts"].Items {
		prompts[prompt.Fields["id"].Value.(string)] = prompt
	}
	assert.Equal(t, []syncdomain.Message{{Role: syncdomain.RoleUser, Content: "Hi"}}, prompts["text"].Fields["messages"].Value)
	assert.Equal(t, []syncdomain.Message{{Role: syncdomain.RoleSystem, Content: "Be kind."}}, prompts["chat"].Fields["messages"].Value)
}

func TestTSXFilesUseTheTSXGrammar(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/App.tsx", Data: []byte(`
import { Agent } from "@openai/agents";
import { TEXT } from "./values";
export const View = () => <div>{TEXT}</div>;
const support = new Agent({ name: "support", instructions: TEXT });
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.tsx": {Path: "src/values.tsx", Data: []byte("export const TEXT = \"Hello\";\nexport const Tag = () => <b />;\n")},
		},
	}, true)
	require.NoError(t, err)
	require.Len(t, root.Fields["agents"].Items, 1)
	assert.Equal(t, "Hello", root.Fields["agents"].Items[0].Fields["instructions"].Value)
}

func TestTypeScriptLocalDeclarationShadowsAnImport(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.ts", Data: []byte(`
import { Agent } from "@openai/agents";
import { TEXT as imported } from "./values";
const TEXT = "local";
const support = new Agent({ name: "support", instructions: TEXT });
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.ts": {Path: "src/values.ts", Data: []byte(`export const TEXT = "imported";`)},
		},
	}, true)
	require.NoError(t, err)
	assert.Equal(t, "local", root.Fields["agents"].Items[0].Fields["instructions"].Value)
}

func TestTypeScriptAwaitedAndNestedCallsAreResources(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { generateText } from "ai";
import { Agent } from "@openai/agents";
import OpenAI from "openai";
const client = new OpenAI();
async function run() {
  const result = await generateText({ model, prompt: "Hello" });
  const reply = await client.chat.completions.create({ model: "gpt", messages: [{ role: "user", content: "Hi" }] });
  return result;
}
const team = [new Agent({ name: "writer", instructions: "Write" }), new Agent({ name: "editor", instructions: "Edit" })];
`)}}, true)
	require.NoError(t, err)
	ids := map[string]bool{}
	for _, prompt := range root.Fields["prompts"].Items {
		ids[prompt.Fields["id"].Value.(string)] = true
	}
	assert.True(t, ids["run.result"], "an awaited framework call takes the variable name")
	assert.True(t, ids["run.reply"], "an awaited client request takes the variable name")
	assert.Len(t, root.Fields["prompts"].Items, 2, "each call is a resource once")
	assert.Len(t, root.Fields["agents"].Items, 2, "each agent in the array is a resource")
}

func TestTypeScriptNamespaceMembersAreCopiedPerUse(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.ts", Data: []byte(`
import { Agent } from "@openai/agents";
import * as values from "./values";
const first = new Agent(values.CONFIG);
const second = new Agent(values.CONFIG);
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.ts": {Path: "src/values.ts", Data: []byte(`export const CONFIG = { name: "shared", instructions: "Help ${name}" };`)},
		},
	}, true)
	require.NoError(t, err)
	require.Len(t, root.Fields["agents"].Items, 2)
	ids := []string{}
	for _, agent := range root.Fields["agents"].Items {
		ids = append(ids, agent.Fields["id"].Value.(string))
		assert.Equal(t, "Help ${name}", agent.Fields["instructions"].Value, "the Agents SDK sends instructions as they are")
	}
	assert.ElementsMatch(t, []string{"first", "second"}, ids, "the variable names the resource")
	root.Fields["agents"].Items[0].Fields["instructions"].MarkReadOnly("one use changes")
	assert.Equal(t, adapter.CapabilityDirect, root.Fields["agents"].Items[1].Fields["instructions"].Capability, "the other use is its own copy")
}

func TestTypeScriptPromptConstructorAndMCPPromptAcceptConstants(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { ChatPromptTemplate } from "@langchain/core/prompts";
import { McpServer } from "@modelcontextprotocol/sdk";
const SHARED = [["human", "Hi"]];
const PROMPT_CONFIG = { description: "Greets" };
const chat = new ChatPromptTemplate(SHARED);
const server = new McpServer({});
server.registerPrompt("greet", PROMPT_CONFIG, handler);
`)}}, true)
	require.NoError(t, err)
	prompts := map[string]*adapter.Node{}
	for _, prompt := range root.Fields["prompts"].Items {
		prompts[prompt.Fields["id"].Value.(string)] = prompt
	}
	require.NotNil(t, prompts["chat"])
	assert.Equal(t, []syncdomain.Message{{Role: syncdomain.RoleUser, Content: "Hi"}}, prompts["chat"].Fields["messages"].Value)
	require.NotNil(t, prompts["greet"])
	assert.Equal(t, "Greets", prompts["greet"].Fields["description"].Value)
}

func TestTypeScriptNestedFunctionPromptsKeepTheirOwnIDs(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { PromptTemplate } from "@langchain/core/prompts";
const factory = () => {
  function helper() {
    return PromptTemplate.fromTemplate("inner");
  }
  return PromptTemplate.fromTemplate("outer");
};
`)}}, true)
	require.NoError(t, err)
	prompts := map[string]*adapter.Node{}
	for _, prompt := range root.Fields["prompts"].Items {
		prompts[prompt.Fields["id"].Value.(string)] = prompt
	}
	require.Len(t, prompts, 2)
	require.NotNil(t, prompts["factory"], "the prompt of the assigned function keeps the variable id")
	assert.Equal(t, "outer", prompts["factory"].Fields["template"].Value)
	assert.False(t, adapter.IsPositionalID(prompts["factory"]))
	require.NotNil(t, prompts["factory.helper.PromptTemplate.fromTemplate"], "a prompt in a nested function has its own id")
	assert.Equal(t, "inner", prompts["factory.helper.PromptTemplate.fromTemplate"].Fields["template"].Value)
}

func TestTypeScriptShorthandDuplicatesMakeTheObjectReadOnly(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { Agent } from "@openai/agents";
const instructions = "Shorthand";
const support = new Agent({ instructions: "Help", instructions });
`)}}, true)
	require.NoError(t, err)
	require.Len(t, root.Fields["agents"].Items, 1)
	agent := root.Fields["agents"].Items[0]
	assert.Len(t, agent.Fields, 1, "a shorthand duplicate of a key makes the object read-only, so the agent has only its id")
	assert.Nil(t, agent.Fields["instructions"])
}

func TestTypeScriptSubscriptedMutationsFreezeTheRootConstant(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
const CONFIG = { items: ["a"] };
CONFIG["items"].push("b");
const LIST = [["a"]];
LIST[0].push("b");
const KEPT = ["a"];
`)}}, true)
	require.NoError(t, err)
	constants := map[string]*adapter.Node{}
	for _, constant := range root.Fields["constants"].Items {
		constants[constant.Fields["id"].Value.(string)] = constant
	}
	assert.Equal(t, adapter.CapabilityReadOnly, constants["CONFIG"].Capability, "a mutating call through a subscript freezes the object")
	assert.Equal(t, adapter.CapabilityReadOnly, constants["LIST"].Capability, "a mutating call through an index freezes the array")
	assert.Equal(t, adapter.CapabilityDirect, constants["KEPT"].Capability)
}

func TestTypeScriptStaticFieldsResolveThroughTheClassName(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { Agent } from "@openai/agents";
class Prompts {
  static system = "Help";
  static items = ["a"];
  static kept = ["k"];
}
Prompts.items.push("b");
const agent = new Agent({ instructions: Prompts.system });
`)}}, true)
	require.NoError(t, err)
	require.Len(t, root.Fields["agents"].Items, 1)
	instructions := root.Fields["agents"].Items[0].Fields["instructions"]
	assert.Equal(t, "Help", instructions.Value, "a read through the class name resolves the static field")
	assert.Equal(t, adapter.CapabilityDirect, instructions.Capability)
	constants := map[string]*adapter.Node{}
	for _, constant := range root.Fields["constants"].Items {
		constants[constant.Fields["id"].Value.(string)] = constant
	}
	assert.Equal(t, adapter.CapabilityReadOnly, constants["Prompts.static.items"].Capability, "a mutating call through the class name freezes the static field")
	assert.Equal(t, adapter.CapabilityDirect, constants["Prompts.static.kept"].Capability)
}

func TestTypeScriptImportEqualsBindsEveryExport(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.ts", Data: []byte(`
import agents = require("@openai/agents");
import values = require("./values");
const support = new agents.Agent({ instructions: values.TEXT });
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.ts": {Path: "src/values.ts", Data: []byte("export const TEXT = \"Hello\";\n")},
		},
	}, true)
	require.NoError(t, err)
	require.Len(t, root.Fields["agents"].Items, 1, "an import-equals of a framework binds its symbols")
	assert.Equal(t, "Hello", root.Fields["agents"].Items[0].Fields["instructions"].Value, "an import-equals of a repository module reaches its exports")
}

func TestTypeScriptNamespaceMutationsFreezeOneExport(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.ts", Data: []byte(`
import { Agent } from "@openai/agents";
import * as values from "./values";
values.LIST.push("b");
const agent = new Agent({ instructions: values.PROMPT });
const copy = [values.LIST];
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.ts": {Path: "src/values.ts", Data: []byte("export const LIST = [\"a\"];\nexport const PROMPT = \"Help\";\n")},
		},
	}, true)
	require.NoError(t, err)
	require.Len(t, root.Fields["agents"].Items, 1)
	instructions := root.Fields["agents"].Items[0].Fields["instructions"]
	assert.Equal(t, "Help", instructions.Value, "an untouched export of the namespace stays writable")
	assert.Equal(t, adapter.CapabilityDirect, instructions.Capability)
	constants := map[string]*adapter.Node{}
	for _, constant := range root.Fields["constants"].Items {
		constants[constant.Fields["id"].Value.(string)] = constant
	}
	assert.Equal(t, adapter.CapabilityReadOnly, constants["copy"].Capability, "the mutated export is read-only")
}

func TestTypeScriptMCPPromptMethodIsCollected(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { McpServer } from "@modelcontextprotocol/sdk";
const server = new McpServer({});
server.prompt("summarize", "Summarizes text", { text: z.string() }, handler);
server.prompt("plain", handler);
`)}}, true)
	require.NoError(t, err)
	prompts := map[string]*adapter.Node{}
	for _, prompt := range root.Fields["prompts"].Items {
		prompts[prompt.Fields["id"].Value.(string)] = prompt
	}
	require.NotNil(t, prompts["summarize"], "the older prompt method is a prompt")
	assert.Equal(t, "Summarizes text", prompts["summarize"].Fields["description"].Value)
	require.NotNil(t, prompts["plain"])
	assert.Nil(t, prompts["plain"].Fields["description"])
}

func TestTypeScriptInstancesResolveByScope(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import OpenAI from "openai";
import Anthropic from "@anthropic-ai/sdk";
const client = new OpenAI();
const first = client.chat.completions.create({ model: "gpt", messages: [{ role: "user", content: "one" }] });
function other() {
  const client = new Anthropic();
  return client.messages.create({ model: "claude", messages: [{ role: "user", content: "two" }] });
}
let shared = new OpenAI();
shared = new Anthropic();
const third = shared.messages.create({ model: "claude", messages: [{ role: "user", content: "three" }] });
`)}}, true)
	require.NoError(t, err)
	ids := map[string]bool{}
	for _, prompt := range root.Fields["prompts"].Items {
		ids[prompt.Fields["id"].Value.(string)] = true
	}
	assert.True(t, ids["first"])
	assert.True(t, ids["other.client.messages.create"], "the local client is an Anthropic client")
}

func TestTypeScriptBlockScopedClientsKeepTheirOwnKinds(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import OpenAI from "openai";
import Anthropic from "@anthropic-ai/sdk";
function run(flag: boolean) {
  if (flag) {
    const client = new OpenAI();
    return client.chat.completions.create({ model: "gpt", messages: [{ role: "user", content: "one" }] });
  } else {
    const client = new Anthropic();
    return client.messages.create({ model: "claude", messages: [{ role: "user", content: "two" }] });
  }
}
`)}}, true)
	require.NoError(t, err)
	ids := []string{}
	for _, prompt := range root.Fields["prompts"].Items {
		ids = append(ids, prompt.Fields["id"].Value.(string))
	}
	assert.ElementsMatch(t, []string{"run.client.chat.completions.create", "run.client.messages.create"}, ids, "each block has its own client")
}

func TestJavaScriptBlockRequiresBindInTheirBlockOnly(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "src/main.js", Data: []byte(`
function build(flag) {
  if (flag) {
    const { Agent } = require("@openai/agents");
    return new Agent({ instructions: "inside" });
  }
  return new Agent({ instructions: "outside" });
}
`)}}, false)
	require.NoError(t, err)
	require.Len(t, root.Fields["agents"].Items, 1, "a require in a block is not visible after the block")
	assert.Equal(t, "inside", root.Fields["agents"].Items[0].Fields["instructions"].Value)
}

func TestJavaScriptModuleRequiresBindEveryExport(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.js", Data: []byte(`
const agents = require("@openai/agents");
const Agent = require("@openai/agents").Agent;
const values = require("./values");
const support = new agents.Agent({ instructions: values.TEXT });
const other = new Agent({ instructions: "Member" });
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.js": {Path: "src/values.js", Data: []byte("const TEXT = \"Hello\";\nmodule.exports = { TEXT };\n")},
		},
	}, false)
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	require.Len(t, agents, 2, "a module require binds every export, and a member require binds one")
	assert.Equal(t, "Hello", agents["support"].Fields["instructions"].Value, "a module require of a repository file reaches its exports")
	assert.Equal(t, "Member", agents["other"].Fields["instructions"].Value)
}

func TestJavaScriptModuleExportsAreImportable(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.js", Data: []byte(`
const { Agent } = require("@openai/agents");
const { TEXT, OTHER, INLINE } = require("./values");
const { DIRECT, NAMED } = require("./exported");
const first = new Agent({ instructions: TEXT });
const second = new Agent({ instructions: OTHER });
const third = new Agent({ instructions: INLINE });
const fourth = new Agent({ instructions: DIRECT });
const fifth = new Agent({ instructions: NAMED });
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.js": {Path: "src/values.js", Data: []byte(`const TEXT = "Hello";
const LOCAL = "Local";
module.exports = { TEXT, OTHER: LOCAL, INLINE: "Inline" };
`)},
			"src/exported.js": {Path: "src/exported.js", Data: []byte(`exports.DIRECT = "Direct";
const VALUE = "Named";
module.exports.NAMED = VALUE;
`)},
		},
	}, false)
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	for id, text := range map[string]string{"first": "Hello", "second": "Local", "third": "Inline", "fourth": "Direct", "fifth": "Named"} {
		require.NotNil(t, agents[id], id)
		assert.Equal(t, text, agents[id].Fields["instructions"].Value, "%s reads a CommonJS export", id)
	}
}

func TestJavaScriptRequireDefaultsBindFrameworkImports(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "src/main.js", Data: []byte(`
const { Agent = Fallback, tool: makeTool = other } = require("@openai/agents");
const support = new Agent({ instructions: "Help" });
`)}}, false)
	require.NoError(t, err)
	require.Len(t, root.Fields["agents"].Items, 1, "a destructuring default still binds the framework import")
	assert.Equal(t, "Help", root.Fields["agents"].Items[0].Fields["instructions"].Value)
}

func TestDefaultParametersAreClientInstances(t *testing.T) {
	typed, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import OpenAI from "openai";
function run(client = new OpenAI()) {
  return client.chat.completions.create({ model: "gpt", messages: [{ role: "user", content: "Hi" }] });
}
`)}}, true)
	require.NoError(t, err)
	require.Len(t, typed.Fields["prompts"].Items, 1, "a TypeScript default parameter is a client of the function")

	plain, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.js", Data: []byte(`
import Anthropic from "@anthropic-ai/sdk";
function run(client = new Anthropic()) {
  return client.messages.create({ model: "claude", messages: [{ role: "user", content: "Hi" }] });
}
`)}}, false)
	require.NoError(t, err)
	require.Len(t, plain.Fields["prompts"].Items, 1, "a JavaScript default parameter is a client of the function")
}

func TestTypeScriptStaticAndInstanceMembersStayApart(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import OpenAI from "openai";
import Anthropic from "@anthropic-ai/sdk";
class Bot {
  static client = new Anthropic();
  client = new OpenAI();
  run() {
    return this.client.chat.completions.create({ model: "gpt", messages: [{ role: "user", content: "instance" }] });
  }
  static make() {
    return this.client.messages.create({ model: "claude", messages: [{ role: "user", content: "static" }] });
  }
}
`)}}, true)
	require.NoError(t, err)
	contents := []string{}
	for _, prompt := range root.Fields["prompts"].Items {
		messages := prompt.Fields["messages"].Value.([]syncdomain.Message)
		contents = append(contents, messages[0].Content)
	}
	assert.ElementsMatch(t, []string{"instance", "static"}, contents, "the static client and the instance client are two bindings")
}

func TestTypeScriptClassFieldsDoNotHideImports(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { Agent } from "@openai/agents";
import OpenAI from "openai";
class Bot {
  Agent = "label";
  client = new OpenAI();
  make() {
    return new Agent({ instructions: "Help" });
  }
  close() {
    this.client = null;
  }
  ask() {
    return this.client.chat.completions.create({ model: "gpt", messages: [{ role: "user", content: "Hi" }] });
  }
}
`)}}, true)
	require.NoError(t, err)
	require.Len(t, root.Fields["agents"].Items, 1, "a field named Agent does not hide the import in a method")
	assert.Equal(t, "Help", root.Fields["agents"].Items[0].Fields["instructions"].Value)
	require.Len(t, root.Fields["prompts"].Items, 1, "a reset to null does not change the kind of the client")
}

func TestTypeScriptStaticThisBindingsAreClients(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import OpenAI from "openai";
import Anthropic from "@anthropic-ai/sdk";
class Bot {
  static {
    this.shared = new Anthropic();
  }
  static make() {
    this.client = new OpenAI();
    const first = this.client.chat.completions.create({ model: "gpt", messages: [{ role: "user", content: "assigned" }] });
    return this.shared.messages.create({ model: "claude", messages: [{ role: "user", content: "block" }] });
  }
}
`)}}, true)
	require.NoError(t, err)
	contents := []string{}
	for _, prompt := range root.Fields["prompts"].Items {
		messages := prompt.Fields["messages"].Value.([]syncdomain.Message)
		contents = append(contents, messages[0].Content)
	}
	assert.ElementsMatch(t, []string{"assigned", "block"}, contents, "a this assignment in a static method and a static block bind static clients")
}

func TestTypeScriptDuplicateToolIDsGetOrdinals(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { generateText, tool } from "ai";
const first = generateText({ model, tools: { search: tool({ description: "First", parameters: { type: "object" } }) } });
const second = generateText({ model, tools: { search: tool({ description: "Second", parameters: { type: "object" } }) } });
`)}}, true)
	require.NoError(t, err)
	tools := map[string]*adapter.Node{}
	for _, item := range root.Fields["tools"].Items {
		tools[item.Fields["id"].Value.(string)] = item
	}
	require.Len(t, tools, 2, "two inline tools of one name in one scope have two ids")
	assert.Equal(t, "First", tools["search#1"].Fields["description"].Value)
	assert.Equal(t, "Second", tools["search#2"].Fields["description"].Value)
	assert.True(t, adapter.IsPositionalID(tools["search#1"]))
}

func TestTypeScriptConstructorParameterPropertiesAreInstances(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import OpenAI from "openai";
class Bot {
  constructor(private client = new OpenAI()) {}
  run() {
    return this.client.chat.completions.create({ model: "gpt", messages: [{ role: "user", content: "Hi" }] });
  }
}
`)}}, true)
	require.NoError(t, err)
	require.Len(t, root.Fields["prompts"].Items, 1, "a parameter property is a client of the instance")
	assert.Equal(t, []syncdomain.Message{{Role: syncdomain.RoleUser, Content: "Hi"}}, root.Fields["prompts"].Items[0].Fields["messages"].Value)
}

func TestTypeScriptAliasesOfImportsFollowTheirPosition(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.ts", Data: []byte(`
import { Agent } from "@openai/agents";
import { TEXT } from "./values";
const early = new Agent({ instructions: prompt });
const prompt = TEXT;
const late = new Agent({ instructions: prompt });
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.ts": {Path: "src/values.ts", Data: []byte("export const TEXT = \"Hello\";\n")},
		},
	}, true)
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	assert.Equal(t, adapter.CapabilityReadOnly, agents["early"].Fields["instructions"].Capability, "an alias of an import is not in scope before its declaration")
	assert.Equal(t, "Hello", agents["late"].Fields["instructions"].Value)
}

func TestTypeScriptShorthandPropertiesDefaultExportsAndScopedMembers(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.ts", Data: []byte(`
import { Agent } from "@openai/agents";
import prompt from "./prompt";
import * as values from "./values";
const instructions = prompt;
const first = new Agent({ name: "first", instructions });
function build() {
  const values = { TEXT: "local" };
  return new Agent({ name: "second", instructions: values.TEXT });
}
const third = new Agent({ name: "third", instructions: values.TEXT });
`)},
		Dependencies: map[string]adapter.Document{
			"src/prompt.mts": {Path: "src/prompt.mts", Data: []byte(`export default "Help from default";`)},
			"src/values.ts":  {Path: "src/values.ts", Data: []byte(`export const TEXT = "imported";`)},
		},
	}, true)
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	assert.Equal(t, "Help from default", agents["first"].Fields["instructions"].Value, "a shorthand property resolves, through a default export in an .mts file")
	assert.Equal(t, "local", agents["second"].Fields["instructions"].Value, "a member access uses the local namespace")
	assert.Equal(t, "imported", agents["third"].Fields["instructions"].Value, "a member access uses the imported namespace")
}

func TestTypeScriptVercelToolsFollowTheImportAlias(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { tool as aiTool } from "ai";
const tools = { search: aiTool({ description: "Search", parameters: { type: "object" } }) };
`)}}, true)
	require.NoError(t, err)
	require.Len(t, root.Fields["tools"].Items, 1)
	assert.Equal(t, "search", root.Fields["tools"].Items[0].Fields["id"].Value)
	assert.Equal(t, "Search", root.Fields["tools"].Items[0].Fields["description"].Value)
}

func TestTypeScriptDynamicLocalShadowsAnImportAndNestedIDsAreDistinct(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.ts", Data: []byte(`
import { Agent } from "@openai/agents";
import { TEXT } from "./values";
const TEXT = load();
const team = [new Agent({ instructions: TEXT }), new Agent({ instructions: "static" })];
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.ts": {Path: "src/values.ts", Data: []byte(`export const TEXT = "imported";`)},
		},
	}, true)
	require.NoError(t, err)
	require.Len(t, root.Fields["agents"].Items, 2)
	ids := []string{}
	for _, agent := range root.Fields["agents"].Items {
		ids = append(ids, agent.Fields["id"].Value.(string))
	}
	assert.ElementsMatch(t, []string{"Agent", "Agent#2"}, ids)
	for _, agent := range root.Fields["agents"].Items {
		if agent.Fields["id"].Value == "Agent" {
			assert.Equal(t, adapter.CapabilityReadOnly, agent.Fields["instructions"].Capability, "the dynamic local hides the import")
		}
	}
	assert.Contains(t, typeScriptModulePaths("src/main.ts", "./lib"), "src/lib/index.tsx")
	assert.Contains(t, typeScriptModulePaths("src/main.ts", "./lib.mjs"), "src/lib.mjs")
}

func TestTypeScriptNamespaceFrameworkImportsAndNestedMembers(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.ts", Data: []byte(`
import * as agents from "@openai/agents";
import * as AI from "ai";
import * as values from "./values";
const support = new agents.Agent({ name: "support", instructions: values.CONFIG.instructions });
const text = AI.generateText({ prompt: values.CONFIG.nested.prompt });
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.ts": {Path: "src/values.ts", Data: []byte(`export const CONFIG = { instructions: "Help", nested: { prompt: "Deep" } };`)},
		},
	}, true)
	require.NoError(t, err)
	require.Len(t, root.Fields["agents"].Items, 1)
	assert.Equal(t, "Help", root.Fields["agents"].Items[0].Fields["instructions"].Value)
	require.Len(t, root.Fields["prompts"].Items, 1)
	assert.Equal(t, "Deep", root.Fields["prompts"].Items[0].Fields["prompt"].Value)
}

func TestTypeScriptNestedPromptsAreNumberedAndNumbersAreParsed(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { PromptTemplate } from "@langchain/core/prompts";
import { generateText } from "ai";
const prompts = { a: PromptTemplate.fromTemplate("one"), b: PromptTemplate.fromTemplate("two") };
const text = generateText({ prompt: "Hi", frequencyPenalty: -0.5, maxRetries: 1_000, limit: 0x10 });
`)}}, true)
	require.NoError(t, err)
	ids := map[string]*adapter.Node{}
	for _, prompt := range root.Fields["prompts"].Items {
		ids[prompt.Fields["id"].Value.(string)] = prompt
	}
	assert.Equal(t, "one", ids["prompts#1"].Fields["template"].Value)
	assert.Equal(t, "two", ids["prompts#2"].Fields["template"].Value)
	assert.Equal(t, -0.5, ids["text"].Fields["frequencyPenalty"].Value)
	assert.Equal(t, 1000.0, ids["text"].Fields["maxRetries"].Value)
	assert.Equal(t, 16.0, ids["text"].Fields["limit"].Value)
}

func TestTypeScriptIntegersBeyondExactFloatsAreReadOnly(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { generateText } from "ai";
const EXACT = 9007199254740992;
const BIG = 9007199254740993;
const text = generateText({
  prompt: "Hi", exact: EXACT, big: 9007199254740993, huge: 99999999999999999999,
  bigint: 9007199254740993n, negative: -9007199254740993, floating: 1e20, hex: 0xff,
});
`)}}, true)
	require.NoError(t, err)
	constants := map[string]*adapter.Node{}
	for _, constant := range root.Fields["constants"].Items {
		constants[constant.Fields["id"].Value.(string)] = constant
	}
	assert.Equal(t, float64(1<<53), constants["EXACT"].Value, "the largest exact integer is a constant")
	assert.Nil(t, constants["BIG"], "an integer without an exact float64 is not a literal constant")
	require.Len(t, root.Fields["prompts"].Items, 1)
	fields := root.Fields["prompts"].Items[0].Fields
	assert.Equal(t, float64(1<<53), fields["exact"].Value)
	for _, field := range []string{"big", "huge", "bigint", "negative"} {
		assert.Equal(t, adapter.CapabilityReadOnly, fields[field].Capability, "%s has no exact float64", field)
		assert.Contains(t, fields[field].Reason, "number is not representable", field)
	}
	assert.Equal(t, 1e20, fields["floating"].Value, "a float literal keeps its float value")
	assert.Equal(t, 255.0, fields["hex"].Value)
}

func TestTypeScriptDynamicLocalShadowsAnImportForLaterConstants(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.ts", Data: []byte(`
import { Agent } from "@openai/agents";
import { TEXT } from "./values";
const TEXT = load();
const CONFIG = { name: "support", instructions: TEXT };
const support = new Agent(CONFIG);
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.ts": {Path: "src/values.ts", Data: []byte(`export const TEXT = "imported";`)},
		},
	}, true)
	require.NoError(t, err)
	require.Len(t, root.Fields["agents"].Items, 1)
	assert.Equal(t, adapter.CapabilityReadOnly, root.Fields["agents"].Items[0].Fields["instructions"].Capability,
		"a later constant sees the dynamic local, not the import")
}

func TestTypeScriptAssignedClientsAreTracked(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import OpenAI from "openai";
import Anthropic from "@anthropic-ai/sdk";
let client;
client = new OpenAI();
const first = client.chat.completions.create({ model: "gpt", messages: [{ role: "user", content: "one" }] });
let shared = new OpenAI();
shared = new Anthropic();
const second = shared.messages.create({ model: "claude", messages: [{ role: "user", content: "two" }] });
`)}}, true)
	require.NoError(t, err)
	ids := map[string]bool{}
	for _, prompt := range root.Fields["prompts"].Items {
		ids[prompt.Fields["id"].Value.(string)] = true
	}
	assert.True(t, ids["first"], "a client assigned after its declaration has a kind")
	assert.False(t, ids["second"], "a client that two frameworks assign is ambiguous")
}

func TestTypeScriptFrameworkTextIsNotTranslated(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { generateText } from "ai";
import { PromptTemplate } from "@langchain/core/prompts";
const text = generateText({ prompt: "Reply as JSON: {\"answer\": \"yes\"} with ${name}" });
const chain = PromptTemplate.fromTemplate("Hello {name}");
`)}}, true)
	require.NoError(t, err)
	prompts := map[string]*adapter.Node{}
	for _, prompt := range root.Fields["prompts"].Items {
		prompts[prompt.Fields["id"].Value.(string)] = prompt
	}
	plain := prompts["text"].Fields["prompt"]
	assert.Equal(t, `Reply as JSON: {"answer": "yes"} with ${name}`, plain.Value, "the AI SDK sends the prompt as it is")
	assert.Equal(t, adapter.CapabilityDirect, plain.Capability)
	assert.Equal(t, "Hello {{name}}", prompts["chain"].Fields["template"].Value, "LangChain fills braces")
}

func TestTypeScriptNamespacedPromptFactoriesAreCollected(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import * as prompts from "@langchain/core/prompts";
const chat = prompts.ChatPromptTemplate.fromMessages([["system", "Help {name}"]]);
const simple = prompts.PromptTemplate.fromTemplate("Summarize {topic}");
`)}}, true)
	require.NoError(t, err)
	byID := map[string]*adapter.Node{}
	for _, prompt := range root.Fields["prompts"].Items {
		byID[prompt.Fields["id"].Value.(string)] = prompt
	}
	require.Contains(t, byID, "chat", "a factory reached through a namespace import is a prompt")
	assert.Equal(t, []syncdomain.Message{{Role: syncdomain.RoleSystem, Content: "Help {{name}}"}}, byID["chat"].Fields["messages"].Value)
	require.Contains(t, byID, "simple")
	assert.Equal(t, "Summarize {{topic}}", byID["simple"].Fields["template"].Value)
}

func TestTypeScriptNestedVercelToolsObjectIsCollected(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { tool } from "ai";
function build() {
  const tools = { search: tool({ description: "Search", parameters: { type: "object" } }) };
  return tools;
}
`)}}, true)
	require.NoError(t, err)
	require.Len(t, root.Fields["tools"].Items, 1)
	assert.Equal(t, "build.search", root.Fields["tools"].Items[0].Fields["id"].Value, "the function scope names the tool")
	assert.Equal(t, "Search", root.Fields["tools"].Items[0].Fields["description"].Value)
}

func TestTypeScriptTemplateLiteralWritesKeepLineBreaks(t *testing.T) {
	source := []byte("const prompt = `line one\nline two`;\n")
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: source}}, true)
	require.NoError(t, err)
	node, err := root.Resolve(`constants[id="prompt"]`)
	require.NoError(t, err)
	assert.Equal(t, "line one\nline two", node.Value)
	encoded, err := node.Encode("first\nsecond\nthird")
	require.NoError(t, err)
	assert.Equal(t, "`first\nsecond\nthird`", string(encoded), "a template literal keeps real line breaks")
}

func TestTypeScriptWrappedMemberAccessResolves(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { Agent } from "@openai/agents";
const CONFIG = { prompt: "Help", nested: { prompt: "Deep" } };
const first = new Agent({ instructions: CONFIG!.prompt });
const second = new Agent({ instructions: (CONFIG as const).nested!.prompt });
`)}}, true)
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	assert.Equal(t, "Help", agents["first"].Fields["instructions"].Value, "a non-null assertion on a segment is ignored")
	assert.Equal(t, "Deep", agents["second"].Fields["instructions"].Value, "a type wrapper on a segment is ignored")
}

func TestTypeScriptNestedPromptIsCollectedOnce(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { PromptTemplate } from "@langchain/core/prompts";
const create = () => {
  const prompt = PromptTemplate.fromTemplate("Hi {name}");
  return prompt;
};
`)}}, true)
	require.NoError(t, err)
	require.Len(t, root.Fields["prompts"].Items, 1, "the inner declaration names the prompt once")
	prompt := root.Fields["prompts"].Items[0]
	assert.Equal(t, "create.prompt", prompt.Fields["id"].Value)
	assert.Equal(t, "Hi {{name}}", prompt.Fields["template"].Value)
}

func TestTypeScriptMutatedImportIsReadOnly(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.ts", Data: []byte(`
import { Agent } from "@openai/agents";
import { CONFIG, OTHER } from "./values";
CONFIG.prompt = "changed";
const first = new Agent({ instructions: CONFIG.prompt });
const second = new Agent({ instructions: OTHER });
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.ts": {Path: "src/values.ts", Data: []byte(`export const CONFIG = { prompt: "Help" };
export const OTHER = "Keep";
`)},
		},
	}, true)
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	mutated := agents["first"].Fields["instructions"]
	assert.Equal(t, adapter.CapabilityReadOnly, mutated.Capability, "an import that this file mutates is read-only")
	assert.Equal(t, "mutated constants are read-only", mutated.Reason)
	assert.Equal(t, adapter.CapabilityDirect, agents["second"].Fields["instructions"].Capability)
}

func TestTypeScriptMCPToolOverloads(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
const server = new McpServer({ name: "orders", version: "1" });
server.tool("described", "Look up an order.", { type: "object" }, handler);
server.tool("bare", { type: "object" }, handler);
server.tool("plain", handler);
`)}}, true)
	require.NoError(t, err)
	tools := map[string]*adapter.Node{}
	for _, tool := range root.Fields["tools"].Items {
		tools[tool.Fields["id"].Value.(string)] = tool
	}
	require.Len(t, tools, 3)
	assert.Equal(t, "Look up an order.", tools["described"].Fields["description"].Value)
	assert.Equal(t, map[string]any{"type": "object"}, tools["described"].Fields["schema"].Value)
	assert.Nil(t, tools["bare"].Fields["description"], "a schema after the name is not a description")
	assert.Equal(t, map[string]any{"type": "object"}, tools["bare"].Fields["schema"].Value)
	assert.Nil(t, tools["plain"].Fields["schema"], "the handler is not a schema")
}

func TestTypeScriptWrappedCallablesMatchFrameworks(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import OpenAI from "openai";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
const client = new OpenAI();
const server = new McpServer({ name: "orders", version: "1" });
const reply = client!.chat.completions.create({ model: "gpt-5", messages: [{ role: "user", content: "Hi" }] });
(server as McpServer).registerTool("wrapped", { description: "Wrapped.", inputSchema: { type: "object" } }, handler);
server!.tool("asserted", "Asserted.", { type: "object" }, handler);
`)}}, true)
	require.NoError(t, err)
	require.Len(t, root.Fields["prompts"].Items, 1, "a wrapped receiver still names its client")
	assert.Equal(t, "reply", root.Fields["prompts"].Items[0].Fields["id"].Value)
	tools := map[string]*adapter.Node{}
	for _, tool := range root.Fields["tools"].Items {
		tools[tool.Fields["id"].Value.(string)] = tool
	}
	assert.Equal(t, "Wrapped.", tools["wrapped"].Fields["description"].Value)
	assert.Equal(t, "Asserted.", tools["asserted"].Fields["description"].Value)
}

func TestTypeScriptShorthandMessagesAreAMessageList(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { generateText } from "ai";
const messages = [{ role: "user", content: "Hi" }];
const reply = generateText({ model, messages });
`)}}, true)
	require.NoError(t, err)
	require.Len(t, root.Fields["prompts"].Items, 1)
	list := root.Fields["prompts"].Items[0].Fields["messages"]
	assert.Equal(t, []syncdomain.Message{{Role: syncdomain.RoleUser, Content: "Hi"}}, list.Value, "a shorthand constant converts as a literal array does")
	require.Len(t, list.Items, 1)
	assert.Equal(t, "Hi", list.Items[0].Fields["content"].Value)
}

func TestTypeScriptReassignedConstantsAreReadOnlyWithDistinctIDs(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
let NAME = "one";
NAME = "two";
`)}}, true)
	require.NoError(t, err)
	constants := map[string]*adapter.Node{}
	for _, constant := range root.Fields["constants"].Items {
		constants[constant.Fields["id"].Value.(string)] = constant
	}
	require.Contains(t, constants, "NAME")
	for id, constant := range constants {
		assert.Equal(t, adapter.CapabilityReadOnly, constant.Capability, id)
	}
}

func TestTypeScriptAssignedFunctionPromptsAreCollectedOnce(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { PromptTemplate } from "@langchain/core/prompts";
const create = () => {
  return PromptTemplate.fromTemplate("Hi {name}");
};
const build = () => PromptTemplate.fromTemplate("Yo {name}");
`)}}, true)
	require.NoError(t, err)
	ids := []string{}
	for _, prompt := range root.Fields["prompts"].Items {
		ids = append(ids, prompt.Fields["id"].Value.(string))
	}
	assert.ElementsMatch(t, []string{"create", "build"}, ids, "a prompt returned by an assigned function is named once, by the variable")
}

func TestTypeScriptWrappedMutationsFreezeImports(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.ts", Data: []byte(`
import { Agent } from "@openai/agents";
import { CONFIG, LIST } from "./values";
CONFIG!.prompt = "changed";
(LIST as string[]).push("more");
const agent = new Agent({ instructions: CONFIG.prompt });
const copy = LIST;
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.ts": {Path: "src/values.ts", Data: []byte(`export const CONFIG = { prompt: "Help" };
export const LIST = ["one"];
`)},
		},
	}, true)
	require.NoError(t, err)
	require.Len(t, root.Fields["agents"].Items, 1)
	assert.Equal(t, adapter.CapabilityReadOnly, root.Fields["agents"].Items[0].Fields["instructions"].Capability,
		"an assignment through a non-null assertion mutates the import")
	copied, err := root.Resolve(`constants[id="copy"]`)
	require.NoError(t, err)
	assert.Equal(t, adapter.CapabilityReadOnly, copied.Capability, "a push through a type wrapper mutates the import")
}

func TestTypeScriptWrappedMCPConstructorIsAServer(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
const server = new (McpServer as any)({ name: "orders", version: "1" });
server.registerTool("wrapped", { description: "Wrapped.", inputSchema: { type: "object" } }, handler);
server.tool("plain", "Plain.", { type: "object" }, handler);
`)}}, true)
	require.NoError(t, err)
	tools := map[string]*adapter.Node{}
	for _, tool := range root.Fields["tools"].Items {
		tools[tool.Fields["id"].Value.(string)] = tool
	}
	require.Len(t, tools, 2, "a wrapped constructor still records the server")
	assert.Equal(t, "Wrapped.", tools["wrapped"].Fields["description"].Value)
	assert.Equal(t, "Plain.", tools["plain"].Fields["description"].Value)
}

func TestTypeScriptClassFieldsAndThisPropertiesAreInstances(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import OpenAI from "openai";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
class Assistant {
  client = new OpenAI();
  server: McpServer;
  constructor() {
    this.server = new McpServer({ name: "orders", version: "1" });
  }
  ask() {
    return this.client.chat.completions.create({ model: "gpt-5", messages: [{ role: "user", content: "Hi" }] });
  }
  register() {
    this.server.tool("lookup", "Look up.", { type: "object" }, handler);
    this.server.registerTool("registered", { description: "Registered.", inputSchema: { type: "object" } }, handler);
  }
}
`)}}, true)
	require.NoError(t, err)
	require.Len(t, root.Fields["prompts"].Items, 1, "this.client names the class field")
	assert.Equal(t, "Assistant.ask.this.client.chat.completions.create", root.Fields["prompts"].Items[0].Fields["id"].Value)
	tools := map[string]*adapter.Node{}
	for _, tool := range root.Fields["tools"].Items {
		tools[tool.Fields["id"].Value.(string)] = tool
	}
	require.Len(t, tools, 2, "this.server names the property set in the constructor")
	assert.Equal(t, "Look up.", tools["lookup"].Fields["description"].Value)
	assert.Equal(t, "Registered.", tools["registered"].Fields["description"].Value)
}

func TestTypeScriptClassFieldsNameTheirResources(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { tool } from "ai";
import { PromptTemplate } from "@langchain/core/prompts";
class Assistant {
  tools = { search: tool({ description: "Search", parameters: { type: "object" } }) };
  prompt = PromptTemplate.fromTemplate("Hi {name}");
}
`)}}, true)
	require.NoError(t, err)
	require.Len(t, root.Fields["tools"].Items, 1, "a tools field of a class is collected")
	assert.Equal(t, "Assistant.search", root.Fields["tools"].Items[0].Fields["id"].Value)
	require.Len(t, root.Fields["prompts"].Items, 1, "a prompt field is named once")
	assert.Equal(t, "Assistant.prompt", root.Fields["prompts"].Items[0].Fields["id"].Value)
}

func TestTypeScriptImportStaysVisibleBeforeALaterShadowingDeclaration(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.ts", Data: []byte(`
import { Agent } from "@openai/agents";
import { TEXT } from "./values";
const first = new Agent({ instructions: TEXT });
const TEXT = "Local";
const second = new Agent({ instructions: TEXT });
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.ts": {Path: "src/values.ts", Data: []byte(`export const TEXT = "Imported";`)},
		},
	}, true)
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	assert.Equal(t, "Imported", agents["first"].Fields["instructions"].Value, "a use before the declaration sees the import")
	assert.Equal(t, "Local", agents["second"].Fields["instructions"].Value)
}

func TestTypeScriptMethodsDoNotSeeClassFieldsForBareNames(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import OpenAI from "openai";
import Anthropic from "@anthropic-ai/sdk";
const client = new Anthropic();
class Assistant {
  client = new OpenAI();
  ask() {
    const first = client.messages.create({ model: "claude", messages: [{ role: "user", content: "One" }] });
    const second = this.client.chat.completions.create({ model: "gpt-5", messages: [{ role: "user", content: "Two" }] });
    return [first, second];
  }
}
`)}}, true)
	require.NoError(t, err)
	prompts := map[string]*adapter.Node{}
	for _, prompt := range root.Fields["prompts"].Items {
		prompts[prompt.Fields["id"].Value.(string)] = prompt
	}
	require.Len(t, prompts, 2, "the bare name is the module client and this.client is the field")
	assert.Equal(t, "claude", prompts["Assistant.ask.first"].Fields["model"].Value)
	assert.Equal(t, "gpt-5", prompts["Assistant.ask.second"].Fields["model"].Value)
}

func TestJavaScriptCommonJSRequiresAreFrameworkImports(t *testing.T) {
	for name, source := range map[string]string{
		"destructured": "const { Agent } = require(\"@openai/agents\");\nconst support = new Agent({ name: \"support\", instructions: \"Help.\" });\n",
		"renamed":      "const { Agent: Bot } = require(\"@openai/agents\");\nconst support = new Bot({ name: \"support\", instructions: \"Help.\" });\n",
		"default":      "const OpenAI = require(\"openai\");\nconst client = new OpenAI();\nconst reply = client.chat.completions.create({ model: \"gpt-5\", messages: [{ role: \"system\", content: \"Help.\" }] });\n",
	} {
		t.Run(name, func(t *testing.T) {
			root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "src/main.js", Data: []byte(source)}}, false)
			require.NoError(t, err)
			assert.Equal(t, 1, len(root.Fields["agents"].Items)+len(root.Fields["prompts"].Items), "a require binds the framework symbols as an import does")
		})
	}

	root, err := parseTypeScriptBundle(adapter.SourceBundle{
		Primary:      adapter.Document{Path: "src/main.js", Data: []byte("const { Agent } = require(\"@openai/agents\");\nconst { TEXT } = require(\"./values\");\nconst support = new Agent({ instructions: TEXT });\n")},
		Dependencies: map[string]adapter.Document{"src/values.js": {Path: "src/values.js", Data: []byte("export const TEXT = \"Imported\";\n")}},
	}, false)
	require.NoError(t, err)
	require.Len(t, root.Fields["agents"].Items, 1)
	assert.Equal(t, "Imported", root.Fields["agents"].Items[0].Fields["instructions"].Value, "a required repository module resolves its constants")
}

func TestTypeScriptLocalBindingsShadowOuterInstances(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import OpenAI from "openai";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
const client = new OpenAI();
const server = new McpServer({ name: "orders", version: "1" });
function test() {
  const client = mockClient();
  const server = mockServer();
  server.registerTool("fake", { description: "Fake.", inputSchema: { type: "object" } }, handler);
  server.tool("other", "Other.", { type: "object" }, handler);
  return client.chat.completions.create({ model: "gpt-5", messages: [{ role: "user", content: "Hi" }] });
}
`)}}, true)
	require.NoError(t, err)
	assert.Empty(t, root.Fields["tools"].Items, "a local mock named server is not the MCP server")
	assert.Empty(t, root.Fields["prompts"].Items, "a local mock named client is not the OpenAI client")
}

func TestTypeScriptClassFieldsAreConstants(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
class Assistant {
  instructions = "Ask for an order ID.";
  limits = { retries: 3 };
}
`)}}, true)
	require.NoError(t, err)
	instructions, err := root.Resolve(`constants[id="Assistant.instructions"]`)
	require.NoError(t, err)
	assert.Equal(t, "Ask for an order ID.", instructions.Value)
	assert.Equal(t, adapter.CapabilityDirect, instructions.Capability)
	retries, err := root.Resolve(`constants[id="Assistant.limits"].retries`)
	require.NoError(t, err)
	assert.Equal(t, 3.0, retries.Value)
}

func TestTypeScriptMutationsThroughThisFreezeClassFields(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
class Assistant {
  items = ["a"];
  limit = 3;
  keep = "same";
  add() {
    this.items.push("b");
    this.limit = 5;
  }
}
`)}}, true)
	require.NoError(t, err)
	capability := func(id string) adapter.Capability {
		node, err := root.Resolve(`constants[id="` + id + `"]`)
		require.NoError(t, err)
		return node.Capability
	}
	assert.Equal(t, adapter.CapabilityReadOnly, capability("Assistant.items"), "a push through this mutates the field")
	assert.Equal(t, adapter.CapabilityReadOnly, capability("Assistant.limit"), "an assignment through this mutates the field")
	assert.Equal(t, adapter.CapabilityDirect, capability("Assistant.keep"))
}

func TestTypeScriptSubscriptsResolveStatically(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { Agent } from "@openai/agents";
const CONFIG = { prompt: "Help" };
const LIST = ["one", "two"];
const first = new Agent({ instructions: CONFIG["prompt"] });
const second = new Agent({ instructions: LIST[1] });
`)}}, true)
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	assert.Equal(t, "Help", agents["first"].Fields["instructions"].Value, "a string subscript reads the object entry")
	assert.Equal(t, "two", agents["second"].Fields["instructions"].Value, "a number subscript reads the array item")
}

func TestTypeScriptThisPropertyMocksShadowClassFieldInstances(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import OpenAI from "openai";
class Assistant {
  client = new OpenAI();
  constructor() {
    this.client = mockClient();
  }
  ask() {
    return this.client.chat.completions.create({ model: "gpt-5", messages: [{ role: "user", content: "Hi" }] });
  }
}
`)}}, true)
	require.NoError(t, err)
	assert.Empty(t, root.Fields["prompts"].Items, "a property set to a mock hides the field instance")
}

func TestTypeScriptThisFieldsResolveToClassConstants(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { Agent } from "@openai/agents";
class Assistant {
  instructions = "Ask for an order ID.";
  build() {
    return new Agent({ instructions: this.instructions });
  }
}
`)}}, true)
	require.NoError(t, err)
	require.Len(t, root.Fields["agents"].Items, 1)
	assert.Equal(t, "Ask for an order ID.", root.Fields["agents"].Items[0].Fields["instructions"].Value, "this.field reads the class field constant")
}

func TestTypeScriptUpdateExpressionsFreezeTheirTargets(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
let count = 1;
count++;
const stable = 2;
class Assistant {
  limit = 3;
  tick() {
    --this.limit;
  }
}
`)}}, true)
	require.NoError(t, err)
	capability := func(id string) adapter.Capability {
		node, err := root.Resolve(`constants[id="` + id + `"]`)
		require.NoError(t, err)
		return node.Capability
	}
	assert.Equal(t, adapter.CapabilityReadOnly, capability("count"), "an increment mutates the constant")
	assert.Equal(t, adapter.CapabilityReadOnly, capability("Assistant.limit"), "a decrement through this mutates the field")
	assert.Equal(t, adapter.CapabilityDirect, capability("stable"))
}

func TestTypeScriptPropertyAfterSubscriptResolves(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { Agent } from "@openai/agents";
const CONFIG = { nested: { prompt: "Help" } };
const LIST = [{ prompt: "First" }];
const first = new Agent({ instructions: CONFIG["nested"].prompt });
const second = new Agent({ instructions: LIST[0].prompt });
`)}}, true)
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	assert.Equal(t, "Help", agents["first"].Fields["instructions"].Value, "a property after a string subscript resolves")
	assert.Equal(t, "First", agents["second"].Fields["instructions"].Value, "a property after a number subscript resolves")
}

func TestTypeScriptWrappedRegisterToolCalleesAreCollected(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
const server = new McpServer({ name: "orders", version: "1" });
server.registerTool!("asserted", { description: "Asserted.", inputSchema: { type: "object" } }, handler);
(server.registerTool)("wrapped", { description: "Wrapped.", inputSchema: { type: "object" } }, handler);
`)}}, true)
	require.NoError(t, err)
	tools := map[string]*adapter.Node{}
	for _, tool := range root.Fields["tools"].Items {
		tools[tool.Fields["id"].Value.(string)] = tool
	}
	require.Len(t, tools, 2, "a wrapper on the callee does not hide the call")
	assert.Equal(t, "Asserted.", tools["asserted"].Fields["description"].Value)
	assert.Equal(t, "Wrapped.", tools["wrapped"].Fields["description"].Value)
}

func TestTypeScriptImportRequireBindsTheDefaultExport(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import OpenAI = require("openai");
const client = new OpenAI();
const reply = client.chat.completions.create({ model: "gpt-5", messages: [{ role: "user", content: "Hi" }] });
`)}}, true)
	require.NoError(t, err)
	require.Len(t, root.Fields["prompts"].Items, 1, "the TypeScript CommonJS import binds the client")
	assert.Equal(t, "reply", root.Fields["prompts"].Items[0].Fields["id"].Value)
}

func TestJavaScriptNestedRequiresDoNotShadowFileImports(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "src/main.js", Data: []byte(`
const { Agent } = require("@openai/agents");
function helper() {
  const { Agent } = require("./local");
  return new Agent({ instructions: "local" });
}
const support = new Agent({ instructions: "Help" });
`)}}, false)
	require.NoError(t, err)
	ids := []string{}
	for _, agent := range root.Fields["agents"].Items {
		ids = append(ids, agent.Fields["id"].Value.(string))
	}
	assert.Equal(t, []string{"support"}, ids, "a require inside a function does not change the imports of the file")
}

func TestTypeScriptDefaultExportClauseResolves(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.ts", Data: []byte(`
import { Agent } from "@openai/agents";
import text from "./values";
const support = new Agent({ instructions: text });
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.ts": {Path: "src/values.ts", Data: []byte("const TEXT = \"Help\";\nexport { TEXT as default };\n")},
		},
	}, true)
	require.NoError(t, err)
	require.Len(t, root.Fields["agents"].Items, 1)
	assert.Equal(t, "Help", root.Fields["agents"].Items[0].Fields["instructions"].Value, "an export clause with a default alias is the default export")
}

func TestTypeScriptDefaultExportClauseBeforeTheDeclarationResolves(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.ts", Data: []byte(`
import { Agent } from "@openai/agents";
import text from "./values";
const support = new Agent({ instructions: text });
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.ts": {Path: "src/values.ts", Data: []byte("export { TEXT as default };\nconst TEXT = \"Help\";\n")},
		},
	}, true)
	require.NoError(t, err)
	require.Len(t, root.Fields["agents"].Items, 1)
	instructions := root.Fields["agents"].Items[0].Fields["instructions"]
	assert.Equal(t, "Help", instructions.Value, "the export clause names the constant that follows it")
	assert.Equal(t, adapter.CapabilityDirect, instructions.Capability)
}

func TestJavaScriptBlockDeclarationsHideARequireOfTheFunction(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "src/main.js", Data: []byte(`
function helper(flag) {
  const { Agent } = require("@openai/agents");
  if (flag) {
    const Agent = makeLocalAgent();
    return new Agent({ instructions: "local" });
  }
  return new Agent({ instructions: "Help" });
}
`)}}, false)
	require.NoError(t, err)
	require.Len(t, root.Fields["agents"].Items, 1, "the block declaration hides the require of the function inside the block")
	assert.Equal(t, "Help", root.Fields["agents"].Items[0].Fields["instructions"].Value)
}

func TestTypeScriptTypeOnlyExportsAreNotImportable(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.ts", Data: []byte(`
import { Agent } from "@openai/agents";
import { TEXT, OTHER, PUBLIC } from "./values";
const erased = new Agent({ instructions: TEXT });
const inline = new Agent({ instructions: OTHER });
const shown = new Agent({ instructions: PUBLIC });
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.ts": {Path: "src/values.ts", Data: []byte(`const TEXT = "erased";
export type { TEXT };
const OTHER = "erased too";
export { type OTHER, PUBLIC };
const PUBLIC = "shown";
`)},
		},
	}, true)
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	assert.Equal(t, adapter.CapabilityReadOnly, agents["erased"].Fields["instructions"].Capability, "a type-only export clause exports no value")
	assert.Equal(t, adapter.CapabilityReadOnly, agents["inline"].Fields["instructions"].Capability, "a type-only specifier exports no value")
	assert.Equal(t, "shown", agents["shown"].Fields["instructions"].Value, "the value specifier of the same clause is importable")
}

func TestTypeScriptCollectionsOfMutatedImportsAreReadOnly(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.ts", Data: []byte(`
import { ITEMS, TEXT } from "./values";
ITEMS.push("b");
const WRAPPED = [ITEMS];
const MAPPED = { items: ITEMS };
const PLAIN = [TEXT];
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.ts": {Path: "src/values.ts", Data: []byte("export const ITEMS = [\"a\"];\nexport const TEXT = \"Hello\";\n")},
		},
	}, true)
	require.NoError(t, err)
	constants := map[string]*adapter.Node{}
	for _, constant := range root.Fields["constants"].Items {
		constants[constant.Fields["id"].Value.(string)] = constant
	}
	assert.Equal(t, adapter.CapabilityReadOnly, constants["WRAPPED"].Capability, "an array that holds a mutated import is read-only")
	assert.Equal(t, adapter.CapabilityReadOnly, constants["MAPPED"].Capability, "an object that holds a mutated import is read-only")
	assert.Equal(t, adapter.CapabilityDirect, constants["PLAIN"].Capability, "an array of a plain import stays writable")
}

func TestTypeScriptReExportsAreNotLocalBindings(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.ts", Data: []byte(`
import { Agent } from "@openai/agents";
import fallback, { TEXT, PUBLIC } from "./values";
const forwarded = new Agent({ instructions: TEXT });
const defaulted = new Agent({ instructions: fallback });
const shown = new Agent({ instructions: PUBLIC });
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.ts": {Path: "src/values.ts", Data: []byte(`const TEXT = "local";
export { TEXT } from "./other";
export { default } from "./other";
export const PUBLIC = "shown";
`)},
		},
	}, true)
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	assert.Equal(t, adapter.CapabilityReadOnly, agents["forwarded"].Fields["instructions"].Capability, "a re-export is not the local constant of the same name")
	assert.NotEqual(t, "local", agents["forwarded"].Fields["instructions"].Value)
	assert.Equal(t, adapter.CapabilityReadOnly, agents["defaulted"].Fields["instructions"].Capability, "a re-exported default is a value of another module")
	assert.Equal(t, "shown", agents["shown"].Fields["instructions"].Value)
}

func TestTypeScriptInlineVercelToolsAreCollected(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { generateText, tool } from "ai";
const reply = generateText({ model, tools: { search: tool({ description: "Search", parameters: { type: "object" } }) } });
function build() {
  return generateText({ model, tools: { lookup: tool({ description: "Look up", parameters: { type: "object" } }) } });
}
`)}}, true)
	require.NoError(t, err)
	tools := map[string]*adapter.Node{}
	for _, item := range root.Fields["tools"].Items {
		tools[item.Fields["id"].Value.(string)] = item
	}
	require.Len(t, tools, 2, "inline tools objects are collected")
	assert.Equal(t, "Search", tools["search"].Fields["description"].Value)
	assert.Equal(t, "Look up", tools["build.lookup"].Fields["description"].Value, "the enclosing function names an inline tool")
}

func TestTypeScriptWrappedThisAssignmentsNameTheProperty(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import OpenAI from "openai";
class Assistant {
  client!: OpenAI;
  constructor() {
    this!.client = new OpenAI();
  }
  ask() {
    return this.client.chat.completions.create({ model: "gpt-5", messages: [{ role: "user", content: "Hi" }] });
  }
}
`)}}, true)
	require.NoError(t, err)
	require.Len(t, root.Fields["prompts"].Items, 1, "a wrapped this assignment binds the same property")
}

func TestTypeScriptLangChainMessageConstructorsAreMessages(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { ChatPromptTemplate } from "@langchain/core/prompts";
import { SystemMessage, HumanMessage } from "@langchain/core/messages";
const chat = ChatPromptTemplate.fromMessages([new SystemMessage("Help {name}"), new HumanMessage({ content: "Hi" })]);
`)}}, true)
	require.NoError(t, err)
	require.Len(t, root.Fields["prompts"].Items, 1)
	messages := root.Fields["prompts"].Items[0].Fields["messages"]
	assert.Equal(t, []syncdomain.Message{
		{Role: syncdomain.RoleSystem, Content: "Help {{name}}"},
		{Role: syncdomain.RoleUser, Content: "Hi"},
	}, messages.Value, "the message classes carry their role and content")
	assert.Equal(t, adapter.CapabilityDirect, messages.Capability)
}

func TestTypeScriptFunctionsSeeLaterModuleConstants(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { Agent } from "@openai/agents";
function build() {
  return new Agent({ instructions: SYSTEM });
}
const SYSTEM = "Help";
`)}}, true)
	require.NoError(t, err)
	require.Len(t, root.Fields["agents"].Items, 1)
	assert.Equal(t, "Help", root.Fields["agents"].Items[0].Fields["instructions"].Value, "a function body runs after the module loads")
}

func TestTypeScriptBlockBindingsHideImportsInTheirBlockOnly(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { tool } from "ai";
function build(items: string[]) {
  const tools = { search: tool({ description: "Search", parameters: { type: "object" } }) };
  for (const tool of items) {
    console.log(tool);
  }
  return tools;
}
`)}}, true)
	require.NoError(t, err)
	require.Len(t, root.Fields["tools"].Items, 1, "a loop binding hides the import inside the loop only")
	assert.Equal(t, "build.search", root.Fields["tools"].Items[0].Fields["id"].Value)
}

func TestTypeScriptParametersHideOuterBindings(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { Agent } from "@openai/agents";
import { generateText, tool } from "ai";
const instructions = "Module";
function build(instructions: string) {
  return new Agent({ instructions });
}
function wrap(tool: (options: object) => object) {
  return generateText({ model, tools: { search: tool({ description: "Not ours", parameters: {} }) } });
}
const plain = new Agent({ instructions });
`)}}, true)
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	assert.NotEqual(t, adapter.CapabilityDirect, agents["build.Agent"].Fields["instructions"].Capability, "a parameter hides the module constant")
	assert.Equal(t, "Module", agents["plain"].Fields["instructions"].Value)
	assert.Empty(t, root.Fields["tools"].Items, "a parameter named tool hides the import")
}

func TestTypeScriptSwitchBindingsHideImportsInTheSwitchOnly(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { tool } from "ai";
function build(mode: number) {
  switch (mode) {
    case 1: {
      const tool = "other";
      console.log(tool);
      break;
    }
  }
  const tools = { search: tool({ description: "Search", parameters: { type: "object" } }) };
  return tools;
}
`)}}, true)
	require.NoError(t, err)
	require.Len(t, root.Fields["tools"].Items, 1, "a switch binding hides the import inside the switch only")
}

func TestTypeScriptUserIDsWithAHashAreNotPositional(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { Agent } from "@openai/agents";
const team = [new Agent({ name: "triage#1", instructions: "Help" })];
`)}}, true)
	require.NoError(t, err)
	agent, err := root.Resolve(`agents[id="triage#1"]`)
	require.NoError(t, err)
	assert.False(t, adapter.IsPositionalID(agent), "a name is an identity whatever characters it has")
}

func TestJavaScriptFunctionParametersHideImportsInTheFunctionOnly(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "src/main.js", Data: []byte(`
import { tool } from "ai";
const handler = function (tool) { return tool; };
function* steps(tool) { yield tool; }
const tools = { search: tool({ description: "Search", parameters: { type: "object" } }) };
`)}}, false)
	require.NoError(t, err)
	require.Len(t, root.Fields["tools"].Items, 1, "a parameter of a function expression hides the import in that function only")
}

func TestTypeScriptLocalFunctionsCatchParametersAndModuleBlocksHideImports(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { Agent } from "@openai/agents";
import { tool } from "ai";
const SYSTEM = "Help";
function build() {
  function Agent(options) { return options; }
  return new Agent({ instructions: "local function" });
}
function handle() {
  try { risky(); } catch (tool) { tool({ description: "Not ours", parameters: {} }); }
  const tools = { search: tool({ description: "Ours", parameters: { type: "object" } }) };
  return tools;
}
function nested() {
  class Agent {}
  const SYSTEM = () => "dynamic";
  return new Agent({ instructions: SYSTEM });
}
if (flag) {
  const Agent = makeLocal();
  const blocked = new Agent({ instructions: "module block" });
}
const plain = new Agent({ instructions: SYSTEM });
`)}}, true)
	require.NoError(t, err)
	ids := []string{}
	for _, agent := range root.Fields["agents"].Items {
		ids = append(ids, agent.Fields["id"].Value.(string))
	}
	assert.Equal(t, []string{"plain"}, ids, "a local function, a local class, and a block declaration at module level hide the import")
	assert.Equal(t, "Help", root.Fields["agents"].Items[0].Fields["instructions"].Value)
	require.Len(t, root.Fields["tools"].Items, 1, "a catch parameter hides the import in the catch clause only")
	assert.Equal(t, "Ours", root.Fields["tools"].Items[0].Fields["description"].Value)
}

func TestJavaScriptVarHidesImportsInTheWholeFunction(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "src/main.js", Data: []byte(`
import { tool } from "ai";
function build(flag) {
  if (flag) {
    var tool = makeLocal();
  }
  const tools = { search: tool({ description: "Not ours", parameters: {} }) };
  return tools;
}
const tools = { lookup: tool({ description: "Ours", parameters: { type: "object" } }) };
`)}}, false)
	require.NoError(t, err)
	require.Len(t, root.Fields["tools"].Items, 1, "a var in a branch hides the import in the whole function")
	assert.Equal(t, "Ours", root.Fields["tools"].Items[0].Fields["description"].Value)
}

func TestJavaScriptVarRequiresHoistToTheFunction(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "src/main.js", Data: []byte(`
function build(flag) {
  if (flag) {
    var { Agent } = require("@openai/agents");
  }
  return new Agent({ instructions: "hoisted" });
}
`)}}, false)
	require.NoError(t, err)
	require.Len(t, root.Fields["agents"].Items, 1, "a var require is visible in the whole function")
	assert.Equal(t, "hoisted", root.Fields["agents"].Items[0].Fields["instructions"].Value)
}

func TestTypeScriptInstanceFieldsSeeLaterConstants(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { Agent } from "@openai/agents";
class Bot {
  agent = new Agent({ instructions: SYSTEM });
  static label = LATER;
}
const SYSTEM = "Help";
const LATER = "late";
`)}}, true)
	require.NoError(t, err)
	require.Len(t, root.Fields["agents"].Items, 1)
	assert.Equal(t, "Help", root.Fields["agents"].Items[0].Fields["instructions"].Value, "an instance field runs in the constructor, after the module loads")
	constants := map[string]*adapter.Node{}
	for _, constant := range root.Fields["constants"].Items {
		constants[constant.Fields["id"].Value.(string)] = constant
	}
	assert.Nil(t, constants["Bot.label"], "a static field runs with the class, before the later constant")
}

func TestTypeScriptBlockConstantsBindInTheirBlockOnly(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { Agent } from "@openai/agents";
function build(flag: boolean) {
  const SYSTEM = "outer";
  if (flag) {
    const SYSTEM = "inner";
    return new Agent({ instructions: SYSTEM });
  }
  return new Agent({ instructions: SYSTEM });
}
function later(flag: boolean) {
  const first = new Agent({ instructions: TEXT });
  if (flag) {
    const TEXT = "block";
  }
  return first;
}
const TEXT = "module";
`)}}, true)
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	require.Len(t, agents, 3)
	assert.Equal(t, "inner", agents["build.Agent"].Fields["instructions"].Value, "a use in the block sees the block constant")
	assert.Equal(t, "outer", agents["build.Agent#2"].Fields["instructions"].Value, "a use outside the block sees the function constant")
	assert.Equal(t, "module", agents["later.first"].Fields["instructions"].Value, "a block constant does not reach a use outside its block")
	ids := []string{}
	for _, constant := range root.Fields["constants"].Items {
		ids = append(ids, constant.Fields["id"].Value.(string))
	}
	assert.ElementsMatch(t, []string{"build.SYSTEM", "TEXT"}, ids, "a block constant is not a constant of the file")
}

func TestTypeScriptDestructuringDefaultsAreNotBindings(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { Agent } from "@openai/agents";
const SYSTEM = "Help";
function make({ model = Agent, options: { text = SYSTEM } = {} }) {
  return new Agent({ instructions: SYSTEM });
}
function pick([first = SYSTEM]) {
  return new Agent({ instructions: SYSTEM });
}
`)}}, true)
	require.NoError(t, err)
	require.Len(t, root.Fields["agents"].Items, 2, "a default named Agent does not hide the import")
	for _, agent := range root.Fields["agents"].Items {
		assert.Equal(t, "Help", agent.Fields["instructions"].Value, "a default named SYSTEM does not hide the module constant")
	}
}

func TestTypeScriptSingleArrowParametersHideBindings(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { Agent } from "@openai/agents";
const instructions = "Module";
const make = Agent => new Agent({ instructions: "Not ours" });
const describe = instructions => new Agent({ instructions });
const plain = new Agent({ instructions });
`)}}, true)
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	require.Len(t, agents, 2, "a parameter named Agent hides the import in its arrow function")
	assert.NotEqual(t, adapter.CapabilityDirect, agents["describe.Agent"].Fields["instructions"].Capability, "a parameter hides the module constant")
	assert.Equal(t, "Module", agents["plain"].Fields["instructions"].Value)
}

func TestJavaScriptRestParametersHideImports(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "src/main.js", Data: []byte(`
import { tool } from "ai";
function collect(...tool) {
  const hidden = tool({ description: "Hidden", parameters: { type: "object" } });
  return hidden;
}
const tools = { search: tool({ description: "Search", parameters: { type: "object" } }) };
`)}}, false)
	require.NoError(t, err)
	require.Len(t, root.Fields["tools"].Items, 1, "a rest parameter hides the import in its function")
	assert.Equal(t, "Search", root.Fields["tools"].Items[0].Fields["description"].Value)
}

func TestTypeScriptImportsResolveOnlyExportedConstants(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.ts", Data: []byte(`
import { Agent } from "@openai/agents";
import * as values from "./values";
import { PRIVATE, RENAMED } from "./values";
const hidden = new Agent({ instructions: values.PRIVATE });
const shown = new Agent({ instructions: values.PUBLIC });
const named = new Agent({ instructions: PRIVATE });
const renamed = new Agent({ instructions: RENAMED });
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.ts": {Path: "src/values.ts", Data: []byte(`const PRIVATE = "hidden";
export const PUBLIC = "shown";
const LOCAL = "aliased";
export { LOCAL as RENAMED };
`)},
		},
	}, true)
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	assert.NotEqual(t, "hidden", agents["hidden"].Fields["instructions"].Value, "a private constant is not importable")
	assert.Equal(t, "shown", agents["shown"].Fields["instructions"].Value)
	assert.NotEqual(t, "hidden", agents["named"].Fields["instructions"].Value)
	assert.Equal(t, "aliased", agents["renamed"].Fields["instructions"].Value, "an export clause alias is importable")
}

func TestTypeScriptMCPNamesIdentifyAssignedToolsAndPrompts(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { McpServer } from "@modelcontextprotocol/sdk";
const server = new McpServer({});
const lookup = server.tool("search", "Search orders.", { type: "object" }, handler);
const greeting = server.prompt("greet", "Greets.", handler);
const registered = server.registerPrompt("summarize", { description: "Summarizes." }, handler);
const tool = server.registerTool("audit", { description: "Audits." }, handler);
`)}}, true)
	require.NoError(t, err)
	tools := map[string]*adapter.Node{}
	for _, tool := range root.Fields["tools"].Items {
		tools[tool.Fields["id"].Value.(string)] = tool
	}
	prompts := map[string]*adapter.Node{}
	for _, prompt := range root.Fields["prompts"].Items {
		prompts[prompt.Fields["id"].Value.(string)] = prompt
	}
	assert.Len(t, tools, 2)
	assert.Contains(t, tools, "search", "the MCP name is the id of an assigned tool")
	assert.Contains(t, tools, "audit")
	assert.Len(t, prompts, 2)
	assert.Contains(t, prompts, "greet", "the MCP name is the id of an assigned prompt")
	assert.Contains(t, prompts, "summarize")
}

func TestJavaScriptModuleExportsValueIsTheDefaultExport(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.js", Data: []byte(`
const { Agent } = require("@openai/agents");
const named = require("./named");
const literal = require("./literal");
const support = new Agent({ instructions: named });
const plain = new Agent({ instructions: literal });
`)},
		Dependencies: map[string]adapter.Document{
			"src/named.js":   {Path: "src/named.js", Data: []byte("const TEXT = \"Help\";\nmodule.exports = TEXT;\n")},
			"src/literal.js": {Path: "src/literal.js", Data: []byte("module.exports = \"Plain\";\n")},
		},
	}, false)
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	assert.Equal(t, "Help", agents["support"].Fields["instructions"].Value, "a module that exports one identifier exports it as the default")
	assert.Equal(t, adapter.CapabilityDirect, agents["support"].Fields["instructions"].Capability)
	assert.Equal(t, "Plain", agents["plain"].Fields["instructions"].Value, "a module that exports one literal exports it as the default")
}

func TestTypeScriptStaticMethodsAreScopesOfTheirOwn(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { Agent } from "@openai/agents";
class Bot {
  static TEXT = "field";
  static make() {
    const TEXT = "static";
    return new Agent({ instructions: TEXT });
  }
  make() {
    const TEXT = "instance";
    return new Agent({ instructions: TEXT });
  }
  static bare() {
    return new Agent({ instructions: TEXT });
  }
}
`)}}, true)
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	require.Len(t, agents, 3)
	assert.Equal(t, "static", agents["Bot.static.make.Agent"].Fields["instructions"].Value, "a static method is a scope of the class")
	assert.Equal(t, "instance", agents["Bot.make.Agent"].Fields["instructions"].Value, "an instance method of the same name is another scope")
	assert.Equal(t, adapter.CapabilityReadOnly, agents["Bot.static.bare.Agent"].Fields["instructions"].Capability, "a bare name in a static method does not read the static field")
}

func TestTypeScriptAssignedRegisterToolFallsBackToTheVariable(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { McpServer } from "@modelcontextprotocol/sdk";
const server = new McpServer({});
const lookup = server.registerTool(dynamicName, { description: "Dynamic." }, handler);
const named = server.registerTool("audit", { description: "Audit." }, handler);
server.registerTool(otherName, { description: "Dropped." }, handler);
`)}}, true)
	require.NoError(t, err)
	tools := map[string]*adapter.Node{}
	for _, tool := range root.Fields["tools"].Items {
		tools[tool.Fields["id"].Value.(string)] = tool
	}
	require.Len(t, tools, 2, "an unassigned tool without a static name has no id")
	assert.Equal(t, "Dynamic.", tools["lookup"].Fields["description"].Value, "an assigned tool without a static name takes the variable")
	assert.Equal(t, "Audit.", tools["audit"].Fields["description"].Value, "the static name wins over the variable")
}

func TestTypeScriptImportsOfReassignedNamesAreReadOnlyBindings(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.ts", Data: []byte(`
import { Agent } from "@openai/agents";
import { TEXT, PLAIN } from "./values";
const support = new Agent({ instructions: TEXT });
const other = new Agent({ instructions: PLAIN });
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.ts": {Path: "src/values.ts", Data: []byte("export let TEXT = \"first\";\nTEXT = \"second\";\nexport const PLAIN = \"plain\";\n")},
		},
	}, true)
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	instructions := agents["support"].Fields["instructions"]
	assert.Equal(t, "first", instructions.Value, "the import binds the first literal")
	assert.Equal(t, adapter.CapabilityReadOnly, instructions.Capability)
	// An assignment to a declared name is a mutation of the binding in the
	// TypeScript adapter.
	assert.Equal(t, "mutated constants are read-only", instructions.Reason)
	assert.Empty(t, instructions.Code, "a reassigned name is not an unresolved import")
	assert.Equal(t, "plain", agents["other"].Fields["instructions"].Value)
}

func TestTypeScriptExportEqualsIsImportable(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.ts", Data: []byte(`
import { Agent } from "@openai/agents";
import whole = require("./whole");
import values = require("./values");
import literal = require("./literal");
const support = new Agent({ instructions: whole });
const named = new Agent({ instructions: values.TEXT });
const plain = new Agent({ instructions: literal });
`)},
		Dependencies: map[string]adapter.Document{
			"src/whole.ts":   {Path: "src/whole.ts", Data: []byte("const TEXT = \"Whole\";\nexport = TEXT;\n")},
			"src/values.ts":  {Path: "src/values.ts", Data: []byte("const TEXT = \"Named\";\nexport = { TEXT };\n")},
			"src/literal.ts": {Path: "src/literal.ts", Data: []byte("export = \"Literal\";\n")},
		},
	}, true)
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	assert.Equal(t, "Whole", agents["support"].Fields["instructions"].Value, "export = TEXT exports the value as the module")
	assert.Equal(t, "Named", agents["named"].Fields["instructions"].Value, "export = { TEXT } exports its entries")
	assert.Equal(t, "Literal", agents["plain"].Fields["instructions"].Value, "export = \"text\" exports the literal as the module")
}

func TestTypeScriptConstructorParametersKeepTheClientKind(t *testing.T) {
	typed, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import OpenAI from "openai";
class Typed {
  client: OpenAI;
  constructor(client: OpenAI) {
    this.client = client;
  }
  ask() {
    return this.client.chat.completions.create({ model: "gpt-5", messages: [{ role: "user", content: "typed" }] });
  }
}
`)}}, true)
	require.NoError(t, err)
	require.Len(t, typed.Fields["prompts"].Items, 1, "the type of the parameter names the client kind")

	plain, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.js", Data: []byte(`
const OpenAI = require("openai");
class Plain {
  client = new OpenAI();
  constructor(client) {
    this.client = client;
  }
  ask() {
    return this.client.chat.completions.create({ model: "gpt-5", messages: [{ role: "user", content: "plain" }] });
  }
}
`)}}, false)
	require.NoError(t, err)
	require.Len(t, plain.Fields["prompts"].Items, 1, "a parameter without a type does not wipe the kind of the field")
}

func TestTypeScriptNamedAndNamespaceImportsShareMutations(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.ts", Data: []byte(`
import * as values from "./values";
import { LIST, OTHER, PLAIN } from "./values";
LIST.push("b");
values.OTHER.push("c");
const fromNamespace = [values.LIST];
const fromNamed = [OTHER];
const untouched = [values.PLAIN, PLAIN];
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.ts": {Path: "src/values.ts", Data: []byte("export const LIST = [\"a\"];\nexport const OTHER = [\"a\"];\nexport const PLAIN = [\"a\"];\n")},
		},
	}, true)
	require.NoError(t, err)
	constants := map[string]*adapter.Node{}
	for _, constant := range root.Fields["constants"].Items {
		constants[constant.Fields["id"].Value.(string)] = constant
	}
	assert.Equal(t, adapter.CapabilityReadOnly, constants["fromNamespace"].Capability, "a mutation through the named import freezes the namespace field")
	assert.Equal(t, adapter.CapabilityReadOnly, constants["fromNamed"].Capability, "a mutation through the namespace freezes the named import")
	assert.Equal(t, adapter.CapabilityDirect, constants["untouched"].Capability, "an untouched export stays writable through both names")
}

func TestTypeScriptStaticFieldInitializersAreScopesOfTheClass(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { Agent } from "@openai/agents";
class Bot {
  static build = () => {
    const TEXT = "static";
    return new Agent({ instructions: TEXT });
  };
  build = () => {
    const TEXT = "instance";
    return new Agent({ instructions: TEXT });
  };
}
`)}}, true)
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	require.Len(t, agents, 2)
	assert.Equal(t, "static", agents["Bot.static.build.Agent"].Fields["instructions"].Value, "a static field initializer is a scope of the class")
	assert.Equal(t, "instance", agents["Bot.build.Agent"].Fields["instructions"].Value, "an instance field of the same name is another scope")
}

func TestTypeScriptImportedAliasesShareMutations(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.ts", Data: []byte(`
import { ITEMS, ALSO, OTHER } from "./values";
ITEMS.push("b");
const wrapped = [ALSO];
const plain = [OTHER];
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.ts": {Path: "src/values.ts", Data: []byte("export const ITEMS = [\"a\"];\nexport const ALSO = ITEMS;\nexport const OTHER = [\"c\"];\n")},
		},
	}, true)
	require.NoError(t, err)
	constants := map[string]*adapter.Node{}
	for _, constant := range root.Fields["constants"].Items {
		constants[constant.Fields["id"].Value.(string)] = constant
	}
	assert.Equal(t, adapter.CapabilityReadOnly, constants["wrapped"].Capability, "an alias of the mutated import binds the same object")
	assert.Equal(t, adapter.CapabilityDirect, constants["plain"].Capability)
}

func TestTypeScriptTypedParameterPropertiesAreClients(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import OpenAI from "openai";
import Anthropic from "@anthropic-ai/sdk";
class Bot {
  constructor(private client: OpenAI, private readonly backup: Anthropic, private label: string) {}
  ask() {
    return this.client.chat.completions.create({ model: "gpt-5", messages: [{ role: "user", content: "first" }] });
  }
  fallback() {
    return this.backup.messages.create({ model: "claude", messages: [{ role: "user", content: "second" }] });
  }
}
`)}}, true)
	require.NoError(t, err)
	contents := []string{}
	for _, prompt := range root.Fields["prompts"].Items {
		messages := prompt.Fields["messages"].Value.([]syncdomain.Message)
		contents = append(contents, messages[0].Content)
	}
	assert.ElementsMatch(t, []string{"first", "second"}, contents, "the type of a parameter property names the client kind")
}

func TestTypeScriptDefaultImportOfACommonJSObjectModule(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.ts", Data: []byte(`
import { Agent } from "@openai/agents";
import values from "./values";
import equals from "./equals";
const support = new Agent({ instructions: values.TEXT });
const other = new Agent({ instructions: equals.TEXT });
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.ts": {Path: "src/values.ts", Data: []byte("const TEXT = \"Object\";\nmodule.exports = { TEXT };\n")},
			"src/equals.ts": {Path: "src/equals.ts", Data: []byte("const TEXT = \"Equals\";\nexport = { TEXT };\n")},
		},
	}, true)
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	assert.Equal(t, "Object", agents["support"].Fields["instructions"].Value, "a default import of a module.exports object binds the object")
	assert.Equal(t, "Equals", agents["other"].Fields["instructions"].Value, "a default import of an export = object binds the object")
}

func TestTypeScriptStaticClientsResolveThroughTheClassName(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import OpenAI from "openai";
class Bot {
  static client = new OpenAI();
  static ask() {
    return Bot.client.chat.completions.create({ model: "gpt-5", messages: [{ role: "user", content: "inside" }] });
  }
}
const outside = Bot.client.chat.completions.create({ model: "gpt-5", messages: [{ role: "user", content: "outside" }] });
`)}}, true)
	require.NoError(t, err)
	contents := []string{}
	for _, prompt := range root.Fields["prompts"].Items {
		messages := prompt.Fields["messages"].Value.([]syncdomain.Message)
		contents = append(contents, messages[0].Content)
	}
	assert.ElementsMatch(t, []string{"inside", "outside"}, contents, "the class name reaches the static client")
}

func TestTypeScriptTypeOnlyImportsNameParameterKinds(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import type OpenAI from "openai";
import { type Anthropic } from "@anthropic-ai/sdk";
class Bot {
  private backup: Anthropic;
  constructor(private client: OpenAI | undefined, backup: Anthropic) {
    this.backup = backup;
  }
  ask() {
    return this.client.chat.completions.create({ model: "gpt-5", messages: [{ role: "user", content: "first" }] });
  }
  fallback() {
    return this.backup.messages.create({ model: "claude", messages: [{ role: "user", content: "second" }] });
  }
}
`)}}, true)
	require.NoError(t, err)
	contents := []string{}
	for _, prompt := range root.Fields["prompts"].Items {
		messages := prompt.Fields["messages"].Value.([]syncdomain.Message)
		contents = append(contents, messages[0].Content)
	}
	assert.ElementsMatch(t, []string{"first", "second"}, contents, "a type-only import and a union type name the client kind")
}

func TestTypeScriptLocalsOfEveryFormHideOuterConstants(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import { Agent } from "@openai/agents";
const TEXT = "module";
function destructured(options: { TEXT: string }) {
  const { TEXT } = options;
  return new Agent({ instructions: TEXT });
}
function uninitialized() {
  let TEXT;
  return new Agent({ instructions: TEXT });
}
function looped(items: string[]) {
  const agents = [];
  for (const TEXT of items) {
    agents.push(new Agent({ instructions: TEXT }));
  }
  return agents;
}
const plain = new Agent({ instructions: TEXT });
`)}}, true)
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	for _, id := range []string{"destructured.Agent", "uninitialized.Agent", "looped.Agent"} {
		require.NotNil(t, agents[id], id)
		assert.Equal(t, adapter.CapabilityReadOnly, agents[id].Fields["instructions"].Capability, "%s hides the module constant", id)
	}
	assert.Equal(t, "module", agents["plain"].Fields["instructions"].Value)
}

func TestTypeScriptAnonymousClassesKeepTheirClients(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import OpenAI from "openai";
export default class {
  client = new OpenAI();
  ask() {
    return this.client.chat.completions.create({ model: "gpt-5", messages: [{ role: "user", content: "default" }] });
  }
}
const Mixin = (Base: any) => class extends Base {
  client = new OpenAI();
  ask() {
    return this.client.chat.completions.create({ model: "gpt-5", messages: [{ role: "user", content: "mixin" }] });
  }
};
`)}}, true)
	require.NoError(t, err)
	contents := []string{}
	for _, prompt := range root.Fields["prompts"].Items {
		messages := prompt.Fields["messages"].Value.([]syncdomain.Message)
		contents = append(contents, messages[0].Content)
	}
	assert.ElementsMatch(t, []string{"default", "mixin"}, contents, "a field and a method of an anonymous class share one scope")
}

func TestTypeScriptClassNameReadsTheStaticClientFirst(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import OpenAI from "openai";
import Anthropic from "@anthropic-ai/sdk";
class Bot {
  static client = new Anthropic();
  client = new OpenAI();
  static ask() {
    return Bot.client.messages.create({ model: "claude", messages: [{ role: "user", content: "static" }] });
  }
  ask() {
    return this.client.chat.completions.create({ model: "gpt-5", messages: [{ role: "user", content: "instance" }] });
  }
}
`)}}, true)
	require.NoError(t, err)
	contents := []string{}
	for _, prompt := range root.Fields["prompts"].Items {
		messages := prompt.Fields["messages"].Value.([]syncdomain.Message)
		contents = append(contents, messages[0].Content)
	}
	assert.ElementsMatch(t, []string{"static", "instance"}, contents, "the class name reads the static field, and this reads the instance field")
}

func TestTypeScriptIdentifierAssignmentsKeepOrMoveTheClientKind(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import OpenAI from "openai";
import Anthropic from "@anthropic-ai/sdk";
class Bot {
  client = new OpenAI();
  constructor() {
    const kept = this.client;
    this.client = kept;
  }
  ask() {
    return this.client.chat.completions.create({ model: "gpt-5", messages: [{ role: "user", content: "kept" }] });
  }
}
let shared;
const backup = new Anthropic();
shared = backup;
const moved = shared.messages.create({ model: "claude", messages: [{ role: "user", content: "moved" }] });
`)}}, true)
	require.NoError(t, err)
	contents := []string{}
	for _, prompt := range root.Fields["prompts"].Items {
		messages := prompt.Fields["messages"].Value.([]syncdomain.Message)
		contents = append(contents, messages[0].Content)
	}
	assert.ElementsMatch(t, []string{"kept", "moved"}, contents, "a name without a kind keeps the property kind, and a name with a kind moves it")
}

func TestTypeScriptTypedParametersAreClients(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import type OpenAI from "openai";
import Anthropic from "@anthropic-ai/sdk";
function run(client: OpenAI) {
  return client.chat.completions.create({ model: "gpt-5", messages: [{ role: "user", content: "function" }] });
}
class Bot {
  ask(client?: Anthropic) {
    return client.messages.create({ model: "claude", messages: [{ role: "user", content: "method" }] });
  }
}
`)}}, true)
	require.NoError(t, err)
	contents := []string{}
	for _, prompt := range root.Fields["prompts"].Items {
		messages := prompt.Fields["messages"].Value.([]syncdomain.Message)
		contents = append(contents, messages[0].Content)
	}
	assert.ElementsMatch(t, []string{"function", "method"}, contents, "the type of a parameter names the client kind in its function")
}

func TestTypeScriptLocalsShadowOuterClients(t *testing.T) {
	root, err := parseTypeScriptBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.ts", Data: []byte(`
import OpenAI from "openai";
const client = new OpenAI();
function untyped(client) {
  return client.chat.completions.create({ model: "gpt-5", messages: [{ role: "user", content: "untyped" }] });
}
function uninitialized() {
  let client;
  return client.chat.completions.create({ model: "gpt-5", messages: [{ role: "user", content: "uninitialized" }] });
}
function destructured(deps) {
  const { client } = deps;
  return client.chat.completions.create({ model: "gpt-5", messages: [{ role: "user", content: "destructured" }] });
}
function looped(list) {
  for (const client of list) {
    client.chat.completions.create({ model: "gpt-5", messages: [{ role: "user", content: "looped" }] });
  }
}
function aliased(other) {
  const client = other;
  return client.chat.completions.create({ model: "gpt-5", messages: [{ role: "user", content: "aliased" }] });
}
const top = client.chat.completions.create({ model: "gpt-5", messages: [{ role: "user", content: "top" }] });
`)}}, true)
	require.NoError(t, err)
	contents := []string{}
	for _, prompt := range root.Fields["prompts"].Items {
		messages := prompt.Fields["messages"].Value.([]syncdomain.Message)
		contents = append(contents, messages[0].Content)
	}
	assert.Equal(t, []string{"top"}, contents, "every local form of the name hides the module client")
}
