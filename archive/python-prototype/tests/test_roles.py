"""Tests for SYNTHOS Agent roles"""

import asyncio
import pytest
from src.core import SyntHOSAgent, AgentConfig, EventType, Event
from src.roles import (
    ValidatorRole,
    EconomistRole,
    GovernorRole,
    CommunicatorRole,
    SimulatorRole,
    EnforcerRole,
    CitizenRole,
)
from src.models import Transaction, Block, Proposal, Vote


async def create_test_agent():
    """Helper to create a test agent with all roles"""
    config = AgentConfig(id="test-agent", network="testnet")
    agent = SyntHOSAgent(config)
    
    agent.register_role(ValidatorRole(agent))
    agent.register_role(EconomistRole(agent))
    agent.register_role(GovernorRole(agent))
    agent.register_role(CommunicatorRole(agent))
    agent.register_role(SimulatorRole(agent))
    agent.register_role(EnforcerRole(agent))
    agent.register_role(CitizenRole(agent))
    
    await agent.initialize()
    return agent


class TestValidatorRole:
    """Test Validator role"""
    
    @pytest.mark.asyncio
    async def test_validator_initialization(self):
        """Test validator initialization"""
        agent = await create_test_agent()
        validator = agent.get_role("Validator")
        
        assert validator is not None
        assert validator.name == "Validator"
        assert len(validator._validation_stats) > 0
    
    @pytest.mark.asyncio
    async def test_transaction_validation(self):
        """Test transaction validation"""
        agent = await create_test_agent()
        validator = agent.get_role("Validator")
        
        # Create transaction
        tx = Transaction(
            sender="alice",
            recipient="bob",
            amount=100,
            fee=1,
            nonce=0
        )
        
        # Set sender balance high enough
        await agent.state.set_balance("alice", 1000)
        
        # Validate
        is_valid = await validator.validate_transaction(tx)
        
        assert is_valid == True


class TestEconomistRole:
    """Test Economist role"""
    
    @pytest.mark.asyncio
    async def test_fee_calculation(self):
        """Test fee calculation"""
        agent = await create_test_agent()
        economist = agent.get_role("Economist")
        
        # Create transaction
        tx = Transaction(
            sender="alice",
            recipient="bob",
            amount=100,
            fee=1,
            nonce=0,
            data=b"test data"
        )
        
        fee = await economist.calculate_fee(tx)
        
        assert isinstance(fee, int)
        assert fee > 0
    
    @pytest.mark.asyncio
    async def test_block_reward_calculation(self):
        """Test block reward calculation"""
        agent = await create_test_agent()
        economist = agent.get_role("Economist")
        
        # Create block with transactions
        block = Block(
            height=1,
            proposer="validator1",
            transactions=[
                Transaction(sender="alice", recipient="bob", amount=100, fee=1, nonce=0),
                Transaction(sender="bob", recipient="charlie", amount=50, fee=1, nonce=0),
            ]
        )
        
        reward = await economist.calculate_block_reward(block)
        
        assert isinstance(reward, int)
        assert reward > 0
    
    @pytest.mark.asyncio
    async def test_reward_distribution(self):
        """Test reward distribution"""
        agent = await create_test_agent()
        economist = agent.get_role("Economist")
        
        initial_balance = 1000
        await agent.state.set_balance("validator1", initial_balance)
        
        # Distribute reward
        await economist.distribute_reward("validator1", 100)
        
        new_balance = await agent.state.get_balance("validator1")
        assert new_balance == initial_balance + 100


class TestGovernorRole:
    """Test Governor role"""
    
    @pytest.mark.asyncio
    async def test_proposal_creation(self):
        """Test proposal creation"""
        agent = await create_test_agent()
        governor = agent.get_role("Governor")
        
        proposal = Proposal(
            id="prop-001",
            proposer=agent.id,
            change_type="FEE_ADJUSTMENT",
            parameters={"new_fee": 2}
        )
        
        proposal_id = await governor.propose_change(proposal)
        
        assert proposal_id in governor.proposals
        assert governor._governance_metrics['proposals_created'] == 1
    
    @pytest.mark.asyncio
    async def test_voting(self):
        """Test voting on proposal"""
        agent = await create_test_agent()
        governor = agent.get_role("Governor")
        
        proposal = Proposal(
            id="prop-001",
            proposer=agent.id,
            change_type="FEE_ADJUSTMENT",
            parameters={"new_fee": 2}
        )
        
        proposal_id = await governor.propose_change(proposal)
        
        # Vote
        result = await governor.vote(proposal_id, vote_value=True)
        assert result == True
        
        proposal_data = governor.proposals[proposal_id]
        assert proposal_data['votes_for'] == 1
    
    @pytest.mark.asyncio
    async def test_vote_finalization(self):
        """Test vote finalization"""
        agent = await create_test_agent()
        governor = agent.get_role("Governor")
        
        proposal = Proposal(
            id="prop-001",
            proposer=agent.id,
            change_type="FEE_ADJUSTMENT",
            parameters={"new_fee": 2}
        )
        
        proposal_id = await governor.propose_change(proposal)
        
        # Vote yes multiple times
        await governor.vote(proposal_id, vote_value=True)
        await governor.vote(proposal_id, vote_value=True)
        
        # Vote no once
        await governor.vote(proposal_id, vote_value=False)
        
        # Finalize
        passed = await governor.finalize_vote(proposal_id)
        
        assert passed == True
        assert governor._governance_metrics['proposals_passed'] == 1


