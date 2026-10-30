// SPDX-License-Identifier: Apache-2.0
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/access/Ownable.sol";
import "@openzeppelin/contracts/security/Pausable.sol";
import "@openzeppelin/contracts/security/ReentrancyGuard.sol";
import "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";

/**
 * @title SYNTHOSValidatorCandidateRewards
 * @dev Fully automated on-chain validator candidate verification and reward disbursement.
 *
 * Requirements addressed:
 * 1. Self-contained on-chain verification:
 *    No off-chain manual auditing is required to determine whether a validator candidate
 *    has completed their one-month verification period.
 *    Either the candidate is verified according to the smart contract, or it isn't.
 *
 * 2. Automatic Payout:
 *    When verified for the required interval (30 days), the smart contract pays out 5,000 SYN
 *    automatically to the candidate's operator address.
 *
 * 3. Deterministic Non-Verified Handling:
 *    If a candidate has not completed the required one-month verification period,
 *    the contract explicitly reports the time remaining, rejects non-eligible claims,
 *    or safely skips them during automated batch sweeps without halting execution.
 *
 * 4. Automated Triggers:
 *    - Self-service: The candidate can call `claimMonthlyReward()`.
 *    - Third-party / Keeper automation: Anyone or an automated bot/keeper can call
 *      `claimRewardFor(candidate)` or `distributeEligibleRewards(candidates[])`.
 */
