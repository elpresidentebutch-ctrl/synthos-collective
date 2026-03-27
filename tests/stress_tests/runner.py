"""
CLI Runner for SYNTHOS Collective Stress Tests.

Usage::

    python -m tests.stress_tests.runner run --profile=heavy
    python -m tests.stress_tests.runner benchmark
    python -m tests.stress_tests.runner bft
    python -m tests.stress_tests.runner profile
    python -m tests.stress_tests.runner report --input=results.json

All subcommands can also be invoked via the ``stress-test`` entry-point if
the package is installed (see ``setup.cfg`` / ``pyproject.toml``).
"""

import argparse
import asyncio
import json
import os
import sys
import time
import tracemalloc
from pathlib import Path

# Locate the project root by walking up from this file until we find the .git
# directory or reach the filesystem root.  This avoids hardcoded parent-count
# traversal that would break if the file is moved.
def _find_project_root(start: Path) -> Path:
    for candidate in [start, *start.parents]:
        if (candidate / ".git").exists() or (candidate / "go.mod").exists():
            return candidate
    return start.parents[3]  # fallback


_ROOT = _find_project_root(Path(__file__).resolve())
if str(_ROOT) not in sys.path:
    sys.path.insert(0, str(_ROOT))

from tests.stress_tests.config import (
    PRESET_PROFILES,
    WorkloadProfile,
    get_bft_config,
    get_heavy_config,
    get_light_config,
    get_medium_config,
)
from tests.stress_tests.framework import (
    ReportGenerator,
    StressTestOrchestrator,
    TestMetrics,
)


# ---------------------------------------------------------------------------
# Sub-commands
# ---------------------------------------------------------------------------

async def cmd_run(profile_name: str, output: str | None) -> None:
    """Run a predefined workload profile."""
    config = PRESET_PROFILES.get(profile_name)
    if config is None:
        valid = ", ".join(PRESET_PROFILES)
        print(f"Unknown profile '{profile_name}'. Valid profiles: {valid}", file=sys.stderr)
        sys.exit(1)

    print(f"[stress-test] Running profile: {profile_name}")
    orch = StressTestOrchestrator(config)
    await orch.setup()
    try:
        metrics = await orch.run()
    finally:
        await orch.teardown()

    print(ReportGenerator.to_text(metrics, config))
    _maybe_save(metrics, config, output)


async def cmd_benchmark(output: str | None) -> None:
    """Run the performance benchmarking suite."""
    print("[stress-test] Running performance benchmarks…")

    results = {}
    for name, config in PRESET_PROFILES.items():
        if config.profile == WorkloadProfile.EXTREME:
            continue  # skip extreme profile in quick benchmark mode
        orch = StressTestOrchestrator(config)
        await orch.setup()
        try:
            metrics = await orch.run()
        finally:
            await orch.teardown()
        results[name] = metrics.to_dict()
        print(f"  [{name}] TPS={metrics.tps:.1f}  avg_latency={metrics.avg_latency_ms:.2f}ms")

    if output:
        Path(output).write_text(json.dumps(results, indent=2))
        print(f"Results saved to {output}")


async def cmd_bft(num_validators: int, output: str | None) -> None:
    """Run Byzantine fault tolerance tests."""
    print(f"[stress-test] Running BFT tests with {num_validators} validators…")
    config = get_bft_config(num_validators=num_validators)
    orch = StressTestOrchestrator(config)
    await orch.setup()
    try:
        metrics = await orch.run()
    finally:
        await orch.teardown()

    byzantine_count = config.failures.byzantine_validator_count
    honest_count = num_validators - byzantine_count
    print(f"  Honest validators : {honest_count}")
    print(f"  Byzantine validators: {byzantine_count}")
    print(ReportGenerator.to_text(metrics, config))
    _maybe_save(metrics, config, output)


