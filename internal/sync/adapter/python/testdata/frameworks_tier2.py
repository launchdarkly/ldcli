from autogen_agentchat.agents import AssistantAgent
from crewai import Agent as CrewAgent, Task
from google.adk.agents import Agent as ADKAgent, LlmAgent
from llama_index.core import PromptTemplate
from llama_index.core.agent import ReActAgent
from llama_index.core.tools import FunctionTool
from pydantic_ai import Agent as PydanticAgent
from semantic_kernel.agents import ChatCompletionAgent
from semantic_kernel.functions import kernel_function
from strands import Agent as StrandsAgent
from strands.tools import tool

crew_researcher = CrewAgent(
    role="Support researcher",
    goal="Find the relevant support policy.",
    backstory="You maintain support documentation.",
)
crew_task = Task(
    description="Research the refund request.",
    expected_output="A policy summary.",
)

pydantic_agent = PydanticAgent(
    "openai:gpt-5",
    system_prompt="Follow the support policy.",
    instructions="Ask for an order ID.",
)

adk_agent = ADKAgent(
    name="support",
    model="gemini-2.5-pro",
    instruction="Ask for an order ID.",
    description="Support agent",
)
llm_agent = LlmAgent("refunds", "gemini-2.5-pro", "Explain refunds.", "Refund agent")

strands_agent = StrandsAgent(
    system_prompt="Follow the support policy.",
    tools=[lookup_order],
)

autogen_agent = AssistantAgent(
    name="support",
    system_message="Ask for an order ID.",
)

llama_prompt = PromptTemplate("Explain {topic}.")
llama_tool = FunctionTool.from_defaults(
    lookup_order,
    name="llama_lookup",
    description="Look up one order.",
)
llama_agent = ReActAgent.from_tools(
    [lookup_order],
    system_prompt="Use the support tools.",
)

semantic_agent = ChatCompletionAgent(
    name="support",
    instructions="Ask for an order ID.",
)


@pydantic_agent.tool
def pydantic_lookup(order_id: str) -> str:
    """Look up one order with Pydantic AI."""
    return order_id


@pydantic_agent.system_prompt
def pydantic_system_prompt() -> str:
    return "Built at runtime."


@tool
def strands_lookup(order_id: str) -> str:
    """Look up one order with Strands."""
    return order_id


@kernel_function(description="Look up one order with Semantic Kernel.")
def semantic_lookup(order_id: str) -> str:
    return order_id
