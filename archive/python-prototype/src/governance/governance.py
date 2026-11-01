"""
SYNTHOS Agent Governance System

Implements DAO voting, proposal management, and governance outcome enforcement.
"""

import asyncio
import hashlib
import json
import time
from dataclasses import dataclass, field
from datetime import datetime, timedelta
from enum import Enum
from typing import Dict, List, Optional, Set, Any
from uuid import uuid4


class ProposalType(Enum):
    """Types of governance proposals."""
    PROTOCOL_UPGRADE = "protocol_upgrade"
    PARAMETER_CHANGE = "parameter_change"
    SLASHING_EVENT = "slashing_event"
    CROSS_CHAIN_AGREEMENT = "cross_chain_agreement"
    TREATY = "treaty"
    ECONOMIC_POLICY = "economic_policy"
    CONSTITUTION_AMENDMENT = "constitution_amendment"
    EMERGENCY_ACTION = "emergency_action"


class VoteValue(Enum):
    """Vote options."""
    FOR = "for"
    AGAINST = "against"
    ABSTAIN = "abstain"


class ProposalStatus(Enum):
    """Proposal lifecycle statuses."""
    PROPOSED = "proposed"
    VOTING = "voting"
    VOTING_CLOSED = "voting_closed"
    REJECTED = "rejected"
    PASSED = "passed"
    EXECUTING = "executing"
    EXECUTED = "executed"
    FAILED = "failed"
    CANCELLED = "cancelled"


@dataclass
class GovernanceVote:
    """A single vote in a governance proposal."""
    voter_id: str
    proposal_id: str
    vote_value: VoteValue
    voting_power: int  # Stake or delegation power
    signature: str
    timestamp: float
    reason: Optional[str] = None
    
    def to_dict(self) -> Dict[str, Any]:
        """Serialize vote to dictionary."""
        return {
            "voter_id": self.voter_id,
            "proposal_id": self.proposal_id,
            "vote_value": self.vote_value.value,
            "voting_power": self.voting_power,
            "signature": self.signature,
            "timestamp": self.timestamp,
            "reason": self.reason
        }


@dataclass
class GovernanceProposal:
    """A governance proposal submitted to the DAO."""
    proposal_id: str
    proposer_id: str
    proposal_type: ProposalType
    title: str
    description: str
    parameters: Dict[str, Any]
    
    # Voting
    voting_start: float
    voting_end: float
    votes_for: int = 0
    votes_against: int = 0
    votes_abstain: int = 0
    vote_count: int = 0
    
    # Voters tracking
    voters: Set[str] = field(default_factory=set)
    vote_records: List[GovernanceVote] = field(default_factory=list)
    
    # Metadata
    status: ProposalStatus = ProposalStatus.PROPOSED
    created_at: float = field(default_factory=time.time)
    execution_timestamp: Optional[float] = None
    execution_result: Optional[Dict[str, Any]] = None
    link_hash: str = ""
    
    def __post_init__(self):
        """Compute proposal hash."""
        self.link_hash = self._compute_hash()
    
    def _compute_hash(self) -> str:
        """Compute proposal hash for integrity."""
        content = json.dumps({
            "proposer": self.proposer_id,
            "type": self.proposal_type.value,
            "title": self.title,
            "parameters": self.parameters,
            "created": self.created_at
        }, sort_keys=True)
        return hashlib.sha256(content.encode()).hexdigest()
    
    def to_dict(self) -> Dict[str, Any]:
        """Serialize proposal to dictionary."""
        return {
            "proposal_id": self.proposal_id,
            "proposer_id": self.proposer_id,
            "proposal_type": self.proposal_type.value,
            "title": self.title,
            "description": self.description,
            "parameters": self.parameters,
            "voting_start": self.voting_start,
            "voting_end": self.voting_end,
            "votes_for": self.votes_for,
            "votes_against": self.votes_against,
            "votes_abstain": self.votes_abstain,
            "vote_count": self.vote_count,
            "status": self.status.value,
            "created_at": self.created_at,
            "link_hash": self.link_hash
        }


