"""Core Blockchain implementation for SYNTHOS Collective"""

import hashlib
import json
from typing import Dict, List, Optional, Tuple
from dataclasses import dataclass, field
from datetime import datetime
from src.models import Transaction, Block


class MerkleTree:
    """Merkle tree for transaction integrity verification"""
    
    def __init__(self, transactions: List[Transaction]):
        """
        Initialize Merkle tree
        
        Args:
            transactions: List of transactions to hash
        """
        self.transactions = transactions
        self.tree = []
        self.root = self._build_tree()
    
    @staticmethod
    def _hash_transaction(tx: Transaction) -> str:
        """Hash a transaction"""
        data = {
            'sender': tx.sender,
            'recipient': tx.recipient,
            'amount': tx.amount,
            'nonce': tx.nonce,
        }
        json_str = json.dumps(data, sort_keys=True)
        return hashlib.sha256(json_str.encode()).hexdigest()
    
    def _build_tree(self) -> str:
        """Build Merkle tree and return root hash"""
        if not self.transactions:
            return hashlib.sha256(b"empty").hexdigest()
        
        # Hash all transactions
        current_level = [self._hash_transaction(tx) for tx in self.transactions]
        self.tree.append(current_level)
        
        # Build tree level by level
        while len(current_level) > 1:
            next_level = []
            
            # Pair up hashes
            for i in range(0, len(current_level), 2):
                if i + 1 < len(current_level):
                    combined = current_level[i] + current_level[i + 1]
                else:
                    combined = current_level[i] + current_level[i]  # Duplicate if odd
                
                hash_value = hashlib.sha256(combined.encode()).hexdigest()
                next_level.append(hash_value)
            
            self.tree.append(next_level)
            current_level = next_level
        
        return current_level[0] if current_level else hashlib.sha256(b"empty").hexdigest()
    
    def verify_transaction(self, tx_index: int, proof: List[str]) -> bool:
        """
        Verify transaction inclusion in Merkle tree
        
        Args:
            tx_index: Index of transaction
            proof: Merkle proof path
            
        Returns:
            True if transaction is in tree
        """
        if tx_index >= len(self.transactions):
            return False
        
        current_hash = self._hash_transaction(self.transactions[tx_index])
        
        for proof_hash in proof:
            combined = current_hash + proof_hash
            current_hash = hashlib.sha256(combined.encode()).hexdigest()
        
        return current_hash == self.root


class Mempool:
    """Transaction mempool for pending transactions"""
    
    def __init__(self, max_size: int = 10000):
        """
        Initialize mempool
        
        Args:
            max_size: Maximum number of pending transactions
        """
        self.transactions: Dict[str, Transaction] = {}
        self.max_size = max_size
        self.nonce_map: Dict[str, int] = {}  # Track nonces per sender
    
    def add_transaction(self, tx: Transaction) -> Tuple[bool, str]:
        """
        Add transaction to mempool
        
        Args:
            tx: Transaction to add
            
        Returns:
            Tuple of (success, message)
        """
        # Check mempool not full
        if len(self.transactions) >= self.max_size:
            return False, "Mempool is full"
        
        # Check duplicate
        if tx.id in self.transactions:
            return False, "Transaction already in mempool"
        
        # Track nonce
        current_nonce = self.nonce_map.get(tx.sender, 0)
        if tx.nonce < current_nonce:
            return False, "Nonce too low"
        
        self.transactions[tx.id] = tx
        self.nonce_map[tx.sender] = max(current_nonce, tx.nonce + 1)
        
        return True, "Transaction added to mempool"
    
    def remove_transaction(self, tx_id: str) -> bool:
        """Remove transaction from mempool"""
        if tx_id in self.transactions:
            del self.transactions[tx_id]
            return True
        return False
    
    def get_pending_transactions(self, count: int = 100) -> List[Transaction]:
        """Get N pending transactions (highest fee first)"""
        sorted_txs = sorted(
            self.transactions.values(),
            key=lambda tx: tx.fee,
            reverse=True
        )
        return sorted_txs[:count]
    
    def get_transaction(self, tx_id: str) -> Optional[Transaction]:
        """Get transaction by ID"""
        return self.transactions.get(tx_id)
    
    def clear(self) -> None:
        """Clear all pending transactions"""
        self.transactions.clear()
        self.nonce_map.clear()
    
    def get_size(self) -> int:
        """Get current mempool size"""
        return len(self.transactions)


@dataclass
class BlockchainState:
    """State snapshot for blockchain"""
    height: int
    block_count: int
    total_supply: int = 1000000  # Initial supply
    total_transactions: int = 0
    total_fees: int = 0
    average_block_time: float = 0.0
    last_block_hash: str = ""
    difficulty: int = 1
    timestamp: float = field(default_factory=lambda: datetime.now().timestamp())


