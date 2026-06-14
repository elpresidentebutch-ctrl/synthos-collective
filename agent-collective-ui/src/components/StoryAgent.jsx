import React, { useEffect } from "react";
import ReactMarkdown from "react-markdown";
import { useAgent } from "../api/useAgent";

const StoryAgent = () => {
  const { data, loading, error, call } = useAgent("story");

  useEffect(() => {
    call({}); // no payload needed
  }, []);

  return (
    <section className="agent-card mt-8 p-6">
      <h2 className="text-lg font-semibold mb-2 text-white">Live Narrative</h2>
      {loading && <p className="text-gray-400">Fetching story…</p>}
      {error && <p className="text-red-400">Error: {error}</p>}
      {data && <ReactMarkdown className="text-white">{data.markdown}</ReactMarkdown>}
    </section>
  );
};

export default StoryAgent;
