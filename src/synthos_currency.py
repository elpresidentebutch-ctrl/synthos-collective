"""
SYNTHOS Currency System - 100 Billion Coins
Original cryptocurrency implementation for SYNTHOS Collective
"""

from dataclasses import dataclass
from typing import Dict, List, Optional, Tuple
from enum import Enum
import hashlib
from datetime import datetime


class CoinType(Enum):
    """SYNTHOS coin types"""
    STANDARD = "STANDARD"  # Standard SYNTHOS token
    STAKING = "STAKING"    # Staking reward token
    GOVERNANCE = "GOVERNANCE"  # Governance token
    VALIDATOR = "VALIDATOR"  # Validator reward token


@dataclass
class CoinSpecification:
    """SYNTHOS coin specifications"""
    name: str = "SYNTHOS"
    symbol: str = "SYNTHOS"
    total_supply: int = 100_000_000_000  # 100 billion
    initial_supply: int = 50_000_000_000  # 50 billion initial
    decimals: int = 18
    burn_address: str = "0x0000000000000000000000000000000000000000"
    block_reward: int = 10 * (10 ** 18)  # 10 SYNTHOS per block
    mining_start_block: int = 0
    halving_interval: int = 2_100_000  # Halving every 2.1M blocks


class TokenDistribution(Enum):
    """Token distribution categories"""
    INITIAL_TEAM = 10_000_000_000      # 10 billion (10%)
    COMMUNITY_REWARDS = 30_000_000_000  # 30 billion (30%)
    STAKING_REWARDS = 25_000_000_000    # 25 billion (25%)
    ECOSYSTEM_GRANTS = 15_000_000_000   # 15 billion (15%)
    FOUNDATION = 10_000_000_000         # 10 billion (10%)
    FUTURE_RESERVE = 10_000_000_000     # 10 billion (10%)
    
    @classmethod
    def total(cls) -> int:
        """Get total distributed coins"""
        return sum(item.value for item in cls)


@dataclass
class CoinHolder:
    """Coin holder information"""
    address: str
    balance: int  # in wei (10^-18 SYNTHOS)
    coin_type: CoinType = CoinType.STANDARD
    locked_until: Optional[int] = None
    vesting_schedule: List[Tuple[int, int]] = None  # (unlock_height, amount)
    
    def readable_balance(self) -> float:
        """Get balance in SYNTHOS (not wei)"""
        return self.balance / (10 ** 18)
    
    def is_locked(self, block_height: int) -> bool:
        """Check if coins are locked"""
        if self.locked_until is None:
            return False
        return block_height < self.locked_until
    
    def get_unlocked_amount(self, block_height: int) -> int:
        """Get unlocked coins at block height"""
        if not self.vesting_schedule:
            return self.balance if not self.is_locked(block_height) else 0
        
        unlocked = 0
        for unlock_height, amount in self.vesting_schedule:
            if block_height >= unlock_height:
                unlocked += amount
        
        return min(unlocked, self.balance)


