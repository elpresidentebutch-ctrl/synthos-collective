"""Data models for SYNTHOS Agents"""

from dataclasses import dataclass, field
from typing import List, Dict, Any
from datetime import datetime


@dataclass
class Transaction:
    """Transaction model"""
    sender: str
    recipient: str
    amount: int
    fee: int = 1
    nonce: int = 0
    signature: bytes = field(default_factory=bytes)
    timestamp: float = field(default_factory=lambda: datetime.now().timestamp())
    data: bytes = field(default_factory=bytes)
    id: str = field(default_factory=lambda: str(datetime.now().timestamp()))


@dataclass
class Block:
    """Block model"""
    height: int
    proposer: str
    transactions: List[Transaction] = field(default_factory=list)
    previous_hash: str = ""
    timestamp: float = field(default_factory=lambda: datetime.now().timestamp())
    hash: str = ""
    signature: bytes = field(default_factory=bytes)
    state_root: str = ""
    
    def compute_hash(self) -> str:
        """Compute block hash"""
        import hashlib
        import json
        data = {
            'height': self.height,
            'proposer': self.proposer,
            'timestamp': self.timestamp,
            'previous_hash': self.previous_hash,
            'state_root': self.state_root,
        }
        json_str = json.dumps(data, sort_keys=True)
        return hashlib.sha256(json_str.encode()).hexdigest()


@dataclass
class Proposal:
    """Governance proposal model"""
    id: str
    proposer: str
    change_type: str
    parameters: Dict[str, Any]
    description: str = ""
    timestamp: float = field(default_factory=lambda: datetime.now().timestamp())
    vote_deadline: int = 0
    votes_for: int = 0
    votes_against: int = 0


@dataclass
class Vote:
    """Vote model"""
    proposal_id: str
    voter: str
    vote_value: bool  # True for yes, False for no
    stake: int = 0
    timestamp: float = field(default_factory=lambda: datetime.now().timestamp())


@dataclass
class Validator:
    """Validator model"""
    id: str
    address: str
    stake: int
    commission: float = 0.05
    uptime: float = 1.0
    slashed: int = 0
    reputation_score: float = 1.0


@dataclass
class Metrics:
    """Network metrics model"""
    throughput: float  # transactions per second
    latency: float  # milliseconds
    block_time: float  # seconds
    finality_time: float  # seconds
    network_health: float  # 0-1 score
