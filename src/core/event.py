"""Event system for SYNTHOS Agents"""

from enum import Enum
from typing import Callable, Dict, List, Any
from dataclasses import dataclass, field
from datetime import datetime
import asyncio
from collections import defaultdict


class EventType(Enum):
    """All event types in the system"""
    # Transaction events
    TRANSACTION_SUBMITTED = "transaction.submitted"
    TRANSACTION_VALIDATED = "transaction.validated"
    TRANSACTION_REJECTED = "transaction.rejected"
    TRANSACTION_FINALIZED = "transaction.finalized"
    
    # Block events
    BLOCK_PROPOSED = "block.proposed"
    BLOCK_VALIDATED = "block.validated"
    BLOCK_FINALIZED = "block.finalized"
    
    # Consensus events
    CONSENSUS_ROUND_START = "consensus.round.start"
    CONSENSUS_VOTE = "consensus.vote"
    CONSENSUS_FINALITY = "consensus.finality"
    
    # Governance events
    PROPOSAL_SUBMITTED = "proposal.submitted"
    PROPOSAL_VOTED = "proposal.voted"
    PROPOSAL_EXECUTED = "proposal.executed"
    
    # Network events
    PEER_CONNECTED = "peer.connected"
    PEER_DISCONNECTED = "peer.disconnected"
    MESSAGE_RECEIVED = "message.received"
    
    # Monitoring events
    ANOMALY_DETECTED = "anomaly.detected"
    SLASHING_TRIGGERED = "slashing.triggered"
    REWARD_DISTRIBUTED = "reward.distributed"


@dataclass
class Event:
    """Event object"""
    type: EventType
    source: str  # Role name
    data: Dict[str, Any]
    timestamp: float = field(default_factory=lambda: datetime.now().timestamp())
    priority: int = 1  # 0 (highest) to 10 (lowest)
    event_id: str = field(default_factory=lambda: str(datetime.now().timestamp()))
    
    def __lt__(self, other):
        """Enable priority queue ordering (lower priority value = higher priority)"""
        if self.priority != other.priority:
            return self.priority < other.priority
        return self.timestamp < other.timestamp


class EventBus:
    """Central event bus for agent communication"""
    
    def __init__(self, agent):
        self.agent = agent
        self.subscribers: Dict[EventType, List[Callable]] = defaultdict(list)
        self.event_queue: asyncio.Queue = asyncio.Queue()
        self.event_history: List[Event] = []
        self.max_history = 10000
        self._processing = False
    
    def subscribe(self, event_type: EventType, handler: Callable) -> None:
        """
        Subscribe to event type
        
        Args:
            event_type: EventType to subscribe to
            handler: Async callable to handle events
        """
        self.subscribers[event_type].append(handler)
        self.agent.logger.debug(
            f"Subscribed {handler.__name__} to {event_type.value}",
            extra={'event_type': event_type.value, 'handler': handler.__name__}
        )
    
    def unsubscribe(self, event_type: EventType, handler: Callable) -> None:
        """Unsubscribe from event type"""
        if event_type in self.subscribers and handler in self.subscribers[event_type]:
            self.subscribers[event_type].remove(handler)
    
    async def publish(self, event: Event) -> None:
        """
        Publish event to bus

        When the background processor is running (i.e. ``process_events`` has
        been started), the event is queued for ordered delivery.  Otherwise it
        is dispatched and recorded immediately, which makes unit tests that
        never call ``start()`` work without a running event loop task.

        Args:
            event: Event to publish
        """
        if self._processing:
            await self.event_queue.put(event)
        else:
            self._record_event(event)
            await self._dispatch_event(event)
    
    async def process_events(self) -> None:
        """Process queued events"""
        self._processing = True
        
        while self._processing:
            try:
                # Get next event with timeout
                event = await asyncio.wait_for(
                    self.event_queue.get(),
                    timeout=1.0
                )
                
                # Record event
                self._record_event(event)
                
                # Dispatch to subscribers
                await self._dispatch_event(event)
                
            except asyncio.TimeoutError:
                # No events, continue
                continue
            except Exception as e:
                self.agent.logger.error(
                    f"Error processing event: {e}",
                    extra={'error': str(e)}
                )
    
    async def _dispatch_event(self, event: Event) -> None:
        """Dispatch event to all subscribers"""
        handlers = self.subscribers.get(event.type, [])
        
        for handler in handlers:
            try:
                if asyncio.iscoroutinefunction(handler):
                    await handler(event)
                else:
                    handler(event)
            except Exception as e:
                self.agent.logger.error(
                    f"Error in event handler {handler.__name__}: {e}",
                    extra={
                        'handler': handler.__name__,
                        'event_type': event.type.value,
                        'error': str(e)
                    }
                )
    
    def _record_event(self, event: Event) -> None:
        """Record event in history"""
        self.event_history.append(event)
        
        # Maintain max history size
        if len(self.event_history) > self.max_history:
            self.event_history = self.event_history[-self.max_history:]
    
    def get_event_history(self, event_type: EventType = None, 
                         limit: int = 100) -> List[Event]:
        """Get event history"""
        if event_type:
            events = [e for e in self.event_history if e.type == event_type]
        else:
            events = self.event_history
        
        return events[-limit:]
    
    def stop_processing(self) -> None:
        """Stop event processing"""
        self._processing = False
    
    async def get_batch(self, size: int, timeout: float = 1.0) -> List[Event]:
        """
        Get batch of events
        
        Useful for batch processing
        """
        events = []
        start_time = datetime.now().timestamp()
        
        while len(events) < size:
            elapsed = datetime.now().timestamp() - start_time
            if elapsed > timeout:
                break
            
            try:
                remaining_timeout = timeout - elapsed
                event = await asyncio.wait_for(
                    self.event_queue.get(),
                    timeout=remaining_timeout
                )
                events.append(event)
            except asyncio.TimeoutError:
                break
        
        return events
