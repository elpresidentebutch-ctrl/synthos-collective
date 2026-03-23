"""
SYNTHOS Blockchain - Agents ARE the Blockchain
The distributed network of agents collectively forms the blockchain.
No separate consensus layer - consensus emerges from agent coordination.
"""

from dataclasses import dataclass, field
from typing import Dict, Optional, List, Tuple, Set, Any
from enum import Enum
from datetime import datetime, timedelta
import asyncio
import hashlib
import json
from uuid import uuid4
import heapq


@dataclass
class Transaction:
    """Transaction on the blockchain"""
    tx_id: str
    sender: str
    receiver: str
    amount: int
    timestamp: datetime
    nonce: int
    signature: str
    status: str = "pending"  # pending, confirmed, finalized


@dataclass
class Block:
    """Block on the blockchain - consensus outcome of agents"""
    block_height: int
    block_hash: str
    parent_hash: str
    proposer_id: str  # Agent that proposed this block
    timestamp: datetime
    transactions: List[Transaction] = field(default_factory=list)
    validator_signatures: Dict[str, str] = field(default_factory=dict)  # agent_id -> signature
    validator_votes: Dict[str, int] = field(default_factory=dict)  # agent_id -> vote (-1/0/1)
    state_root: str = ""
    votes_for: int = 0
    votes_against: int = 0
    votes_abstain: int = 0
    is_finalized: bool = False
    finalized_at: Optional[datetime] = None
    confirmation_count: int = 0  # How many agents confirmed


@dataclass
class ChainState:
    """Current state of blockchain from agent perspective"""
    chain_height: int = 0
    chain_tip: Optional[Block] = None
    state_root: str = "0x0"
    total_transactions: int = 0
    last_finalized_block: Optional[Block] = None
    last_finalized_height: int = 0
    pending_transactions: List[Transaction] = field(default_factory=list)
    unconfirmed_blocks: List[Block] = field(default_factory=list)
    forks: Dict[str, Block] = field(default_factory=dict)  # Alternative chains


