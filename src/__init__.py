"""SYNTHOS Collective - Decentralized Agent Framework"""

from src.core import (
    SyntHOSAgent,
    AgentConfig,
    AgentState,
    Event,
    EventBus,
    EventType,
    Role,
    RoleStatus,
)

from src.roles import (
    ValidatorRole,
    EconomistRole,
    GovernorRole,
    CommunicatorRole,
    SimulatorRole,
    EnforcerRole,
    CitizenRole,
)

from src.models import (
    Transaction,
    Block,
    Proposal,
    Vote,
    Validator,
    Metrics,
)

__version__ = "1.0.0"
__all__ = [
    # Core
    'SyntHOSAgent',
    'AgentConfig',
    'AgentState',
    'Event',
    'EventBus',
    'EventType',
    'Role',
    'RoleStatus',
    # Roles
    'ValidatorRole',
    'EconomistRole',
    'GovernorRole',
    'CommunicatorRole',
    'SimulatorRole',
    'EnforcerRole',
    'CitizenRole',
    # Models
    'Transaction',
    'Block',
    'Proposal',
    'Vote',
    'Validator',
    'Metrics',
]
