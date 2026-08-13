// SPDX-License-Identifier: Apache-2.0
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/access/Ownable.sol";
import "@openzeppelin/contracts/security/Pausable.sol";
import "@openzeppelin/contracts/security/ReentrancyGuard.sol";
import "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";

/**
 * @title SYNTHOSBridgeVault
 * @dev Testnet-grade bridge vault for SYN and approved ERC20 assets.
 *
 * Outbound bridge:
 * - user locks an approved ERC20 in this vault;
 * - contract emits a deterministic BridgeLocked event;
 * - off-chain bridge observers/validators submit the corresponding native
 *   SYNTHOS allocation or destination-chain mint/release.
 *
 * Inbound bridge:
 * - relayers approve a source-chain event by its sourceEventId;
 * - once the quorum threshold is reached, this vault releases pre-funded tokens;
 * - each source event can be processed only once.
 *
 * This contract intentionally starts as a guarded foundation, not an autonomous
 * production bridge. Production deployment should use governance/multisig
 * ownership, independent relayers, monitoring, withdrawal limits, and audit.
 */
contract SYNTHOSBridgeVault is Ownable, Pausable, ReentrancyGuard {
    using SafeERC20 for IERC20;

    struct ChainConfig {
        bool enabled;
        uint64 minConfirmations;
    }

    struct PendingRelease {
        bool queued;
        bytes32 sourceEventId;
        uint256 sourceChainId;
        address asset;
        address recipient;
        uint256 amount;
        uint256 executableAt;
    }

    mapping(uint256 => ChainConfig) public chains;
    mapping(address => bool) public supportedAssets;
    mapping(address => bool) public relayers;
    mapping(bytes32 => bool) public processedMessages;
    mapping(bytes32 => bool) public queuedMessages;
    mapping(bytes32 => PendingRelease) public pendingReleases;
    mapping(bytes32 => uint256) public approvalCount;
    mapping(bytes32 => mapping(address => bool)) public approvedBy;

    uint256 public outboundNonce;
    uint256 public relayerCount;
    uint256 public threshold;
    uint256 public maxLockAmount;
    uint256 public maxReleaseAmount;
    uint256 public epochReleaseLimit;
    uint256 public epochDuration;
    uint256 public currentEpochStart;
    uint256 public releasedThisEpoch;
    uint256 public releaseDelay;

    event ChainUpdated(uint256 indexed chainId, bool enabled, uint64 minConfirmations);
    event AssetSupportUpdated(address indexed asset, bool supported);
    event RelayerUpdated(address indexed relayer, bool enabled);
    event ThresholdUpdated(uint256 previousThreshold, uint256 newThreshold);
    event BridgeLimitsUpdated(
        uint256 maxLockAmount,
        uint256 maxReleaseAmount,
        uint256 epochReleaseLimit,
        uint256 epochDuration,
        uint256 releaseDelay
    );
    event BridgePaused(address indexed account);
    event BridgeUnpaused(address indexed account);

    event BridgeLocked(
        bytes32 indexed depositId,
        uint256 indexed sourceChainId,
        uint256 indexed destinationChainId,
        address asset,
        address sender,
        bytes destinationRecipient,
        uint256 amount,
        uint256 nonce
    );

    event ReleaseApproved(
        bytes32 indexed messageId,
        bytes32 indexed sourceEventId,
        address indexed relayer,
        uint256 approvals,
        uint256 threshold
    );

    event BridgeReleased(
        bytes32 indexed messageId,
        bytes32 indexed sourceEventId,
        uint256 indexed sourceChainId,
        address asset,
        address recipient,
        uint256 amount
    );

    event BridgeReleaseQueued(
        bytes32 indexed messageId,
        bytes32 indexed sourceEventId,
        uint256 executableAt
    );

    modifier onlyRelayer() {
        require(relayers[msg.sender], "not relayer");
        _;
    }

    constructor(address[] memory initialRelayers, uint256 initialThreshold) {
        require(initialRelayers.length > 0, "relayers required");
        for (uint256 i = 0; i < initialRelayers.length; i++) {
            _setRelayer(initialRelayers[i], true);
        }
        _setThreshold(initialThreshold);
        _pause();
    }

    function lock(
        address asset,
        uint256 amount,
        uint256 destinationChainId,
        bytes calldata destinationRecipient
    ) external whenNotPaused nonReentrant returns (bytes32 depositId) {
        require(supportedAssets[asset], "unsupported asset");
        require(chains[destinationChainId].enabled, "destination chain disabled");
        require(amount > 0, "amount required");
        require(maxLockAmount == 0 || amount <= maxLockAmount, "lock amount exceeds limit");
        require(destinationRecipient.length > 0, "recipient required");

        outboundNonce++;
        depositId = keccak256(
            abi.encode(
                block.chainid,
                address(this),
                msg.sender,
                asset,
                amount,
                destinationChainId,
                destinationRecipient,
                outboundNonce
            )
        );

        IERC20(asset).safeTransferFrom(msg.sender, address(this), amount);

        emit BridgeLocked(
            depositId,
            block.chainid,
            destinationChainId,
            asset,
            msg.sender,
            destinationRecipient,
            amount,
            outboundNonce
        );
    }

    function approveRelease(
        bytes32 sourceEventId,
        uint256 sourceChainId,
        address asset,
        address recipient,
        uint256 amount
    ) external onlyRelayer whenNotPaused nonReentrant returns (bytes32 messageId) {
        require(sourceEventId != bytes32(0), "source event required");
        require(chains[sourceChainId].enabled, "source chain disabled");
        require(supportedAssets[asset], "unsupported asset");
        require(recipient != address(0), "invalid recipient");
        require(amount > 0, "amount required");
        require(maxReleaseAmount == 0 || amount <= maxReleaseAmount, "release amount exceeds limit");

        messageId = releaseMessageId(sourceEventId, sourceChainId, asset, recipient, amount);
        require(!processedMessages[messageId], "already processed");
        require(!queuedMessages[messageId], "already queued");
        require(!approvedBy[messageId][msg.sender], "already approved");

        approvedBy[messageId][msg.sender] = true;
        approvalCount[messageId]++;

        emit ReleaseApproved(
            messageId,
            sourceEventId,
            msg.sender,
            approvalCount[messageId],
            threshold
        );

        if (approvalCount[messageId] >= threshold) {
            if (releaseDelay == 0) {
                _executeRelease(messageId, sourceEventId, sourceChainId, asset, recipient, amount);
            } else {
                queuedMessages[messageId] = true;
                uint256 executableAt = block.timestamp + releaseDelay;
                pendingReleases[messageId] = PendingRelease({
                    queued: true,
                    sourceEventId: sourceEventId,
                    sourceChainId: sourceChainId,
                    asset: asset,
                    recipient: recipient,
                    amount: amount,
                    executableAt: executableAt
                });
                emit BridgeReleaseQueued(messageId, sourceEventId, executableAt);
            }
        }
    }

    function executeQueuedRelease(bytes32 messageId) external whenNotPaused nonReentrant {
        PendingRelease memory pending = pendingReleases[messageId];
        require(pending.queued, "release not queued");
        require(block.timestamp >= pending.executableAt, "release delay active");
        delete pendingReleases[messageId];
        _executeRelease(
            messageId,
            pending.sourceEventId,
            pending.sourceChainId,
            pending.asset,
            pending.recipient,
            pending.amount
        );
    }

    function releaseMessageId(
        bytes32 sourceEventId,
        uint256 sourceChainId,
        address asset,
        address recipient,
        uint256 amount
    ) public view returns (bytes32) {
        return keccak256(
            abi.encode(
                "SYNTHOS_BRIDGE_RELEASE_V1",
                block.chainid,
                address(this),
                sourceEventId,
                sourceChainId,
                asset,
                recipient,
                amount
            )
        );
    }

    function setChain(
        uint256 chainId,
        bool enabled,
        uint64 minConfirmations
    ) external onlyOwner {
        require(chainId != 0, "invalid chain");
        chains[chainId] = ChainConfig({
            enabled: enabled,
            minConfirmations: minConfirmations
        });
        emit ChainUpdated(chainId, enabled, minConfirmations);
    }

    function setAssetSupported(address asset, bool supported) external onlyOwner {
        require(asset != address(0), "invalid asset");
        supportedAssets[asset] = supported;
        emit AssetSupportUpdated(asset, supported);
    }

    function setRelayer(address relayer, bool enabled) external onlyOwner {
        _setRelayer(relayer, enabled);
        require(threshold == 0 || threshold <= relayerCount, "threshold exceeds relayers");
    }

    function setThreshold(uint256 newThreshold) external onlyOwner {
        _setThreshold(newThreshold);
    }

    function setBridgeLimits(
        uint256 newMaxLockAmount,
        uint256 newMaxReleaseAmount,
        uint256 newEpochReleaseLimit,
        uint256 newEpochDuration,
        uint256 newReleaseDelay
    ) external onlyOwner {
        require(newEpochReleaseLimit == 0 || newEpochDuration > 0, "epoch duration required");
        maxLockAmount = newMaxLockAmount;
        maxReleaseAmount = newMaxReleaseAmount;
        epochReleaseLimit = newEpochReleaseLimit;
        epochDuration = newEpochDuration;
        releaseDelay = newReleaseDelay;
        if (currentEpochStart == 0 && newEpochDuration > 0) {
            currentEpochStart = block.timestamp;
        }
        emit BridgeLimitsUpdated(
            newMaxLockAmount,
            newMaxReleaseAmount,
            newEpochReleaseLimit,
            newEpochDuration,
            newReleaseDelay
        );
    }

    function pause() external onlyOwner {
        _pause();
        emit BridgePaused(msg.sender);
    }

    function unpause() external onlyOwner {
        _unpause();
        emit BridgeUnpaused(msg.sender);
    }

    function _setRelayer(address relayer, bool enabled) internal {
        require(relayer != address(0), "invalid relayer");
        if (relayers[relayer] == enabled) {
            return;
        }
        relayers[relayer] = enabled;
        if (enabled) {
            relayerCount++;
        } else {
            relayerCount--;
        }
        emit RelayerUpdated(relayer, enabled);
    }

    function _setThreshold(uint256 newThreshold) internal {
        require(newThreshold > 0, "threshold required");
        require(newThreshold <= relayerCount, "threshold exceeds relayers");
        uint256 previousThreshold = threshold;
        threshold = newThreshold;
        emit ThresholdUpdated(previousThreshold, newThreshold);
    }

    function _executeRelease(
        bytes32 messageId,
        bytes32 sourceEventId,
        uint256 sourceChainId,
        address asset,
        address recipient,
        uint256 amount
    ) internal {
        require(!processedMessages[messageId], "already processed");
        _consumeEpochReleaseCapacity(amount);
        processedMessages[messageId] = true;
        queuedMessages[messageId] = false;
        IERC20(asset).safeTransfer(recipient, amount);
        emit BridgeReleased(messageId, sourceEventId, sourceChainId, asset, recipient, amount);
    }

    function _consumeEpochReleaseCapacity(uint256 amount) internal {
        if (epochReleaseLimit == 0) {
            return;
        }
        if (currentEpochStart == 0 || block.timestamp >= currentEpochStart + epochDuration) {
            currentEpochStart = block.timestamp;
            releasedThisEpoch = 0;
        }
        require(releasedThisEpoch + amount <= epochReleaseLimit, "epoch release limit exceeded");
        releasedThisEpoch += amount;
    }
}
