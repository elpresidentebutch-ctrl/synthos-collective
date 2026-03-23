"""Main SYNTHOS Agent implementation"""

import asyncio
import logging
from typing import Dict, Any, Optional, List
from datetime import datetime
from dataclasses import dataclass

from src.core.state import AgentState
from src.core.event import EventBus, EventType, Event
from src.core.base_role import Role


@dataclass
class AgentConfig:
    """Configuration for SYNTHOS Agent"""
    id: str
    network: str  # mainnet, testnet, devnet
    log_level: str = "INFO"
    consensus_timeout_ms: int = 4000
    max_peers: int = 50
    storage_path: str = "./data"


class SyntHOSAgent:
    """
    Main SYNTHOS Agent class
    
    A sovereign computational entity that collectively forms the blockchain.
    Encapsulates seven core roles:
    - Validator
    - Economist
    - Governor
    - Communicator
    - Simulator
    - Enforcer
    - Citizen
    """
    
    def __init__(self, config: AgentConfig):
        """
        Initialize SYNTHOS Agent
        
        Args:
            config: Agent configuration
        """
        self.config = config
        self.id = config.id
        self.network = config.network
        
        # Core systems
        self.state = AgentState()
        self.event_bus = EventBus(self)
        
        # Logging
        self.logger = self._setup_logger()
        
        # Roles (will be initialized by subclass or factory)
        self.roles: Dict[str, Role] = {}
        
        # Agent status
        self.started = False
        self.running = False
    
    def _setup_logger(self) -> logging.Logger:
        """Setup logging for agent"""
        logger = logging.getLogger(f"SYNTHOS.{self.id}")
        logger.setLevel(logging.getLevelName(self.config.log_level))
        
        # Console handler
        handler = logging.StreamHandler()
        formatter = logging.Formatter(
            '%(asctime)s - %(name)s - %(levelname)s - %(message)s'
        )
        handler.setFormatter(formatter)
        logger.addHandler(handler)
        
        return logger
    
    def register_role(self, role: Role) -> None:
        """
        Register a role with this agent
        
        Args:
            role: Role instance to register
        """
        self.roles[role.name] = role
        self.logger.info(f"Registered role: {role.name} v{role.version}")
    
    def get_role(self, role_name: str) -> Optional[Role]:
        """Get role by name"""
        return self.roles.get(role_name)
    
    async def initialize(self) -> None:
        """Initialize agent and all roles"""
        self.logger.info(f"Initializing agent {self.id} on network {self.network}")
        
        try:
            # Initialize state
            await self.state.begin_transaction()
            await self.state.set('agent_id', self.id)
            await self.state.set('network', self.network)
            await self.state.set('started_at', datetime.now().timestamp())
            await self.state.commit()
            
            # Initialize all roles
            for role_name, role in self.roles.items():
                self.logger.info(f"Initializing role: {role_name}")
                await role.initialize()
            
            self.started = True
            self.logger.info("Agent initialization complete")
            
            # Record event
            await self.event_bus.publish(Event(
                type=EventType.CONSENSUS_ROUND_START,
                source='agent',
                data={'agent_id': self.id, 'action': 'initialized'}
            ))
            
        except Exception as e:
            self.logger.error(f"Error initializing agent: {e}")
            raise
    
    async def start(self) -> None:
        """Start agent operations"""
        if not self.started:
            await self.initialize()
        
        self.running = True
        self.logger.info(f"Starting agent {self.id}")
        
        # Start role execution and event processing concurrently
        tasks = [
            self.event_bus.process_events(),
            self._run_roles(),
        ]
        
        await asyncio.gather(*tasks)
    
    async def _run_roles(self) -> None:
        """Execute all roles"""
        while self.running:
            try:
                # Execute each role
                for role_name, role in self.roles.items():
                    if role.enabled:
                        try:
                            await role.execute()
                        except Exception as e:
                            await role.handle_error(e)
                
                # Yield control
                await asyncio.sleep(0.1)
                
            except Exception as e:
                self.logger.error(f"Error running roles: {e}")
                await asyncio.sleep(1.0)
    
    async def stop(self) -> None:
        """Stop agent operations"""
        self.logger.info(f"Stopping agent {self.id}")
        self.running = False
        
        # Stop event processing
        self.event_bus.stop_processing()
        
        # Finalize all roles
        for role_name, role in self.roles.items():
            try:
                await role.finalize()
            except Exception as e:
                self.logger.error(f"Error finalizing role {role_name}: {e}")
        
        self.logger.info("Agent stopped")
    
    async def submit_transaction(self, transaction: Any) -> bool:
        """
        Submit transaction to agent
        
        Args:
            transaction: Transaction object
            
        Returns:
            True if transaction was accepted
        """
        await self.event_bus.publish(Event(
            type=EventType.TRANSACTION_SUBMITTED,
            source='citizen',
            data={'transaction': transaction},
            priority=2
        ))
        return True
    
    async def handle_incoming_block(self, block: Any) -> bool:
        """
        Handle incoming block from network
        
        Args:
            block: Block object
            
        Returns:
            True if block was accepted
        """
        await self.event_bus.publish(Event(
            type=EventType.BLOCK_PROPOSED,
            source='communicator',
            data={'block': block},
            priority=1
        ))
        return True
    
    async def handle_incoming_proposal(self, proposal: Any) -> bool:
        """
        Handle governance proposal
        
        Args:
            proposal: Proposal object
            
        Returns:
            True if proposal was accepted
        """
        await self.event_bus.publish(Event(
            type=EventType.PROPOSAL_SUBMITTED,
            source='communicator',
            data={'proposal': proposal},
            priority=2
        ))
        return True
    
    def get_status(self) -> Dict[str, Any]:
        """Get agent status"""
        return {
            'id': self.id,
            'network': self.network,
            'started': self.started,
            'running': self.running,
            'roles': {
                role_name: role.get_state()
                for role_name, role in self.roles.items()
            },
            'state': self.state.get_state(),
        }
    
    async def create_state_checkpoint(self) -> str:
        """Create checkpoint of current state"""
        snapshot = await self.state.create_snapshot()
        self.logger.info(f"Created state checkpoint: {snapshot.hash}")
        return snapshot.hash
    
    async def restore_state_checkpoint(self, version: int) -> bool:
        """Restore state from checkpoint"""
        snapshot = await self.state.fork_at_version(version)
        if snapshot:
            await self.state.restore_snapshot(snapshot)
            self.logger.info(f"Restored state to version {version}")
            return True
        return False
    
    def enable_role(self, role_name: str) -> None:
        """Enable a role"""
        role = self.get_role(role_name)
        if role:
            role.set_enabled(True)
            self.logger.info(f"Enabled role: {role_name}")
    
    def disable_role(self, role_name: str) -> None:
        """Disable a role"""
        role = self.get_role(role_name)
        if role:
            role.set_enabled(False)
            self.logger.info(f"Disabled role: {role_name}")
    
    async def get_event_history(self, 
                               event_type: Optional[EventType] = None,
                               limit: int = 100) -> List[Dict]:
        """Get agent event history"""
        events = self.event_bus.get_event_history(event_type, limit)
        return [
            {
                'id': e.event_id,
                'type': e.type.value,
                'source': e.source,
                'timestamp': e.timestamp,
                'data': e.data,
            }
            for e in events
        ]
