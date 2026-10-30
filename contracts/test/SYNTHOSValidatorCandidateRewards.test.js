const { expect } = require("chai");
const { ethers } = require("hardhat");
const { time } = require("@nomicfoundation/hardhat-network-helpers");

describe("SYNTHOSValidatorCandidateRewards", function () {
  let synToken;
  let rewardsContract;
  let owner;
  let candidate1;
  let candidate2;
  let keeper;

  const ONE_MONTH = 30 * 24 * 60 * 60; // 30 days in seconds
  const FIVE_THOUSAND_SYN = ethers.parseUnits("5000", 18);
  const POOL_FUNDING = ethers.parseUnits("1000000", 18); // 1M SYN

  beforeEach(async function () {
    [owner, candidate1, candidate2, keeper] = await ethers.getSigners();

    // Deploy a mock or real SynCoin / MockERC20
    const MockERC20 = await ethers.getContractFactory("MockERC20");
    synToken = await MockERC20.deploy("SYNTHOS", "SYN", 18, ethers.parseUnits("100000000", 18));
    await synToken.waitForDeployment();

    // Deploy SYNTHOSValidatorCandidateRewards
    const RewardsFactory = await ethers.getContractFactory("SYNTHOSValidatorCandidateRewards");
    rewardsContract = await RewardsFactory.deploy(await synToken.getAddress());
    await rewardsContract.waitForDeployment();

    // Fund the rewards contract with SYN tokens
    await synToken.transfer(await rewardsContract.getAddress(), POOL_FUNDING);
  });

  describe("Candidate Registration", function () {
    it("allows a validator candidate to register", async function () {
      const nodeKey = ethers.keccak256(ethers.toUtf8Bytes("node-key-candidate-1"));
      await expect(rewardsContract.connect(candidate1).registerCandidate(nodeKey))
        .to.emit(rewardsContract, "CandidateRegistered")
        .withArgs(candidate1.address, nodeKey, await time.latest() + 1);

      const candidateData = await rewardsContract.getCandidate(candidate1.address);
      expect(candidateData.operator).to.equal(candidate1.address);
      expect(candidateData.nodeKeyHash).to.equal(nodeKey);
      expect(candidateData.monthsPaid).to.equal(0n);
      expect(candidateData.active).to.be.true;
    });

    it("prevents duplicate candidate registration", async function () {
      const nodeKey = ethers.keccak256(ethers.toUtf8Bytes("node-key-candidate-1"));
      await rewardsContract.connect(candidate1).registerCandidate(nodeKey);

      await expect(
        rewardsContract.connect(candidate1).registerCandidate(nodeKey)
      ).to.be.revertedWithCustomError(rewardsContract, "CandidateAlreadyRegistered");
    });
  });

  describe("Automatic 1-Month Verification & Reward Payout", function () {
    const nodeKey1 = ethers.keccak256(ethers.toUtf8Bytes("candidate-1"));
    const nodeKey2 = ethers.keccak256(ethers.toUtf8Bytes("candidate-2"));

    beforeEach(async function () {
      await rewardsContract.connect(candidate1).registerCandidate(nodeKey1);
      await rewardsContract.connect(candidate2).registerCandidate(nodeKey2);
    });

    it("reports not verified before 1 month has elapsed", async function () {
      const status = await rewardsContract.isVerifiedForOneMonth(candidate1.address);
      expect(status.isVerified).to.be.false;
      expect(status.timeRemaining).to.be.closeTo(BigInt(ONE_MONTH), 5n);
      expect(status.pendingReward).to.equal(0n);

      // Attempting to claim reverts with VerificationIntervalNotMet
      await expect(
        rewardsContract.connect(candidate1).claimMonthlyReward()
      ).to.be.revertedWithCustomError(rewardsContract, "VerificationIntervalNotMet");
    });

    it("automatically becomes verified after 30 days and pays exactly 5,000 SYN", async function () {
      // Fast forward 30 days
      await time.increase(ONE_MONTH);

      // Verify on-chain status
      const status = await rewardsContract.isVerifiedForOneMonth(candidate1.address);
      expect(status.isVerified).to.be.true;
      expect(status.timeRemaining).to.equal(0n);
      expect(status.pendingReward).to.equal(FIVE_THOUSAND_SYN);

      // Candidate claims reward
      const balanceBefore = await synToken.balanceOf(candidate1.address);
      await expect(rewardsContract.connect(candidate1).claimMonthlyReward())
        .to.emit(rewardsContract, "ValidatorRewardPaid")
        .withArgs(candidate1.address, candidate1.address, FIVE_THOUSAND_SYN, 1n, await time.latest() + 1);

      const balanceAfter = await synToken.balanceOf(candidate1.address);
      expect(balanceAfter - balanceBefore).to.equal(FIVE_THOUSAND_SYN);

      // Immediately after claim, candidate is no longer verified until the next 30 days
      const statusAfter = await rewardsContract.isVerifiedForOneMonth(candidate1.address);
      expect(statusAfter.isVerified).to.be.false;
      expect(statusAfter.timeRemaining).to.be.closeTo(BigInt(ONE_MONTH), 5n);
    });

    it("allows a keeper or automated bot to trigger the payout for a verified candidate", async function () {
      // Fast forward 30 days
      await time.increase(ONE_MONTH);

      const balanceBefore = await synToken.balanceOf(candidate1.address);

      // Keeper calls claimRewardFor
      await rewardsContract.connect(keeper).claimRewardFor(candidate1.address);

      const balanceAfter = await synToken.balanceOf(candidate1.address);
      expect(balanceAfter - balanceBefore).to.equal(FIVE_THOUSAND_SYN);
    });

    it("batch distributes rewards to verified candidates and cleanly handles non-verified ones", async function () {
      // Candidate 1 was registered at T=0
      // Advance 15 days, then register candidate 3
      await time.increase(15 * 24 * 60 * 60);

      const [,,, , candidate3] = await ethers.getSigners();
      const nodeKey3 = ethers.keccak256(ethers.toUtf8Bytes("candidate-3"));
      await rewardsContract.connect(candidate3).registerCandidate(nodeKey3);

      // Advance another 16 days (Total 31 days for candidates 1 & 2; 16 days for candidate 3)
      await time.increase(16 * 24 * 60 * 60);

      // Batch sweep all 3
      const tx = await rewardsContract.connect(keeper).distributeEligibleRewards([
        candidate1.address,
        candidate2.address,
        candidate3.address,
      ]);

      // Candidate 1 and 2 should get paid
      await expect(tx).to.emit(rewardsContract, "ValidatorRewardPaid")
        .withArgs(candidate1.address, candidate1.address, FIVE_THOUSAND_SYN, 1n, await time.latest());
      await expect(tx).to.emit(rewardsContract, "ValidatorRewardPaid")
        .withArgs(candidate2.address, candidate2.address, FIVE_THOUSAND_SYN, 1n, await time.latest());

      // Candidate 3 is NOT verified yet (only 16 days), so it emits VerificationFailed and doesn't halt
      await expect(tx).to.emit(rewardsContract, "VerificationFailed");

      expect(await synToken.balanceOf(candidate1.address)).to.equal(FIVE_THOUSAND_SYN);
      expect(await synToken.balanceOf(candidate2.address)).to.equal(FIVE_THOUSAND_SYN);
      expect(await synToken.balanceOf(candidate3.address)).to.equal(0n);
    });
  });
});
