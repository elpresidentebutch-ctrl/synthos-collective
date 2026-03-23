"""
Smart Contracts System
Complete smart contract platform for SYNTHOS and Gemini Megachain 2.0
"""

from . import synthos
from . import gemini
from . import deployment

# Re-export key classes
from .synthos import (
    SynthosTokenContract,
    SynthosGovernanceContract,
    SynthosStakingContract,
)
from .gemini import (
    GeminiMegachain20,
    GeminiOracleContract,
    GeminiLendingContract,
    GeminiDEXContract,
)
from .deployment import (
    SmartContractManager,
    DeploymentStatus,
    ContractNetwork,
)

__all__ = [
    # Packages
    "synthos",
    "gemini",
    "deployment",
    
    # SYNTHOS contracts
    "SynthosTokenContract",
    "SynthosGovernanceContract",
    "SynthosStakingContract",
    
    # Gemini contracts
    "GeminiMegachain20",
    "GeminiOracleContract",
    "GeminiLendingContract",
    "GeminiDEXContract",
    
    # Deployment management
    "SmartContractManager",
    "DeploymentStatus",
    "ContractNetwork",
]
