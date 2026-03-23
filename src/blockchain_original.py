"""
SYNTHOS Blockchain - Original Implementation
Sovereign, deterministic consensus system with full blockchain capabilities
"""

import hashlib
import hmac
import json
from typing import Dict, List, Optional, Set, Tuple
from dataclasses import dataclass, field
from datetime import datetime
from enum import Enum
import struct


class TransactionType(Enum):
    """Original transaction types"""
    TRANSFER = "transfer"
    STAKE = "stake"
    UNSTAKE = "unstake"
    DELEGATE = "delegate"
    CONTRACT_CALL = "contract_call"
    CONTRACT_DEPLOY = "contract_deploy"
    DATA_STORE = "data_store"
    CLAIM_REWARD = "claim_reward"


class BlockStatus(Enum):
    """Block status in consensus"""
    PROPOSED = "proposed"
    COMMITTED = "committed"
    FINAL = "final"
    ORPHANED = "orphaned"


@dataclass
class OriginTransaction:
    """Original transaction model with full features"""
    tx_id: str
    tx_type: TransactionType
    sender: str
    receiver: str
    amount: int
    fee: int
    nonce: int
    timestamp: float
    signature: str = ""
    data_payload: str = ""  # Contract data
    gas_limit: int = 21000  # Standard gas
    gas_price: int = 1
    
    def compute_hash(self) -> str:
        """Compute transaction hash"""
        data = {
            'type': self.tx_type.value,
            'sender': self.sender,
            'receiver': self.receiver,
            'amount': self.amount,
            'nonce': self.nonce,
            'timestamp': self.timestamp,
        }
        json_str = json.dumps(data, sort_keys=True)
        return hashlib.sha256(json_str.encode()).hexdigest()


@dataclass
class OriginBlock:
    """Original block model with full consensus data"""
    block_id: str
    height: int
    proposer: str
    timestamp: float
    transactions: List[OriginTransaction] = field(default_factory=list)
    previous_hash: str = ""
    merkle_root: str = ""
    state_root: str = ""
    consensus_hash: str = ""
    status: BlockStatus = BlockStatus.PROPOSED
    round_number: int = 0
    quorum_votes: Dict[str, bool] = field(default_factory=dict)
    
    def compute_hash(self) -> str:
        """Compute block hash"""
        data = {
            'height': self.height,
            'proposer': self.proposer,
            'timestamp': self.timestamp,
            'previous': self.previous_hash,
            'merkle': self.merkle_root,
            'state': self.state_root,
            'round': self.round_number,
        }
        json_str = json.dumps(data, sort_keys=True)
        return hashlib.sha256(json_str.encode()).hexdigest()


class MerkleProof:
    """Original Merkle proof system"""
    
    @staticmethod
    def build_tree(items: List[str]) -> Tuple[List[List[str]], str]:
        """
        Build Merkle tree from items
        Returns: (tree_levels, root_hash)
        """
        if not items:
            return [], hashlib.sha256(b"empty").hexdigest()
        
        tree = []
        current_level = [hashlib.sha256(item.encode()).hexdigest() for item in items]
        tree.append(current_level.copy())
        
        while len(current_level) > 1:
            next_level = []
            for i in range(0, len(current_level), 2):
                left = current_level[i]
                right = current_level[i + 1] if i + 1 < len(current_level) else left
                combined = left + right
                hash_val = hashlib.sha256(combined.encode()).hexdigest()
                next_level.append(hash_val)
            
            tree.append(next_level)
            current_level = next_level
        
        return tree, tree[-1][0] if tree[-1] else hashlib.sha256(b"empty").hexdigest()
    
    @staticmethod
    def generate_proof(tree: List[List[str]], index: int) -> List[str]:
        """Generate Merkle proof for item at index"""
        proof = []
        current_index = index
        
        for level in tree[:-1]:
            sibling_index = current_index ^ 1
            if sibling_index < len(level):
                proof.append(level[sibling_index])
            current_index //= 2
        
        return proof
    
    @staticmethod
    def verify_proof(item: str, proof: List[str], root: str) -> bool:
        """Verify Merkle proof"""
        current_hash = hashlib.sha256(item.encode()).hexdigest()
        
        for sibling in proof:
            combined = current_hash + sibling
            current_hash = hashlib.sha256(combined.encode()).hexdigest()
        
        return current_hash == root


