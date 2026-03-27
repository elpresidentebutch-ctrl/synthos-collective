"""
Stress Test Framework for SYNTHOS Collective.

Provides multi-agent orchestration, transaction generators,
network simulation, chaos injection, metrics collection,
and report generation.
"""

import asyncio
import gc
import json
import random
import time
import tracemalloc
from collections import defaultdict
from dataclasses import dataclass, field
from typing import Any, Dict, List, Optional

from src.core import AgentConfig, SyntHOSAgent, Event, EventType
from src.models import Transaction, Block
from src.roles import (
    CitizenRole,
    CommunicatorRole,
    EconomistRole,
    EnforcerRole,
    GovernorRole,
    SimulatorRole,
    ValidatorRole,
)

from tests.stress_tests.config import (
    FailureConfig,
    NetworkConfig,
    StressTestConfig,
    TransactionConfig,
)


# ---------------------------------------------------------------------------
# Metrics
# ---------------------------------------------------------------------------

@dataclass
class TestMetrics:
    """Collected metrics from a stress test run."""
    transactions_submitted: int = 0
    transactions_validated: int = 0
    transactions_rejected: int = 0
    blocks_proposed: int = 0
    blocks_finalized: int = 0
    events_processed: int = 0
    errors: int = 0
    start_time: float = field(default_factory=time.monotonic)
    end_time: float = 0.0
    latency_samples: List[float] = field(default_factory=list)
    memory_samples: List[int] = field(default_factory=list)
    byzantine_faults_detected: int = 0
    slashing_events: int = 0

    @property
    def duration_seconds(self) -> float:
        end = self.end_time if self.end_time else time.monotonic()
        return max(end - self.start_time, 1e-9)

    @property
    def tps(self) -> float:
        return self.transactions_submitted / self.duration_seconds

    @property
    def avg_latency_ms(self) -> float:
        if not self.latency_samples:
            return 0.0
        return sum(self.latency_samples) / len(self.latency_samples) * 1000

    @property
    def p99_latency_ms(self) -> float:
        if not self.latency_samples:
            return 0.0
        sorted_samples = sorted(self.latency_samples)
        idx = int(len(sorted_samples) * 0.99)
        return sorted_samples[idx] * 1000

    def to_dict(self) -> Dict[str, Any]:
        return {
            "transactions_submitted": self.transactions_submitted,
            "transactions_validated": self.transactions_validated,
            "transactions_rejected": self.transactions_rejected,
            "blocks_proposed": self.blocks_proposed,
            "blocks_finalized": self.blocks_finalized,
            "events_processed": self.events_processed,
            "errors": self.errors,
            "duration_seconds": round(self.duration_seconds, 3),
            "tps": round(self.tps, 2),
            "avg_latency_ms": round(self.avg_latency_ms, 3),
            "p99_latency_ms": round(self.p99_latency_ms, 3),
            "byzantine_faults_detected": self.byzantine_faults_detected,
            "slashing_events": self.slashing_events,
        }


# ---------------------------------------------------------------------------
# Agent factory
# ---------------------------------------------------------------------------

def make_agent(agent_id: str, network: str = "testnet") -> SyntHOSAgent:
    """Create a fully-initialised SyntHOSAgent with all roles attached."""
    config = AgentConfig(id=agent_id, network=network)
    agent = SyntHOSAgent(config)
    for role_cls in (
        ValidatorRole,
        EconomistRole,
        GovernorRole,
        CommunicatorRole,
        SimulatorRole,
        EnforcerRole,
        CitizenRole,
    ):
        agent.register_role(role_cls(agent))
    return agent


# ---------------------------------------------------------------------------
# Transaction generator
# ---------------------------------------------------------------------------

class TransactionGenerator:
    """Generates configurable transaction workloads."""

    def __init__(self, cfg: TransactionConfig, sender: str, recipient: str):
        self._cfg = cfg
        self._sender = sender
        self._recipient = recipient
        self._nonce = 0

    def _make_tx(self) -> Transaction:
        data = bytes(self._cfg.payload_size_bytes)
        tx = Transaction(
            sender=self._sender,
            recipient=self._recipient,
            amount=self._cfg.transaction_amount,
            fee=self._cfg.transaction_fee,
            nonce=self._nonce,
            data=data,
        )
        self._nonce += 1
        return tx

    def generate(self) -> List[Transaction]:
        """Return the full transaction list for the configured pattern."""
        pattern = self._cfg.pattern
        total = self._cfg.num_transactions

        if pattern == "constant":
            return [self._make_tx() for _ in range(total)]

        if pattern == "burst":
            txs: List[Transaction] = []
            burst = self._cfg.burst_size
            for _ in range(0, total, burst):
                txs.extend(self._make_tx() for _ in range(min(burst, total - len(txs))))
            return txs

        if pattern == "ramp":
            txs = []
            steps = max(1, self._cfg.ramp_steps)
            per_step = total // steps
            for step in range(steps):
                count = per_step * (step + 1) // steps
                txs.extend(self._make_tx() for _ in range(count))
            # Fill remaining
            while len(txs) < total:
                txs.append(self._make_tx())
            return txs[:total]

        # Default fallback
        return [self._make_tx() for _ in range(total)]


