// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/access/Ownable.sol";
import "@openzeppelin/contracts/token/ERC20/IERC20.sol";

/**
 * @title SYNTHOSTreasury
 * @dev Simple, hardened treasury vault.
 *
 * Security model:
 * - The owner SHOULD be a timelock or multisig (not an EOA).
 * - Treasury holds ERC20 tokens (like SYN) and can release them only via owner actions.
 */
contract SYNTHOSTreasury is Ownable {
    event ERC20Transferred(address indexed token, address indexed to, uint256 amount);
    event ETHTransferred(address indexed to, uint256 amount);
    event ERC20Approved(address indexed token, address indexed spender, uint256 amount);

    constructor(address owner_) {
        _transferOwnership(owner_);
    }

    receive() external payable {}

    function transferERC20(address token, address to, uint256 amount) external onlyOwner {
        require(to != address(0), "Invalid recipient");
        require(amount > 0, "Amount must be positive");
        require(IERC20(token).transfer(to, amount), "Transfer failed");
        emit ERC20Transferred(token, to, amount);
    }

    function approveERC20(address token, address spender, uint256 amount) external onlyOwner {
        require(spender != address(0), "Invalid spender");
        require(IERC20(token).approve(spender, amount), "Approve failed");
        emit ERC20Approved(token, spender, amount);
    }

    function transferETH(address payable to, uint256 amount) external onlyOwner {
        require(to != address(0), "Invalid recipient");
        require(amount > 0, "Amount must be positive");
        (bool ok, ) = to.call{value: amount}("");
        require(ok, "ETH transfer failed");
        emit ETHTransferred(to, amount);
    }
}

