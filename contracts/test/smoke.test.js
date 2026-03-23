const { expect } = require("chai");
const { ethers } = require("hardhat");

describe("post-incubation compile / deploy smoke", function () {
  it("deploys core SYNTHOS stack on hardhat", async function () {
    const [deployer] = await ethers.getSigners();

    const SYNToken = await ethers.getContractFactory("SYNToken");
    const token = await SYNToken.deploy();
    await token.waitForDeployment();

    const Timelock = await ethers.getContractFactory("SYNTHOSTimelock");
    const timelock = await Timelock.deploy(
      60,
      [deployer.address],
      [deployer.address],
      deployer.address
    );
    await timelock.waitForDeployment();
    const timelockAddr = await timelock.getAddress();

    const Gov = await ethers.getContractFactory("SYNTHOSGovernance");
    const gov = await Gov.deploy(await token.getAddress(), timelockAddr);
    await gov.waitForDeployment();

    const proposerRole = await timelock.PROPOSER_ROLE();
    await timelock.grantRole(proposerRole, await gov.getAddress());
    await timelock.revokeRole(proposerRole, deployer.address);

    const Staking = await ethers.getContractFactory("SYNTHOSStaking");
    const staking = await Staking.deploy(
      await token.getAddress(),
      await gov.getAddress()
    );
    await staking.waitForDeployment();

    const Rewards = await ethers.getContractFactory("RewardDistributor");
    const rewards = await Rewards.deploy(await gov.getAddress());
    await rewards.waitForDeployment();

    expect(await token.getAddress()).to.match(/^0x[a-fA-F0-9]{40}$/);
    expect(await staking.getAddress()).to.match(/^0x[a-fA-F0-9]{40}$/);
  });
});
