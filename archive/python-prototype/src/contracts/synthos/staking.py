"""
SYNTHOS Staking Contract
Validator staking, delegation, and reward distribution
"""

from dataclasses import dataclass, field
from typing import Dict, Optional, List, Tuple
from enum import Enum
from datetime import datetime, timedelta
import math


class StakeStatus(Enum):
    """Stake status"""
    ACTIVE = "ACTIVE"
    UNBONDING = "UNBONDING"
    UNSTAKED = "UNSTAKED"
    SLASHED = "SLASHED"


@dataclass
class Stake:
    """Stake record"""
    staker: str
    validator: str
    amount: int
    timestamp: datetime
    status: StakeStatus = StakeStatus.ACTIVE
    
    # Rewards
    accumulated_rewards: int = 0
    last_reward_block: int = 0
    
    # Unbonding
    unbonding_timestamp: Optional[datetime] = None
    unbonding_blocks_remaining: int = 0


@dataclass
class Validator:
    """Validator record"""
    address: str
    name: str
    self_stake: int
    delegated_stake: int = 0
    total_stake: int = field(init=False)
    
    # Status
    active: bool = True
    commission_rate: int = 100  # Basis points (100 = 1%)
    
    # Performance
    blocks_proposed: int = 0
    blocks_missed: int = 0
    slashes: int = 0
    total_rewards: int = 0
    
    # Metadata
    created_timestamp: datetime = field(default_factory=datetime.now)
    website: Optional[str] = None
    description: Optional[str] = None
    
    def __post_init__(self):
        self.total_stake = self.self_stake + self.delegated_stake


@dataclass
class SlashingEvent:
    """Slashing event record"""
    validator: str
    amount: int
    reason: str
    timestamp: datetime
    block_height: int
    affected_delegators: List[str] = field(default_factory=list)


