const { expect } = require("chai");
const { ethers } = require("hardhat");

describe("RewardDistributor", function () {
  async function deployFixture() {
    const [governance, beneficiary, recipient, outsider] = await ethers.getSigners();

    const MockERC20 = await ethers.getContractFactory("MockERC20");
    const token = await MockERC20.deploy(
      "SYNTHOS",
      "SYN",
      governance.address,
      ethers.parseEther("1000000")
    );
    await token.waitForDeployment();

    const Distributor = await ethers.getContractFactory("RewardDistributor");
    const distributor = await Distributor.deploy(governance.address);
    await distributor.waitForDeployment();
    const distributorAddress = await distributor.getAddress();

    await distributor.connect(governance).approveToken(await token.getAddress());

    // Fund the distributor the way a real deployment would: governance
    // transfers in the tokens a vesting/reward program will pay out of.
    await token.connect(governance).transfer(distributorAddress, ethers.parseEther("100000"));

    return { governance, beneficiary, recipient, outsider, token, distributor, distributorAddress };
  }

  it("actually pays out tokens on claimVesting, not just an event", async function () {
    const { governance, beneficiary, token, distributor } = await deployFixture();
    const tokenAddress = await token.getAddress();

    const total = ethers.parseEther("1000");
    const duration = 1000; // seconds
    const cliff = 0;

    const tx = await distributor
      .connect(governance)
      .createVesting(tokenAddress, beneficiary.address, total, duration, cliff);
    const receipt = await tx.wait();
    const event = receipt.logs
      .map((log) => {
        try {
          return distributor.interface.parseLog(log);
        } catch {
          return null;
        }
      })
      .find((parsed) => parsed && parsed.name === "VestingCreated");
    const vestingId = event.args.vesting_id;

    // Push past the full vesting duration so the whole amount is claimable.
    await ethers.provider.send("evm_increaseTime", [duration + 1]);
    await ethers.provider.send("evm_mine", []);

    const before = await token.balanceOf(beneficiary.address);
    await distributor.connect(beneficiary).claimVesting(vestingId);
    const after = await token.balanceOf(beneficiary.address);

    // This is the actual regression check: before the fix, claimVesting
    // updated claimed_amount and emitted VestingClaimed but never called
    // transfer, so this balance delta would be zero even though the event
    // said the full amount was claimed.
    expect(after - before).to.equal(total);
  });

  it("actually pays out tokens on claimReward, not just an event", async function () {
    const { governance, recipient, token, distributor } = await deployFixture();
    const tokenAddress = await token.getAddress();

    const amount = ethers.parseEther("50");
    await distributor
      .connect(governance)
      .batchDistributeRewards(tokenAddress, [recipient.address], [amount], "test-reward");

    const before = await token.balanceOf(recipient.address);
    await distributor.connect(recipient).claimReward(0);
    const after = await token.balanceOf(recipient.address);

    expect(after - before).to.equal(amount);

    // Second claim on the same (now-zeroed) reward must fail, same as
    // before the fix.
    await expect(distributor.connect(recipient).claimReward(0)).to.be.revertedWith(
      "Already claimed"
    );
  });

  it("lets governance sweep a token that was never approved", async function () {
    const { governance, outsider, distributor, distributorAddress } = await deployFixture();

    const MockERC20 = await ethers.getContractFactory("MockERC20");
    const strayToken = await MockERC20.deploy(
      "Stray",
      "STRAY",
      governance.address,
      ethers.parseEther("10")
    );
    await strayToken.waitForDeployment();
    await strayToken.connect(governance).transfer(distributorAddress, ethers.parseEther("10"));

    await distributor
      .connect(governance)
      .sweepUnapprovedToken(await strayToken.getAddress(), outsider.address, ethers.parseEther("10"));

    expect(await strayToken.balanceOf(outsider.address)).to.equal(ethers.parseEther("10"));
  });

  it("refuses to sweep the real reward token, even after it's revoked", async function () {
    const { governance, outsider, token, distributor } = await deployFixture();
    const tokenAddress = await token.getAddress();

    // sweepUnapprovedToken must reject it while approved...
    await expect(
      distributor.connect(governance).sweepUnapprovedToken(tokenAddress, outsider.address, 1)
    ).to.be.revertedWith("Token was or is an approved reward token");

    // ...and must keep rejecting it after revocation, since revoking
    // doesn't erase whatever vestings/rewards were already created against
    // it -- this is the exact gap a "not currently approved" check would
    // have left open.
    await distributor.connect(governance).revokeToken(tokenAddress);
    await expect(
      distributor.connect(governance).sweepUnapprovedToken(tokenAddress, outsider.address, 1)
    ).to.be.revertedWith("Token was or is an approved reward token");
  });
});
