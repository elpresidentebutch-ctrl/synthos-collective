"""
Smart Contracts System
Complete smart contract platform for SYNTHOS
"""

from . import synthos
from . import deployment

# Re-export key classes
from .synthos import (
    SynthosTokenContract,
    SynthosGovernanceContract,
    SynthosStakingContract,
)
from .deployment import (
    SmartContractManager,
    DeploymentStatus,
    ContractNetwork,
)

__all__ = [
    # Packages
    "synthos",
    "deployment",
    
    # SYNTHOS contracts
    "SynthosTokenContract",
    "SynthosGovernanceContract",
    "SynthosStakingContract",
    
    # Deployment management
    "SmartContractManager",
    "DeploymentStatus",
    "ContractNetwork",
]
