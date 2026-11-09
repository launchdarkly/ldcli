import Anthropic from "@anthropic-ai/sdk";
import { Agent, tool as agentTool } from "@openai/agents";
import { Agent as MastraAgent } from "@mastra/core/agent";
import { createTool } from "@mastra/core/tools";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { tool as langchainTool } from "@langchain/core/tools";
import { generateText, streamText, tool } from "ai";
import OpenAI from "openai";

const openai = new OpenAI();
const anthropic = new Anthropic();
const server = new McpServer({ name: "support", version: "1.0.0" });

const openaiChat = openai.chat.completions.create({
  model: "gpt-5",
  messages: [{ role: "system", content: "Answer support questions." }],
  tools: [{ type: "function", name: "lookup" }],
});

const openaiResponse = openai.responses.create({
  instructions: "Answer in one paragraph.",
});

const anthropicMessage = anthropic.messages.create({
  system: "Follow the support policy.",
  messages: [{ role: "user", content: "Help with an order." }],
  tools: [{ name: "lookup", description: "Look up an order.", input_schema: { type: "object" } }],
});

const generated = generateText({
  system: "Follow the support policy.",
  prompt: "Explain refunds.",
  tools: {},
});

const streamed = streamText({
  messages: [{ role: "system", content: "Stream support answers." }],
});

const vercelLookup = tool({
  description: "Look up one order.",
  inputSchema: { type: "object" },
});

const supportAgent = new Agent({
  name: "Support",
  instructions: "Ask for an order ID.",
  model: "gpt-5",
  tools: [],
});

const openaiLookup = agentTool({
  name: "openai_lookup",
  description: "Look up one order with OpenAI Agents.",
  parameters: { type: "object" },
});

const langchainLookup = langchainTool(lookupOrder, {
  name: "langchain_lookup",
  description: "Look up one order with LangChain.",
  schema: z.object({ orderId: z.string() }),
});

server.tool(
  "mcp_lookup",
  "Look up one order through MCP.",
  { orderId: z.string() },
  lookupOrder,
);

server.registerPrompt("mcp_prompt", {
  description: "Build one MCP prompt.",
}, promptHandler);

const mastraAgent = new MastraAgent({
  name: "Support",
  instructions: "Ask for an order ID with Mastra.",
  model: "openai/gpt-5",
});

const mastraLookup = createTool({
  id: "mastra_lookup",
  description: "Look up one order with Mastra.",
  inputSchema: { type: "object" },
});

function bareCalls() {
  openai.responses.create({ instructions: "Bare OpenAI response." });
  anthropic.messages.create({ system: "Bare Anthropic message.", messages: [] });
}