# ---------------------------------------------------------------------------
# Network simulator
# ---------------------------------------------------------------------------

class NetworkSimulator:
    """Injects artificial latency, jitter, and packet loss."""

    def __init__(self, cfg: NetworkConfig):
        self._cfg = cfg
        self._rng = random.Random(42)

    async def simulate_send(self, payload: Any = None) -> bool:
        """
        Simulate network transmission.

        Returns False if the packet was "dropped".
        """
        if self._cfg.packet_loss_rate > 0:
            if self._rng.random() < self._cfg.packet_loss_rate:
                return False  # packet dropped

        delay = self._cfg.latency_ms / 1000.0
        if self._cfg.jitter_ms > 0:
            jitter = self._rng.uniform(0, self._cfg.jitter_ms / 1000.0)
            delay += jitter

        if delay > 0:
            await asyncio.sleep(delay)

        return True


# ---------------------------------------------------------------------------
# Chaos monkey
# ---------------------------------------------------------------------------

class ChaosMonkey:
    """Randomly injects failures into the agent network."""

    def __init__(self, cfg: FailureConfig):
        self._cfg = cfg
        self._rng = random.Random(0xDEADBEEF)

    def should_crash(self) -> bool:
        return self._cfg.crash_probability > 0 and (
            self._rng.random() < self._cfg.crash_probability
        )

    def should_drop_message(self) -> bool:
        return self._cfg.message_drop_rate > 0 and (
            self._rng.random() < self._cfg.message_drop_rate
        )

    async def maybe_crash_agent(self, agent: SyntHOSAgent) -> bool:
        """Stop agent if chaos conditions trigger a crash. Returns True if crashed."""
        if self.should_crash():
            await agent.stop()
            return True
        return False


# ---------------------------------------------------------------------------
# Byzantine agent
# ---------------------------------------------------------------------------

class ByzantineAgent:
    """
    Wrapper around a regular agent that simulates Byzantine behaviour.

    Supports equivocation (publishing conflicting blocks) and double-spend
    (submitting conflicting transactions).
    """

    def __init__(self, agent: SyntHOSAgent, cfg: FailureConfig):
        self.agent = agent
        self._cfg = cfg
        self._rng = random.Random(id(agent))

    async def submit_equivocating_blocks(
        self, height: int, metrics: TestMetrics
    ) -> None:
        """Propose two conflicting blocks at the same height."""
        if not self._cfg.equivocation_enabled:
            return
        for suffix in ("_a", "_b"):
            block = Block(
                height=height,
                proposer=self.agent.id + suffix,
            )
            await self.agent.handle_incoming_block(block)
            metrics.blocks_proposed += 1

    async def submit_double_spend(
        self, sender: str, recipient: str, metrics: TestMetrics
    ) -> None:
        """Submit two transactions that attempt to spend the same funds."""
        if not self._cfg.double_spend_enabled:
            return
        for recipient_variant in (recipient, recipient + "_alt"):
            tx = Transaction(
                sender=sender,
                recipient=recipient_variant,
                amount=999_999,  # exceeds typical balance to trigger rejection
                fee=1,
                nonce=0,
            )
            result = await self.agent.submit_transaction(tx)
            if result:
                metrics.transactions_submitted += 1


# ---------------------------------------------------------------------------
# Multi-agent orchestrator
# ---------------------------------------------------------------------------

