package all

import (
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	"github.com/launchdarkly/ldcli/internal/sync/adapter"
	"github.com/launchdarkly/ldcli/internal/sync/adapter/adaptertest"
	"github.com/launchdarkly/ldcli/internal/sync/adapter/code"
)

func TestTypeScriptLangChainAndQualifiedConstant(t *testing.T) {
	source := fixtureFile(t, "typescript/langchain.ts")
	id := syncdomain.VariationID("project", "config", "default")
	tests := []struct {
		name, selectPath, field, mapPath string
		mode                             syncdomain.VariationMode
	}{
		{name: "messages", selectPath: `prompts[id="chat"]`, field: "messages", mapPath: "messages", mode: syncdomain.VariationModeCompletion},
		{name: "constant messages", selectPath: `prompts[id="sharedChat"]`, field: "messages", mapPath: "messages", mode: syncdomain.VariationModeCompletion},
		{name: "template", selectPath: `prompts[id="simple"]`, field: "prompt", mapPath: "template", mode: syncdomain.VariationModeAgent},
		{name: "constructor", selectPath: `prompts[id="constructor"]`, field: "messages", mapPath: "messages", mode: syncdomain.VariationModeCompletion},
		{name: "function constant", selectPath: `constants[id="build.prompt"]`, field: "prompt", mapPath: "$", mode: syncdomain.VariationModeAgent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reference := syncdomain.Reference{
				File: "langchain.ts", Format: "typescript", Select: test.selectPath,
				Map: map[string]string{test.field: test.mapPath},
			}
			projection, err := adapter.ParseSource(adaptertest.Bundle(reference.File, source), id, reference)
			require.NoError(t, err)
			assert.NotNil(t, projection)
		})
	}
}

func TestTypeScriptAndJavaScriptConstantLiteralKinds(t *testing.T) {
	source := []byte(`const CONFIG = {
  single: 'one',
  double: "two",
  template: ` + "`three`" + `,
  retries: 3,
  enabled: true,
  models: ["gpt-5", "claude-sonnet"],
  nested: { prompt: "Help" },
};`)
	for _, format := range []string{"typescript", "javascript"} {
		t.Run(format, func(t *testing.T) {
			root, err := adapter.ParseDocument(adapter.Document{Path: "config.js", Data: source}, format)
			require.NoError(t, err)
			tests := []struct {
				path string
				want any
			}{
				{path: `constants[id="CONFIG"].single`, want: "one"},
				{path: `constants[id="CONFIG"].double`, want: "two"},
				{path: `constants[id="CONFIG"].template`, want: "three"},
				{path: `constants[id="CONFIG"].retries`, want: 3.0},
				{path: `constants[id="CONFIG"].enabled`, want: true},
				{path: `constants[id="CONFIG"].models`, want: []any{"gpt-5", "claude-sonnet"}},
				{path: `constants[id="CONFIG"].nested.prompt`, want: "Help"},
			}
			for _, test := range tests {
				node, err := root.Resolve(test.path)
				require.NoError(t, err)
				assert.Equal(t, test.want, node.Value)
			}
		})
	}
}

func TestDollarPlaceholderTranslationRoundTrip(t *testing.T) {
	source := []byte(`import { generateText } from "ai";
const result = generateText({ prompt: "Hello ${customer}." });
`)
	id := syncdomain.VariationID("project", "config", "default")
	reference := syncdomain.Reference{
		File: "prompt.ts", Format: "typescript", Select: `prompts[id="result"]`,
		Map: map[string]string{"prompt": "prompt"},
	}
	projection, err := adapter.ParseSource(
		adaptertest.Bundle(reference.File, source), id, reference,
	)
	require.NoError(t, err)
	assert.Equal(t, "Hello {{customer}}.", *projection.(syncdomain.VariationProjection).Instructions)

	next := "Welcome {{customer_name}}."
	result, err := adapter.UpdateSource(
		adaptertest.Bundle(reference.File, source), id, reference,
		syncdomain.VariationProjection{Mode: syncdomain.VariationModeAgent, Instructions: &next},
	)
	require.NoError(t, err)
	updated := adaptertest.ApplyChecked(t, source, adaptertest.DirectEdits(t, result))
	assert.Contains(t, string(updated), `"Welcome ${customer_name}."`)
}

