// SPDX-License-Identifier: Apache-2.0
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/access/Ownable.sol";
import "@openzeppelin/contracts/security/Pausable.sol";
import "@openzeppelin/contracts/security/ReentrancyGuard.sol";

import "./SynCoin.sol";

/**
 * @title SYNTHOSSynBridgeMinter
 * @dev The only thing allowed to mint or burn SynCoin on this chain. This is
 * the actual bridge: it turns a real, relayer-verified lock event on the
 * native SYNTHOS chain into a matching mint here, and turns a burn here
 * into an event the native chain's bridge relayer can act on to release the
 * real coin back home. No SYN is ever created here without a proven native
 * lock behind it, and no SYN leaves here without being destroyed first.
 *
 * The relayer/quorum/epoch-limit design intentionally mirrors
 * SYNTHOSBridgeVault, which already secures every other bridged asset on
 * this chain -- same trust model, same operational pattern, just wired to
 * mint/burn SynCoin directly instead of custodying a pre-funded balance
 * (SynCoin has no pre-funded balance to custody: it starts at zero supply).
 */
contract SYNTHOSSynBridgeMinter is Ownable, Pausable, ReentrancyGuard {
    SynCoin public immutable synCoin;

    mapping(address => bool) public relayers;
    mapping(bytes32 => bool) public processedMintMessages;
    mapping(bytes32 => mapping(address => bool)) public approvedBy;
    mapping(bytes32 => uint256) public approvalCount;

    uint256 public relayerCount;
    uint256 public threshold;
    uint256 public outboundNonce;

    uint256 public maxMintAmount;
    uint256 public epochMintLimit;
    uint256 public epochDuration;
    uint256 public currentEpochStart;
    uint256 public mintedThisEpoch;

    event RelayerUpdated(address indexed relayer, bool enabled);
    event ThresholdUpdated(uint256 previousThreshold, uint256 newThreshold);
    event MintLimitsUpdated(uint256 maxMintAmount, uint256 epochMintLimit, uint256 epochDuration);
    event MinterPaused(address indexed account);
    event MinterUnpaused(address indexed account);

    /// @dev Emitted when a holder sends their wrapped SYN back toward the
    /// native chain. The off-chain bridge relayer watches this event the
    /// same way it already watches SYNTHOSBridgeVault's BridgeLocked event,
    /// then submits the matching release on the native chain.
    event SynBurnedForNativeRelease(
        bytes32 indexed burnId,
        address indexed sender,
        bytes nativeRecipient,
        uint256 amount,
        uint256 nonce
    );

    event MintApproved(
        bytes32 indexed messageId,
        bytes32 indexed sourceEventId,
        address indexed relayer,
        uint256 approvals,
        uint256 threshold
    );

    event SynMinted(
        bytes32 indexed messageId,
        bytes32 indexed sourceEventId,
        address indexed recipient,
        uint256 amount
    );

    modifier onlyRelayer() {
        require(relayers[msg.sender], "not relayer");
        _;
    }

    constructor(address synCoinAddress, address[] memory initialRelayers, uint256 initialThreshold) {
        require(synCoinAddress != address(0), "invalid syn coin");
        synCoin = SynCoin(synCoinAddress);
        require(initialRelayers.length > 0, "relayers required");
        for (uint256 i = 0; i < initialRelayers.length; i++) {
            _setRelayer(initialRelayers[i], true);
        }
        _setThreshold(initialThreshold);
        _pause();
    }

    /// @dev Any holder can send their own wrapped SYN home -- no relayer
    /// approval needed to burn, exactly like SYNTHOSBridgeVault.lock()
    /// needs no approval to lock. The native-side release is what actually
    /// requires relayer quorum, on the native chain's own governance.
    function burnToNative(
        uint256 amount,
        bytes calldata nativeRecipient
    ) external whenNotPaused nonReentrant returns (bytes32 burnId) {
        require(amount > 0, "amount required");
        require(nativeRecipient.length > 0, "recipient required");

        outboundNonce++;
        burnId = keccak256(
            abi.encode(
                block.chainid,
                address(this),
                msg.sender,
                amount,
                nativeRecipient,
                outboundNonce
            )
        );

        synCoin.bridgeBurn(msg.sender, amount);

        emit SynBurnedForNativeRelease(burnId, msg.sender, nativeRecipient, amount, outboundNonce);
    }

    /// @dev Relayers each attest that `amount` was really locked on the
    /// native chain under `sourceEventId`. Once `threshold` relayers agree,
    /// the mint actually happens -- mirrors
    /// SYNTHOSBridgeVault.approveRelease exactly, replacing the vault's
    /// safeTransfer with a real, bridge-only mint.
    function approveMint(
        bytes32 sourceEventId,
        address recipient,
        uint256 amount
    ) external onlyRelayer whenNotPaused nonReentrant returns (bytes32 messageId) {
        require(sourceEventId != bytes32(0), "source event required");
        require(recipient != address(0), "invalid recipient");
        require(amount > 0, "amount required");
        require(maxMintAmount == 0 || amount <= maxMintAmount, "mint amount exceeds limit");

        messageId = mintMessageId(sourceEventId, recipient, amount);
        require(!processedMintMessages[messageId], "already processed");
        require(!approvedBy[messageId][msg.sender], "already approved");

        approvedBy[messageId][msg.sender] = true;
        approvalCount[messageId]++;

        emit MintApproved(messageId, sourceEventId, msg.sender, approvalCount[messageId], threshold);

        if (approvalCount[messageId] >= threshold) {
            _executeMint(messageId, sourceEventId, recipient, amount);
        }
    }

    function mintMessageId(
        bytes32 sourceEventId,
        address recipient,
        uint256 amount
    ) public view returns (bytes32) {
        return keccak256(
            abi.encode(
                "SYNTHOS_SYN_BRIDGE_MINT_V1",
                block.chainid,
                address(this),
                sourceEventId,
                recipient,
                amount
            )
        );
    }

    function setRelayer(address relayer, bool enabled) external onlyOwner {
        _setRelayer(relayer, enabled);
        require(threshold == 0 || threshold <= relayerCount, "threshold exceeds relayers");
    }

    function setThreshold(uint256 newThreshold) external onlyOwner {
        _setThreshold(newThreshold);
    }

    function setMintLimits(
        uint256 newMaxMintAmount,
        uint256 newEpochMintLimit,
        uint256 newEpochDuration
    ) external onlyOwner {
        require(newEpochMintLimit == 0 || newEpochDuration > 0, "epoch duration required");
        maxMintAmount = newMaxMintAmount;
        epochMintLimit = newEpochMintLimit;
        epochDuration = newEpochDuration;
        if (currentEpochStart == 0 && newEpochDuration > 0) {
            currentEpochStart = block.timestamp;
        }
        emit MintLimitsUpdated(newMaxMintAmount, newEpochMintLimit, newEpochDuration);
    }

    function pause() external onlyOwner {
        _pause();
        emit MinterPaused(msg.sender);
    }

    function unpause() external onlyOwner {
        _unpause();
        emit MinterUnpaused(msg.sender);
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

    function _executeMint(
        bytes32 messageId,
        bytes32 sourceEventId,
        address recipient,
        uint256 amount
    ) internal {
        require(!processedMintMessages[messageId], "already processed");
        _consumeEpochMintCapacity(amount);
        processedMintMessages[messageId] = true;
        synCoin.mint(recipient, amount);
        emit SynMinted(messageId, sourceEventId, recipient, amount);
    }

    function _consumeEpochMintCapacity(uint256 amount) internal {
        if (epochMintLimit == 0) {
            return;
        }
        if (currentEpochStart == 0 || block.timestamp >= currentEpochStart + epochDuration) {
            currentEpochStart = block.timestamp;
            mintedThisEpoch = 0;
        }
        require(mintedThisEpoch + amount <= epochMintLimit, "epoch mint limit exceeded");
        mintedThisEpoch += amount;
    }
}
