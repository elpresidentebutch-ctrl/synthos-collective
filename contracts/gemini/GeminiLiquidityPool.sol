// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/**
 * @title GeminiLiquidityPool
 * @dev Automated Market Maker (AMM) for Gemini Megachain 2.0
 * 
 * Features:
 * - Constant product formula (x*y=k)
 * - LP token minting and burning
 * - Fee collection (0.25%)
 * - Slippage protection
 * - Multi-token pools
 */
contract GeminiLiquidityPool {
    // Pool structure
    struct Pool {
        address token_a;
        address token_b;
        uint256 reserve_a;
        uint256 reserve_b;
        uint256 total_lp_shares;
        uint256 fee_collected_a;
        uint256 fee_collected_b;
    }

    // Constants
    uint256 public constant FEE_PERCENT = 25; // 0.25% = 25/10000
    uint256 public constant MINIMUM_LIQUIDITY = 1000; // Minimum LP tokens

    // State variables
    mapping(bytes32 => Pool) public pools;
    mapping(bytes32 => mapping(address => uint256)) public lp_balances;

    uint256 public pool_count = 0;

    // Events
    event PoolCreated(
        bytes32 indexed pool_id,
        address indexed token_a,
        address indexed token_b,
        uint256 initial_amount_a,
        uint256 initial_amount_b
    );

    event LiquidityAdded(
        bytes32 indexed pool_id,
        address indexed provider,
        uint256 amount_a,
        uint256 amount_b,
        uint256 lp_shares_minted
    );

    event LiquidityRemoved(
        bytes32 indexed pool_id,
        address indexed provider,
        uint256 amount_a,
        uint256 amount_b,
        uint256 lp_shares_burned
    );

    event Swap(
        bytes32 indexed pool_id,
        address indexed swapper,
        address indexed token_in,
        uint256 amount_in,
        address token_out,
        uint256 amount_out,
        uint256 fee
    );

    event FeeCollected(
        bytes32 indexed pool_id,
        uint256 fee_a,
        uint256 fee_b
    );

    /**
     * @dev Create a new liquidity pool
     * @param token_a First token address
     * @param token_b Second token address
     * @param amount_a Initial amount of token A
     * @param amount_b Initial amount of token B
     * @return pool_id ID of created pool
     */
    function createPool(
        address token_a,
        address token_b,
        uint256 amount_a,
        uint256 amount_b
    ) 
        public 
        returns (bytes32) 
    {
        require(token_a != address(0), "Invalid token A");
        require(token_b != address(0), "Invalid token B");
        require(token_a != token_b, "Tokens must differ");
        require(amount_a > 0 && amount_b > 0, "Amounts must be positive");

        // Create pool ID
        bytes32 pool_id = keccak256(abi.encodePacked(token_a, token_b));

        require(pools[pool_id].token_a == address(0), "Pool exists");

        // Initialize pool
        Pool storage pool = pools[pool_id];
        pool.token_a = token_a;
        pool.token_b = token_b;
        pool.reserve_a = amount_a;
        pool.reserve_b = amount_b;
        pool.total_lp_shares = sqrt(amount_a * amount_b) - MINIMUM_LIQUIDITY;
        pool.fee_collected_a = 0;
        pool.fee_collected_b = 0;

        // Burn minimum liquidity
        lp_balances[pool_id][address(0)] = MINIMUM_LIQUIDITY;

        // Issue LP shares to creator
        lp_balances[pool_id][msg.sender] = pool.total_lp_shares;

        pool_count++;

        emit PoolCreated(
            pool_id,
            token_a,
            token_b,
            amount_a,
            amount_b
        );

        return pool_id;
    }

    /**
     * @dev Add liquidity to existing pool
     * @param token_a First token address
     * @param token_b Second token address
     * @param amount_a Amount of token A to add
     * @param amount_b Amount of token B to add
     * @return lp_shares LP shares minted
     */
    function addLiquidity(
        address token_a,
        address token_b,
        uint256 amount_a,
        uint256 amount_b
    ) 
        public 
        returns (uint256) 
    {
        require(amount_a > 0 && amount_b > 0, "Amounts must be positive");

        bytes32 pool_id = keccak256(abi.encodePacked(token_a, token_b));
        Pool storage pool = pools[pool_id];

        require(pool.token_a != address(0), "Pool doesn't exist");

        // Calculate LP shares to mint
        uint256 lp_shares;
        if (pool.total_lp_shares == 0) {
            lp_shares = sqrt(amount_a * amount_b) - MINIMUM_LIQUIDITY;
        } else {
            uint256 lp_a = (amount_a * pool.total_lp_shares) / pool.reserve_a;
            uint256 lp_b = (amount_b * pool.total_lp_shares) / pool.reserve_b;
            lp_shares = lp_a < lp_b ? lp_a : lp_b;
        }

        require(lp_shares > 0, "Insufficient liquidity minted");

        // Update reserves
        pool.reserve_a += amount_a;
        pool.reserve_b += amount_b;
        pool.total_lp_shares += lp_shares;

        // Mint LP shares
        lp_balances[pool_id][msg.sender] += lp_shares;

        emit LiquidityAdded(pool_id, msg.sender, amount_a, amount_b, lp_shares);

        return lp_shares;
    }

    /**
     * @dev Remove liquidity from pool
     * @param token_a First token address
     * @param token_b Second token address
     * @param lp_shares Amount of LP shares to burn
     * @return amount_a Amount of token A received
     * @return amount_b Amount of token B received
     */
    function removeLiquidity(
        address token_a,
        address token_b,
        uint256 lp_shares
    ) 
        public 
        returns (uint256, uint256) 
    {
        require(lp_shares > 0, "Shares must be positive");

        bytes32 pool_id = keccak256(abi.encodePacked(token_a, token_b));
        Pool storage pool = pools[pool_id];

        require(pool.token_a != address(0), "Pool doesn't exist");
        require(lp_balances[pool_id][msg.sender] >= lp_shares, "Insufficient LP shares");

        // Calculate amounts
        uint256 amount_a = (lp_shares * pool.reserve_a) / pool.total_lp_shares;
        uint256 amount_b = (lp_shares * pool.reserve_b) / pool.total_lp_shares;

        require(amount_a > 0 && amount_b > 0, "Insufficient liquidity");

        // Burn LP shares
        lp_balances[pool_id][msg.sender] -= lp_shares;
        pool.total_lp_shares -= lp_shares;

        // Update reserves
        pool.reserve_a -= amount_a;
        pool.reserve_b -= amount_b;

        emit LiquidityRemoved(pool_id, msg.sender, amount_a, amount_b, lp_shares);

        return (amount_a, amount_b);
    }

    /**
     * @dev Swap tokens in pool
     * @param token_in Token to swap in
     * @param token_out Token to swap out
     * @param amount_in Amount to swap
     * @param min_amount_out Minimum amount out (slippage protection)
     * @return amount_out Amount swapped out
     */
    function swap(
        address token_in,
        address token_out,
        uint256 amount_in,
        uint256 min_amount_out
    ) 
        public 
        returns (uint256) 
    {
        require(amount_in > 0, "Invalid amount");

        bytes32 pool_id = keccak256(abi.encodePacked(token_in, token_out));
        Pool storage pool = pools[pool_id];

        require(pool.token_a != address(0), "Pool doesn't exist");

        // Determine which token is input
        bool is_token_a_in = (pool.token_a == token_in);
        require(
            (is_token_a_in && pool.token_b == token_out) ||
            (!is_token_a_in && pool.token_a == token_out),
            "Invalid token pair"
        );

        // Calculate fee
        uint256 fee = (amount_in * FEE_PERCENT) / 10000;
        uint256 amount_in_with_fee = amount_in - fee;

        // Calculate output using constant product formula (x*y=k)
        uint256 amount_out;
        if (is_token_a_in) {
            uint256 k = pool.reserve_a * pool.reserve_b;
            uint256 new_reserve_a = pool.reserve_a + amount_in_with_fee;
            uint256 new_reserve_b = k / new_reserve_a;
            amount_out = pool.reserve_b - new_reserve_b;

            // Update reserves
            pool.reserve_a = new_reserve_a;
            pool.reserve_b = new_reserve_b;
            pool.fee_collected_a += fee;
        } else {
            uint256 k = pool.reserve_a * pool.reserve_b;
            uint256 new_reserve_b = pool.reserve_b + amount_in_with_fee;
            uint256 new_reserve_a = k / new_reserve_b;
            amount_out = pool.reserve_a - new_reserve_a;

            // Update reserves
            pool.reserve_a = new_reserve_a;
            pool.reserve_b = new_reserve_b;
            pool.fee_collected_b += fee;
        }

        require(amount_out >= min_amount_out, "Slippage exceeded");
        require(amount_out > 0, "Insufficient output");

        emit Swap(
            pool_id,
            msg.sender,
            token_in,
            amount_in,
            token_out,
            amount_out,
            fee
        );

        return amount_out;
    }

    /**
     * @dev Get pool details
     * @param token_a First token
     * @param token_b Second token
     */
    function getPoolDetails(address token_a, address token_b) 
        public 
        view 
        returns (
            uint256 reserve_a,
            uint256 reserve_b,
            uint256 total_lp_shares,
            uint256 fee_a,
            uint256 fee_b
        ) 
    {
        bytes32 pool_id = keccak256(abi.encodePacked(token_a, token_b));
        Pool storage pool = pools[pool_id];

        return (
            pool.reserve_a,
            pool.reserve_b,
            pool.total_lp_shares,
            pool.fee_collected_a,
            pool.fee_collected_b
        );
    }

    /**
     * @dev Calculate price of token
     * @param token_in Input token
     * @param token_out Output token
     * @param amount_in Amount of input token
     * @return price Price (amount out per amount in)
     */
    function getPrice(
        address token_in,
        address token_out,
        uint256 amount_in
    ) 
        public 
        view 
        returns (uint256) 
    {
        bytes32 pool_id = keccak256(abi.encodePacked(token_in, token_out));
        Pool storage pool = pools[pool_id];

        require(pool.token_a != address(0), "Pool doesn't exist");

        bool is_token_a_in = (pool.token_a == token_in);

        // Calculate with fee
        uint256 fee = (amount_in * FEE_PERCENT) / 10000;
        uint256 amount_in_with_fee = amount_in - fee;

        if (is_token_a_in) {
            uint256 k = pool.reserve_a * pool.reserve_b;
            uint256 new_reserve_a = pool.reserve_a + amount_in_with_fee;
            uint256 new_reserve_b = k / new_reserve_a;
            return pool.reserve_b - new_reserve_b;
        } else {
            uint256 k = pool.reserve_a * pool.reserve_b;
            uint256 new_reserve_b = pool.reserve_b + amount_in_with_fee;
            uint256 new_reserve_a = k / new_reserve_b;
            return pool.reserve_a - new_reserve_a;
        }
    }

    /**
     * @dev Collect accumulated fees
     * @param token_a First token
     * @param token_b Second token
     */
    function collectFees(address token_a, address token_b) public {
        bytes32 pool_id = keccak256(abi.encodePacked(token_a, token_b));
        Pool storage pool = pools[pool_id];

        require(pool.token_a != address(0), "Pool doesn't exist");

        uint256 fee_a = pool.fee_collected_a;
        uint256 fee_b = pool.fee_collected_b;

        pool.fee_collected_a = 0;
        pool.fee_collected_b = 0;

        emit FeeCollected(pool_id, fee_a, fee_b);
    }

    /**
     * @dev Helper function to calculate square root
     * @param y Value to calculate root of
     * @return z Square root
     */
    function sqrt(uint256 y) internal pure returns (uint256 z) {
        if (y > 3) {
            z = y;
            uint256 x = y / 2 + 1;
            while (x < z) {
                z = x;
                x = (y / x + x) / 2;
            }
        } else if (y != 0) {
            z = 1;
        }
    }
}
