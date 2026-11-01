"""Block Proposal System"""

from typing import List, Dict, Any, Optional
from dataclasses import dataclass, field
from datetime import datetime
import hashlib


@dataclass
class BlockProposal:
    """A block proposal ready for voting"""
    proposer: str
    height: int
    transactions: List[Any] = field(default_factory=list)
    proofs: List[Any] = field(default_factory=list)
    previous_hash: str = ""
    timestamp: float = field(default_factory=lambda: datetime.now().timestamp())
    
    def compute_hash(self) -> str:
        """Compute block hash"""
        data = {
            'height': self.height,
            'proposer': self.proposer,
            'timestamp': self.timestamp,
            'tx_count': len(self.transactions),
            'previous_hash': self.previous_hash,
        }
        import json
        json_str = json.dumps(data, sort_keys=True)
        return hashlib.sha256(json_str.encode()).hexdigest()


class BlockProposer:
    """Assembles and optimizes block proposals"""
    
    def __init__(self, agent):
        self.agent = agent
        self.max_block_size = 4 * 1024 * 1024  # 4MB
        self.max_transactions_per_block = 10000
    
    async def propose_block(self, pending_txs: List[Any]) -> Optional[BlockProposal]:
        """
        Assemble block proposal from pending transactions
        
        Process:
        1. Filter valid transactions
        2. Order transactions optimally
        3. Collect proofs
        4. Assemble into block
        5. Publish proposal
        """
        # Filter to valid transactions only
        valid_txs = []
        for tx in pending_txs:
            is_valid = await self._is_valid_transaction(tx)
            if is_valid:
                valid_txs.append(tx)
        
        # Optimize transaction ordering
        ordered_txs = await self._optimize_transaction_order(valid_txs)
        
        # Collect proofs
        proofs = await self._collect_proofs(ordered_txs)
        
        # Create proposal
        proposal = BlockProposal(
            proposer=self.agent.id,
            height=await self._get_block_height(),
            transactions=ordered_txs[:self.max_transactions_per_block],
            proofs=proofs,
            previous_hash=await self._get_previous_hash(),
        )
        
        # Publish proposal
        await self._publish_proposal(proposal)
        
        return proposal
    
    async def _is_valid_transaction(self, transaction: Any) -> bool:
        """Check if transaction is valid"""
        # Would use TransactionValidator
        return True
    
    async def _optimize_transaction_order(self, 
                                         transactions: List[Any]) -> List[Any]:
        """
        Optimize transaction ordering for:
        
        - Maximum fee extraction
        - Transaction dependencies
        - Cache efficiency
        - MEV minimization
        - Parallelizability
        """
        # Sort by fee per byte (greedy approach)
        def fee_per_byte(tx):
            fee = getattr(tx, 'fee', 0)
            size = len(str(tx))
            return fee / (size + 1)  # Avoid division by zero
        
        sorted_txs = sorted(transactions, key=fee_per_byte, reverse=True)
        
        # Resolve dependencies
        ordered = await self._resolve_dependencies(sorted_txs)
        
        return ordered
    
    async def _resolve_dependencies(self, 
                                   transactions: List[Any]) -> List[Any]:
        """
        Resolve transaction dependencies
        
        Ensures:
        - Account nonces are sequential
        - Spending dependencies are satisfied
        - No circular dependencies
        """
        # Build dependency graph
        deps = {}
        for tx in transactions:
            sender = getattr(tx, 'sender', None)
            recipient = getattr(tx, 'recipient', None)
            tx_id = getattr(tx, 'id', None)
            
            # Find transactions from same sender (nonce dependency)
            sender_txs = [t for t in transactions 
                         if getattr(t, 'sender', None) == sender]
            
            if sender not in deps:
                deps[tx_id] = []
            
            # Previous nonce is a dependency
            for other in sender_txs:
                if getattr(other, 'nonce', 0) < getattr(tx, 'nonce', 0):
                    deps[tx_id].append(other.id)
        
        # Topological sort
        ordered = []
        remaining = set(t.id for t in transactions)
        
        while remaining:
            ready = [
                tx for tx in transactions
                if tx.id in remaining and 
                all(d not in remaining for d in deps.get(tx.id, []))
            ]
            
            if not ready:
                break
            
            # Pick highest fee of ready transactions
            next_tx = max(ready, key=lambda t: getattr(t, 'fee', 0))
            ordered.append(next_tx)
            remaining.remove(next_tx.id)
        
        return ordered
    
    async def _collect_proofs(self, transactions: List[Any]) -> List[Any]:
        """
        Collect proofs for transactions
        
        Proofs include:
        - State proofs (merkle proofs of account state)
        - Cross-chain proofs
        - Availability proofs
        - Execution proofs
        """
        proofs = []
        
        for tx in transactions:
            # Collect cross-chain proof if present
            if hasattr(tx, 'cross_chain_proof'):
                proofs.append({
                    'type': 'cross_chain',
                    'tx_id': tx.id,
                    'proof': tx.cross_chain_proof
                })
            
            # Collect state proof (merkle proof of sender's state)
            state_proof = await self._create_state_proof(tx)
            proofs.append({
                'type': 'state',
                'tx_id': tx.id,
                'proof': state_proof
            })
        
        return proofs
    
    async def _create_state_proof(self, transaction: Any) -> Dict:
        """Create merkle proof of account state"""
        sender = getattr(transaction, 'sender', None)
        balance = await self.agent.state.get_balance(sender)
        
        return {
            'account': sender,
            'balance': balance,
            'merkle_root': await self.agent.state.get_merkle_root(),
        }
    
    async def _publish_proposal(self, proposal: BlockProposal) -> None:
        """Publish block proposal to network"""
        from src.core.event import Event, EventType
        
        proposal_hash = proposal.compute_hash()
        
        await self.agent.event_bus.publish(Event(
            type=EventType.BLOCK_PROPOSED,
            source='BlockProposer',
            data={
                'proposal': proposal,
                'hash': proposal_hash,
                'proposer': proposal.proposer,
            },
            priority=1
        ))
    
    async def _get_block_height(self) -> int:
        """Get current block height"""
        ledger = await self.agent.state.get_ledger_state()
        return ledger.get('block_height', 0) + 1
    
    async def _get_previous_hash(self) -> str:
        """Get previous block hash"""
        ledger = await self.agent.state.get_ledger_state()
        return ledger.get('last_block_hash', '')


