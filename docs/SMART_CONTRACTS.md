# SYNTHOS Smart Contracts System

Complete smart contract platform for SYNTHOS Agent civilization, featuring governance, staking, and advanced DeFi capabilities.

## Overview

### SYNTHOS Smart Contracts (SynCoin, Governance, Staking)

**SYNTHOS SynCoin Contract** (`contracts/src/synthos/SynCoin.sol`)
- AI-native, agent-centric coin with modular, programmable features
- Supports programmable agent-to-agent transfers, staking, governance, and more
- No supply inflation (max supply = initial, programmable by agents)
- Advanced transfer controls, agent roles, and metadata
- Complete transfer and event history

**SYNTHOS Governance Contract** (`src/contracts/synthos/governance.py`)
- DAO voting on protocol upgrades, treaties, economic policy, constitution changes
- Proposal creation, voting, and execution lifecycle
- Delegation system for voting power
- Time-locked execution (1-day default)
- Supermajority voting requirement (2/3+)
- Emergency proposal cancellation by guardians
- Vote tallying with quorum enforcement (33% default)
- 8 proposal types with custom enforcement

**SYNTHOS Staking Contract** (`src/contracts/synthos/staking.py`)
- Validator registration and stake management
- Delegation system with unbonding period (1 week)
- Reward distribution to validators and delegators
- Proportional reward calculation based on stake
- Slashing for misbehavior with cooldown periods
- Max 100 validators with minimum stake requirements
- Commission-based reward split between validator and delegators
- Unbonding period with block-based tracking


**Deployment Manager** (`src/contracts/deployment/manager.py`)
- Centralized deployment orchestration
- Multi-network support (Ethereum, Polygon, Arbitrum, etc.)
- Configuration management and versioning
- Emergency pause/resume capabilities
- Health monitoring and status tracking
- Role-based access control (deployers, operators)

## Architecture

### Contract Organization

```
src/contracts/
├── synthos/                    # SYNTHOS Token, Governance, Staking
│   ├── __init__.py            # Module exports
│   ├── token.py               # Token contract (1B supply)
│   ├── governance.py          # DAO voting system
│   └── staking.py             # Validator staking and delegation

├── deployment/                 # Deployment Management
│   ├── __init__.py            # Module exports
│   └── manager.py             # Deployment orchestration
└── __init__.py               # Main contracts package
```

### Key Design Patterns

1. **Dataclass Models**: All data structures use Python dataclasses for type safety
2. **Enum States**: State management via enums (ProposalState, StakeStatus, etc.)
3. **Audit Trails**: Complete history tracking for all major operations
4. **Access Control**: Role-based permissions (owner, operators, validators, guardians)
5. **Error Handling**: Tuple returns with (success, message) pattern
6. **Configuration**: Centralized parameter management with update mechanisms

## SYNTHOS Token Contract

### Features

**Supply Management**
  - Initial: 100 billion (1×10¹¹) SynCoin
  - Decimals: 18
  - Max supply programmable by agent consensus
  - Minting/burning and advanced supply logic are agent-governed

**Governance Integration**
  - Agent-centric voting and governance hooks
  - Delegation and programmable voting power
  - Complete delegation and agent role management

**Transfer Controls**
  - Programmable agent-to-agent transfers
  - Pause/resume and advanced transfer controls
  - Complete transfer and event history
  - Advanced agent permission and approval management

**History & Auditing**
  - Complete transfer and event history (unlimited)
  - Agent action and permission tracking
  - Mint/burn and programmable event history

### Interface

```python
# Create token
token = SynthosTokenContract(owner="0xowner")

# Basic operations
success, tx_hash = token.transfer(from_addr, to_addr, amount, reason="payment")
success, tx_hash = token.approve(owner, spender, amount)
success, tx_hash = token.transfer_from(spender, from_addr, to_addr, amount)

# Mint/Burn
success, msg = token.mint(minter, to_addr, amount, reason="reward")
success, msg = token.burn(burner, from_addr, amount, reason="slashing")

# Governance
snapshot_id = token.create_snapshot(block_height)
voting_power = token.get_voting_power(address, snapshot_id)
success, msg = token.delegate(delegator, delegatee)

# Management
balance = token.balance_of(address)
allowance = token.allowance(owner, spender)
state = token.get_contract_state()
```

## SYNTHOS Governance Contract

### Features

