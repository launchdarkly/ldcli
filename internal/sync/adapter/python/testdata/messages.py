from agents import Agent

completion = Agent(
    name="Completion",
    messages=[
        {"role": "system", "content": "You answer support questions."},
        {"role": "user", "content": "Where is order {order_id}?"},
        {"role": "assistant", "content": "I will look up the order."},
    ],
    model="gpt-5",
)
