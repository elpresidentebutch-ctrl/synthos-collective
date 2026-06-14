import OpenAI from "openai";

// Determines which model to use for trial vs. paid usage.
const isTrial = process.env.TRIAL_MODE === "true";

export const openai = new OpenAI({
  apiKey: process.env.OPENAI_API_KEY,
  baseURL: process.env.OPENAI_BASE_URL || "https://api.openai.com/v1",
  // The model can be overridden per request; we expose a default for convenience.
  defaultModel: isTrial ? "gpt-3.5-turbo" : process.env.OPENAI_MODEL || "gpt-4o-mini",
});