class StressTestOrchestrator:
    """
    Orchestrates a fleet of SYNTHOS agents for stress and BFT testing.

    Usage::

        orchestrator = StressTestOrchestrator(config)
        await orchestrator.setup()
        metrics = await orchestrator.run()
        await orchestrator.teardown()
    """

    def __init__(self, config: StressTestConfig):
        self._config = config
        self._agents: List[SyntHOSAgent] = []
        self._byzantine_agents: List[ByzantineAgent] = []
        self._network_sim = NetworkSimulator(config.network)
        self._chaos = ChaosMonkey(config.failures)
        self.metrics = TestMetrics()

    # ------------------------------------------------------------------
    # Lifecycle
    # ------------------------------------------------------------------

    async def setup(self) -> None:
        """Create and initialise all agents."""
        cfg = self._config.agents
        # Honest validators
        for i in range(cfg.num_validators):
            agent = make_agent(f"validator-{i}")
            await agent.state.set_balance(f"validator-{i}", cfg.initial_balance)
            await agent.initialize()
            self._agents.append(agent)

        # Citizens
        for i in range(cfg.num_citizens):
            agent = make_agent(f"citizen-{i}")
            await agent.state.set_balance(f"citizen-{i}", cfg.initial_balance)
            await agent.initialize()
            self._agents.append(agent)

        # Byzantine validators (replace tail of validators list with byzantine wrappers)
        byz_count = min(
            self._config.failures.byzantine_validator_count,
            cfg.num_validators,
        )
        if byz_count > 0:
            for i in range(cfg.num_validators - byz_count, cfg.num_validators):
                byz = ByzantineAgent(self._agents[i], self._config.failures)
                self._byzantine_agents.append(byz)

    async def teardown(self) -> None:
        """Stop all agents."""
        for agent in self._agents:
            if agent.running:
                await agent.stop()

    # ------------------------------------------------------------------
    # Run
    # ------------------------------------------------------------------

    async def run(self) -> TestMetrics:
        """Execute the stress test and return collected metrics."""
        self.metrics = TestMetrics()
        tracemalloc.start()

        tx_cfg = self._config.transactions
        sender_agent = self._agents[0]
        sender_id = sender_agent.id
        recipient_id = self._agents[-1].id if len(self._agents) > 1 else "recipient"

        gen = TransactionGenerator(tx_cfg, sender_id, recipient_id)
        transactions = gen.generate()

        # Subscribe to events on first agent for metric tracking
        sender_agent.event_bus.subscribe(
            EventType.TRANSACTION_VALIDATED, self._on_tx_validated
        )
        sender_agent.event_bus.subscribe(
            EventType.TRANSACTION_REJECTED, self._on_tx_rejected
        )
        sender_agent.event_bus.subscribe(
            EventType.BLOCK_FINALIZED, self._on_block_finalized
        )
        sender_agent.event_bus.subscribe(
            EventType.SLASHING_TRIGGERED, self._on_slashing
        )

        # Submit transactions
        for tx in transactions:
            submitted = await self._network_sim.simulate_send(tx)
            if submitted:
                start = time.monotonic()
                await sender_agent.submit_transaction(tx)
                self.metrics.latency_samples.append(time.monotonic() - start)
                self.metrics.transactions_submitted += 1

        # BFT attacks
        for byz in self._byzantine_agents:
            await byz.submit_equivocating_blocks(1, self.metrics)
            await byz.submit_double_spend(sender_id, recipient_id, self.metrics)

        # Allow event processing
        await asyncio.sleep(0.2)

        # Sample memory
        current, _ = tracemalloc.get_traced_memory()
        self.metrics.memory_samples.append(current)
        tracemalloc.stop()

        gc.collect()
        self.metrics.end_time = time.monotonic()
        return self.metrics

    # ------------------------------------------------------------------
    # Event callbacks
    # ------------------------------------------------------------------

    async def _on_tx_validated(self, event: Event) -> None:
        self.metrics.transactions_validated += 1
        self.metrics.events_processed += 1

    async def _on_tx_rejected(self, event: Event) -> None:
        self.metrics.transactions_rejected += 1
        self.metrics.events_processed += 1

    async def _on_block_finalized(self, event: Event) -> None:
        self.metrics.blocks_finalized += 1
        self.metrics.events_processed += 1

    async def _on_slashing(self, event: Event) -> None:
        self.metrics.slashing_events += 1
        self.metrics.events_processed += 1


# ---------------------------------------------------------------------------
# Report generation
# ---------------------------------------------------------------------------

class ReportGenerator:
    """Generates JSON and plain-text reports from TestMetrics."""

    @staticmethod
    def to_json(metrics: TestMetrics, config: StressTestConfig) -> str:
        report = {
            "profile": config.profile.value,
            "metrics": metrics.to_dict(),
            "config": {
                "num_validators": config.agents.num_validators,
                "num_citizens": config.agents.num_citizens,
                "num_transactions": config.transactions.num_transactions,
                "pattern": config.transactions.pattern,
                "byzantine_validators": config.failures.byzantine_validator_count,
            },
        }
        return json.dumps(report, indent=2)

    @staticmethod
    def to_text(metrics: TestMetrics, config: StressTestConfig) -> str:
        lines = [
            "=" * 60,
            f"SYNTHOS Stress Test Report – profile={config.profile.value}",
            "=" * 60,
            f"Duration          : {metrics.duration_seconds:.2f}s",
            f"TPS               : {metrics.tps:.1f}",
            f"Submitted         : {metrics.transactions_submitted}",
            f"Validated         : {metrics.transactions_validated}",
            f"Rejected          : {metrics.transactions_rejected}",
            f"Blocks Finalized  : {metrics.blocks_finalized}",
            f"Avg Latency       : {metrics.avg_latency_ms:.3f}ms",
            f"P99 Latency       : {metrics.p99_latency_ms:.3f}ms",
            f"Slashing Events   : {metrics.slashing_events}",
            f"Errors            : {metrics.errors}",
            "=" * 60,
        ]
        return "\n".join(lines)
