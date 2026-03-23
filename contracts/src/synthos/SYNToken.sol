// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/token/ERC20/ERC20.sol";
import "@openzeppelin/contracts/token/ERC20/extensions/ERC20Burnable.sol";
import "@openzeppelin/contracts/token/ERC20/extensions/ERC20Snapshot.sol";
import "@openzeppelin/contracts/access/Ownable.sol";
import "@openzeppelin/contracts/security/Pausable.sol";

/**
 * @title SYNToken
 * @dev SYNTHOS native token implementing ERC-20 with governance features
 * 
 * Features:
 * - Standard ERC-20 token interface
 * - Burnable tokens for deflation
 * - Snapshot capability for governance voting
 * - Pausable for emergency situations
 * - Owner controls (governance contract becomes owner)
 */
contract SYNToken is ERC20, ERC20Burnable, ERC20Snapshot, Ownable, Pausable {
    // Constants
    uint256 public constant INITIAL_SUPPLY = 100_000_000_000 * 10**18; // 100 billion SYN
    uint256 public constant ECOSYSTEM_ALLOCATION = 40_000_000_000 * 10**18; // 40%
    uint256 public constant VALIDATORS_ALLOCATION = 30_000_000_000 * 10**18; // 30%
    uint256 public constant COMMUNITY_ALLOCATION = 20_000_000_000 * 10**18; // 20%
    uint256 public constant FOUNDATION_ALLOCATION = 10_000_000_000 * 10**18; // 10%

    // Allocation tracking
    mapping(address => uint256) public allocations;
    mapping(address => bool) public ecosystem_recipients;
    mapping(address => bool) public validator_recipients;

    // Snapshot history
    uint256 private current_snapshot;
    mapping(uint256 => mapping(address => uint256)) private snapshots;

    // Events
    event SnapshotCreated(uint256 indexed snapshotId, uint256 timestamp);
    event AllocationSet(address indexed recipient, uint256 amount, string allocationType);
    event TokensBurned(address indexed burner, uint256 amount);
    event TokensMinted(address indexed recipient, uint256 amount);

    constructor() ERC20("SYNTHOS", "SYN") {
        current_snapshot = 0;
        
        // Initial mint to contract (will be distributed)
        _mint(address(this), INITIAL_SUPPLY);
        
        // Emit allocation events
        emit AllocationSet(address(this), ECOSYSTEM_ALLOCATION, "ECOSYSTEM");
        emit AllocationSet(address(this), VALIDATORS_ALLOCATION, "VALIDATORS");
        emit AllocationSet(address(this), COMMUNITY_ALLOCATION, "COMMUNITY");
        emit AllocationSet(address(this), FOUNDATION_ALLOCATION, "FOUNDATION");
    }

    /**
     * @dev Create a snapshot of current token balances for governance voting
     * @return snapshotId The ID of the created snapshot
     */
    function createSnapshot() public onlyOwner returns (uint256) {
        current_snapshot++;
        emit SnapshotCreated(current_snapshot, block.timestamp);
        return current_snapshot;
    }

    /**
     * @dev Get balance at specific snapshot for governance voting
     * @param account Address to check balance for
     * @param snapshotId Snapshot ID to query
     * @return Balance at snapshot
     */
    function balanceOfAtSnapshot(address account, uint256 snapshotId) 
        public 
        view 
        returns (uint256) 
    {
        require(snapshotId <= current_snapshot, "Invalid snapshot ID");
        require(snapshotId > 0, "Snapshot must be positive");
        
        return snapshots[snapshotId][account];
    }

    /**
     * @dev Allocate tokens to ecosystem participants
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
        onlyOwner 
    {
        require(recipient != address(0), "Invalid recipient");
        require(amount > 0, "Amount must be positive");
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
     * @dev Burn tokens (deflation mechanism)
     * @param amount Amount to burn
     */
    function burn(uint256 amount) public override {
        super.burn(amount);
        emit TokensBurned(msg.sender, amount);
    }

    /**
     * @dev Emergency pause function
     */
    function pause() public onlyOwner {
        _pause();
    }

    /**
     * @dev Resume token transfers after pause
     */
    function unpause() public onlyOwner {
        _unpause();
    }

    /**
     * @dev Override transfer to respect pause state
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
        super._beforeTokenTransfer(from, to, amount);
        
        // Update snapshots
        if (from != address(0)) {
            snapshots[current_snapshot][from] = balanceOf(from) - amount;
        }
        if (to != address(0)) {
            snapshots[current_snapshot][to] = balanceOf(to) + amount;
        }
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
        require(snapshotId <= current_snapshot, "Invalid snapshot ID");
        return INITIAL_SUPPLY;
    }

    /**
     * @dev Get current snapshot ID
     * @return Current snapshot ID
     */
    function getCurrentSnapshot() public view returns (uint256) {
        return current_snapshot;
    }
}
