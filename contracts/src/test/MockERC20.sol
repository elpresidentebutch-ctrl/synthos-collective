// SPDX-License-Identifier: Apache-2.0
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/token/ERC20/ERC20.sol";

/**
 * @dev Test asset for local DEX launch rehearsals.
 * Do not deploy this as a production asset.
 */
contract MockERC20 is ERC20 {
    constructor(
        string memory name_,
        string memory symbol_,
        address recipient,
        uint256 initialSupply
    ) ERC20(name_, symbol_) {
        _mint(recipient, initialSupply);
    }
}