class TestCommunicatorRole:
    """Test Communicator role"""
    
    @pytest.mark.asyncio
    async def test_peer_connection(self):
        """Test peer connection"""
        agent = await create_test_agent()
        communicator = agent.get_role("Communicator")
        
        class MockPeer:
            def __init__(self, id):
                self.id = id
        
        peer = MockPeer("peer-001")
        
        result = await communicator.connect_peer(peer)
        
        assert result == True
        assert "peer-001" in communicator.peers
        assert communicator._network_metrics['peers_connected'] == 1
    
    @pytest.mark.asyncio
    async def test_message_sending(self):
        """Test message sending"""
        agent = await create_test_agent()
        communicator = agent.get_role("Communicator")
        
        class MockPeer:
            def __init__(self, id):
                self.id = id
        
        # Connect peer
        peer = MockPeer("peer-001")
        await communicator.connect_peer(peer)
        
        # Send message
        result = await communicator.send_message("peer-001", {"type": "hello"})
        
        assert result == True
        assert communicator._network_metrics['messages_sent'] == 1


class TestEnforcerRole:
    """Test Enforcer role"""
    
    @pytest.mark.asyncio
    async def test_penalty_application(self):
        """Test penalty application"""
        agent = await create_test_agent()
        enforcer = agent.get_role("Enforcer")
        
        # Set initial balance
        await agent.state.set_balance("violator", 1000)
        
        # Apply penalty
        result = await enforcer.apply_penalty("violator", 100)
        
        assert result == True
        
        # Check new balance
        new_balance = await agent.state.get_balance("violator")
        assert new_balance == 900


class TestSimulatorRole:
    """Test Simulator role"""
    
    @pytest.mark.asyncio
    async def test_protocol_simulation(self):
        """Test protocol change simulation"""
        agent = await create_test_agent()
        simulator = agent.get_role("Simulator")
        
        change = {"type": "FEE", "value": 2}
        
        result = await simulator.simulate_protocol_change(change)
        
        assert "simulated_state" in result
        assert "predicted_impact" in result
        assert simulator._simulation_metrics['simulations_run'] > 0
    
    @pytest.mark.asyncio
    async def test_economic_simulation(self):
        """Test economic scenario simulation"""
        agent = await create_test_agent()
        simulator = agent.get_role("Simulator")
        
        scenario = {"market_cap": 1000000, "volume": 100000}
        
        result = await simulator.simulate_economic_scenario(scenario)
        
        assert "predicted_outcomes" in result
        assert "confidence" in result


class TestCitizenRole:
    """Test Citizen role"""
    
    @pytest.mark.asyncio
    async def test_transaction_submission(self):
        """Test transaction submission"""
        agent = await create_test_agent()
        citizen = agent.get_role("Citizen")
        
        tx = Transaction(
            sender="alice",
            recipient="bob",
            amount=100,
            fee=1,
            nonce=0
        )
        
        result = await citizen.submit_transaction(tx)
        
        assert result == True
        assert citizen._citizen_metrics['transactions_submitted'] >= 1
    
    @pytest.mark.asyncio
    async def test_staking(self):
        """Test token staking"""
        agent = await create_test_agent()
        citizen = agent.get_role("Citizen")
        
        initial_balance = await agent.state.get_balance(agent.id)
        
        result = await citizen.stake_tokens(100)
        
        assert result == True
        
        new_balance = await agent.state.get_balance(agent.id)
        assert new_balance < initial_balance
    
    @pytest.mark.asyncio
    async def test_reward_claiming(self):
        """Test reward claiming"""
        agent = await create_test_agent()
        citizen = agent.get_role("Citizen")
        
        initial_balance = await agent.state.get_balance(agent.id)
        
        reward = await citizen.claim_rewards()
        
        assert reward > 0
        
        new_balance = await agent.state.get_balance(agent.id)
        assert new_balance > initial_balance


if __name__ == "__main__":
    pytest.main([__file__, "-v"])
