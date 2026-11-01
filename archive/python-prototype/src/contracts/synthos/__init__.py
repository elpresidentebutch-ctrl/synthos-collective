"""
SYNTHOS Smart Contracts
Token, Governance, and Staking contracts for SYNTHOS Agent civilization
"""

from .token import SynthosTokenContract, TokenTransfer, TokenApproval, TokenSnapshot
from .governance import (
    SynthosGovernanceContract, Proposal, ProposalState, VoteType, Vote,
    ProposalAction, GovernanceParams
)
from .staking import (
    SynthosStakingContract, Validator, Stake, StakeStatus,
    SlashingEvent
)

__all__ = [
    # Token exports
    "SynthosTokenContract",
    "TokenTransfer",
    "TokenApproval",
    "TokenSnapshot",
    
    # Governance exports
    "SynthosGovernanceContract",
    "Proposal",
    "ProposalState",
    "VoteType",
    "Vote",
    "ProposalAction",
    "GovernanceParams",
    
    # Staking exports
    "SynthosStakingContract",
    "Validator",
    "Stake",
    "StakeStatus",
    "SlashingEvent",
]
