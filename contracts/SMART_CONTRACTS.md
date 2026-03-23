# Smart Contract Documentation

## Overview

This document covers the complete smart contract suite for SYNTHOS and Gemini Megachain 2.0. The contracts enable governance, staking, cross-chain functionality, and decentralized finance operations.

---

## SYNTHOS Smart Contracts

### 1. SYNToken (ERC-20)

**File**: `contracts/src/synthos/SYNToken.sol`  
**Network**: SYNTHOS Chain  
**Total Supply**: 1 billion SYN tokens

#### Features

- **Standard ERC-20**: Full compliance with ERC20 interface
- **Burnable**: Token burning for deflationary mechanisms
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

// Burn tokens (deflationary mechanism)
function burn(uint256 amount)

// Emergency pause mechanism
function pause() / unpause()
```

#### Usage Example

```solidity
// Create snapshot for proposal voting
uint256 snapshotId = synToken.createSnapshot();

// Get voting power at snapshot
uint256 votingPower = synToken.balanceOfAtSnapshot(msg.sender, snapshotId);

// Allocate tokens
synToken.allocateTokens(
    ecosystemPartner,
    1_000_000 * 10**18,
    "ECOSYSTEM"
);
```

---

### 2. SYNTHOSGovernance (DAO)

**File**: `contracts/src/synthos/SYNTHOSGovernance.sol`  
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

**File**: `contracts/src/synthos/SYNTHOSStaking.sol`  
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

## Gemini Megachain 2.0 Smart Contracts

### 1. GEMToken (ERC-20)

**File**: `contracts/gemini/GEMToken.sol`  
**Network**: Gemini Megachain 2.0 (Multi-chain as secondary)  
**Total Supply**: 2 billion GEM tokens

#### Features

- **Multi-chain Support**: Bridge support for cross-chain transfers
- **Fee-on-Transfer**: 0.25% fee to ecosystem fund
- **Burnable**: Deflation mechanism
- **Pausable**: Emergency controls
- **Bridge Integration**: Lock-and-mint cross-chain mechanism

#### Constants

```solidity
uint256 public constant INITIAL_SUPPLY = 2_000_000_000 * 10**18;
uint256 public constant ECOSYSTEM_FEE = 25; // 0.25% = 25/10000
```

#### Key Functions

```solidity
// Standard ERC-20 with fee
function transfer(address to, uint256 amount) returns (bool)
function transferFrom(address from, address to, uint256 amount) returns (bool)

// Set ecosystem fund recipient
function setEcosystemFund(address new_fund)

// Set fee exemption for address (governance, bridges)
function setFeeExemption(address account, bool exempt)

// Set bridge contract
function setBridgeContract(address bridge_addr)

// Mint from bridge (cross-chain)
function bridgeMint(address recipient, uint256 amount, uint256 source_chain_id)

// Burn for cross-chain transfer
function bridgeBurn(uint256 amount, uint256 destination_chain_id)

// Emergency controls
function pause() / unpause()
```

#### Fee Distribution

- Every transfer has 0.25% fee
- Fee collected in ecosystem fund
- Used for incentives, development, and grants
- Fee exempt: governance contracts, bridges, ecosystem fund

#### Usage Example

```solidity
// Transfer with automatic fee
uint256 received = gem.transfer(recipient, 1000 * 10**18);
// Recipient receives ~997.5 GEM, 2.5 GEM to ecosystem fund

// Set bridge for cross-chain
gem.setBridgeContract(bridgeAddress);

// Exempt bridge from fee
gem.setFeeExemption(bridgeAddress, true);

// Cross-chain mint from bridge
gem.bridgeMint(recipient, 100 * 10**18, 1); // From chain 1

// Cross-chain burn
gem.bridgeBurn(100 * 10**18, 137); // To Polygon
```

---

### 2. CrossChainBridge

**File**: `contracts/gemini/CrossChainBridge.sol`  
**Networks**: SYNTHOS Chain and Gemini Megachain 2.0  
**Model**: Lock-and-mint bridging with validator confirmation

#### Features

- **Multi-chain Support**: Supports up to 4 networks
  - SYNTHOS (1234)
  - Gemini Megachain 2.0 (2048)
  - Ethereum (1)
  - Polygon (137)

- **Validator Verification**: Multiple validators confirm transfers
- **Challenge Window**: 1 day challenge period for fraud detection
- **Fee**: 0.25% bridge fee
- **Rate Limiting**: 1-minute cooldown between transfers per user

#### Chain IDs

```solidity
uint256 public constant SYNTHOS_CHAIN_ID = 1234;
uint256 public constant GEMINI_CHAIN_ID = 2048;
uint256 public constant ETHEREUM_CHAIN_ID = 1;
uint256 public constant POLYGON_CHAIN_ID = 137;
```

#### Transfer States

```
PENDING → CONFIRMED → EXECUTED
     ↓
