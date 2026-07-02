"""
SYNTHOS Remote Node Stress Test
Targets validator nodes running behind Cloudflare Zero Trust tunnels.

Usage:
    python scripts/stress_test_remote.py                          # uses config/nodes.json defaults
    python scripts/stress_test_remote.py --duration 120 --tps 100 # override params
    python scripts/stress_test_remote.py --nodes-file config/nodes.json --health-only
"""

import argparse
import asyncio
import json
import os
import sys
import time
from dataclasses import dataclass, field
from datetime import datetime, timezone

try:
    import aiohttp  # type: ignore[import-unresolved]
except ImportError:
    print("ERROR: aiohttp required. Install with: pip install aiohttp")
    sys.exit(1)


@dataclass
class NodeEndpoint:
    name: str
    url: str
    role: str = "validator"


@dataclass
class StressResult:
    total_requests: int = 0
    successful: int = 0
    failed: int = 0
    errors: dict = field(default_factory=dict)
    latencies_ms: list = field(default_factory=list)
    txs_submitted: int = 0
    txs_accepted: int = 0
    blocks_proposed: int = 0
    start_time: float = 0.0
    end_time: float = 0.0

    @property
    def duration(self) -> float:
        return self.end_time - self.start_time

    @property
    def avg_latency_ms(self) -> float:
        return sum(self.latencies_ms) / len(self.latencies_ms) if self.latencies_ms else 0.0

    @property
    def p50_latency_ms(self) -> float:
        return self._percentile(50)

    @property
    def p95_latency_ms(self) -> float:
        return self._percentile(95)

    @property
    def p99_latency_ms(self) -> float:
        return self._percentile(99)

    @property
    def rps(self) -> float:
        return self.total_requests / self.duration if self.duration > 0 else 0.0

    def _percentile(self, pct: int) -> float:
        if not self.latencies_ms:
            return 0.0
        s = sorted(self.latencies_ms)
        idx = int(len(s) * pct / 100)
        return s[min(idx, len(s) - 1)]


def load_config(path: str) -> dict:
    with open(path) as f:
        return json.load(f)


def parse_nodes(config: dict) -> list[NodeEndpoint]:
    return [NodeEndpoint(**n) for n in config["nodes"]]


# ── Health & Discovery ──────────────────────────────────────────────────────

async def check_node_health(session: aiohttp.ClientSession, node: NodeEndpoint) -> dict:
    """Hit /health and /status on a single node."""
    result = {"name": node.name, "url": node.url, "health": None, "status": None, "error": None}
    try:
        t0 = time.monotonic()
        async with session.get(f"{node.url.rstrip('/')}/health", timeout=aiohttp.ClientTimeout(total=10)) as resp:
            result["health"] = await resp.json()
            result["health_latency_ms"] = round((time.monotonic() - t0) * 1000, 1)

        t0 = time.monotonic()
        async with session.get(f"{node.url.rstrip('/')}/status", timeout=aiohttp.ClientTimeout(total=10)) as resp:
            result["status"] = await resp.json()
            result["status_latency_ms"] = round((time.monotonic() - t0) * 1000, 1)
    except Exception as e:
        result["error"] = str(e)
    return result


async def health_check_all(nodes: list[NodeEndpoint]) -> list[dict]:
    """Run health checks against all nodes in parallel."""
    async with aiohttp.ClientSession() as session:
        tasks = [check_node_health(session, n) for n in nodes]
        return await asyncio.gather(*tasks)


# ── Stress Test Workloads ────────────────────────────────────────────────────

async def _submit_tx(session: aiohttp.ClientSession, node: NodeEndpoint, tx_payload: dict, result: StressResult):
    """Submit a single transaction to a node."""
    url = f"{node.url.rstrip('/')}/submitTx"
    t0 = time.monotonic()
    try:
        async with session.post(url, json=tx_payload, timeout=aiohttp.ClientTimeout(total=15)) as resp:
            latency = (time.monotonic() - t0) * 1000
            result.latencies_ms.append(latency)
            result.total_requests += 1
            result.txs_submitted += 1
            if resp.status == 200:
                result.successful += 1
                result.txs_accepted += 1
            else:
                result.failed += 1
                err_text = await resp.text()
                key = f"HTTP {resp.status}: {err_text[:80]}"
                result.errors[key] = result.errors.get(key, 0) + 1
    except Exception as e:
        result.total_requests += 1
        result.failed += 1
        result.latencies_ms.append((time.monotonic() - t0) * 1000)
        key = type(e).__name__
        result.errors[key] = result.errors.get(key, 0) + 1


