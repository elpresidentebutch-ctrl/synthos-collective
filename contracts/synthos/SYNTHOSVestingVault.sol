// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/finance/VestingWallet.sol";
import "@openzeppelin/contracts/token/ERC20/IERC20.sol";

/**
 * @title SYNTHOSVestingVault
 * @dev Minimal vesting vault for SYN allocations.
 *
 * Use this for team/foundation/community tranches so the supply is minted
 * once but released over time under predictable rules.
 */
contract SYNTHOSVestingVault is VestingWallet {
    IERC20 public immutable token;

    constructor(
        address beneficiary,
        uint64 startTimestamp,
        uint64 durationSeconds,
        address tokenAddress
    ) VestingWallet(beneficiary, startTimestamp, durationSeconds) {
        token = IERC20(tokenAddress);
    }

    function releaseToken() external {
        release(address(token));
    }

    function releasableToken() external view returns (uint256) {
        return releasable(address(token));
    }

    function releasedToken() external view returns (uint256) {
        return released(address(token));
    }
}

