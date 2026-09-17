const { expect } = require("chai");
const { ethers } = require("hardhat");
const { anyValue } = require("@nomicfoundation/hardhat-chai-matchers/withArgs");

describe("SynCoin + SYNTHOSSynBridgeMinter (bridge-pegged wrapper)", function () {
  async function deployFixture() {
    const [owner, relayerA, relayerB, relayerC, user, recipient, outsider, treasury] =
      await ethers.getSigners();

    const SynCoin = await ethers.getContractFactory("SynCoin");
    const syn = await SynCoin.deploy(treasury.address);
    await syn.waitForDeployment();

    const Minter = await ethers.getContractFactory("SYNTHOSSynBridgeMinter");
    const minter = await Minter.deploy(
      await syn.getAddress(),
      [relayerA.address, relayerB.address, relayerC.address],
      2
    );
    await minter.waitForDeployment();

    return {
      owner,
      relayerA,
      relayerB,
      relayerC,
      user,
      recipient,
      outsider,
      treasury,
      syn,
      minter,
    };
  }

  it("starts with zero supply and no bridge minter wired up", async function () {
    const { syn } = await deployFixture();
    expect(await syn.totalSupply()).to.equal(0);
    expect(await syn.bridgeMinterInitialized()).to.equal(false);
    expect(await syn.bridgeMinter()).to.equal(ethers.ZeroAddress);
  });

  it("cannot mint before the bridge minter is wired up, and the owner can never mint directly", async function () {
    const { syn, minter, owner, recipient } = await deployFixture();

    // Nobody -- not even the owner -- can call mint() directly; only the
    // wired-up bridge minter contract can, and it hasn't been wired up yet.
    await expect(
      syn.connect(owner).mint(recipient.address, 1)
    ).to.be.revertedWith("not bridge minter");

    // And the minter itself can't mint until SynCoin has wired it up.
    await minter.unpause();
    const sourceEventId = ethers.keccak256(ethers.toUtf8Bytes("pre-init"));
    const [, relayerA] = await ethers.getSigners();
    await expect(
      minter.connect(relayerA).approveMint(sourceEventId, recipient.address, 100)
    ).to.not.be.reverted; // first approval alone doesn't hit quorum (threshold 2), so nothing mints yet
    expect(await syn.totalSupply()).to.equal(0);
  });

  it("wires the bridge minter exactly once and permanently", async function () {
    const { syn, minter, owner } = await deployFixture();

    await expect(syn.initializeBridgeMinter(await minter.getAddress()))
      .to.emit(syn, "BridgeMinterInitialized")
      .withArgs(await minter.getAddress());

    expect(await syn.bridgeMinterInitialized()).to.equal(true);
    expect(await syn.bridgeMinter()).to.equal(await minter.getAddress());

    // Not even the owner can rewire it afterward -- there's no
    // setBridgeMinter, and calling initializeBridgeMinter again fails.
    await expect(
      syn.connect(owner).initializeBridgeMinter(owner.address)
    ).to.be.revertedWith("bridge minter already initialized");
    expect(await syn.bridgeMinter()).to.equal(await minter.getAddress());
  });

  it("mints only once relayer quorum is reached, and only exactly what was approved", async function () {
    const { syn, minter, relayerA, relayerB, recipient } = await deployFixture();
    await syn.initializeBridgeMinter(await minter.getAddress());
    await minter.unpause();

    const sourceEventId = ethers.keccak256(ethers.toUtf8Bytes("native-lock-1"));
    const amount = ethers.parseUnits("500", 18);

    await expect(
      minter.connect(relayerA).approveMint(sourceEventId, recipient.address, amount)
    ).to.emit(minter, "MintApproved");
    expect(await syn.balanceOf(recipient.address)).to.equal(0);
    expect(await syn.totalSupply()).to.equal(0);

    await expect(
      minter.connect(relayerB).approveMint(sourceEventId, recipient.address, amount)
    )
      .to.emit(minter, "SynMinted")
      .withArgs(anyValue, sourceEventId, recipient.address, amount);

    expect(await syn.balanceOf(recipient.address)).to.equal(amount);
    expect(await syn.totalSupply()).to.equal(amount);
  });

  it("blocks duplicate relayer approvals, outsider approvals, and replayed mints", async function () {
    const { syn, minter, relayerA, relayerB, recipient, outsider } = await deployFixture();
    await syn.initializeBridgeMinter(await minter.getAddress());
    await minter.unpause();

    const sourceEventId = ethers.keccak256(ethers.toUtf8Bytes("native-lock-2"));
    const amount = ethers.parseUnits("40", 18);

    await expect(
      minter.connect(outsider).approveMint(sourceEventId, recipient.address, amount)
    ).to.be.revertedWith("not relayer");

    await minter.connect(relayerA).approveMint(sourceEventId, recipient.address, amount);
    await expect(
      minter.connect(relayerA).approveMint(sourceEventId, recipient.address, amount)
    ).to.be.revertedWith("already approved");

    await minter.connect(relayerB).approveMint(sourceEventId, recipient.address, amount);
    await expect(
      minter.connect(relayerA).approveMint(sourceEventId, recipient.address, amount)
    ).to.be.revertedWith("already processed");

    expect(await syn.totalSupply()).to.equal(amount);
  });

  it("lets any holder burn their own wrapped SYN back toward the native chain, and only their own", async function () {
    const { syn, minter, relayerA, relayerB, user, outsider } = await deployFixture();
    await syn.initializeBridgeMinter(await minter.getAddress());
    await minter.unpause();

    const amount = ethers.parseUnits("300", 18);
    const sourceEventId = ethers.keccak256(ethers.toUtf8Bytes("fund-user"));
    await minter.connect(relayerA).approveMint(sourceEventId, user.address, amount);
    await minter.connect(relayerB).approveMint(sourceEventId, user.address, amount);
    expect(await syn.balanceOf(user.address)).to.equal(amount);

    const burnAmount = ethers.parseUnits("120", 18);
    const nativeRecipient = ethers.toUtf8Bytes("0xnative-recipient-address");

    await expect(
      minter.connect(outsider).burnToNative(burnAmount, nativeRecipient)
    ).to.be.revertedWith("ERC20: burn amount exceeds balance");

    await expect(minter.connect(user).burnToNative(burnAmount, nativeRecipient))
      .to.emit(minter, "SynBurnedForNativeRelease")
      .withArgs(anyValue, user.address, ethers.hexlify(nativeRecipient), burnAmount, 1);

    expect(await syn.balanceOf(user.address)).to.equal(amount - burnAmount);
    expect(await syn.totalSupply()).to.equal(amount - burnAmount);
  });

  it("enforces per-mint and epoch mint limits set by the owner", async function () {
    const { syn, minter, relayerA, relayerB, recipient } = await deployFixture();
    await syn.initializeBridgeMinter(await minter.getAddress());
    await minter.setMintLimits(ethers.parseUnits("100", 18), ethers.parseUnits("120", 18), 86400);
    await minter.unpause();

    const overLimit = ethers.keccak256(ethers.toUtf8Bytes("over-limit"));
    await expect(
      minter.connect(relayerA).approveMint(overLimit, recipient.address, ethers.parseUnits("101", 18))
    ).to.be.revertedWith("mint amount exceeds limit");

    const first = ethers.keccak256(ethers.toUtf8Bytes("epoch-1"));
    const second = ethers.keccak256(ethers.toUtf8Bytes("epoch-2"));
    await minter.connect(relayerA).approveMint(first, recipient.address, ethers.parseUnits("100", 18));
    await minter.connect(relayerB).approveMint(first, recipient.address, ethers.parseUnits("100", 18));

    await minter.connect(relayerA).approveMint(second, recipient.address, ethers.parseUnits("50", 18));
    await expect(
      minter.connect(relayerB).approveMint(second, recipient.address, ethers.parseUnits("50", 18))
    ).to.be.revertedWith("epoch mint limit exceeded");
  });

  it("prevents the owner from setting an impossible relayer threshold", async function () {
    const { minter, relayerB, relayerC } = await deployFixture();

    await expect(minter.setThreshold(4)).to.be.revertedWith("threshold exceeds relayers");

    await minter.setRelayer(relayerC.address, false);
    await expect(minter.setRelayer(relayerB.address, false)).to.be.revertedWith(
      "threshold exceeds relayers"
    );
  });

  it("starts paused, blocking both mint approval side-effects and burns until explicitly opened", async function () {
    const { syn, minter, relayerA, relayerB, user } = await deployFixture();
    await syn.initializeBridgeMinter(await minter.getAddress());

    const sourceEventId = ethers.keccak256(ethers.toUtf8Bytes("while-paused"));
    await expect(
      minter.connect(relayerA).approveMint(sourceEventId, user.address, 1)
    ).to.be.revertedWith("Pausable: paused");
    await expect(
      minter.connect(user).burnToNative(1, ethers.toUtf8Bytes("dest"))
    ).to.be.revertedWith("Pausable: paused");
  });
});