class StateTransition:
    """Track state changes per block"""
    
    def __init__(self):
        self.balances: Dict[str, int] = {}
        self.stakes: Dict[str, int] = {}
        self.delegations: Dict[str, str] = {}
        self.contracts: Dict[str, Dict] = {}
        self.nonces: Dict[str, int] = {}
        self.transitions: List[Dict] = []
    
    def apply_transaction(self, tx: OriginTransaction) -> Tuple[bool, str]:
        """Apply transaction to state"""
        # Check nonce
        current_nonce = self.nonces.get(tx.sender, 0)
        if tx.nonce != current_nonce:
            return False, f"Invalid nonce: expected {current_nonce}, got {tx.nonce}"
        
        # Check balance
        balance = self.balances.get(tx.sender, 0)
        total_cost = tx.amount + tx.fee
        
        if balance < total_cost:
            return False, "Insufficient balance"
        
        # Type-specific logic
        if tx.tx_type == TransactionType.TRANSFER:
            self.balances[tx.sender] = balance - total_cost
            self.balances[tx.receiver] = self.balances.get(tx.receiver, 0) + tx.amount
        
        elif tx.tx_type == TransactionType.STAKE:
            self.balances[tx.sender] = balance - total_cost
            self.stakes[tx.sender] = self.stakes.get(tx.sender, 0) + tx.amount
        
        elif tx.tx_type == TransactionType.UNSTAKE:
            stake = self.stakes.get(tx.sender, 0)
            if stake < tx.amount:
                return False, "Insufficient stake"
            self.stakes[tx.sender] = stake - tx.amount
            self.balances[tx.sender] = balance - tx.fee + tx.amount
        
        elif tx.tx_type == TransactionType.DELEGATE:
            self.delegations[tx.sender] = tx.receiver
            self.balances[tx.sender] = balance - tx.fee
        
        elif tx.tx_type == TransactionType.CLAIM_REWARD:
            self.balances[tx.sender] = balance + tx.amount - tx.fee
        
        # Update nonce
        self.nonces[tx.sender] = current_nonce + 1
        
        # Record transition
        self.transitions.append({
            'tx_id': tx.tx_id,
            'type': tx.tx_type.value,
            'sender': tx.sender,
            'timestamp': tx.timestamp,
        })
        
        return True, "Transaction applied"
    
    def compute_root(self) -> str:
        """Compute state root hash"""
        state = {
            'balances': self.balances,
            'stakes': self.stakes,
            'delegations': self.delegations,
        }
        json_str = json.dumps(state, sort_keys=True)
        return hashlib.sha256(json_str.encode()).hexdigest()


class ConsensusMechanism:
    """Original BFT-like consensus"""
    
    def __init__(self, validator_set: Set[str], fault_tolerance: int = 3):
        """
        Initialize consensus
        
        Args:
            validator_set: Set of validator addresses
            fault_tolerance: f in 3f+1 BFT
        """
        self.validators = validator_set
        self.f = fault_tolerance
        self.require_quorum = 2 * self.f + 1
        self.current_round = 0
        self.votes: Dict[int, Dict[str, Dict[str, bool]]] = {}
        self.commits: Dict[str, int] = {}
    
    def propose_block(self, block: OriginBlock) -> bool:
        """Propose new block"""
        if len(self.validators) < self.require_quorum:
            return False
        
        block.round_number = self.current_round
        self.votes[self.current_round] = {}
        
        return True
    
    def cast_vote(self, round_num: int, block_hash: str, voter: str, vote: bool) -> bool:
        """Cast vote on block"""
        if voter not in self.validators:
            return False
        
        if round_num not in self.votes:
            self.votes[round_num] = {}
        
        if block_hash not in self.votes[round_num]:
            self.votes[round_num][block_hash] = {}
        
        self.votes[round_num][block_hash][voter] = vote
        
        # Check if quorum reached
        if len(self.votes[round_num][block_hash]) >= self.require_quorum:
            commit_votes = sum(1 for v in self.votes[round_num][block_hash].values() if v)
            if commit_votes >= self.require_quorum:
                self.commits[block_hash] = round_num
                self.current_round += 1
                return True
        
        return False
    
    def is_committed(self, block_hash: str) -> bool:
        """Check if block is committed"""
        return block_hash in self.commits
    
    def get_finality_depth(self) -> int:
        """Get number of blocks required for finality"""
        return self.f + 1


