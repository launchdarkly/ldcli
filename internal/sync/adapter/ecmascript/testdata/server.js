import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";

const server = new McpServer({ name: "weather", version: "1.0.0" });

server.registerTool("weather", {
  description: "Return the weather for one city.",
  inputSchema: {
    type: "object",
    properties: { city: { type: "string" } },
    required: ["city"],
  },
}, getWeather);
