// SPDX-License-Identifier: Apache-2.0
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/access/Ownable.sol";
import "@openzeppelin/contracts/security/Pausable.sol";
import "@openzeppelin/contracts/security/ReentrancyGuard.sol";
import "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";

import "./SYNTHOSComplianceRegistry.sol";

/**
 * @title SYNTHOSBitcoinAdopterSale
 * @dev Confirmer-attested SYN sale for buyers who pay with native Bitcoin.
 *
 * Native BTC settles on the Bitcoin network, which this EVM contract cannot
 * observe on its own. The buyer sends BTC off-chain to a SYNTHOS-controlled
 * Bitcoin address; an authorized confirmer then records the verified payment
 * here so SYN releases under the same eligibility, minimum-purchase,
 * wallet-cap, and allocation rules as the crypto-native early adopter sale.
 *
 * This contract does not and cannot verify Bitcoin transactions itself. It
 * trusts whoever holds the confirmer role to have checked the payment on the
 * Bitcoin network (sufficient confirmations, correct address, correct amount)
 * before calling confirmBitcoinPayment. A compromised or dishonest confirmer
 * key can release SYN without a real BTC payment ever happening, so the
 * confirmer key must be held to the same custody standard as a treasury
 * signer, not left on a routine hot wallet.
 */
