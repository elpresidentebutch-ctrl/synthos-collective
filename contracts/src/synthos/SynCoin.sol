// SPDX-License-Identifier: Apache-2.0
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/access/Ownable.sol";
import "@openzeppelin/contracts/token/ERC20/ERC20.sol";
import "@openzeppelin/contracts/token/ERC20/extensions/ERC20Pausable.sol";
import "@openzeppelin/contracts/token/ERC20/extensions/ERC20Snapshot.sol";

/**
 * @title SynCoin
 * @dev A bridge-pegged wrapper for SYN on EVM chains (Base, Ethereum, ...).
 *
 * SYNTHOS is a sovereign native Layer-1 chain. The native chain is the only
 * place SYN is ever really created -- its 100B supply cap is enforced there,
 * once, at genesis, and nowhere else. This contract is NOT a second,
 * independent supply of SYN. It never mints on its own, has no genesis
 * allocation, and no owner-controlled mint path. Every unit that exists here
 * exists only because it was proven, through the SYNTHOSSynBridgeMinter
 * contract, to correspond to one real SYN locked on the native chain. When a
 * holder wants their SYN back on the native chain, this contract's supply
 * shrinks by exactly that much (see bridgeBurn).
 *
 * Concretely:
 *   - mint() can only ever be called by `bridgeMinter`, a single contract
 *     address wired up exactly once via initializeBridgeMinter() and never
 *     changeable again after that -- not by the owner, not by anyone.
 *   - bridgeBurn() can only ever be called by that same `bridgeMinter`
 *     contract, and only burns the balance of whoever is actually calling
 *     into the bridge to send their coins home.
 *   - There is no owner mint function, no pre-mined "undistributed pool"
 *     sitting in this contract for an owner to redirect, and no genesis
 *     token-bucket bookkeeping here at all -- those live in the native
 *     chain's own genesis allocation and treasury governance.
 *
 * Ownership (pause/unpause/treasury-recycling-spend-type management) should
 * still be transferred to a timelock or multisig before any public launch,
 * exactly as before -- but the owner's remaining powers no longer include
 * anything that can create SYN or seize an undistributed balance, because
 * neither of those things exist on this contract anymore.
 */
