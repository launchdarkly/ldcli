import { ChatPromptTemplate, PromptTemplate } from "@langchain/core/prompts";

const SHARED_MESSAGES = [
  ["system", "Use shared instructions."],
  ["human", "{question}"],
];

const chat = ChatPromptTemplate.fromMessages([
  ["system", "You route support requests."],
  ["human", "Help with {request}."],
]);
const sharedChat = ChatPromptTemplate.fromMessages(SHARED_MESSAGES);

const simple = PromptTemplate.fromTemplate("Explain {topic}.");
const constructor = new ChatPromptTemplate([
  ["system", "Use the company policy."],
  ["human", "{question}"],
]);

function build() {
  const prompt = "Function-local prompt.";
  return prompt;
}