- **Proposal Types** (8 types)
  1. PROTOCOL_UPGRADE - Version changes, feature additions
  2. PARAMETER_CHANGE - Consensus, economic, network parameters
  3. SLASHING_EVENT - Validator penalties
  4. CROSS_CHAIN_AGREEMENT - Bridge and interop agreements
  5. TREATY - Cross-chain treaties
  6. ECONOMIC_POLICY - Inflation, rewards, fees
  7. CONSTITUTION_AMENDMENT - Constitution rule changes
  8. EMERGENCY_ACTION - Emergency protocol changes

- **Voting System**
  - Voting power from token balance
  - Delegation system for power transfer
  - Vote types: FOR, AGAINST, ABSTAIN
  - Quorum requirement (33% default)
  - Supermajority approval (67% default)

- **Execution Flow**
  1. Proposer submits proposal with actions
  2. Voting period begins (configurable, ~1 week)
  3. Voters cast votes with delegation
  4. Voting period closes, votes tallied
  5. Queued for execution (time-locked, 1 day default)
  6. Executed after timelock expires
  7. Results tracked and history maintained

- **Emergency Features**
  - Guardian role for emergency cancellation
  - Proposal threshold to prevent spam
  - Time-locked execution prevents flash loans
  - Vote delegation enables participation

### Interface

```python
# Create governance
gov = SynthosGovernanceContract(owner, token_contract)

# Propose
success, proposal_id = gov.propose(
    proposer="0xuser",
    title="Protocol Upgrade v2.0",
    description="Major upgrade",
    actions=[ProposalAction(target, method, params)],
    current_block=12345000
)

# Vote
success, msg = gov.cast_vote(
    voter="0xvoting_address",
    proposal_id=proposal_id,
    vote_type=VoteType.FOR,
    reason="I support this upgrade"
)

# Queue and execute
success, msg = gov.queue_proposal(proposal_id, current_block)
success, msg = gov.execute_proposal(proposal_id)

# Query
proposal = gov.get_proposal(proposal_id)
vote = gov.get_vote(voter, proposal_id)
stats = gov.get_voting_stats()
```

## SYNTHOS Staking Contract

### Features

- **Validator Management**
  - Registration with minimum self-stake (10k SYN default)
  - Max 100 validators
  - Commission rates (basis points, 0-100%)
  - Active/inactive status

- **Delegation System**
  - Delegate to any active validator
  - Minimum delegation (0.001 SYN default)
  - Unbonding period (1 week default)
  - No slashing during unbonding

- **Reward Distribution**
  - Per-block rewards
  - Commission split: validator keeps commission
  - Remaining rewards split by stake ratio
  - Interest accrual on positions
  - Claimable after unbonding

- **Slashing Mechanism**
  - Percentage-based slashing
  - Affects self-stake and delegators proportionally
  - Cooldown period between slashes
  - Complete slashing event history
  - Appeal windows for delegators

- **Unbonding Process**
  1. Delegator initiates unbond
  2. Moved to UNBONDING status
  3. Blocks countdown during epochs
  4. After period expires, claim unbonded tokens
  5. Receive principal + accumulated rewards

### Interface

```python
# Create staking
staking = SynthosStakingContract(owner, token_contract, min_stake=10**18)

# Register validator
success, msg = staking.register_validator(
    validator_addr="0xvalidator",
    name="MyValidator",
    stake_amount=100000 * 10**18,
    commission_rate=100  # 1%
)

# Delegate
success, msg = staking.delegate(
    delegator="0xdelegator",
    validator_addr="0xvalidator",
    amount=1000 * 10**18
)

# Unbond
success, msg = staking.undelegate(delegator, validator_addr, stake_index=0)

# Claim after unbonding
success, msg = staking.claim_unstaked(delegator, stake_index=0)

# Distribute rewards
success, msg = staking.distribute_rewards(
    validator_addr="0xvalidator",
    reward_amount=100 * 10**18,
    block_height=12345000
)

# Slash for misbehavior
success, msg = staking.slash_validator(
    validator_addr="0xvalidator",
    slash_percentage=50,  # 50% slash
    reason="Double signing"
)

# Query
validator = staking.get_validator("0xvalidator")
rankings = staking.get_validator_ranking(top_n=50)
stats = staking.get_staking_stats()
```

## Deployment Manager

### Features

- **Deployment Orchestration**
  - Plan deployments before execution
  - Batch deployments across networks
  - Track deployments by plan ID
  - Gas cost estimation and tracking

- **Configuration Management**
  - Version control for configurations
  - Configuration history
  - Emergency pause/resume
  - Operator-level access control