contract SYNTHOSBitcoinAdopterSale is Ownable, Pausable, ReentrancyGuard {
    using SafeERC20 for IERC20;

    uint256 public constant USD_PRICE_PER_SYN_18 = 1 * 10 ** 17; // $0.10
    uint256 public constant SATOSHIS_PER_BTC = 1e8;

    IERC20 public immutable synToken;
    SYNTHOSComplianceRegistry public immutable complianceRegistry;
    address public confirmer;
    uint256 public maxSaleAllocation;
    uint256 public totalSynSold;
    uint256 public minSynPurchase;
    uint256 public maxSynPerWallet;
    uint256 public btcUsdPrice18;

    mapping(address => uint256) public purchasedByWallet;
    mapping(bytes32 => bool) public processedBtcTx;

    event ConfirmerUpdated(address indexed previousConfirmer, address indexed newConfirmer);
    event BtcUsdPriceUpdated(uint256 usdPrice18);
    event SaleLimitsUpdated(uint256 maxSaleAllocation, uint256 minSynPurchase, uint256 maxSynPerWallet);
    event BitcoinPurchaseConfirmed(
        bytes32 indexed btcTxId,
        address indexed beneficiary,
        uint256 satoshis,
        uint256 synAmount,
        uint256 usdValue18
    );

    modifier onlyConfirmer() {
        require(msg.sender == confirmer, "not confirmer");
        _;
    }

    constructor(
        address synTokenAddress,
        address complianceRegistryAddress,
        address confirmerAddress,
        uint256 maxSaleAllocation_,
        uint256 minSynPurchase_,
        uint256 maxSynPerWallet_
    ) {
        require(synTokenAddress != address(0), "invalid SYN");
        require(complianceRegistryAddress != address(0), "invalid compliance");
        require(confirmerAddress != address(0), "invalid confirmer");
        require(maxSaleAllocation_ > 0, "allocation required");

        synToken = IERC20(synTokenAddress);
        complianceRegistry = SYNTHOSComplianceRegistry(complianceRegistryAddress);
        confirmer = confirmerAddress;
        maxSaleAllocation = maxSaleAllocation_;
        minSynPurchase = minSynPurchase_;
        maxSynPerWallet = maxSynPerWallet_;
    }

    /**
     * @dev Records an off-chain-verified Bitcoin payment and releases SYN.
     * @param btcTxId keccak256 of the Bitcoin transaction id, used to block double-crediting.
     * @param satoshis the confirmed payment amount, in satoshis (1 BTC = 100,000,000 satoshis).
     * @param beneficiary the SYN-receiving EVM address the buyer registered with their BTC payment.
     */
    function confirmBitcoinPayment(
        bytes32 btcTxId,
        uint256 satoshis,
        address beneficiary
    ) external onlyConfirmer whenNotPaused nonReentrant returns (uint256) {
        require(btcTxId != bytes32(0), "invalid btc tx");
        require(!processedBtcTx[btcTxId], "btc tx already processed");
        require(satoshis > 0, "payment required");
        require(beneficiary != address(0), "invalid beneficiary");
        require(btcUsdPrice18 > 0, "btc price not set");

        uint256 usdValue18 = (satoshis * btcUsdPrice18) / SATOSHIS_PER_BTC;
        uint256 synAmount = _quoteSyn(usdValue18);
        _validatePurchase(beneficiary, synAmount);

        processedBtcTx[btcTxId] = true;
        totalSynSold += synAmount;
        purchasedByWallet[beneficiary] += synAmount;

        synToken.safeTransfer(beneficiary, synAmount);

        emit BitcoinPurchaseConfirmed(btcTxId, beneficiary, satoshis, synAmount, usdValue18);
        return synAmount;
    }

    function quoteBitcoinPurchase(
        uint256 satoshis
    ) external view returns (uint256 synAmount, uint256 usdValue18) {
        require(btcUsdPrice18 > 0, "btc price not set");
        usdValue18 = (satoshis * btcUsdPrice18) / SATOSHIS_PER_BTC;
        synAmount = _quoteSyn(usdValue18);
    }

    function setConfirmer(address newConfirmer) external onlyOwner {
        require(newConfirmer != address(0), "invalid confirmer");
        address previous = confirmer;
        confirmer = newConfirmer;
        emit ConfirmerUpdated(previous, newConfirmer);
    }

    function setBtcUsdPrice(uint256 usdPrice18) external onlyOwner {
        require(usdPrice18 > 0, "invalid price");
        btcUsdPrice18 = usdPrice18;
        emit BtcUsdPriceUpdated(usdPrice18);
    }

    function setSaleLimits(
        uint256 maxSaleAllocation_,
        uint256 minSynPurchase_,
        uint256 maxSynPerWallet_
    ) external onlyOwner {
        require(maxSaleAllocation_ >= totalSynSold, "below sold amount");
        maxSaleAllocation = maxSaleAllocation_;
        minSynPurchase = minSynPurchase_;
        maxSynPerWallet = maxSynPerWallet_;
        emit SaleLimitsUpdated(maxSaleAllocation_, minSynPurchase_, maxSynPerWallet_);
    }

    function pause() external onlyOwner {
        _pause();
    }

    function unpause() external onlyOwner {
        _unpause();
    }

    function withdrawUnsoldSyn(address recipient, uint256 amount) external onlyOwner {
        require(recipient != address(0), "invalid recipient");
        synToken.safeTransfer(recipient, amount);
    }

    function availableSyn() external view returns (uint256) {
        return synToken.balanceOf(address(this));
    }

    function _quoteSyn(uint256 usdValue18) internal pure returns (uint256) {
        return (usdValue18 * 1 ether) / USD_PRICE_PER_SYN_18;
    }

    function _validatePurchase(address beneficiary, uint256 synAmount) internal view {
        require(synAmount > 0, "payment too small");
        require(synAmount >= minSynPurchase, "below minimum");
        require(totalSynSold + synAmount <= maxSaleAllocation, "sale allocation exhausted");
        if (maxSynPerWallet > 0) {
            require(purchasedByWallet[beneficiary] + synAmount <= maxSynPerWallet, "wallet cap exceeded");
        }
        require(synToken.balanceOf(address(this)) >= synAmount, "insufficient sale inventory");
        require(
            complianceRegistry.eligibleToReceive(
                beneficiary,
                SYNTHOSComplianceRegistry.RecipientCategory.Community
            ),
            "buyer not eligible"
        );
    }
}