CHALLENGED → FAILED
```

#### Key Functions

```solidity
// Validator management
function addValidator(address validator_addr)
function removeValidator(address validator_addr)

// Initiate cross-chain transfer
function initiateTransfer(
    address recipient,
    uint256 amount,
    uint256 destination_chain_id
) payable returns (uint256 transfer_id)

// Validator confirms transfer
function confirmTransfer(uint256 transfer_id)

// Execute confirmed transfer
function executeTransfer(uint256 transfer_id)

// Challenge suspicious transfer
function challengeTransfer(uint256 transfer_id)

// Bridge configuration
function registerToken(uint256 chain_id, address token_addr)
function setRequiredSignatures(uint256 num_signatures)
```

#### Usage Example

```solidity
// Add validators
bridge.addValidator(validator1);
bridge.addValidator(validator2);
bridge.addValidator(validator3);

// Set required signatures (2-of-3 multisig)
bridge.setRequiredSignatures(2);

// Initiate cross-chain transfer
uint256 transferId = bridge.initiateTransfer{
    value: calculateBridgeFee(1000 * 10**18)
}(
    recipient,
    1000 * 10**18,
    137 // To Polygon
);

// Validators confirm
validator1.confirmTransfer(transferId);
validator2.confirmTransfer(transferId);

// Execute after confirmation
bridge.executeTransfer(transferId);
```

---

### 3. GeminiLiquidityPool (AMM)

**File**: `contracts/gemini/GeminiLiquidityPool.sol`  
**Network**: Gemini Megachain 2.0  
**Model**: Automated Market Maker (AMM) with constant product formula

#### Features

- **Constant Product Formula**: x * y = k
- **LP Tokens**: Liquidity provider shares
- **Slippage Protection**: Minimum output amount
- **Fee Mechanism**: 0.25% fee on swaps
- **Multi-pool Support**: Unlimited token pairs

#### Constants

```solidity
uint256 public constant FEE_PERCENT = 25; // 0.25% = 25/10000
uint256 public constant MINIMUM_LIQUIDITY = 1000; // Anti-rug pull
```

#### Pool Structure

Each pool consists of:
- Two ERC-20 tokens
- Liquidity reserves for each token
- LP token shares
- Fee accumulated

#### Key Functions

```solidity
// Pool management
function createPool(
    address token_a,
    address token_b,
    uint256 amount_a,
    uint256 amount_b
) returns (bytes32 pool_id)

// Liquidity provision
function addLiquidity(
    address token_a,
    address token_b,
    uint256 amount_a,
    uint256 amount_b
) returns (uint256 lp_shares)

function removeLiquidity(
    address token_a,
    address token_b,
    uint256 lp_shares
) returns (uint256 amount_a, uint256 amount_b)

// Trading
function swap(
    address token_in,
    address token_out,
    uint256 amount_in,
    uint256 min_amount_out
) returns (uint256 amount_out)

// Price information
function getPrice(address token_in, address token_out, uint256 amount_in) returns (uint256)
function getPoolDetails(address token_a, address token_b) returns (...)

// Fee collection
function collectFees(address token_a, address token_b)
```

#### Pricing Formula

```
Price = (input_amount * (1 - 0.0025)) * (reserve_y / (reserve_x + input_amount * (1 - 0.0025)))
```

#### Usage Example

```solidity
// Create GEM/USDC pool
bytes32 poolId = pool.createPool(
    gemToken,
    usdcToken,
    1_000_000 * 10**18, // 1M GEM
    1_000_000 * 10**6   // 1M USDC
);

// Add liquidity (500k GEM, 500k USDC)
uint256 lpShares = pool.addLiquidity(
    gemToken,
    usdcToken,
    500_000 * 10**18,
    500_000 * 10**6
);

