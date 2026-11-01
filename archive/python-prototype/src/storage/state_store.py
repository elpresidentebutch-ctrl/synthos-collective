"""Local State Storage System"""

from typing import Dict, Any, Optional, List
from dataclasses import dataclass, field
from datetime import datetime


@dataclass
class PeerReputation:
    """Reputation data for a peer"""
    peer_id: str
    reliability_score: float = 1.0  # 0-1
    message_count: int = 0
    failed_messages: int = 0
    last_seen: float = field(default_factory=lambda: datetime.now().timestamp())
    uptime_percentage: float = 100.0
    average_message_latency: float = 0.0


class LocalStateStore:
    """Manages local state storage for agent"""
    
    def __init__(self, agent):
        self.agent = agent
        
        # Ledger state
        self.ledger_state = {
            'block_height': 0,
            'last_block_hash': '',
            'accounts': {},
            'total_supply': 0,
            'burned_tokens': 0,
        }
        
        # Peer reputations
        self.peer_reputations: Dict[str, PeerReputation] = {}
        
        # Proposal history
        self.proposal_history: Dict[str, Any] = {}
        
        # Governance decisions
        self.governance_decisions: Dict[str, Any] = {}
        
        # Consensus history
        self.consensus_rounds: Dict[int, Any] = {}
        
        # Mempool (pending transactions)
        self.mempool: List[Any] = []
        
        # Block cache
        self.block_cache: Dict[str, Any] = {}
    
    async def store_block(self, block: Any) -> None:
        """Store block in local cache"""
        block_hash = getattr(block, 'hash', '')
        block_height = getattr(block, 'height', 0)
        
        self.block_cache[block_hash] = block
        
        # Update ledger
        self.ledger_state['last_block_hash'] = block_hash
        self.ledger_state['block_height'] = block_height
    
    async def get_block(self, block_hash: str) -> Optional[Any]:
        """Retrieve block from cache"""
        return self.block_cache.get(block_hash)
    
    async def store_proposal(self, proposal: Any) -> None:
        """Store governance proposal"""
        proposal_id = getattr(proposal, 'id', None)
        
        if proposal_id:
            self.proposal_history[proposal_id] = {
                'proposal': proposal,
                'timestamp': datetime.now().timestamp(),
                'votes_for': 0,
                'votes_against': 0,
            }
    
    async def get_proposal(self, proposal_id: str) -> Optional[Any]:
        """Retrieve proposal"""
        proposal_data = self.proposal_history.get(proposal_id)
        if proposal_data:
            return proposal_data['proposal']
        return None
    
    async def record_governance_decision(self,
                                        proposal_id: str,
                                        decision: str,
                                        votes_for: int,
                                        votes_against: int) -> None:
        """Record governance decision"""
        self.governance_decisions[proposal_id] = {
            'decision': decision,  # PASSED or REJECTED
            'votes_for': votes_for,
            'votes_against': votes_against,
            'timestamp': datetime.now().timestamp(),
        }
    
    async def update_peer_reputation(self,
                                    peer_id: str,
                                    message_success: bool,
                                    latency: float = 0.0) -> None:
        """Update peer reputation"""
        if peer_id not in self.peer_reputations:
            self.peer_reputations[peer_id] = PeerReputation(peer_id=peer_id)
        
        rep = self.peer_reputations[peer_id]
        rep.message_count += 1
        rep.last_seen = datetime.now().timestamp()
        
        if not message_success:
            rep.failed_messages += 1
        
        if latency > 0:
            # Update average latency
            old_avg = rep.average_message_latency
            rep.average_message_latency = (
                (old_avg * (rep.message_count - 1) + latency) /
                rep.message_count
            )
        
        # Calculate reliability score
        if rep.message_count > 0:
            rep.reliability_score = 1.0 - (rep.failed_messages / rep.message_count)
        
        # Calculate uptime
        success_count = rep.message_count - rep.failed_messages
        if rep.message_count > 0:
            rep.uptime_percentage = (success_count / rep.message_count) * 100
    
    async def get_peer_reputation(self, peer_id: str) -> Optional[PeerReputation]:
        """Get peer reputation"""
        return self.peer_reputations.get(peer_id)
    
    async def get_trusted_peers(self, 
                               trust_threshold: float = 0.8) -> List[str]:
        """Get peers above trust threshold"""
        trusted = []
        for peer_id, rep in self.peer_reputations.items():
            if rep.reliability_score >= trust_threshold:
                trusted.append(peer_id)
        return trusted
    
    async def add_to_mempool(self, transaction: Any) -> None:
        """Add transaction to mempool"""
        tx_id = getattr(transaction, 'id', None)
        
        # Check if already in mempool
        if any(getattr(t, 'id', None) == tx_id for t in self.mempool):
            return
        
        self.mempool.append(transaction)
    
    async def remove_from_mempool(self, tx_id: str) -> None:
        """Remove transaction from mempool (after inclusion)"""
        self.mempool = [
            t for t in self.mempool
            if getattr(t, 'id', None) != tx_id
        ]
    
    async def get_mempool_transactions(self, limit: int = 100) -> List[Any]:
        """Get pending transactions from mempool"""
        return self.mempool[:limit]
    
    async def record_consensus_round(self,
                                    height: int,
                                    block_hash: str,
                                    finalized: bool) -> None:
        """Record consensus round"""
        self.consensus_rounds[height] = {
            'block_hash': block_hash,
            'finalized': finalized,
            'timestamp': datetime.now().timestamp(),
        }
    
    def get_state_summary(self) -> Dict[str, Any]:
        """Get summary of local state"""
        return {
            'ledger': self.ledger_state,
            'mempool_size': len(self.mempool),
            'peer_count': len(self.peer_reputations),
            'proposal_count': len(self.proposal_history),
            'governance_decisions_count': len(self.governance_decisions),
            'block_cache_size': len(self.block_cache),
        }
