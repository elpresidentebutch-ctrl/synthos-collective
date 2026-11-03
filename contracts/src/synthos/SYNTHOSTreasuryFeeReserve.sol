// SPDX-License-Identifier: Apache-2.0
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/access/Ownable.sol";
import "@openzeppelin/contracts/security/ReentrancyGuard.sol";
import "@openzeppelin/contracts/security/Pausable.sol";
import "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";

/**
 * @title SYNTHOSTreasuryFeeReserve
 * @dev Protocol Treasury and Fee Reserve for SYNTHOS Collective.
 *
 * Architecture:
 * 1. Protocol Fee Ingestion:
 *    Network execution fees and gas route into this Treasury Reserve vault.
 * 2. Deterministic Allocation Accounting:
 *    Deposited fees are deterministically accounted into burn, validator reserve,
 *    and ecosystem growth allocations based on configured basis points.
 * 3. Validator Disbursements:
 *    The contract programmatically funds the Staking Reward Pool up to the accumulated
 *    validator reserve allocation.
 * 4. Native Currency Handling:
 *    Native currency received via receive() is accounted and disbursable via governance/owner.
 */
contract SYNTHOSTreasuryFeeReserve is Ownable, Pausable, ReentrancyGuard {
    using SafeERC20 for IERC20;

    // Tokens & Contracts
    IERC20 public immutable synToken;
    address public stakingContract;
    address public governanceContract;

    // Accounting - SYN Token
    uint256 public totalProtocolFeesCollected;
    uint256 public totalDisbursedToValidators;
    uint256 public totalDisbursedToEcosystem;
    uint256 public totalBurnedFromFees;
    uint256 public validatorReserveBalance;
    uint256 public ecosystemReserveBalance;

    // Accounting - Native Currency
    uint256 public totalNativeFeesCollected;
    uint256 public totalNativeDisbursed;

    // Fee allocation parameters (Basis points: 10000 = 100%)
    uint256 public constant MAX_BPS = 10000;
    uint256 public burnBps = 1000;                // 10% burned
    uint256 public validatorReserveBps = 7000;    // 70% earmarked for validator staking
    uint256 public ecosystemGrowthBps = 2000;     // 20% retained for ecosystem growth

    // Events
    event ProtocolFeeReceived(address indexed from, uint256 amount, uint256 timestamp);
    event NativeFeeReceived(address indexed from, uint256 amount, uint256 timestamp);
    event ValidatorEpochFunded(address indexed stakingContract, uint256 amount, uint256 timestamp);
    event EcosystemDisbursed(address indexed recipient, uint256 amount, uint256 timestamp);
    event NativeFeeDisbursed(address indexed recipient, uint256 amount, uint256 timestamp);
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
        require(msg.value > 0, "Zero native value");
        totalNativeFeesCollected += msg.value;
        emit NativeFeeReceived(msg.sender, msg.value, block.timestamp);
    }

    /**
     * @dev Ingests protocol transaction fees in SYN token and deterministically
     * applies allocation percentages across burn, validator reserve, and ecosystem growth.
     * @param amount The fee amount deposited
     */
    function depositProtocolFees(uint256 amount) external nonReentrant whenNotPaused {
        require(amount > 0, "Amount must be positive");

        synToken.safeTransferFrom(msg.sender, address(this), amount);
        totalProtocolFeesCollected += amount;

        uint256 burnAmount = (amount * burnBps) / MAX_BPS;
        uint256 validatorAmount = (amount * validatorReserveBps) / MAX_BPS;
        uint256 ecosystemAmount = amount - burnAmount - validatorAmount;

        validatorReserveBalance += validatorAmount;
        ecosystemReserveBalance += ecosystemAmount;

        if (burnAmount > 0) {
            address burnAddress = 0x000000000000000000000000000000000000dEaD;
            synToken.safeTransfer(burnAddress, burnAmount);
            totalBurnedFromFees += burnAmount;
            emit FeesBurned(burnAmount, block.timestamp);
        }

        emit ProtocolFeeReceived(msg.sender, amount, block.timestamp);
    }

    /**
     * @dev Disburses SYN tokens from the validator reserve to fund the Validator Staking contract.
     * Enforces that disbursements cannot exceed the accumulated validator reserve allocation.
     * @param amount Amount of SYN to deposit into the Staking Reward Pool
     */
    function fundValidatorEpoch(uint256 amount) external onlyGovernanceOrOwner nonReentrant whenNotPaused {
        require(stakingContract != address(0), "Staking contract not set");
        require(amount > 0, "Amount must be positive");
        require(validatorReserveBalance >= amount, "Insufficient validator reserve balance");

        validatorReserveBalance -= amount;
        totalDisbursedToValidators += amount;
        synToken.safeTransfer(stakingContract, amount);

        emit ValidatorEpochFunded(stakingContract, amount, block.timestamp);
    }

    /**
     * @dev Disburses SYN tokens from the ecosystem growth reserve.
     * Enforces that disbursements cannot exceed the accumulated ecosystem reserve allocation.
     * @param recipient Address receiving the ecosystem disbursement
     * @param amount Amount of SYN to disburse
     */
    function disburseEcosystemGrowth(address recipient, uint256 amount) external onlyGovernanceOrOwner nonReentrant whenNotPaused {
        require(recipient != address(0), "Invalid recipient address");
        require(amount > 0, "Amount must be positive");
        require(ecosystemReserveBalance >= amount, "Insufficient ecosystem reserve balance");

        ecosystemReserveBalance -= amount;
        totalDisbursedToEcosystem += amount;
        synToken.safeTransfer(recipient, amount);

        emit EcosystemDisbursed(recipient, amount, block.timestamp);
    }

    /**
     * @dev Disburses accumulated native currency fees to a designated recipient.
     * @param recipient Address receiving the native funds
     * @param amount Amount of native currency in wei to disburse
     */
    function disburseNativeFees(address payable recipient, uint256 amount) external onlyGovernanceOrOwner nonReentrant whenNotPaused {
        require(recipient != address(0), "Invalid recipient address");
        require(amount > 0, "Amount must be positive");
        require(address(this).balance >= amount, "Insufficient native balance");

        totalNativeDisbursed += amount;
        (bool success, ) = recipient.call{value: amount}("");
        require(success, "Native transfer failed");

        emit NativeFeeDisbursed(recipient, amount, block.timestamp);
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
