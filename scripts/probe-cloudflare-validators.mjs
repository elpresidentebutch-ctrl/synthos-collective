const validators = Array.from({ length: 6 }, (_, index) => {
  const number = index + 10;
  return { name: `synthos-validator-${number}`, url: `https://synthos-validator-${number}.jamesishamwilliams.workers.dev` };
});
const paths = ["/health", "/status", "/heartbeat", "/peers", "/blocks?from=0"];

async function probe(validator, path) {
  const started = Date.now();
  try {
    const response = await fetch(`${validator.url}${path}`, { signal: AbortSignal.timeout(15000), headers: { accept: "application/json" } });
    const text = await response.text();
    let body;
    try { body = JSON.parse(text); } catch { body = text.slice(0, 500); }
    return { path, ok: response.ok, status: response.status, latency_ms: Date.now() - started, body };
  } catch (error) {
    return { path, ok: false, status: 0, latency_ms: Date.now() - started, error: String(error) };
  }
}

const report = [];
for (const validator of validators) {
  const results = await Promise.all(paths.map((path) => probe(validator, path)));
  report.push({ ...validator, results });
  console.log(`\n=== ${validator.name} ===`);
  for (const result of results) console.log(JSON.stringify(result));
}

const statuses = report.map((validator) => {
  const status = validator.results.find((result) => result.path === "/status")?.body;
  const heartbeat = validator.results.find((result) => result.path === "/heartbeat")?.body;
  return {
    name: validator.name,
    reachable: validator.results.some((result) => result.ok),
    height: status?.height ?? null,
    tip: status?.tip ?? null,
    state_root: status?.state_root ?? null,
    next_proposer: status?.next_proposer ?? null,
    heartbeat_last_check: heartbeat?.last_check ?? null,
    heartbeat_count: heartbeat?.heartbeat_count ?? heartbeat?.count ?? null,
  };
});
console.log("\n=== SUMMARY ===");
console.log(JSON.stringify(statuses, null, 2));

const reachable = statuses.filter((status) => status.reachable);
if (reachable.length === 0) process.exitCode = 2;
