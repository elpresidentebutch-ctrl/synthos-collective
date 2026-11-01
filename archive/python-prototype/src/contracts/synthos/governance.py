"""
SYNTHOS Governance Contract
DAO-integrated governance with voting power delegation and proposal execution
"""

from dataclasses import dataclass, field
from typing import Dict, Optional, List, Tuple, Callable
from enum import Enum
from datetime import datetime, timedelta
import hashlib
from uuid import uuid4


class ProposalState(Enum):
    """Proposal state lifecycle"""
    PENDING = "PENDING"
    ACTIVE = "ACTIVE"
    CANCELLED = "CANCELLED"
    DEFEATED = "DEFEATED"
    SUCCEEDED = "SUCCEEDED"
    QUEUED = "QUEUED"
    EXPIRED = "EXPIRED"
    EXECUTED = "EXECUTED"
    FAILED = "FAILED"


class VoteType(Enum):
    """Vote types"""
    FOR = "FOR"
    AGAINST = "AGAINST"
    ABSTAIN = "ABSTAIN"


@dataclass
class ProposalAction:
    """Action to execute in proposal"""
    target_contract: str
    method_name: str
    parameters: Dict
    value: int = 0  # ETH/token value to send


@dataclass
class Vote:
    """Vote record"""
    voter: str
    proposal_id: int
    vote_type: VoteType
    power: int
    reason: Optional[str] = None
    timestamp: datetime = field(default_factory=datetime.now)


@dataclass
class Proposal:
    """Governance proposal"""
    proposal_id: int
    proposer: str
    title: str
    description: str
    actions: List[ProposalAction]
    
    # Voting
    voting_start_block: int
    voting_end_block: int
    votes_for: int = 0
    votes_against: int = 0
    votes_abstain: int = 0
    vote_count: int = 0
    voters: Dict[str, VoteType] = field(default_factory=dict)
    
    # State
    state: ProposalState = ProposalState.PENDING
    created_timestamp: datetime = field(default_factory=datetime.now)
    executed: bool = False
    execution_timestamp: Optional[datetime] = None
    execution_results: Dict = field(default_factory=dict)
    
    # Metadata
    cancelled: bool = False
    cancelled_reason: Optional[str] = None


@dataclass
class GovernanceParams:
    """Governance parameters"""
    voting_delay_blocks: int = 1
    voting_period_blocks: int = 50400  # 1 week at 12s blocks
    proposal_threshold: int = 10**18   # 1 SYN token
    quorum_percentage: int = 33        # 33% of voting power
    approval_percentage: int = 67      # 67% for supermajority
    timelock_delay_seconds: int = 86400  # 1 day