func TestTypeScriptToolUsesImportAwareInspector(t *testing.T) {
	source := []byte(`import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
const server = new McpServer({ name: "orders", version: "1" });
server.registerTool("lookup", {
  description: "Look up an order.",
  inputSchema: { type: "object" },
}, handler);
`)
	id := syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "lookup"}
	reference := syncdomain.Reference{
		File: "tools.ts", Format: "typescript", Select: `tools[id="lookup"]`,
		Map: map[string]string{"description": "description", "schema": "inputSchema"},
	}
	projection, err := adapter.ParseSource(adaptertest.Bundle(reference.File, source), id, reference)
	require.NoError(t, err)
	tool := projection.(syncdomain.ToolProjection)
	assert.Equal(t, "Look up an order.", *tool.Description)
	assert.Equal(t, "object", (*tool.Schema)["type"])

	description := "Look up one order and its status."
	schema := map[string]any{"type": "object", "required": []any{"orderId"}}
	update, err := adapter.UpdateSource(
		adaptertest.Bundle(reference.File, source), id, reference,
		syncdomain.ToolProjection{Description: &description, Schema: &schema},
	)
	require.NoError(t, err)
	assert.Equal(t, adapter.CapabilityDirect, update.Fields[syncdomain.FieldDescription].Capability)
	assert.Equal(t, adapter.CapabilityDirect, update.Fields[syncdomain.FieldSchema].Capability)
}

func TestTypeScriptAgentProjection(t *testing.T) {
	source := []byte(`import { Agent } from "@openai/agents";
const support = new Agent({
  instructions: "Help with orders.",
  model: "gpt-5",
});
`)
	id := syncdomain.VariationID("project", "support", "default")
	reference := syncdomain.Reference{
		File: "agents.ts", Format: "typescript", Select: `agents[id="support"]`,
		Map: map[string]string{"prompt": "instructions", "model": "model"},
	}

	projection, err := adapter.ParseSource(adaptertest.Bundle(reference.File, source), id, reference)

	require.NoError(t, err)
	variation := projection.(syncdomain.VariationProjection)
	assert.Equal(t, "Help with orders.", *variation.Instructions)
	assert.Equal(t, "gpt-5", *variation.Model)
}

func TestTypeScriptPromptProjection(t *testing.T) {
	source := []byte(`import { PromptTemplate } from "@langchain/core/prompts";
const summary = new PromptTemplate({
  template: "Summarize the case.",
});
`)
	id := syncdomain.VariationID("project", "summary", "default")
	reference := syncdomain.Reference{
		File: "prompts.ts", Format: "typescript", Select: `prompts[id="summary"]`,
		Map: map[string]string{"prompt": "template"},
	}

	projection, err := adapter.ParseSource(adaptertest.Bundle(reference.File, source), id, reference)

	require.NoError(t, err)
	assert.Equal(t, "Summarize the case.", *projection.(syncdomain.VariationProjection).Instructions)
}

func FuzzTypeScriptStringRoundTrip(f *testing.F) {
	for _, value := range []string{"plain", "quotes ' \" and slash \\", "line one\nline two", "Unicode 🌍"} {
		f.Add(value)
	}
	f.Fuzz(func(t *testing.T, value string) {
		if !utf8.ValidString(value) {
			t.Skip()
		}
		for _, quote := range []byte{'\'', '"'} {
			encoded, err := code.EncodeJSString(value, quote)
			require.NoError(t, err)
			decoded, _, err := code.DecodeJSString(encoded)
			require.NoError(t, err)
			require.Equal(t, value, decoded)
		}
	})
}