- **Monitoring & Health**
  - Health check on contracts
  - Status tracking (healthy, paused)
  - Transaction logging
  - Error logging and history

- **Role-Based Access**
  - Owner: Full control
  - Deployers: Can deploy contracts
  - Operators: Can update configuration, pause/resume

### Interface

```python
# Create manager
manager = SmartContractManager(owner="0xowner")

# Plan deployment
success, plan_id = manager.plan_deployment(
    contract_name="SynthosToken",
    contract_type="ERC20",
    network=ContractNetwork.ETHEREUM_MAINNET,
    constructor_args={"name": "SYNTHOS", "symbol": "SYN"}
)

# Deploy
success, address = manager.deploy_contract("0xdeployer", plan_id)

# Update configuration
success, msg = manager.update_configuration(
    "0xoperator",
    address,
    {"paused": False}
)

# Emergency pause
success, msg = manager.pause_contract(
    "0xoperator",
    address,
    reason="Security investigation"
)

# Resume
success, msg = manager.resume_contract("0xoperator", address)

# Health check
is_healthy, status = manager.perform_health_check(address)

# Query deployment
deployment = manager.get_deployment_status(plan_id)
config = manager.get_contract_configuration(address)
stats = manager.get_system_statistics()
```

## Gas & Cost Estimation

The system provides gas estimation for all operations:

```python
# SYNTHOS Token Contract
# - Basic transfer: ~21,000 gas
# - Token approval: ~46,000 gas
# - Snapshot creation: ~100,000 gas

# SYNTHOS Governance
# - Proposal submission: ~200,000 gas
# - Vote casting: ~100,000 gas
# - Proposal execution: ~500,000 gas (depends on actions)

# SYNTHOS Staking
# - Validator registration: ~250,000 gas
# - Delegation: ~150,000 gas
# - Reward distribution: ~200,000 gas

# Cross-chain operations: 1.5x-3x of base operation
```

## Security Considerations

1. **Access Control**: Role-based permissions prevent unauthorized operations
2. **Time-Locks**: Governance execution waits for minimum period
3. **Voting Power Snapshots**: Prevents voting power changes mid-election
4. **Unbonding Periods**: Prevents flash loan attacks on staking
5. **Slashing Cooldowns**: Prevents rapid-fire penalties
6. **Emergency Pause**: Can halt operations during security issues
7. **Audit Trails**: Complete history for compliance and investigation

## Testing

```python
# Example: Complete voting flow
from src.contracts.synthos import SynthosTokenContract, SynthosGovernanceContract

# Setup
token = SynthosTokenContract("0xowner")
gov = SynthosGovernanceContract("0xowner", token)

# Scenario: Community votes on protocol upgrade
# 1. Give voting power to community members
token.transfer("0xowner", "0xcommunity1", 10000 * 10**18, reason="grant_voting_power")
token.transfer("0xowner", "0xcommunity2", 5000 * 10**18, reason="grant_voting_power")

# 2. Create proposal
success, proposal_id = gov.propose(
    proposer="0xcommunity1",
    title="Protocol Upgrade to v2.0",
    description="Enable sharding and improve consensus",
    actions=[],
    current_block=1000
)

# 3. Community votes
gov.cast_vote("0xcommunity1", proposal_id, VoteType.FOR, reason="Support sharding")
gov.cast_vote("0xcommunity2", proposal_id, VoteType.FOR, reason="Improves throughput")

# 4. Voting period closes
gov.advance_block(blocks=100)

# 5. Queue for execution
gov.queue_proposal(proposal_id, 1100)

# 6. Execute after time-lock
gov.advance_block(blocks=86400)  # Wait 1 day
gov.execute_proposal(proposal_id)

# 7. Check results
proposal = gov.get_proposal(proposal_id)
assert proposal["state"] == "EXECUTED"
assert proposal["executed"] == True
```

## Summary

The Smart Contracts System provides:

- **SYNTHOS SynCoin**: 100B supply, agent-native, programmable, with governance integration
- **SYNTHOS Governance**: Complete DAO voting system with time-locked execution
- **SYNTHOS Staking**: Validator management with delegation and slashing
- **Deployment Manager**: Centralized orchestration across multiple networks

All contracts feature:
- Comprehensive audit trails
- Role-based access control
- Error handling with meaningful messages
- Configuration versioning
- Health monitoring
- Emergency controls

Total: **3000+ lines of production-ready smart contract code**
