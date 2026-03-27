"""
Stress Test Configuration for SYNTHOS Collective.

Provides preset workload profiles and configurable parameters
for load tests, benchmarks, and BFT scenarios.
"""

from dataclasses import dataclass, field
from typing import Dict, Any, List
from enum import Enum


class WorkloadProfile(Enum):
    """Predefined workload intensity profiles."""
    LIGHT = "light"
    MEDIUM = "medium"
    HEAVY = "heavy"
    EXTREME = "extreme"


@dataclass
class AgentStressConfig:
    """Configuration for stress test agents."""
    num_validators: int = 4
    num_citizens: int = 2
    stake_per_validator: int = 10_000
    initial_balance: int = 100_000
    roles: List[str] = field(default_factory=lambda: [
        "Validator", "Economist", "Governor",
        "Communicator", "Simulator", "Enforcer", "Citizen",
    ])


@dataclass
class TransactionConfig:
    """Configuration for transaction workloads."""
    num_transactions: int = 100
    transaction_amount: int = 10
    transaction_fee: int = 1
    payload_size_bytes: int = 0
    pattern: str = "constant"  # constant | burst | ramp
    burst_size: int = 50
    ramp_steps: int = 5


@dataclass
class NetworkConfig:
    """Simulated network condition parameters."""
    latency_ms: float = 0.0
    packet_loss_rate: float = 0.0
    jitter_ms: float = 0.0


@dataclass
class FailureConfig:
    """Byzantine and crash failure configuration."""
    byzantine_validator_count: int = 0
    crash_probability: float = 0.0
    message_drop_rate: float = 0.0
    equivocation_enabled: bool = False
    double_spend_enabled: bool = False
    block_withholding_enabled: bool = False


@dataclass
class TimingConfig:
    """Consensus and block timing parameters."""
    consensus_timeout_ms: int = 5000
    block_interval_ms: int = 1000
    finality_depth: int = 1
    test_duration_seconds: float = 30.0


@dataclass
class StressTestConfig:
    """Top-level configuration grouping all sub-configs."""
    profile: WorkloadProfile = WorkloadProfile.LIGHT
    agents: AgentStressConfig = field(default_factory=AgentStressConfig)
    transactions: TransactionConfig = field(default_factory=TransactionConfig)
    network: NetworkConfig = field(default_factory=NetworkConfig)
    failures: FailureConfig = field(default_factory=FailureConfig)
    timing: TimingConfig = field(default_factory=TimingConfig)
    extra: Dict[str, Any] = field(default_factory=dict)


# ---------------------------------------------------------------------------
# Preset factory helpers
# ---------------------------------------------------------------------------

def get_light_config() -> StressTestConfig:
    """Light workload: minimal agents, low transaction volume."""
    return StressTestConfig(
        profile=WorkloadProfile.LIGHT,
        agents=AgentStressConfig(num_validators=4, num_citizens=1),
        transactions=TransactionConfig(num_transactions=50, pattern="constant"),
        timing=TimingConfig(test_duration_seconds=10.0),
    )


def get_medium_config() -> StressTestConfig:
    """Medium workload: moderate agents and transaction volume."""
    return StressTestConfig(
        profile=WorkloadProfile.MEDIUM,
        agents=AgentStressConfig(num_validators=7, num_citizens=3),
        transactions=TransactionConfig(
            num_transactions=500,
            pattern="burst",
            burst_size=100,
        ),
        timing=TimingConfig(test_duration_seconds=30.0),
    )


def get_heavy_config() -> StressTestConfig:
    """Heavy workload: many agents, high transaction volume."""
    return StressTestConfig(
        profile=WorkloadProfile.HEAVY,
        agents=AgentStressConfig(num_validators=16, num_citizens=8),
        transactions=TransactionConfig(
            num_transactions=2000,
            pattern="ramp",
            ramp_steps=10,
        ),
        timing=TimingConfig(test_duration_seconds=60.0),
    )


def get_extreme_config() -> StressTestConfig:
    """Extreme workload: maximum agents, peak transaction volume."""
    return StressTestConfig(
        profile=WorkloadProfile.EXTREME,
        agents=AgentStressConfig(num_validators=50, num_citizens=50),
        transactions=TransactionConfig(
            num_transactions=10_000,
            pattern="burst",
            burst_size=500,
        ),
        timing=TimingConfig(test_duration_seconds=120.0),
    )


def get_bft_config(num_validators: int = 10) -> StressTestConfig:
    """BFT test configuration: 1/3 Byzantine validators."""
    byzantine_count = max(1, num_validators // 3)
    return StressTestConfig(
        profile=WorkloadProfile.MEDIUM,
        agents=AgentStressConfig(num_validators=num_validators, num_citizens=2),
        transactions=TransactionConfig(num_transactions=200, pattern="constant"),
        failures=FailureConfig(
            byzantine_validator_count=byzantine_count,
            equivocation_enabled=True,
            double_spend_enabled=True,
            block_withholding_enabled=True,
        ),
        timing=TimingConfig(test_duration_seconds=30.0),
    )


PRESET_PROFILES: Dict[str, StressTestConfig] = {
    WorkloadProfile.LIGHT.value: get_light_config(),
    WorkloadProfile.MEDIUM.value: get_medium_config(),
    WorkloadProfile.HEAVY.value: get_heavy_config(),
    WorkloadProfile.EXTREME.value: get_extreme_config(),
}
