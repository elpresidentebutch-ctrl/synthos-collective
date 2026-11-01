"""State management for SYNTHOS Agents"""

from typing import Any, Dict, List, Optional
from dataclasses import dataclass, field
from datetime import datetime
import json
import hashlib
from enum import Enum


class StateType(Enum):
    """Types of state"""
    LEDGER = "ledger"
    CONSENSUS = "consensus"
    REPUTATION = "reputation"
    RESOURCES = "resources"


@dataclass
class StateSnapshot:
    """Snapshot of agent state"""
    timestamp: float
    version: int
    ledger: Dict[str, Any]
    consensus: Dict[str, Any]
    reputation: Dict[str, Any]
    resources: Dict[str, Any]
    hash: str = ""
    
    def compute_hash(self) -> str:
        """Compute deterministic hash of snapshot"""
        data = {
            'ledger': self.ledger,
            'consensus': self.consensus,
            'reputation': self.reputation,
            'resources': self.resources,
            'version': self.version,
        }
        json_str = json.dumps(data, sort_keys=True)
        return hashlib.sha256(json_str.encode()).hexdigest()


class AgentState:
    """
    Centralized state management for SYNTHOS Agent
    
    Manages:
    - Transactional consistency
    - State versioning
    - Snapshots and rollback
    """
    
    def __init__(self):
        self.data: Dict[str, Dict[str, Any]] = {
            StateType.LEDGER.value: {},
            StateType.CONSENSUS.value: {},
            StateType.REPUTATION.value: {},
            StateType.RESOURCES.value: {},
        }
        self.version = 0
        self.snapshots: List[StateSnapshot] = []
        self.transaction_buffer: Dict[str, Any] = {}
        self.in_transaction = False
        self.committed_version = 0
    
    async def get(self, key: str, state_type: StateType = None) -> Any:
        """
        Get value by key
        
        Args:
            key: Key to retrieve
            state_type: Optional specific state type to search
            
        Returns:
            Value or None if not found
        """
        if state_type:
            return self.data[state_type.value].get(key)
        
        # Search all state types
        for state_dict in self.data.values():
            if key in state_dict:
                return state_dict[key]
        
        return None
    
    async def set(self, key: str, value: Any, 
                 state_type: StateType = StateType.LEDGER) -> None:
        """
        Set value by key
        
        Args:
            key: Key to set
            value: Value to store
            state_type: State type category
        """
        if self.in_transaction:
            self.transaction_buffer[f"{state_type.value}:{key}"] = value
        else:
            self.data[state_type.value][key] = value
    
    async def get_balance(self, account: str) -> int:
        """Get account balance"""
        ledger = self.data[StateType.LEDGER.value]
        accounts = ledger.get('accounts', {})
        return accounts.get(account, {}).get('balance', 0)
    
    async def set_balance(self, account: str, balance: int) -> None:
        """Set account balance"""
        ledger = self.data[StateType.LEDGER.value]
        if 'accounts' not in ledger:
            ledger['accounts'] = {}
        if account not in ledger['accounts']:
            ledger['accounts'][account] = {}
        ledger['accounts'][account]['balance'] = balance
    
    async def begin_transaction(self) -> None:
        """Begin transaction"""
        self.transaction_buffer = {}
        self.in_transaction = True
    
    async def commit(self) -> None:
        """
        Commit transaction
        
        Applies all buffered changes and increments version
        """
        if not self.in_transaction:
            raise RuntimeError("No transaction in progress")
        
        # Apply buffered changes
        for key, value in self.transaction_buffer.items():
            state_type_str, actual_key = key.split(':', 1)
            self.data[state_type_str][actual_key] = value
        
        # Clear buffer and increment version
        self.transaction_buffer = {}
        self.in_transaction = False
        self.version += 1
        self.committed_version = self.version
    
    async def rollback(self) -> None:
        """Rollback transaction"""
        if not self.in_transaction:
            raise RuntimeError("No transaction in progress")
        
        self.transaction_buffer = {}
        self.in_transaction = False
    
    async def create_snapshot(self) -> StateSnapshot:
        """Create snapshot of current state"""
        snapshot = StateSnapshot(
            timestamp=datetime.now().timestamp(),
            version=self.version,
            ledger=self.data[StateType.LEDGER.value].copy(),
            consensus=self.data[StateType.CONSENSUS.value].copy(),
            reputation=self.data[StateType.REPUTATION.value].copy(),
            resources=self.data[StateType.RESOURCES.value].copy(),
        )
        snapshot.hash = snapshot.compute_hash()
        
        self.snapshots.append(snapshot)
        return snapshot
    
    async def restore_snapshot(self, snapshot: StateSnapshot) -> None:
        """Restore state from snapshot"""
        self.data[StateType.LEDGER.value] = snapshot.ledger.copy()
        self.data[StateType.CONSENSUS.value] = snapshot.consensus.copy()
        self.data[StateType.REPUTATION.value] = snapshot.reputation.copy()
        self.data[StateType.RESOURCES.value] = snapshot.resources.copy()
        self.version = snapshot.version
        self.transaction_buffer = {}
        self.in_transaction = False
    
    async def fork_at_version(self, version: int) -> Optional[StateSnapshot]:
        """
        Get snapshot at specific version
        
        Used for simulations and rollbacks
        """
        for snapshot in self.snapshots:
            if snapshot.version == version:
                return snapshot
        return None
    
    def get_state(self) -> Dict[str, Any]:
        """Get current state snapshot"""
        return {
            'version': self.version,
            'committed_version': self.committed_version,
            'ledger': self.data[StateType.LEDGER.value].copy(),
            'consensus': self.data[StateType.CONSENSUS.value].copy(),
            'reputation': self.data[StateType.REPUTATION.value].copy(),
            'resources': self.data[StateType.RESOURCES.value].copy(),
            'in_transaction': self.in_transaction,
        }
    
    async def get_ledger_state(self) -> Dict[str, Any]:
        """Get ledger state"""
        return self.data[StateType.LEDGER.value].copy()
    
    async def get_consensus_state(self) -> Dict[str, Any]:
        """Get consensus state"""
        return self.data[StateType.CONSENSUS.value].copy()
    
    async def get_reputation_state(self) -> Dict[str, Any]:
        """Get reputation state"""
        return self.data[StateType.REPUTATION.value].copy()
    
    async def get_resource_state(self) -> Dict[str, Any]:
        """Get resource state"""
        return self.data[StateType.RESOURCES.value].copy()
    
    def verify_state_hash(self, expected_hash: str) -> bool:
        """Verify state hash matches expected"""
        current_state = {
            'ledger': self.data[StateType.LEDGER.value],
            'consensus': self.data[StateType.CONSENSUS.value],
            'reputation': self.data[StateType.REPUTATION.value],
            'resources': self.data[StateType.RESOURCES.value],
        }
        json_str = json.dumps(current_state, sort_keys=True)
        current_hash = hashlib.sha256(json_str.encode()).hexdigest()
        return current_hash == expected_hash
    
    def get_merkle_root(self) -> str:
        """Get merkle root of all state"""
        snapshot = StateSnapshot(
            timestamp=datetime.now().timestamp(),
            version=self.version,
            ledger=self.data[StateType.LEDGER.value],
            consensus=self.data[StateType.CONSENSUS.value],
            reputation=self.data[StateType.REPUTATION.value],
            resources=self.data[StateType.RESOURCES.value],
        )
        return snapshot.compute_hash()
