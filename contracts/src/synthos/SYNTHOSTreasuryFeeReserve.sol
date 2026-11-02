// SPDX-License-Identifier: Apache-2.0
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/access/Ownable.sol";
import "@openzeppelin/contracts/security/ReentrancyGuard.sol";
import "@openzeppelin/contracts/security/Pausable.sol";
import "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";

/**
 * @title SYNTHOSTreasuryFeeReserve
 * @dev Autonomous Treasury and Fee Reserve for SYNTHOS Collective Layer 1.
 *
 * Core Architecture & Economic Principles:
 * 1. Treasury-Centric Fee Model:
 *    All protocol transaction fees, execution gas, and network revenues route directly
 *    into this Treasury Reserve vault. Fees are NEVER split directly to individual block proposers.
 *
 * 2. Backing & Intrinsic Value:
 *    Retained fee reserves accumulate in the Treasury, building cryptographic balance-sheet
 *    backing and intrinsic reserve value for the native SYN ecosystem.
 *
 * 3. Programmatic Validator Disbursements:
 *    The Treasury deterministically funds the Staking Reward Pool (SYNTHOSStaking).
 *    Validators are compensated as independent infrastructure service providers
 *    based on verified Proof-of-Uptime, signature aggregation, and Byzantine consensus duties.
 *
 * 4. Regulatory & Legal Integrity:
 *    Prevents passive fee-skimming claims (Howey Test compliance) by establishing that
 *    validators receive standardized protocol service disbursements for hardware availability,
 *    not commercial fee shares or equity-like dividends.
 */
contract SYNTHOSTreasuryFeeReserve is Ownable, Pausable, ReentrancyGuard {
    using SafeERC20 for IERC20;

    // Tokens & Contracts
    IERC20 public immutable synToken;
    address public stakingContract;
    address public governanceContract;

    // Accounting
    uint256 public totalProtocolFeesCollected;
    uint256 public totalDisbursedToValidators;
    uint256 public totalBurnedFromFees;
    
    // Fee allocation parameters (Basis points: 10000 = 100%)
    uint256 public constant MAX_BPS = 10000;
    uint256 public burnBps = 1000;                // 10% burned to increase scarcity
    uint256 public validatorReserveBps = 7000;    // 70% earmarked for validator uptime & consensus disbursements
    uint256 public ecosystemGrowthBps = 2000;     // 20% retained as permanent treasury liquidity / backing

    // Events
    event ProtocolFeeReceived(address indexed from, uint256 amount, uint256 timestamp);
    event ValidatorEpochFunded(address indexed stakingContract, uint256 amount, uint256 timestamp);
    event FeesBurned(uint256 amount, uint256 timestamp);
    event AllocationUpdated(uint256 burnBps, uint256 validatorReserveBps, uint256 ecosystemGrowthBps);
    event StakingContractUpdated(address indexed newStakingContract);
    event GovernanceContractUpdated(address indexed newGovernanceContract);

    modifier onlyGovernanceOrOwner() {
        require(
            msg.sender == owner() || (governanceContract != address(0) && msg.sender == governanceContract),
            "Caller is not authorized"
        );
        _;
    }

    constructor(address _synToken, address _owner) {
        require(_synToken != address(0), "Invalid token address");
        require(_owner != address(0), "Invalid owner address");
        synToken = IERC20(_synToken);
        _transferOwnership(_owner);
    }

    /**
     * @dev Fallback to receive native network gas fees
     */
    receive() external payable {
        emit ProtocolFeeReceived(msg.sender, msg.value, block.timestamp);
    }

    /**
     * @dev Ingests protocol transaction fees in SYN token directly from blocks / execution engine
     * @param amount The fee amount deposited
     */
    function depositProtocolFees(uint256 amount) external nonReentrant whenNotPaused {
        require(amount > 0, "Amount must be positive");
        
        synToken.safeTransferFrom(msg.sender, address(this), amount);
        totalProtocolFeesCollected += amount;

        // Apply automatic burn portion if configured
        if (burnBps > 0) {
            uint256 burnAmount = (amount * burnBps) / MAX_BPS;
            if (burnAmount > 0) {
                // Transfer to dead/burn address
                address burnAddress = 0x000000000000000000000000000000000000dEaD;
                synToken.safeTransfer(burnAddress, burnAmount);
                totalBurnedFromFees += burnAmount;
                emit FeesBurned(burnAmount, block.timestamp);
            }
        }

        emit ProtocolFeeReceived(msg.sender, amount, block.timestamp);
    }

    /**
     * @dev Programmatically disburses SYN tokens to fund the Validator Staking contract
     * for active epoch payouts. Only callable by Governance or Authorized Keeper.
     * @param amount Amount of SYN to deposit into the Staking Reward Pool
     */
    function fundValidatorEpoch(uint256 amount) external onlyGovernanceOrOwner nonReentrant whenNotPaused {
        require(stakingContract != address(0), "Staking contract not set");
        require(amount > 0, "Amount must be positive");
        
        uint256 availableBalance = synToken.balanceOf(address(this));
        require(availableBalance >= amount, "Insufficient treasury reserve balance");

        totalDisbursedToValidators += amount;
        synToken.safeTransfer(stakingContract, amount);

        emit ValidatorEpochFunded(stakingContract, amount, block.timestamp);
    }

    /**
     * @dev Configure Staking Contract recipient
     */
    function setStakingContract(address _stakingContract) external onlyOwner {
        require(_stakingContract != address(0), "Invalid staking address");
        stakingContract = _stakingContract;
        emit StakingContractUpdated(_stakingContract);
    }

    /**
     * @dev Configure Governance Contract
     */
    function setGovernanceContract(address _governanceContract) external onlyOwner {
        governanceContract = _governanceContract;
        emit GovernanceContractUpdated(_governanceContract);
    }

    /**
     * @dev Adjust allocation parameters
     */
    function setAllocation(uint256 _burnBps, uint256 _validatorReserveBps, uint256 _ecosystemGrowthBps) external onlyOwner {
        require(_burnBps + _validatorReserveBps + _ecosystemGrowthBps == MAX_BPS, "Must total 10000 BPS");
        burnBps = _burnBps;
        validatorReserveBps = _validatorReserveBps;
        ecosystemGrowthBps = _ecosystemGrowthBps;
        emit AllocationUpdated(_burnBps, _validatorReserveBps, _ecosystemGrowthBps);
    }

    /**
     * @dev View function for total treasury reserves
     */
    function getReserveBalance() external view returns (uint256 synBalance, uint256 nativeBalance) {
        return (synToken.balanceOf(address(this)), address(this).balance);
    }

    function pause() external onlyOwner {
        _pause();
    }

    function unpause() external onlyOwner {
        _unpause();
    }
}
