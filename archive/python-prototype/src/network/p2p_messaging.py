"""Peer-to-Peer Messaging and Gossip Protocol"""

from typing import Dict, List, Any, Optional, Callable
from dataclasses import dataclass, field
from enum import Enum
from datetime import datetime
import asyncio


class MessageType(Enum):
    """Types of P2P messages"""
    BLOCK_PROPOSAL = "block_proposal"
    BLOCK_COMMIT = "block_commit"
    TRANSACTION = "transaction"
    VOTE = "vote"
    CHALLENGE = "challenge"
    GOVERNANCE_PROPOSAL = "governance_proposal"
    GOVERNANCE_VOTE = "governance_vote"
    SLASHING_EVENT = "slashing_event"
    PEER_INFO = "peer_info"
    SYNC_REQUEST = "sync_request"
    SYNC_RESPONSE = "sync_response"


@dataclass
class P2PMessage:
    """A P2P message between agents"""
    message_type: MessageType
    sender: str
    content: Dict[str, Any]
    timestamp: float = field(default_factory=lambda: datetime.now().timestamp())
    nonce: str = ""
    signature: bytes = field(default_factory=bytes)
    
    def compute_hash(self) -> str:
        """Compute message hash"""
        import hashlib
        import json
        data = {
            'type': self.message_type.value,
            'sender': self.sender,
            'timestamp': self.timestamp,
            'nonce': self.nonce,
        }
        json_str = json.dumps(data, sort_keys=True)
        return hashlib.sha256(json_str.encode()).hexdigest()


@dataclass
class GossipMessage:
    """Message for gossip protocol"""
    message_id: str
    original_sender: str
    message_type: MessageType
    content: Any
    propagation_count: int = 0
    max_propagation: int = 5  # Max hops
    seen_by: List[str] = field(default_factory=list)
    timestamp: float = field(default_factory=lambda: datetime.now().timestamp())


class P2PMessenger:
    """Handles peer-to-peer messaging"""
    
    def __init__(self, agent):
        self.agent = agent
        self.pending_messages: Dict[str, P2PMessage] = {}
        self.message_handlers: Dict[MessageType, Callable] = {}
    
    async def register_message_handler(self,
                                      message_type: MessageType,
                                      handler: Callable) -> None:
        """Register handler for message type"""
        self.message_handlers[message_type] = handler
    
    async def send_message(self,
                          peer_id: str,
                          message: P2PMessage) -> bool:
        """Send message to specific peer"""
        try:
            # Would send over network
            return await self.agent.get_role("Communicator").unicast(
                peer_id, message
            )
        except Exception as e:
            self.agent.logger.error(f"Error sending message to {peer_id}: {e}")
            return False
    
    async def broadcast_message(self, message: P2PMessage) -> bool:
        """Broadcast message to all peers"""
        try:
            return await self.agent.get_role("Communicator").broadcast(message)
        except Exception as e:
            self.agent.logger.error(f"Error broadcasting message: {e}")
            return False
    
    async def receive_message(self, message: P2PMessage) -> bool:
        """Receive and process message"""
        # Handle message based on type
        handler = self.message_handlers.get(message.message_type)
        
        if handler:
            try:
                return await handler(message)
            except Exception as e:
                self.agent.logger.error(
                    f"Error handling {message.message_type.value}: {e}"
                )
                return False
        
        return False
    
    # Default message handlers
    async def handle_block_proposal(self, message: P2PMessage) -> bool:
        """Handle block proposal message"""
        block = message.content.get('block')
        if block:
            return await self.agent.handle_incoming_block(block)
        return False
    
    async def handle_transaction(self, message: P2PMessage) -> bool:
        """Handle transaction message"""
        transaction = message.content.get('transaction')
        if transaction:
            return await self.agent.submit_transaction(transaction)
        return False
    
    async def handle_governance_proposal(self, message: P2PMessage) -> bool:
        """Handle governance proposal message"""
        proposal = message.content.get('proposal')
        if proposal:
            return await self.agent.handle_incoming_proposal(proposal)
        return False
    
    async def handle_vote(self, message: P2PMessage) -> bool:
        """Handle vote message"""
        # Update local voting state
        return True
    
    async def handle_slashing_event(self, message: P2PMessage) -> bool:
        """Handle slashing event notification"""
        # Update local state with slashing info
        return True


