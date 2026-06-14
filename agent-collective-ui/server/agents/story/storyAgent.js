// storyAgent.js – returns a short markdown narrative of recent events
import { OpenAI } from "openai";

const openai = new OpenAI({
  apiKey: process.env.OPENAI_API_KEY,
  baseURL: process.env.OPENAI_BASE_URL,
});

const STORY_PROMPT = `You are the Synthos Storyline AI. Summarize the most recent 5 network events in a concise, engaging markdown list. Each item should be a single sentence with an emoji if appropriate. Use a futuristic tone.`;

export async function generateStory() {
  const completion = await openai.chat.completions.create({
    model: "gpt-oss-120b",
    messages: [{ role: "user", content: STORY_PROMPT }],
    temperature: 0.8,
    max_tokens: 150,
  });
  return completion.choices[0].message.content.trim();
}
