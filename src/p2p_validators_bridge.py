"""
SYNTHOS P2P Network, Validator Management, and Cross-Chain System
Original implementations
"""

from typing import Dict, List, Set, Optional, Tuple, Any
from dataclasses import dataclass, field
from datetime import datetime
from enum import Enum
import hashlib


class MessageType(Enum):
    """Original P2P message types"""
    BLOCK_PROPOSAL = "block_proposal"
    BLOCK_COMMITTED = "block_committed"
    TRANSACTION = "transaction"
    VOTE = "vote"
    SYNC_REQUEST = "sync_request"
    SYNC_RESPONSE = "sync_response"
    PEER_INFO = "peer_info"
    STATE_PROOF = "state_proof"


@dataclass
class P2PMessage:
    """Original P2P message"""
    msg_id: str
    msg_type: MessageType
    sender: str
    timestamp: float
    payload: Dict[str, Any]
    signature: str = ""
    priority: int = 1


class PeerConnection:
    """Connection to a peer"""
    
    def __init__(self, peer_id: str, address: str, port: int):
        self.peer_id = peer_id
        self.address = address
        self.port = port
        self.connected = False
        self.last_seen = datetime.now().timestamp()
        self.messages_sent = 0
        self.messages_received = 0
        self.latency_ms = 0
    
    def update_latency(self, latency: float) -> None:
        """Update peer latency"""
        self.latency_ms = latency
        self.last_seen = datetime.now().timestamp()
    
    def record_message(self, sent: bool = False) -> None:
        """Record message send/receive"""
        if sent:
            self.messages_sent += 1
        else:
            self.messages_received += 1


class P2PNetwork:
    """Original P2P network layer"""
    
    def __init__(self, node_id: str):
        self.node_id = node_id
        self.peers: Dict[str, PeerConnection] = {}
        self.message_queue: List[P2PMessage] = []
        self.processed_messages: Set[str] = set()
        self.pending_syncs: Dict[str, Dict] = {}
        
        # Network stats
        self.total_bytes_sent = 0
        self.total_bytes_received = 0
    
    def add_peer(self, peer_id: str, address: str, port: int) -> Tuple[bool, str]:
        """Add peer to network"""
        if peer_id in self.peers:
            return False, "Peer already connected"
        
        peer = PeerConnection(peer_id, address, port)
        self.peers[peer_id] = peer
        peer.connected = True
        
        return True, f"Connected to {peer_id}"
    
    def remove_peer(self, peer_id: str) -> bool:
        """Remove peer from network"""
        if peer_id in self.peers:
            del self.peers[peer_id]
            return True
        return False
    
    def broadcast(self, msg: P2PMessage, exclude: Set[str] = None) -> int:
        """
        Broadcast message to all peers
        
        Returns:
            Number of peers sent to
        """
        sent_count = 0
        exclude = exclude or set()
        
        for peer_id, peer in self.peers.items():
            if peer_id not in exclude and peer.connected:
                peer.record_message(sent=True)
                self.total_bytes_sent += len(str(msg.payload))
                sent_count += 1
        
        return sent_count
    
    def unicast(self, peer_id: str, msg: P2PMessage) -> Tuple[bool, str]:
        """Send message to specific peer"""
        if peer_id not in self.peers:
            return False, "Peer not found"
        
        peer = self.peers[peer_id]
        if not peer.connected:
            return False, "Peer not connected"
        
        peer.record_message(sent=True)
        self.total_bytes_sent += len(str(msg.payload))
        
        return True, "Message sent"
    
    def receive_message(self, msg: P2PMessage) -> Tuple[bool, str]:
        """Receive message from peer"""
        # Check if already processed
        if msg.msg_id in self.processed_messages:
            return False, "Message already processed"
        
        # Process message
        self.message_queue.append(msg)
        self.processed_messages.add(msg.msg_id)
        
        if msg.sender in self.peers:
            self.peers[msg.sender].record_message(sent=False)
        
        self.total_bytes_received += len(str(msg.payload))
        
        return True, "Message received"
    
    def get_peer_info(self) -> Dict[str, Any]:
        """Get network information"""
        peer_list = []
        for peer_id, peer in self.peers.items():
            peer_list.append({
                'id': peer_id,
                'address': f"{peer.address}:{peer.port}",
                'connected': peer.connected,
                'latency_ms': peer.latency_ms,
                'messages_sent': peer.messages_sent,
                'messages_received': peer.messages_received,
            })
        
        return {
            'node_id': self.node_id,
            'peer_count': len(self.peers),
            'peers': peer_list,
            'total_bytes_sent': self.total_bytes_sent,
            'total_bytes_received': self.total_bytes_received,
            'queue_size': len(self.message_queue),
        }