async def _check_status(session: aiohttp.ClientSession, node: NodeEndpoint, result: StressResult):
    """Hit /status to measure read latency."""
    url = f"{node.url.rstrip('/')}/status"
    t0 = time.monotonic()
    try:
        async with session.get(url, timeout=aiohttp.ClientTimeout(total=10)) as resp:
            latency = (time.monotonic() - t0) * 1000
            result.latencies_ms.append(latency)
            result.total_requests += 1
            if resp.status == 200:
                result.successful += 1
            else:
                result.failed += 1
    except Exception as e:
        result.total_requests += 1
        result.failed += 1
        result.latencies_ms.append((time.monotonic() - t0) * 1000)
        key = type(e).__name__
        result.errors[key] = result.errors.get(key, 0) + 1


async def _propose_block(session: aiohttp.ClientSession, node: NodeEndpoint, result: StressResult):
    """Ask a node to propose a block."""
    url = f"{node.url.rstrip('/')}/proposeBlock"
    t0 = time.monotonic()
    try:
        async with session.post(url, timeout=aiohttp.ClientTimeout(total=15)) as resp:
            latency = (time.monotonic() - t0) * 1000
            result.latencies_ms.append(latency)
            result.total_requests += 1
            if resp.status == 200:
                result.successful += 1
                result.blocks_proposed += 1
            else:
                result.failed += 1
    except Exception as e:
        result.total_requests += 1
        result.failed += 1
        result.latencies_ms.append((time.monotonic() - t0) * 1000)
        key = type(e).__name__
        result.errors[key] = result.errors.get(key, 0) + 1


def _make_tx(from_addr: str, to_addr: str, nonce: int) -> dict:
    """Build a transaction payload for the RPC submitTx endpoint."""
    return {
        "from": from_addr,
        "to": to_addr,
        "amount": 1,
        "fee": 0,
        "nonce": nonce,
        "timestamp": datetime.now(timezone.utc).isoformat(),
    }


async def run_stress_test(
    nodes: list[NodeEndpoint],
    duration_seconds: int = 60,
    tps_target: int = 50,
    concurrency: int = 10,
    ramp_up_seconds: int = 5,
) -> StressResult:
    """
    Main stress test loop.
    
    Distributes transactions across all healthy nodes in a round-robin pattern.
    Mixes workload: 70% submitTx, 20% status reads, 10% proposeBlock.
    """
    result = StressResult()
    result.start_time = time.monotonic()

    # Use genesis addresses for transactions
    from_addr = "agent-0"
    to_addr = "0x4ab2003c0391f25013f15539c9123e770d3c5a67"
    nonce = 0

    interval = 1.0 / tps_target if tps_target > 0 else 0.02
    sem = asyncio.Semaphore(concurrency)

    async with aiohttp.ClientSession() as session:
        tasks = []
        deadline = time.monotonic() + duration_seconds
        node_idx = 0

        print(f"\n{'='*72}")
        print(f"  STRESS TEST STARTED")
        print(f"  Targeting {len(nodes)} nodes | {tps_target} TPS target | {duration_seconds}s duration")
        print(f"  Concurrency: {concurrency} | Ramp-up: {ramp_up_seconds}s")
        print(f"{'='*72}\n")

        while time.monotonic() < deadline:
            elapsed = time.monotonic() - result.start_time
            # Ramp up: scale TPS linearly during ramp period
            if elapsed < ramp_up_seconds and ramp_up_seconds > 0:
                current_interval = interval / max(elapsed / ramp_up_seconds, 0.01)
            else:
                current_interval = interval

            node = nodes[node_idx % len(nodes)]
            node_idx += 1

            # Workload mix
            roll = nonce % 10
            async def _work(n=node, r=roll, nx=nonce):
                async with sem:
                    if r < 7:
                        await _submit_tx(session, n, _make_tx(from_addr, to_addr, nx), result)
                    elif r < 9:
                        await _check_status(session, n, result)
                    else:
                        await _propose_block(session, n, result)

            tasks.append(asyncio.create_task(_work()))
            nonce += 1

            # Progress every 10 seconds
            if nonce % (tps_target * 10) == 0:
                print(f"  [{elapsed:.0f}s] requests={result.total_requests}  ok={result.successful}  "
                      f"fail={result.failed}  avg_lat={result.avg_latency_ms:.1f}ms")

            await asyncio.sleep(current_interval)

        # Wait for in-flight requests
        if tasks:
            await asyncio.gather(*tasks, return_exceptions=True)

    result.end_time = time.monotonic()
    return result


