// SPDX-License-Identifier: Apache-2.0
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/access/Ownable.sol";
import "@openzeppelin/contracts/security/Pausable.sol";
import "@openzeppelin/contracts/security/ReentrancyGuard.sol";
import "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import "@openzeppelin/contracts/token/ERC20/extensions/IERC20Metadata.sol";
import "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";

import "./SYNTHOSComplianceRegistry.sol";

/**
 * @title SYNTHOSEarlyAdopterSale
 * @dev Crypto-only SYN sale contract for eligible early adopters.
 *
 * The contract must be pre-funded with SYN. Buyers pay accepted crypto assets
 * to the treasury wallet and receive SYN immediately at 0.05 USD per SYN.
 *
 * Non-stable payment assets require the owner to update the asset USD price.
 * This contract intentionally does not use an external oracle.
 */
contract SYNTHOSEarlyAdopterSale is Ownable, Pausable, ReentrancyGuard {
    using SafeERC20 for IERC20;

    uint256 public constant USD_PRICE_PER_SYN_18 = 5 * 10 ** 16; // $0.05

    struct PaymentAsset {
        bool enabled;
        uint8 decimals;
        uint256 usdPricePerToken18;
    }

    IERC20 public immutable synToken;
    SYNTHOSComplianceRegistry public immutable complianceRegistry;
    address public treasury;
    uint256 public maxSaleAllocation;
    uint256 public totalSynSold;
    uint256 public minSynPurchase;
    uint256 public maxSynPerWallet;
    uint256 public nativeUsdPrice18;
    bool public nativePaymentsEnabled;

    mapping(address => PaymentAsset) public paymentAssets;
    mapping(address => uint256) public purchasedByWallet;

    event TreasuryUpdated(address indexed previousTreasury, address indexed newTreasury);
    event SaleLimitsUpdated(uint256 maxSaleAllocation, uint256 minSynPurchase, uint256 maxSynPerWallet);
    event PaymentAssetUpdated(address indexed asset, bool enabled, uint8 decimals, uint256 usdPricePerToken18);
    event NativePaymentConfigUpdated(bool enabled, uint256 usdPrice18);
    event SynPurchased(
        address indexed buyer,
        address indexed beneficiary,
        address indexed paymentAsset,
        uint256 paymentAmount,
        uint256 synAmount,
        uint256 usdValue18
    );
    event NativeSynPurchased(
        address indexed buyer,
        address indexed beneficiary,
        uint256 nativeAmount,
        uint256 synAmount,
        uint256 usdValue18
    );

    constructor(
        address synTokenAddress,
        address complianceRegistryAddress,
        address treasuryAddress,
        uint256 maxSaleAllocation_,
        uint256 minSynPurchase_,
        uint256 maxSynPerWallet_
    ) {
        require(synTokenAddress != address(0), "invalid SYN");
        require(complianceRegistryAddress != address(0), "invalid compliance");
        require(treasuryAddress != address(0), "invalid treasury");
        require(maxSaleAllocation_ > 0, "allocation required");

        synToken = IERC20(synTokenAddress);
        complianceRegistry = SYNTHOSComplianceRegistry(complianceRegistryAddress);
        treasury = treasuryAddress;
        maxSaleAllocation = maxSaleAllocation_;
        minSynPurchase = minSynPurchase_;
        maxSynPerWallet = maxSynPerWallet_;
    }

    receive() external payable {
        buyWithNative(msg.sender);
    }

    function buyWithToken(
        address paymentAsset,
        uint256 paymentAmount,
        address beneficiary
    ) external nonReentrant whenNotPaused returns (uint256) {
        require(beneficiary != address(0), "invalid beneficiary");
        require(paymentAmount > 0, "payment required");

        PaymentAsset memory asset = paymentAssets[paymentAsset];
        require(asset.enabled, "payment asset disabled");
        require(asset.usdPricePerToken18 > 0, "asset price missing");

        uint256 usdValue18 = (paymentAmount * asset.usdPricePerToken18) / (10 ** asset.decimals);
        uint256 synAmount = _quoteSyn(usdValue18);
        _validatePurchase(beneficiary, synAmount);

        IERC20(paymentAsset).safeTransferFrom(msg.sender, treasury, paymentAmount);
        synToken.safeTransfer(beneficiary, synAmount);

        totalSynSold += synAmount;
        purchasedByWallet[beneficiary] += synAmount;

        emit SynPurchased(msg.sender, beneficiary, paymentAsset, paymentAmount, synAmount, usdValue18);
        return synAmount;
    }

    function buyWithNative(address beneficiary) public payable nonReentrant whenNotPaused returns (uint256) {
        require(nativePaymentsEnabled, "native payments disabled");
        require(nativeUsdPrice18 > 0, "native price missing");
        require(beneficiary != address(0), "invalid beneficiary");
        require(msg.value > 0, "payment required");

        uint256 usdValue18 = (msg.value * nativeUsdPrice18) / 1 ether;
        uint256 synAmount = _quoteSyn(usdValue18);
        _validatePurchase(beneficiary, synAmount);

        totalSynSold += synAmount;
        purchasedByWallet[beneficiary] += synAmount;
        synToken.safeTransfer(beneficiary, synAmount);

        (bool sent, ) = treasury.call{value: msg.value}("");
        require(sent, "native transfer failed");

        emit NativeSynPurchased(msg.sender, beneficiary, msg.value, synAmount, usdValue18);
        return synAmount;
    }

    function quoteTokenPurchase(
        address paymentAsset,
        uint256 paymentAmount
    ) external view returns (uint256 synAmount, uint256 usdValue18) {
        PaymentAsset memory asset = paymentAssets[paymentAsset];
        require(asset.enabled, "payment asset disabled");
        usdValue18 = (paymentAmount * asset.usdPricePerToken18) / (10 ** asset.decimals);
        synAmount = _quoteSyn(usdValue18);
    }

    function quoteNativePurchase(
        uint256 nativeAmount
    ) external view returns (uint256 synAmount, uint256 usdValue18) {
        require(nativePaymentsEnabled, "native payments disabled");
        usdValue18 = (nativeAmount * nativeUsdPrice18) / 1 ether;
        synAmount = _quoteSyn(usdValue18);
    }

    function setTreasury(address newTreasury) external onlyOwner {
        require(newTreasury != address(0), "invalid treasury");
        address previousTreasury = treasury;
        treasury = newTreasury;
        emit TreasuryUpdated(previousTreasury, newTreasury);
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

    function setPaymentAsset(
        address paymentAsset,
        bool enabled,
        uint256 usdPricePerToken18
    ) external onlyOwner {
        require(paymentAsset != address(0), "invalid asset");
        uint8 decimals = IERC20Metadata(paymentAsset).decimals();
        paymentAssets[paymentAsset] = PaymentAsset({
            enabled: enabled,
            decimals: decimals,
            usdPricePerToken18: usdPricePerToken18
        });
        emit PaymentAssetUpdated(paymentAsset, enabled, decimals, usdPricePerToken18);
    }

    function setNativePaymentConfig(bool enabled, uint256 usdPrice18) external onlyOwner {
        nativePaymentsEnabled = enabled;
        nativeUsdPrice18 = usdPrice18;
        emit NativePaymentConfigUpdated(enabled, usdPrice18);
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
