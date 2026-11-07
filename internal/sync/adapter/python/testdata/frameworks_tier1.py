from anthropic import Anthropic
from agents import Agent, function_tool
from langchain_core.tools import StructuredTool, tool
from langgraph.prebuilt import create_react_agent
from mcp.server.fastmcp import FastMCP
from openai import OpenAI

openai = OpenAI()
anthropic = Anthropic()
mcp = FastMCP("support")

openai_chat = openai.chat.completions.create(
    model="gpt-5",
    messages=[{"role": "system", "content": "Answer support questions."}],
    tools=[{"type": "function", "name": "lookup"}],
)

openai_response = openai.responses.create(
    model="gpt-5",
    instructions="Answer in one paragraph.",
)

anthropic_message = anthropic.messages.create(
    model="claude-sonnet",
    system="Follow the support policy.",
    messages=[{"role": "user", "content": "Help with an order."}],
    tools=[{"name": "lookup", "description": "Look up an order.", "input_schema": {"type": "object"}}],
)

support_agent = Agent(
    name="Support",
    instructions="Ask for an order ID.",
    model="gpt-5",
)

react_agent = create_react_agent(
    "gpt-5",
    [lookup_order],
    prompt="Use the support tools.",
)


@function_tool
def lookup_order(order_id: str) -> str:
    """Look up one order."""
    return order_id


@tool
def refund_order(order_id: str) -> str:
    """Refund one order."""
    return order_id


structured = StructuredTool.from_function(
    lookup_order,
    name="structured_lookup",
    description="Look up one order with a structured tool.",
)

support_tool = support_agent.as_tool(
    tool_name="support_tool",
    tool_description="Ask the support agent.",
)


@mcp.tool()
def mcp_lookup(order_id: str) -> str:
    """Look up one order through MCP."""
    return order_id


@mcp.prompt()
def mcp_prompt(topic: str) -> str:
    """Build one support prompt."""
    return f"Explain {topic}"


def bare_calls():
    openai.responses.create(model="gpt-5", instructions="Bare response prompt.")
    anthropic.messages.create(model="claude-sonnet", system="Bare Anthropic prompt.", messages=[])