class SynthosGovernanceContract:
    """
    SYNTHOS Governance Contract
    - DAO voting and proposal system
    - Time-locked execution
    - Multi-sig integration
    - Emergency actions
    """

    def __init__(self, owner: str, token_contract, voting_delay: int = 1):
        """Initialize governance contract"""
        self.owner = owner
        self.token_contract = token_contract  # Reference to token contract
        
        # Governance parameters
        self.params = GovernanceParams(voting_delay_blocks=voting_delay)
        
        # Proposals storage
        self.proposals: Dict[int, Proposal] = {}
        self.proposal_count = 0
        self.next_proposal_id = 1
        
        # Voting storage
        self.votes_cast: List[Vote] = []
        
        # Timelock
        self.queued_actions: Dict[str, datetime] = {}  # action_hash -> execution_time
        self.executed_actions: Dict[str, bool] = {}
        
        # Access control
        self.proposers: Dict[str, bool] = {owner: True}
        self.guardians: Dict[str, bool] = {owner: True}
        
        # Config
        self.current_block = 0


    def set_governance_params(self, owner: str, new_params: GovernanceParams) -> Tuple[bool, str]:
        """Update governance parameters"""
        if owner != self.owner:
            return False, "Only owner can update governance parameters"
        
        self.params = new_params
        return True, "Governance parameters updated"


    def propose(self, proposer: str, title: str, description: str, 
               actions: List[ProposalAction], current_block: int) -> Tuple[bool, int]:
        """
        Create new governance proposal
        Returns: (success, proposal_id or error)
        """
        # Check proposer voting power
        voting_power = self.token_contract.get_voting_power(proposer)
        if voting_power < self.params.proposal_threshold:
            return False, f"Insufficient voting power to propose. Have: {voting_power}, Need: {self.params.proposal_threshold}"
        
        # Validate actions
        if not actions:
            return False, "Proposal must have at least one action"
        
        # Create proposal
        proposal_id = self.next_proposal_id
        self.next_proposal_id += 1
        
        proposal = Proposal(
            proposal_id=proposal_id,
            proposer=proposer,
            title=title,
            description=description,
            actions=actions,
            voting_start_block=current_block + self.params.voting_delay_blocks,
            voting_end_block=current_block + self.params.voting_delay_blocks + self.params.voting_period_blocks,
            state=ProposalState.PENDING,
        )
        
        self.proposals[proposal_id] = proposal
        self.proposal_count += 1
        
        return True, proposal_id


    def cast_vote(self, voter: str, proposal_id: int, vote_type: VoteType, 
                 reason: Optional[str] = None) -> Tuple[bool, str]:
        """
        Cast vote on proposal
        Returns: (success, message)
        """
        if proposal_id not in self.proposals:
            return False, f"Proposal {proposal_id} does not exist"
        
        proposal = self.proposals[proposal_id]
        
        # Check proposal state
        if proposal.state != ProposalState.ACTIVE:
            return False, f"Proposal is in {proposal.state} state, not ACTIVE"
        
        # Check if already voted
        if voter in proposal.voters:
            return False, f"Voter has already voted on proposal {proposal_id}"
        
        # Get voting power at snapshot
        voting_power = self.token_contract.get_voting_power(voter)
        if voting_power == 0:
            return False, "Voter has no voting power"
        
        # Record vote
        proposal.voters[voter] = vote_type
        proposal.vote_count += 1
        
        if vote_type == VoteType.FOR:
            proposal.votes_for += voting_power
        elif vote_type == VoteType.AGAINST:
            proposal.votes_against += voting_power
        else:  # ABSTAIN
            proposal.votes_abstain += voting_power
        
        # Record in history
        vote = Vote(
            voter=voter,
            proposal_id=proposal_id,
            vote_type=vote_type,
            power=voting_power,
            reason=reason
        )
        self.votes_cast.append(vote)
        
        return True, f"Voted {vote_type.value} with {voting_power} power"


    def queue_proposal(self, proposal_id: int, current_block: int) -> Tuple[bool, str]:
        """
        Queue proposal for execution after timelock
        Returns: (success, message)
        """
        if proposal_id not in self.proposals:
            return False, f"Proposal {proposal_id} does not exist"
        
        proposal = self.proposals[proposal_id]
        
        # Check if voting ended
        if current_block <= proposal.voting_end_block:
            return False, f"Voting still active. End block: {proposal.voting_end_block}"
        
        # Calculate if proposal passed
        total_votes = proposal.votes_for + proposal.votes_against
        if total_votes == 0:
            proposal.state = ProposalState.DEFEATED
            return False, "No votes cast on proposal"
        
        # Check quorum
        total_voting_power = self._get_total_voting_power()
        quorum_votes = (total_voting_power * self.params.quorum_percentage) // 100
        if (proposal.votes_for + proposal.votes_against + proposal.votes_abstain) < quorum_votes:
            proposal.state = ProposalState.DEFEATED
            return False, f"Quorum not reached. Need: {quorum_votes}"
        
        # Check approval threshold
        approval_votes = (total_votes * self.params.approval_percentage) // 100
        if proposal.votes_for < approval_votes:
            proposal.state = ProposalState.DEFEATED
            return False, f"Approval threshold not met. Need: {approval_votes}, Got: {proposal.votes_for}"
        
        # Queue execution
        proposal.state = ProposalState.QUEUED
        execution_time = datetime.now() + timedelta(seconds=self.params.timelock_delay_seconds)
        
        for action in proposal.actions:
            action_hash = self._hash_action(proposal_id, action)
            self.queued_actions[action_hash] = execution_time
        
        proposal.state = ProposalState.SUCCEEDED
        return True, f"Proposal queued for execution at {execution_time}"


    def execute_proposal(self, proposal_id: int) -> Tuple[bool, str]:
        """
        Execute queued proposal
        Returns: (success, message)
        """
        if proposal_id not in self.proposals:
            return False, f"Proposal {proposal_id} does not exist"
        
        proposal = self.proposals[proposal_id]
        
        if proposal.state != ProposalState.SUCCEEDED:
            return False, f"Proposal is not ready for execution. State: {proposal.state}"
        
        # Check timelock expiry
        now = datetime.now()
        execution_ready = True
        
        for action in proposal.actions:
            action_hash = self._hash_action(proposal_id, action)
            if action_hash in self.queued_actions:
                if now < self.queued_actions[action_hash]:
                    execution_ready = False
                    break
        
        if not execution_ready:
            return False, "Timelock not expired"
        
        # Execute actions
        try:
            results = {}
            for i, action in enumerate(proposal.actions):
                result = self._execute_action(action)
                results[f"action_{i}"] = result
            
            proposal.state = ProposalState.EXECUTED
            proposal.executed = True
            proposal.execution_timestamp = now
            proposal.execution_results = results
            
            # Mark as executed
            for action in proposal.actions:
                action_hash = self._hash_action(proposal_id, action)
                self.executed_actions[action_hash] = True
            
            return True, f"Proposal executed successfully"
        
        except Exception as e:
            proposal.state = ProposalState.FAILED
            return False, f"Execution failed: {str(e)}"


    def cancel_proposal(self, canceller: str, proposal_id: int, reason: str) -> Tuple[bool, str]:
        """
        Cancel proposal (only guardian or proposer)
        Returns: (success, message)
        """
        if proposal_id not in self.proposals:
            return False, f"Proposal {proposal_id} does not exist"
        
        proposal = self.proposals[proposal_id]
        
        # Check authorization
        is_guardian = self.guardians.get(canceller, False)
        is_proposer = canceller == proposal.proposer
        
        if not (is_guardian or is_proposer):
            return False, "Not authorized to cancel proposal"
        
        if proposal.state == ProposalState.EXECUTED:
            return False, "Cannot cancel executed proposal"
        
        proposal.state = ProposalState.CANCELLED
        proposal.cancelled = True
        proposal.cancelled_reason = reason
        
        return True, f"Proposal cancelled: {reason}"


    def get_proposal(self, proposal_id: int) -> Optional[Dict]:
        """Get proposal details"""
        if proposal_id not in self.proposals:
            return None
        
        p = self.proposals[proposal_id]
        
        return {
            "proposal_id": p.proposal_id,
            "proposer": p.proposer,
            "title": p.title,
            "description": p.description,
            "voting_start_block": p.voting_start_block,
            "voting_end_block": p.voting_end_block,
            "state": p.state.value,
            "votes_for": p.votes_for,
            "votes_against": p.votes_against,
            "votes_abstain": p.votes_abstain,
            "vote_count": p.vote_count,
            "created_timestamp": p.created_timestamp.isoformat(),
            "executed": p.executed,
            "execution_timestamp": p.execution_timestamp.isoformat() if p.execution_timestamp else None,
            "action_count": len(p.actions),
        }


    def get_vote(self, voter: str, proposal_id: int) -> Optional[Dict]:
        """Get voter's vote"""
        for vote in self.votes_cast:
            if vote.voter == voter and vote.proposal_id == proposal_id:
                return {
                    "voter": vote.voter,
                    "proposal_id": vote.proposal_id,
                    "vote_type": vote.vote_type.value,
                    "power": vote.power,
                    "reason": vote.reason,
                    "timestamp": vote.timestamp.isoformat(),
                }
        return None


    def add_guardian(self, owner: str, guardian: str) -> Tuple[bool, str]:
        """Add emergency guardian"""
        if owner != self.owner:
            return False, "Only owner can add guardians"
        
        self.guardians[guardian] = True
        return True, f"Added {guardian} as guardian"


    def update_proposal_threshold(self, owner: str, new_threshold: int) -> Tuple[bool, str]:
        """Update proposal threshold"""
        if owner != self.owner:
            return False, "Only owner can update threshold"
        
        self.params.proposal_threshold = new_threshold
        return True, f"Updated proposal threshold to {new_threshold}"


    def advance_block(self, blocks: int = 1):
        """Advance current block (for testing)"""
        self.current_block += blocks


    def get_all_proposals(self, state_filter: Optional[ProposalState] = None) -> List[Dict]:
        """Get all proposals optionally filtered by state"""
        proposals = []
        
        for proposal_id in sorted(self.proposals.keys()):
            p = self.proposals[proposal_id]
            
            if state_filter and p.state != state_filter:
                continue
            
            proposals.append(self.get_proposal(proposal_id))
        
        return proposals


    def get_voting_stats(self) -> Dict:
        """Get governance voting statistics"""
        total_proposals = len(self.proposals)
        executed = sum(1 for p in self.proposals.values() if p.executed)
        defeated = sum(1 for p in self.proposals.values() if p.state == ProposalState.DEFEATED)
        active = sum(1 for p in self.proposals.values() if p.state == ProposalState.ACTIVE)
        
        return {
            "total_proposals": total_proposals,
            "executed": executed,
            "defeated": defeated,
            "active": active,
            "total_votes_cast": len(self.votes_cast),
            "proposal_threshold": self.params.proposal_threshold,
            "quorum_percentage": self.params.quorum_percentage,
            "approval_percentage": self.params.approval_percentage,
        }


    def _get_total_voting_power(self) -> int:
        """Get total voting power in system"""
        return self.token_contract.total_supply


    def _hash_action(self, proposal_id: int, action: ProposalAction) -> str:
        """Hash action for identification"""
        action_data = f"{proposal_id}{action.target_contract}{action.method_name}"
        return "0x" + hashlib.sha256(action_data.encode()).hexdigest()[:40]


    def _execute_action(self, action: ProposalAction) -> Dict:
        """Execute single action"""
        # In real implementation, would call external contract
        return {
            "target": action.target_contract,
            "method": action.method_name,
            "success": True,
            "message": f"Executed {action.method_name} on {action.target_contract}"
        }