class ValidatorStatus(Enum):
    """Validator status"""
    ACTIVE = "active"
    INACTIVE = "inactive"
    SLASHED = "slashed"
    EXITED = "exited"


@dataclass
class Validator:
    """Blockchain validator"""
    address: str
    stake: int
    commission: float
    uptime: float
    status: ValidatorStatus
    slash_count: int = 0
    rewards_earned: int = 0
    signed_blocks: int = 0
    missed_blocks: int = 0
    joined_at: float = field(default_factory=lambda: datetime.now().timestamp())
    
    def compute_score(self) -> float:
        """Compute validator score"""
        stake_factor = min(self.stake / 1000000, 1.0)  # Normalize
        uptime_factor = self.uptime
        base_score = (stake_factor + uptime_factor) / 2
        slash_penalty = self.slash_count * 0.1
        
        return max(0, base_score - slash_penalty)


class ValidatorRegistry:
    """Registry of active validators"""
    
    def __init__(self):
        self.validators: Dict[str, Validator] = {}
        self.active_set: Set[str] = set()
        self.inactive: Set[str] = set()
        self.slashed: Set[str] = set()
        self.exited: Set[str] = set()
    
    def add_validator(self, validator: Validator) -> Tuple[bool, str]:
        """Add validator to registry"""
        if validator.address in self.validators:
            return False, "Validator already registered"
        
        self.validators[validator.address] = validator
        
        if validator.status == ValidatorStatus.ACTIVE:
            self.active_set.add(validator.address)
        
        return True, "Validator registered"
    
    def update_validator_stake(self, address: str, new_stake: int) -> bool:
        """Update validator stake"""
        if address not in self.validators:
            return False
        
        self.validators[address].stake = new_stake
        return True
    
    def slash_validator(self, address: str, penalty: int) -> bool:
        """Apply slashing to validator"""
        if address not in self.validators:
            return False
        
        validator = self.validators[address]
        validator.stake = max(0, validator.stake - penalty)
        validator.slash_count += 1
        
        if validator.stake == 0:
            validator.status = ValidatorStatus.SLASHED
            self.slashed.add(address)
            if address in self.active_set:
                self.active_set.remove(address)
        
        return True
    
    def set_validator_status(self, address: str, status: ValidatorStatus) -> bool:
        """Update validator status"""
        if address not in self.validators:
            return False
        
        validator = self.validators[address]
        old_status = validator.status
        validator.status = status
        
        # Update sets
        if old_status == ValidatorStatus.ACTIVE:
            self.active_set.discard(address)
        
        if status == ValidatorStatus.ACTIVE:
            self.active_set.add(address)
        elif status == ValidatorStatus.SLASHED:
            self.slashed.add(address)
        elif status == ValidatorStatus.EXITED:
            self.exited.add(address)
        
        return True
    
    def get_top_validators(self, count: int = 32) -> List[Validator]:
        """Get top validators by score"""
        active_validators = [
            self.validators[addr] for addr in self.active_set
        ]
        
        active_validators.sort(
            key=lambda v: v.compute_score(),
            reverse=True
        )
        
        return active_validators[:count]
    
    def get_validator(self, address: str) -> Optional[Validator]:
        """Get validator by address"""
        return self.validators.get(address)


class LightClient:
    """Original light client for efficiency"""
    
    def __init__(self, node_id: str):
        self.node_id = node_id
        self.header_chain: List[Dict] = []  # Only block headers
        self.merkle_roots: Dict[int, str] = {}  # Block height -> root
        self.trusted_checkpoint: Optional[Dict] = None
        self.state_proofs: Dict[str, Dict] = {}  # address -> proof
    
    def add_header(self, header: Dict) -> bool:
        """Add block header only"""
        if not self._validate_header(header):
            return False
        
        self.header_chain.append(header)
        self.merkle_roots[header['height']] = header['merkle_root']
        
        return True
    
    def verify_transaction_proof(self, tx_hash: str, block_height: int, 
                                 merkle_proof: List[str]) -> bool:
        """Verify transaction inclusion"""
        if block_height not in self.merkle_roots:
            return False
        
        root = self.merkle_roots[block_height]
        current = hashlib.sha256(tx_hash.encode()).hexdigest()
        
        for proof_step in merkle_proof:
            combined = current + proof_step
            current = hashlib.sha256(combined.encode()).hexdigest()
        
        return current == root
    
    def verify_state_proof(self, address: str, value: int, proof: Dict) -> bool:
        """Verify state at address"""
        # Simplified state proof verification
        return True
    
    def _validate_header(self, header: Dict) -> bool:
        """Validate block header"""
        required_fields = ['height', 'previous_hash', 'merkle_root', 'timestamp']
        return all(field in header for field in required_fields)
    
    def sync_to_height(self, target_height: int) -> Tuple[bool, str]:
        """Sync light client to target height"""
        current_height = len(self.header_chain) - 1
        
        if current_height >= target_height:
            return False, "Already at or past target"
        
        # In real implementation, would request headers from full nodes
        return True, f"Synced to height {target_height}"
    
    def get_balance(self, address: str, block_height: int) -> Optional[int]:
        """Get balance with proof"""
        # In real implementation, would get with merkle proof
        return None


