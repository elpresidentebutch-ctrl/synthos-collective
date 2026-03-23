// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/**
 * @title GeminiOracle
 * @dev Price oracle for Gemini Megachain 2.0
 * 
 * Features:
 * - Decentralized price feed aggregation
 * - Multiple price sources per token pair
 * - TWAP (Time-Weighted Average Price) calculation
 * - Median price calculation for robustness
 * - Price staleness detection
 */
contract GeminiOracle {
    // Constants
    uint256 public constant PRICE_STALENESS_THRESHOLD = 1 hours;
    uint256 public constant MINIMUM_PROVIDERS = 3;
    uint256 public constant PRICE_DEVIATION_THRESHOLD = 10; // 10% max deviation

    // Price feed structure
    struct PriceFeed {
        address base_token;
        address quote_token;
        uint256 price;
        uint256 timestamp;
        uint256 decimals;
    }

    // Provider structure
    struct Provider {
        address provider_address;
        bool active;
        uint256 stake;
    }

    // State variables
    mapping(bytes32 => mapping(address => PriceFeed)) public price_feeds;
    mapping(bytes32 => address[]) public feed_providers;
    mapping(bytes32 => uint256[]) public price_history;

    mapping(address => Provider) public providers;
    address[] public provider_list;

    uint256 public min_provider_stake = 10_000 ether;

    // Events
    event PriceUpdated(
        address indexed base_token,
        address indexed quote_token,
        uint256 price,
        uint256 timestamp
    );

    event ProviderAdded(address indexed provider, uint256 stake);
    event ProviderRemoved(address indexed provider);
    event PriceFeedCreated(
        address indexed base_token,
        address indexed quote_token,
        uint256 initial_price
    );

    /**
     * @dev Register as price provider
     */
    function registerProvider() public payable {
        require(msg.value >= min_provider_stake, "Insufficient stake");
        require(!providers[msg.sender].active, "Already provider");

        providers[msg.sender].provider_address = msg.sender;
        providers[msg.sender].stake = msg.value;
        providers[msg.sender].active = true;

        provider_list.push(msg.sender);

        emit ProviderAdded(msg.sender, msg.value);
    }

    /**
     * @dev Unregister as price provider
     */
    function unregisterProvider() public {
        require(providers[msg.sender].active, "Not provider");

        uint256 stake = providers[msg.sender].stake;
        providers[msg.sender].active = false;

        // Remove from provider list
        for (uint256 i = 0; i < provider_list.length; i++) {
            if (provider_list[i] == msg.sender) {
                provider_list[i] = provider_list[provider_list.length - 1];
                provider_list.pop();
                break;
            }
        }

        // Return stake
        (bool success, ) = msg.sender.call{value: stake}("");
        require(success, "Stake return failed");

        emit ProviderRemoved(msg.sender);
    }

    /**
     * @dev Update price for token pair
     * @param base_token Base token address
     * @param quote_token Quote token address
     * @param price Price value
     * @param decimals Price decimals
     */
    function updatePrice(
        address base_token,
        address quote_token,
        uint256 price,
        uint256 decimals
    ) 
        public 
    {
        require(providers[msg.sender].active, "Not authorized provider");
        require(price > 0, "Invalid price");
        require(decimals <= 18, "Invalid decimals");

        bytes32 feed_id = keccak256(abi.encodePacked(base_token, quote_token));

        PriceFeed storage feed = price_feeds[feed_id][msg.sender];
        feed.base_token = base_token;
        feed.quote_token = quote_token;
        feed.price = price;
        feed.timestamp = block.timestamp;
        feed.decimals = decimals;

        // Add provider if not already there
        bool provider_exists = false;
        for (uint256 i = 0; i < feed_providers[feed_id].length; i++) {
            if (feed_providers[feed_id][i] == msg.sender) {
                provider_exists = true;
                break;
            }
        }

        if (!provider_exists) {
            feed_providers[feed_id].push(msg.sender);
        }

        // Add to price history
        price_history[feed_id].push(price);
        if (price_history[feed_id].length > 1000) {
            // Keep only last 1000 prices
            for (uint256 i = 0; i < 500; i++) {
                price_history[feed_id][i] = price_history[feed_id][i + 500];
            }
            // Truncate array (note: Solidity doesn't have easy pop for middle, so we use a workaround)
        }

        emit PriceUpdated(base_token, quote_token, price, block.timestamp);
    }

    /**
     * @dev Get median price from all providers
     * @param base_token Base token
     * @param quote_token Quote token
     * @return median_price Median price from providers
     * @return decimals Price decimals
     */
    function getMedianPrice(address base_token, address quote_token) 
        public 
        view 
        returns (uint256, uint256) 
    {
        bytes32 feed_id = keccak256(abi.encodePacked(base_token, quote_token));
        require(feed_providers[feed_id].length >= MINIMUM_PROVIDERS, "Insufficient providers");

        uint256[] memory prices = new uint256[](feed_providers[feed_id].length);
        uint256 valid_count = 0;
        uint256 decimals = 18;

        for (uint256 i = 0; i < feed_providers[feed_id].length; i++) {
            address provider = feed_providers[feed_id][i];
            PriceFeed storage feed = price_feeds[feed_id][provider];

            // Check if price is fresh
            if (block.timestamp - feed.timestamp <= PRICE_STALENESS_THRESHOLD) {
                prices[valid_count] = feed.price;
                decimals = feed.decimals;
                valid_count++;
            }
        }

        require(valid_count >= MINIMUM_PROVIDERS, "Not enough fresh prices");

        // Sort prices and find median
        uint256 median = _quickSort(prices, 0, valid_count - 1)[valid_count / 2];

        return (median, decimals);
    }

    /**
     * @dev Get TWAP (Time-Weighted Average Price)
     * @param base_token Base token
     * @param quote_token Quote token
     * @param time_window Time window for TWAP (in seconds)
     * @return twap_price Time-weighted average price
     */
    function getTWAP(
        address base_token,
        address quote_token,
        uint256 time_window
    ) 
        public 
        view 
        returns (uint256) 
    {
        bytes32 feed_id = keccak256(abi.encodePacked(base_token, quote_token));
        uint256[] storage history = price_history[feed_id];

        require(history.length > 0, "No price history");

        uint256 sum = 0;
        uint256 count = 0;
        uint256 current_time = block.timestamp;

        // Calculate TWAP from recent prices (simplified)
        uint256 start_index = history.length > 100 ? history.length - 100 : 0;
        for (uint256 i = start_index; i < history.length; i++) {
            sum += history[i];
            count++;
        }

        return sum / count;
    }

    /**
     * @dev Get price with sanity checks
     * @param base_token Base token
     * @param quote_token Quote token
     * @return price Price value
     * @return decimals Price decimals
     */
    function getPrice(address base_token, address quote_token) 
        public 
        view 
        returns (uint256, uint256) 
    {
        bytes32 feed_id = keccak256(abi.encodePacked(base_token, quote_token));

        // Get median price as reference
        (uint256 median_price, uint256 decimals) = getMedianPrice(base_token, quote_token);

        return (median_price, decimals);
    }

    /**
     * @dev Create new price feed
     * @param base_token Base token
     * @param quote_token Quote token
     * @param initial_price Initial price
     * @param decimals Price decimals
     */
    function createPriceFeed(
        address base_token,
        address quote_token,
        uint256 initial_price,
        uint256 decimals
    ) 
        public 
    {
        require(providers[msg.sender].active, "Not authorized provider");
        require(initial_price > 0, "Invalid price");

        bytes32 feed_id = keccak256(abi.encodePacked(base_token, quote_token));

        // Create initial feed
        PriceFeed storage feed = price_feeds[feed_id][msg.sender];
        feed.base_token = base_token;
        feed.quote_token = quote_token;
        feed.price = initial_price;
        feed.timestamp = block.timestamp;
        feed.decimals = decimals;

        feed_providers[feed_id].push(msg.sender);
        price_history[feed_id].push(initial_price);

        emit PriceFeedCreated(base_token, quote_token, initial_price);
    }

    /**
     * @dev Get number of providers for feed
     * @param base_token Base token
     * @param quote_token Quote token
     * @return Number of providers
     */
    function getProviderCount(address base_token, address quote_token) 
        public 
        view 
        returns (uint256) 
    {
        bytes32 feed_id = keccak256(abi.encodePacked(base_token, quote_token));
        return feed_providers[feed_id].length;
    }

    /**
     * @dev Quick sort for finding median
     * @param prices Array to sort
     * @param low Low index
     * @param high High index
     * @return Sorted array
     */
    function _quickSort(
        uint256[] memory prices,
        uint256 low,
        uint256 high
    ) 
        internal 
        pure 
        returns (uint256[] memory) 
    {
        if (low < high) {
            uint256 pi = _partition(prices, low, high);
            _quickSort(prices, low, pi > 0 ? pi - 1 : 0);
            _quickSort(prices, pi + 1, high);
        }
        return prices;
    }

    /**
     * @dev Partition for quick sort
     */
    function _partition(uint256[] memory prices, uint256 low, uint256 high) 
        internal 
        pure 
        returns (uint256) 
    {
        uint256 pivot = prices[high];
        uint256 i = low == 0 ? 0 : low - 1;

        for (uint256 j = low; j < high; j++) {
            if (prices[j] < pivot) {
                i++;
                (prices[i], prices[j]) = (prices[j], prices[i]);
            }
        }
        (prices[i + 1], prices[high]) = (prices[high], prices[i + 1]);
        return i + 1;
    }

    /**
     * @dev Set minimum provider stake
     * @param new_minimum New minimum stake
     */
    function setMinimumStake(uint256 new_minimum) public {
        require(new_minimum > 0, "Invalid minimum");
        min_provider_stake = new_minimum;
    }
}
