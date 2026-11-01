"""Communicator Role Implementation"""

from src.core.base_role import Role
from src.core.event import Event, EventType
from typing import Any, Dict, List, Optional
from collections import deque


class CommunicatorRole(Role):
    """
    Communicator Role - Network communication and peer coordination
    
    Responsibilities:
    - Broadcast transactions and blocks
    - Receive and relay peer messages
    - Maintain peer connections
    - Coordinate consensus messages
    """
    
    def __init__(self, agent):
        super().__init__(agent)
        self.name = "Communicator"
        self.version = "1.0.0"
        self.peers: Dict[str, Any] = {}
        self.message_queue: deque = deque(maxlen=1000)
        self._network_metrics = {
            'messages_sent': 0,
            'messages_received': 0,
            'peers_connected': 0,
            'broadcast_count': 0,
        }
    
    async def initialize(self) -> None:
        """Initialize communicator"""
        await super().initialize()
        
        self.agent.event_bus.subscribe(
            EventType.TRANSACTION_VALIDATED,
            self.broadcast_validated_transaction
        )
        self.agent.event_bus.subscribe(
            EventType.BLOCK_VALIDATED,
            self.broadcast_validated_block
        )
        self.agent.event_bus.subscribe(
            EventType.CONSENSUS_VOTE,
            self.broadcast_vote
        )
        
        self.agent.logger.info(f"{self.name} role initialized")
    
    async def broadcast(self, message: Any) -> bool:
        """
        Broadcast message to all peers
        
        Args:
            message: Message to broadcast
            
        Returns:
            True if broadcast was successful
        """
        try:
            # Send to all connected peers
            failed_peers = []
            for peer_id, peer in self.peers.items():
                if not await self.send_message(peer_id, message):
                    failed_peers.append(peer_id)
            
            self._network_metrics['broadcast_count'] += 1
            
            # Remove failed peers
            for peer_id in failed_peers:
                await self.disconnect_peer(peer_id)
            
            return len(failed_peers) == 0
            
        except Exception as e:
            self.agent.logger.error(f"Broadcast error: {e}")
            return False
    
    async def unicast(self, peer_id: str, message: Any) -> bool:
        """
        Send message to specific peer
        
        Args:
            peer_id: Target peer ID
            message: Message to send
            
        Returns:
            True if message was sent
        """
        return await self.send_message(peer_id, message)
    
    async def send_message(self, peer_id: str, message: Any) -> bool:
        """
        Send message to peer
        
        Args:
            peer_id: Target peer
            message: Message to send
            
        Returns:
            True if successful
        """
        if peer_id not in self.peers:
            return False
        
        try:
            # Placeholder - would actually send over network
            self._network_metrics['messages_sent'] += 1
            return True
        except Exception as e:
            self.agent.logger.error(
                f"Error sending message to {peer_id}: {e}"
            )
            return False
    
    async def receive_message(self, message: Any) -> bool:
        """
        Receive message from network
        
        Args:
            message: Received message
            
        Returns:
            True if message was accepted
        """
        self.message_queue.append(message)
        self._network_metrics['messages_received'] += 1
        
        await self.agent.event_bus.publish(Event(
            type=EventType.MESSAGE_RECEIVED,
            source=self.name,
            data={'message': message},
            priority=2
        ))
        
        return True
    
    async def connect_peer(self, peer: Any) -> bool:
        """
        Connect to peer
        
        Args:
            peer: Peer object
            
        Returns:
            True if connection successful
        """
        peer_id = getattr(peer, 'id', str(peer))
        self.peers[peer_id] = peer
        self._network_metrics['peers_connected'] = len(self.peers)
        
        self.agent.logger.info(f"Connected to peer: {peer_id}")
        
        await self.agent.event_bus.publish(Event(
            type=EventType.PEER_CONNECTED,
            source=self.name,
            data={'peer_id': peer_id},
            priority=3
        ))
        
        return True
    
    async def disconnect_peer(self, peer_id: str) -> bool:
        """
        Disconnect from peer
        
        Args:
            peer_id: Peer ID to disconnect
            
        Returns:
            True if disconnection successful
        """
        if peer_id in self.peers:
            del self.peers[peer_id]
            self._network_metrics['peers_connected'] = len(self.peers)
            
            await self.agent.event_bus.publish(Event(
                type=EventType.PEER_DISCONNECTED,
                source=self.name,
                data={'peer_id': peer_id},
                priority=3
            ))
            
            return True
        return False
    
    async def broadcast_validated_transaction(self, event: Event) -> None:
        """Broadcast validated transaction"""
        transaction = event.data.get('transaction')
        if transaction:
            await self.broadcast(transaction)
    
    async def broadcast_validated_block(self, event: Event) -> None:
        """Broadcast validated block"""
        block = event.data.get('block')
        if block:
            await self.broadcast(block)
    
    async def broadcast_vote(self, event: Event) -> None:
        """Broadcast vote"""
        await self.broadcast(event.data)
    
    async def discover_peers(self) -> List[Any]:
        """Discover new peers"""
        # Placeholder for peer discovery
        return list(self.peers.values())
    
    async def execute(self) -> None:
        """Main communicator loop"""
        # Process message queue
        while self.message_queue:
            message = self.message_queue.popleft()
            # Handle received messages
            pass
    
    async def finalize(self) -> None:
        """Cleanup communicator resources"""
        await super().finalize()
        self.agent.logger.info(
            f"{self.name} finalized. Metrics: {self._network_metrics}"
        )
