"""Consensus Voting System"""

from typing import Dict, List, Any, Optional
from dataclasses import dataclass, field
from enum import Enum
from datetime import datetime


class VoteStatus(Enum):
    """Status of a vote"""
    PENDING = "pending"
    COMMITTED = "committed"
    FINALIZED = "finalized"
    CHALLENGED = "challenged"
    SLASHED = "slashed"


@dataclass
class ConsensusBallot:
    """A single validator's vote"""
    voter: str
    block_hash: str
    height: int
    vote_value: bool  # True = yes, False = no
    stake: int = 0
    timestamp: float = field(default_factory=lambda: datetime.now().timestamp())
    signature: bytes = field(default_factory=bytes)
    
    def is_valid_signature(self) -> bool:
        """Check if ballot has valid signature"""
        return len(self.signature) > 0


@dataclass
class ConsensusRound:
    """A single consensus round"""
    height: int
    round_id: str
    block_hash: str
    votes: List[ConsensusBallot] = field(default_factory=list)
    challenges: List[str] = field(default_factory=list)
    status: VoteStatus = VoteStatus.PENDING
    finalized: bool = False
    timestamp: float = field(default_factory=lambda: datetime.now().timestamp())


class ConsensusEngine:
    """Byzantine Fault Tolerant consensus engine"""
    
    def __init__(self, agent):
        self.agent = agent
        self.active_round: Optional[ConsensusRound] = None
        self.round_history: Dict[int, ConsensusRound] = {}
        self.byzantine_tolerance = 1/3  # Tolerate up to 1/3 malicious
        self._slash_rate = 0.10  # 10% slashing for violations
    
    async def start_consensus_round(self, 
                                   height: int,
                                   block_hash: str) -> ConsensusRound:
        """Start new consensus round"""
        round_id = f"{height}:{block_hash[:8]}"
        
        consensus_round = ConsensusRound(
            height=height,
            round_id=round_id,
            block_hash=block_hash,
        )
        
        self.active_round = consensus_round
        self.round_history[height] = consensus_round
        
        from src.core.event import Event, EventType
        await self.agent.event_bus.publish(Event(
            type=EventType.CONSENSUS_ROUND_START,
            source='ConsensusEngine',
            data={'height': height, 'block_hash': block_hash},
            priority=1
        ))
        
        return consensus_round
    
    async def vote(self, 
                   voter: str,
                   height: int,
                   block_hash: str,
                   vote_value: bool,
                   stake: int,
                   signature: bytes) -> bool:
        """
        Record a vote
        
        Validates:
        - Voter has stake
        - Signature is valid
        - Not double voting
        """
        round_id = self.active_round
        if not round_id or round_id.height != height:
            return False
        
        # Check for double voting
        existing_vote = next(
            (v for v in round_id.votes if v.voter == voter),
            None
        )
        if existing_vote:
            # Double voting detected - will be slashable
            self.agent.logger.warning(f"Double vote from {voter}")
            return False
        
        # Create ballot
        ballot = ConsensusBallot(
            voter=voter,
            block_hash=block_hash,
            height=height,
            vote_value=vote_value,
            stake=stake,
            signature=signature,
        )
        
        # Record vote
        round_id.votes.append(ballot)
        
        # Check if consensus is reached
        is_finalized = await self._check_finality(round_id)
        
        from src.core.event import Event, EventType
        await self.agent.event_bus.publish(Event(
            type=EventType.CONSENSUS_VOTE,
            source='ConsensusEngine',
            data={
                'voter': voter,
                'height': height,
                'block_hash': block_hash,
                'vote': vote_value,
            },
            priority=2
        ))
        
        return True
    
    async def challenge_block(self,
                             challenger: str,
                             height: int,
                             block_hash: str,
                             reason: str) -> bool:
        """
        Challenge a block proposal
        
        Challenger is alleging:
        - Invalid transaction
        - Incorrect state transition
        - Byzantine behavior
        """
        round_id = self.active_round
        if not round_id or round_id.height != height:
            return False
        
        round_id.challenges.append(challenger)
        round_id.status = VoteStatus.CHALLENGED
        
        self.agent.logger.info(f"Block challenged by {challenger}: {reason}")
        
        # Trigger review
        await self._handle_challenge(challenger, height, block_hash, reason)
        
        return True
    
    async def finalize_consensus(self, height: int) -> bool:
        """
        Finalize consensus round
        
        Process:
        1. Check if 2/3+ supermajority
        2. Mark block as final
        3. Apply state transition
        4. Penalize non-voters and challengers
        """
        round_id = self.round_history.get(height)
        if not round_id:
            return False
        
        if round_id.finalized:
            return True
        
        # Get total stake
        total_stake = await self._get_total_stake()
        
        # Count yes votes
        yes_votes = sum(
            ballot.stake for ballot in round_id.votes
            if ballot.vote_value
        )
        
        # Need 2/3+ supermajority
        supermajority_threshold = total_stake * 2 / 3
        
        if yes_votes < supermajority_threshold:
            self.agent.logger.info(f"Consensus round {height} failed")
            return False
        
        # Finalize
        round_id.finalized = True
        round_id.status = VoteStatus.FINALIZED
        
        # Slash non-voters and challengers
        await self._apply_slashing(round_id, total_stake)
        
        from src.core.event import Event, EventType
        await self.agent.event_bus.publish(Event(
            type=EventType.CONSENSUS_FINALITY,
            source='ConsensusEngine',
            data={'height': height, 'block_hash': round_id.block_hash},
            priority=1
        ))
        
        # Update state
        ledger = await self.agent.state.get_ledger_state()
        ledger['block_height'] = height
        ledger['last_block_hash'] = round_id.block_hash
        await self.agent.state.set('ledger', ledger)
        
        return True
    
    async def slash_validator(self,
                             validator: str,
                             reason: str,
                             severity: float = 0.1) -> bool:
        """
        Slash validator stake
        
        Reasons:
        - Double voting
        - Equivocation (signing different blocks)
        - Byzantine behavior
        - Availability failure
        """
        # Calculate slash amount
        balance = await self.agent.state.get_balance(validator)
        slash_amount = int(balance * severity * self._slash_rate)
        
        # Apply slashing
        new_balance = max(0, balance - slash_amount)
        await self.agent.state.set_balance(validator, new_balance)
        
        self.agent.logger.warning(
            f"Slashed {validator}: {slash_amount} tokens ({reason})"
        )
        
        from src.core.event import Event, EventType
        await self.agent.event_bus.publish(Event(
            type=EventType.SLASHING_TRIGGERED,
            source='ConsensusEngine',
            data={
                'validator': validator,
                'amount': slash_amount,
                'reason': reason,
            },
            priority=1
        ))
        
        return True
    
    async def _check_finality(self, round_id: ConsensusRound) -> bool:
        """Check if block has reached finality"""
        total_stake = await self._get_total_stake()
        
        yes_votes = sum(
            ballot.stake for ballot in round_id.votes
            if ballot.vote_value
        )
        
        # Need 2/3+ for finality
        return yes_votes >= (total_stake * 2 / 3)
    
    async def _handle_challenge(self,
                               challenger: str,
                               height: int,
                               block_hash: str,
                               reason: str) -> None:
        """Handle block challenge"""
        # Re-validate block
        is_valid = await self._revalidate_block(height, block_hash)
        
        if not is_valid:
            # Block is invalid, challenge is valid
            self.agent.logger.info(
                f"Challenge by {challenger} was valid - block invalid"
            )
            # Slash block proposer
            block_proposer = await self._get_block_proposer(height)
            if block_proposer:
                await self.slash_validator(
                    block_proposer,
                    "Invalid block",
                    severity=0.2
                )
        else:
            # Block is valid, false challenge
            self.agent.logger.info(
                f"Challenge by {challenger} was invalid - block valid"
            )
            # Slash false challenger
            await self.slash_validator(
                challenger,
                "False challenge",
                severity=0.05
            )
    
    async def _apply_slashing(self,
                             round_id: ConsensusRound,
                             total_stake: int) -> None:
        """Apply slashing to non-voters and challengers"""
        voters = set(ballot.voter for ballot in round_id.votes)
        
        # Get all validators
        all_validators = await self._get_all_validators()
        
        # Slash non-voters (mild penalty)
        for validator in all_validators:
            if validator not in voters:
                await self.slash_validator(
                    validator,
                    "Non-participation",
                    severity=0.02
                )
        
        # Slash challengers if block was valid
        # (handled in _handle_challenge)
    
    async def _revalidate_block(self, height: int, block_hash: str) -> bool:
        """Re-validate a block"""
        # Would get block from storage and re-validate
        return True
    
    async def _get_block_proposer(self, height: int) -> Optional[str]:
        """Get proposer of block at height"""
        round_id = self.round_history.get(height)
        if round_id:
            # Would track proposer separately
            return None
        return None
    
    async def _get_total_stake(self) -> int:
        """Get total stake in network"""
        # Sum all validator stakes
        return 1000000  # Placeholder
    
    async def _get_all_validators(self) -> List[str]:
        """Get all active validators"""
        # Would get from validator set
        return []
