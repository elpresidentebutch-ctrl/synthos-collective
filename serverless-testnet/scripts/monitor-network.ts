import { Genesis } from "../core.ts";

const genesisPath = Deno.args[0] ?? new URL("../generated/genesis.json", import.meta.url).pathname;
const genesis = JSON.parse(await Deno.readTextFile(genesisPath)) as Genesis;
const intervalMs = Number(Deno.args[1] ?? 15000);

while (true) {
  const started = Date.now();
  const rows = await Promise.all(genesis.validators.map(async (validator) => {
    try {
      const response = await fetch(`${validator.url}/status`, { signal: AbortSignal.timeout(8000) });
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      const status = await response.json() as { head: { height: number; hash: string }; round: number };
      return { id: validator.id, ok: true, height: status.head.height, hash: status.head.hash, latencyMs: Date.now() - started };
    } catch (error) {
      return { id: validator.id, ok: false, height: -1, hash: "", latencyMs: Date.now() - started, error: String(error) };
    }
  }));
  const healthy = rows.filter((row) => row.ok);
  const converged = healthy.length === 6 && new Set(healthy.map((row) => `${row.height}:${row.hash}`)).size === 1;
  console.log(JSON.stringify({ timestamp: new Date().toISOString(), converged, healthy: healthy.length, validators: rows }));
  await new Promise((resolve) => setTimeout(resolve, Math.max(0, intervalMs - (Date.now() - started))));
}