// Check price: How much USDC per GEM?
uint256 price = pool.getPrice(
    gemToken,
    usdcToken,
    1 * 10**18 // 1 GEM
);

// Swap 100 GEM for USDC
uint256 minOut = 99 * 10**6; // Min 99 USDC
uint256 usdcReceived = pool.swap(
    gemToken,
    usdcToken,
    100 * 10**18,
    minOut
);

// Remove liquidity
(uint256 gemBack, uint256 usdcBack) = pool.removeLiquidity(
    gemToken,
    usdcToken,
    lpShares / 2 // Remove half
);
```

---

### 4. GeminiOracle

**File**: `contracts/gemini/GeminiOracle.sol`  
**Network**: Gemini Megachain 2.0  
**Model**: Decentralized oracle with price aggregation

#### Features

- **Multiple Providers**: Decentralized oracle network
- **Median Calculation**: Robust price aggregation
- **TWAP**: Time-weighted average prices
- **Staleness Detection**: 1-hour freshness requirement
- **Price History**: Track historical prices

#### Provider Requirements

- Minimum 10k ether stake
- Active provider status
- Regular price updates

#### Key Functions

```solidity
// Provider management
function registerProvider() payable
function unregisterProvider()

// Price updates
function createPriceFeed(
    address base_token,
    address quote_token,
    uint256 initial_price,
    uint256 decimals
)

function updatePrice(
    address base_token,
    address quote_token,
    uint256 price,
    uint256 decimals
)

// Price queries
function getMedianPrice(address base_token, address quote_token)
    returns (uint256 median_price, uint256 decimals)

function getTWAP(
    address base_token,
    address quote_token,
    uint256 time_window
) returns (uint256 twap_price)

function getPrice(address base_token, address quote_token)
    returns (uint256, uint256)

// Information
function getProviderCount(address base_token, address quote_token) returns (uint256)
```

#### Usage Example

```solidity
// Register as price provider (10k ETH minimum)
oracle.registerProvider{value: 10_000 ether}();

// Create price feed for GEM/USD
oracle.createPriceFeed(
    gem,
    usd,
    1_50 * 10**6, // $1.50
    6 // 6 decimals for price
);

// Update price
oracle.updatePrice(
    gem,
    usd,
    1_48 * 10**6,
    6
);

// Get median price
(uint256 medianPrice, uint256 decimals) = oracle.getMedianPrice(gem, usd);

// Get 1-hour TWAP
uint256 twap = oracle.getTWAP(gem, usd, 1 hours);

// Unregister when done
oracle.unregisterProvider();
```

---

### 5. RewardDistributor

**File**: `contracts/RewardDistributor.sol`  
**Networks**: Shared by both SYNTHOS and Gemini  
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
distributor.approveToken(gem);

// Create 1-year vesting with 90-day cliff
bytes32 vestingId = distributor.createVesting(
    syn,
    beneficiary,
    1_000_000 * 10**18, // 1M SYN
    365 days,
    90 days
);

// Check vested amount
uint256 vested = distributor.calculateVestedAmount(vestingId);

// Claim vested tokens
uint256 claimed = distributor.claimVesting(vestingId);

// Batch distribute immediate rewards
distributor.batchDistributeRewards(
    gem,
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

1. **Deploy SYNToken**
   ```solidity
   SYNToken syn = new SYNToken();
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

#### Gemini Megachain 2.0

1. **Deploy GEMToken**
   ```solidity
   GEMToken gem = new GEMToken(ecosystemFund);
   ```

2. **Deploy CrossChainBridge**
   ```solidity
   CrossChainBridge bridge = new CrossChainBridge();
   bridge.registerToken(SYNTHOS_CHAIN_ID, synToken);
   bridge.registerToken(GEMINI_CHAIN_ID, gem);
   ```

3. **Deploy GeminiLiquidityPool**
   ```solidity
   GeminiLiquidityPool pool = new GeminiLiquidityPool();
   ```

4. **Deploy GeminiOracle**
   ```solidity
   GeminiOracle oracle = new GeminiOracle();
   ```

5. **Deploy RewardDistributor** (Shared)
   ```solidity
   RewardDistributor rewards = new RewardDistributor(governance);
   rewards.approveToken(address(syn));
   rewards.approveToken(address(gem));
   ```

---

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
