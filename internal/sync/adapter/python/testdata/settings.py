from dataclasses import dataclass
from enum import Enum

from app.models import DEFAULT_MODEL


class Language(Enum):
    EN = "en"
    FR = "fr"


@dataclass
class SupportSettings:
    instructions = """Ask for an order ID.
Keep the answer concise."""
    temperature = 0.2
    enabled = True


CONFIG = {
    "agents": {
        "support": {
            "prompt": f"Use {DEFAULT_MODEL} for support.",
            "model": "gpt-5",
        },
        "refunds": {
            "prompt": "Explain the refund policy.",
            "model": "gpt-5",
        },
    }
}

PROMPTS = {
    Language.EN: "Answer in English.",
    Language.FR: "Answer in French.",
}

MODELS = ["gpt-5", 'claude-sonnet']


def build():
    prompt = "Function-local prompt."
    retries = 3
    return prompt, retries
