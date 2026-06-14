import express from "express";
import { generateTagline } from "../agents/hero/heroAgent.js";

const router = express.Router();

router.post("/", async (req, res) => {
  const { metric } = req.body;
  try {
    const tagline = await generateTagline(metric);
    res.json({ tagline, timestamp: Date.now() });
  } catch (e) {
    console.error("Hero route error", e);
    res.status(500).json({ error: "Failed to generate tagline" });
  }
});

export default router;
