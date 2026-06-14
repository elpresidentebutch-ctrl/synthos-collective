// story route for Express
import express from "express";
import { generateStory } from "../agents/story/storyAgent.js";

const router = express.Router();

router.post("/", async (req, res) => {
  try {
    const markdown = await generateStory();
    res.json({ markdown, timestamp: Date.now() });
  } catch (e) {
    console.error("Story route error", e);
    res.status(500).json({ error: "Failed to generate story" });
  }
});

export default router;
