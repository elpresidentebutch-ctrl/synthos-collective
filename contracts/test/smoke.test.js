const { expect } = require("chai");
const { ethers } = require("hardhat");

describe("post-incubation compile / deploy smoke", function () {
  it("rejects unsafe sovereign multisig configuration", async function () {
    const [ownerA, ownerB] = await ethers.getSigners();

    const Multisig = await ethers.getContractFactory("SYNTHOSMultisig");

    await expect(Multisig.deploy([], 1)).to.be.revertedWith("owners required");
    await expect(
      Multisig.deploy([ownerA.address], 0)
    ).to.be.revertedWith("threshold required");
    await expect(
      Multisig.deploy([ownerA.address], 2)
    ).to.be.revertedWith("threshold exceeds owners");
    await expect(
      Multisig.deploy([ownerA.address, ethers.ZeroAddress], 1)
    ).to.be.revertedWith("invalid owner");
    await expect(
      Multisig.deploy([ownerA.address, ownerA.address], 1)
    ).to.be.revertedWith("duplicate owner");

    const multisig = await Multisig.deploy([ownerA.address, ownerB.address], 2);
    await multisig.waitForDeployment();

    await expect(
      multisig.submitTransaction(ethers.ZeroAddress, 0, "0x")
    ).to.be.revertedWith("invalid target");
  });

  it("executes launch admin calls through the sovereign multisig", async function () {
    const [ownerA, ownerB, ownerC, outsider, treasury] = await ethers.getSigners();

    const SynCoin = await ethers.getContractFactory("SynCoin");
    const token = await SynCoin.deploy();
    await token.waitForDeployment();

    const Multisig = await ethers.getContractFactory("SYNTHOSMultisig");
    const multisig = await Multisig.deploy(
      [ownerA.address, ownerB.address, ownerC.address],
      2
    );
    await multisig.waitForDeployment();

    expect(await multisig.threshold()).to.equal(2);
    expect(await multisig.owners()).to.deep.equal([
      ownerA.address,
      ownerB.address,
      ownerC.address,
    ]);

    await token.transferOwnership(await multisig.getAddress());

    const data = token.interface.encodeFunctionData("setTreasury", [
      treasury.address,
    ]);
    await multisig.submitTransaction(await token.getAddress(), 0, data);

    await expect(multisig.executeTransaction(0)).to.be.revertedWith(
      "insufficient confirmations"
    );
    await expect(
      multisig.connect(outsider).confirmTransaction(0)
    ).to.be.revertedWith("not owner");

    await multisig.connect(ownerB).confirmTransaction(0);
    await multisig.connect(outsider).executeTransaction(0);

    expect(await token.treasury()).to.equal(treasury.address);
    const transaction = await multisig.getTransaction(0);
    expect(transaction.executed).to.equal(true);

    await expect(multisig.executeTransaction(0)).to.be.revertedWith(
      "transaction already executed"
    );
  });

  it("handles multisig confirmation revocation and failed calls safely", async function () {
    const [ownerA, ownerB, ownerC, treasury] = await ethers.getSigners();

    const SynCoin = await ethers.getContractFactory("SynCoin");
    const token = await SynCoin.deploy();
    await token.waitForDeployment();

    const Multisig = await ethers.getContractFactory("SYNTHOSMultisig");
    const multisig = await Multisig.deploy(
      [ownerA.address, ownerB.address, ownerC.address],
      2
    );
    await multisig.waitForDeployment();

    await token.transferOwnership(await multisig.getAddress());

    const data = token.interface.encodeFunctionData("setTreasury", [
      treasury.address,
    ]);
    await multisig.submitTransaction(await token.getAddress(), 0, data);

    await expect(
      multisig.confirmTransaction(0)
    ).to.be.revertedWith("already confirmed");

    await multisig.connect(ownerB).confirmTransaction(0);
    await multisig.connect(ownerB).revokeConfirmation(0);
    await expect(multisig.executeTransaction(0)).to.be.revertedWith(
      "insufficient confirmations"
    );
    expect(await multisig.isConfirmed(0, ownerB.address)).to.equal(false);

    await multisig.connect(ownerC).confirmTransaction(0);
    await multisig.executeTransaction(0);
    expect(await token.treasury()).to.equal(treasury.address);

    const failingData = token.interface.encodeFunctionData("setTreasury", [
      ethers.ZeroAddress,
    ]);
    await multisig.submitTransaction(await token.getAddress(), 0, failingData);
    await multisig.connect(ownerB).confirmTransaction(1);
    await expect(multisig.executeTransaction(1)).to.be.revertedWith(
      "transaction failed"
    );
    const failedTransaction = await multisig.getTransaction(1);
    expect(failedTransaction.executed).to.equal(false);
  });

  it("can custody and release ETH through the sovereign multisig", async function () {
    const [ownerA, ownerB, recipient] = await ethers.getSigners();

    const Multisig = await ethers.getContractFactory("SYNTHOSMultisig");
    const multisig = await Multisig.deploy([ownerA.address, ownerB.address], 2);
    await multisig.waitForDeployment();

    await ownerA.sendTransaction({
      to: await multisig.getAddress(),
      value: ethers.parseEther("1"),
    });

    const recipientBefore = await ethers.provider.getBalance(recipient.address);
    await multisig.submitTransaction(
      recipient.address,
      ethers.parseEther("0.25"),
      "0x"
    );
    await multisig.connect(ownerB).confirmTransaction(0);
    await multisig.executeTransaction(0);

    expect(await ethers.provider.getBalance(await multisig.getAddress())).to.equal(
      ethers.parseEther("0.75")
    );
    expect(await ethers.provider.getBalance(recipient.address)).to.equal(
      recipientBefore + ethers.parseEther("0.25")
    );
  });

  it("applies treasury recycling burn only through protocol spend", async function () {
    const [deployer, spender, treasury] = await ethers.getSigners();

    const SynCoin = await ethers.getContractFactory("SynCoin");
    const token = await SynCoin.deploy();
    await token.waitForDeployment();

    await token.setTreasury(treasury.address);
    expect(await token.treasury()).to.equal(treasury.address);

    await token.allocateTokens(
      spender.address,
      ethers.parseUnits("1000", 18),
      "COMMUNITY"
    );

    await token.connect(spender).transfer(deployer.address, ethers.parseUnits("100", 18));
    expect(await token.balanceOf(deployer.address)).to.equal(
      ethers.parseUnits("100", 18)
    );
    expect(await token.totalSupply()).to.equal(
      ethers.parseUnits("100000000000", 18)
    );

    await token.connect(spender).treasuryRecyclingBurn(
      ethers.parseUnits("200", 18),
      await token.SPEND_PROTOCOL()
    );

    expect(await token.balanceOf(spender.address)).to.equal(
      ethers.parseUnits("700", 18)
    );
    expect(await token.balanceOf(treasury.address)).to.equal(
      ethers.parseUnits("100", 18)
    );
    expect(await token.totalSupply()).to.equal(
      ethers.parseUnits("99999999900", 18)
    );
    expect(await token.totalTreasuryRecyclingBurned()).to.equal(
      ethers.parseUnits("100", 18)
    );
    expect(await token.totalTreasuryRecycled()).to.equal(
      ethers.parseUnits("100", 18)
    );
    expect(await token.treasuryRecyclingBurnedByType(await token.SPEND_PROTOCOL())).to.equal(
      ethers.parseUnits("100", 18)
    );
    expect(await token.treasuryRecycledByType(await token.SPEND_PROTOCOL())).to.equal(
      ethers.parseUnits("100", 18)
    );

    await expect(
      token.connect(spender).setTreasury(spender.address)
    ).to.be.revertedWith("Ownable: caller is not the owner");
    await expect(
      token.connect(spender).treasuryRecyclingBurn(1, await token.SPEND_PROTOCOL())
    ).to.be.revertedWith("amount too small");
  });

  it("preserves treasury recycling burn invariants under edge cases", async function () {
    const [deployer, spender, treasury] = await ethers.getSigners();

    const SynCoin = await ethers.getContractFactory("SynCoin");
    const token = await SynCoin.deploy();
    await token.waitForDeployment();

    await token.setTreasury(treasury.address);
    await token.allocateTokens(spender.address, 9, "TEST");

    await token.connect(spender).treasuryRecyclingBurn(
      3,
      await token.SPEND_SERVICE_FEE()
    );
    expect(await token.totalSupply()).to.equal(
      ethers.parseUnits("100000000000", 18) - 1n
    );
    expect(await token.balanceOf(treasury.address)).to.equal(2);
    expect(await token.balanceOf(spender.address)).to.equal(6);
    expect(await token.totalTreasuryRecyclingBurned()).to.equal(1);
    expect(await token.totalTreasuryRecycled()).to.equal(2);

    await expect(
      token.connect(spender).treasuryRecyclingBurn(7, await token.SPEND_SERVICE_FEE())
    ).to.be.revertedWith("insufficient balance");

    await token.pause();
    await expect(
      token.connect(spender).treasuryRecyclingBurn(2, await token.SPEND_SERVICE_FEE())
    ).to.be.revertedWith("ERC20Pausable: token transfer while paused");

    await token.unpause();
    await token.connect(spender).treasuryRecyclingBurn(
      2,
      await token.SPEND_SERVICE_FEE()
    );
    expect(await token.balanceOf(treasury.address)).to.equal(3);
    expect(await token.balanceOf(spender.address)).to.equal(4);
    expect(await token.balanceOf(deployer.address)).to.equal(0);

    const unapprovedSpendType = ethers.keccak256(
      ethers.toUtf8Bytes("UNAPPROVED")
    );
    await expect(
      token.connect(spender).treasuryRecyclingBurn(2, unapprovedSpendType)
    ).to.be.revertedWith("spend type not approved");

    await expect(
      token.connect(spender).setTreasuryRecyclingSpendType(unapprovedSpendType, true)
    ).to.be.revertedWith("Ownable: caller is not the owner");
    await expect(
      token.setTreasuryRecyclingSpendType(ethers.ZeroHash, true)
    ).to.be.revertedWith("invalid spend type");

    await token.setTreasuryRecyclingSpendType(unapprovedSpendType, true);
    expect(await token.approvedTreasuryRecyclingSpendTypes(unapprovedSpendType)).to.equal(
      true
    );
    await token.setTreasuryRecyclingSpendType(unapprovedSpendType, false);
    expect(await token.approvedTreasuryRecyclingSpendTypes(unapprovedSpendType)).to.equal(
      false
    );
  });

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

    const ComplianceRegistry = await ethers.getContractFactory(
      "SYNTHOSComplianceRegistry"
    );
    const compliance = await ComplianceRegistry.deploy();
    await compliance.waitForDeployment();

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
    expect(await compliance.getAddress()).to.match(/^0x[a-fA-F0-9]{40}$/);
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

    const disclosureHash = ethers.keccak256(
      ethers.toUtf8Bytes("SYNTHOS token risk disclosure v1")
    );
    const jurisdictionHash = ethers.keccak256(ethers.toUtf8Bytes("US"));
    await compliance.setComplianceRecord(
      adopter.address,
      3, // ImmuneOperator
      true,
      true,
      false,
      false,
      0,
      ethers.ZeroHash,
      jurisdictionHash
    );
    expect(await compliance.eligibleToReceive(adopter.address, 3)).to.equal(false);
    await compliance.connect(adopter).acknowledgeDisclosure(disclosureHash);
    expect(await compliance.eligibleToReceive(adopter.address, 3)).to.equal(true);
    await compliance.revokeRecipient(adopter.address, "operator disqualified");
    expect(await compliance.eligibleToReceive(adopter.address, 3)).to.equal(false);
    await compliance.restoreRecipient(adopter.address);
    expect(await compliance.eligibleToReceive(adopter.address, 3)).to.equal(true);
  });
});
