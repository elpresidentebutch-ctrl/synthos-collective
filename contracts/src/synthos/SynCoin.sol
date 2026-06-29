// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/access/Ownable.sol";
import "@openzeppelin/contracts/token/ERC20/ERC20.sol";
import "@openzeppelin/contracts/token/ERC20/extensions/ERC20Burnable.sol";
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
contract SynCoin is ERC20, ERC20Burnable, ERC20Pausable, ERC20Snapshot, Ownable {
    uint256 public constant INITIAL_SUPPLY = 100_000_000_000 * 10 ** 18;

    uint256 public constant IMMUNE_NODE_REWARDS_ALLOCATION = 22_000_000_000 * 10 ** 18;
    uint256 public constant LOCKED_DEX_LIQUIDITY_ALLOCATION = 20_000_000_000 * 10 ** 18;
    uint256 public constant FOUNDER_VESTING_ALLOCATION = 17_000_000_000 * 10 ** 18;
    uint256 public constant VALIDATOR_REWARDS_ALLOCATION = 12_000_000_000 * 10 ** 18;
    uint256 public constant COMMUNITY_ALLOCATION = 12_500_000_000 * 10 ** 18;
    uint256 public constant ECOSYSTEM_TREASURY_ALLOCATION = 10_000_000_000 * 10 ** 18;
    uint256 public constant CMO_LAUNCH_GRANT = 3_000_000_000 * 10 ** 18;
    uint256 public constant STRATEGIC_RESERVE_ALLOCATION = 3_000_000_000 * 10 ** 18;
    uint256 public constant FOUNDER_OPERATIONS_GRANT = 500_000_000 * 10 ** 18;

    uint256 public constant FOUNDER_ANNUAL_RELEASE = 1_700_000_000 * 10 ** 18;
    uint256 public constant FOUNDER_RELEASE_COUNT = 10;
    uint256 public constant FOUNDER_FIRST_RELEASE_YEAR = 2027;
    uint256 public constant FOUNDER_FIRST_RELEASE_MONTH = 5;
    uint256 public constant FOUNDER_FIRST_RELEASE_DAY = 29;

    mapping(string => uint256) public allocatedByType;

    event TokensAllocated(
        address indexed recipient,
        uint256 amount,
        string allocationType
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

    function _beforeTokenTransfer(
        address from,
        address to,
        uint256 amount
    ) internal override(ERC20, ERC20Pausable, ERC20Snapshot) {
        super._beforeTokenTransfer(from, to, amount);
    }
}
