// SPDX-License-Identifier: Apache-2.0
pragma solidity ^0.8.20;

import "./SYNTHOSEarlyAdopterSale.sol";

/**
 * @title SYNTHOSEarlyAdopterPresale
 * @dev Founder-facing name for the automatic early adopter pre-sale.
 *
 * The inherited contract accepts enabled crypto payment assets, prices SYN at
 * $0.10, and delivers SYN immediately from the pre-funded 250M SYN tranche.
 */
contract SYNTHOSEarlyAdopterPresale is SYNTHOSEarlyAdopterSale {
    constructor(
        address synTokenAddress,
        address complianceRegistryAddress,
        address treasuryAddress,
        uint256 maxSaleAllocation_,
        uint256 minSynPurchase_,
        uint256 maxSynPerWallet_
    )
        SYNTHOSEarlyAdopterSale(
            synTokenAddress,
            complianceRegistryAddress,
            treasuryAddress,
            maxSaleAllocation_,
            minSynPurchase_,
            maxSynPerWallet_
        )
    {}
}
