from agents import Agent
from .prompts import SUPPORT_PROMPT

support = Agent(
    name="Support",
    instructions=SUPPORT_PROMPT,
    model="gpt-5",
)
