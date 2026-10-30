package reference

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

type frameworkCase struct {
	name, fixture, format, collection, id, sourcePath, want string
	kind                                                    syncdomain.Kind
	mode                                                    syncdomain.VariationMode
}

func TestFrameworkInspectorsReadAndWriteStaticFields(t *testing.T) {
	tests := []frameworkCase{
		{name: "OpenAI Python responses", fixture: "python/frameworks_tier1.py", format: "python", collection: "prompts", id: "openai_response", sourcePath: "instructions", want: "Answer in one paragraph.", kind: syncdomain.KindVariation},
		{name: "Anthropic Python", fixture: "python/frameworks_tier1.py", format: "python", collection: "prompts", id: "anthropic_message", sourcePath: "system", want: "Follow the support policy.", kind: syncdomain.KindVariation},
		{name: "OpenAI Agents Python", fixture: "python/frameworks_tier1.py", format: "python", collection: "agents", id: "support_agent", sourcePath: "instructions", want: "Ask for an order ID.", kind: syncdomain.KindVariation},
		{name: "LangGraph", fixture: "python/frameworks_tier1.py", format: "python", collection: "agents", id: "react_agent", sourcePath: "prompt", want: "Use the support tools.", kind: syncdomain.KindVariation},
		{name: "OpenAI function tool", fixture: "python/frameworks_tier1.py", format: "python", collection: "tools", id: "lookup_order", sourcePath: "description", want: "Look up one order.", kind: syncdomain.KindTool},
		{name: "LangChain tool", fixture: "python/frameworks_tier1.py", format: "python", collection: "tools", id: "refund_order", sourcePath: "description", want: "Refund one order.", kind: syncdomain.KindTool},
		{name: "StructuredTool", fixture: "python/frameworks_tier1.py", format: "python", collection: "tools", id: "structured", sourcePath: "description", want: "Look up one order with a structured tool.", kind: syncdomain.KindTool},
		{name: "Agent as tool", fixture: "python/frameworks_tier1.py", format: "python", collection: "tools", id: "support_tool", sourcePath: "tool_description", want: "Ask the support agent.", kind: syncdomain.KindTool},
		{name: "FastMCP tool", fixture: "python/frameworks_tier1.py", format: "python", collection: "tools", id: "mcp_lookup", sourcePath: "description", want: "Look up one order through MCP.", kind: syncdomain.KindTool},
		{name: "CrewAI agent", fixture: "python/frameworks_tier2.py", format: "python", collection: "agents", id: "crew_researcher", sourcePath: "goal", want: "Find the relevant support policy.", kind: syncdomain.KindVariation},
		{name: "CrewAI task", fixture: "python/frameworks_tier2.py", format: "python", collection: "prompts", id: "crew_task", sourcePath: "description", want: "Research the refund request.", kind: syncdomain.KindVariation},
		{name: "Pydantic AI", fixture: "python/frameworks_tier2.py", format: "python", collection: "agents", id: "pydantic_agent", sourcePath: "instructions", want: "Ask for an order ID.", kind: syncdomain.KindVariation},
		{name: "Google ADK", fixture: "python/frameworks_tier2.py", format: "python", collection: "agents", id: "adk_agent", sourcePath: "instruction", want: "Ask for an order ID.", kind: syncdomain.KindVariation},
		{name: "Google LlmAgent", fixture: "python/frameworks_tier2.py", format: "python", collection: "agents", id: "llm_agent", sourcePath: "instruction", want: "Explain refunds.", kind: syncdomain.KindVariation},
		{name: "AWS Strands", fixture: "python/frameworks_tier2.py", format: "python", collection: "agents", id: "strands_agent", sourcePath: "system_prompt", want: "Follow the support policy.", kind: syncdomain.KindVariation},
		{name: "AutoGen", fixture: "python/frameworks_tier2.py", format: "python", collection: "agents", id: "autogen_agent", sourcePath: "system_message", want: "Ask for an order ID.", kind: syncdomain.KindVariation},
		{name: "LlamaIndex prompt", fixture: "python/frameworks_tier2.py", format: "python", collection: "prompts", id: "llama_prompt", sourcePath: "template", want: "Explain {{topic}}.", kind: syncdomain.KindVariation},
		{name: "LlamaIndex tool", fixture: "python/frameworks_tier2.py", format: "python", collection: "tools", id: "llama_tool", sourcePath: "description", want: "Look up one order.", kind: syncdomain.KindTool},
		{name: "LlamaIndex agent", fixture: "python/frameworks_tier2.py", format: "python", collection: "agents", id: "llama_agent", sourcePath: "system_prompt", want: "Use the support tools.", kind: syncdomain.KindVariation},
		{name: "Semantic Kernel agent", fixture: "python/frameworks_tier2.py", format: "python", collection: "agents", id: "semantic_agent", sourcePath: "instructions", want: "Ask for an order ID.", kind: syncdomain.KindVariation},
		{name: "Semantic Kernel tool", fixture: "python/frameworks_tier2.py", format: "python", collection: "tools", id: "semantic_lookup", sourcePath: "description", want: "Look up one order with Semantic Kernel.", kind: syncdomain.KindTool},
		{name: "Pydantic AI tool", fixture: "python/frameworks_tier2.py", format: "python", collection: "tools", id: "pydantic_lookup", sourcePath: "description", want: "Look up one order with Pydantic AI.", kind: syncdomain.KindTool},
		{name: "Strands tool", fixture: "python/frameworks_tier2.py", format: "python", collection: "tools", id: "strands_lookup", sourcePath: "description", want: "Look up one order with Strands.", kind: syncdomain.KindTool},
		{name: "OpenAI TypeScript responses", fixture: "typescript/frameworks.ts", format: "typescript", collection: "prompts", id: "openaiResponse", sourcePath: "instructions", want: "Answer in one paragraph.", kind: syncdomain.KindVariation},
		{name: "Anthropic TypeScript", fixture: "typescript/frameworks.ts", format: "typescript", collection: "prompts", id: "anthropicMessage", sourcePath: "system", want: "Follow the support policy.", kind: syncdomain.KindVariation},
		{name: "Vercel generateText", fixture: "typescript/frameworks.ts", format: "typescript", collection: "prompts", id: "generated", sourcePath: "system", want: "Follow the support policy.", kind: syncdomain.KindVariation},
		{name: "Vercel tool", fixture: "typescript/frameworks.ts", format: "typescript", collection: "tools", id: "vercelLookup", sourcePath: "description", want: "Look up one order.", kind: syncdomain.KindTool},
		{name: "OpenAI Agents JS", fixture: "typescript/frameworks.ts", format: "typescript", collection: "agents", id: "supportAgent", sourcePath: "instructions", want: "Ask for an order ID.", kind: syncdomain.KindVariation},
		{name: "OpenAI Agents JS tool", fixture: "typescript/frameworks.ts", format: "typescript", collection: "tools", id: "openaiLookup", sourcePath: "description", want: "Look up one order with OpenAI Agents.", kind: syncdomain.KindTool},
		{name: "LangChain JS tool", fixture: "typescript/frameworks.ts", format: "typescript", collection: "tools", id: "langchainLookup", sourcePath: "description", want: "Look up one order with LangChain.", kind: syncdomain.KindTool},
		{name: "MCP TypeScript tool", fixture: "typescript/frameworks.ts", format: "typescript", collection: "tools", id: "mcp_lookup", sourcePath: "description", want: "Look up one order through MCP.", kind: syncdomain.KindTool},
		{name: "MCP TypeScript prompt", fixture: "typescript/frameworks.ts", format: "typescript", collection: "prompts", id: "mcp_prompt", sourcePath: "description", want: "Build one MCP prompt.", kind: syncdomain.KindVariation},
		{name: "Mastra agent", fixture: "typescript/frameworks.ts", format: "typescript", collection: "agents", id: "mastraAgent", sourcePath: "instructions", want: "Ask for an order ID with Mastra.", kind: syncdomain.KindVariation},
		{name: "Mastra tool", fixture: "typescript/frameworks.ts", format: "typescript", collection: "tools", id: "mastraLookup", sourcePath: "description", want: "Look up one order with Mastra.", kind: syncdomain.KindTool},
	}
	for _, test := range tests {
		if test.format != "python" {
			continue
		}
		t.Run(test.name, func(t *testing.T) {
			source := fixtureFile(t, test.fixture)
			mode := test.mode
			if mode == "" {
				mode = syncdomain.VariationModeAgent
			}
			field := "prompt"
			id := syncdomain.VariationID("project", "config", "default")
			if test.kind == syncdomain.KindTool {
				field = "description"
				id = syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: test.id}
			}
			reference := syncdomain.Reference{
				File: test.fixture, Format: test.format,
				Select: test.collection + `[id="` + test.id + `"]`,
				Map:    map[string]string{field: test.sourcePath},
			}
			projection, err := ParseSource(sourceBundle(test.fixture, source), id, mode, reference)
			require.NoError(t, err)
			assert.Equal(t, test.want, primaryText(t, projection))
			update := "Updated " + test.want
			result, err := UpdateSource(
				sourceBundle(test.fixture, source), id, mode, reference,
				projectionWithText(projection, update),
			)
			require.NoError(t, err)
			updated := applyConformanceEdits(t, source, directEdits(t, result))
			reparsed, err := ParseSource(sourceBundle(test.fixture, updated), id, mode, reference)
			require.NoError(t, err)
			assert.Equal(t, update, primaryText(t, reparsed))
		})
	}
}

