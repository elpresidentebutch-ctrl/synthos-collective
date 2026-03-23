"""Economist Role Implementation"""

from src.core.base_role import Role
from src.core.event import Event, EventType
from typing import Any, Dict


class EconomistRole(Role):
    """
    Economist Role - Manages incentives and resource allocation
    
    Responsibilities:
    - Calculate transaction fees
    - Distribute rewards
    - Manage token economics
    - Monitor economic metrics
    """
    
    def __init__(self, agent):
        super().__init__(agent)
        self.name = "Economist"
        self.version = "1.0.0"
        self.base_fee = 1
        self.reward_rate = 0.05
        self._economic_metrics = {
            'total_fees_collected': 0,
            'total_rewards_distributed': 0,
            'current_price': 1.0,
        }
    
    async def initialize(self) -> None:
        """Initialize economist"""
        await super().initialize()
        
        self.agent.event_bus.subscribe(
            EventType.TRANSACTION_VALIDATED,
            self.handle_transaction_validated
        )
        self.agent.event_bus.subscribe(
            EventType.BLOCK_FINALIZED,
            self.handle_block_finalized
        )
        
        self.agent.logger.info(f"{self.name} role initialized")
    
    async def handle_transaction_validated(self, event: Event) -> None:
        """Handle validated transaction"""
        transaction = event.data.get('transaction')
        if transaction:
            # Record fee
            fee = getattr(transaction, 'fee', 0)
            self._economic_metrics['total_fees_collected'] += fee
    
    async def handle_block_finalized(self, event: Event) -> None:
        """Handle block finalization"""
        block = event.data.get('block')
        if block:
            # Calculate and distribute rewards
            reward = await self.calculate_block_reward(block)
            await self.distribute_reward(block.proposer, reward)
    
    async def calculate_fee(self, transaction: Any) -> int:
        """
        Calculate transaction fee
        
        Args:
            transaction: Transaction object
            
        Returns:
            Calculated fee amount
        """
        base_fee = self.base_fee
        
        # Multiply by data size (in bytes)
        if hasattr(transaction, 'data'):
            size_multiplier = len(transaction.data) / 1000  # Scale per KB
            base_fee = int(base_fee * (1 + size_multiplier))
        
        return base_fee
    
    async def calculate_block_reward(self, block: Any) -> int:
        """
        Calculate reward for block proposal
        
        Args:
            block: Block object
            
        Returns:
            Reward amount
        """
        # Base reward
        base_reward = 100
        
        # Bonus for transaction count
        tx_count = len(getattr(block, 'transactions', []))
        tx_bonus = tx_count * 0.1
        
        total_reward = int(base_reward + tx_bonus)
        return total_reward
    
    async def distribute_reward(self, address: str, amount: int) -> None:
        """
        Distribute reward to address
        
        Args:
            address: Recipient address
            amount: Reward amount
        """
        current_balance = await self.agent.state.get_balance(address)
        new_balance = current_balance + amount
        await self.agent.state.set_balance(address, new_balance)
        
        self._economic_metrics['total_rewards_distributed'] += amount
        
        await self.agent.event_bus.publish(Event(
            type=EventType.REWARD_DISTRIBUTED,
            source=self.name,
            data={'address': address, 'amount': amount},
            priority=3
        ))
    
    async def adjust_parameters(self, metrics: Dict) -> None:
        """Adjust economic parameters based on metrics"""
        # Placeholder for dynamic fee adjustment
        pass
    
    async def execute(self) -> None:
        """Main economist loop"""
        pass
    
    async def finalize(self) -> None:
        """Cleanup economist resources"""
        await super().finalize()
        self.agent.logger.info(
            f"{self.name} finalized. Metrics: {self._economic_metrics}"
        )