class DistributedLedger:
    """
    Distributed Ledger = The agents ARE the blockchain
    Each agent maintains identical copy through consensus
    """

    def __init__(self):
        """Initialize distributed ledger"""
        # The actual chain
        self.full_chain: List[Block] = []
        self.blocks_by_hash: Dict[str, Block] = {}
        self.transactions_by_id: Dict[str, Transaction] = {}
        
        # Mempool - transactions waiting to be confirmed
        self.mempool: Dict[str, Transaction] = {}
        self.mempool_by_sender: Dict[str, List[Transaction]] = {}
        
        # Unconfirmed blocks from consensus
        self.unconfirmed_blocks: List[Block] = []
        self.block_votes: Dict[str, Dict[str, int]] = {}  # block_hash -> {agent_id -> vote}
        
        # State
        self.state_root = "0x0"
        self.account_states: Dict[str, Dict] = {}  # address -> {balance, nonce, ...}
        
        # Fork handling
        self.forks: Dict[str, List[Block]] = {}
        self.fork_choice = self._longest_chain_rule


    def add_transaction(self, transaction: Transaction) -> Tuple[bool, str]:
        """Add transaction to mempool"""
        # Validation
        if transaction.tx_id in self.transactions_by_id:
            return False, "Transaction already exists"
        
        # Add to mempool
        self.mempool[transaction.tx_id] = transaction
        
        if transaction.sender not in self.mempool_by_sender:
            self.mempool_by_sender[transaction.sender] = []
        self.mempool_by_sender[transaction.sender].append(transaction)
        
        self.transactions_by_id[transaction.tx_id] = transaction
        
        return True, transaction.tx_id


    def get_pending_transactions(self, limit: int = 1000) -> List[Transaction]:
        """Get transactions ready for block from mempool"""
        # Sort by sender nonce (to avoid double spend)
        pending = []
        seen_nonces: Dict[str, int] = {}
        
        for tx_id, tx in list(self.mempool.items()):
            last_nonce = seen_nonces.get(tx.sender, -1)
            
            if tx.nonce == last_nonce + 1:
                pending.append(tx)
                seen_nonces[tx.sender] = tx.nonce
                
                if len(pending) >= limit:
                    break
        
        return pending


    def propose_block(self, proposer_id: str, transactions: List[Transaction]) -> Block:
        """Propose new block - agents create blocks through consensus"""
        
        block_height = len(self.full_chain)
        parent_hash = self.full_chain[-1].block_hash if self.full_chain else "0x0"
        
        block = Block(
            block_height=block_height,
            block_hash=self._hash_block_candidate(block_height, parent_hash, transactions),
            parent_hash=parent_hash,
            proposer_id=proposer_id,
            timestamp=datetime.now(),
            transactions=transactions,
            state_root=self.state_root
        )
        
        self.unconfirmed_blocks.append(block)
        self.block_votes[block.block_hash] = {}
        
        return block


    def vote_on_block(self, block_hash: str, agent_id: str, vote: int) -> bool:
        """Agent votes on block (-1/abstain, 0/abstain, 1/for)"""
        
        if block_hash not in self.block_votes:
            return False
        
        if agent_id in self.block_votes[block_hash]:
            return False  # Already voted
        
        self.block_votes[block_hash][agent_id] = vote
        
        return True


    def check_block_finality(self, block_hash: str, total_agents: int) -> Tuple[bool, int]:
        """Check if block reached consensus (2/3+ validators agree)"""
        
        if block_hash not in self.block_votes:
            return False, 0
        
        votes = self.block_votes[block_hash]
        votes_for = sum(1 for v in votes.values() if v == 1)
        
        required = max(1, total_agents * 2 // 3)
        
        if votes_for >= required:
            return True, votes_for
        
        return False, votes_for


    def finalize_block(self, block_hash: str) -> Tuple[bool, str]:
        """Finalize block - add to chain"""
        
        # Find block
        block = None
        for ub in self.unconfirmed_blocks:
            if ub.block_hash == block_hash:
                block = ub
                break
        
        if not block:
            return False, "Block not found"
        
        # Verify chain continuity
        if len(self.full_chain) > 0:
            if block.parent_hash != self.full_chain[-1].block_hash:
                return False, "Parent hash mismatch - would create fork"
        
        # Apply transactions to state
        for tx in block.transactions:
            self._apply_transaction(tx)
            
            # Remove from mempool
            if tx.tx_id in self.mempool:
                del self.mempool[tx.tx_id]
            
            # Mark as confirmed
            tx.status = "confirmed"
        
        # Add to chain
        block.is_finalized = True
        block.finalized_at = datetime.now()
        
        self.full_chain.append(block)
        self.blocks_by_hash[block_hash] = block
        
        # Update state
        self.state_root = self._calculate_state_root()
        
        # Remove from unconfirmed
        self.unconfirmed_blocks.remove(block)
        
        return True, block_hash


    def get_chain_state(self) -> ChainState:
        """Get current chain state"""
        return ChainState(
            chain_height=len(self.full_chain),
            chain_tip=self.full_chain[-1] if self.full_chain else None,
            state_root=self.state_root,
            total_transactions=len([t for b in self.full_chain for t in b.transactions]),
            last_finalized_block=self.full_chain[-1] if self.full_chain else None,
            last_finalized_height=len(self.full_chain) - 1,
            pending_transactions=list(self.mempool.values()),
            unconfirmed_blocks=self.unconfirmed_blocks.copy(),
        )


    def verify_chain_integrity(self) -> bool:
        """Verify entire chain is valid"""
        
        if not self.full_chain:
            return True
        
        # Check first block
        first_block = self.full_chain[0]
        if first_block.parent_hash != "0x0":
            return False
        
        # Check chain continuity
        for i in range(1, len(self.full_chain)):
            block = self.full_chain[i]
            parent = self.full_chain[i - 1]
            
            if block.parent_hash != parent.block_hash:
                return False
            
            if block.block_height != i:
                return False
        
        return True


    def _apply_transaction(self, tx: Transaction):
        """Apply transaction to state"""
        # Deduct from sender
        if tx.sender not in self.account_states:
            self.account_states[tx.sender] = {"balance": 0, "nonce": 0}
        
        self.account_states[tx.sender]["balance"] -= tx.amount
        self.account_states[tx.sender]["nonce"] += 1
        
        # Add to receiver
        if tx.receiver not in self.account_states:
            self.account_states[tx.receiver] = {"balance": 0, "nonce": 0}
        
        self.account_states[tx.receiver]["balance"] += tx.amount


    def _calculate_state_root(self) -> str:
        """Calculate merkle root of current state"""
        state_str = json.dumps(self.account_states, sort_keys=True, default=str)
        return "0x" + hashlib.sha256(state_str.encode()).hexdigest()[:16]


    def _hash_block_candidate(self, height: int, parent_hash: str, 
                              transactions: List[Transaction]) -> str:
        """Hash block candidate"""
        block_data = {
            "height": height,
            "parent_hash": parent_hash,
            "tx_count": len(transactions),
            "timestamp": datetime.now().isoformat(),
        }
        data_str = json.dumps(block_data, sort_keys=True)
        return "0x" + hashlib.sha256(data_str.encode()).hexdigest()[:16]


    def _longest_chain_rule(self, fork1: List[Block], fork2: List[Block]) -> List[Block]:
        """Fork choice: take longest chain"""
        return fork1 if len(fork1) >= len(fork2) else fork2


class SynthosBlockchain:
    """
    SYNTHOS Blockchain - The agents ARE the chain
    Formed by distributed consensus of agent network
    """

    def __init__(self, agents: List['SynthosAgentInstance'] = None):
        """Initialize blockchain with agent network"""
        self.agents = agents or []
        self.ledger = DistributedLedger()
        
        # Synchronization state
        self.agent_ledger_states: Dict[str, ChainState] = {}
        self.network_consensus_height: int = 0
        
        # Consensus rounds
        self.consensus_round: int = 0
        self.round_leader: Optional[str] = None  # Block proposer
        self.round_deadline: datetime = datetime.now()
        
        # Statistics
        self.blocks_created: int = 0
        self.transactions_confirmed: int = 0
        self.finality_time_ms: List[int] = []


    async def run_consensus_round(self) -> Tuple[bool, Optional[str]]:
        """
        Execute single consensus round
        All agents participate autonomously
        Returns: (success, block_hash or error)
        """
        
        self.consensus_round += 1
        round_id = self.consensus_round
        
        # Step 1: Collect transactions from network
        all_transactions = self._collect_transactions()
        
        # Step 2: Elect block proposer (round-robin or random)
        proposer = self._elect_block_proposer()
        self.round_leader = proposer.identity.agent_id
        
        # Step 3: Proposer creates block from pending transactions
        pending = self.ledger.get_pending_transactions(limit=1000)
        block = self.ledger.propose_block(proposer.identity.agent_id, pending)
        
        print(f"[Consensus Round {round_id}] Block proposed by {self.round_leader}")
        print(f"  Block height: {block.block_height}")
        print(f"  Transactions: {len(block.transactions)}")
        
        # Step 4: All agents validate and vote on block
        votes_for = 0
        votes_against = 0
        
        for agent in self.agents:
            # Each agent independently validates
            validation = await agent.validate_block(block.__dict__)
            
            # Each agent independently votes
            vote_value = 1 if validation.is_valid else -1
            
            self.ledger.vote_on_block(block.block_hash, agent.identity.agent_id, vote_value)
            
            if vote_value == 1:
                votes_for += 1
            else:
                votes_against += 1
        
        print(f"  Votes: {votes_for} for, {votes_against} against")
        
        # Step 5: Check finality - 2/3+ agents must agree
        is_finalized, final_votes = self.ledger.check_block_finality(
            block.block_hash, 
            len(self.agents)
        )
        
        if not is_finalized:
            print(f"  Finality FAILED - Need {len(self.agents) * 2 // 3}, got {final_votes}")
            return False, None
        
        # Step 6: Finalize block - add to chain
        success, result = self.ledger.finalize_block(block.block_hash)
        
        if success:
            self.blocks_created += 1
            self.transactions_confirmed += len(block.transactions)
            print(f"  ✓ Block FINALIZED and added to chain")
            return True, block.block_hash
        else:
            print(f"  ✗ Finalization failed: {result}")
            return False, None


    async def synchronize_agents(self) -> bool:
        """Synchronize agent ledgers - all agents converge on same chain"""
        
        # Each agent gets current ledger state
        for agent in self.agents:
            state = self.ledger.get_chain_state()
            self.agent_ledger_states[agent.identity.agent_id] = state
            
            # Update agent's local chain
            agent.registry_state["chain_height"] = state.chain_height
            agent.state_root = state.state_root
            agent.local_blocks = [b.__dict__ for b in self.ledger.full_chain]
        
        return True


    async def run_blockchain(self, rounds: int = 10) -> Dict:
        """Run blockchain for N consensus rounds"""
        
        print(f"\n{'='*70}")
        print(f"SYNTHOS BLOCKCHAIN - Agents ARE the Blockchain")
        print(f"Running {rounds} consensus rounds with {len(self.agents)} agents")
        print(f"{'='*70}\n")
        
        for round_num in range(rounds):
            # Add test transaction
            tx = Transaction(
                tx_id=f"tx_{round_num}_{int(datetime.now().timestamp() * 1000)}",
                sender="0xAlice",
                receiver="0xBob",
                amount=10,
                timestamp=datetime.now(),
                nonce=round_num,
                signature="0xsig"
            )
            self.ledger.add_transaction(tx)
            
            # Run consensus
            start = datetime.now()
            success, block_hash = await self.run_consensus_round()
            elapsed = int((datetime.now() - start).total_seconds() * 1000)
            
            if success:
                self.finality_time_ms.append(elapsed)
                print(f"  Finality time: {elapsed}ms\n")
            
            # Synchronize agents
            await self.synchronize_agents()
            
            # Small delay between rounds
            await asyncio.sleep(0.1)
        
        return self.get_blockchain_stats()


    def get_blockchain_stats(self) -> Dict:
        """Get blockchain statistics"""
        
        chain_state = self.ledger.get_chain_state()
        
        stats = {
            "consensus_rounds": self.consensus_round,
            "blocks_created": self.blocks_created,
            "chain_height": chain_state.chain_height,
            "total_transactions_confirmed": self.transactions_confirmed,
            "avg_finality_time_ms": (
                sum(self.finality_time_ms) / len(self.finality_time_ms)
                if self.finality_time_ms else 0
            ),
            "agents_in_network": len(self.agents),
            "ledger_state": {
                "chain_height": chain_state.chain_height,
                "pending_transactions": len(chain_state.pending_transactions),
                "unconfirmed_blocks": len(chain_state.unconfirmed_blocks),
                "state_root": chain_state.state_root,
            },
            "agents_synchronized": all(
                self.agent_ledger_states.get(a.identity.agent_id, ChainState()).chain_height
                == chain_state.chain_height
                for a in self.agents
            ),
            "is_chain_valid": self.ledger.verify_chain_integrity(),
        }
        
        return stats


    def _collect_transactions(self) -> List[Transaction]:
        """Collect transactions from all agents"""
        # In real implementation, would propagate from agent network
        return list(self.ledger.mempool.values())


    def _elect_block_proposer(self) -> 'SynthosAgentInstance':
        """Elect block proposer (round-robin)"""
        proposer_index = self.consensus_round % len(self.agents)
        return self.agents[proposer_index]


    def print_chain(self):
        """Print blockchain for inspection"""
        print(f"\n{'='*70}")
        print(f"SYNTHOS BLOCKCHAIN - Chain State")
        print(f"{'='*70}\n")
        
        for block in self.ledger.full_chain:
            print(f"Block #{block.block_height}")
            print(f"  Hash: {block.block_hash}")
            print(f"  Parent: {block.parent_hash}")
            print(f"  Proposer: {block.proposer_id}")
            print(f"  Transactions: {len(block.transactions)}")
            print(f"  Finalized: {block.is_finalized}")
            print()
        
        print(f"Chain height: {len(self.ledger.full_chain)}")
        print(f"State root: {self.ledger.state_root}")
        print(f"Chain valid: {self.ledger.verify_chain_integrity()}\n")
