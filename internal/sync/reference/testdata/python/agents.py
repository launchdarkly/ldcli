from agents import Agent, ModelSettings
from langchain_core.prompts import PromptTemplate

# Shared text stays in this module.
SUMMARY = """Summarize the support case.
Keep the answer short."""

support = Agent(
    name="Support",
    instructions="Ask for an order ID before discussing an order.",
    model="gpt-5",
    model_settings=ModelSettings(temperature=0.2),
)

refunds = Agent(
    name='Refunds',
    instructions='Explain the refund policy.',
    model='gpt-5',
    model_settings=ModelSettings(temperature=0.1),
)

summary = PromptTemplate(template=SUMMARY)
