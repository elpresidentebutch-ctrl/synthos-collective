const { expect } = require("chai");
const { ethers } = require("hardhat");

describe("post-incubation compile / deploy smoke", function () {
  it("deploys core SYNTHOS stack on hardhat", async function () {
    this.timeout(120000);
    const [deployer, adopter] = await ethers.getSigners();

    const SynCoin = await ethers.getContractFactory("SynCoin");
    const token = await SynCoin.deploy();
    await token.waitForDeployment();
    const tokenAddr = await token.getAddress();

    expect(await token.totalSupply()).to.equal(
      ethers.parseUnits("100000000000", 18)
    );
    expect(await token.undistributedSupply()).to.equal(
      ethers.parseUnits("100000000000", 18)
    );
    expect(await token.tokenomicsTotal()).to.equal(await token.INITIAL_SUPPLY());
    expect(await token.immuneRewardsBreakdownTotal()).to.equal(
      await token.IMMUNE_NODE_REWARDS_ALLOCATION()
    );
    expect(await token.validatorRewardsBreakdownTotal()).to.equal(
      await token.VALIDATOR_REWARDS_ALLOCATION()
    );
    expect(await token.communityRewardsBreakdownTotal()).to.equal(
      await token.COMMUNITY_ALLOCATION()
    );

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
    const gov = await Gov.deploy(tokenAddr, timelockAddr);
    await gov.waitForDeployment();

    const proposerRole = await timelock.PROPOSER_ROLE();
    await timelock.grantRole(proposerRole, await gov.getAddress());
    await timelock.revokeRole(proposerRole, deployer.address);

    const Staking = await ethers.getContractFactory("SYNTHOSStaking");
    const staking = await Staking.deploy(
      tokenAddr,
      await gov.getAddress()
    );
    await staking.waitForDeployment();

    const Rewards = await ethers.getContractFactory("RewardDistributor");
    const rewards = await Rewards.deploy(await gov.getAddress());
    await rewards.waitForDeployment();

    const AdopterRewards = await ethers.getContractFactory(
      "SYNTHOSAdopterRewards"
    );
    const adopterRewards = await AdopterRewards.deploy(
      tokenAddr,
      ethers.parseUnits("500", 18),
      ethers.parseUnits("1000", 18),
      2592000,
      120
    );
    await adopterRewards.waitForDeployment();

    const Dex = await ethers.getContractFactory("SYNTHOSDex");
    const dex = await Dex.deploy(tokenAddr);
    await dex.waitForDeployment();

    const MockERC20 = await ethers.getContractFactory("MockERC20");
    const b12 = await MockERC20.deploy(
      "B12 Test Asset",
      "B12",
      deployer.address,
      ethers.parseUnits("50000", 18)
    );
    await b12.waitForDeployment();

    const FounderVesting = await ethers.getContractFactory(
      "SYNTHOSFounderAnnualVesting"
    );
    const founderSchedule = [
      1811548800, 1843171200, 1874707200, 1906243200, 1937779200,
      1969401600, 2000937600, 2032473600, 2064009600, 2095632000,
    ];
    const founderVesting = await FounderVesting.deploy(
      tokenAddr,
      deployer.address,
      await token.FOUNDER_ANNUAL_RELEASE(),
      founderSchedule
    );
    await founderVesting.waitForDeployment();

    await token.allocateTokens(
      await founderVesting.getAddress(),
      await token.FOUNDER_VESTING_ALLOCATION(),
      "FOUNDER_VESTING"
    );
    await token.allocateTokens(
      await adopterRewards.getAddress(),
      await token.IMMUNE_NODE_REWARDS_ALLOCATION(),
      "IMMUNE_NODE_REWARDS"
    );
    await token.allocateTokens(
      deployer.address,
      ethers.parseUnits("10000000", 18),
      "LOCKED_DEX_LIQUIDITY"
    );

    await dex.createPool(await b12.getAddress());
    await token.approve(await dex.getAddress(), ethers.parseUnits("10000000", 18));
    await b12.approve(await dex.getAddress(), ethers.parseUnits("50000", 18));
    await dex.addLiquidity(
      await b12.getAddress(),
      ethers.parseUnits("10000000", 18),
      ethers.parseUnits("50000", 18)
    );

    const hardwareCommitment = ethers.keccak256(
      ethers.toUtf8Bytes("desktop-node-smoke-test")
    );
    const nodeId = await adopterRewards.registerAndClaim.staticCall(
      hardwareCommitment,
      "DESKTOP"
    );
    await adopterRewards.registerAndClaim(hardwareCommitment, "DESKTOP");

    expect(await token.getAddress()).to.match(/^0x[a-fA-F0-9]{40}$/);
    expect(await staking.getAddress()).to.match(/^0x[a-fA-F0-9]{40}$/);
    expect(await token.balanceOf(await founderVesting.getAddress())).to.equal(
      await token.FOUNDER_VESTING_ALLOCATION()
    );
    expect(await adopterRewards.registeredNodeCount()).to.equal(1);
    expect(await adopterRewards.nodeByOperator(deployer.address)).to.equal(
      nodeId
    );
    expect(await token.balanceOf(deployer.address)).to.equal(
      ethers.parseUnits("500", 18)
    );
    expect(await adopterRewards.TARGET_IMMUNE_OPERATORS()).to.equal(100000);
    expect(await adopterRewards.TEN_YEAR_HEARTBEAT_PERIODS()).to.equal(120);
    expect(await adopterRewards.TEN_YEAR_MAX_REWARD_PER_OPERATOR()).to.equal(
      ethers.parseUnits("120500", 18)
    );
    expect(await staking.TARGET_VALIDATOR_OPERATORS()).to.equal(5000);
    expect(await staking.TEN_YEAR_MAX_REWARD_PER_VALIDATOR()).to.equal(
      ethers.parseUnits("910000", 18)
    );
    await expect(
      adopterRewards.registerAndClaim(
        ethers.keccak256(ethers.toUtf8Bytes("second-node-same-operator")),
        "DESKTOP"
      )
    ).to.be.revertedWith("operator already registered");

    const gatedHardwareCommitment = ethers.keccak256(
      ethers.toUtf8Bytes("desktop-node-merkle-test")
    );
    const merkleLeaf = await adopterRewards.adopterLeaf(
      adopter.address,
      gatedHardwareCommitment,
      "DESKTOP"
    );
    await adopterRewards.setAdopterMerkleRoot(merkleLeaf, true);
    await expect(
      adopterRewards.connect(adopter).registerAndClaim(
        gatedHardwareCommitment,
        "DESKTOP"
      )
    ).to.be.revertedWith("merkle proof required");
    await adopterRewards.connect(adopter).registerAndClaimWithProof(
      gatedHardwareCommitment,
      "DESKTOP",
      []
    );
    expect(await adopterRewards.adopterMerkleRoot()).to.equal(merkleLeaf);
    expect(await token.balanceOf(adopter.address)).to.equal(
      ethers.parseUnits("500", 18)
    );
    expect(await dex.synToken()).to.equal(tokenAddr);
    expect(
      await dex.quoteSynForAsset(await b12.getAddress(), ethers.parseUnits("1000", 18))
    ).to.be.gt(0);
  });
});
