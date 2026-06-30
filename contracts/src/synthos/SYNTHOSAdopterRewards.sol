// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/access/Ownable.sol";
import "@openzeppelin/contracts/security/Pausable.sol";
import "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";
import "@openzeppelin/contracts/utils/cryptography/MerkleProof.sol";

/**
 * @title SYNTHOSAdopterRewards
 * @dev Rewards verified operators who register and keep a SYNTHOS node alive.
 *
 * This contract does not mint SYN. It must be funded from the
 * IMMUNE_NODE_REWARDS allocation in SynCoin.
 *
 * Rewards are capped at one reward stream per operator address. An operator
 * may run multiple nodes operationally, but this contract only rewards the
 * first registered node for that operator. Monthly rewards are claimed in
 * arrears after the configured reward interval has elapsed.
 */
contract SYNTHOSAdopterRewards is Ownable, Pausable {
    using SafeERC20 for IERC20;

    uint256 public constant TARGET_IMMUNE_OPERATORS = 100_000;
    uint256 public constant TEN_YEAR_HEARTBEAT_PERIODS = 120;
    uint256 public constant DEFAULT_EARLY_OPERATOR_REWARD = 500 * 10 ** 18;
    uint256 public constant DEFAULT_HEARTBEAT_REWARD = 1_000 * 10 ** 18;
    uint256 public constant DEFAULT_HEARTBEAT_INTERVAL = 30 days;
    uint256 public constant TEN_YEAR_MAX_REWARD_PER_OPERATOR =
        DEFAULT_EARLY_OPERATOR_REWARD + (DEFAULT_HEARTBEAT_REWARD * TEN_YEAR_HEARTBEAT_PERIODS);
    uint256 public constant TEN_YEAR_TARGET_OPERATOR_BUDGET =
        TEN_YEAR_MAX_REWARD_PER_OPERATOR * TARGET_IMMUNE_OPERATORS;

    struct AdopterNode {
        address operator;
        bytes32 hardwareCommitment;
        string nodeType;
        uint256 registeredAt;
        uint256 lastHeartbeatAt;
        uint256 heartbeatClaims;
        bool activationClaimed;
        bool active;
    }

    IERC20 public immutable synToken;

    uint256 public activationReward;
    uint256 public heartbeatReward;
    uint256 public heartbeatInterval;
    uint256 public maxHeartbeatClaimsPerOperator;
    bytes32 public adopterMerkleRoot;
    bool public adopterMerkleGateRequired;

    mapping(bytes32 => AdopterNode) public nodes;
    mapping(address => bytes32) public nodeByOperator;
    mapping(bytes32 => bool) public hardwareCommitmentUsed;
    mapping(bytes32 => bool) public heartbeatProofUsed;
    mapping(bytes32 => bool) public adopterMerkleLeafUsed;

    uint256 public registeredNodeCount;
    uint256 public totalRewardsPaid;

    event NodeRegistered(
        bytes32 indexed nodeId,
        address indexed operator,
        bytes32 indexed hardwareCommitment,
        string nodeType,
        uint256 activationReward
    );
    event HeartbeatRewardClaimed(
        bytes32 indexed nodeId,
        address indexed operator,
        bytes32 indexed heartbeatProof,
        uint256 amount,
        uint256 heartbeatClaims
    );
    event NodeStatusChanged(bytes32 indexed nodeId, bool active);
    event RewardPolicyUpdated(
        uint256 activationReward,
        uint256 heartbeatReward,
        uint256 heartbeatInterval,
        uint256 maxHeartbeatClaimsPerOperator
    );
    event AdopterMerkleRootUpdated(bytes32 indexed root, bool gateRequired);

    constructor(
        address synTokenAddress,
        uint256 activationReward_,
        uint256 heartbeatReward_,
        uint256 heartbeatInterval_,
        uint256 maxHeartbeatClaimsPerOperator_
    ) {
        require(synTokenAddress != address(0), "invalid token");
        require(heartbeatInterval_ > 0, "invalid heartbeat interval");

        synToken = IERC20(synTokenAddress);
        activationReward = activationReward_;
        heartbeatReward = heartbeatReward_;
        heartbeatInterval = heartbeatInterval_;
        maxHeartbeatClaimsPerOperator = maxHeartbeatClaimsPerOperator_;
    }

    function registerAndClaim(
        bytes32 hardwareCommitment,
        string calldata nodeType
    ) external whenNotPaused returns (bytes32) {
        require(!adopterMerkleGateRequired, "merkle proof required");
        return _registerAndClaim(msg.sender, hardwareCommitment, nodeType);
    }

    function registerAndClaimWithProof(
        bytes32 hardwareCommitment,
        string calldata nodeType,
        bytes32[] calldata merkleProof
    ) external whenNotPaused returns (bytes32) {
        require(adopterMerkleRoot != bytes32(0), "merkle root not set");
        bytes32 leaf = adopterLeaf(msg.sender, hardwareCommitment, nodeType);
        require(!adopterMerkleLeafUsed[leaf], "merkle leaf used");
        require(MerkleProof.verify(merkleProof, adopterMerkleRoot, leaf), "invalid merkle proof");

        adopterMerkleLeafUsed[leaf] = true;
        return _registerAndClaim(msg.sender, hardwareCommitment, nodeType);
    }

    function adopterLeaf(
        address operator,
        bytes32 hardwareCommitment,
        string calldata nodeType
    ) public pure returns (bytes32) {
        return keccak256(
            bytes.concat(
                keccak256(
                    abi.encode(
                        operator,
                        hardwareCommitment,
                        keccak256(bytes(nodeType))
                    )
                )
            )
        );
    }

    function setAdopterMerkleRoot(
        bytes32 root,
        bool gateRequired
    ) external onlyOwner {
        require(!gateRequired || root != bytes32(0), "root required");
        adopterMerkleRoot = root;
        adopterMerkleGateRequired = gateRequired;
        emit AdopterMerkleRootUpdated(root, gateRequired);
    }

    function _registerAndClaim(
        address operator,
        bytes32 hardwareCommitment,
        string calldata nodeType
    ) internal returns (bytes32) {
        require(hardwareCommitment != bytes32(0), "hardware commitment required");
        require(bytes(nodeType).length > 0, "node type required");
        require(nodeByOperator[operator] == bytes32(0), "operator already registered");
        require(!hardwareCommitmentUsed[hardwareCommitment], "hardware already registered");

        bytes32 nodeId = keccak256(
            abi.encodePacked(operator, hardwareCommitment, block.chainid)
        );

        nodes[nodeId] = AdopterNode({
            operator: operator,
            hardwareCommitment: hardwareCommitment,
            nodeType: nodeType,
            registeredAt: block.timestamp,
            lastHeartbeatAt: block.timestamp,
            heartbeatClaims: 0,
            activationClaimed: true,
            active: true
        });

        nodeByOperator[operator] = nodeId;
        hardwareCommitmentUsed[hardwareCommitment] = true;
        registeredNodeCount++;

        _payReward(operator, activationReward);

        emit NodeRegistered(
            nodeId,
            operator,
            hardwareCommitment,
            nodeType,
            activationReward
        );

        return nodeId;
    }

    function claimHeartbeatReward(
        bytes32 nodeId,
        bytes32 heartbeatProof
    ) external whenNotPaused {
        AdopterNode storage node = nodes[nodeId];
        require(node.operator == msg.sender, "not node operator");
        require(node.active, "node inactive");
        require(heartbeatProof != bytes32(0), "heartbeat proof required");
        require(!heartbeatProofUsed[heartbeatProof], "heartbeat proof used");
        require(
            block.timestamp >= node.lastHeartbeatAt + heartbeatInterval,
            "heartbeat interval not met"
        );
        require(
            node.heartbeatClaims < maxHeartbeatClaimsPerOperator,
            "heartbeat reward cap reached"
        );

        heartbeatProofUsed[heartbeatProof] = true;
        node.lastHeartbeatAt = block.timestamp;
        node.heartbeatClaims++;

        _payReward(msg.sender, heartbeatReward);

        emit HeartbeatRewardClaimed(
            nodeId,
            msg.sender,
            heartbeatProof,
            heartbeatReward,
            node.heartbeatClaims
        );
    }

    function setNodeActive(bytes32 nodeId, bool active) external onlyOwner {
        require(nodes[nodeId].operator != address(0), "unknown node");
        nodes[nodeId].active = active;
        emit NodeStatusChanged(nodeId, active);
    }

    function setRewardPolicy(
        uint256 activationReward_,
        uint256 heartbeatReward_,
        uint256 heartbeatInterval_,
        uint256 maxHeartbeatClaimsPerOperator_
    ) external onlyOwner {
        require(heartbeatInterval_ > 0, "invalid heartbeat interval");
        activationReward = activationReward_;
        heartbeatReward = heartbeatReward_;
        heartbeatInterval = heartbeatInterval_;
        maxHeartbeatClaimsPerOperator = maxHeartbeatClaimsPerOperator_;

        emit RewardPolicyUpdated(
            activationReward_,
            heartbeatReward_,
            heartbeatInterval_,
            maxHeartbeatClaimsPerOperator_
        );
    }

    function pause() external onlyOwner {
        _pause();
    }

    function unpause() external onlyOwner {
        _unpause();
    }

    function availableRewards() external view returns (uint256) {
        return synToken.balanceOf(address(this));
    }

    function _payReward(address recipient, uint256 amount) internal {
        if (amount == 0) {
            return;
        }
        totalRewardsPaid += amount;
        synToken.safeTransfer(recipient, amount);
    }
}
