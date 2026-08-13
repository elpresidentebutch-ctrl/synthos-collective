const { expect } = require("chai");
const { ethers } = require("hardhat");

describe("SYNTHOSBridgeVault", function () {
  const DESTINATION_CHAIN_ID = 20260702n;
  const SOURCE_CHAIN_ID = 84532n;
  const MIN_CONFIRMATIONS = 12;

  async function deployBridgeFixture() {
    const [owner, relayerA, relayerB, relayerC, user, recipient, outsider] =
      await ethers.getSigners();

    const MockERC20 = await ethers.getContractFactory("MockERC20");
    const token = await MockERC20.deploy(
      "SYNTHOS",
      "SYN",
      owner.address,
      ethers.parseEther("1000000")
    );
    await token.waitForDeployment();

    const Bridge = await ethers.getContractFactory("SYNTHOSBridgeVault");
    const bridge = await Bridge.deploy(
      [relayerA.address, relayerB.address, relayerC.address],
      2
    );
    await bridge.waitForDeployment();

    await bridge.setAssetSupported(await token.getAddress(), true);
    await bridge.setChain(DESTINATION_CHAIN_ID, true, MIN_CONFIRMATIONS);
    await bridge.setChain(SOURCE_CHAIN_ID, true, MIN_CONFIRMATIONS);

    await token.transfer(user.address, ethers.parseEther("1000"));
    await token.transfer(await bridge.getAddress(), ethers.parseEther("5000"));

    return {
      owner,
      relayerA,
      relayerB,
      relayerC,
      user,
      recipient,
      outsider,
      token,
      bridge,
    };
  }

  it("starts paused and rejects bridge actions until explicitly opened", async function () {
    const { bridge, token, user } = await deployBridgeFixture();
    await token.connect(user).approve(await bridge.getAddress(), ethers.parseEther("1"));

    await expect(
      bridge
        .connect(user)
        .lock(
          await token.getAddress(),
          ethers.parseEther("1"),
          DESTINATION_CHAIN_ID,
          ethers.toUtf8Bytes("syn1receiver")
        )
    ).to.be.revertedWith("Pausable: paused");
  });

  it("locks approved assets and emits a unique bridge event", async function () {
    const { bridge, token, user } = await deployBridgeFixture();
    await bridge.unpause();

    const amount = ethers.parseEther("25");
    await token.connect(user).approve(await bridge.getAddress(), amount);

    await expect(
      bridge
        .connect(user)
        .lock(
          await token.getAddress(),
          amount,
          DESTINATION_CHAIN_ID,
          ethers.toUtf8Bytes("syn1receiver")
        )
    )
      .to.emit(bridge, "BridgeLocked")
      .withArgs(
        anyValue,
        31337,
        DESTINATION_CHAIN_ID,
        await token.getAddress(),
        user.address,
        ethers.toUtf8Bytes("syn1receiver"),
        amount,
        1
      );

    expect(await token.balanceOf(await bridge.getAddress())).to.equal(
      ethers.parseEther("5025")
    );
    expect(await bridge.outboundNonce()).to.equal(1);
  });

  it("rejects unsupported assets and disabled destination chains", async function () {
    const { bridge, user, owner } = await deployBridgeFixture();
    const MockERC20 = await ethers.getContractFactory("MockERC20");
    const fake = await MockERC20.deploy(
      "Fake",
      "FAKE",
      user.address,
      ethers.parseEther("100")
    );
    await fake.waitForDeployment();

    await bridge.unpause();
    await fake.connect(user).approve(await bridge.getAddress(), ethers.parseEther("1"));

    await expect(
      bridge
        .connect(user)
        .lock(
          await fake.getAddress(),
          ethers.parseEther("1"),
          DESTINATION_CHAIN_ID,
          ethers.toUtf8Bytes("syn1receiver")
        )
    ).to.be.revertedWith("unsupported asset");

    await bridge.connect(owner).setAssetSupported(await fake.getAddress(), true);
    await expect(
      bridge
        .connect(user)
        .lock(
          await fake.getAddress(),
          ethers.parseEther("1"),
          999n,
          ethers.toUtf8Bytes("syn1receiver")
        )
    ).to.be.revertedWith("destination chain disabled");
  });

  it("requires relayer quorum before releasing locked liquidity", async function () {
    const { bridge, token, relayerA, relayerB, recipient } =
      await deployBridgeFixture();
    await bridge.unpause();

    const sourceEventId = ethers.keccak256(ethers.toUtf8Bytes("source-lock-1"));
    const amount = ethers.parseEther("100");

    await expect(
      bridge
        .connect(relayerA)
        .approveRelease(
          sourceEventId,
          SOURCE_CHAIN_ID,
          await token.getAddress(),
          recipient.address,
          amount
        )
    ).to.emit(bridge, "ReleaseApproved");

    expect(await token.balanceOf(recipient.address)).to.equal(0);

    await expect(
      bridge
        .connect(relayerB)
        .approveRelease(
          sourceEventId,
          SOURCE_CHAIN_ID,
          await token.getAddress(),
          recipient.address,
          amount
        )
    ).to.emit(bridge, "BridgeReleased");

    expect(await token.balanceOf(recipient.address)).to.equal(amount);
  });

  it("blocks duplicate approvals, outsider approvals, and replayed releases", async function () {
    const { bridge, token, relayerA, relayerB, recipient, outsider } =
      await deployBridgeFixture();
    await bridge.unpause();

    const sourceEventId = ethers.keccak256(ethers.toUtf8Bytes("source-lock-2"));
    const amount = ethers.parseEther("40");

    await expect(
      bridge
        .connect(outsider)
        .approveRelease(
          sourceEventId,
          SOURCE_CHAIN_ID,
          await token.getAddress(),
          recipient.address,
          amount
        )
    ).to.be.revertedWith("not relayer");

    await bridge
      .connect(relayerA)
      .approveRelease(
        sourceEventId,
        SOURCE_CHAIN_ID,
        await token.getAddress(),
        recipient.address,
        amount
      );

    await expect(
      bridge
        .connect(relayerA)
        .approveRelease(
          sourceEventId,
          SOURCE_CHAIN_ID,
          await token.getAddress(),
          recipient.address,
          amount
        )
    ).to.be.revertedWith("already approved");

    await bridge
      .connect(relayerB)
      .approveRelease(
        sourceEventId,
        SOURCE_CHAIN_ID,
        await token.getAddress(),
        recipient.address,
        amount
      );

    await expect(
      bridge
        .connect(relayerA)
        .approveRelease(
          sourceEventId,
          SOURCE_CHAIN_ID,
          await token.getAddress(),
          recipient.address,
          amount
        )
    ).to.be.revertedWith("already processed");
  });

  it("prevents owner from setting an impossible relayer threshold", async function () {
    const { bridge, relayerB, relayerC } = await deployBridgeFixture();

    await expect(bridge.setThreshold(4)).to.be.revertedWith(
      "threshold exceeds relayers"
    );

    await bridge.setRelayer(relayerC.address, false);
    await expect(bridge.setRelayer(relayerB.address, false)).to.be.revertedWith(
      "threshold exceeds relayers"
    );
  });
});

const { anyValue } = require("@nomicfoundation/hardhat-chai-matchers/withArgs");
