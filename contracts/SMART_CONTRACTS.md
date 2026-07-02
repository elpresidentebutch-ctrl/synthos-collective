# Smart Contract Documentation

## Overview

This document covers the complete smart contract suite for SYNTHOS. The contracts enable governance and staking.

---

## SYNTHOS Smart Contracts

### 0. SYNTHOSMultisig (Sovereign Admin)

**File**: `contracts/synthos/SYNTHOSMultisig.sol`  
**Purpose**: Launch custody for timelock administration, treasury control, and emergency admin actions.

#### Features

- **Fixed Owner Set**: Owners are declared at deployment for a small, auditable launch surface
- **Threshold Execution**: Transactions require `M-of-N` owner confirmations
- **Revocable Confirmations**: Owners can revoke before execution
- **Arbitrary Calls**: Can administer timelock, treasury, and ownable contracts
- **Open Execution**: Once threshold is met, any account can execute the approved transaction

#### Key Functions

```solidity
function submitTransaction(address target, uint256 value, bytes calldata data)
function confirmTransaction(uint256 transactionId)
function revokeConfirmation(uint256 transactionId)
function executeTransaction(uint256 transactionId)
function getTransaction(uint256 transactionId)
function owners()
```

#### Launch Custody Model

The deployment flow uses the sovereign multisig as the admin for
`SYNTHOSTimelock`. Governance is granted proposer/canceller rights on the
timelock, and execution is open once a queued operation matures. After bootstrap
configuration and genesis allocations, owner-controlled launch contracts are
transferred to the timelock:

- `SynCoin`
- `SYNTHOSAdopterRewards`
- `SYNTHOSDex`
- `SYNTHOSComplianceRegistry`

This leaves the deployer as a bootstrap actor only, not the continuing launch
administrator.

---

### 1. SynCoin (AI-native)

**File**: `contracts/synthos/SynCoin.sol`  
**Network**: SYNTHOS Chain  
**Total Supply**: 100 billion SYN tokens

#### Features

- **Standard ERC-20**: Full compliance with ERC20 interface
- **Treasury Recycling Burn**: Protocol spending burns half and recycles half to treasury
- **Snapshots**: Point-in-time balance snapshots for governance voting
- **Pausable**: Emergency pause functionality
- **Allocation System**: Structured token distribution

#### Token Allocations

| Category | Amount | Percentage | Purpose |
|----------|--------|------------|---------|
| Ecosystem | 400M | 40% | DeFi, partnerships, ecosystem development |
| Validators | 300M | 30% | Validator rewards and staking |
| Community | 200M | 20% | Community programs and airdrops |
| Foundation | 100M | 10% | Foundation operations and reserves |

#### Key Functions

```solidity
// Create snapshot for governance voting
function createSnapshot() public onlyOwner returns (uint256)

// Get balance at specific snapshot
function balanceOfAtSnapshot(address account, uint256 snapshotId) public view returns (uint256)

// Allocate tokens to ecosystem participants
function allocateTokens(address recipient, uint256 amount, string memory allocationType)

// Protocol spend sink: burns half, recycles half to treasury
function treasuryRecyclingBurn(uint256 amount, bytes32 spendType)

// Update the treasury destination for recycled spend
function setTreasury(address newTreasury)

// Approve or revoke a protocol spend category
function setTreasuryRecyclingSpendType(bytes32 spendType, bool approved)

// Emergency pause mechanism
function pause() / unpause()
```

#### Usage Example

```solidity
// Create snapshot for proposal voting
uint256 snapshotId = synCoin.createSnapshot();

// Get voting power at snapshot
uint256 votingPower = synCoin.balanceOfAtSnapshot(msg.sender, snapshotId);

// Allocate tokens
synCoin.allocateTokens(
    ecosystemPartner,
    1_000_000 * 10**18,
    "ECOSYSTEM"
);

// Spend SYN into the protocol sink
// 500 SYN is burned, 500 SYN returns to treasury
synCoin.treasuryRecyclingBurn(
    1_000 * 10**18,
    synCoin.SPEND_PROTOCOL()
);
```

Default approved Treasury Recycling Burn spend categories are:

- `SPEND_PROTOCOL`
- `SPEND_NODE_REGISTRATION`
- `SPEND_SERVICE_FEE`
- `SPEND_MARKETPLACE`

---

### 2. SYNTHOSGovernance (DAO)

**File**: `contracts/synthos/SYNTHOSGovernance.sol`  
**Network**: SYNTHOS Chain  
**Governance Model**: Delegative democracy with vote delegation

#### Features

- **Proposal Types**: 5 types of governance proposals
  - PROTOCOL_UPGRADE: Network upgrades with version management
  - PARAMETER_CHANGE: Economic and consensus parameter adjustments
  - TREASURY_ACTION: Fund allocation from treasury
  - EMERGENCY_ACTION: Time-critical governance actions
  - CONSTITUTIONAL_AMENDMENT: Constitution and rule changes