func TestFrameworkMessageListsAndBareCallIDs(t *testing.T) {
	messageTests := []struct {
		name, fixture, format, id string
	}{
		{name: "OpenAI Python chat", fixture: "python/frameworks_tier1.py", format: "python", id: "openai_chat"},
		{name: "Anthropic Python messages", fixture: "python/frameworks_tier1.py", format: "python", id: "anthropic_message"},
		{name: "OpenAI TypeScript chat", fixture: "typescript/frameworks.ts", format: "typescript", id: "openaiChat"},
		{name: "Anthropic TypeScript messages", fixture: "typescript/frameworks.ts", format: "typescript", id: "anthropicMessage"},
		{name: "Vercel streamText messages", fixture: "typescript/frameworks.ts", format: "typescript", id: "streamed"},
	}
	for _, test := range messageTests {
		if test.format != "python" {
			continue
		}
		t.Run(test.name, func(t *testing.T) {
			source := fixtureFile(t, test.fixture)
			id := syncdomain.VariationID("project", "config", "default")
			reference := syncdomain.Reference{
				File: test.fixture, Format: test.format, Select: `prompts[id="` + test.id + `"]`,
				Map: map[string]string{"messages": "messages"},
			}
			projection, err := ParseSource(
				sourceBundle(test.fixture, source), id, syncdomain.VariationModeCompletion, reference,
			)
			require.NoError(t, err)
			messages := append([]syncdomain.Message{}, (*projection.(syncdomain.VariationProjection).Messages)...)
			require.NotEmpty(t, messages)
			messages[0].Content = "Updated framework message."
			result, err := UpdateSource(
				sourceBundle(test.fixture, source), id, syncdomain.VariationModeCompletion, reference,
				syncdomain.VariationProjection{
					Mode: syncdomain.VariationModeCompletion, Messages: &messages,
				},
			)
			require.NoError(t, err)
			assert.Equal(t, CapabilityDirect, result.Fields[syncdomain.FieldMessages].Capability)
			applyConformanceEdits(t, source, result.Fields[syncdomain.FieldMessages].Edits)
		})
	}

	bareTests := []struct {
		name, fixture, format, id, path, want string
	}{
		{name: "OpenAI Python bare call", fixture: "python/frameworks_tier1.py", format: "python", id: "bare_calls.openai.responses.create", path: "instructions", want: "Bare response prompt."},
		{name: "Anthropic Python bare call", fixture: "python/frameworks_tier1.py", format: "python", id: "bare_calls.anthropic.messages.create", path: "system", want: "Bare Anthropic prompt."},
		{name: "OpenAI TypeScript bare call", fixture: "typescript/frameworks.ts", format: "typescript", id: "bareCalls.openai.responses.create", path: "instructions", want: "Bare OpenAI response."},
		{name: "Anthropic TypeScript bare call", fixture: "typescript/frameworks.ts", format: "typescript", id: "bareCalls.anthropic.messages.create", path: "system", want: "Bare Anthropic message."},
	}
	for _, test := range bareTests {
		if test.format != "python" {
			continue
		}
		t.Run(test.name, func(t *testing.T) {
			source := fixtureFile(t, test.fixture)
			id := syncdomain.VariationID("project", "config", "default")
			reference := syncdomain.Reference{
				File: test.fixture, Format: test.format, Select: `prompts[id="` + test.id + `"]`,
				Map: map[string]string{"prompt": test.path},
			}
			projection, err := ParseSource(sourceBundle(test.fixture, source), id, syncdomain.VariationModeAgent, reference)
			require.NoError(t, err)
			assert.Equal(t, test.want, *projection.(syncdomain.VariationProjection).Instructions)
		})
	}
}