contract SynCoin is ERC20, ERC20Pausable, ERC20Snapshot, Ownable {
    /// @dev The only address ever allowed to mint or bridge-burn. Set once,
    /// permanently, via initializeBridgeMinter -- never mutable again.
    address public bridgeMinter;
    bool public bridgeMinterInitialized;

    bytes32 public constant SPEND_PROTOCOL = keccak256("PROTOCOL_SPEND");
    bytes32 public constant SPEND_NODE_REGISTRATION = keccak256("NODE_REGISTRATION");
    bytes32 public constant SPEND_SERVICE_FEE = keccak256("SERVICE_FEE");
    bytes32 public constant SPEND_MARKETPLACE = keccak256("MARKETPLACE");

    address public treasury;

    uint256 public totalTreasuryRecyclingBurned;
    uint256 public totalTreasuryRecycled;

    mapping(bytes32 => bool) public approvedTreasuryRecyclingSpendTypes;
    mapping(bytes32 => uint256) public treasuryRecyclingBurnedByType;
    mapping(bytes32 => uint256) public treasuryRecycledByType;

    event BridgeMinterInitialized(address indexed bridgeMinter);
    event BridgeMinted(address indexed recipient, uint256 amount);
    event BridgeBurned(address indexed holder, uint256 amount);

    event TreasuryUpdated(address indexed previousTreasury, address indexed newTreasury);
    event TreasuryRecyclingSpendTypeUpdated(bytes32 indexed spendType, bool approved);

    event TreasuryRecyclingBurn(
        address indexed spender,
        address indexed treasury,
        uint256 amount,
        uint256 burnedAmount,
        uint256 recycledAmount,
        bytes32 indexed spendType
    );

    modifier onlyBridgeMinter() {
        require(bridgeMinterInitialized && msg.sender == bridgeMinter, "not bridge minter");
        _;
    }

    constructor(address initialTreasury) ERC20("SYNTHOS", "SYN") {
        require(initialTreasury != address(0), "invalid treasury");
        treasury = initialTreasury;
        _setTreasuryRecyclingSpendType(SPEND_PROTOCOL, true);
        _setTreasuryRecyclingSpendType(SPEND_NODE_REGISTRATION, true);
        _setTreasuryRecyclingSpendType(SPEND_SERVICE_FEE, true);
        _setTreasuryRecyclingSpendType(SPEND_MARKETPLACE, true);
    }

    /// @dev One-time wiring of the bridge minter contract. Callable only by
    /// the owner, and only once ever -- after this call, `bridgeMinter` is
    /// permanent for the life of the contract. There is deliberately no
    /// "setBridgeMinter" or "updateBridgeMinter" function: once wired, the
    /// mint authority can never be redirected, by the owner or anyone else.
    function initializeBridgeMinter(address minter) external onlyOwner {
        require(!bridgeMinterInitialized, "bridge minter already initialized");
        require(minter != address(0), "invalid bridge minter");
        bridgeMinterInitialized = true;
        bridgeMinter = minter;
        emit BridgeMinterInitialized(minter);
    }

    /// @dev Mints SYN that the bridge minter has verified is backed by a real
    /// coin locked on the native chain. Only the bridge minter can call
    /// this -- see SYNTHOSSynBridgeMinter for the relayer-quorum logic that
    /// gates when this actually gets called.
    function mint(address recipient, uint256 amount) external onlyBridgeMinter {
        require(recipient != address(0), "invalid recipient");
        require(amount > 0, "amount must be positive");
        _mint(recipient, amount);
        emit BridgeMinted(recipient, amount);
    }

    /// @dev Burns `amount` from `holder` when they send SYN back to the
    /// native chain through the bridge minter. Only the bridge minter can
    /// call this, and it is only ever invoked with `holder` set to whoever
    /// actually called into the bridge (see
    /// SYNTHOSSynBridgeMinter.burnToNative), never an arbitrary address --
    /// this contract has no way to know that on its own, so that guarantee
    /// lives in the bridge minter, not here.
    function bridgeBurn(address holder, uint256 amount) external onlyBridgeMinter {
        require(amount > 0, "amount must be positive");
        _burn(holder, amount);
        emit BridgeBurned(holder, amount);
    }

    function setTreasury(address newTreasury) external onlyOwner {
        require(newTreasury != address(0), "invalid treasury");

        address previousTreasury = treasury;
        treasury = newTreasury;

        emit TreasuryUpdated(previousTreasury, newTreasury);
    }

    function setTreasuryRecyclingSpendType(
        bytes32 spendType,
        bool approved
    ) external onlyOwner {
        require(spendType != bytes32(0), "invalid spend type");

        _setTreasuryRecyclingSpendType(spendType, approved);
    }

    /// @dev Lets a holder spend SYN they already have into a recognized
    /// protocol-spend category, burning half and recycling half to
    /// treasury. This never creates SYN -- it only ever moves or destroys
    /// SYN a holder already legitimately holds (whether bridged in or
    /// received on this chain), so it needs no bridge involvement.
    function treasuryRecyclingBurn(
        uint256 amount,
        bytes32 spendType
    ) external {
        require(amount > 1, "amount too small");
        require(treasury != address(0), "treasury not set");
        require(balanceOf(_msgSender()) >= amount, "insufficient balance");
        require(approvedTreasuryRecyclingSpendTypes[spendType], "spend type not approved");

        uint256 burnedAmount = amount / 2;
        uint256 recycledAmount = amount - burnedAmount;

        totalTreasuryRecyclingBurned += burnedAmount;
        totalTreasuryRecycled += recycledAmount;
        treasuryRecyclingBurnedByType[spendType] += burnedAmount;
        treasuryRecycledByType[spendType] += recycledAmount;
        _burn(_msgSender(), burnedAmount);
        _transfer(_msgSender(), treasury, recycledAmount);

        emit TreasuryRecyclingBurn(
            _msgSender(),
            treasury,
            amount,
            burnedAmount,
            recycledAmount,
            spendType
        );
    }

    function _setTreasuryRecyclingSpendType(
        bytes32 spendType,
        bool approved
    ) internal {
        approvedTreasuryRecyclingSpendTypes[spendType] = approved;
        emit TreasuryRecyclingSpendTypeUpdated(spendType, approved);
    }

    function pause() external onlyOwner {
        _pause();
    }

    function unpause() external onlyOwner {
        _unpause();
    }

    function createSnapshot() external onlyOwner returns (uint256) {
        return _snapshot();
    }

    function _beforeTokenTransfer(
        address from,
        address to,
        uint256 amount
    ) internal override(ERC20, ERC20Pausable, ERC20Snapshot) {
        super._beforeTokenTransfer(from, to, amount);
    }
}
