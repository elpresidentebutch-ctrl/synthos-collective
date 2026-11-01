"""Tests for SYNTHOS Agent core components"""

import asyncio
import pytest
from src.core import (
    SyntHOSAgent,
    AgentConfig,
    AgentState,
    StateType,
    Event,
    EventBus,
    EventType,
)


class TestAgentConfig:
    """Test configuration handling"""
    
    def test_agent_config_creation(self):
        """Test AgentConfig object creation"""
        config = AgentConfig(
            id="test-001",
            network="testnet",
            log_level="DEBUG",
            consensus_timeout_ms=5000,
            max_peers=100,
            storage_path="./test_data"
        )
        
        assert config.id == "test-001"
        assert config.network == "testnet"
        assert config.log_level == "DEBUG"
        assert config.consensus_timeout_ms == 5000
        assert config.max_peers == 100
        assert config.storage_path == "./test_data"
    
    def test_agent_config_defaults(self):
        """Test AgentConfig default values"""
        config = AgentConfig(
            id="test-001",
            network="mainnet"
        )
        
        assert config.log_level == "INFO"
        assert config.consensus_timeout_ms == 4000
        assert config.max_peers == 50
        assert config.storage_path == "./data"


class TestAgentState:
    """Test state management"""
    
    @pytest.mark.asyncio
    async def test_state_set_get(self):
        """Test setting and getting state values"""
        state = AgentState()
        
        await state.set("key1", "value1")
        result = await state.get("key1")
        
        assert result == "value1"
    
    @pytest.mark.asyncio
    async def test_state_balance_operations(self):
        """Test balance operations"""
        state = AgentState()
        
        # Set balance
        await state.set_balance("alice", 1000)
        balance = await state.get_balance("alice")
        
        assert balance == 1000
        
        # Update balance
        await state.set_balance("alice", 500)
        balance = await state.get_balance("alice")
        
        assert balance == 500
    
    @pytest.mark.asyncio
    async def test_state_transaction(self):
        """Test state transactions"""
        state = AgentState()
        
        # Start transaction
        await state.begin_transaction()
        await state.set("key1", "value1")
        await state.set("key2", "value2")
        
        # Commit
        await state.commit()
        
        # Verify data was committed
        value1 = await state.get("key1")
        value2 = await state.get("key2")
        
        assert value1 == "value1"
        assert value2 == "value2"
        assert state.version == 1
    
    @pytest.mark.asyncio
    async def test_state_rollback(self):
        """Test state rollback"""
        state = AgentState()
        
        # Start transaction
        await state.begin_transaction()
        await state.set("key1", "value1")
        
        # Rollback
        await state.rollback()
        
        # Verify data was not committed
        value1 = await state.get("key1")
        assert value1 is None
        assert state.version == 0
    
    @pytest.mark.asyncio
    async def test_state_snapshot(self):
        """Test state snapshots"""
        state = AgentState()
        
        await state.set("key1", "value1")
        await state.set_balance("alice", 1000)
        
        snapshot = await state.create_snapshot()
        
        assert snapshot.version == 0
        assert snapshot.ledger["key1"] == "value1"
        assert snapshot.hash != ""
    
    @pytest.mark.asyncio
    async def test_state_restore(self):
        """Test state restoration from snapshot"""
        state = AgentState()
        
        # Create initial state
        await state.set_balance("alice", 1000)
        snapshot1 = await state.create_snapshot()
        
        # Modify state
        await state.set_balance("alice", 500)
        assert state.version == 1
        
        # Restore to previous version
        await state.restore_snapshot(snapshot1)
        assert state.version == 0
        
        balance = await state.get_balance("alice")
        assert balance == 1000


class TestEventSystem:
    """Test event bus and event handling"""
    
    @pytest.mark.asyncio
    async def test_event_creation(self):
        """Test Event object creation"""
        event = Event(
            type=EventType.TRANSACTION_SUBMITTED,
            source="test_source",
            data={"test": "data"},
            priority=1
        )
        
        assert event.type == EventType.TRANSACTION_SUBMITTED
        assert event.source == "test_source"
        assert event.data == {"test": "data"}
        assert event.priority == 1
        assert event.event_id != ""
    
    @pytest.mark.asyncio
    async def test_event_bus_subscribe(self):
        """Test event bus subscription"""
        config = AgentConfig(id="test", network="testnet")
        agent = SyntHOSAgent(config)
        
        # Track handler calls
        calls = []
        
        async def test_handler(event):
            calls.append(event)
        
        # Subscribe
        agent.event_bus.subscribe(EventType.TRANSACTION_SUBMITTED, test_handler)
        
        # Publish event
        event = Event(
            type=EventType.TRANSACTION_SUBMITTED,
            source="test",
            data={}
        )
        await agent.event_bus.publish(event)
        await asyncio.sleep(0.1)  # Let processing happen
        
        assert len(calls) > 0
    
    @pytest.mark.asyncio
    async def test_event_history(self):
        """Test event history tracking"""
        config = AgentConfig(id="test", network="testnet")
        agent = SyntHOSAgent(config)
        
        # Publish events
        for i in range(5):
            event = Event(
                type=EventType.TRANSACTION_SUBMITTED,
                source="test",
                data={"num": i}
            )
            await agent.event_bus.publish(event)
        
        await asyncio.sleep(0.1)
        
        # Get history
        history = agent.event_bus.get_event_history(limit=10)
        
        assert len(history) >= 5


class TestSyntHOSAgent:
    """Test main agent functionality"""
    
    @pytest.mark.asyncio
    async def test_agent_initialization(self):
        """Test agent initialization"""
        config = AgentConfig(id="test-001", network="testnet")
        agent = SyntHOSAgent(config)
        
        assert agent.id == "test-001"
        assert agent.network == "testnet"
        assert agent.state is not None
        assert agent.event_bus is not None
        assert len(agent.roles) == 0
    
    @pytest.mark.asyncio
    async def test_agent_role_registration(self):
        """Test role registration"""
        from src.core.base_role import Role
        
        config = AgentConfig(id="test-001", network="testnet")
        agent = SyntHOSAgent(config)
        
        # Create mock role
        class MockRole(Role):
            async def initialize(self):
                await super().initialize()
        
        role = MockRole(agent)
        agent.register_role(role)
        
        assert "MockRole" in agent.roles
        assert agent.get_role("MockRole") == role
    
    @pytest.mark.asyncio
    async def test_agent_status(self):
        """Test agent status reporting"""
        config = AgentConfig(id="test-001", network="testnet")
        agent = SyntHOSAgent(config)
        
        await agent.initialize()
        
        status = agent.get_status()
        
        assert status["id"] == "test-001"
        assert status["network"] == "testnet"
        assert status["started"] == True
        assert "state" in status
        assert "roles" in status
    
    @pytest.mark.asyncio
    async def test_agent_checkpoint(self):
        """Test state checkpoint functionality"""
        config = AgentConfig(id="test-001", network="testnet")
        agent = SyntHOSAgent(config)
        
        await agent.initialize()
        
        # Create checkpoint
        checkpoint = await agent.create_state_checkpoint()
        
        assert checkpoint != ""
        assert len(checkpoint) == 64  # SHA256 hex length
    
    @pytest.mark.asyncio
    async def test_agent_event_history_retrieval(self):
        """Test agent event history retrieval"""
        config = AgentConfig(id="test-001", network="testnet")
        agent = SyntHOSAgent(config)
        
        await agent.initialize()
        
        # Get history
        history = await agent.get_event_history(limit=100)
        
        assert isinstance(history, list)


if __name__ == "__main__":
    pytest.main([__file__, "-v"])
