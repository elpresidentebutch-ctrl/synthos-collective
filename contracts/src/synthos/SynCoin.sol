// SPDX-License-Identifier: Apache-2.0
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/access/Ownable.sol";
import "@openzeppelin/contracts/token/ERC20/ERC20.sol";
import "@openzeppelin/contracts/token/ERC20/extensions/ERC20Pausable.sol";
import "@openzeppelin/contracts/token/ERC20/extensions/ERC20Snapshot.sol";

/**
 * @title SynCoin
 * @dev Canonical ERC-20 for SYNTHOS tokenomics.
 *
 * The entire 100B SYN supply is minted once at genesis to this contract.
 * Distribution is then performed by the owner with allocation accounting.
 * Production ownership should be transferred to governance, a timelock, or
 * a multisig before any public launch.
 */
contract SynCoin is ERC20, ERC20Pausable, ERC20Snapshot, Ownable {
    uint256 public constant INITIAL_SUPPLY = 100_000_000_000 * 10 ** 18;

    uint256 public constant IMMUNE_NODE_REWARDS_ALLOCATION = 22_000_000_000 * 10 ** 18;
    uint256 public constant LOCKED_DEX_LIQUIDITY_ALLOCATION = 20_000_000_000 * 10 ** 18;
    uint256 public constant FOUNDER_VESTING_ALLOCATION = 17_000_000_000 * 10 ** 18;
    uint256 public constant VALIDATOR_REWARDS_ALLOCATION = 12_000_000_000 * 10 ** 18;
    uint256 public constant COMMUNITY_ALLOCATION = 12_500_000_000 * 10 ** 18;
    uint256 public constant ECOSYSTEM_TREASURY_ALLOCATION = 13_000_000_000 * 10 ** 18;
    uint256 public constant CMO_LAUNCH_GRANT = 0;
    uint256 public constant STRATEGIC_RESERVE_ALLOCATION = 3_000_000_000 * 10 ** 18;
    uint256 public constant FOUNDER_OPERATIONS_GRANT = 500_000_000 * 10 ** 18;

    uint256 public constant IMMUNE_STANDARD_HEARTBEAT_REWARDS = 15_000_000_000 * 10 ** 18;
    uint256 public constant IMMUNE_EARLY_OPERATOR_REWARDS = 500_000_000 * 10 ** 18;
    uint256 public constant IMMUNE_RELIABILITY_BONUSES = 2_000_000_000 * 10 ** 18;
    uint256 public constant IMMUNE_FUTURE_EXPANSION = 3_000_000_000 * 10 ** 18;
    uint256 public constant IMMUNE_FRAUD_GOVERNANCE_RESERVE = 1_500_000_000 * 10 ** 18;

    uint256 public constant VALIDATOR_UPTIME_FINALITY_REWARDS = 5_000_000_000 * 10 ** 18;
    uint256 public constant VALIDATOR_STAKING_DELEGATION_REWARDS = 3_000_000_000 * 10 ** 18;
    uint256 public constant VALIDATOR_SECURITY_INCENTIVES = 1_000_000_000 * 10 ** 18;
    uint256 public constant VALIDATOR_TESTNET_MAINNET_MIGRATION = 1_000_000_000 * 10 ** 18;
    uint256 public constant VALIDATOR_LONG_TERM_RESERVE = 2_000_000_000 * 10 ** 18;

    uint256 public constant COMMUNITY_TESTNET_PARTICIPATION = 2_000_000_000 * 10 ** 18;
    uint256 public constant COMMUNITY_BUILDER_GRANTS = 2_500_000_000 * 10 ** 18;
    uint256 public constant COMMUNITY_AMBASSADOR_EDUCATION = 1_500_000_000 * 10 ** 18;
    uint256 public constant COMMUNITY_BUG_DOCS_QA = 1_500_000_000 * 10 ** 18;
    uint256 public constant COMMUNITY_EARLY_ADOPTER_CAMPAIGNS = 2_000_000_000 * 10 ** 18;
    uint256 public constant COMMUNITY_RETRO_PUBLIC_GOODS = 1_000_000_000 * 10 ** 18;
    uint256 public constant COMMUNITY_RESERVE = 2_000_000_000 * 10 ** 18;

    uint256 public constant FOUNDER_ANNUAL_RELEASE = 1_700_000_000 * 10 ** 18;
    uint256 public constant FOUNDER_RELEASE_COUNT = 10;
    uint256 public constant FOUNDER_FIRST_RELEASE_YEAR = 2027;
    uint256 public constant FOUNDER_FIRST_RELEASE_MONTH = 5;
    uint256 public constant FOUNDER_FIRST_RELEASE_DAY = 29;

    bytes32 public constant SPEND_PROTOCOL = keccak256("PROTOCOL_SPEND");
    bytes32 public constant SPEND_NODE_REGISTRATION = keccak256("NODE_REGISTRATION");
    bytes32 public constant SPEND_SERVICE_FEE = keccak256("SERVICE_FEE");
    bytes32 public constant SPEND_MARKETPLACE = keccak256("MARKETPLACE");

    address public treasury;

    uint256 public totalTreasuryRecyclingBurned;
    uint256 public totalTreasuryRecycled;

    mapping(string => uint256) public allocatedByType;
    mapping(bytes32 => bool) public approvedTreasuryRecyclingSpendTypes;
    mapping(bytes32 => uint256) public treasuryRecyclingBurnedByType;
    mapping(bytes32 => uint256) public treasuryRecycledByType;

    event TokensAllocated(
        address indexed recipient,
        uint256 amount,
        string allocationType
    );

    event TreasuryUpdated(address indexed previousTreasury, address indexed newTreasury);
    event TreasuryRecyclingSpendTypeUpdated(bytes32 indexed spendType, bool approved);

    event TreasuryRecyclingBurn(
        address indexed spender,
        address indexed treasury,
        uint256 amount,
        uint256 burnedAmount,
        uint256 recycledAmount,
        bytes32 indexed spendType
    );

    event GenesisAllocationDeclared(string allocationType, uint256 amount);

    constructor() ERC20("SYNTHOS", "SYN") {
        uint256 allocationTotal = IMMUNE_NODE_REWARDS_ALLOCATION
            + LOCKED_DEX_LIQUIDITY_ALLOCATION
            + FOUNDER_VESTING_ALLOCATION
            + VALIDATOR_REWARDS_ALLOCATION
            + COMMUNITY_ALLOCATION
            + ECOSYSTEM_TREASURY_ALLOCATION
            + CMO_LAUNCH_GRANT
            + STRATEGIC_RESERVE_ALLOCATION
            + FOUNDER_OPERATIONS_GRANT;
        require(allocationTotal == INITIAL_SUPPLY, "allocation total mismatch");
        require(immuneRewardsBreakdownTotal() == IMMUNE_NODE_REWARDS_ALLOCATION, "immune breakdown mismatch");
        require(validatorRewardsBreakdownTotal() == VALIDATOR_REWARDS_ALLOCATION, "validator breakdown mismatch");
        require(communityRewardsBreakdownTotal() == COMMUNITY_ALLOCATION, "community breakdown mismatch");

        treasury = _msgSender();
        _setTreasuryRecyclingSpendType(SPEND_PROTOCOL, true);
        _setTreasuryRecyclingSpendType(SPEND_NODE_REGISTRATION, true);
        _setTreasuryRecyclingSpendType(SPEND_SERVICE_FEE, true);
        _setTreasuryRecyclingSpendType(SPEND_MARKETPLACE, true);
        _mint(address(this), INITIAL_SUPPLY);

        emit GenesisAllocationDeclared("IMMUNE_NODE_REWARDS", IMMUNE_NODE_REWARDS_ALLOCATION);
        emit GenesisAllocationDeclared("LOCKED_DEX_LIQUIDITY", LOCKED_DEX_LIQUIDITY_ALLOCATION);
        emit GenesisAllocationDeclared("FOUNDER_VESTING", FOUNDER_VESTING_ALLOCATION);
        emit GenesisAllocationDeclared("VALIDATOR_REWARDS", VALIDATOR_REWARDS_ALLOCATION);
        emit GenesisAllocationDeclared("COMMUNITY", COMMUNITY_ALLOCATION);
        emit GenesisAllocationDeclared("ECOSYSTEM_TREASURY", ECOSYSTEM_TREASURY_ALLOCATION);
        emit GenesisAllocationDeclared("CMO_LAUNCH_GRANT", CMO_LAUNCH_GRANT);
        emit GenesisAllocationDeclared("STRATEGIC_RESERVE", STRATEGIC_RESERVE_ALLOCATION);
        emit GenesisAllocationDeclared("FOUNDER_OPERATIONS_GRANT", FOUNDER_OPERATIONS_GRANT);
    }

    function allocateTokens(
        address recipient,
        uint256 amount,
        string calldata allocationType
    ) external onlyOwner {
        require(recipient != address(0), "invalid recipient");
        require(amount > 0, "amount must be positive");
        require(balanceOf(address(this)) >= amount, "insufficient undistributed supply");

        allocatedByType[allocationType] += amount;
        _transfer(address(this), recipient, amount);

        emit TokensAllocated(recipient, amount, allocationType);
    }

    function setTreasury(address newTreasury) external onlyOwner {
        require(newTreasury != address(0), "invalid treasury");

        address previousTreasury = treasury;
        treasury = newTreasury;

        emit TreasuryUpdated(previousTreasury, newTreasury);
    }

    function setTreasuryRecyclingSpendType(
        bytes32 spendType,
        bool approved
    ) external onlyOwner {
        require(spendType != bytes32(0), "invalid spend type");

        _setTreasuryRecyclingSpendType(spendType, approved);
    }

    function treasuryRecyclingBurn(
        uint256 amount,
        bytes32 spendType
    ) external {
        require(amount > 1, "amount too small");
        require(treasury != address(0), "treasury not set");
        require(balanceOf(_msgSender()) >= amount, "insufficient balance");
        require(approvedTreasuryRecyclingSpendTypes[spendType], "spend type not approved");

        uint256 burnedAmount = amount / 2;
        uint256 recycledAmount = amount - burnedAmount;

        totalTreasuryRecyclingBurned += burnedAmount;
        totalTreasuryRecycled += recycledAmount;
        treasuryRecyclingBurnedByType[spendType] += burnedAmount;
        treasuryRecycledByType[spendType] += recycledAmount;
        _burn(_msgSender(), burnedAmount);
        _transfer(_msgSender(), treasury, recycledAmount);

        emit TreasuryRecyclingBurn(
            _msgSender(),
            treasury,
            amount,
            burnedAmount,
            recycledAmount,
            spendType
        );
    }

    function _setTreasuryRecyclingSpendType(
        bytes32 spendType,
        bool approved
    ) internal {
        approvedTreasuryRecyclingSpendTypes[spendType] = approved;
        emit TreasuryRecyclingSpendTypeUpdated(spendType, approved);
    }

    function pause() external onlyOwner {
        _pause();
    }

    function unpause() external onlyOwner {
        _unpause();
    }

    function createSnapshot() external onlyOwner returns (uint256) {
        return _snapshot();
    }

    function undistributedSupply() external view returns (uint256) {
        return balanceOf(address(this));
    }

    function tokenomicsTotal() external pure returns (uint256) {
        return IMMUNE_NODE_REWARDS_ALLOCATION
            + LOCKED_DEX_LIQUIDITY_ALLOCATION
            + FOUNDER_VESTING_ALLOCATION
            + VALIDATOR_REWARDS_ALLOCATION
            + COMMUNITY_ALLOCATION
            + ECOSYSTEM_TREASURY_ALLOCATION
            + CMO_LAUNCH_GRANT
            + STRATEGIC_RESERVE_ALLOCATION
            + FOUNDER_OPERATIONS_GRANT;
    }

    function immuneRewardsBreakdownTotal() public pure returns (uint256) {
        return IMMUNE_STANDARD_HEARTBEAT_REWARDS
            + IMMUNE_EARLY_OPERATOR_REWARDS
            + IMMUNE_RELIABILITY_BONUSES
            + IMMUNE_FUTURE_EXPANSION
            + IMMUNE_FRAUD_GOVERNANCE_RESERVE;
    }

    function validatorRewardsBreakdownTotal() public pure returns (uint256) {
        return VALIDATOR_UPTIME_FINALITY_REWARDS
            + VALIDATOR_STAKING_DELEGATION_REWARDS
            + VALIDATOR_SECURITY_INCENTIVES
            + VALIDATOR_TESTNET_MAINNET_MIGRATION
            + VALIDATOR_LONG_TERM_RESERVE;
    }

    function communityRewardsBreakdownTotal() public pure returns (uint256) {
        return COMMUNITY_TESTNET_PARTICIPATION
            + COMMUNITY_BUILDER_GRANTS
            + COMMUNITY_AMBASSADOR_EDUCATION
            + COMMUNITY_BUG_DOCS_QA
            + COMMUNITY_EARLY_ADOPTER_CAMPAIGNS
            + COMMUNITY_RETRO_PUBLIC_GOODS
            + COMMUNITY_RESERVE;
    }

    function _beforeTokenTransfer(
        address from,
        address to,
        uint256 amount
    ) internal override(ERC20, ERC20Pausable, ERC20Snapshot) {
        super._beforeTokenTransfer(from, to, amount);
    }
}
