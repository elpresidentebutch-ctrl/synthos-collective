"""
Load Tests for SYNTHOS Collective.

Measures transaction throughput, concurrent submission behaviour,
block generation stress, and memory usage under load.

Run with pytest::

    pytest tests/stress_tests/load_tests.py -v
"""

import asyncio
import gc
import time
import tracemalloc

import pytest

from src.core import AgentConfig, SyntHOSAgent, EventType
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
    TransactionConfig,
    get_light_config,
    get_medium_config,
)
from tests.stress_tests.framework import (
    StressTestOrchestrator,
    TransactionGenerator,
    make_agent,
)


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

async def _setup_agent(agent_id: str, balance: int = 100_000) -> SyntHOSAgent:
    agent = make_agent(agent_id)
    await agent.state.set_balance(agent_id, balance)
    await agent.initialize()
    return agent


# ---------------------------------------------------------------------------
# Transaction throughput tests
# ---------------------------------------------------------------------------

class TestTransactionThroughput:
    """Measure maximum TPS with various payload sizes."""

    @pytest.mark.asyncio
    async def test_throughput_no_payload(self):
        """Submit 100 zero-byte transactions and verify all are accepted."""
        agent = await _setup_agent("tp-agent-0")

        submitted = 0
        start = time.monotonic()
        for i in range(100):
            tx = Transaction(
                sender="tp-agent-0",
                recipient="recipient",
                amount=1,
                fee=1,
                nonce=i,
            )
            result = await agent.submit_transaction(tx)
            if result:
                submitted += 1

        elapsed = time.monotonic() - start
        tps = submitted / max(elapsed, 1e-9)

        assert submitted == 100, f"Expected 100 submitted, got {submitted}"
        assert tps > 0, "TPS must be positive"

    @pytest.mark.asyncio
    async def test_throughput_with_payload(self):
        """Submit 50 transactions each carrying a 1 KB payload."""
        agent = await _setup_agent("tp-agent-payload")
        payload = bytes(1024)  # 1 KB

        submitted = 0
        for i in range(50):
            tx = Transaction(
                sender="tp-agent-payload",
                recipient="recipient",
                amount=1,
                fee=1,
                nonce=i,
                data=payload,
            )
            result = await agent.submit_transaction(tx)
            if result:
                submitted += 1

        assert submitted == 50

    @pytest.mark.asyncio
    async def test_throughput_large_payload(self):
        """Submit 10 transactions each carrying a 64 KB payload."""
        agent = await _setup_agent("tp-agent-large")
        payload = bytes(64 * 1024)  # 64 KB

        submitted = 0
        for i in range(10):
            tx = Transaction(
                sender="tp-agent-large",
                recipient="recipient",
                amount=1,
                fee=1,
                nonce=i,
                data=payload,
            )
            result = await agent.submit_transaction(tx)
            if result:
                submitted += 1

        assert submitted == 10


# ---------------------------------------------------------------------------
# Concurrent transaction submission
# ---------------------------------------------------------------------------

class TestConcurrentSubmission:
    """Test system behaviour under simultaneous load from multiple agents."""

    @pytest.mark.asyncio
    async def test_concurrent_single_agent(self):
        """Submit 200 transactions concurrently to a single agent."""
        agent = await _setup_agent("concurrent-agent")

        async def _submit(i: int) -> bool:
            tx = Transaction(
                sender="concurrent-agent",
                recipient="recipient",
                amount=1,
                fee=1,
                nonce=i,
            )
            return await agent.submit_transaction(tx)

        results = await asyncio.gather(*[_submit(i) for i in range(200)])
        accepted = sum(1 for r in results if r)
        assert accepted == 200

    @pytest.mark.asyncio
    async def test_concurrent_multi_agent(self):
        """
        Spin up 5 agents and have each submit 20 transactions concurrently.
        Validates that independent agents don't interfere with each other.
        """
        agents = [await _setup_agent(f"multi-{i}") for i in range(5)]

        async def _agent_workload(agent: SyntHOSAgent, count: int) -> int:
            ok = 0
            tasks = []
            for i in range(count):
                tx = Transaction(
                    sender=agent.id,
                    recipient="shared-recipient",
                    amount=1,
                    fee=1,
                    nonce=i,
                )
                tasks.append(agent.submit_transaction(tx))
            results = await asyncio.gather(*tasks)
            return sum(1 for r in results if r)

        totals = await asyncio.gather(
            *[_agent_workload(a, 20) for a in agents]
        )
        assert sum(totals) == 100


# ---------------------------------------------------------------------------
# Block generation stress
# ---------------------------------------------------------------------------