class SynthosCurrency:
    """Main SYNTHOS currency system"""
    
    def __init__(self):
        self.spec = CoinSpecification()
        self.holders: Dict[str, CoinHolder] = {}  # address -> CoinHolder
        self.total_burned = 0
        self.total_circulating = 0
        self.block_height = 0
        self.transaction_history: List[Dict] = []
        self.minting_history: List[Dict] = []
        self.burn_history: List[Dict] = []
        
        # Initialize distribution
        self._initialize_distribution()
    
    def _initialize_distribution(self) -> None:
        """Initialize 100 billion SYNTHOS coins"""
        distributions = {
            "team": (TokenDistribution.INITIAL_TEAM.value, "0x1111111111111111111111111111111111111111"),
            "community": (TokenDistribution.COMMUNITY_REWARDS.value, "0x2222222222222222222222222222222222222222"),
            "staking": (TokenDistribution.STAKING_REWARDS.value, "0x3333333333333333333333333333333333333333"),
            "ecosystem": (TokenDistribution.ECOSYSTEM_GRANTS.value, "0x4444444444444444444444444444444444444444"),
            "foundation": (TokenDistribution.FOUNDATION.value, "0x5555555555555555555555555555555555555555"),
            "reserve": (TokenDistribution.FUTURE_RESERVE.value, "0x6666666666666666666666666666666666666666"),
        }
        
        for category, (amount, address) in distributions.items():
            amount_wei = amount * (10 ** self.spec.decimals)
            self.holders[address] = CoinHolder(
                address=address,
                balance=amount_wei,
                coin_type=CoinType.STANDARD
            )
            self.total_circulating += amount_wei
    
    def mint_coins(self, recipient: str, amount: int, reason: str = "block_reward") -> Tuple[bool, str]:
        """
        Mint new coins
        
        Args:
            recipient: Address to receive coins
            amount: Amount in wei
            reason: Reason for minting
            
        Returns:
            Tuple of (success, message)
        """
        # Check total supply limit
        if self.total_circulating + amount > self.spec.total_supply * (10 ** self.spec.decimals):
            return False, "Max supply exceeded"
        
        if recipient not in self.holders:
            self.holders[recipient] = CoinHolder(
                address=recipient,
                balance=0,
                coin_type=CoinType.STANDARD
            )
        
        self.holders[recipient].balance += amount
        self.total_circulating += amount
        
        self.minting_history.append({
            'timestamp': datetime.now().isoformat(),
            'block': self.block_height,
            'recipient': recipient,
            'amount': amount,
            'reason': reason,
            'circulating': self.total_circulating
        })
        
        return True, f"Minted {amount / (10 ** 18):.2f} SYNTHOS to {recipient}"
    
    def burn_coins(self, from_addr: str, amount: int, reason: str = "burn") -> Tuple[bool, str]:
        """
        Burn coins
        
        Args:
            from_addr: Address to burn from
            amount: Amount in wei
            reason: Reason for burn
            
        Returns:
            Tuple of (success, message)
        """
        if from_addr not in self.holders:
            return False, "Address not found"
        
        if self.holders[from_addr].balance < amount:
            return False, "Insufficient balance"
        
        self.holders[from_addr].balance -= amount
        self.total_burned += amount
        self.total_circulating -= amount
        
        self.burn_history.append({
            'timestamp': datetime.now().isoformat(),
            'block': self.block_height,
            'address': from_addr,
            'amount': amount,
            'reason': reason,
            'circulating': self.total_circulating
        })
        
        return True, f"Burned {amount / (10 ** 18):.2f} SYNTHOS"
    
    def transfer(self, from_addr: str, to_addr: str, amount: int) -> Tuple[bool, str]:
        """
        Transfer coins
        
        Args:
            from_addr: Sender address
            to_addr: Receiver address
            amount: Amount in wei
            
        Returns:
            Tuple of (success, message)
        """
        if from_addr not in self.holders:
            return False, "Sender not found"
        
        if self.holders[from_addr].balance < amount:
            return False, "Insufficient balance"
        
        # Deduct from sender
        self.holders[from_addr].balance -= amount
        
        # Add to receiver
        if to_addr not in self.holders:
            self.holders[to_addr] = CoinHolder(
                address=to_addr,
                balance=0,
                coin_type=CoinType.STANDARD
            )
        
        self.holders[to_addr].balance += amount
        
        self.transaction_history.append({
            'timestamp': datetime.now().isoformat(),
            'block': self.block_height,
            'from': from_addr,
            'to': to_addr,
            'amount': amount,
            'status': 'confirmed'
        })
        
        return True, f"Transferred {amount / (10 ** 18):.2f} SYNTHOS from {from_addr} to {to_addr}"
    
    def get_balance(self, address: str) -> int:
        """Get balance in wei"""
        if address not in self.holders:
            return 0
        return self.holders[address].balance
    
    def get_balance_readable(self, address: str) -> float:
        """Get balance in SYNTHOS"""
        return self.get_balance(address) / (10 ** self.spec.decimals)
    
    def get_total_balance(self) -> float:
        """Get total balance in SYNTHOS"""
        return self.total_circulating / (10 ** self.spec.decimals)
    
    def add_holder(self, address: str, amount: int, coin_type: CoinType = CoinType.STANDARD,
                   locked_until: Optional[int] = None) -> None:
        """Add new coin holder"""
        self.holders[address] = CoinHolder(
            address=address,
            balance=amount * (10 ** self.spec.decimals),
            coin_type=coin_type,
            locked_until=locked_until
        )
    
    def lock_coins(self, address: str, unlock_block: int) -> Tuple[bool, str]:
        """Lock coins until specific block"""
        if address not in self.holders:
            return False, "Address not found"
        
        self.holders[address].locked_until = unlock_block
        return True, f"Coins locked until block {unlock_block}"
    
    def add_vesting_schedule(self, address: str, schedule: List[Tuple[int, int]]) -> Tuple[bool, str]:
        """
        Add vesting schedule
        
        Args:
            address: Recipient address
            schedule: List of (unlock_block, amount_wei) tuples
            
        Returns:
            Tuple of (success, message)
        """
        if address not in self.holders:
            return False, "Address not found"
        
        self.holders[address].vesting_schedule = schedule
        return True, f"Vesting schedule added for {address}"
    
    def get_statistics(self) -> Dict:
        """Get currency statistics"""
        return {
            'total_supply': self.spec.total_supply,
            'circulating_supply': self.get_total_balance(),
            'burned_supply': self.total_burned / (10 ** self.spec.decimals),
            'locked_supply': sum(
                h.balance for h in self.holders.values()
                if h.is_locked(self.block_height)
            ) / (10 ** self.spec.decimals),
            'total_holders': len(self.holders),
            'block_height': self.block_height,
            'total_transactions': len(self.transaction_history),
            'total_mints': len(self.minting_history),
            'total_burns': len(self.burn_history),
        }
    
    def advance_block(self, block_reward_recipient: Optional[str] = None) -> None:
        """Advance to next block"""
        self.block_height += 1
        
        # Mint block reward
        if block_reward_recipient:
            reward = self.spec.block_reward
            
            # Apply halving
            halvings = self.block_height // self.spec.halving_interval
            for _ in range(halvings):
                reward //= 2
            
            if reward > 0:
                self.mint_coins(block_reward_recipient, reward, "block_reward")
    
    def get_top_holders(self, limit: int = 10) -> List[Tuple[str, float]]:
        """Get top coin holders"""
        holders = sorted(
            self.holders.items(),
            key=lambda x: x[1].balance,
            reverse=True
        )
        
        return [
            (addr, holder.readable_balance())
            for addr, holder in holders[:limit]
        ]
    
    def get_circulation_breakdown(self) -> Dict[CoinType, float]:
        """Get circulation by coin type"""
        breakdown = {}
        for coin_type in CoinType:
            total = sum(
                h.balance for h in self.holders.values()
                if h.coin_type == coin_type
            ) / (10 ** self.spec.decimals)
            breakdown[coin_type] = total
        
        return breakdown


