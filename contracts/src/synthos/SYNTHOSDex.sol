// SPDX-License-Identifier: Apache-2.0
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/access/Ownable.sol";
import "@openzeppelin/contracts/security/Pausable.sol";
import "@openzeppelin/contracts/security/ReentrancyGuard.sol";
import "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";

/**
 * @title SYNTHOSDex
 * @dev Constant-product AMM for SYN paired with approved ERC-20 assets.
 *
 * This is a real deployable DEX primitive, not a UI simulation. It does not
 * mint tokens and it does not custody anything except liquidity explicitly
 * transferred into pools by liquidity providers.
 */
contract SYNTHOSDex is Ownable, Pausable, ReentrancyGuard {
    using SafeERC20 for IERC20;

    uint256 public constant FEE_BPS = 30;
    uint256 public constant BPS_DENOMINATOR = 10_000;

    IERC20 public immutable synToken;

    struct Pool {
        address asset;
        uint256 synReserve;
        uint256 assetReserve;
        uint256 totalShares;
        bool active;
    }

    mapping(address => Pool) public pools;
    mapping(address => mapping(address => uint256)) public liquidityShares;
    address[] public poolAssets;

    event PoolCreated(address indexed asset);
    event LiquidityAdded(
        address indexed provider,
        address indexed asset,
        uint256 synAmount,
        uint256 assetAmount,
        uint256 shares
    );
    event LiquidityRemoved(
        address indexed provider,
        address indexed asset,
        uint256 synAmount,
        uint256 assetAmount,
        uint256 shares
    );
    event Swap(
        address indexed trader,
        address indexed asset,
        bool synForAsset,
        uint256 amountIn,
        uint256 amountOut
    );

    constructor(address synTokenAddress) {
        require(synTokenAddress != address(0), "invalid SYN token");
        synToken = IERC20(synTokenAddress);
    }

    function createPool(address asset) external onlyOwner {
        require(asset != address(0), "invalid asset");
        require(asset != address(synToken), "asset is SYN");
        require(!pools[asset].active, "pool exists");

        pools[asset] = Pool({
            asset: asset,
            synReserve: 0,
            assetReserve: 0,
            totalShares: 0,
            active: true
        });
        poolAssets.push(asset);

        emit PoolCreated(asset);
    }

    function addLiquidity(
        address asset,
        uint256 synAmount,
        uint256 assetAmount
    ) external nonReentrant whenNotPaused returns (uint256 shares) {
        Pool storage pool = pools[asset];
        require(pool.active, "pool not active");
        require(synAmount > 0 && assetAmount > 0, "invalid liquidity");

        if (pool.totalShares == 0) {
            shares = sqrt(synAmount * assetAmount);
        } else {
            uint256 synShares = (synAmount * pool.totalShares) / pool.synReserve;
            uint256 assetShares = (assetAmount * pool.totalShares) / pool.assetReserve;
            shares = synShares < assetShares ? synShares : assetShares;
        }
        require(shares > 0, "zero shares");

        synToken.safeTransferFrom(msg.sender, address(this), synAmount);
        IERC20(asset).safeTransferFrom(msg.sender, address(this), assetAmount);

        pool.synReserve += synAmount;
        pool.assetReserve += assetAmount;
        pool.totalShares += shares;
        liquidityShares[asset][msg.sender] += shares;

        emit LiquidityAdded(msg.sender, asset, synAmount, assetAmount, shares);
    }

    function removeLiquidity(
        address asset,
        uint256 shares
    ) external nonReentrant whenNotPaused returns (uint256 synAmount, uint256 assetAmount) {
        Pool storage pool = pools[asset];
        require(pool.active, "pool not active");
        require(shares > 0, "invalid shares");
        require(liquidityShares[asset][msg.sender] >= shares, "insufficient shares");

        synAmount = (shares * pool.synReserve) / pool.totalShares;
        assetAmount = (shares * pool.assetReserve) / pool.totalShares;
        require(synAmount > 0 && assetAmount > 0, "zero output");

        liquidityShares[asset][msg.sender] -= shares;
        pool.totalShares -= shares;
        pool.synReserve -= synAmount;
        pool.assetReserve -= assetAmount;

        synToken.safeTransfer(msg.sender, synAmount);
        IERC20(asset).safeTransfer(msg.sender, assetAmount);

        emit LiquidityRemoved(msg.sender, asset, synAmount, assetAmount, shares);
    }

    function swapExactSynForAsset(
        address asset,
        uint256 synAmountIn,
        uint256 minAssetOut
    ) external nonReentrant whenNotPaused returns (uint256 assetOut) {
        Pool storage pool = pools[asset];
        require(pool.active, "pool not active");

        assetOut = quoteOut(synAmountIn, pool.synReserve, pool.assetReserve);
        require(assetOut >= minAssetOut, "slippage");

        synToken.safeTransferFrom(msg.sender, address(this), synAmountIn);
        IERC20(asset).safeTransfer(msg.sender, assetOut);

        pool.synReserve += synAmountIn;
        pool.assetReserve -= assetOut;

        emit Swap(msg.sender, asset, true, synAmountIn, assetOut);
    }

    function swapExactAssetForSyn(
        address asset,
        uint256 assetAmountIn,
        uint256 minSynOut
    ) external nonReentrant whenNotPaused returns (uint256 synOut) {
        Pool storage pool = pools[asset];
        require(pool.active, "pool not active");

        synOut = quoteOut(assetAmountIn, pool.assetReserve, pool.synReserve);
        require(synOut >= minSynOut, "slippage");

        IERC20(asset).safeTransferFrom(msg.sender, address(this), assetAmountIn);
        synToken.safeTransfer(msg.sender, synOut);

        pool.assetReserve += assetAmountIn;
        pool.synReserve -= synOut;

        emit Swap(msg.sender, asset, false, assetAmountIn, synOut);
    }

    function quoteSynForAsset(address asset, uint256 synAmountIn) external view returns (uint256) {
        Pool storage pool = pools[asset];
        require(pool.active, "pool not active");
        return quoteOut(synAmountIn, pool.synReserve, pool.assetReserve);
    }

    function quoteAssetForSyn(address asset, uint256 assetAmountIn) external view returns (uint256) {
        Pool storage pool = pools[asset];
        require(pool.active, "pool not active");
        return quoteOut(assetAmountIn, pool.assetReserve, pool.synReserve);
    }

    function quoteOut(
        uint256 amountIn,
        uint256 reserveIn,
        uint256 reserveOut
    ) public pure returns (uint256) {
        require(amountIn > 0, "insufficient input");
        require(reserveIn > 0 && reserveOut > 0, "insufficient liquidity");

        uint256 amountInWithFee = amountIn * (BPS_DENOMINATOR - FEE_BPS);
        return (amountInWithFee * reserveOut) / ((reserveIn * BPS_DENOMINATOR) + amountInWithFee);
    }

    function poolCount() external view returns (uint256) {
        return poolAssets.length;
    }

    function pause() external onlyOwner {
        _pause();
    }

    function unpause() external onlyOwner {
        _unpause();
    }

    function sqrt(uint256 x) internal pure returns (uint256 y) {
        if (x == 0) return 0;
        uint256 z = (x + 1) / 2;
        y = x;
        while (z < y) {
            y = z;
            z = (x / z + z) / 2;
        }
    }
}