async def cmd_profile(num_transactions: int, output: str | None) -> None:
    """Run with memory/CPU profiling and display a usage report."""
    print(f"[stress-test] Profiling with {num_transactions} transactions…")

    config = get_medium_config()
    config.transactions.num_transactions = num_transactions

    tracemalloc.start()
    start_time = time.monotonic()

    orch = StressTestOrchestrator(config)
    await orch.setup()
    try:
        metrics = await orch.run()
    finally:
        await orch.teardown()

    elapsed = time.monotonic() - start_time
    current, peak = tracemalloc.get_traced_memory()
    tracemalloc.stop()

    report = {
        "duration_seconds": round(elapsed, 3),
        "tps": round(metrics.tps, 2),
        "memory_current_mb": round(current / 1024 ** 2, 2),
        "memory_peak_mb": round(peak / 1024 ** 2, 2),
        "transactions": metrics.transactions_submitted,
    }

    print("[memory profile]")
    for k, v in report.items():
        print(f"  {k}: {v}")

    if output:
        Path(output).write_text(json.dumps(report, indent=2))
        print(f"Profile saved to {output}")


def cmd_report(input_path: str) -> None:
    """Parse a previously saved JSON results file and print a summary."""
    data = json.loads(Path(input_path).read_text())
    print("=" * 60)
    print("SYNTHOS Stress Test – Saved Results")
    print("=" * 60)
    if isinstance(data, dict) and "metrics" in data:
        # Single-run result
        m = data["metrics"]
        for k, v in m.items():
            print(f"  {k}: {v}")
    elif isinstance(data, dict):
        # Benchmark suite result
        for profile, m in data.items():
            print(f"\n  Profile: {profile}")
            for k, v in m.items():
                print(f"    {k}: {v}")
    print("=" * 60)


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _maybe_save(metrics: TestMetrics, config, output: str | None) -> None:
    if output:
        json_str = ReportGenerator.to_json(metrics, config)
        Path(output).write_text(json_str)
        print(f"Results saved to {output}")


# ---------------------------------------------------------------------------
# CLI entry-point
# ---------------------------------------------------------------------------

def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="stress-test",
        description="SYNTHOS Collective stress testing CLI",
    )
    sub = parser.add_subparsers(dest="command", required=True)

    # run
    run_p = sub.add_parser("run", help="Run a predefined workload profile")
    run_p.add_argument(
        "--profile",
        default="light",
        choices=[p.value for p in WorkloadProfile],
        help="Workload profile (default: light)",
    )
    run_p.add_argument("--output", default=None, help="Save JSON results to file")

    # benchmark
    bench_p = sub.add_parser("benchmark", help="Run performance benchmarking suite")
    bench_p.add_argument("--output", default=None, help="Save JSON results to file")

    # bft
    bft_p = sub.add_parser("bft", help="Run Byzantine fault tolerance tests")
    bft_p.add_argument(
        "--validators",
        type=int,
        default=10,
        help="Total number of validators (default: 10)",
    )
    bft_p.add_argument("--output", default=None, help="Save JSON results to file")

    # profile
    prof_p = sub.add_parser("profile", help="Memory/CPU profiling")
    prof_p.add_argument(
        "--transactions",
        type=int,
        default=500,
        help="Number of transactions to submit (default: 500)",
    )
    prof_p.add_argument("--output", default=None, help="Save JSON profile to file")

    # report
    rep_p = sub.add_parser("report", help="Print a saved results file")
    rep_p.add_argument("--input", required=True, help="Path to JSON results file")

    return parser


def main() -> None:
    parser = build_parser()
    args = parser.parse_args()

    if args.command == "run":
        asyncio.run(cmd_run(args.profile, args.output))
    elif args.command == "benchmark":
        asyncio.run(cmd_benchmark(args.output))
    elif args.command == "bft":
        asyncio.run(cmd_bft(args.validators, args.output))
    elif args.command == "profile":
        asyncio.run(cmd_profile(args.transactions, args.output))
    elif args.command == "report":
        cmd_report(args.input)
    else:
        parser.print_help()
        sys.exit(1)


if __name__ == "__main__":
    main()
