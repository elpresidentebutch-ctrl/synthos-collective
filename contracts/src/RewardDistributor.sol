// SPDX-License-Identifier: Apache-2.0
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";
import "@openzeppelin/contracts/security/ReentrancyGuard.sol";

/**
 * @title RewardDistributor
 * @dev Unified reward distribution for SYNTHOS and Gemini ecosystems
 *
 * Features:
 * - Multi-token reward distribution
 * - Vesting schedules for locked rewards
 * - Claim history tracking
 * - Batch distribution support
 * - Governance control
 */
contract RewardDistributor is ReentrancyGuard {
    using SafeERC20 for IERC20;
    // Vesting structure
    struct Vesting {
        address token;
        address beneficiary;
        uint256 total_amount;
        uint256 claimed_amount;
        uint256 start_time;
        uint256 duration;
        uint256 cliff;
        uint256 created_at;
    }

    // Reward structure
    struct Reward {
        address token;
        address recipient;
        uint256 amount;
        uint256 timestamp;
        string reward_type;
    }

    // State variables
    mapping(bytes32 => Vesting) public vestings;
    mapping(address => bytes32[]) public user_vestings;
    
    Reward[] public reward_history;
    
    address public governance;
    mapping(address => bool) public approved_tokens;
    // Sticky, never unset by revokeToken -- see sweepUnapprovedToken.
    mapping(address => bool) public ever_approved_tokens;

    uint256 public vesting_count = 0;

    // Events
    event VestingCreated(
        bytes32 indexed vesting_id,
        address indexed beneficiary,
        address indexed token,
        uint256 total_amount,
        uint256 duration,
        uint256 cliff
    );

    event VestingClaimed(
        bytes32 indexed vesting_id,
        address indexed beneficiary,
        uint256 amount
    );

    event RewardDistributed(
        address indexed recipient,
        address indexed token,
        uint256 amount,
        string reward_type
    );

    event TokenApproved(address indexed token);
    event TokenRevoked(address indexed token);

    constructor(address governance_addr) {
        governance = governance_addr;
    }

    /**
     * @dev Approve token for rewards
     * @param token Token address to approve
     */
    function approveToken(address token) public {
        require(msg.sender == governance, "Only governance");
        require(token != address(0), "Invalid token");

        approved_tokens[token] = true;
        ever_approved_tokens[token] = true;
        emit TokenApproved(token);
    }

    /**
     * @dev Revoke token for rewards
     * @param token Token address to revoke
     */
    function revokeToken(address token) public {
        require(msg.sender == governance, "Only governance");
        
        approved_tokens[token] = false;
        emit TokenRevoked(token);
    }

    /**
     * @dev Create vesting schedule
     * @param token Token to vest
     * @param beneficiary Beneficiary address
     * @param total_amount Total amount to vest
     * @param duration Total vesting duration
     * @param cliff Cliff period (no vesting before this)
     */
    function createVesting(
        address token,
        address beneficiary,
        uint256 total_amount,
        uint256 duration,
        uint256 cliff
    ) 
        public 
        returns (bytes32) 
    {
        require(approved_tokens[token], "Token not approved");
        require(beneficiary != address(0), "Invalid beneficiary");
        require(total_amount > 0, "Invalid amount");
        require(duration > 0, "Invalid duration");
        require(cliff <= duration, "Cliff exceeds duration");

        bytes32 vesting_id = keccak256(
            abi.encodePacked(
                token,
                beneficiary,
                total_amount,
                block.timestamp,
                vesting_count++
            )
        );

        Vesting storage vesting = vestings[vesting_id];
        vesting.token = token;
        vesting.beneficiary = beneficiary;
        vesting.total_amount = total_amount;
        vesting.claimed_amount = 0;
        vesting.start_time = block.timestamp;
        vesting.duration = duration;
        vesting.cliff = cliff;
        vesting.created_at = block.timestamp;

        user_vestings[beneficiary].push(vesting_id);

        emit VestingCreated(
            vesting_id,
            beneficiary,
            token,
            total_amount,
            duration,
            cliff
        );

        return vesting_id;
    }

    /**
     * @dev Calculate vested amount for vesting schedule
     * @param vesting_id Vesting ID
     * @return Vested amount available to claim
     */
    function calculateVestedAmount(bytes32 vesting_id) 
        public 
        view 
        returns (uint256) 
    {
        Vesting storage vesting = vestings[vesting_id];
        require(vesting.beneficiary != address(0), "Invalid vesting");

        uint256 elapsed = block.timestamp - vesting.start_time;

        // Check if still in cliff period
        if (elapsed < vesting.cliff) {
            return 0;
        }

        // Calculate vested amount (linear vesting after cliff)
        if (elapsed >= vesting.duration) {
            return vesting.total_amount;
        }

        uint256 vested = (vesting.total_amount * (elapsed - vesting.cliff)) /
            (vesting.duration - vesting.cliff);

        return vested - vesting.claimed_amount;
    }

    /**
     * @dev Claim vested tokens
     * @param vesting_id Vesting ID to claim
     * @return Amount claimed
     */
    function claimVesting(bytes32 vesting_id) public nonReentrant returns (uint256) {
        Vesting storage vesting = vestings[vesting_id];

        require(vesting.beneficiary != address(0), "Invalid vesting");
        require(msg.sender == vesting.beneficiary, "Not beneficiary");

        uint256 claimable = calculateVestedAmount(vesting_id);
        require(claimable > 0, "Nothing to claim");

        // Effects before interaction: accounting is updated first so a
        // reentrant call into claimVesting (or anything else) sees this
        // vesting's claimed_amount already reflecting this claim.
        vesting.claimed_amount += claimable;

        // This used to only emit VestingClaimed without ever moving tokens
        // -- claimVesting updated claimed_amount and looked, from the
        // event log, exactly like a real payout, but the beneficiary
        // received nothing. Fixed to actually pay out.
        IERC20(vesting.token).safeTransfer(msg.sender, claimable);

        emit VestingClaimed(vesting_id, msg.sender, claimable);

        return claimable;
    }

    /**
     * @dev Distribute rewards immediately
     * @param token Token to distribute
     * @param recipients Array of recipients
     * @param amounts Array of amounts
     * @param reward_type Type of reward
     */
    function batchDistributeRewards(
        address token,
        address[] calldata recipients,
        uint256[] calldata amounts,
        string calldata reward_type
    ) 
        public 
    {
        require(msg.sender == governance, "Only governance");
        require(approved_tokens[token], "Token not approved");
        require(recipients.length == amounts.length, "Array length mismatch");
        require(recipients.length > 0, "Empty recipients");

        for (uint256 i = 0; i < recipients.length; i++) {
            require(recipients[i] != address(0), "Invalid recipient");
            require(amounts[i] > 0, "Invalid amount");

            Reward memory reward;
            reward.token = token;
            reward.recipient = recipients[i];
            reward.amount = amounts[i];
            reward.timestamp = block.timestamp;
            reward.reward_type = reward_type;

            reward_history.push(reward);

            emit RewardDistributed(recipients[i], token, amounts[i], reward_type);
        }
    }

    /**
     * @dev Claim immediate rewards (non-vested)
     * @param reward_index Index in reward history
     */
    function claimReward(uint256 reward_index) public nonReentrant {
        require(reward_index < reward_history.length, "Invalid reward");

        Reward storage reward = reward_history[reward_index];
        require(reward.recipient == msg.sender, "Not recipient");
        require(reward.amount > 0, "Already claimed");

        uint256 amount = reward.amount;
        address token = reward.token;
        reward.amount = 0; // Mark as claimed, before the external call below

        // Same bug as claimVesting: batchDistributeRewards recorded a
        // Reward entry and claimReward "marked" it claimed, but no tokens
        // ever moved -- a recipient calling this got an event and nothing
        // else. Fixed to actually pay out.
        IERC20(token).safeTransfer(msg.sender, amount);

        emit RewardDistributed(reward.recipient, token, amount, reward.reward_type);
    }

    /**
     * @dev Get user vesting count
     * @param user User address
     * @return Count of vestings
     */
    function getUserVestingCount(address user) public view returns (uint256) {
        return user_vestings[user].length;
    }

    /**
     * @dev Get user vesting IDs
     * @param user User address
     * @return Array of vesting IDs
     */
    function getUserVestings(address user) 
        public 
        view 
        returns (bytes32[] memory) 
    {
        return user_vestings[user];
    }

    /**
     * @dev Get vesting details
     * @param vesting_id Vesting ID
     */
    function getVestingDetails(bytes32 vesting_id) 
        public 
        view 
        returns (
            address token,
            address beneficiary,
            uint256 total_amount,
            uint256 claimed_amount,
            uint256 vested_amount,
            uint256 start_time,
            uint256 duration,
            uint256 cliff
        ) 
    {
        Vesting storage vesting = vestings[vesting_id];
        
        return (
            vesting.token,
            vesting.beneficiary,
            vesting.total_amount,
            vesting.claimed_amount,
            calculateVestedAmount(vesting_id),
            vesting.start_time,
            vesting.duration,
            vesting.cliff
        );
    }

    /**
     * @dev Get reward history length
     * @return Total rewards distributed
     */
    function getRewardHistoryLength() public view returns (uint256) {
        return reward_history.length;
    }

    /**
     * @dev Get reward details
     * @param reward_index Reward index
     */
    function getRewardDetails(uint256 reward_index) 
        public 
        view 
        returns (
            address token,
            address recipient,
            uint256 amount,
            uint256 timestamp,
            string memory reward_type
        ) 
    {
        require(reward_index < reward_history.length, "Invalid reward");
        Reward storage reward = reward_history[reward_index];
        
        return (
            reward.token,
            reward.recipient,
            reward.amount,
            reward.timestamp,
            reward.reward_type
        );
    }

    /**
     * @dev Update governance address
     * @param new_governance New governance
     */
    function setGovernance(address new_governance) public {
        require(msg.sender == governance, "Only governance");
        governance = new_governance;
    }

    /**
     * @dev Recovers tokens that have NEVER been an approved reward/vesting
     * token -- e.g. an unrelated ERC-20 sent to this contract by mistake.
     * Checked against ever_approved_tokens, not the live approved_tokens
     * toggle: approved_tokens flips back to false on revokeToken, but a
     * revoked token can still have real, unclaimed vestings/rewards
     * outstanding against it. Gating on the sticky ever_approved_tokens
     * flag instead means a token that was ever approved -- and so could
     * ever have backed a real claim -- can never be swept here, revoked or
     * not. Only a token that was never approved, and therefore could never
     * have backed any vesting or reward in the first place, is eligible.
     * @param token Token to recover
     * @param to Recipient of the recovered tokens
     * @param amount Amount to recover
     */
    function sweepUnapprovedToken(address token, address to, uint256 amount) public nonReentrant {
        require(msg.sender == governance, "Only governance");
        require(!ever_approved_tokens[token], "Token was or is an approved reward token");
        require(to != address(0), "Invalid recipient");

        IERC20(token).safeTransfer(to, amount);
    }
}
