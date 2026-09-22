const { expect } = require("chai");
const { ethers } = require("hardhat");
const { deploySynWithBridge, mintSyn } = require("./helpers/syn");

describe("SYNTHOSStaking", function () {
  async function deployFixture() {
    const [governance, treasury, validatorSigner, delegator] = await ethers.getSigners();

    const { syn, minter } = await deploySynWithBridge(treasury, [treasury]);

    const Staking = await ethers.getContractFactory("SYNTHOSStaking");
    const staking = await Staking.deploy(await syn.getAddress(), governance.address);
    await staking.waitForDeployment();

    const stakingAddress = await staking.getAddress();

    // Fund and register a validator.
    const minStake = ethers.parseUnits("100000", 18);
    await mintSyn(minter, [treasury], validatorSigner.address, minStake, "validator-stake");
    await syn.connect(validatorSigner).approve(stakingAddress, minStake);
    await staking.connect(validatorSigner).registerValidator(minStake);

    // Fund a delegator with exactly ONE real delegation of 1,000 SYN.
    const delegateAmount = ethers.parseUnits("1000", 18);
    await mintSyn(minter, [treasury], delegator.address, delegateAmount, "delegator-funds");
    await syn.connect(delegator).approve(stakingAddress, delegateAmount);
    await staking.connect(delegator).delegateToValidator(validatorSigner.address, delegateAmount);

    return { governance, treasury, validatorSigner, delegator, syn, staking, delegateAmount };
  }

  it("does not let a delegator request-unstake the same delegation more than once", async function () {
    const { validatorSigner, delegator, staking, delegateAmount } = await deployFixture();

    // First request against the real 1,000 SYN delegation succeeds.
    await expect(staking.connect(delegator).requestUnstake(validatorSigner.address, delegateAmount))
      .to.emit(staking, "UnstakeRequested");

    // A second request against the SAME already-claimed delegation must be
    // rejected -- this is the exact drain this fix closes. Before the fix,
    // requestUnstake only summed delegations[msg.sender] without ever
    // reducing them, so this second call (and a third, and a
    // hundredth) would all succeed, each creating its own independent
    // UnstakeRequest claimable via claimUnstake once the cooldown passed.
    await expect(
      staking.connect(delegator).requestUnstake(validatorSigner.address, delegateAmount)
    ).to.be.revertedWith("Insufficient delegation");
  });

  it("lets a delegator unstake in partial pieces that sum to their real delegation, but no more", async function () {
    const { validatorSigner, delegator, staking, delegateAmount } = await deployFixture();

    const half = delegateAmount / 2n;

    await staking.connect(delegator).requestUnstake(validatorSigner.address, half);
    await staking.connect(delegator).requestUnstake(validatorSigner.address, half);

    // The delegation is now fully consumed (half + half == delegateAmount);
    // any further request, even for 1 wei, must fail.
    await expect(
      staking.connect(delegator).requestUnstake(validatorSigner.address, 1n)
    ).to.be.revertedWith("Insufficient delegation");
  });

  it("actually pays out real delegated tokens once, after cooldown, via claimUnstake", async function () {
    const { validatorSigner, delegator, syn, staking, delegateAmount } = await deployFixture();

    await staking.connect(delegator).requestUnstake(validatorSigner.address, delegateAmount);

    // Cooldown hasn't passed yet.
    await expect(staking.connect(delegator).claimUnstake(0)).to.be.revertedWith(
      "Cooldown not expired"
    );

    const UNSTAKE_COOLDOWN_SECONDS = 7 * 24 * 60 * 60;
    await ethers.provider.send("evm_increaseTime", [UNSTAKE_COOLDOWN_SECONDS + 1]);
    await ethers.provider.send("evm_mine", []);

    const before = await syn.balanceOf(delegator.address);
    await staking.connect(delegator).claimUnstake(0);
    const after = await syn.balanceOf(delegator.address);
    expect(after - before).to.equal(delegateAmount);

    // Claiming the same request a second time must fail -- and there is no
    // second request to claim either, since the fixed requestUnstake
    // wouldn't have allowed creating one against this already-consumed
    // delegation in the first place.
    await expect(staking.connect(delegator).claimUnstake(0)).to.be.revertedWith("Already claimed");
  });
});
