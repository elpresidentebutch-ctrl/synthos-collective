const { expect } = require("chai");
const { ethers } = require("hardhat");
const { time } = require("@nomicfoundation/hardhat-network-helpers");

describe("SYNTHOSTreasuryFeeReserve", function () {
  let synToken;
  let treasuryReserve;
  let owner;
  let governance;
  let stakingContract;
  let feePayer;
  let ecosystemRecipient;
  let unauthorized;

  const INITIAL_SUPPLY = ethers.parseUnits("1000000000", 18); // 1B SYN
  const FEE_AMOUNT = ethers.parseUnits("1000", 18);            // 1,000 SYN
  const BURN_ADDRESS = "0x000000000000000000000000000000000000dEaD";

  beforeEach(async function () {
    [owner, governance, stakingContract, feePayer, ecosystemRecipient, unauthorized] = await ethers.getSigners();

    // Deploy MockERC20 as native SYN token
    const MockERC20 = await ethers.getContractFactory("MockERC20");
    synToken = await MockERC20.deploy("SYNTHOS", "SYN", owner.address, INITIAL_SUPPLY);
    await synToken.waitForDeployment();

    // Deploy SYNTHOSTreasuryFeeReserve
    const TreasuryFactory = await ethers.getContractFactory("SYNTHOSTreasuryFeeReserve");
    treasuryReserve = await TreasuryFactory.deploy(await synToken.getAddress(), owner.address);
    await treasuryReserve.waitForDeployment();

    // Provide feePayer with tokens and approve treasury
    await synToken.transfer(feePayer.address, ethers.parseUnits("50000", 18));
    await synToken.connect(feePayer).approve(await treasuryReserve.getAddress(), ethers.MaxUint256);
  });

  describe("Deployment & Initial Configuration", function () {
    it("initializes token and owner correctly", async function () {
      expect(await treasuryReserve.synToken()).to.equal(await synToken.getAddress());
      expect(await treasuryReserve.owner()).to.equal(owner.address);
      expect(await treasuryReserve.burnBps()).to.equal(1000n); // 10%
      expect(await treasuryReserve.validatorReserveBps()).to.equal(7000n); // 70%
      expect(await treasuryReserve.ecosystemGrowthBps()).to.equal(2000n); // 20%
      expect(await treasuryReserve.totalProtocolFeesCollected()).to.equal(0n);
      expect(await treasuryReserve.totalDisbursedToValidators()).to.equal(0n);
      expect(await treasuryReserve.totalDisbursedToEcosystem()).to.equal(0n);
      expect(await treasuryReserve.totalBurnedFromFees()).to.equal(0n);
      expect(await treasuryReserve.validatorReserveBalance()).to.equal(0n);
      expect(await treasuryReserve.ecosystemReserveBalance()).to.equal(0n);
    });

    it("reverts deployment if token or owner is zero address", async function () {
      const TreasuryFactory = await ethers.getContractFactory("SYNTHOSTreasuryFeeReserve");
      await expect(
        TreasuryFactory.deploy(ethers.ZeroAddress, owner.address)
      ).to.be.revertedWith("Invalid token address");

      await expect(
        TreasuryFactory.deploy(await synToken.getAddress(), ethers.ZeroAddress)
      ).to.be.revertedWith("Invalid owner address");
    });
  });

  describe("Native Gas Fee Fallback & Disbursement", function () {
    it("accepts native ETH via receive() and records accounting", async function () {
      const nativeAmount = ethers.parseEther("2.5");
      await expect(
        feePayer.sendTransaction({
          to: await treasuryReserve.getAddress(),
          value: nativeAmount,
        })
      ).to.emit(treasuryReserve, "NativeFeeReceived")
        .withArgs(feePayer.address, nativeAmount, (val) => val > 0n);

      expect(await treasuryReserve.totalNativeFeesCollected()).to.equal(nativeAmount);

      const [synBal, nativeBal] = await treasuryReserve.getReserveBalance();
      expect(nativeBal).to.equal(nativeAmount);
      expect(synBal).to.equal(0n);
    });

    it("disburses native currency fees via disburseNativeFees", async function () {
      const nativeAmount = ethers.parseEther("3.0");
      await feePayer.sendTransaction({
        to: await treasuryReserve.getAddress(),
        value: nativeAmount,
      });

      const disburseAmount = ethers.parseEther("1.0");
      const preRecipientBal = await ethers.provider.getBalance(ecosystemRecipient.address);

      await expect(
        treasuryReserve.connect(owner).disburseNativeFees(ecosystemRecipient.address, disburseAmount)
      ).to.emit(treasuryReserve, "NativeFeeDisbursed")
        .withArgs(ecosystemRecipient.address, disburseAmount, (val) => val > 0n);

      expect(await treasuryReserve.totalNativeDisbursed()).to.equal(disburseAmount);
      const postRecipientBal = await ethers.provider.getBalance(ecosystemRecipient.address);
      expect(postRecipientBal - preRecipientBal).to.equal(disburseAmount);
    });

    it("reverts disburseNativeFees if caller unauthorized or amount exceeds balance", async function () {
      await expect(
        treasuryReserve.connect(unauthorized).disburseNativeFees(ecosystemRecipient.address, ethers.parseEther("1"))
      ).to.be.revertedWith("Caller is not authorized");

      await expect(
        treasuryReserve.connect(owner).disburseNativeFees(ecosystemRecipient.address, ethers.parseEther("100"))
      ).to.be.revertedWith("Insufficient native balance");
    });
  });

  describe("depositProtocolFees & Deterministic Bucket Accounting", function () {
    it("deposits SYN fees and splits into burn, validator reserve, and ecosystem reserve", async function () {
      // 1,000 SYN:
      // 10% burn = 100 SYN
      // 70% validator reserve = 700 SYN
      // 20% ecosystem growth = 200 SYN
      const expectedBurn = (FEE_AMOUNT * 1000n) / 10000n;
      const expectedValidator = (FEE_AMOUNT * 7000n) / 10000n;
      const expectedEcosystem = FEE_AMOUNT - expectedBurn - expectedValidator;

      await expect(treasuryReserve.connect(feePayer).depositProtocolFees(FEE_AMOUNT))
        .to.emit(treasuryReserve, "FeesBurned")
        .withArgs(expectedBurn, (val) => val > 0n)
        .and.to.emit(treasuryReserve, "ProtocolFeeReceived")
        .withArgs(feePayer.address, FEE_AMOUNT, (val) => val > 0n);

      expect(await treasuryReserve.totalProtocolFeesCollected()).to.equal(FEE_AMOUNT);
      expect(await treasuryReserve.totalBurnedFromFees()).to.equal(expectedBurn);
      expect(await treasuryReserve.validatorReserveBalance()).to.equal(expectedValidator);
      expect(await treasuryReserve.ecosystemReserveBalance()).to.equal(expectedEcosystem);

      expect(await synToken.balanceOf(BURN_ADDRESS)).to.equal(expectedBurn);
      expect(await synToken.balanceOf(await treasuryReserve.getAddress())).to.equal(expectedValidator + expectedEcosystem);
    });

    it("handles 0% burn configuration properly", async function () {
      await treasuryReserve.connect(owner).setAllocation(0, 8000, 2000);
      await treasuryReserve.connect(feePayer).depositProtocolFees(FEE_AMOUNT);

      expect(await treasuryReserve.totalBurnedFromFees()).to.equal(0n);
      expect(await treasuryReserve.validatorReserveBalance()).to.equal(ethers.parseUnits("800", 18));
      expect(await treasuryReserve.ecosystemReserveBalance()).to.equal(ethers.parseUnits("200", 18));
    });

    it("reverts on zero deposit amount", async function () {
      await expect(
        treasuryReserve.connect(feePayer).depositProtocolFees(0)
      ).to.be.revertedWith("Amount must be positive");
    });

    it("reverts when paused", async function () {
      await treasuryReserve.connect(owner).pause();
      await expect(
        treasuryReserve.connect(feePayer).depositProtocolFees(FEE_AMOUNT)
      ).to.be.revertedWith("Pausable: paused");
    });
  });

  describe("fundValidatorEpoch", function () {
    beforeEach(async function () {
      await treasuryReserve.connect(owner).setStakingContract(stakingContract.address);
      await treasuryReserve.connect(feePayer).depositProtocolFees(FEE_AMOUNT);
      // Fee amount 1000 SYN -> validatorReserveBalance is 700 SYN
    });

    it("allows owner to disburse up to validatorReserveBalance", async function () {
      const disburseAmount = ethers.parseUnits("500", 18);
      const preBalance = await synToken.balanceOf(stakingContract.address);

      await expect(treasuryReserve.connect(owner).fundValidatorEpoch(disburseAmount))
        .to.emit(treasuryReserve, "ValidatorEpochFunded")
        .withArgs(stakingContract.address, disburseAmount, (val) => val > 0n);

      expect(await treasuryReserve.totalDisbursedToValidators()).to.equal(disburseAmount);
      expect(await treasuryReserve.validatorReserveBalance()).to.equal(ethers.parseUnits("200", 18));
      expect(await synToken.balanceOf(stakingContract.address)).to.equal(preBalance + disburseAmount);
    });

    it("reverts if disburse amount exceeds validatorReserveBalance", async function () {
      // validator reserve has 700 SYN. Trying to disburse 701 SYN must revert.
      const excessAmount = ethers.parseUnits("701", 18);
      await expect(
        treasuryReserve.connect(owner).fundValidatorEpoch(excessAmount)
      ).to.be.revertedWith("Insufficient validator reserve balance");
    });

    it("allows authorized governanceContract to disburse", async function () {
      await treasuryReserve.connect(owner).setGovernanceContract(governance.address);
      const disburseAmount = ethers.parseUnits("300", 18);

      await expect(treasuryReserve.connect(governance).fundValidatorEpoch(disburseAmount))
        .to.emit(treasuryReserve, "ValidatorEpochFunded")
        .withArgs(stakingContract.address, disburseAmount, (val) => val > 0n);

      expect(await treasuryReserve.totalDisbursedToValidators()).to.equal(disburseAmount);
    });

    it("rejects unauthorized caller", async function () {
      await expect(
        treasuryReserve.connect(unauthorized).fundValidatorEpoch(ethers.parseUnits("100", 18))
      ).to.be.revertedWith("Caller is not authorized");
    });

    it("reverts if stakingContract is not set", async function () {
      const TreasuryFactory = await ethers.getContractFactory("SYNTHOSTreasuryFeeReserve");
      const unconfigured = await TreasuryFactory.deploy(await synToken.getAddress(), owner.address);
      await unconfigured.waitForDeployment();

      await expect(
        unconfigured.connect(owner).fundValidatorEpoch(ethers.parseUnits("100", 18))
      ).to.be.revertedWith("Staking contract not set");
    });
  });

  describe("disburseEcosystemGrowth", function () {
    beforeEach(async function () {
      await treasuryReserve.connect(feePayer).depositProtocolFees(FEE_AMOUNT);
      // Fee amount 1000 SYN -> ecosystemReserveBalance is 200 SYN
    });

    it("allows owner to disburse ecosystem growth reserve to recipient", async function () {
      const disburseAmount = ethers.parseUnits("150", 18);
      const preRecipientBal = await synToken.balanceOf(ecosystemRecipient.address);

      await expect(treasuryReserve.connect(owner).disburseEcosystemGrowth(ecosystemRecipient.address, disburseAmount))
        .to.emit(treasuryReserve, "EcosystemDisbursed")
        .withArgs(ecosystemRecipient.address, disburseAmount, (val) => val > 0n);

      expect(await treasuryReserve.totalDisbursedToEcosystem()).to.equal(disburseAmount);
      expect(await treasuryReserve.ecosystemReserveBalance()).to.equal(ethers.parseUnits("50", 18));
      expect(await synToken.balanceOf(ecosystemRecipient.address)).to.equal(preRecipientBal + disburseAmount);
    });

    it("enforces that ecosystem disbursement cannot exceed accumulated ecosystem allocation", async function () {
      const excessAmount = ethers.parseUnits("201", 18);
      await expect(
        treasuryReserve.connect(owner).disburseEcosystemGrowth(ecosystemRecipient.address, excessAmount)
      ).to.be.revertedWith("Insufficient ecosystem reserve balance");
    });

    it("rejects unauthorized caller", async function () {
      await expect(
        treasuryReserve.connect(unauthorized).disburseEcosystemGrowth(ecosystemRecipient.address, ethers.parseUnits("50", 18))
      ).to.be.revertedWith("Caller is not authorized");
    });
  });

  describe("sweepSurplusTokens", function () {
    let otherToken;

    beforeEach(async function () {
      const MockERC20 = await ethers.getContractFactory("MockERC20");
      otherToken = await MockERC20.deploy("Random Token", "RND", owner.address, ethers.parseUnits("1000", 18));
      await otherToken.waitForDeployment();
    });

    it("sweeps plain transferred SYN that was not accounted for in reserves", async function () {
      // 1. Fee deposit of 1000 SYN: burn 100, validator 700, ecosystem 200 (accounted = 900)
      await treasuryReserve.connect(feePayer).depositProtocolFees(FEE_AMOUNT);

      // 2. Someone accidentally plain-transfers 50 SYN directly to the treasury contract
      const accidentalAmount = ethers.parseUnits("50", 18);
      await synToken.transfer(await treasuryReserve.getAddress(), accidentalAmount);

      const preBalance = await synToken.balanceOf(ecosystemRecipient.address);

      // 3. Sweep surplus SYN: exactly 50 SYN is swept, leaving accounted reserves intact
      await expect(
        treasuryReserve.connect(owner).sweepSurplusTokens(await synToken.getAddress(), ecosystemRecipient.address)
      ).to.emit(treasuryReserve, "SurplusTokensSwept")
        .withArgs(await synToken.getAddress(), ecosystemRecipient.address, accidentalAmount, (val) => val > 0n);

      expect(await synToken.balanceOf(ecosystemRecipient.address)).to.equal(preBalance + accidentalAmount);
      // Verify accounted reserves are untouched
      expect(await treasuryReserve.validatorReserveBalance()).to.equal(ethers.parseUnits("700", 18));
      expect(await treasuryReserve.ecosystemReserveBalance()).to.equal(ethers.parseUnits("200", 18));
    });

    it("reverts if attempting to sweep SYN when there is no unallocated surplus", async function () {
      await treasuryReserve.connect(feePayer).depositProtocolFees(FEE_AMOUNT);
      await expect(
        treasuryReserve.connect(owner).sweepSurplusTokens(await synToken.getAddress(), ecosystemRecipient.address)
      ).to.be.revertedWith("No surplus SYN to sweep");
    });

    it("sweeps non-SYN third-party ERC20 tokens sent to the contract", async function () {
      const sendAmount = ethers.parseUnits("250", 18);
      await otherToken.transfer(await treasuryReserve.getAddress(), sendAmount);

      await expect(
        treasuryReserve.connect(owner).sweepSurplusTokens(await otherToken.getAddress(), ecosystemRecipient.address)
      ).to.emit(treasuryReserve, "SurplusTokensSwept")
        .withArgs(await otherToken.getAddress(), ecosystemRecipient.address, sendAmount, (val) => val > 0n);

      expect(await otherToken.balanceOf(ecosystemRecipient.address)).to.equal(sendAmount);
    });

    it("rejects unauthorized sweep caller", async function () {
      await expect(
        treasuryReserve.connect(unauthorized).sweepSurplusTokens(await synToken.getAddress(), ecosystemRecipient.address)
      ).to.be.revertedWith("Caller is not authorized");
    });
  });

  describe("Administrative & Configuration Functions", function () {
    it("sets stakingContract and emits StakingContractUpdated", async function () {
      await expect(treasuryReserve.connect(owner).setStakingContract(stakingContract.address))
        .to.emit(treasuryReserve, "StakingContractUpdated")
        .withArgs(stakingContract.address);

      expect(await treasuryReserve.stakingContract()).to.equal(stakingContract.address);
    });

    it("reverts setStakingContract to address zero", async function () {
      await expect(
        treasuryReserve.connect(owner).setStakingContract(ethers.ZeroAddress)
      ).to.be.revertedWith("Invalid staking address");
    });

    it("sets governanceContract and emits GovernanceContractUpdated", async function () {
      await expect(treasuryReserve.connect(owner).setGovernanceContract(governance.address))
        .to.emit(treasuryReserve, "GovernanceContractUpdated")
        .withArgs(governance.address);

      expect(await treasuryReserve.governanceContract()).to.equal(governance.address);
    });

    it("adjusts allocation and enforces MAX_BPS invariant (10000 bps = 100%)", async function () {
      await expect(treasuryReserve.connect(owner).setAllocation(1500, 6500, 2000))
        .to.emit(treasuryReserve, "AllocationUpdated")
        .withArgs(1500n, 6500n, 2000n);

      expect(await treasuryReserve.burnBps()).to.equal(1500n);
      expect(await treasuryReserve.validatorReserveBps()).to.equal(6500n);
      expect(await treasuryReserve.ecosystemGrowthBps()).to.equal(2000n);

      await expect(
        treasuryReserve.connect(owner).setAllocation(2000, 7000, 2000)
      ).to.be.revertedWith("Must total 10000 BPS");
    });

    it("prevents non-owners from calling admin configuration", async function () {
      await expect(
        treasuryReserve.connect(unauthorized).setStakingContract(stakingContract.address)
      ).to.be.revertedWith("Ownable: caller is not the owner");

      await expect(
        treasuryReserve.connect(unauthorized).setGovernanceContract(governance.address)
      ).to.be.revertedWith("Ownable: caller is not the owner");

      await expect(
        treasuryReserve.connect(unauthorized).setAllocation(1000, 7000, 2000)
      ).to.be.revertedWith("Ownable: caller is not the owner");
    });

    it("pauses and unpauses correctly", async function () {
      expect(await treasuryReserve.paused()).to.be.false;
      await treasuryReserve.connect(owner).pause();
      expect(await treasuryReserve.paused()).to.be.true;
      await treasuryReserve.connect(owner).unpause();
      expect(await treasuryReserve.paused()).to.be.false;
    });
  });
});
