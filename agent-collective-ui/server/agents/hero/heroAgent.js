// heroAgent.js – generate a tagline based on live metric
import { OpenAI } from "openai";

const openai = new OpenAI({
  apiKey: process.env.OPENAI_API_KEY,
  baseURL: process.env.OPENAI_BASE_URL, // e.g., http://localhost:8000/v1
});

const HERO_PROMPT = (metric) => `You are the Synthos Hero AI. Produce a short, inspirational tagline (max 10 words) that incorporates the metric: ${metric}. Use a futuristic, poetic tone. Return plain text.`;

export async function generateTagline(metric) {
  const completion = await openai.chat.completions.create({
    model: "gpt-oss-120b",
    messages: [{ role: "user", content: HERO_PROMPT(metric) }],
    temperature: 0.7,
    max_tokens: 50,
  });
  return completion.choices[0].message.content.trim();
}
