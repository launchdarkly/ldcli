from langchain_core.messages import HumanMessage, SystemMessage
from langchain_core.prompts import ChatPromptTemplate, PromptTemplate
from langchain_core.tools import tool

SHARED_MESSAGES = [
    ("system", "Use shared instructions."),
    ("human", "{question}"),
]

chat = ChatPromptTemplate.from_messages([
    ("system", "You route support requests."),
    ("human", "Help with {request}."),
])
shared_chat = ChatPromptTemplate.from_messages(SHARED_MESSAGES)

chat_constructor = ChatPromptTemplate([
    SystemMessage("You summarize support cases."),
    HumanMessage("Summarize {case}."),
])

simple = PromptTemplate.from_template("Explain {topic}.")
simple_keyword = PromptTemplate(template="Answer in one paragraph.")
chain = ChatPromptTemplate.from_messages([
    ("system", "Use the company policy."),
    ("human", "{question}"),
]) | llm

ambiguous = (
    ChatPromptTemplate.from_messages([("system", "First.")])
    | ChatPromptTemplate.from_messages([("system", "Second.")])
)


@tool
def lookup_order(order_id: str) -> str:
    """Look up one order and return its status."""
    return order_id
