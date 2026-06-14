import express from "express";
import cors from "cors";
import heroRouter from "./routes/hero.js";
import storyRouter from "./routes/story.js";

const app = express();
app.use(cors({ origin: "http://localhost:5173" }));
app.use(express.json());

app.use("/api/hero", heroRouter);
app.use("/api/story", storyRouter);

const PORT = process.env.PORT ?? 5000;
app.listen(PORT, () => console.log(`🤖 AI‑Agent server listening on ${PORT}`));