class BlockValidator:
    """Validates block proposals"""
    
    def __init__(self, agent):
        self.agent = agent
    
    async def validate_proposal(self, proposal: BlockProposal) -> bool:
        """
        Validate block proposal
        
        Checks:
        - All transactions are valid
        - No duplicate transactions
        - Block size is within limits
        - Proofs are valid
        - Proposer has right to propose
        - Block height is correct
        """
        # Validate all transactions
        for tx in proposal.transactions:
            if not await self._validate_transaction(tx):
                return False
        
        # Check for duplicates
        tx_ids = [getattr(tx, 'id', None) for tx in proposal.transactions]
        if len(tx_ids) != len(set(tx_ids)):
            return False
        
        # Check block size
        total_size = sum(len(str(tx)) for tx in proposal.transactions)
        if total_size > 4 * 1024 * 1024:  # 4MB
            return False
        
        # Validate proofs
        for proof in proposal.proofs:
            if not await self._validate_proof(proof):
                return False
        
        # Check proposer authorization
        if not await self._is_proposer_authorized(proposal.proposer):
            return False
        
        return True
    
    async def _validate_transaction(self, tx: Any) -> bool:
        """Validate single transaction"""
        return True  # Would use TransactionValidator
    
    async def _validate_proof(self, proof: Dict) -> bool:
        """Validate proof"""
        proof_type = proof.get('type')
        
        if proof_type == 'cross_chain':
            return await self._validate_cross_chain_proof(proof)
        elif proof_type == 'state':
            return await self._validate_state_proof(proof)
        
        return False
    
    async def _validate_cross_chain_proof(self, proof: Dict) -> bool:
        """Validate cross-chain proof"""
        # Would verify against source chain
        return True
    
    async def _validate_state_proof(self, proof: Dict) -> bool:
        """Validate state proof"""
        # Would verify merkle path
        return True
    
    async def _is_proposer_authorized(self, proposer: str) -> bool:
        """Check if proposer is authorized"""
        # Would check validator set or rotation
        return True