- **Voting Mechanism**:
  - Voting power based on token holdings
  - Vote delegation support
  - Supermajority requirement (66%+)
  - 3-day voting period
  - 2-day timelock before execution

- **Vote Types**: FOR, AGAINST, ABSTAIN

#### Constants

```solidity
uint256 public constant PROPOSAL_THRESHOLD = 100_000 * 10**18; // 100k SYN to propose
uint256 public constant VOTING_PERIOD = 3 days;
uint256 public constant EXECUTION_DELAY = 2 days;
uint256 public constant SUPERMAJORITY = 66; // 66% required
```

#### Key Functions

```solidity
// Create new proposal
function createProposal(
    ProposalType proposal_type,
    string memory title,
    string memory description,
    address[] memory targets,
    uint256[] memory values,
    string[] memory signatures,
    bytes[] memory calldatas
) returns (uint256 proposal_id)

// Delegate voting power
function delegateVotingPower(address delegate_addr)
function revokeDelegation()

// Cast vote on proposal
function castVote(uint256 proposal_id, uint8 vote)

// Queue proposal for execution
function queueProposal(uint256 proposal_id)

// Execute queued proposal
function executeProposal(uint256 proposal_id)

// Cancel proposal
function cancelProposal(uint256 proposal_id)

// Get proposal state
function getProposalState(uint256 proposal_id) returns (ProposalState)
```

#### Proposal Lifecycle

1. **PENDING** (1 block): Proposal created, voting not started
2. **ACTIVE** (3 days): Voting is open
3. **VOTING_CLOSED**: Voting has ended
4. **DEFEATED/SUCCEEDED**: Outcome determined by vote count
5. **QUEUED** (2 days): Timelock delay before execution
6. **EXECUTED**: Proposal executed on-chain

#### Usage Example

```solidity
// Create protocol upgrade proposal
uint256 proposalId = governance.createProposal(
    ProposalType.PROTOCOL_UPGRADE,
    "Upgrade to v2.0",
    "Major features: sharding, cross-chain bridges",
    [contractAddr],
    [0],
    ["upgradeToV2()"],
    [encodeFunction()]
);

// Delegate voting power
governance.delegateVotingPower(delegateAddress);

// Cast vote
governance.castVote(proposalId, 1); // 1 = FOR

// After voting period, queue
governance.queueProposal(proposalId);

// After timelock (2 days), execute
governance.executeProposal(proposalId);
```

---

### 3. SYNTHOSStaking

**File**: `contracts/synthos/SYNTHOSStaking.sol`  
**Network**: SYNTHOS Chain  
**Model**: Delegated Proof-of-Stake (DPoS)

#### Features

- **Validator Registration**: 100k SYN minimum stake
- **Delegation**: Users delegate to validators
- **Reward Distribution**: Epoch-based (1 day per epoch)
- **Slashing**: 10% penalty for misbehavior
- **Cooldown Period**: 7 days to unstake

#### Constants

```solidity
uint256 public constant MINIMUM_VALIDATOR_STAKE = 100_000 * 10**18;
uint256 public constant UNSTAKE_COOLDOWN = 7 days;
uint256 public constant SLASH_RATE = 10; // 10% slashing
```

#### Key Functions

```solidity
// Register as validator
function registerValidator(uint256 stake)

// Delegate to validator
function delegateToValidator(address validator, uint256 amount)

// Request unstaking
function requestUnstake(address validator, uint256 amount)

// Claim after cooldown
function claimUnstake(uint256 request_index)

// Distribute epoch rewards
function distributeRewards(uint256 reward_amount)

// Claim validator rewards
function claimRewards()

// Slash for misbehavior
function slash(address validator, uint256 amount, string memory reason)

// Advance to next epoch
function advanceEpoch()

// Calculate epoch rewards (5% annual inflation)
function calculateEpochRewards() returns (uint256)
```

#### Reward Structure

- **Annual Inflation**: 5% of total staked tokens
- **Epoch Duration**: 1 day
- **Distribution**: Equally distributed to all active validators
- **Claiming**: Validators claim accumulated rewards

#### Usage Example

```solidity
// Register validator with 100k SYN
staking.registerValidator(100_000 * 10**18);

// Delegate to validator
staking.delegateToValidator(validatorAddress, 50_000 * 10**18);

// Request unstaking after 10 days
staking.requestUnstake(validatorAddress, 25_000 * 10**18);

// Wait 7 days
await new Promise(resolve => setTimeout(resolve, 7 * 24 * 60 * 60 * 1000));

// Claim unstaked tokens
staking.claimUnstake(0);

// Claim rewards
staking.claimRewards();
```

---



---

### 4. RewardDistributor

