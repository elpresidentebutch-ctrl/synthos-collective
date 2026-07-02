// SPDX-License-Identifier: Apache-2.0
pragma solidity ^0.8.20;

/**
 * @title SYNTHOSMultisig
 * @dev Sovereign fixed-owner multisig for launch administration.
 *
 * Owners submit arbitrary calls, other owners confirm them, and anyone can
 * execute once the threshold is met. Owner rotation should happen through a
 * newly deployed multisig plus timelock/governance handoff.
 */
contract SYNTHOSMultisig {
    struct Transaction {
        address target;
        uint256 value;
        bytes data;
        bool executed;
        uint256 confirmations;
    }

    address[] private _owners;

    uint256 public immutable threshold;
    uint256 public transactionCount;

    mapping(address => bool) public isOwner;
    mapping(uint256 => Transaction) private _transactions;
    mapping(uint256 => mapping(address => bool)) public isConfirmed;

    event Deposit(address indexed sender, uint256 amount);
    event TransactionSubmitted(
        uint256 indexed transactionId,
        address indexed owner,
        address indexed target,
        uint256 value,
        bytes data
    );
    event TransactionConfirmed(uint256 indexed transactionId, address indexed owner);
    event TransactionRevoked(uint256 indexed transactionId, address indexed owner);
    event TransactionExecuted(uint256 indexed transactionId, address indexed executor);

    modifier onlyOwner() {
        require(isOwner[msg.sender], "not owner");
        _;
    }

    modifier transactionExists(uint256 transactionId) {
        require(transactionId < transactionCount, "transaction does not exist");
        _;
    }

    modifier notExecuted(uint256 transactionId) {
        require(!_transactions[transactionId].executed, "transaction already executed");
        _;
    }

    constructor(address[] memory owners_, uint256 threshold_) {
        require(owners_.length > 0, "owners required");
        require(threshold_ > 0, "threshold required");
        require(threshold_ <= owners_.length, "threshold exceeds owners");

        for (uint256 i = 0; i < owners_.length; i++) {
            address owner = owners_[i];
            require(owner != address(0), "invalid owner");
            require(!isOwner[owner], "duplicate owner");

            isOwner[owner] = true;
            _owners.push(owner);
        }

        threshold = threshold_;
    }

    receive() external payable {
        emit Deposit(msg.sender, msg.value);
    }

    function owners() external view returns (address[] memory) {
        return _owners;
    }

    function submitTransaction(
        address target,
        uint256 value,
        bytes calldata data
    ) external onlyOwner returns (uint256 transactionId) {
        require(target != address(0), "invalid target");

        transactionId = transactionCount;
        _transactions[transactionId] = Transaction({
            target: target,
            value: value,
            data: data,
            executed: false,
            confirmations: 0
        });
        transactionCount++;

        emit TransactionSubmitted(transactionId, msg.sender, target, value, data);
        _confirmTransaction(transactionId);
    }

    function confirmTransaction(
        uint256 transactionId
    )
        external
        onlyOwner
        transactionExists(transactionId)
        notExecuted(transactionId)
    {
        _confirmTransaction(transactionId);
    }

    function revokeConfirmation(
        uint256 transactionId
    )
        external
        onlyOwner
        transactionExists(transactionId)
        notExecuted(transactionId)
    {
        require(isConfirmed[transactionId][msg.sender], "not confirmed");

        isConfirmed[transactionId][msg.sender] = false;
        _transactions[transactionId].confirmations--;

        emit TransactionRevoked(transactionId, msg.sender);
    }

    function executeTransaction(
        uint256 transactionId
    ) external transactionExists(transactionId) notExecuted(transactionId) {
        Transaction storage transaction = _transactions[transactionId];
        require(transaction.confirmations >= threshold, "insufficient confirmations");

        transaction.executed = true;
        (bool ok, ) = transaction.target.call{value: transaction.value}(transaction.data);
        require(ok, "transaction failed");

        emit TransactionExecuted(transactionId, msg.sender);
    }

    function getTransaction(
        uint256 transactionId
    )
        external
        view
        transactionExists(transactionId)
        returns (
            address target,
            uint256 value,
            bytes memory data,
            bool executed,
            uint256 confirmations
        )
    {
        Transaction storage transaction = _transactions[transactionId];
        return (
            transaction.target,
            transaction.value,
            transaction.data,
            transaction.executed,
            transaction.confirmations
        );
    }

    function _confirmTransaction(uint256 transactionId) private {
        require(!isConfirmed[transactionId][msg.sender], "already confirmed");

        isConfirmed[transactionId][msg.sender] = true;
        _transactions[transactionId].confirmations++;

        emit TransactionConfirmed(transactionId, msg.sender);
    }
}
