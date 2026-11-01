"""SYNTHOS Agent Roles Module"""

from src.roles.validator import ValidatorRole
from src.roles.economist import EconomistRole
from src.roles.governor import GovernorRole
from src.roles.communicator import CommunicatorRole
from src.roles.simulator import SimulatorRole
from src.roles.enforcer import EnforcerRole
from src.roles.citizen import CitizenRole

__all__ = [
    'ValidatorRole',
    'EconomistRole',
    'GovernorRole',
    'CommunicatorRole',
    'SimulatorRole',
    'EnforcerRole',
    'CitizenRole',
]
