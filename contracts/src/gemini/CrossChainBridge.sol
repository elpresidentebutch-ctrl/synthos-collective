// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/**
 * @title CrossChainBridge
 * @dev Bridge contract enabling cross-chain token transfers between SYNTHOS and Gemini Megachain 2.0
 * 
 * Features:
 * - Lock-and-mint mechanism for bridge transfers
 * - Multi-chain support (SYNTHOS chain, Gemini chain, Ethereum, etc.)
 * - Fee collection for bridge operations
 * - Validator set for cross-chain verification
 * - Pause/resume functionality
 */
contract CrossChainBridge {
    // Constants
    uint256 public constant BRIDGE_FEE = 25; // 0.25% fee
    uint256 public constant MIN_TRANSFER = 1 ether; // Minimum token amount
    uint256 public constant CHALLENGE_WINDOW = 1 days;

    // Chain IDs
    uint256 public constant SYNTHOS_CHAIN_ID = 1234;
    uint256 public constant GEMINI_CHAIN_ID = 2048;
    uint256 public constant ETHEREUM_CHAIN_ID = 1;
    uint256 public constant POLYGON_CHAIN_ID = 137;

    // Token references per chain
    mapping(uint256 => address) public chain_tokens;

    // Bridge state
    struct BridgeTransfer {
        uint256 transfer_id;
        address sender;
        address recipient;
        uint256 amount;
        uint256 source_chain_id;
        uint256 destination_chain_id;
        uint256 timestamp;
        BridgeStatus status;
        uint256 signatures_count;
        mapping(address => bool) validator_signed;
    }

    enum BridgeStatus {
        PENDING,
        CONFIRMED,
        EXECUTED,
        FAILED,
        CHALLENGED
    }

    // State variables
    mapping(uint256 => BridgeTransfer) public bridge_transfers;
    uint256 public transfer_nonce = 0;

    uint256 public locked_tokens = 0;
    uint256 public bridge_fee_collected = 0;

    // Validator set
    mapping(address => bool) public validators;
    address[] public validator_list;
    uint256 public required_signatures;

    // Rate limiting
    mapping(address => uint256) public last_transfer_time;
    uint256 public transfer_cooldown = 1 minutes;

    // Pause flag
    bool public paused = false;

    // Events
    event TokensLocked(
        uint256 indexed transfer_id,
        address indexed sender,
        uint256 amount,
        uint256 source_chain_id,
        uint256 destination_chain_id
    );

    event TokensMinted(
        uint256 indexed transfer_id,
        address indexed recipient,
        uint256 amount,
        uint256 source_chain_id
    );

    event ValidatorAdded(address indexed validator);
    event ValidatorRemoved(address indexed validator);
    event SignatureReceived(uint256 indexed transfer_id, address indexed validator);
    event TransferConfirmed(uint256 indexed transfer_id);
    event TransferExecuted(uint256 indexed transfer_id);
    event TransferFailed(uint256 indexed transfer_id, string reason);
    event BridgePaused();
    event BridgeResumed();

    constructor() {
        required_signatures = 1; // Can be updated by governance
    }

    /**
     * @dev Register validator for cross-chain verification
     * @param validator_addr Validator address
     */
    function addValidator(address validator_addr) public {
        require(validator_addr != address(0), "Invalid validator");
        require(!validators[validator_addr], "Already validator");

        validators[validator_addr] = true;
        validator_list.push(validator_addr);

        emit ValidatorAdded(validator_addr);
    }

    /**
     * @dev Remove validator from set
     * @param validator_addr Validator to remove
     */
    function removeValidator(address validator_addr) public {
        require(validators[validator_addr], "Not a validator");

        validators[validator_addr] = false;

        // Remove from list
        for (uint256 i = 0; i < validator_list.length; i++) {
            if (validator_list[i] == validator_addr) {
                validator_list[i] = validator_list[validator_list.length - 1];
                validator_list.pop();
                break;
            }
        }

        emit ValidatorRemoved(validator_addr);
    }

    /**
     * @dev Initiate cross-chain transfer
     * @param recipient Recipient on destination chain
     * @param amount Amount to transfer
     * @param destination_chain_id Destination chain ID
     */
    function initiateTransfer(
        address recipient,
        uint256 amount,
        uint256 destination_chain_id
    ) 
        public 
        payable 
        returns (uint256) 
    {
        require(!paused, "Bridge is paused");
        require(recipient != address(0), "Invalid recipient");
        require(amount >= MIN_TRANSFER, "Amount below minimum");
        require(msg.value >= calculateBridgeFee(amount), "Insufficient fee");
        require(
            block.timestamp >= last_transfer_time[msg.sender] + transfer_cooldown,
            "Transfer cooldown active"
        );

        uint256 transfer_id = transfer_nonce++;

        BridgeTransfer storage transfer = bridge_transfers[transfer_id];
        transfer.transfer_id = transfer_id;
        transfer.sender = msg.sender;
        transfer.recipient = recipient;
        transfer.amount = amount;
        transfer.source_chain_id = block.chainid;
        transfer.destination_chain_id = destination_chain_id;
        transfer.timestamp = block.timestamp;
        transfer.status = BridgeStatus.PENDING;
        transfer.signatures_count = 0;

        locked_tokens += amount;
        last_transfer_time[msg.sender] = block.timestamp;

        // Add fee to collected fees
        uint256 fee = calculateBridgeFee(amount);
        bridge_fee_collected += fee;

        emit TokensLocked(
            transfer_id,
            msg.sender,
            amount,
            block.chainid,
            destination_chain_id
        );

        return transfer_id;
    }

    /**
     * @dev Validator confirms cross-chain transfer
     * @param transfer_id Transfer ID
     */
    function confirmTransfer(uint256 transfer_id) public {
        require(validators[msg.sender], "Not a validator");

        BridgeTransfer storage transfer = bridge_transfers[transfer_id];
        require(transfer.amount > 0, "Invalid transfer");
        require(!transfer.validator_signed[msg.sender], "Already signed");
        require(transfer.status == BridgeStatus.PENDING, "Wrong status");

        transfer.validator_signed[msg.sender] = true;
        transfer.signatures_count++;

        emit SignatureReceived(transfer_id, msg.sender);

        // Check if we have enough signatures
        if (transfer.signatures_count >= required_signatures) {
            transfer.status = BridgeStatus.CONFIRMED;
            emit TransferConfirmed(transfer_id);
        }
    }

    /**
     * @dev Execute confirmed transfer on destination chain
     * @param transfer_id Transfer ID
     */
    function executeTransfer(uint256 transfer_id) public {
        BridgeTransfer storage transfer = bridge_transfers[transfer_id];
        
        require(transfer.amount > 0, "Invalid transfer");
        require(transfer.status == BridgeStatus.CONFIRMED, "Not confirmed");
        require(
            block.timestamp >= transfer.timestamp + 1 minutes,
            "Must wait for confirmation"
        );

        transfer.status = BridgeStatus.EXECUTED;
        locked_tokens -= transfer.amount;

        emit TokensMinted(
            transfer_id,
            transfer.recipient,
            transfer.amount,
            transfer.source_chain_id
        );

        emit TransferExecuted(transfer_id);
    }

    /**
     * @dev Challenge a transfer if fraud is detected
     * @param transfer_id Transfer ID to challenge
     */
    function challengeTransfer(uint256 transfer_id) public {
        BridgeTransfer storage transfer = bridge_transfers[transfer_id];
        
        require(transfer.amount > 0, "Invalid transfer");
        require(
            block.timestamp <= transfer.timestamp + CHALLENGE_WINDOW,
            "Challenge window expired"
        );
        require(transfer.status == BridgeStatus.PENDING, "Can't challenge");

        transfer.status = BridgeStatus.CHALLENGED;

        // Refund sender (or holder)
        locked_tokens -= transfer.amount;

        emit TransferFailed(transfer_id, "Transfer challenged due to fraud suspicion");
    }

    /**
     * @dev Register token for a chain
     * @param chain_id Chain ID
     * @param token_addr Token address on that chain
     */
    function registerToken(uint256 chain_id, address token_addr) public {
        require(chain_id > 0, "Invalid chain ID");
        require(token_addr != address(0), "Invalid token address");

        chain_tokens[chain_id] = token_addr;
    }

    /**
     * @dev Calculate bridge fee for amount
     * @param amount Token amount
     * @return Fee amount
     */
    function calculateBridgeFee(uint256 amount) public pure returns (uint256) {
        return (amount * BRIDGE_FEE) / 10000; // 0.25% fee
    }

    /**
     * @dev Set required number of validator signatures
     * @param num_signatures Required signatures
     */
    function setRequiredSignatures(uint256 num_signatures) public {
        require(num_signatures > 0, "Must require at least 1 signature");
        require(num_signatures <= validator_list.length, "More than validators");
        required_signatures = num_signatures;
    }

    /**
     * @dev Pause bridge operations
     */
    function pauseBridge() public {
        paused = true;
        emit BridgePaused();
    }

    /**
     * @dev Resume bridge operations
     */
    function resumeBridge() public {
        paused = false;
        emit BridgeResumed();
    }

    /**
     * @dev Get transfer details
     * @param transfer_id Transfer ID
     */
    function getTransferDetails(uint256 transfer_id) 
        public 
        view 
        returns (
            address sender,
            address recipient,
            uint256 amount,
            uint256 source_chain_id,
            uint256 destination_chain_id,
            uint256 timestamp,
            BridgeStatus status,
            uint256 signatures_count
        ) 
    {
        BridgeTransfer storage transfer = bridge_transfers[transfer_id];
        return (
            transfer.sender,
            transfer.recipient,
            transfer.amount,
            transfer.source_chain_id,
            transfer.destination_chain_id,
            transfer.timestamp,
            transfer.status,
            transfer.signatures_count
        );
    }

    /**
     * @dev Get number of validators
     * @return Validator count
     */
    function getValidatorCount() public view returns (uint256) {
        return validator_list.length;
    }

    /**
     * @dev Get bridge fee collected
     * @return Fee amount
     */
    function getBridgeFeeCollected() public view returns (uint256) {
        return bridge_fee_collected;
    }

    /**
     * @dev Get locked tokens total
     * @return Locked amount
     */
    function getLockedTokens() public view returns (uint256) {
        return locked_tokens;
    }
}