class SynthosStakingContract:
    """
    SYNTHOS Staking Contract
    - Validator registration and staking
    - Delegation system
    - Reward distribution
    - Slashing for misbehavior
    - Unbonding period
    """

    def __init__(self, owner: str, token_contract, min_validator_stake: int = 10**18):
        """Initialize staking contract"""
        self.owner = owner
        self.token_contract = token_contract
        
        # Parameters
        self.min_validator_stake = min_validator_stake
        self.min_delegation = 10**15  # 0.001 SYN
        self.unbonding_period_blocks = 2016  # 1 week (7 days * 7200 blocks/day)
        self.max_validators = 100
        self.inflation_rate = 50  # 50 basis points (0.5% annually)
        
        # Validator management
        self.validators: Dict[str, Validator] = {}
        self.validator_count = 0
        
        # Staking storage
        self.stakes: Dict[str, List[Stake]] = {}  # delegator -> [stakes]
        self.validator_delegators: Dict[str, List[str]] = {}  # validator -> [delegators]
        
        # Rewards
        self.reward_pool: int = 0
        self.total_rewards_distributed: int = 0
        self.reward_per_block: int = 10**15  # Rewards per block
        
        # Slashing
        self.slashing_events: List[SlashingEvent] = []
        self.slashing_cooldown_blocks = 1000
        self.last_slash_block: Dict[str, int] = {}
        
        # Epoch tracking
        self.current_epoch = 0
        self.current_block = 0
        self.epoch_length_blocks = 100


    def register_validator(self, validator_addr: str, name: str, 
                          stake_amount: int, commission_rate: int = 100,
                          website: Optional[str] = None,
                          description: Optional[str] = None) -> Tuple[bool, str]:
        """
        Register new validator with self-stake
        Returns: (success, message)
        """
        if validator_addr in self.validators:
            return False, f"Validator {validator_addr} already registered"
        
        if len(self.validators) >= self.max_validators:
            return False, f"Maximum validators ({self.max_validators}) reached"
        
        if stake_amount < self.min_validator_stake:
            return False, f"Insufficient self-stake. Have: {stake_amount}, Need: {self.min_validator_stake}"
        
        if commission_rate < 0 or commission_rate > 10000:
            return False, "Commission rate must be between 0 and 10000 basis points"
        
        # Check token balance
        balance = self.token_contract.balance_of(validator_addr)
        if balance < stake_amount:
            return False, f"Insufficient token balance. Have: {balance}, Need: {stake_amount}"
        
        # Lock tokens in staking contract
        success, msg = self.token_contract.transfer(validator_addr, self.owner, stake_amount, reason="validator_stake")
        if not success:
            return False, f"Token transfer failed: {msg}"
        
        # Create validator
        validator = Validator(
            address=validator_addr,
            name=name,
            self_stake=stake_amount,
            commission_rate=commission_rate,
            website=website,
            description=description
        )
        
        self.validators[validator_addr] = validator
        self.validator_delegators[validator_addr] = []
        self.validator_count += 1
        
        # Record self-stake
        stake = Stake(
            staker=validator_addr,
            validator=validator_addr,
            amount=stake_amount,
            timestamp=datetime.now()
        )
        
        if validator_addr not in self.stakes:
            self.stakes[validator_addr] = []
        self.stakes[validator_addr].append(stake)
        
        return True, f"Registered validator {validator_addr} with {stake_amount} stake"


    def delegate(self, delegator: str, validator_addr: str, amount: int) -> Tuple[bool, str]:
        """
        Delegate tokens to validator
        Returns: (success, message)
        """
        if validator_addr not in self.validators:
            return False, f"Validator {validator_addr} not found"
        
        if amount < self.min_delegation:
            return False, f"Delegation too small. Have: {amount}, Need: {self.min_delegation}"
        
        validator = self.validators[validator_addr]
        
        if not validator.active:
            return False, "Validator is not active"
        
        # Check token balance
        balance = self.token_contract.balance_of(delegator)
        if balance < amount:
            return False, f"Insufficient balance. Have: {balance}, Need: {amount}"
        
        # Lock tokens
        success, msg = self.token_contract.transfer(delegator, self.owner, amount, reason="delegation")
        if not success:
            return False, f"Token transfer failed: {msg}"
        
        # Record delegation
        stake = Stake(
            staker=delegator,
            validator=validator_addr,
            amount=amount,
            timestamp=datetime.now()
        )
        
        if delegator not in self.stakes:
            self.stakes[delegator] = []
        
        self.stakes[delegator].append(stake)
        
        # Add to validator's delegator list
        if delegator not in self.validator_delegators[validator_addr]:
            self.validator_delegators[validator_addr].append(delegator)
        
        # Update validator
        validator.delegated_stake += amount
        validator.total_stake = validator.self_stake + validator.delegated_stake
        
        return True, f"Delegated {amount} tokens to {validator_addr}"


    def undelegate(self, delegator: str, validator_addr: str, stake_index: int) -> Tuple[bool, str]:
        """
        Start unbonding delegation (requires waiting period)
        Returns: (success, message)
        """
        if delegator not in self.stakes or not self.stakes[delegator]:
            return False, "No stakes found for delegator"
        
        if stake_index >= len(self.stakes[delegator]):
            return False, "Stake index out of range"
        
        stake = self.stakes[delegator][stake_index]
        
        if stake.status != StakeStatus.ACTIVE:
            return False, f"Stake status is {stake.status}, not ACTIVE"
        
        if stake.validator != validator_addr:
            return False, "Stake not delegated to specified validator"
        
        # Start unbonding
        stake.status = StakeStatus.UNBONDING
        stake.unbonding_timestamp = datetime.now()
        stake.unbonding_blocks_remaining = self.unbonding_period_blocks
        
        # Update validator
        validator = self.validators[stake.validator]
        validator.delegated_stake -= stake.amount
        validator.total_stake = validator.self_stake + validator.delegated_stake
        
        return True, f"Unbonding {stake.amount} tokens (unbonds in {self.unbonding_period_blocks} blocks)"


    def claim_unstaked(self, delegator: str, stake_index: int) -> Tuple[bool, str]:
        """
        Claim unstaked tokens after unbonding period
        Returns: (success, message)
        """
        if delegator not in self.stakes or not self.stakes[delegator]:
            return False, "No stakes found for delegator"
        
        if stake_index >= len(self.stakes[delegator]):
            return False, "Stake index out of range"
        
        stake = self.stakes[delegator][stake_index]
        
        if stake.status != StakeStatus.UNBONDING:
            return False, f"Stake status is {stake.status}, not UNBONDING"
        
        if stake.unbonding_blocks_remaining > 0:
            return False, f"Unbonding period not complete. Blocks remaining: {stake.unbonding_blocks_remaining}"
        
        # Calculate rewards
        rewards = stake.accumulated_rewards
        total_return = stake.amount + rewards
        
        # Transfer tokens back
        success, msg = self.token_contract.transfer(self.owner, delegator, total_return, reason="unstake_claim")
        if not success:
            return False, f"Token transfer failed: {msg}"
        
        # Mark as unstaked
        stake.status = StakeStatus.UNSTAKED
        
        return True, f"Claimed {total_return} tokens ({stake.amount} principal + {rewards} rewards)"


    def distribute_rewards(self, validator_addr: str, reward_amount: int, block_height: int) -> Tuple[bool, str]:
        """
        Distribute block rewards to validator and delegators
        Returns: (success, message)
        """
        if validator_addr not in self.validators:
            return False, f"Validator {validator_addr} not found"
        
        if reward_amount <= 0:
            return False, "Reward amount must be positive"
        
        validator = self.validators[validator_addr]
        commission_amount = (reward_amount * validator.commission_rate) // 10000
        remaining_reward = reward_amount - commission_amount
        
        # Validator reward (commission + share based on self-stake ratio)
        validator_share_ratio = validator.self_stake / max(validator.total_stake, 1)
        validator_share = int((remaining_reward * validator_share_ratio))
        
        # Update validator
        validator.total_rewards += commission_amount + validator_share
        
        # Find validator's self-stake
        for stake in self.stakes.get(validator_addr, []):
            if stake.validator == validator_addr and stake.status == StakeStatus.ACTIVE:
                stake.accumulated_rewards += commission_amount + validator_share
                stake.last_reward_block = block_height
                break
        
        # Distribute to delegators
        delegators = self.validator_delegators.get(validator_addr, [])
        if delegators and validator.total_stake > validator.self_stake:
            delegated_portion = reward_amount - commission_amount - validator_share
            
            for delegator in delegators:
                for stake in self.stakes.get(delegator, []):
                    if stake.validator == validator_addr and stake.status == StakeStatus.ACTIVE:
                        # Reward proportional to stake
                        share_ratio = stake.amount / (validator.total_stake - validator.self_stake)
                        delegator_reward = int(delegated_portion * share_ratio)
                        
                        stake.accumulated_rewards += delegator_reward
                        stake.last_reward_block = block_height
        
        self.total_rewards_distributed += reward_amount
        return True, f"Distributed {reward_amount} rewards to validator {validator_addr}"


    def slash_validator(self, validator_addr: str, slash_percentage: int, reason: str) -> Tuple[bool, str]:
        """
        Slash validator and delegators for misbehavior
        Returns: (success, message)
        """
        if validator_addr not in self.validators:
            return False, f"Validator {validator_addr} not found"
        
        # Check cooldown
        last_slash = self.last_slash_block.get(validator_addr, -10000)
        if self.current_block - last_slash < self.slashing_cooldown_blocks:
            return False, f"Slashing on cooldown. Next allowed at block {last_slash + self.slashing_cooldown_blocks}"
        
        validator = self.validators[validator_addr]
        total_slashed = (validator.total_stake * slash_percentage) // 100
        
        # Slash self-stake
        slashed_self = (validator.self_stake * slash_percentage) // 100
        validator.self_stake -= slashed_self
        
        # Slash delegated stakes
        delegators = self.validator_delegators.get(validator_addr, [])
        for delegator in delegators:
            for stake in self.stakes.get(delegator, []):
                if stake.validator == validator_addr and stake.status == StakeStatus.ACTIVE:
                    slashed_amount = (stake.amount * slash_percentage) // 100
                    stake.amount -= slashed_amount
        
        # Update validator
        validator.delegated_stake = max(0, validator.delegated_stake - (total_slashed - slashed_self))
        validator.total_stake = validator.self_stake + validator.delegated_stake
        validator.slashes += 1
        
        # Record slashing event
        event = SlashingEvent(
            validator=validator_addr,
            amount=total_slashed,
            reason=reason,
            timestamp=datetime.now(),
            block_height=self.current_block,
            affected_delegators=delegators.copy()
        )
        self.slashing_events.append(event)
        self.last_slash_block[validator_addr] = self.current_block
        
        return True, f"Slashed {total_slashed} tokens from validator {validator_addr}"


    def get_validator(self, validator_addr: str) -> Optional[Dict]:
        """Get validator details"""
        if validator_addr not in self.validators:
            return None
        
        v = self.validators[validator_addr]
        
        return {
            "address": v.address,
            "name": v.name,
            "active": v.active,
            "self_stake": v.self_stake,
            "delegated_stake": v.delegated_stake,
            "total_stake": v.total_stake,
            "commission_rate": v.commission_rate,
            "blocks_proposed": v.blocks_proposed,
            "blocks_missed": v.blocks_missed,
            "slashes": v.slashes,
            "total_rewards": v.total_rewards,
            "delegator_count": len(self.validator_delegators.get(validator_addr, [])),
            "website": v.website,
            "description": v.description,
            "created_timestamp": v.created_timestamp.isoformat(),
        }


    def get_all_validators(self, active_only: bool = True) -> List[Dict]:
        """Get all validators"""
        validators = []
        
        for addr in self.validators:
            if active_only and not self.validators[addr].active:
                continue
            
            validators.append(self.get_validator(addr))
        
        return sorted(validators, key=lambda x: x["total_stake"], reverse=True)


    def get_validator_ranking(self, top_n: int = 50) -> List[Dict]:
        """Get top validators by stake"""
        all_validators = self.get_all_validators(active_only=True)
        
        ranking = []
        for i, validator in enumerate(all_validators[:top_n]):
            ranking.append({
                "rank": i + 1,
                **validator
            })
        
        return ranking


    def advance_epoch(self):
        """Advance to next epoch (for testing)"""
        self.current_epoch += 1
        
        # Reduce unbonding period for active unbondings
        for delegator_stakes in self.stakes.values():
            for stake in delegator_stakes:
                if stake.status == StakeStatus.UNBONDING:
                    stake.unbonding_blocks_remaining = max(0, stake.unbonding_blocks_remaining - self.epoch_length_blocks)


    def get_staking_stats(self) -> Dict:
        """Get staking statistics"""
        total_staked = sum(v.total_stake for v in self.validators.values())
        active_validators = sum(1 for v in self.validators.values() if v.active)
        
        return {
            "total_validators": len(self.validators),
            "active_validators": active_validators,
            "total_staked": total_staked,
            "total_rewards_distributed": self.total_rewards_distributed,
            "reward_pool": self.reward_pool,
            "min_validator_stake": self.min_validator_stake,
            "unbonding_period_blocks": self.unbonding_period_blocks,
            "slashing_events": len(self.slashing_events),
            "current_epoch": self.current_epoch,
        }


    def set_reward_per_block(self, owner: str, new_reward: int) -> Tuple[bool, str]:
        """Update reward per block (only owner)"""
        if owner != self.owner:
            return False, "Only owner can update rewards"
        
        self.reward_per_block = new_reward
        return True, f"Updated reward per block to {new_reward}"


    def set_active(self, owner: str, validator_addr: str, active: bool) -> Tuple[bool, str]:
        """Enable/disable validator"""
        if owner != self.owner:
            return False, "Only owner can update validator status"
        
        if validator_addr not in self.validators:
            return False, f"Validator {validator_addr} not found"
        
        self.validators[validator_addr].active = active
        return True, f"Set validator {validator_addr} active={active}"
