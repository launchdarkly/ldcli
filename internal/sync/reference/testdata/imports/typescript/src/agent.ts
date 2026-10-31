import { Agent } from "@openai/agents";
import { SUPPORT_PROMPT } from "./prompts";

export const support = new Agent({
  name: "Support",
  instructions: SUPPORT_PROMPT,
  model: "gpt-5",
});
