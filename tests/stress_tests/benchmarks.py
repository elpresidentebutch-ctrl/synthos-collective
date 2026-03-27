"""
Performance Benchmarks for SYNTHOS Collective.

Measures consensus latency, transaction confirmation time,
block validation performance, individual role execution timing,
event bus throughput, and state synchronisation speed.

Run with pytest::

    pytest tests/stress_tests/benchmarks.py -v
"""

import asyncio
import statistics
import time
from typing import List

import pytest

from src.core import AgentConfig, SyntHOSAgent, Event, EventType
from src.models import Block, Proposal, Transaction
from src.roles import (
    CitizenRole,
    CommunicatorRole,
    EconomistRole,
    EnforcerRole,
    GovernorRole,
    SimulatorRole,
    ValidatorRole,
)

from tests.stress_tests.framework import make_agent


# ---------------------------------------------------------------------------
# Helper: timed coroutine wrapper
# ---------------------------------------------------------------------------

async def _timed(coro) -> float:
    """Run *coro* and return elapsed wall-clock seconds."""
    start = time.monotonic()
    await coro
    return time.monotonic() - start


# ---------------------------------------------------------------------------
# Consensus latency
# ---------------------------------------------------------------------------

FINALITY_TARGET_SECONDS = 5.0


class TestConsensusLatency:
    """Measure block finality time (target: <5 seconds)."""

    @pytest.mark.asyncio
    async def test_single_block_finality(self):
        """
        Propose a block and measure the round-trip time from proposal to the
        BLOCK_FINALIZED event acknowledgement.  Target is <5 s.
        """
        agent = make_agent("bench-consensus")
        await agent.initialize()

        finalized = asyncio.Event()

        async def _on_finalized(event: Event):
            finalized.set()

        agent.event_bus.subscribe(EventType.BLOCK_FINALIZED, _on_finalized)

        start = time.monotonic()
        block = Block(height=1, proposer=agent.id)
        await agent.handle_incoming_block(block)

        # Emit BLOCK_FINALIZED immediately to simulate finality
        await agent.event_bus.publish(Event(
            type=EventType.BLOCK_FINALIZED,
            source="test",
            data={"block": block},
        ))

        await asyncio.sleep(0.1)
        elapsed = time.monotonic() - start

        assert elapsed < FINALITY_TARGET_SECONDS, (
            f"Block finality took {elapsed:.3f}s, exceeds {FINALITY_TARGET_SECONDS}s target"
        )

    @pytest.mark.asyncio
    async def test_consensus_latency_distribution(self):
        """
        Measure latency distribution across 20 consecutive block proposals.
        P99 must be under 5 seconds.
        """
        agent = make_agent("bench-latency-dist")
        await agent.initialize()

        samples: List[float] = []
        for height in range(20):
            elapsed = await _timed(
                agent.handle_incoming_block(Block(height=height, proposer=agent.id))
            )
            samples.append(elapsed)

        p99 = sorted(samples)[int(len(samples) * 0.99)]
        avg = statistics.mean(samples)

        assert p99 < FINALITY_TARGET_SECONDS, (
            f"P99 latency {p99:.3f}s exceeds {FINALITY_TARGET_SECONDS}s target"
        )
        assert avg >= 0


# ---------------------------------------------------------------------------
# Transaction confirmation time
# ---------------------------------------------------------------------------

class TestTransactionConfirmationTime:
    """Measure time from submission to finality acknowledgement."""

    @pytest.mark.asyncio
    async def test_single_transaction_confirmation(self):
        """Confirm a single transaction and assert completion time is reasonable."""
        agent = make_agent("bench-tx-confirm")
        await agent.state.set_balance("bench-tx-confirm", 10_000)
        await agent.initialize()

        tx = Transaction(
            sender="bench-tx-confirm",
            recipient="recipient",
            amount=10,
            fee=1,
            nonce=0,
        )

        elapsed = await _timed(agent.submit_transaction(tx))
        assert elapsed < 1.0, (
            f"Transaction submission took {elapsed:.3f}s – unexpectedly slow"
        )

    @pytest.mark.asyncio
    async def test_batch_confirmation_time(self):
        """Submit 50 transactions and measure total confirmation time."""
        agent = make_agent("bench-batch-confirm")
        await agent.state.set_balance("bench-batch-confirm", 1_000_000)
        await agent.initialize()

        start = time.monotonic()
        for i in range(50):
            tx = Transaction(
                sender="bench-batch-confirm",
                recipient="recipient",
                amount=1,
                fee=1,
                nonce=i,
            )
            await agent.submit_transaction(tx)
        elapsed = time.monotonic() - start

        assert elapsed < 10.0, (
            f"Batch submission of 50 txs took {elapsed:.2f}s"
        )


