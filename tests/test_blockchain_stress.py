"""Regression and stress tests for the SYNTHOS agent-chain implementation."""

import pytest

from src.core.blockchain import SynthosBlockchain
from src.core.blockchain_integration import AgentNetworkBlockchain


class TestSynthosBlockchain:
    """Test consensus behavior in the agent-backed blockchain."""

    @pytest.mark.asyncio
    async def test_consensus_round_requires_agents(self):
        """Consensus should fail cleanly when no agents are registered."""
        blockchain = SynthosBlockchain()

        success, message = await blockchain.run_consensus_round()

        assert success is False
        assert message == "No agents available for consensus"

    @pytest.mark.asyncio
    async def test_stress_test_preserves_chain_integrity(self):
        """A short stress run should create finalized blocks on a valid chain."""
        network = AgentNetworkBlockchain(num_agents=3)

        stats = await network.run_stress_test(duration_seconds=1, agents_per_round=2)

        assert stats["consensus_rounds"] > 0
        assert stats["blocks_created"] > 0
        assert stats["chain_height"] == stats["blocks_created"]
        assert stats["total_transactions_confirmed"] > 0
        assert stats["agents_synchronized"] is True
        assert stats["is_chain_valid"] is True