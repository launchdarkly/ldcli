import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";

const server = new McpServer({ name: "orders", version: "1.0.0" });

server.registerTool("lookup_order", {
  description: "Look up one order.",
  inputSchema: {
    type: "object",
    properties: { orderId: { type: "string" } },
    required: ["orderId"],
  },
}, lookupOrder);

server.registerTool("refund_order", {
  description: `Refund one order.`,
  inputSchema: {
    type: "object",
    properties: { orderId: { type: "string" }, reason: { type: "string" } },
    required: ["orderId", "reason"],
  },
}, refundOrder);
