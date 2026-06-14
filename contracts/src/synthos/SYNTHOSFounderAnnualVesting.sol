// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";

/**
 * @title SYNTHOSFounderAnnualVesting
 * @dev Releases a fixed annual SYN amount to the founder beneficiary.
 *
 * Deploy with May 29 UTC release timestamps for 2027 through 2036.
 * Fund this vault with SynCoin.FOUNDER_VESTING_ALLOCATION.
 */
contract SYNTHOSFounderAnnualVesting {
    using SafeERC20 for IERC20;

    IERC20 public immutable token;
    address public immutable beneficiary;
    uint256 public immutable annualAmount;
    uint256 public immutable totalAllocation;

    uint256[] private releaseTimestamps;
    uint256 public released;

    event FounderRelease(address indexed beneficiary, uint256 amount, uint256 releasedTotal);

    constructor(
        address tokenAddress,
        address beneficiaryAddress,
        uint256 annualAmount_,
        uint256[] memory releaseTimestamps_
    ) {
        require(tokenAddress != address(0), "invalid token");
        require(beneficiaryAddress != address(0), "invalid beneficiary");
        require(annualAmount_ > 0, "annual amount required");
        require(releaseTimestamps_.length > 0, "release schedule required");

        token = IERC20(tokenAddress);
        beneficiary = beneficiaryAddress;
        annualAmount = annualAmount_;
        totalAllocation = annualAmount_ * releaseTimestamps_.length;

        for (uint256 i = 0; i < releaseTimestamps_.length; i++) {
            require(releaseTimestamps_[i] > block.timestamp, "release must be future");
            if (i > 0) {
                require(releaseTimestamps_[i] > releaseTimestamps_[i - 1], "schedule not sorted");
            }
            releaseTimestamps.push(releaseTimestamps_[i]);
        }
    }

    function releaseCount() external view returns (uint256) {
        return releaseTimestamps.length;
    }

    function releaseTimestamp(uint256 index) external view returns (uint256) {
        return releaseTimestamps[index];
    }

    function vestedAmount(uint256 timestamp) public view returns (uint256) {
        uint256 releasesElapsed = 0;
        for (uint256 i = 0; i < releaseTimestamps.length; i++) {
            if (timestamp < releaseTimestamps[i]) {
                break;
            }
            releasesElapsed++;
        }

        return annualAmount * releasesElapsed;
    }

    function releasable() public view returns (uint256) {
        return vestedAmount(block.timestamp) - released;
    }

    function release() external {
        uint256 amount = releasable();
        require(amount > 0, "nothing releasable");

        released += amount;
        token.safeTransfer(beneficiary, amount);

        emit FounderRelease(beneficiary, amount, released);
    }
}
