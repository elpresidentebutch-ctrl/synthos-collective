import React, { useEffect } from "react";
import ReactMarkdown from "react-markdown";
import { useAgent } from "../api/useAgent";

const HeroAgent = () => {
  const { data, loading, error, call } = useAgent("hero");

  // Grab metric from existing DOM element if present, else fallback
  const metric = document.getElementById("live-count")?.textContent || "0 validators";

  useEffect(() => {
    call({ metric });
    const interval = setInterval(() => call({ metric }), 30000); // refresh every 30s
    return () => clearInterval(interval);
  }, []);

  return (
    <section className="agent-card mt-8 p-6">
      <h2 className="text-lg font-semibold mb-2 text-white">AI‑Powered Tagline</h2>
      {loading && <p className="text-gray-400">Generating…</p>}
      {error && <p className="text-red-400">Error: {error}</p>}
      {data && <ReactMarkdown className="text-white">{data.tagline}</ReactMarkdown>}
    </section>
  );
};

export default HeroAgent;