class GossipProtocol:
    """
    Gossip protocol for reliable message propagation
    
    Ensures:
    - High probability all nodes receive message
    - Reduced bandwidth vs broadcast
    - Resilience to packet loss
    """
    
    def __init__(self, agent):
        self.agent = agent
        self.gossip_cache: Dict[str, GossipMessage] = {}
        self.propagation_rate = 0.5  # Propagate to 50% of peers on average
        self._message_id_counter = 0
    
    async def publish_gossip(self,
                            message_type: MessageType,
                            content: Any) -> str:
        """
        Publish message via gossip protocol
        
        Message will propagate throughout network
        """
        message_id = f"{self.agent.id}:{self._message_id_counter}"
        self._message_id_counter += 1
        
        gossip = GossipMessage(
            message_id=message_id,
            original_sender=self.agent.id,
            message_type=message_type,
            content=content,
        )
        
        self.gossip_cache[message_id] = gossip
        
        # Start propagation
        await self._propagate_gossip(gossip)
        
        return message_id
    
    async def receive_gossip(self, gossip: GossipMessage) -> bool:
        """Receive gossip message"""
        message_id = gossip.message_id
        
        # Check if already seen
        if message_id in self.gossip_cache:
            return True
        
        # Store and re-propagate
        self.gossip_cache[message_id] = gossip
        gossip.seen_by.append(self.agent.id)
        
        # Continue propagation if under limit
        if gossip.propagation_count < gossip.max_propagation:
            gossip.propagation_count += 1
            await self._propagate_gossip(gossip)
        
        # Process message
        return await self._process_gossip_content(gossip)
    
    async def _propagate_gossip(self, gossip: GossipMessage) -> None:
        """Propagate gossip to random peers"""
        peers = await self.agent.get_role("Communicator").discover_peers()
        
        # Randomly select subset of peers
        import random
        propagate_count = max(
            1,
            int(len(peers) * self.propagation_rate)
        )
        
        selected_peers = random.sample(
            peers,
            min(propagate_count, len(peers))
        )
        
        # Send to selected peers (don't send to originator)
        for peer in selected_peers:
            if peer.id != gossip.original_sender:
                await self.agent.get_role("Communicator").unicast(
                    peer.id,
                    {
                        'type': 'gossip',
                        'content': gossip,
                    }
                )
    
    async def _process_gossip_content(self, gossip: GossipMessage) -> bool:
        """Process gossip message content"""
        message_type = gossip.message_type
        
        if message_type == MessageType.BLOCK_PROPOSAL:
            from src.core.event import Event, EventType
            await self.agent.event_bus.publish(Event(
                type=EventType.BLOCK_PROPOSED,
                source='GossipProtocol',
                data=gossip.content,
                priority=2
            ))
            return True
        
        elif message_type == MessageType.TRANSACTION:
            await self.agent.submit_transaction(gossip.content)
            return True
        
        elif message_type == MessageType.SLASHING_EVENT:
            from src.core.event import Event, EventType
            await self.agent.event_bus.publish(Event(
                type=EventType.SLASHING_TRIGGERED,
                source='GossipProtocol',
                data=gossip.content,
                priority=1
            ))
            return True
        
        return False
    
    async def get_gossip_stats(self) -> Dict[str, Any]:
        """Get gossip protocol statistics"""
        total_messages = len(self.gossip_cache)
        
        # Count by type
        type_counts = {}
        for gossip in self.gossip_cache.values():
            msg_type = gossip.message_type.value
            type_counts[msg_type] = type_counts.get(msg_type, 0) + 1
        
        # Average propagation
        if total_messages > 0:
            avg_propagation = sum(
                g.propagation_count for g in self.gossip_cache.values()
            ) / total_messages
        else:
            avg_propagation = 0
        
        return {
            'total_messages': total_messages,
            'messages_by_type': type_counts,
            'average_propagation_hops': avg_propagation,
            'cache_size': len(self.gossip_cache),
        }


class PeerNegotiator:
    """
    Handles negotiation with peers for:
    - Fee agreements
    - Liquidity provision
    - Collateral requirements
    - Risk exposure limits
    """
    
    def __init__(self, agent):
        self.agent = agent
        self.active_negotiations: Dict[str, Dict] = {}
        self.negotiation_history: List[Dict] = []
    
    async def negotiate_fees(self,
                            peer_id: str,
                            desired_fee_rate: float) -> Optional[float]:
        """
        Negotiate transaction fee rate with peer
        
        Returns:
            Agreed fee rate or None if negotiation fails
        """
        # Create negotiation session
        negotiation_id = f"fee_{self.agent.id}_{peer_id}"
        
        self.active_negotiations[negotiation_id] = {
            'type': 'fee_negotiation',
            'peer': peer_id,
            'desired_rate': desired_fee_rate,
            'status': 'pending',
        }
        
        # Send negotiation request
        # (Would communicate with peer)
        
        # Placeholder - would complete negotiation
        agreed_rate = desired_fee_rate * 0.95  # 5% discount
        
        self.active_negotiations[negotiation_id]['status'] = 'completed'
        self.active_negotiations[negotiation_id]['agreed_rate'] = agreed_rate
        
        self.negotiation_history.append(self.active_negotiations[negotiation_id])
        
        return agreed_rate
    
    async def negotiate_liquidity(self,
                                 peer_id: str,
                                 required_liquidity: int) -> Optional[int]:
        """
        Negotiate liquidity provision with peer
        
        Returns:
            Agreed liquidity amount or None
        """
        # Would negotiate with peer for liquidity provision
        return required_liquidity
    
    async def set_collateral_requirement(self,
                                        peer_id: str,
                                        collateral_amount: int) -> bool:
        """Set collateral requirement for peer"""
        # Would establish collateral agreement
        return True
    
    async def set_risk_exposure_limit(self,
                                     peer_id: str,
                                     max_exposure: int) -> bool:
        """Set maximum risk exposure limit with peer"""
        # Would establish exposure limit
        return True