class GasSystem:
    """Original gas metering system"""
    
    # Gas costs
    OPCODES = {
        'transfer': 21000,
        'stake': 50000,
        'unstake': 50000,
        'delegate': 30000,
        'storage_write': 20000,
        'storage_read': 200,
    }
    
    @staticmethod
    def calculate_gas(tx: OriginTransaction) -> int:
        """Calculate gas used by transaction"""
        base_gas = GasSystem.OPCODES.get(tx.tx_type.value, 21000)
        
        # Add gas for data
        data_gas = len(tx.data_payload) * 4
        
        return base_gas + data_gas
    
    @staticmethod
    def validate_gas(tx: OriginTransaction) -> Tuple[bool, str]:
        """Validate gas parameters"""
        gas_used = GasSystem.calculate_gas(tx)
        
        if gas_used > tx.gas_limit:
            return False, f"Gas used ({gas_used}) exceeds limit ({tx.gas_limit})"
        
        return True, "Gas valid"


class OriginalBlockchain:
    """Complete original blockchain implementation"""
    
    def __init__(self, validators: Set[str] = None, agent=None):
        """
        Initialize blockchain
        
        Args:
            validators: Set of validator addresses
            agent: Parent agent (optional)
        """
        self.agent = agent
        self.chain: List[OriginBlock] = []
        self.transaction_pool: Dict[str, OriginTransaction] = {}
        self.state = StateTransition()
        self.consensus = ConsensusMechanism(validators or {"validator1", "validator2", "validator3"})
        
        # Indexing
        self.block_index: Dict[str, OriginBlock] = {}
        self.tx_index: Dict[str, OriginBlock] = {}
        
        # Fork handling
        self.forks: Dict[str, 'OriginalBlockchain'] = {}
        self.main_chain_tip: Optional[str] = None
        
        # Metrics
        self.metrics = {
            'total_blocks': 0,
            'total_transactions': 0,
            'total_gas_used': 0,
            'average_block_time': 0.0,
            'block_times': [],
        }
    
    def create_genesis(self, initial_balances: Dict[str, int]) -> OriginBlock:
        """Create genesis block"""
        self.state.balances = initial_balances.copy()
        
        genesis = OriginBlock(
            block_id="genesis",
            height=0,
            proposer="system",
            timestamp=datetime.now().timestamp(),
            transactions=[],
            previous_hash="0x0",
            status=BlockStatus.FINAL,
        )
        
        genesis.merkle_root = "0x0"
        genesis.state_root = self.state.compute_root()
        genesis.consensus_hash = genesis.compute_hash()
        
        self.chain.append(genesis)
        self.block_index[genesis.consensus_hash] = genesis
        self.main_chain_tip = genesis.consensus_hash
        self.metrics['total_blocks'] = 1
        
        return genesis
    
    def submit_transaction(self, tx: OriginTransaction) -> Tuple[bool, str]:
        """Submit transaction to mempool"""
        # Validate gas
        gas_valid, gas_msg = GasSystem.validate_gas(tx)
        if not gas_valid:
            return False, gas_msg
        
        # Check balance
        balance = self.state.balances.get(tx.sender, 0)
        total_cost = tx.amount + tx.fee
        
        if balance < total_cost:
            return False, "Insufficient balance"
        
        # Add to pool
        self.transaction_pool[tx.tx_id] = tx
        self.metrics['total_gas_used'] += GasSystem.calculate_gas(tx)
        
        return True, "Transaction submitted"
    
    def build_block(self, proposer: str, max_txs: int = 100) -> OriginBlock:
        """Build new block from transaction pool"""
        # Select transactions
        selected_txs = list(self.transaction_pool.values())[:max_txs]
        
        # Sort by fee (highest first)
        selected_txs.sort(key=lambda x: x.gas_price, reverse=True)
        
        # Build Merkle tree
        tx_strings = [tx.compute_hash() for tx in selected_txs]
        _, merkle_root = MerkleProof.build_tree(tx_strings)
        
        # Create block
        block = OriginBlock(
            block_id=f"block_{len(self.chain)}",
            height=len(self.chain),
            proposer=proposer,
            timestamp=datetime.now().timestamp(),
            transactions=selected_txs,
            previous_hash=self.main_chain_tip or "0x0",
            merkle_root=merkle_root,
            status=BlockStatus.PROPOSED,
        )
        
        # Apply state transitions
        temp_state = StateTransition()
        temp_state.balances = self.state.balances.copy()
        temp_state.stakes = self.state.stakes.copy()
        temp_state.nonces = self.state.nonces.copy()
        
        for tx in selected_txs:
            temp_state.apply_transaction(tx)
        
        block.state_root = temp_state.compute_root()
        block.consensus_hash = block.compute_hash()
        
        return block
    
    def add_block(self, block: OriginBlock) -> Tuple[bool, str]:
        """Add block to chain"""
        # Validate
        if block.height != len(self.chain):
            return False, "Invalid block height"
        
        if block.previous_hash != (self.main_chain_tip or "0x0"):
            return False, "Invalid previous hash"
        
        # Apply state
        for tx in block.transactions:
            success, _ = self.state.apply_transaction(tx)
            if not success:
                return False, "Transaction validation failed"
            
            # Remove from pool
            if tx.tx_id in self.transaction_pool:
                del self.transaction_pool[tx.tx_id]
            
            self.tx_index[tx.tx_id] = block
        
        # Update state root
        block.state_root = self.state.compute_root()
        
        # Add to chain
        self.chain.append(block)
        self.block_index[block.consensus_hash] = block
        self.main_chain_tip = block.consensus_hash
        
        # Update metrics
        self.metrics['total_blocks'] += 1
        self.metrics['total_transactions'] += len(block.transactions)
        
        if len(self.chain) > 1:
            block_time = block.timestamp - self.chain[-2].timestamp
            self.metrics['block_times'].append(block_time)
            if len(self.metrics['block_times']) > 100:
                self.metrics['block_times'] = self.metrics['block_times'][-100:]
            self.metrics['average_block_time'] = sum(self.metrics['block_times']) / len(self.metrics['block_times'])
        
        return True, "Block added"
    
    def get_balance(self, address: str) -> int:
        """Get account balance"""
        return self.state.balances.get(address, 0)
    
    def get_stake(self, address: str) -> int:
        """Get staked amount"""
        return self.state.stakes.get(address, 0)
    
    def get_transaction(self, tx_id: str) -> Optional[OriginTransaction]:
        """Get transaction by ID"""
        if tx_id in self.transaction_pool:
            return self.transaction_pool[tx_id]
        
        block = self.tx_index.get(tx_id)
        if block:
            for tx in block.transactions:
                if tx.tx_id == tx_id:
                    return tx
        
        return None
    
    def get_transaction_block(self, tx_id: str) -> Optional[OriginBlock]:
        """Get block containing transaction"""
        return self.tx_index.get(tx_id)
    
    def get_block(self, height: int) -> Optional[OriginBlock]:
        """Get block by height"""
        if 0 <= height < len(self.chain):
            return self.chain[height]
        return None
    
    def finalize_blocks(self, depth: int = 6) -> int:
        """Finalize blocks beyond reorg depth"""
        finalized_count = 0
        
        for block in self.chain:
            if block.status == BlockStatus.PROPOSED:
                blocks_since = len(self.chain) - block.height
                if blocks_since > depth:
                    block.status = BlockStatus.FINAL
                    finalized_count += 1
            elif block.status == BlockStatus.COMMITTED:
                blocks_since = len(self.chain) - block.height
                if blocks_since > depth:
                    block.status = BlockStatus.FINAL
                    finalized_count += 1
        
        return finalized_count
    
    def get_chain_state(self) -> Dict:
        """Get current chain state"""
        return {
            'height': len(self.chain) - 1,
            'block_count': len(self.chain),
            'tx_pool_size': len(self.transaction_pool),
            'state_root': self.state.compute_root(),
            'metrics': self.metrics,
        }
    
    def get_mempool_info(self) -> Dict:
        """Get mempool information"""
        total_gas = sum(GasSystem.calculate_gas(tx) for tx in self.transaction_pool.values())
        
        return {
            'size': len(self.transaction_pool),
            'total_gas': total_gas,
            'txs': list(self.transaction_pool.keys())[:100],
        }


if __name__ == "__main__":
    # Test original blockchain
    print("SYNTHOS Original Blockchain Test")
    print("=" * 50)
    
    bc = OriginalBlockchain({"validator1", "validator2", "validator3"})
    
    # Genesis
    genesis = bc.create_genesis({"alice": 10000, "bob": 5000, "charlie": 3000})
    print(f"✓ Genesis block created")
    
    # Transactions
    tx1 = OriginTransaction(
        tx_id="tx_001",
        tx_type=TransactionType.TRANSFER,
        sender="alice",
        receiver="bob",
        amount=100,
        fee=10,
        nonce=0,
        timestamp=datetime.now().timestamp(),
    )
    
    success, msg = bc.submit_transaction(tx1)
    print(f"✓ Transaction submitted: {msg}")
    
    # Build and add block
    block1 = bc.build_block("validator1", max_txs=10)
    success, msg = bc.add_block(block1)
    print(f"✓ Block added: {msg}")
    
    # Check state
    print(f"✓ Alice balance: {bc.get_balance('alice')}")
    print(f"✓ Bob balance: {bc.get_balance('bob')}")
    print(f"✓ Chain state: {bc.get_chain_state()}")
