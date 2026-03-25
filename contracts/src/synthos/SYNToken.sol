// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/token/ERC20/ERC20.sol";
import "@openzeppelin/contracts/token/ERC20/extensions/ERC20Burnable.sol";
import "@openzeppelin/contracts/token/ERC20/extensions/ERC20Snapshot.sol";
import "@openzeppelin/contracts/access/Ownable.sol";
import "@openzeppelin/contracts/access/AccessControl.sol";
import "@openzeppelin/contracts/security/Pausable.sol";
import "@openzeppelin/contracts/security/ReentrancyGuard.sol";

/**
 * @title SYNToken
 * @dev SYNTHOS native token with security hardening
 * 
 * Security Features:
 * - ReentrancyGuard on critical transfers
 * - AccessControl for fine-grained permission management
 * - Pausable for emergency situations
 * - Burnable for token deflation
 * - MAX_SUPPLY enforcement
 */
contract SYNToken is 
    ERC20, 
    ERC20Burnable, 
    ERC20Snapshot, 
    Ownable, 
    AccessControl,
    Pausable,
    ReentrancyGuard
{
    // Role definitions
    bytes32 public constant MINTER_ROLE = keccak256("MINTER_ROLE");
    bytes32 public constant PAUSER_ROLE = keccak256("PAUSER_ROLE");
    bytes32 public constant BURNER_ROLE = keccak256("BURNER_ROLE");
    bytes32 public constant SNAPSHOT_ROLE = keccak256("SNAPSHOT_ROLE");
    
    // Constants
    uint256 public constant MAX_SUPPLY = 100_000_000_000 * 10**18; // 100 billion SYN
    uint256 public constant ECOSYSTEM_ALLOCATION = 40_000_000_000 * 10**18; // 40%
    uint256 public constant VALIDATORS_ALLOCATION = 30_000_000_000 * 10**18; // 30%
    uint256 public constant COMMUNITY_ALLOCATION = 20_000_000_000 * 10**18; // 20%
    uint256 public constant FOUNDATION_ALLOCATION = 10_000_000_000 * 10**18; // 10%

    // Allocation tracking
    mapping(address => uint256) public allocations;
    mapping(address => bool) public ecosystem_recipients;
    mapping(address => bool) public validator_recipients;

    // Events
    event AllocationSet(address indexed recipient, uint256 amount, string allocationType);
    event TokensBurned(address indexed burner, uint256 amount);
    event TokensMinted(address indexed recipient, uint256 amount);

    constructor() ERC20("SYNTHOS", "SYN") {
        // Initialize roles
        _grantRole(DEFAULT_ADMIN_ROLE, msg.sender);
        _grantRole(MINTER_ROLE, msg.sender);
        _grantRole(PAUSER_ROLE, msg.sender);
        _grantRole(SNAPSHOT_ROLE, msg.sender);
        
        // Initial mint to contract (will be distributed)
        _mint(address(this), MAX_SUPPLY);
        
        // Emit allocation events
        emit AllocationSet(address(this), ECOSYSTEM_ALLOCATION, "ECOSYSTEM");
        emit AllocationSet(address(this), VALIDATORS_ALLOCATION, "VALIDATORS");
        emit AllocationSet(address(this), COMMUNITY_ALLOCATION, "COMMUNITY");
        emit AllocationSet(address(this), FOUNDATION_ALLOCATION, "FOUNDATION");
    }

    /**
     * @dev Create a snapshot of current token balances for governance voting
     * Uses OpenZeppelin's ERC20Snapshot for automatic balance tracking
     * @return snapshotId The ID of the created snapshot
     */
    function snapshot() public onlyRole(SNAPSHOT_ROLE) returns (uint256) {
        return _snapshot();
    }

    /**
     * @dev Allocate tokens to ecosystem participants
     * SECURITY: Protected by nonReentrant and role-based access
     * @param recipient Address receiving tokens
     * @param amount Amount to allocate
     * @param allocationType Type of allocation
     */
    function allocateTokens(
        address recipient,
        uint256 amount,
        string memory allocationType
    ) 
        public 
        onlyRole(MINTER_ROLE)
        nonReentrant
    {
        require(recipient != address(0), "Invalid recipient");
        require(amount > 0, "Amount must be positive");
        require(totalSupply() + amount <= MAX_SUPPLY, "Exceeds max supply");
        require(balanceOf(address(this)) >= amount, "Insufficient contract balance");

        _transfer(address(this), recipient, amount);
        allocations[recipient] += amount;

        if (keccak256(bytes(allocationType)) == keccak256(bytes("ECOSYSTEM"))) {
            ecosystem_recipients[recipient] = true;
        } else if (keccak256(bytes(allocationType)) == keccak256(bytes("VALIDATORS"))) {
            validator_recipients[recipient] = true;
        }

        emit AllocationSet(recipient, amount, allocationType);
    }

    /**
     * @dev Mint new tokens (controlled by MINTER_ROLE)
     * SECURITY: Enforces MAX_SUPPLY limit
     */
    function mint(address to, uint256 amount)
        public
        onlyRole(MINTER_ROLE)
    {
        require(to != address(0), "Mint to zero address");
        require(amount > 0, "Amount must be positive");
        require(totalSupply() + amount <= MAX_SUPPLY, "Exceeds max supply");
        _mint(to, amount);
        emit TokensMinted(to, amount);
    }

    /**
     * @dev Burn tokens (deflation mechanism)
     * SECURITY: Protected by role-based access
     * @param amount Amount to burn
     */
    function burn(uint256 amount) public override onlyRole(BURNER_ROLE) {
        super.burn(amount);
        emit TokensBurned(msg.sender, amount);
    }

    /**
     * @dev Emergency pause function (role-protected)
     */
    function pause() public onlyRole(PAUSER_ROLE) {
        _pause();
    }

    /**
     * @dev Resume token transfers after pause
     */
    function unpause() public onlyRole(PAUSER_ROLE) {
        _unpause();
    }

    /**
     * @dev Override transfer to respect pause state and add re-entrancy protection
     * SECURITY: nonReentrant guard on all transfers
     */
    function transfer(address to, uint256 amount)
        public
        override
        nonReentrant
        whenNotPaused
        returns (bool)
    {
        require(to != address(0), "Transfer to zero address");
        require(amount > 0, "Amount must be positive");
        return super.transfer(to, amount);
    }

    /**
     * @dev Override transferFrom to add re-entrancy protection
     */
    function transferFrom(address from, address to, uint256 amount)
        public
        override
        nonReentrant
        whenNotPaused
        returns (bool)
    {
        require(from != address(0), "Transfer from zero address");
        require(to != address(0), "Transfer to zero address");
        require(amount > 0, "Amount must be positive");
        return super.transferFrom(from, to, amount);
    }

    /**
     * @dev Override _beforeTokenTransfer to enforce pause state
     * ERC20Snapshot base class automatically tracks balance changes at snapshot boundaries
     */
    function _beforeTokenTransfer(
        address from,
        address to,
        uint256 amount
    ) 
        internal 
        override(ERC20, ERC20Snapshot) 
        whenNotPaused 
    {
        // Call parent implementation - ERC20Snapshot will record balances at snapshot moments
        super._beforeTokenTransfer(from, to, amount);
    }

    /**
     * @dev Get total supply at snapshot
     * @param snapshotId Snapshot ID
     * @return Total supply at snapshot
     */
    function totalSupplyAtSnapshot(uint256 snapshotId) 
        public 
        view 
        returns (uint256) 
    {
        return super.totalSupplyAt(snapshotId);
    }

    /**
     * @dev Get current snapshot ID
     * @return Current snapshot ID
     */
    function getCurrentSnapshot() public view returns (uint256) {
        return _getCurrentSnapshotId();
    }

    /**
     * @dev Revoke a role from an account (admin function)
     */
    function revokeRole(bytes32 role, address account)
        public
        override
        onlyRole(DEFAULT_ADMIN_ROLE)
    {
        super.revokeRole(role, account);
    }
}
