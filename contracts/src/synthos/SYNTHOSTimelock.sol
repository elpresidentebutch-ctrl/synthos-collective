// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/governance/TimelockController.sol";

/**
 * @title SYNTHOSTimelock
 * @dev Wrapper around OpenZeppelin TimelockController.
 *
 * Deploy with:
 * - minDelay: e.g. 2 days
 * - proposers: governance contract(s)
 * - executors: address(0) to allow anyone to execute queued operations
 * - admin: a multisig for emergency role management OR address(0) to fully renounce
 */
contract SYNTHOSTimelock is TimelockController {
    constructor(
        uint256 minDelay,
        address[] memory proposers,
        address[] memory executors,
        address admin
    ) TimelockController(minDelay, proposers, executors, admin) {}
}

