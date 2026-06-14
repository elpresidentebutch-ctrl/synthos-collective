import React, { useEffect, useState } from "react";

const DEFAULT_RPC = import.meta.env.VITE_SYNTHOS_RPC_URL || "http://localhost:8080";

export default function ImmuneDashboard() {
  const [status, setStatus] = useState(null);
  const [immune, setImmune] = useState(null);
  const [error, setError] = useState("");

  useEffect(() => {
    let cancelled = false;

    async function load() {
      try {
        const [statusRes, immuneRes] = await Promise.all([
          fetch(`${DEFAULT_RPC}/status`),
          fetch(`${DEFAULT_RPC}/immune/status`),
        ]);
        if (!statusRes.ok) throw new Error(`status ${statusRes.status}`);
        if (!immuneRes.ok) throw new Error(`immune ${immuneRes.status}`);
        const [statusJson, immuneJson] = await Promise.all([
          statusRes.json(),
          immuneRes.json(),
        ]);
        if (!cancelled) {
          setStatus(statusJson);
          setImmune(immuneJson);
          setError("");
        }
      } catch (err) {
        if (!cancelled) setError(err.message);
      }
    }

    load();
    const id = setInterval(load, 15000);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, []);

  return (
    <section className="w-full max-w-5xl mx-auto mt-10 rounded-lg border border-white/10 bg-white/5 p-6 backdrop-blur">
      <div className="flex flex-col gap-2 md:flex-row md:items-end md:justify-between">
        <div>
          <p className="text-xs font-semibold uppercase tracking-widest text-accent">Live Protocol State</p>
          <h2 className="mt-2 text-2xl font-bold text-white">Distributed Immune System</h2>
        </div>
        <div className="text-sm text-gray-400">
          {error ? `RPC offline: ${error}` : "Connected to Synthos RPC"}
        </div>
      </div>

      <div className="mt-6 grid grid-cols-1 gap-4 md:grid-cols-4">
        <Metric label="Height" value={status?.height ?? "-"} />
        <Metric label="Immune Nodes" value={immune?.active_immune_nodes ?? status?.immune?.active_immune_nodes ?? "-"} />
        <Metric label="Sovereign Proofs" value={immune?.sovereign_proofs ?? status?.immune?.sovereign_proofs ?? "-"} />
        <Metric label="Inbound Ports" value={immune?.inbound_ports_required ?? status?.immune?.inbound_ports_required ?? 0} />
      </div>

      <div className="mt-6 rounded-md border border-cyan-400/20 bg-cyan-400/5 p-4">
        <p className="text-sm text-gray-300">
          Poison the Well is implemented as opt-in local proof commitments: immune nodes generate
          cryptographic noise locally, then submit signed proof hashes to the chain for transparent
          accounting without targeting third-party systems.
        </p>
      </div>
    </section>
  );
}

function Metric({ label, value }) {
  return (
    <div className="rounded-md border border-white/10 bg-obsidian/70 p-4">
      <div className="text-xs uppercase tracking-widest text-gray-500">{label}</div>
      <div className="mt-2 text-2xl font-bold text-white">{value}</div>
    </div>
  );
}