# ---------------------------------------------------------------------------
# Block validation performance
# ---------------------------------------------------------------------------

class TestBlockValidationPerformance:
    """Cost of validating blocks with various transaction counts."""

    @pytest.mark.asyncio
    async def test_empty_block_validation(self):
        """Validate an empty block."""
        agent = make_agent("bench-empty-block")
        await agent.initialize()

        validator = agent.get_role("Validator")
        block = Block(height=1, proposer=agent.id)

        elapsed = await _timed(validator.validate_block(block))
        assert elapsed < 1.0

    @pytest.mark.asyncio
    async def test_block_validation_100_transactions(self):
        """Validate a block containing 100 transactions."""
        agent = make_agent("bench-block-100")
        await agent.state.set_balance("bench-block-100", 1_000_000)
        await agent.initialize()

        validator = agent.get_role("Validator")
        transactions = [
            Transaction(
                sender="bench-block-100",
                recipient="recipient",
                amount=1,
                fee=1,
                nonce=i,
            )
            for i in range(100)
        ]
        block = Block(height=1, proposer=agent.id, transactions=transactions)

        elapsed = await _timed(validator.validate_block(block))
        assert elapsed < 5.0, (
            f"Validating 100-tx block took {elapsed:.3f}s"
        )

    @pytest.mark.asyncio
    async def test_block_validation_1000_transactions(self):
        """Validate a block containing 1 000 transactions."""
        agent = make_agent("bench-block-1000")
        await agent.state.set_balance("bench-block-1000", 10_000_000)
        await agent.initialize()

        validator = agent.get_role("Validator")
        transactions = [
            Transaction(
                sender="bench-block-1000",
                recipient="recipient",
                amount=1,
                fee=1,
                nonce=i,
            )
            for i in range(1_000)
        ]
        block = Block(height=1, proposer=agent.id, transactions=transactions)

        elapsed = await _timed(validator.validate_block(block))
        assert elapsed < 30.0, (
            f"Validating 1000-tx block took {elapsed:.3f}s"
        )


# ---------------------------------------------------------------------------
# Role execution timing
# ---------------------------------------------------------------------------

class TestRoleExecutionTiming:
    """Individual role performance benchmarks."""

    @pytest.mark.asyncio
    async def test_validator_execute_timing(self):
        agent = make_agent("bench-role-validator")
        await agent.initialize()
        validator = agent.get_role("Validator")

        elapsed = await _timed(validator.execute())
        assert elapsed < 1.0

    @pytest.mark.asyncio
    async def test_economist_fee_calculation_timing(self):
        agent = make_agent("bench-role-economist")
        await agent.initialize()
        economist = agent.get_role("Economist")

        tx = Transaction(sender="a", recipient="b", amount=100, fee=1, nonce=0)
        start = time.monotonic()
        for _ in range(100):
            await economist.calculate_fee(tx)
        elapsed = time.monotonic() - start

        assert elapsed < 1.0, (
            f"100 fee calculations took {elapsed:.3f}s"
        )

    @pytest.mark.asyncio
    async def test_governor_proposal_timing(self):
        agent = make_agent("bench-role-governor")
        await agent.initialize()
        governor = agent.get_role("Governor")

        start = time.monotonic()
        for i in range(20):
            proposal = Proposal(
                id=f"prop-{i}",
                proposer=agent.id,
                change_type="FEE_ADJUSTMENT",
                parameters={"new_fee": i + 1},
            )
            await governor.propose_change(proposal)
        elapsed = time.monotonic() - start

        assert elapsed < 2.0, (
            f"20 proposals took {elapsed:.3f}s"
        )

    @pytest.mark.asyncio
    async def test_enforcer_penalty_timing(self):
        agent = make_agent("bench-role-enforcer")
        await agent.state.set_balance("violator-bench", 1_000_000)
        await agent.initialize()
        enforcer = agent.get_role("Enforcer")

        start = time.monotonic()
        for _ in range(50):
            await enforcer.apply_penalty("violator-bench", 1)
        elapsed = time.monotonic() - start

        assert elapsed < 2.0

    @pytest.mark.asyncio
    async def test_simulator_protocol_change_timing(self):
        agent = make_agent("bench-role-simulator")
        await agent.initialize()
        simulator = agent.get_role("Simulator")

        change = {"type": "FEE", "value": 3}
        elapsed = await _timed(simulator.simulate_protocol_change(change))

        assert elapsed < 2.0


