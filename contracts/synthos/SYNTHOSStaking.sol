// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "./SYNToken.sol";

/**
 * @title SYNTHOSStaking
 * @dev Validator staking contract for SYNTHOS network
 * 
 * Features:
 * - Validator registration with minimum stake
 * - Reward distribution
 * - Slashing for misbehavior
 * - Unstaking with cooldown period
 * - Delegation to validators
 */
contract SYNTHOSStaking {
    // Constants
    uint256 public constant MINIMUM_VALIDATOR_STAKE = 100_000 * 10**18; // 100k SYN
    uint256 public constant UNSTAKE_COOLDOWN = 7 days; // 7-day unstaking period
    uint256 public constant SLASH_RATE = 10; // 10% slashing rate

    // Token reference
    SYNToken public synToken;

    // Validator structure
    struct Validator {
        address validator_address;
        uint256 total_stake;
        uint256 delegated_stake;
        uint256 rewards_earned;
        uint256 registration_block;
        bool active;
        uint256 slash_count;
    }

    // Delegator structure
    struct Delegation {
        address delegator;
        address validator;
        uint256 amount;
        uint256 timestamp;
    }

    // Unstaking request structure
    struct UnstakeRequest {
        address validator;
        uint256 amount;
        uint256 unlock_time;
        bool claimed;
    }

    // State variables
    mapping(address => Validator) public validators;
    mapping(address => Delegation[]) public delegations;
    mapping(address => UnstakeRequest[]) public unstake_requests;
    
    address[] public validator_list;
    uint256 public total_staked = 0;
    uint256 public reward_pool = 0;
    uint256 public current_epoch = 1;
    uint256 public epoch_start_time = block.timestamp;
    uint256 public epoch_duration = 1 days;

    // Owner/governance
    address public governance;

    // Events
    event ValidatorRegistered(address indexed validator, uint256 stake);
    event ValidatorActivated(address indexed validator);
    event ValidatorDeactivated(address indexed validator);
    event DelegationCreated(address indexed delegator, address indexed validator, uint256 amount);
    event RewardDistributed(address indexed validator, uint256 amount);
    event SlashingApplied(address indexed validator, uint256 amount, string reason);
    event UnstakeRequested(address indexed validator, uint256 amount, uint256 unlock_time);
    event UnstakeClaimed(address indexed validator, uint256 amount);
    event EpochEnded(uint256 epoch, uint256 total_rewards);

    constructor(address token_addr, address governance_addr) {
        synToken = SYNToken(token_addr);
        governance = governance_addr;
    }

    /**
     * @dev Register as a validator
     * @param stake Amount of tokens to stake
     */
    function registerValidator(uint256 stake) public {
        require(stake >= MINIMUM_VALIDATOR_STAKE, "Stake below minimum");
        require(!validators[msg.sender].active, "Already validator");

        // Transfer tokens from sender to contract
        require(
            synToken.transferFrom(msg.sender, address(this), stake),
            "Transfer failed"
        );

        Validator storage v = validators[msg.sender];
        v.validator_address = msg.sender;
        v.total_stake = stake;
        v.delegated_stake = 0;
        v.rewards_earned = 0;
        v.registration_block = block.number;
        v.active = true;
        v.slash_count = 0;

        validator_list.push(msg.sender);
        total_staked += stake;

        emit ValidatorRegistered(msg.sender, stake);
    }

    /**
     * @dev Delegate tokens to a validator
     * @param validator Address of validator
     * @param amount Amount to delegate
     */
    function delegateToValidator(address validator, uint256 amount) public {
        require(validators[validator].active, "Validator not active");
        require(amount > 0, "Amount must be positive");

        // Transfer tokens
        require(
            synToken.transferFrom(msg.sender, address(this), amount),
            "Transfer failed"
        );

        Delegation memory del;
        del.delegator = msg.sender;
        del.validator = validator;
        del.amount = amount;
        del.timestamp = block.timestamp;

        delegations[msg.sender].push(del);
        validators[validator].delegated_stake += amount;
        total_staked += amount;

        emit DelegationCreated(msg.sender, validator, amount);
    }

    /**
     * @dev Request unstaking of delegated tokens
     * @param validator Address of validator
     * @param amount Amount to unstake
     */
    function requestUnstake(address validator, uint256 amount) public {
        require(amount > 0, "Amount must be positive");

        // Find and reduce delegation
        uint256 delegated = 0;
        for (uint256 i = 0; i < delegations[msg.sender].length; i++) {
            if (delegations[msg.sender][i].validator == validator) {
                delegated += delegations[msg.sender][i].amount;
            }
        }

        require(delegated >= amount, "Insufficient delegation");

        uint256 unlock_time = block.timestamp + UNSTAKE_COOLDOWN;

        UnstakeRequest memory req;
        req.validator = validator;
        req.amount = amount;
        req.unlock_time = unlock_time;
        req.claimed = false;

        unstake_requests[msg.sender].push(req);
        validators[validator].delegated_stake -= amount;
        total_staked -= amount;

        emit UnstakeRequested(validator, amount, unlock_time);
    }

    /**
     * @dev Claim unstaked tokens after cooldown
     */
    function claimUnstake(uint256 request_index) public {
        require(request_index < unstake_requests[msg.sender].length, "Invalid index");

        UnstakeRequest storage req = unstake_requests[msg.sender][request_index];
        require(!req.claimed, "Already claimed");
        require(block.timestamp >= req.unlock_time, "Cooldown not expired");

        req.claimed = true;

        require(
            synToken.transfer(msg.sender, req.amount),
            "Transfer failed"
        );

        emit UnstakeClaimed(msg.sender, req.amount);
    }

    /**
     * @dev Distribute rewards to validators
     * @param reward_amount Total rewards to distribute
     */
    function distributeRewards(uint256 reward_amount) public {
        require(msg.sender == governance, "Only governance");
        require(validator_list.length > 0, "No validators");

        uint256 per_validator = reward_amount / validator_list.length;

        for (uint256 i = 0; i < validator_list.length; i++) {
            address validator = validator_list[i];
            if (validators[validator].active) {
                validators[validator].rewards_earned += per_validator;
                reward_pool += per_validator;
                emit RewardDistributed(validator, per_validator);
            }
        }
    }

    /**
     * @dev Claim accumulated rewards
     */
    function claimRewards() public {
        require(validators[msg.sender].active, "Not a validator");
        require(validators[msg.sender].rewards_earned > 0, "No rewards");

        uint256 rewards = validators[msg.sender].rewards_earned;
        validators[msg.sender].rewards_earned = 0;
        reward_pool -= rewards;

        require(synToken.transfer(msg.sender, rewards), "Transfer failed");
    }

    /**
     * @dev Slash validator for misbehavior
     * @param validator Address to slash
     * @param amount Amount to slash
     * @param reason Reason for slashing
     */
    function slash(address validator, uint256 amount, string memory reason) public {
        require(msg.sender == governance, "Only governance");
        require(validators[validator].active, "Not a validator");
        require(amount > 0, "Amount must be positive");

        uint256 slash_amount = (amount * SLASH_RATE) / 100;
        require(validators[validator].total_stake >= slash_amount, "Insufficient stake");

        validators[validator].total_stake -= slash_amount;
        validators[validator].slash_count++;
        total_staked -= slash_amount;

        // If validator has insufficient stake, deactivate
        if (validators[validator].total_stake < MINIMUM_VALIDATOR_STAKE) {
            validators[validator].active = false;
            emit ValidatorDeactivated(validator);
        }

        emit SlashingApplied(validator, slash_amount, reason);
    }

    /**
     * @dev Deactivate validator
     * @param validator Address to deactivate
     */
    function deactivateValidator(address validator) public {
        require(
            msg.sender == validator || msg.sender == governance,
            "Unauthorized"
        );
        require(validators[validator].active, "Not active");

        validators[validator].active = false;
        total_staked -= validators[validator].total_stake;

        emit ValidatorDeactivated(validator);
    }

    /**
     * @dev Advance epoch and distribute epoch rewards
     */
    function advanceEpoch() public {
        require(
            block.timestamp >= epoch_start_time + epoch_duration,
            "Epoch not ended"
        );

        // Calculate and distribute epoch rewards
        uint256 epoch_rewards = calculateEpochRewards();
        if (epoch_rewards > 0) {
            distributeRewards(epoch_rewards);
        }

        current_epoch++;
        epoch_start_time = block.timestamp;

        emit EpochEnded(current_epoch - 1, epoch_rewards);
    }

    /**
     * @dev Calculate rewards for current epoch
     * @return Epoch rewards amount
     */
    function calculateEpochRewards() public view returns (uint256) {
        // Base reward = 5% annual inflation
        uint256 annual_inflation = (total_staked * 5) / 100;
        uint256 epoch_rewards = annual_inflation / (365 days / epoch_duration);
        return epoch_rewards;
    }

    /**
     * @dev Get validator info
     * @param validator Address of validator
     */
    function getValidator(address validator) 
        public 
        view 
        returns (
            uint256 total_stake,
            uint256 delegated_stake,
            uint256 rewards_earned,
            bool active,
            uint256 slash_count
        ) 
    {
        Validator storage v = validators[validator];
        return (
            v.total_stake,
            v.delegated_stake,
            v.rewards_earned,
            v.active,
            v.slash_count
        );
    }

    /**
     * @dev Get number of active validators
     * @return Count of active validators
     */
    function getValidatorCount() public view returns (uint256) {
        uint256 count = 0;
        for (uint256 i = 0; i < validator_list.length; i++) {
            if (validators[validator_list[i]].active) {
                count++;
            }
        }
        return count;
    }

    /**
     * @dev Update governance address
     * @param new_governance New governance address
     */
    function setGovernance(address new_governance) public {
        require(msg.sender == governance, "Only governance");
        governance = new_governance;
    }
}
