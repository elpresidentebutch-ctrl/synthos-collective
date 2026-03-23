"""
SYNTHOS Agent Governance Module

Exports governance capabilities:
- Governance voting system
- Governance enforcer
- Constraint enforcement system
"""

from .governance import (
    GovernanceVotingSystem,
    GovernanceEnforcer,
    GovernanceProposal,
    GovernanceVote,
    ProposalType,
    ProposalStatus,
    VoteValue
)

from .constraints import (
    ConstraintEnforcer,
    IdentityConstraint,
    ConstitutionalConstraint,
    DeterministicConstraint,
    EconomicConstraint,
    ResourceConstraint,
    SafetyConstraint,
    TimeConstraint,
    ConstraintType,
    ConstraintViolation,
    AgentIdentity
)

__all__ = [
    # Governance
    "GovernanceVotingSystem",
    "GovernanceEnforcer",
    "GovernanceProposal",
    "GovernanceVote",
    "ProposalType",
    "ProposalStatus",
    "VoteValue",
    
    # Constraints
    "ConstraintEnforcer",
    "IdentityConstraint",
    "ConstitutionalConstraint",
    "DeterministicConstraint",
    "EconomicConstraint",
    "ResourceConstraint",
    "SafetyConstraint",
    "TimeConstraint",
    "ConstraintType",
    "ConstraintViolation",
    "AgentIdentity"
]