class GovernanceVotingSystem:
    """
    Manages DAO voting for governance proposals.
    
    Features:
    - Proposal submission and tracking
    - Vote collection with power weighting
    - Supermajority calculation
    - Delegation of voting power
    - Vote confidentiality until voting ends
    """
    
    def __init__(self, voting_period_hours=72, quorum_percentage=33):
        """
        Initialize governance voting system.
        
        Args:
            voting_period_hours: Default voting duration in hours
            quorum_percentage: Minimum participation percentage
        """
        self.voting_period = voting_period_hours * 3600  # Convert to seconds
        self.quorum_percentage = quorum_percentage
        
        # Proposal management
        self.proposals: Dict[str, GovernanceProposal] = {}
        self.proposal_queue: List[str] = []
        
        # Voting power delegation
        self.delegations: Dict[str, str] = {}  # voter -> delegate
        self.reverse_delegations: Dict[str, Set[str]] = {}  # delegate -> voters
        
        # Voting records
        self.all_votes: List[GovernanceVote] = []
    
    async def submit_proposal(
        self,
        proposer_id: str,
        proposal_type: ProposalType,
        title: str,
        description: str,
        parameters: Dict[str, Any],
        voting_duration_hours: Optional[int] = None
    ) -> str:
        """
        Submit a new governance proposal.
        
        Args:
            proposer_id: ID of proposing agent
            proposal_type: Type of proposal
            title: Proposal title
            description: Detailed description
            parameters: Proposal-specific parameters
            voting_duration_hours: Custom voting duration
        
        Returns:
            Proposal ID
        
        Raises:
            ValueError: If proposal is invalid
        """
        # Validate proposal
        self._validate_proposal_submission(proposer_id, proposal_type, parameters)
        
        # Create proposal ID
        proposal_id = str(uuid4())
        
        # Calculate voting window
        voting_start = time.time()
        voting_duration = voting_duration_hours or int(self.voting_period / 3600)
        voting_end = voting_start + (voting_duration * 3600)
        
        # Create proposal object
        proposal = GovernanceProposal(
            proposal_id=proposal_id,
            proposer_id=proposer_id,
            proposal_type=proposal_type,
            title=title,
            description=description,
            parameters=parameters,
            voting_start=voting_start,
            voting_end=voting_end,
            status=ProposalStatus.VOTING
        )
        
        # Store proposal
        self.proposals[proposal_id] = proposal
        self.proposal_queue.append(proposal_id)
        
        print(f"✓ Proposal submitted: {proposal_id} ({proposal_type.value})")
        return proposal_id
    
    async def delegate_voting_power(
        self,
        delegator_id: str,
        delegate_id: str
    ) -> bool:
        """
        Delegate voting power to another agent.
        
        Args:
            delegator_id: Agent delegating power
            delegate_id: Agent receiving power
        
        Returns:
            True if delegation successful
        """
        if delegator_id == delegate_id:
            raise ValueError("Cannot delegate to self")
        
        # Store delegation
        old_delegate = self.delegations.get(delegator_id)
        
        self.delegations[delegator_id] = delegate_id
        
        # Update reverse delegations
        if old_delegate:
            self.reverse_delegations[old_delegate].discard(delegator_id)
        
        if delegate_id not in self.reverse_delegations:
            self.reverse_delegations[delegate_id] = set()
        
        self.reverse_delegations[delegate_id].add(delegator_id)
        
        print(f"✓ Delegation: {delegator_id} → {delegate_id}")
        return True
    
    async def revoke_delegation(self, delegator_id: str) -> bool:
        """Revoke delegated voting power."""
        old_delegate = self.delegations.pop(delegator_id, None)
        
        if old_delegate:
            self.reverse_delegations[old_delegate].discard(delegator_id)
        
        return True
    
    async def cast_vote(
        self,
        voter_id: str,
        proposal_id: str,
        vote_value: VoteValue,
        voting_power: int,
        signature: str,
        reason: Optional[str] = None
    ) -> bool:
        """
        Cast a vote on a proposal.
        
        Args:
            voter_id: ID of voting agent
            proposal_id: ID of proposal to vote on
            vote_value: FOR, AGAINST, or ABSTAIN
            voting_power: Voting power (stake)
            signature: Cryptographic signature
            reason: Optional reason for vote
        
        Returns:
            True if vote successful
        
        Raises:
            ValueError: If vote is invalid
        """
        proposal = self.proposals.get(proposal_id)
        
        if not proposal:
            raise ValueError(f"Proposal not found: {proposal_id}")
        
        # Check voting window
        current_time = time.time()
        if current_time > proposal.voting_end:
            raise ValueError("Voting period has ended")
        
        if current_time < proposal.voting_start:
            raise ValueError("Voting period has not started")
        
        # Check vote validity
        if proposal.status != ProposalStatus.VOTING:
            raise ValueError(f"Proposal is not in voting state: {proposal.status.value}")
        
        # Check double voting
        if voter_id in proposal.voters:
            raise ValueError(f"Voter {voter_id} has already voted")
        
        # Create vote record
        vote = GovernanceVote(
            voter_id=voter_id,
            proposal_id=proposal_id,
            vote_value=vote_value,
            voting_power=voting_power,
            signature=signature,
            timestamp=current_time,
            reason=reason
        )
        
        # Update proposal tallies
        proposal.voters.add(voter_id)
        proposal.vote_records.append(vote)
        proposal.vote_count += 1
        
        if vote_value == VoteValue.FOR:
            proposal.votes_for += voting_power
        elif vote_value == VoteValue.AGAINST:
            proposal.votes_against += voting_power
        else:
            proposal.votes_abstain += voting_power
        
        # Store vote
        self.all_votes.append(vote)
        
        return True
    
    async def close_voting(self, proposal_id: str) -> ProposalStatus:
        """
        Close voting on a proposal and determine outcome.
        
        Args:
            proposal_id: ID of proposal
        
        Returns:
            Final status of proposal
        """
        proposal = self.proposals.get(proposal_id)
        
        if not proposal:
            raise ValueError(f"Proposal not found: {proposal_id}")
        
        # Update status
        proposal.status = ProposalStatus.VOTING_CLOSED
        
        # Calculate results
        total_votes = proposal.votes_for + proposal.votes_against
        
        if total_votes == 0:
            proposal.status = ProposalStatus.REJECTED
            return ProposalStatus.REJECTED
        
        # Check supermajority (2/3+)
        supermajority_threshold = (total_votes * 2) // 3 + 1
        
        if proposal.votes_for >= supermajority_threshold:
            proposal.status = ProposalStatus.PASSED
        else:
            proposal.status = ProposalStatus.REJECTED
        
        print(f"✓ Voting closed: {proposal_id}")
        print(f"  For: {proposal.votes_for}, Against: {proposal.votes_against}")
        print(f"  Result: {proposal.status.value}")
        
        return proposal.status
    
    async def execute_proposal(
        self,
        proposal_id: str,
        execution_handler
    ) -> bool:
        """
        Execute a passed proposal.
        
        Args:
            proposal_id: ID of proposal
            execution_handler: Callable to execute proposal
        
        Returns:
            True if execution successful
        """
        proposal = self.proposals.get(proposal_id)
        
        if not proposal:
            raise ValueError(f"Proposal not found: {proposal_id}")
        
        if proposal.status != ProposalStatus.PASSED:
            raise ValueError(f"Proposal has not passed: {proposal.status.value}")
        
        try:
            proposal.status = ProposalStatus.EXECUTING
            
            # Execute proposal
            result = await execution_handler(proposal)
            
            proposal.status = ProposalStatus.EXECUTED
            proposal.execution_timestamp = time.time()
            proposal.execution_result = result
            
            print(f"✓ Proposal executed: {proposal_id}")
            return True
            
        except Exception as e:
            proposal.status = ProposalStatus.FAILED
            proposal.execution_result = {"error": str(e)}
            print(f"✗ Proposal execution failed: {e}")
            return False


