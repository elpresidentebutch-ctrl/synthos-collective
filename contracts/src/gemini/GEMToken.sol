// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/token/ERC20/ERC20.sol";
import "@openzeppelin/contracts/token/ERC20/extensions/ERC20Burnable.sol";
import "@openzeppelin/contracts/access/Ownable.sol";
import "@openzeppelin/contracts/security/Pausable.sol";

/**
 * @title GEMToken
 * @dev Gemini Megachain 2.0 native token with multi-chain support
 * 
 * Features:
 * - Standard ERC-20 interface
 * - Multi-chain bridge support
 * - Burnable for deflation
 * - Pausable for emergency
 * - Fee-on-transfer support for ecosystem fund
 */
contract GEMToken is ERC20, ERC20Burnable, Ownable, Pausable {
    // Constants
    uint256 public constant INITIAL_SUPPLY = 2_000_000_000 * 10**18; // 2 billion GEM
    uint256 public constant ECOSYSTEM_FEE = 25; // 0.25% fee to ecosystem fund

    // State variables
    address public ecosystem_fund;
    address public bridge_contract;
    mapping(address => bool) public fee_exempt;
    uint256 public total_fees_collected = 0;

    // Multi-chain support
    mapping(uint256 => address) public bridge_addresses; // Chain ID => Bridge address
    mapping(address => uint256) public bridge_mints; // Track minted tokens from bridge

    // Events
    event EcosystemFeeUpdated(uint256 new_fee);
    event EcosystemFundChanged(address indexed old_fund, address indexed new_fund);
    event BridgeContractSet(address indexed bridge);
    event BridgeMint(address indexed recipient, uint256 amount, uint256 chain_id);
    event BridgeBurn(address indexed burner, uint256 amount, uint256 destination_chain_id);
    event FeeExemptionChanged(address indexed account, bool exempt);

    constructor(address ecosystem_fund_addr) ERC20("Gemini", "GEM") {
        ecosystem_fund = ecosystem_fund_addr;
        fee_exempt[msg.sender] = true;
        fee_exempt[ecosystem_fund_addr] = true;
        
        // Initial mint to contract
        _mint(msg.sender, INITIAL_SUPPLY);
    }

    /**
     * @dev Transfer tokens with ecosystem fee
     * @param to Recipient address
     * @param amount Amount to transfer
     * @return Success flag
     */
    function transfer(address to, uint256 amount) 
        public 
        override 
        whenNotPaused 
        returns (bool) 
    {
        return _transferWithFee(msg.sender, to, amount);
    }

    /**
     * @dev Transfer from with ecosystem fee
     * @param from Sender address
     * @param to Recipient address
     * @param amount Amount to transfer
     * @return Success flag
     */
    function transferFrom(address from, address to, uint256 amount) 
        public 
        override 
        whenNotPaused 
        returns (bool) 
    {
        address spender = _msgSender();
        _spendAllowance(from, spender, amount);
        return _transferWithFee(from, to, amount);
    }

    /**
     * @dev Internal transfer with fee logic
     * @param from Sender
     * @param to Recipient
     * @param amount Amount
     * @return Success flag
     */
    function _transferWithFee(address from, address to, uint256 amount) 
        internal 
        returns (bool) 
    {
        require(from != address(0), "Invalid from address");
        require(to != address(0), "Invalid to address");

        // Check if either address is fee exempt
        if (fee_exempt[from] || fee_exempt[to]) {
            _transfer(from, to, amount);
            return true;
        }

        // Calculate fee
        uint256 fee_amount = (amount * ECOSYSTEM_FEE) / 10000; // 0.25% = 25/10000
        uint256 transfer_amount = amount - fee_amount;

        // Transfer fee to ecosystem fund
        if (fee_amount > 0) {
            _transfer(from, ecosystem_fund, fee_amount);
            total_fees_collected += fee_amount;
        }

        // Transfer remaining to recipient
        _transfer(from, to, transfer_amount);
        return true;
    }

    /**
     * @dev Set ecosystem fund address
     * @param new_fund New ecosystem fund address
     */
    function setEcosystemFund(address new_fund) public onlyOwner {
        require(new_fund != address(0), "Invalid address");
        address old_fund = ecosystem_fund;
        ecosystem_fund = new_fund;
        fee_exempt[new_fund] = true;
        emit EcosystemFundChanged(old_fund, new_fund);
    }

    /**
     * @dev Set fee exemption for address
     * @param account Address to modify
     * @param exempt Whether to exempt from fees
     */
    function setFeeExemption(address account, bool exempt) public onlyOwner {
        require(account != address(0), "Invalid address");
        fee_exempt[account] = exempt;
        emit FeeExemptionChanged(account, exempt);
    }

    /**
     * @dev Set bridge contract address
     * @param bridge_addr Bridge contract address
     */
    function setBridgeContract(address bridge_addr) public onlyOwner {
        require(bridge_addr != address(0), "Invalid address");
        bridge_contract = bridge_addr;
        fee_exempt[bridge_addr] = true;
        emit BridgeContractSet(bridge_addr);
    }

    /**
     * @dev Mint tokens from bridge
     * @param recipient Recipient address
     * @param amount Amount to mint
     * @param source_chain_id Source chain ID
     */
    function bridgeMint(address recipient, uint256 amount, uint256 source_chain_id) 
        public 
    {
        require(msg.sender == bridge_contract, "Only bridge can mint");
        require(recipient != address(0), "Invalid recipient");
        require(amount > 0, "Invalid amount");

        _mint(recipient, amount);
        bridge_mints[recipient] += amount;

        emit BridgeMint(recipient, amount, source_chain_id);
    }

    /**
     * @dev Burn tokens for cross-chain transfer
     * @param amount Amount to burn
     * @param destination_chain_id Destination chain ID
     */
    function bridgeBurn(uint256 amount, uint256 destination_chain_id) public {
        require(amount > 0, "Invalid amount");
        require(destination_chain_id > 0, "Invalid chain ID");

        _burn(msg.sender, amount);
        if (bridge_mints[msg.sender] >= amount) {
            bridge_mints[msg.sender] -= amount;
        }

        emit BridgeBurn(msg.sender, amount, destination_chain_id);
    }

    /**
     * @dev Set bridge address for specific chain
     * @param chain_id Chain ID
     * @param bridge_addr Bridge address on that chain
     */
    function setBridgeAddress(uint256 chain_id, address bridge_addr) public onlyOwner {
        require(chain_id > 0, "Invalid chain ID");
        require(bridge_addr != address(0), "Invalid address");
        bridge_addresses[chain_id] = bridge_addr;
    }

    /**
     * @dev Pause token transfers (emergency)
     */
    function pause() public onlyOwner {
        _pause();
    }

    /**
     * @dev Resume token transfers
     */
    function unpause() public onlyOwner {
        _unpause();
    }

    /**
     * @dev Burn tokens (deflation)
     * @param amount Amount to burn
     */
    function burn(uint256 amount) public override {
        super.burn(amount);
    }

    /**
     * @dev Get total fees collected
     * @return Total collected fees
     */
    function getTotalFeesCollected() public view returns (uint256) {
        return total_fees_collected;
    }

    /**
     * @dev Check if address is fee exempt
     * @param account Account to check
     * @return Whether account is exempt
     */
    function isFeeExempt(address account) public view returns (bool) {
        return fee_exempt[account];
    }
}