def print_report(result: StressResult, nodes: list[NodeEndpoint]):
    """Print a formatted stress test report."""
    print(f"\n{'='*72}")
    print(f"  STRESS TEST REPORT")
    print(f"{'='*72}")
    print(f"  Nodes tested:        {len(nodes)}")
    print(f"  Duration:            {result.duration:.1f}s")
    print(f"  Total requests:      {result.total_requests}")
    print(f"  Successful:          {result.successful}")
    print(f"  Failed:              {result.failed}")
    print(f"  Success rate:        {result.successful / max(result.total_requests, 1) * 100:.1f}%")
    print(f"  Throughput:          {result.rps:.1f} req/s")
    print(f"  TXs submitted:       {result.txs_submitted}")
    print(f"  TXs accepted:        {result.txs_accepted}")
    print(f"  Blocks proposed:     {result.blocks_proposed}")
    print(f"  {'─'*68}")
    print(f"  Latency (avg):       {result.avg_latency_ms:.1f}ms")
    print(f"  Latency (p50):       {result.p50_latency_ms:.1f}ms")
    print(f"  Latency (p95):       {result.p95_latency_ms:.1f}ms")
    print(f"  Latency (p99):       {result.p99_latency_ms:.1f}ms")

    if result.errors:
        print(f"  {'─'*68}")
        print(f"  Errors:")
        for err, count in sorted(result.errors.items(), key=lambda x: -x[1]):
            print(f"    [{count:>5}x] {err}")

    print(f"{'='*72}\n")

    # Write JSON report
    report_path = os.path.join("reports", f"stress_test_{int(time.time())}.json")
    os.makedirs("reports", exist_ok=True)
    report = {
        "timestamp": datetime.now(timezone.utc).isoformat(),
        "nodes": [{"name": n.name, "url": n.url} for n in nodes],
        "duration_seconds": round(result.duration, 1),
        "total_requests": result.total_requests,
        "successful": result.successful,
        "failed": result.failed,
        "success_rate_pct": round(result.successful / max(result.total_requests, 1) * 100, 2),
        "throughput_rps": round(result.rps, 1),
        "txs_submitted": result.txs_submitted,
        "txs_accepted": result.txs_accepted,
        "blocks_proposed": result.blocks_proposed,
        "latency_avg_ms": round(result.avg_latency_ms, 1),
        "latency_p50_ms": round(result.p50_latency_ms, 1),
        "latency_p95_ms": round(result.p95_latency_ms, 1),
        "latency_p99_ms": round(result.p99_latency_ms, 1),
        "errors": result.errors,
    }
    with open(report_path, "w") as f:
        json.dump(report, f, indent=2)
    print(f"  Report saved to: {report_path}")


async def main():
    parser = argparse.ArgumentParser(description="SYNTHOS Remote Node Stress Test")
    parser.add_argument("--nodes-file", default="config/nodes.json", help="Path to nodes config")
    parser.add_argument("--duration", type=int, default=None, help="Test duration in seconds")
    parser.add_argument("--tps", type=int, default=None, help="Target transactions per second")
    parser.add_argument("--concurrency", type=int, default=None, help="Max concurrent requests")
    parser.add_argument("--ramp-up", type=int, default=None, help="Ramp-up period in seconds")
    parser.add_argument("--health-only", action="store_true", help="Only run health checks")
    args = parser.parse_args()

    config = load_config(args.nodes_file)
    nodes = parse_nodes(config)

    # Check for unset URLs
    unset_nodes = [n for n in nodes if not n.url or n.url == "about:blank"]
    if unset_nodes:
        print("ERROR: Node URLs are unset.")
        print("Edit config/nodes.json with the actual validator URLs.")
        print("Cloudflare tunnel domains are visible in Zero Trust under Networks -> Tunnels.")
        print()
        print("To find your tunnel URLs:")
        print("  1. Go to https://one.dash.cloudflare.com/ -> Networks -> Tunnels")
        print("  2. Click each tunnel to see its public hostname")
        print("  3. Update config/nodes.json with those hostnames")
        sys.exit(1)

    # Health check
    print(f"\n  Checking {len(nodes)} nodes...")
    health = await health_check_all(nodes)
    healthy_nodes = []
    for h in health:
        if h["error"]:
            print(f"  FAIL  {h['name']}: {h['error']}")
        else:
            status = h.get("status", {})
            print(f"  OK    {h['name']}: height={status.get('height', '?')} "
                  f"chain={status.get('chain_id', '?')} "
                  f"latency={h.get('health_latency_ms', '?')}ms")
            healthy_nodes.append(next(n for n in nodes if n.name == h["name"]))

    if not healthy_nodes:
        print("\n  No healthy nodes found. Aborting.")
        sys.exit(1)

    print(f"\n  {len(healthy_nodes)}/{len(nodes)} nodes healthy")

    if args.health_only:
        return

    # Stress test params (CLI overrides > config > defaults)
    st_config = config.get("stress_test", {})
    duration = args.duration or st_config.get("duration_seconds", 60)
    tps = args.tps or st_config.get("tx_per_second_target", 50)
    concurrency = args.concurrency or st_config.get("concurrency", 10)
    ramp_up = args.ramp_up if args.ramp_up is not None else st_config.get("ramp_up_seconds", 5)

    result = await run_stress_test(healthy_nodes, duration, tps, concurrency, ramp_up)
    print_report(result, healthy_nodes)


if __name__ == "__main__":
    asyncio.run(main())