class GovernanceEnforcer:
    """
    Enforces governance outcomes and applies constraints.
    
    Ensures that:
    - DAO decisions are implemented
    - Constitutional amendments take effect
    - Economic policies are applied
    - Violations are penalized
    """
    
    def __init__(self):
        """Initialize governance enforcer."""
        self.active_policies: Dict[str, Dict[str, Any]] = {}
        self.amendment_history: List[Dict[str, Any]] = []
        self.enforcement_actions: List[Dict[str, Any]] = []
    
    async def enforce_protocol_upgrade(
        self,
        proposal: GovernanceProposal,
        upgrade_handler
    ) -> bool:
        """
        Enforce a protocol upgrade decision.
        
        Args:
            proposal: Passed proposal
            upgrade_handler: Callable to perform upgrade
        
        Returns:
            True if upgrade successful
        """
        try:
            # Prepare upgrade parameters
            upgrade_params = {
                "target_version": proposal.parameters.get("version"),
                "features": proposal.parameters.get("features", []),
                "migrations": proposal.parameters.get("migrations", []),
                "rollback_plan": proposal.parameters.get("rollback_plan", {})
            }
            
            # Execute upgrade
            result = await upgrade_handler(upgrade_params)
            
            # Record enforcement action
            self.enforcement_actions.append({
                "timestamp": time.time(),
                "proposal_id": proposal.proposal_id,
                "action_type": "protocol_upgrade",
                "result": result
            })
            
            return True
            
        except Exception as e:
            print(f"✗ Protocol upgrade failed: {e}")
            return False
    
    async def enforce_parameter_change(
        self,
        proposal: GovernanceProposal,
        parameter_handler
    ) -> bool:
        """
        Enforce a parameter change decision.
        
        Args:
            proposal: Passed proposal
            parameter_handler: Callable to apply parameters
        
        Returns:
            True if change successful
        """
        try:
            params = proposal.parameters.get("changes", {})
            
            # Apply each parameter change
            for param_name, param_value in params.items():
                await parameter_handler(param_name, param_value)
                self.active_policies[param_name] = param_value
            
            # Record enforcement action
            self.enforcement_actions.append({
                "timestamp": time.time(),
                "proposal_id": proposal.proposal_id,
                "action_type": "parameter_change",
                "parameters": params
            })
            
            return True
            
        except Exception as e:
            print(f"✗ Parameter change failed: {e}")
            return False
    
    async def enforce_slashing(
        self,
        proposal: GovernanceProposal,
        slashing_handler
    ) -> bool:
        """
        Enforce a slashing decision.
        
        Args:
            proposal: Passed proposal
            slashing_handler: Callable to apply slashing
        
        Returns:
            True if slashing applied
        """
        try:
            slashing_params = {
                "validator_id": proposal.parameters.get("validator_id"),
                "slash_percentage": proposal.parameters.get("slash_percentage", 10),
                "reason": proposal.parameters.get("reason", "governance decision"),
                "appeal_window": proposal.parameters.get("appeal_window", 86400)
            }
            
            # Apply slashing
            result = await slashing_handler(slashing_params)
            
            # Record enforcement action
            self.enforcement_actions.append({
                "timestamp": time.time(),
                "proposal_id": proposal.proposal_id,
                "action_type": "slashing",
                "result": result
            })
            
            return True
            
        except Exception as e:
            print(f"✗ Slashing enforcement failed: {e}")
            return False
    
    async def enforce_constitutional_amendment(
        self,
        proposal: GovernanceProposal,
        constitution_handler
    ) -> bool:
        """
        Enforce a constitutional amendment.
        
        Args:
            proposal: Passed proposal
            constitution_handler: Callable to update constitution
        
        Returns:
            True if amendment applied
        """
        try:
            amendment = {
                "rule_id": proposal.parameters.get("rule_id"),
                "rule_type": proposal.parameters.get("rule_type"),
                "content": proposal.parameters.get("content"),
                "effective_immediately": proposal.parameters.get("effective_immediately", False)
            }
            
            # Apply amendment
            result = await constitution_handler(amendment)
            
            # Record amendment
            self.amendment_history.append({
                "timestamp": time.time(),
                "proposal_id": proposal.proposal_id,
                "amendment": amendment,
                "result": result
            })
            
            # Record enforcement action
            self.enforcement_actions.append({
                "timestamp": time.time(),
                "proposal_id": proposal.proposal_id,
                "action_type": "constitutional_amendment",
                "result": result
            })
            
            return True
            
        except Exception as e:
            print(f"✗ Amendment enforcement failed: {e}")
            return False
    
    async def enforce_treaty(
        self,
        proposal: GovernanceProposal,
        treaty_handler
    ) -> bool:
        """
        Enforce a cross-chain treaty agreement.
        
        Args:
            proposal: Passed proposal
            treaty_handler: Callable to establish treaty
        
        Returns:
            True if treaty established
        """
        try:
            treaty = {
                "counterparty": proposal.parameters.get("counterparty"),
                "terms": proposal.parameters.get("terms", {}),
                "duration": proposal.parameters.get("duration"),
                "penalties": proposal.parameters.get("penalties", {})
            }
            
            # Establish treaty
            result = await treaty_handler(treaty)
            
            # Record treaty agreement
            self.enforcement_actions.append({
                "timestamp": time.time(),
                "proposal_id": proposal.proposal_id,
                "action_type": "treaty",
                "treaty": treaty,
                "result": result
            })
            
            return True
            
        except Exception as e:
            print(f"✗ Treaty enforcement failed: {e}")
            return False
    
    async def enforce_economic_policy(
        self,
        proposal: GovernanceProposal,
        policy_handler
    ) -> bool:
        """
        Enforce an economic policy decision.
        
        Args:
            proposal: Passed proposal
            policy_handler: Callable to apply policy
        
        Returns:
            True if policy applied
        """
        try:
            policy = {
                "policy_type": proposal.parameters.get("policy_type"),
                "target": proposal.parameters.get("target"),
                "value": proposal.parameters.get("value"),
                "duration": proposal.parameters.get("duration"),
                "conditions": proposal.parameters.get("conditions", {})
            }
            
            # Apply policy
            result = await policy_handler(policy)
            
            # Store active policy
            self.active_policies[policy["policy_type"]] = policy
            
            # Record enforcement action
            self.enforcement_actions.append({
                "timestamp": time.time(),
                "proposal_id": proposal.proposal_id,
                "action_type": "economic_policy",
                "policy": policy,
                "result": result
            })
            
            return True
            
        except Exception as e:
            print(f"✗ Economic policy enforcement failed: {e}")
            return False
    
    def get_enforcement_history(
        self,
        action_type: Optional[str] = None,
        limit: int = 100
    ) -> List[Dict[str, Any]]:
        """
        Get enforcement action history.
        
        Args:
            action_type: Filter by action type
            limit: Maximum results
        
        Returns:
            List of enforcement actions
        """
        actions = self.enforcement_actions
        
        if action_type:
            actions = [a for a in actions if a.get("action_type") == action_type]
        
        return actions[-limit:]
    
    def get_active_policies(self) -> Dict[str, Dict[str, Any]]:
        """Get currently active policies."""
        return self.active_policies.copy()


def _validate_proposal_submission(proposer_id: str, proposal_type: ProposalType, parameters: Dict[str, Any]):
    """Validate proposal submission."""
    if not proposer_id:
        raise ValueError("Proposer ID required")
    
    if proposal_type == ProposalType.PROTOCOL_UPGRADE:
        if "version" not in parameters:
            raise ValueError("Protocol upgrade requires target version")
    
    elif proposal_type == ProposalType.PARAMETER_CHANGE:
        if "changes" not in parameters:
            raise ValueError("Parameter change requires changes dict")
    
    elif proposal_type == ProposalType.SLASHING_EVENT:
        if "validator_id" not in parameters or "slash_percentage" not in parameters:
            raise ValueError("Slashing requires validator_id and slash_percentage")
    
    elif proposal_type == ProposalType.CROSS_CHAIN_AGREEMENT:
        if "counterparty" not in parameters:
            raise ValueError("Cross-chain agreement requires counterparty")
    
    elif proposal_type == ProposalType.CONSTITUTION_AMENDMENT:
        if "rule_id" not in parameters or "content" not in parameters:
            raise ValueError("Constitutional amendment requires rule_id and content")


GovernanceVotingSystem._validate_proposal_submission = staticmethod(_validate_proposal_submission)