class TestBlockGenerationStress:
    """Rapid block creation and event-bus handling."""

    @pytest.mark.asyncio
    async def test_rapid_block_proposals(self):
        """Propose 50 blocks in rapid succession to a single agent."""
        agent = await _setup_agent("block-agent")

        proposed = 0
        for height in range(50):
            block = Block(height=height, proposer=agent.id)
            result = await agent.handle_incoming_block(block)
            if result:
                proposed += 1

        assert proposed == 50

    @pytest.mark.asyncio
    async def test_block_with_many_transactions(self):
        """Propose a single block containing 500 transactions."""
        agent = await _setup_agent("big-block-agent")

        transactions = [
            Transaction(
                sender="big-block-agent",
                recipient="recipient",
                amount=1,
                fee=1,
                nonce=i,
            )
            for i in range(500)
        ]

        block = Block(
            height=1,
            proposer=agent.id,
            transactions=transactions,
        )
        result = await agent.handle_incoming_block(block)
        assert result is True


# ---------------------------------------------------------------------------
# Memory profiling
# ---------------------------------------------------------------------------

class TestMemoryProfiling:
    """Track memory usage patterns under load."""

    @pytest.mark.asyncio
    async def test_memory_under_load(self):
        """
        Submit 1,000 transactions and verify peak memory is below 256 MB.
        (Conservative threshold suitable for CI environments.)
        """
        agent = await _setup_agent("mem-agent", balance=10_000_000)
        tracemalloc.start()

        for i in range(1_000):
            tx = Transaction(
                sender="mem-agent",
                recipient="recipient",
                amount=1,
                fee=1,
                nonce=i,
            )
            await agent.submit_transaction(tx)

        await asyncio.sleep(0.1)

        current, peak = tracemalloc.get_traced_memory()
        tracemalloc.stop()

        peak_mb = peak / (1024 ** 2)
        assert peak_mb < 256, (
            f"Peak memory {peak_mb:.1f} MB exceeds 256 MB threshold"
        )

    @pytest.mark.asyncio
    async def test_gc_impact(self):
        """Verify that a GC cycle does not corrupt agent state."""
        agent = await _setup_agent("gc-agent")

        for i in range(50):
            tx = Transaction(
                sender="gc-agent",
                recipient="recipient",
                amount=1,
                fee=1,
                nonce=i,
            )
            await agent.submit_transaction(tx)

        # Force GC and verify agent is still operational
        gc.collect()

        status = agent.get_status()
        assert status["id"] == "gc-agent"
        assert status["started"] is True


# ---------------------------------------------------------------------------
# Orchestrator-based load test
# ---------------------------------------------------------------------------

class TestOrchestratorLoad:
    """End-to-end load tests using the StressTestOrchestrator."""

    @pytest.mark.asyncio
    async def test_light_profile(self):
        """Run the LIGHT workload profile end-to-end."""
        config = get_light_config()
        orch = StressTestOrchestrator(config)
        await orch.setup()
        try:
            metrics = await orch.run()
            assert metrics.transactions_submitted == config.transactions.num_transactions
            assert metrics.tps > 0
        finally:
            await orch.teardown()

    @pytest.mark.asyncio
    async def test_medium_profile(self):
        """Run the MEDIUM workload profile end-to-end."""
        config = get_medium_config()
        orch = StressTestOrchestrator(config)
        await orch.setup()
        try:
            metrics = await orch.run()
            assert metrics.transactions_submitted == config.transactions.num_transactions
        finally:
            await orch.teardown()

    @pytest.mark.asyncio
    async def test_burst_pattern(self):
        """Run a burst-pattern workload with explicit config."""
        from tests.stress_tests.config import (
            AgentStressConfig,
            StressTestConfig,
            TransactionConfig,
            TimingConfig,
            WorkloadProfile,
        )

        config = StressTestConfig(
            profile=WorkloadProfile.MEDIUM,
            agents=AgentStressConfig(num_validators=4, num_citizens=1),
            transactions=TransactionConfig(
                num_transactions=100,
                pattern="burst",
                burst_size=25,
            ),
            timing=TimingConfig(test_duration_seconds=5.0),
        )
        orch = StressTestOrchestrator(config)
        await orch.setup()
        try:
            metrics = await orch.run()
            assert metrics.transactions_submitted == 100
        finally:
            await orch.teardown()

    @pytest.mark.asyncio
    async def test_ramp_pattern(self):
        """Run a ramp-pattern workload with explicit config."""
        from tests.stress_tests.config import (
            AgentStressConfig,
            StressTestConfig,
            TransactionConfig,
            TimingConfig,
            WorkloadProfile,
        )

        config = StressTestConfig(
            profile=WorkloadProfile.MEDIUM,
            agents=AgentStressConfig(num_validators=4, num_citizens=1),
            transactions=TransactionConfig(
                num_transactions=50,
                pattern="ramp",
                ramp_steps=5,
            ),
            timing=TimingConfig(test_duration_seconds=5.0),
        )
        orch = StressTestOrchestrator(config)
        await orch.setup()
        try:
            metrics = await orch.run()
            assert metrics.transactions_submitted == 50
        finally:
            await orch.teardown()
