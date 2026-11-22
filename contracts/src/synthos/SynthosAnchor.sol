// SPDX-License-Identifier: Apache-2.0
pragma solidity ^0.8.24;

/// @title SynthosAnchor
/// @notice Records block hashes of the SYNTHOS Collective CometBFT chain
///         ("synthos-mainnet-1") on Base, so anyone can check SYNTHOS
///         history against a public, independent ledger.
/// @dev One trusted relayer posts (height, block hash, app hash, block time).
///      Heights must strictly increase. The owner can rotate the relayer.
contract SynthosAnchor {
    string public constant SYNTHOS_CHAIN_ID = "synthos-mainnet-1";

    struct Anchor {
        bytes32 blockHash;  // CometBFT block_id.hash
        bytes32 appHash;    // CometBFT header.app_hash
        uint64 blockTime;   // SYNTHOS block time (unix seconds)
        uint64 anchoredAt;  // Base block.timestamp when recorded
    }

    address public owner;
    address public relayer;
    uint64 public latestHeight;
    uint64 public anchorCount;
    mapping(uint64 => Anchor) public anchors;

    event Anchored(uint64 indexed height, bytes32 blockHash, bytes32 appHash, uint64 blockTime);
    event RelayerChanged(address indexed relayer);
    event OwnerChanged(address indexed owner);

    error NotOwner();
    error NotRelayer();
    error HeightNotIncreasing();
    error ZeroHash();
    error ZeroAddress();

    constructor(address initialRelayer) {
        owner = msg.sender;
        relayer = initialRelayer == address(0) ? msg.sender : initialRelayer;
        emit OwnerChanged(owner);
        emit RelayerChanged(relayer);
    }

    function anchor(uint64 height, bytes32 blockHash, bytes32 appHash, uint64 blockTime) external {
        if (msg.sender != relayer) revert NotRelayer();
        if (height <= latestHeight) revert HeightNotIncreasing();
        if (blockHash == bytes32(0)) revert ZeroHash();
        anchors[height] = Anchor(blockHash, appHash, blockTime, uint64(block.timestamp));
        latestHeight = height;
        unchecked { anchorCount++; }
        emit Anchored(height, blockHash, appHash, blockTime);
    }

    function latest() external view returns (uint64 height, Anchor memory a) {
        return (latestHeight, anchors[latestHeight]);
    }

    function setRelayer(address newRelayer) external {
        if (msg.sender != owner) revert NotOwner();
        if (newRelayer == address(0)) revert ZeroAddress();
        relayer = newRelayer;
        emit RelayerChanged(newRelayer);
    }

    function transferOwnership(address newOwner) external {
        if (msg.sender != owner) revert NotOwner();
        if (newOwner == address(0)) revert ZeroAddress();
        owner = newOwner;
        emit OwnerChanged(newOwner);
    }
}
