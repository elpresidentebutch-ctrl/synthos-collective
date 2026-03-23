"""SYNTHOS Collective - Decentralized Agent Framework"""

from src.core import (
    SyntHOSAgent,
    AgentState,
    Event,
    EventBus,
    EventType,
    Role,
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
    'AgentState',
    'Event',
    'EventBus',
    'EventType',
    'Role',
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