**File**: `contracts/RewardDistributor.sol`  
**Networks**: SYNTHOS
**Model**: Unified vesting and reward distribution

#### Features

- **Vesting Schedules**: Linear vesting with cliff period
- **Immediate Rewards**: Direct reward distribution
- **Multi-token**: Support multiple reward tokens
- **Batch Distribution**: Distribute to many recipients at once
- **Claim Tracking**: History of all claims

#### Constants per Vesting

- Duration: Customizable (days/months/years)
- Cliff: No vesting before cliff period
- Linear: Proportional unlock after cliff

#### Key Functions

```solidity
// Token management
function approveToken(address token)
function revokeToken(address token)

// Vesting creation
function createVesting(
    address token,
    address beneficiary,
    uint256 total_amount,
    uint256 duration,
    uint256 cliff
) returns (bytes32 vesting_id)

// Vesting claiming
function calculateVestedAmount(bytes32 vesting_id) returns (uint256)
function claimVesting(bytes32 vesting_id) returns (uint256 amount_claimed)

// Immediate rewards
function batchDistributeRewards(
    address token,
    address[] calldata recipients,
    uint256[] calldata amounts,
    string calldata reward_type
)

function claimReward(uint256 reward_index)

// Queries
function getVestingDetails(bytes32 vesting_id) returns (...)
function getRewardDetails(uint256 reward_index) returns (...)
function getUserVestings(address user) returns (bytes32[])
```

#### Vesting Timeline Example

```
Day 0: Vesting created
Days 0-90: Cliff period (no vesting)
Days 90-365: Linear vesting (1.1% per day)
Day 365: All tokens vested

If duration = 365 days, cliff = 90 days:
- Day 90: 0 claimable
- Day 180: ~24% claimable (90/365 of vesting portion)
- Day 365: 100% claimable
```

#### Usage Example

```solidity
// Approve reward tokens
distributor.approveToken(syn);
// Batch distribute immediate rewards
distributor.batchDistributeRewards(
    syn,
    [addr1, addr2, addr3],
    [100 * 10**18, 200 * 10**18, 150 * 10**18],
    "VALIDATOR_BONUS"
);

// Claim reward
distributor.claimReward(0);
```

---

## Deployment Guide

### Prerequisites

- Solidity 0.8.20+
- Hardhat or Truffle
- OpenZeppelin Contracts

### Installation

```bash
npm install @openzeppelin/contracts
```

### Deployment Order

#### SYNTHOS Network

1. **Deploy SynCoin**
   ```solidity
    SynCoin syn = new SynCoin();
   ```

2. **Deploy SYNTHOSGovernance**
   ```solidity
   SYNTHOSGovernance gov = new SYNTHOSGovernance(
       address(syn),
       timeLock
   );
   ```

3. **Deploy SYNTHOSStaking**
   ```solidity
   SYNTHOSStaking staking = new SYNTHOSStaking(
       address(syn),
       address(gov)
   );
   ```

4. **Set Ownership to Governance**
   ```solidity
   syn.transferOwnership(address(gov));
   ```



## Security Considerations

### Audit Recommendations

- [ ] Internal audit of all contracts
- [ ] External security audit by professional firm
- [ ] Formal verification of core logic
- [ ] Fuzzing and invariant testing

### Key Security Features

1. **Access Control**: `onlyOwner` guards on sensitive functions
2. **Reentrancy Protection**: Checks-effects-interactions pattern
3. **Integer Overflow/Underflow**: Safe math with Solidity 0.8.20+
4. **Input Validation**: All parameters validated
5. **Emergency Pause**: Pausable pattern for tokens and bridge

### Risk Mitigation

- Start with conservative parameters
- Gradual increase of limits after monitoring
- Multi-sig governance for parameter changes
- Regular security updates and patches
- Community oversight of governance decisions

---

## Testing

### Unit Tests

Test coverage includes:
- Token transfer and burning
- Governance voting and execution
- Staking and unstaking mechanisms
- Bridge transfer validation
- AMM swap calculations
- Oracle price aggregation
- Vesting schedule execution

### Integration Tests

- End-to-end governance flow
- Cross-chain token transfer
- Multi-step staking rewards
- Complex swap scenarios
- Oracle price feed updates

### Test Command

```bash
npx hardhat test
```

---

## References

- [ERC-20 Token Standard](https://eips.ethereum.org/EIPS/eip-20)
- [OpenZeppelin Contracts](https://docs.openzeppelin.com/contracts/)
- [Uniswap V2 Whitepaper](https://uniswap.org/whitepaper.pdf)
- [Delegated Proof of Stake](https://en.wikipedia.org/wiki/Proof_of_stake#Delegated_proof_of_stake)

---

## Support & Contributions

For questions or contributions:
- Create issues in the repository
- Submit pull requests with improvements
- Contact governance team for major changes