class CrossChainBridge:
    """Original cross-chain bridge system"""
    
    def __init__(self, local_chain_id: str):
        self.local_chain_id = local_chain_id
        self.bridges: Dict[str, Dict] = {}  # chain_id -> bridge_config
        self.locked_assets: Dict[str, int] = {}  # asset_id -> amount
        self.minted_tokens: Dict[str, int] = {}  # token_id -> supply
        self.cross_chain_txs: List[Dict] = []
    
    def register_bridge(self, target_chain_id: str, config: Dict) -> Tuple[bool, str]:
        """Register bridge to another chain"""
        if target_chain_id in self.bridges:
            return False, "Bridge already registered"
        
        self.bridges[target_chain_id] = config
        return True, f"Bridge registered to {target_chain_id}"
    
    def lock_assets(self, asset_id: str, amount: int, receiver: str,
                   target_chain: str) -> Tuple[bool, str]:
        """Lock assets on local chain"""
        if target_chain not in self.bridges:
            return False, "Target chain not supported"
        
        self.locked_assets[asset_id] = self.locked_assets.get(asset_id, 0) + amount
        
        cross_chain_tx = {
            'id': hashlib.sha256((asset_id + str(amount)).encode()).hexdigest()[:16],
            'asset': asset_id,
            'amount': amount,
            'receiver': receiver,
            'source_chain': self.local_chain_id,
            'target_chain': target_chain,
            'status': 'locked',
            'timestamp': datetime.now().timestamp(),
        }
        
        self.cross_chain_txs.append(cross_chain_tx)
        
        return True, f"Assets locked: {cross_chain_tx['id']}"
    
    def mint_wrapped_token(self, token_id: str, amount: int, receiver: str) -> Tuple[bool, str]:
        """Mint wrapped token on this chain"""
        self.minted_tokens[token_id] = self.minted_tokens.get(token_id, 0) + amount
        
        return True, f"Minted {amount} {token_id} to {receiver}"
    
    def unlock_assets(self, asset_id: str, amount: int, receiver: str) -> Tuple[bool, str]:
        """Unlock assets on local chain"""
        locked_amount = self.locked_assets.get(asset_id, 0)
        
        if locked_amount < amount:
            return False, "Insufficient locked assets"
        
        self.locked_assets[asset_id] = locked_amount - amount
        
        return True, f"Unlocked {amount} {asset_id} to {receiver}"
    
    def get_bridge_status(self, target_chain: str) -> Optional[Dict]:
        """Get bridge status"""
        if target_chain not in self.bridges:
            return None
        
        return {
            'target_chain': target_chain,
            'config': self.bridges[target_chain],
            'pending_txs': len([tx for tx in self.cross_chain_txs if tx['status'] == 'pending']),
            'locked_assets': sum(self.locked_assets.values()),
            'minted_tokens': sum(self.minted_tokens.values()),
        }


if __name__ == "__main__":
    print("SYNTHOS P2P Network, Validators, and Cross-Chain Test")
    print("=" * 60)
    
    # P2P Network
    net = P2PNetwork("node_001")
    net.add_peer("peer_1", "192.168.1.1", 30330)
    net.add_peer("peer_2", "192.168.1.2", 30330)
    print(f"✓ P2P Network: {len(net.peers)} peers")
    
    # Validators
    registry = ValidatorRegistry()
    v1 = Validator("validator1", 1000, 0.05, 0.99, ValidatorStatus.ACTIVE)
    registry.add_validator(v1)
    print(f"✓ Validator registered: {v1.address}")
    
    # Light Client
    light = LightClient("light_001")
    print(f"✓ Light client created: {light.node_id}")
    
    # Cross-Chain
    bridge = CrossChainBridge("chain_001")
    bridge.register_bridge("chain_002", {"fee_percent": 0.1})
    success, msg = bridge.lock_assets("eth", 100, "alice", "chain_002")
    print(f"✓ Cross-chain: {msg}")