func TestFrameworkSchemasAndFunctionBuiltPromptsStayReadOnly(t *testing.T) {
	tests := []struct {
		name, fixture, format, selectPath, field, path string
		kind                                           syncdomain.Kind
	}{
		{name: "Pydantic type hints", fixture: "python/frameworks_tier2.py", format: "python", selectPath: `tools[id="pydantic_lookup"]`, field: "schema", path: "schema", kind: syncdomain.KindTool},
		{name: "FastMCP prompt function", fixture: "python/frameworks_tier1.py", format: "python", selectPath: `prompts[id="mcp_prompt"]`, field: "prompt", path: "template", kind: syncdomain.KindVariation},
		{name: "Pydantic system prompt function", fixture: "python/frameworks_tier2.py", format: "python", selectPath: `prompts[id="pydantic_system_prompt"]`, field: "prompt", path: "template", kind: syncdomain.KindVariation},
		{name: "LangChain Zod schema", fixture: "typescript/frameworks.ts", format: "typescript", selectPath: `tools[id="langchainLookup"]`, field: "schema", path: "schema", kind: syncdomain.KindTool},
		{name: "MCP Zod schema", fixture: "typescript/frameworks.ts", format: "typescript", selectPath: `tools[id="mcp_lookup"]`, field: "schema", path: "schema", kind: syncdomain.KindTool},
	}
	for _, test := range tests {
		if test.format != "python" {
			continue
		}
		t.Run(test.name, func(t *testing.T) {
			source := fixtureFile(t, test.fixture)
			id := syncdomain.VariationID("project", "config", "default")
			if test.kind == syncdomain.KindTool {
				id = syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "tool"}
			}
			reference := syncdomain.Reference{
				File: test.fixture, Format: test.format, Select: test.selectPath,
				Map: map[string]string{test.field: test.path},
			}
			projection, err := ParseSource(sourceBundle(test.fixture, source), id, syncdomain.VariationModeAgent, reference)
			require.NoError(t, err)
			state, ok := syncdomain.ProjectionFieldState(projection, syncdomain.Field(test.field))
			require.True(t, ok)
			assert.Equal(t, syncdomain.CapabilityReadOnly, state.Capability)
		})
	}
}