class RewardSystem:
    """SYNTHOS reward distribution system"""
    
    def __init__(self, currency: SynthosCurrency):
        self.currency = currency
        self.staking_rewards: List[Dict] = []
        self.validator_rewards: List[Dict] = []
        self.ecosystem_rewards: List[Dict] = []
    
    def distribute_staking_rewards(self, staker: str, amount: int) -> Tuple[bool, str]:
        """Distribute staking rewards"""
        success, msg = self.currency.mint_coins(staker, amount, "staking_reward")
        
        if success:
            self.staking_rewards.append({
                'timestamp': datetime.now().isoformat(),
                'staker': staker,
                'amount': amount / (10 ** 18),
                'block': self.currency.block_height
            })
        
        return success, msg
    
    def distribute_validator_rewards(self, validator: str, amount: int) -> Tuple[bool, str]:
        """Distribute validator rewards"""
        success, msg = self.currency.mint_coins(validator, amount, "validator_reward")
        
        if success:
            self.validator_rewards.append({
                'timestamp': datetime.now().isoformat(),
                'validator': validator,
                'amount': amount / (10 ** 18),
                'block': self.currency.block_height
            })
        
        return success, msg
    
    def distribute_ecosystem_grants(self, recipient: str, amount: int, grant_type: str) -> Tuple[bool, str]:
        """Distribute ecosystem grants"""
        success, msg = self.currency.mint_coins(recipient, amount, f"ecosystem_grant_{grant_type}")
        
        if success:
            self.ecosystem_rewards.append({
                'timestamp': datetime.now().isoformat(),
                'recipient': recipient,
                'amount': amount / (10 ** 18),
                'type': grant_type,
                'block': self.currency.block_height
            })
        
        return success, msg