class Blockchain:
    """
    Core blockchain implementation
    
    Manages:
    - Block chain and validation
    - Transaction pool
    - Chain state
    - Fork handling
    - Finality
    """
    
    def __init__(self, agent=None):
        """
        Initialize blockchain
        
        Args:
            agent: Parent SYNTHOS Agent (optional)
        """
        self.agent = agent
        self.chain: List[Block] = []
        self.mempool = Mempool()
        self.state = BlockchainState(height=0, block_count=0)
        self.difficult = 1
        self.max_block_size = 1_000_000  # 1MB
        self.block_time_target = 12.0  # seconds
        self.block_times: List[float] = []
        self.finalized_height = 0
        self.finality_depth = 6  # 6 blocks for finality
        
        # Fork tracking
        self.fork_points: Dict[int, List[Block]] = {}
        
        # State transitions
        self.balances: Dict[str, int] = {}
    
    def create_genesis_block(self, initial_balances: Dict[str, int] = None) -> Block:
        """
        Create genesis block
        
        Args:
            initial_balances: Initial account balances
            
        Returns:
            Genesis Block
        """
        if initial_balances:
            self.balances = initial_balances.copy()
        else:
            self.balances = {"genesis": self.state.total_supply}
        
        genesis = Block(
            height=0,
            proposer="genesis",
            transactions=[],
            previous_hash="0x0",
            timestamp=datetime.now().timestamp(),
        )
        
        genesis.hash = genesis.compute_hash()
        genesis.state_root = self._compute_state_root()
        
        self.chain.append(genesis)
        self.state.block_count = 1
        self.state.last_block_hash = genesis.hash
        
        return genesis
    
    def get_pending_block(self, proposer: str, max_transactions: int = 100) -> Block:
        """
        Create pending block from mempool
        
        Args:
            proposer: Block proposer address
            max_transactions: Max transactions to include
            
        Returns:
            Pending Block (not yet added to chain)
        """
        # Get transactions from mempool
        pending_txs = self.mempool.get_pending_transactions(max_transactions)
        
        # Check block size
        block_size = 0
        included_txs = []
        
        for tx in pending_txs:
            tx_size = len(json.dumps({
                'sender': tx.sender,
                'recipient': tx.recipient,
                'amount': tx.amount,
            }))
            
            if block_size + tx_size <= self.max_block_size:
                included_txs.append(tx)
                block_size += tx_size
            else:
                break
        
        # Create block
        block = Block(
            height=len(self.chain),
            proposer=proposer,
            transactions=included_txs,
            previous_hash=self.chain[-1].hash if self.chain else "0x0",
            timestamp=datetime.now().timestamp(),
        )
        
        block.state_root = self._compute_state_root()
        
        return block
    
    def submit_transaction(self, tx: Transaction) -> Tuple[bool, str]:
        """
        Submit transaction to mempool
        
        Args:
            tx: Transaction to submit
            
        Returns:
            Tuple of (success, message)
        """
        return self.mempool.add_transaction(tx)
    
    def add_block(self, block: Block) -> Tuple[bool, str]:
        """
        Add block to blockchain
        
        Args:
            block: Block to add
            
        Returns:
            Tuple of (success, message)
        """
        # Validate block
        is_valid, message = self.validate_block(block)
        if not is_valid:
            return False, message
        
        # Compute block hash
        block.hash = block.compute_hash()
        
        # Remove transactions from mempool
        for tx in block.transactions:
            self.mempool.remove_transaction(tx.id)
        
        # Add to chain
        self.chain.append(block)
        self.state.block_count += 1
        self.state.height = len(self.chain) - 1
        self.state.last_block_hash = block.hash
        self.state.total_transactions += len(block.transactions)
        
        # Track block time
        if len(self.chain) > 1:
            block_time = block.timestamp - self.chain[-2].timestamp
            self.block_times.append(block_time)
            if len(self.block_times) > 100:
                self.block_times = self.block_times[-100:]
            
            self.state.average_block_time = sum(self.block_times) / len(self.block_times)
        
        # Update finality
        if self.state.height - self.finalized_height >= self.finality_depth:
            self.finalized_height = self.state.height - self.finality_depth
        
        return True, "Block added to blockchain"
    
    def validate_block(self, block: Block) -> Tuple[bool, str]:
        """
        Validate block structure and transactions
        
        Args:
            block: Block to validate
            
        Returns:
            Tuple of (is_valid, message)
        """
        # Check block height
        if block.height != len(self.chain):
            return False, f"Invalid block height: expected {len(self.chain)}, got {block.height}"
        
        # Check previous hash
        expected_prev = self.chain[-1].hash if self.chain else "0x0"
        if block.previous_hash != expected_prev:
            return False, "Invalid previous hash"
        
        # Validate all transactions
        temp_balances = self.balances.copy()
        
        for tx in block.transactions:
            # Check sender balance
            sender_balance = temp_balances.get(tx.sender, 0)
            if sender_balance < tx.amount + tx.fee:
                return False, f"Insufficient balance for {tx.sender}"
            
            # Apply transaction
            temp_balances[tx.sender] = sender_balance - tx.amount - tx.fee
            temp_balances[tx.recipient] = temp_balances.get(tx.recipient, 0) + tx.amount
        
        # Validate Merkle root
        if block.transactions:
            merkle_tree = MerkleTree(block.transactions)
            if not merkle_tree.root:
                return False, "Invalid Merkle root"
        
        return True, "Block valid"
    
    def validate_chain(self) -> Tuple[bool, str]:
        """
        Validate entire blockchain
        
        Returns:
            Tuple of (is_valid, message)
        """
        if not self.chain:
            return False, "Empty blockchain"
        
        # Check genesis block
        genesis = self.chain[0]
        if genesis.height != 0 or genesis.previous_hash != "0x0":
            return False, "Invalid genesis block"
        
        # Check chain continuity
        for i in range(1, len(self.chain)):
            block = self.chain[i]
            prev_block = self.chain[i - 1]
            
            if block.height != i:
                return False, f"Height mismatch at block {i}"
            
            if block.previous_hash != prev_block.hash:
                return False, f"Hash mismatch at block {i}"
        
        return True, "Blockchain valid"
    
    def get_block(self, height: int) -> Optional[Block]:
        """Get block by height"""
        if 0 <= height < len(self.chain):
            return self.chain[height]
        return None
    
    def get_balance(self, address: str) -> int:
        """Get account balance"""
        return self.balances.get(address, 0)
    
    def _compute_state_root(self) -> str:
        """Compute state root hash"""
        state_data = json.dumps(self.balances, sort_keys=True)
        return hashlib.sha256(state_data.encode()).hexdigest()
    
    def get_chain_height(self) -> int:
        """Get current chain height"""
        return len(self.chain) - 1
    
    def is_on_main_chain(self, block_hash: str) -> bool:
        """Check if block is on main chain"""
        for block in self.chain:
            if block.hash == block_hash:
                return True
        return False
    
    def fork_at_height(self, height: int) -> 'Blockchain':
        """
        Create fork at specific height
        
        Args:
            height: Height to fork at
            
        Returns:
            New Blockchain instance with fork
        """
        new_chain = Blockchain(self.agent)
        new_chain.chain = self.chain[:height].copy()
        new_chain.balances = self.balances.copy()
        new_chain.state = BlockchainState(
            height=height - 1,
            block_count=height,
            total_supply=self.state.total_supply,
            total_transactions=self.state.total_transactions,
        )
        return new_chain
    
    def reorg_to_chain(self, new_chain_blocks: List[Block]) -> Tuple[bool, str]:
        """
        Reorganize chain to new canonical chain
        
        Args:
            new_chain_blocks: Blocks from new chain
            
        Returns:
            Tuple of (success, message)
        """
        # Find common ancestor
        common_height = 0
        for i in range(min(len(self.chain), len(new_chain_blocks))):
            if self.chain[i].hash != new_chain_blocks[i].hash:
                break
            common_height = i + 1
        
        # Replace chain
        self.chain = new_chain_blocks
        self.state.height = len(self.chain) - 1
        self.state.block_count = len(self.chain)
        
        return True, f"Reorganized to new chain at height {common_height}"
    
    def get_finalized_height(self) -> int:
        """Get finalized block height (cannot be reorganized)"""
        return self.finalized_height
    
    def get_unfinalized_blocks(self) -> List[Block]:
        """Get blocks that are not yet finalized"""
        start = self.finalized_height + 1
        return self.chain[start:]


if __name__ == "__main__":
    # Test blockchain
    bc = Blockchain()
    
    # Create genesis
    genesis = bc.create_genesis_block({"alice": 1000, "bob": 500})
    print(f"Genesis: {genesis.hash[:16]}...")
    
    # Create transaction
    tx = Transaction(
        sender="alice",
        recipient="bob",
        amount=100,
        fee=1,
        nonce=0
    )
    
    success, msg = bc.submit_transaction(tx)
    print(f"Transaction: {msg}")
    
    # Create and add block
    block = bc.get_pending_block("validator1")
    success, msg = bc.add_block(block)
    print(f"Block: {msg}")
    
    # Check state
    print(f"Chain height: {bc.get_chain_height()}")
    print(f"Alice balance: {bc.get_balance('alice')}")
    print(f"Bob balance: {bc.get_balance('bob')}")