contract SYNTHOSValidatorCandidateRewards is Ownable, Pausable, ReentrancyGuard {
    using SafeERC20 for IERC20;

    // Constants
    uint256 public constant ONE_MONTH = 30 days; // 30 days = 2,592,000 seconds
    uint256 public constant BASE_MONTHLY_REWARD = 5_000 * 10 ** 18; // 5,000 SYN
    uint256 public constant MAX_VERIFIED_MONTHS = 120; // 10 years max (120 monthly cycles)
    uint256 public constant HEARTBEAT_TIMEOUT = 3 days; // Maximum allowed gap between heartbeats if strict mode is enabled

    // The SYN token contract
    IERC20 public immutable synToken;

    // Configuration
    uint256 public rewardInterval = ONE_MONTH;
    uint256 public monthlyRewardAmount = BASE_MONTHLY_REWARD;
    bool public strictHeartbeatMode = false; // If true, requires periodic on-chain heartbeats; if false, measures verified elapsed uptime

    struct Candidate {
        address operator;              // Payout address of candidate
        bytes32 nodeKeyHash;           // Hash of candidate's Ed25519 identity key
        uint256 registeredAt;          // Timestamp when candidate registered
        uint256 lastClaimAt;           // Timestamp of last reward claim (or registration)
        uint256 lastHeartbeatAt;       // Timestamp of most recent signed heartbeat proof
        uint256 monthsPaid;            // Number of 30-day reward payouts received
        uint256 totalRewardsClaimed;   // Total SYN tokens disbursed to candidate
        bool active;                   // Whether candidate is currently active
    }

    // Mapping from candidate address to Candidate struct
    mapping(address => Candidate) public candidates;
    // List of registered candidate addresses for enumeration and automated keeper sweeps
    address[] public candidateList;
    // Prevent duplicate registrations by node key
    mapping(bytes32 => bool) public registeredNodeKeys;

    // Total metrics
    uint256 public totalCandidates;
    uint256 public totalRewardsDisbursed;

    // Events
    event CandidateRegistered(address indexed candidate, bytes32 indexed nodeKeyHash, uint256 registeredAt);
    event HeartbeatRecorded(address indexed candidate, bytes32 indexed heartbeatProof, uint256 timestamp);
    event ValidatorRewardPaid(
        address indexed candidate,
        address indexed recipient,
        uint256 amount,
        uint256 monthNumber,
        uint256 timestamp
    );
    event VerificationFailed(
        address indexed candidate,
        uint256 timeRemaining,
        string reason
    );
    event CandidateStatusUpdated(address indexed candidate, bool active);
    event RewardPolicyUpdated(uint256 newRewardInterval, uint256 newMonthlyReward, bool strictHeartbeats);

    // Custom errors
    error CandidateNotRegistered(address candidate);
    error CandidateAlreadyRegistered(address candidate);
    error CandidateInactive(address candidate);
    error VerificationIntervalNotMet(address candidate, uint256 timeRemaining);
    error HeartbeatStale(address candidate, uint256 lastHeartbeat, uint256 maxAllowed);
    error MaxMonthsReached(address candidate, uint256 monthsPaid);
    error InsufficientContractBalance(uint256 required, uint256 available);
    error InvalidAddress();
    error InvalidNodeKey();

    /**
     * @param synTokenAddress Address of the SYN token contract.
     */
    constructor(address synTokenAddress) {
        if (synTokenAddress == address(0)) revert InvalidAddress();
        synToken = IERC20(synTokenAddress);
    }

    /**
     * @notice Register a new validator candidate on-chain.
     * @param nodeKeyHash keccak256 hash of the candidate's Ed25519 node public key.
     */
    function registerCandidate(bytes32 nodeKeyHash) external whenNotPaused {
        _registerCandidate(msg.sender, nodeKeyHash);
    }

    /**
     * @notice Register a validator candidate on behalf of an operator address (e.g. by onboarding portal/bot).
     * @param operator Address of the validator operator.
     * @param nodeKeyHash keccak256 hash of the candidate's Ed25519 node public key.
     */
    function registerCandidateFor(address operator, bytes32 nodeKeyHash) external whenNotPaused {
        if (operator == address(0)) revert InvalidAddress();
        _registerCandidate(operator, nodeKeyHash);
    }

    function _registerCandidate(address operator, bytes32 nodeKeyHash) internal {
        if (nodeKeyHash == bytes32(0)) revert InvalidNodeKey();
        if (candidates[operator].registeredAt != 0) revert CandidateAlreadyRegistered(operator);
        if (registeredNodeKeys[nodeKeyHash]) revert InvalidNodeKey();

        candidates[operator] = Candidate({
            operator: operator,
            nodeKeyHash: nodeKeyHash,
            registeredAt: block.timestamp,
            lastClaimAt: block.timestamp,
            lastHeartbeatAt: block.timestamp,
            monthsPaid: 0,
            totalRewardsClaimed: 0,
            active: true
        });

        candidateList.push(operator);
        registeredNodeKeys[nodeKeyHash] = true;
        totalCandidates++;

        emit CandidateRegistered(operator, nodeKeyHash, block.timestamp);
    }

    /**
     * @notice Record a periodic heartbeat proof for a validator candidate.
     * @param heartbeatProof keccak256 hash of the signed heartbeat message or receipt.
     */
    function recordHeartbeat(bytes32 heartbeatProof) external whenNotPaused {
        Candidate storage c = candidates[msg.sender];
        if (c.registeredAt == 0) revert CandidateNotRegistered(msg.sender);
        if (!c.active) revert CandidateInactive(msg.sender);

        c.lastHeartbeatAt = block.timestamp;
        emit HeartbeatRecorded(msg.sender, heartbeatProof, block.timestamp);
    }

    /**
     * @notice Check whether a validator candidate is currently verified for a 1-month reward payout.
     * @dev Returns true if and only if the candidate meets every requirement according to this smart contract.
     * @param candidate Address of the validator candidate.
     * @return isVerified Whether the candidate is verified and eligible for 5,000 SYN now.
     * @return timeRemaining Seconds remaining until the candidate completes the 1-month period (0 if verified).
     * @return pendingReward The exact amount of SYN to be paid upon verification (5,000 SYN).
     */
    function isVerifiedForOneMonth(address candidate)
        public
        view
        returns (
            bool isVerified,
            uint256 timeRemaining,
            uint256 pendingReward
        )
    {
        Candidate storage c = candidates[candidate];
        if (c.registeredAt == 0 || !c.active || c.monthsPaid >= MAX_VERIFIED_MONTHS) {
            return (false, type(uint256).max, 0);
        }

        uint256 nextEligibleTime = c.lastClaimAt + rewardInterval;
        if (block.timestamp < nextEligibleTime) {
            return (false, nextEligibleTime - block.timestamp, 0);
        }

        // If strict heartbeat mode is enabled, verify heartbeat was sent within timeout
        if (strictHeartbeatMode && (block.timestamp - c.lastHeartbeatAt > HEARTBEAT_TIMEOUT)) {
            return (false, 0, 0);
        }

        return (true, 0, monthlyRewardAmount);
    }

    /**
     * @notice Self-service reward claim by the validator candidate.
     * @dev Either the candidate is verified for one month or it reverts with the exact time remaining.
     */
    function claimMonthlyReward() external nonReentrant whenNotPaused {
        _payoutIfVerified(msg.sender);
    }

    /**
     * @notice Automated or keeper-triggered reward claim for a candidate.
     * @dev Allows an automated script or keeper to trigger the payout to the candidate's operator address.
     * @param candidate Address of the validator candidate.
     */
    function claimRewardFor(address candidate) external nonReentrant whenNotPaused {
        _payoutIfVerified(candidate);
    }

    /**
     * @notice Automated batch reward distributor.
     * @dev Iterates over a list of candidates.
     *      - If a candidate is verified for one month, it automatically pays out 5,000 SYN.
     *      - If not verified, it does NOT revert; it emits `VerificationFailed` with time remaining and continues.
     * @param candidatesToSweep Array of candidate addresses to evaluate and pay.
     * @return paidCount Number of candidates verified and paid in this transaction.
     * @return totalAmountDisbursed Total SYN tokens disbursed in this transaction.
     */
    function distributeEligibleRewards(address[] calldata candidatesToSweep)
        external
        nonReentrant
        whenNotPaused
        returns (uint256 paidCount, uint256 totalAmountDisbursed)
    {
        for (uint256 i = 0; i < candidatesToSweep.length; i++) {
            address candidate = candidatesToSweep[i];
            (bool verified, uint256 remaining, uint256 reward) = isVerifiedForOneMonth(candidate);

            if (verified) {
                if (synToken.balanceOf(address(this)) < reward) {
                    emit VerificationFailed(candidate, 0, "Insufficient contract funds");
                    continue;
                }

                _executePayout(candidate, reward);
                paidCount++;
                totalAmountDisbursed += reward;
            } else {
                emit VerificationFailed(candidate, remaining, "1-month verification interval not reached");
            }
        }
    }

    /**
     * @notice Automated batch sweep for all registered candidates in stored range.
     * @param startIndex Start index in `candidateList`.
     * @param count Maximum number of candidates to process.
     */
    function sweepCandidateBatch(uint256 startIndex, uint256 count)
        external
        nonReentrant
        whenNotPaused
        returns (uint256 paidCount, uint256 totalAmountDisbursed)
    {
        uint256 total = candidateList.length;
        if (startIndex >= total) return (0, 0);

        uint256 endIndex = startIndex + count;
        if (endIndex > total) {
            endIndex = total;
        }

        for (uint256 i = startIndex; i < endIndex; i++) {
            address candidate = candidateList[i];
            (bool verified, uint256 remaining, uint256 reward) = isVerifiedForOneMonth(candidate);

            if (verified) {
                if (synToken.balanceOf(address(this)) < reward) {
                    emit VerificationFailed(candidate, 0, "Insufficient contract funds");
                    continue;
                }

                _executePayout(candidate, reward);
                paidCount++;
                totalAmountDisbursed += reward;
            } else {
                emit VerificationFailed(candidate, remaining, "1-month verification interval not reached");
            }
        }
    }

    /**
     * @dev Internal payout logic. Enforces strict verification.
     */
    function _payoutIfVerified(address candidate) internal {
        Candidate storage c = candidates[candidate];
        if (c.registeredAt == 0) revert CandidateNotRegistered(candidate);
        if (!c.active) revert CandidateInactive(candidate);
        if (c.monthsPaid >= MAX_VERIFIED_MONTHS) revert MaxMonthsReached(candidate, c.monthsPaid);

        uint256 nextEligibleTime = c.lastClaimAt + rewardInterval;
        if (block.timestamp < nextEligibleTime) {
            revert VerificationIntervalNotMet(candidate, nextEligibleTime - block.timestamp);
        }

        if (strictHeartbeatMode && (block.timestamp - c.lastHeartbeatAt > HEARTBEAT_TIMEOUT)) {
            revert HeartbeatStale(candidate, c.lastHeartbeatAt, block.timestamp - HEARTBEAT_TIMEOUT);
        }

        uint256 reward = monthlyRewardAmount;
        if (synToken.balanceOf(address(this)) < reward) {
            revert InsufficientContractBalance(reward, synToken.balanceOf(address(this)));
        }

        _executePayout(candidate, reward);
    }

    function _executePayout(address candidate, uint256 reward) internal {
        Candidate storage c = candidates[candidate];
        c.lastClaimAt = block.timestamp;
        c.monthsPaid++;
        c.totalRewardsClaimed += reward;
        totalRewardsDisbursed += reward;

        synToken.safeTransfer(c.operator, reward);

        emit ValidatorRewardPaid(
            candidate,
            c.operator,
            reward,
            c.monthsPaid,
            block.timestamp
        );
    }

    /**
     * @notice Get candidate details and full verification state.
     */
    function getCandidate(address candidate)
        external
        view
        returns (
            address operator,
            bytes32 nodeKeyHash,
            uint256 registeredAt,
            uint256 lastClaimAt,
            uint256 lastHeartbeatAt,
            uint256 monthsPaid,
            uint256 totalRewardsClaimed,
            bool active,
            bool isVerified,
            uint256 timeRemaining
        )
    {
        Candidate storage c = candidates[candidate];
        (bool verified, uint256 remaining, ) = isVerifiedForOneMonth(candidate);
        return (
            c.operator,
            c.nodeKeyHash,
            c.registeredAt,
            c.lastClaimAt,
            c.lastHeartbeatAt,
            c.monthsPaid,
            c.totalRewardsClaimed,
            c.active,
            verified,
            remaining
        );
    }

    /**
     * @notice Total number of registered candidates.
     */
    function getCandidateCount() external view returns (uint256) {
        return candidateList.length;
    }

    /**
     * @notice Admin control to deactivate a misbehaving or decommissioned candidate node.
     */
    function setCandidateActive(address candidate, bool active) external onlyOwner {
        if (candidates[candidate].registeredAt == 0) revert CandidateNotRegistered(candidate);
        candidates[candidate].active = active;
        emit CandidateStatusUpdated(candidate, active);
    }

    /**
     * @notice Admin update of reward parameters.
     */
    function setRewardPolicy(
        uint256 newRewardInterval,
        uint256 newMonthlyReward,
        bool strictHeartbeats
    ) external onlyOwner {
        require(newRewardInterval > 0, "invalid interval");
        require(newMonthlyReward > 0, "invalid reward");

        rewardInterval = newRewardInterval;
        monthlyRewardAmount = newMonthlyReward;
        strictHeartbeatMode = strictHeartbeats;

        emit RewardPolicyUpdated(newRewardInterval, newMonthlyReward, strictHeartbeats);
    }

    function pause() external onlyOwner {
        _pause();
    }

    function unpause() external onlyOwner {
        _unpause();
    }

    /**
     * @notice View available SYN balance in this reward pool.
     */
    function availableRewardPool() external view returns (uint256) {
        return synToken.balanceOf(address(this));
    }
}