if __name__ == "__main__":
    print("SYNTHOS CURRENCY SYSTEM - 100 BILLION COINS")
    print("=" * 60)
    
    # Initialize currency
    currency = SynthosCurrency()
    print(f"✓ Initialized with {currency.get_total_balance():,.0f} SYNTHOS coins")
    
    # Get initial distribution
    print("\nInitial Distribution:")
    print("-" * 60)
    distributions = {
        "Team": "0x1111111111111111111111111111111111111111",
        "Community": "0x2222222222222222222222222222222222222222",
        "Staking": "0x3333333333333333333333333333333333333333",
        "Ecosystem": "0x4444444444444444444444444444444444444444",
        "Foundation": "0x5555555555555555555555555555555555555555",
        "Reserve": "0x6666666666666666666666666666666666666666",
    }
    
    for name, addr in distributions.items():
        balance = currency.get_balance_readable(addr)
        print(f"  {name:15} {balance:>20,.0f} SYNTHOS")
    
    # Show statistics
    print("\nCurrency Statistics:")
    print("-" * 60)
    stats = currency.get_statistics()
    print(f"  Total Supply:        {stats['total_supply']:>20,} tokens")
    print(f"  Circulating Supply:  {stats['circulating_supply']:>20,.0f} tokens")
    print(f"  Total Holders:       {stats['total_holders']:>20,}")
    print(f"  Block Height:        {stats['block_height']:>20,}")
    
    # Test transfer
    print("\nTransaction Test:")
    print("-" * 60)
    from_addr = "0x1111111111111111111111111111111111111111"
    to_addr = "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    transfer_amount = int(100 * (10 ** 18))  # 100 SYNTHOS in wei
    
    success, msg = currency.transfer(from_addr, to_addr, transfer_amount)
    print(f"  {msg}")
    print(f"  From balance: {currency.get_balance_readable(from_addr):,.0f} SYNTHOS")
    print(f"  To balance:   {currency.get_balance_readable(to_addr):,.0f} SYNTHOS")
    
    # Test minting
    print("\nMinting Test:")
    print("-" * 60)
    minter_addr = "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
    mint_amount = int(1_000 * (10 ** 18))  # 1000 SYNTHOS
    
    success, msg = currency.mint_coins(minter_addr, mint_amount, "block_reward")
    print(f"  {msg}")
    print(f"  Balance: {currency.get_balance_readable(minter_addr):,.0f} SYNTHOS")
    
    # Test burning
    print("\nBurning Test:")
    print("-" * 60)
    burn_amount = int(50 * (10 ** 18))  # 50 SYNTHOS
    success, msg = currency.burn_coins(minter_addr, burn_amount, "governance_fee")
    print(f"  {msg}")
    print(f"  Remaining balance: {currency.get_balance_readable(minter_addr):,.0f} SYNTHOS")
    print(f"  Total burned: {currency.total_burned / (10 ** 18):,.0f} SYNTHOS")
    
    # Reward system test
    print("\nReward Distribution Test:")
    print("-" * 60)
    rewards = RewardSystem(currency)
    
    staker = "0xcccccccccccccccccccccccccccccccccccccccc"
    staking_reward = int(10 * (10 ** 18))  # 10 SYNTHOS
    success, msg = rewards.distribute_staking_rewards(staker, staking_reward)
    print(f"  {msg}")
    
    validator = "0xdddddddddddddddddddddddddddddddddddddddd"
    validator_reward = int(5 * (10 ** 18))  # 5 SYNTHOS
    success, msg = rewards.distribute_validator_rewards(validator, validator_reward)
    print(f"  {msg}")
    
    # Top holders
    print("\nTop Coin Holders:")
    print("-" * 60)
    top_holders = currency.get_top_holders(5)
    for i, (addr, balance) in enumerate(top_holders, 1):
        print(f"  {i}. {addr}: {balance:>20,.0f} SYNTHOS")
