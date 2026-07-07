const { expect } = require("chai");
const { ethers } = require("hardhat");

describe("SYNTHOSBitcoinAdopterSale", function () {
  async function deployFixture() {
    const [owner, confirmer, buyer, outsider] = await ethers.getSigners();

    const SynCoin = await ethers.getContractFactory("SynCoin");
    const syn = await SynCoin.deploy();
    await syn.waitForDeployment();

    const ComplianceRegistry = await ethers.getContractFactory(
      "SYNTHOSComplianceRegistry"
    );
    const compliance = await ComplianceRegistry.deploy();
    await compliance.waitForDeployment();

    const Sale = await ethers.getContractFactory("SYNTHOSBitcoinAdopterSale");
    const sale = await Sale.deploy(
      await syn.getAddress(),
      await compliance.getAddress(),
      confirmer.address,
      ethers.parseUnits("50000000", 18),
      ethers.parseUnits("20", 18),
      ethers.parseUnits("100000", 18)
    );
    await sale.waitForDeployment();

    await syn.allocateTokens(
      await sale.getAddress(),
      ethers.parseUnits("2000000", 18),
      "COMMUNITY_EARLY_ADOPTER_BITCOIN_SALE"
    );

    await compliance.setComplianceRecord(
      buyer.address,
      6, // Community
      true,
      true,
      true,
      false,
      0,
      ethers.keccak256(ethers.toUtf8Bytes("SYNTHOS bitcoin sale disclosure v1")),
      ethers.keccak256(ethers.toUtf8Bytes("US"))
    );

    return { owner, confirmer, buyer, outsider, syn, compliance, sale };
  }

  it("rejects a non-owner setting the BTC/USD price", async function () {
    const { confirmer, sale } = await deployFixture();
    await expect(
      sale.connect(confirmer).setBtcUsdPrice(ethers.parseUnits("60000", 18))
    ).to.be.revertedWith("Ownable: caller is not the owner");
  });

  it("delivers SYN once the confirmer records a verified Bitcoin payment", async function () {
    const { owner, confirmer, buyer, syn, sale } = await deployFixture();

    await sale.connect(owner).setBtcUsdPrice(ethers.parseUnits("60000", 18));

    // 0.01 BTC at $60,000/BTC = $600 -> 6,000 SYN at $0.10/SYN
    const satoshis = 1_000_000n; // 0.01 BTC
    const btcTxHash = "b1a2c3d4e5f60718293a4b5c6d7e8f9012345678901234567890abcdef01234";
    const btcTxId = ethers.id(btcTxHash);

    const [quotedSyn] = await sale.quoteBitcoinPurchase(satoshis);
    expect(quotedSyn).to.equal(ethers.parseUnits("6000", 18));

    await expect(
      sale.connect(confirmer).confirmBitcoinPayment(btcTxId, satoshis, buyer.address)
    )
      .to.emit(sale, "BitcoinPurchaseConfirmed")
      .withArgs(btcTxId, buyer.address, satoshis, ethers.parseUnits("6000", 18), ethers.parseUnits("600", 18));

    expect(await syn.balanceOf(buyer.address)).to.equal(ethers.parseUnits("6000", 18));
    expect(await sale.totalSynSold()).to.equal(ethers.parseUnits("6000", 18));
    expect(await sale.purchasedByWallet(buyer.address)).to.equal(ethers.parseUnits("6000", 18));
    expect(await sale.processedBtcTx(btcTxId)).to.equal(true);
  });

  it("blocks a non-confirmer from releasing SYN", async function () {
    const { owner, outsider, buyer, sale } = await deployFixture();
    await sale.connect(owner).setBtcUsdPrice(ethers.parseUnits("60000", 18));

    const btcTxId = ethers.id("someone-elses-tx");
    await expect(
      sale.connect(outsider).confirmBitcoinPayment(btcTxId, 1_000_000n, buyer.address)
    ).to.be.revertedWith("not confirmer");
  });

  it("blocks crediting the same Bitcoin transaction twice", async function () {
    const { owner, confirmer, buyer, sale } = await deployFixture();
    await sale.connect(owner).setBtcUsdPrice(ethers.parseUnits("60000", 18));

    const btcTxId = ethers.id("replayed-tx");
    await sale.connect(confirmer).confirmBitcoinPayment(btcTxId, 1_000_000n, buyer.address);

    await expect(
      sale.connect(confirmer).confirmBitcoinPayment(btcTxId, 1_000_000n, buyer.address)
    ).to.be.revertedWith("btc tx already processed");
  });

  it("blocks an ineligible beneficiary", async function () {
    const { owner, confirmer, outsider, sale } = await deployFixture();
    await sale.connect(owner).setBtcUsdPrice(ethers.parseUnits("60000", 18));

    const btcTxId = ethers.id("ineligible-tx");
    await expect(
      sale.connect(confirmer).confirmBitcoinPayment(btcTxId, 1_000_000n, outsider.address)
    ).to.be.revertedWith("buyer not eligible");
  });

  it("enforces the wallet cap across repeated confirmations", async function () {
    const { owner, confirmer, buyer, sale } = await deployFixture();
    // $200,000/BTC so 1 BTC quotes to 2,000,000 SYN, comfortably over the 100,000 cap.
    await sale.connect(owner).setBtcUsdPrice(ethers.parseUnits("200000", 18));

    const btcTxId = ethers.id("wallet-cap-tx");
    await expect(
      sale.connect(confirmer).confirmBitcoinPayment(btcTxId, SATOSHIS_PER_BTC(1), buyer.address)
    ).to.be.revertedWith("wallet cap exceeded");
  });

  it("lets the owner rotate the confirmer and pause confirmations", async function () {
    const { owner, confirmer, buyer, outsider, sale } = await deployFixture();
    await sale.connect(owner).setBtcUsdPrice(ethers.parseUnits("60000", 18));

    await sale.connect(owner).setConfirmer(outsider.address);
    await expect(
      sale.connect(confirmer).confirmBitcoinPayment(ethers.id("post-rotation-tx"), 1_000_000n, buyer.address)
    ).to.be.revertedWith("not confirmer");

    await sale.connect(owner).pause();
    await expect(
      sale.connect(outsider).confirmBitcoinPayment(ethers.id("paused-tx"), 1_000_000n, buyer.address)
    ).to.be.revertedWith("Pausable: paused");
  });

  it("lets the owner recover unsold SYN", async function () {
    const { owner, syn, sale } = await deployFixture();
    await expect(
      sale.connect(owner).withdrawUnsoldSyn(owner.address, ethers.parseUnits("10", 18))
    ).to.changeTokenBalances(
      syn,
      [await sale.getAddress(), owner.address],
      [-ethers.parseUnits("10", 18), ethers.parseUnits("10", 18)]
    );
  });

  function SATOSHIS_PER_BTC(btc) {
    return BigInt(btc) * 100_000_000n;
  }
});