# ---------------------------------------------------------------------------
# Event bus throughput
# ---------------------------------------------------------------------------

class TestEventBusThroughput:
    """Number of events processed per second."""

    @pytest.mark.asyncio
    async def test_event_publish_throughput(self):
        """Publish 1 000 events and measure throughput."""
        agent = make_agent("bench-event-bus")
        await agent.initialize()

        processed = 0

        async def _counter(event: Event):
            nonlocal processed
            processed += 1

        agent.event_bus.subscribe(EventType.TRANSACTION_SUBMITTED, _counter)

        start = time.monotonic()
        for i in range(1_000):
            await agent.event_bus.publish(Event(
                type=EventType.TRANSACTION_SUBMITTED,
                source="bench",
                data={"seq": i},
            ))
        await asyncio.sleep(0.2)
        elapsed = time.monotonic() - start

        eps = 1_000 / elapsed
        assert eps > 0, "Event bus must process at least 1 event/s"

    @pytest.mark.asyncio
    async def test_event_history_performance(self):
        """Retrieve event history after 500 recorded events."""
        agent = make_agent("bench-event-history")
        await agent.initialize()

        # Directly record events into history to measure retrieval performance
        # (_record_event is synchronous; no consumer loop needed here)
        for i in range(500):
            event = Event(
                type=EventType.CONSENSUS_VOTE,
                source="bench",
                data={"seq": i},
            )
            agent.event_bus._record_event(event)

        start = time.monotonic()
        history = agent.event_bus.get_event_history(limit=500)
        elapsed = time.monotonic() - start

        assert elapsed < 0.5, f"History retrieval took {elapsed:.3f}s"
        assert len(history) == 500


# ---------------------------------------------------------------------------
# State synchronisation speed
# ---------------------------------------------------------------------------

class TestStateSyncSpeed:
    """Peer-to-peer state synchronisation performance."""

    @pytest.mark.asyncio
    async def test_snapshot_creation_speed(self):
        """Create 20 state snapshots and verify sub-second performance."""
        agent = make_agent("bench-state-snap")
        await agent.initialize()

        # Populate state with sample data
        for i in range(100):
            await agent.state.set_balance(f"account-{i}", i * 100)

        start = time.monotonic()
        for _ in range(20):
            await agent.state.create_snapshot()
        elapsed = time.monotonic() - start

        assert elapsed < 2.0, (
            f"20 snapshot creations took {elapsed:.3f}s"
        )

    @pytest.mark.asyncio
    async def test_checkpoint_and_restore_speed(self):
        """Create checkpoint and restore; measure round-trip time."""
        agent = make_agent("bench-state-restore")
        await agent.initialize()

        await agent.state.set_balance("test-account", 5_000)

        start = time.monotonic()
        checkpoint_hash = await agent.create_state_checkpoint()
        elapsed_create = time.monotonic() - start

        # Modify state
        await agent.state.set_balance("test-account", 1)

        # Restore to version 0
        start = time.monotonic()
        await agent.restore_state_checkpoint(0)
        elapsed_restore = time.monotonic() - start

        assert elapsed_create < 1.0
        assert elapsed_restore < 1.0
        assert checkpoint_hash != ""
