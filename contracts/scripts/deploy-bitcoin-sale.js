const fs = require("fs");
const path = require("path");
const hre = require("hardhat");

const { ethers, network } = hre;

function requiredEnv(name) {
  const value = process.env[name];
  if (!value) {
    throw new Error(`${name} is required`);
  }
  return value;
}

function deploymentPath() {
  return path.resolve(
    __dirname,
    "..",
    process.env.DEPLOYMENT_FILE || "deployments/latest.json"
  );
}

/**
 * Deploys SYNTHOSBitcoinAdopterSale and funds it from the same
 * COMMUNITY_EARLY_ADOPTER_CAMPAIGNS bucket the crypto-native early adopter
 * sale draws from. Run deploy-synthos.js first so synCoin and
 * complianceRegistry already exist.
 */
async function main() {
  const file = deploymentPath();
  if (!fs.existsSync(file)) {
    throw new Error(`Deployment file not found: ${file}`);
  }

  const deployment = JSON.parse(fs.readFileSync(file, "utf8"));
  const contracts = deployment.contracts || {};
  if (!contracts.synCoin || !contracts.complianceRegistry) {
    throw new Error("Deployment file is missing contracts.synCoin or contracts.complianceRegistry");
  }
  const isLocalNetwork = network.name === "hardhat" || network.name === "localhost";
  // deploy-synthos.js transfers ownership of every other launch Ownable
  // (SynCoin, the bridge minter, the DEX, the early adopter sale, etc.) to
  // its timelock before finishing -- this contract's owner controls
  // setConfirmer, setBtcUsdPrice, pause/unpause, and critically
  // withdrawUnsoldSyn (can move the entire unsold allocation to any
  // address), so leaving it on the deploy key instead of the same timelock
  // would be a real gap, not just an inconsistency. Required on a real
  // network; on hardhat/localhost dev deploys with no MULTISIG_OWNERS/
  // timelock ceremony run yet, contracts.timelock may legitimately be
  // absent, so that case is allowed to fall back to the deployer.
  if (!contracts.timelock && !isLocalNetwork) {
    throw new Error(
      "Deployment file is missing contracts.timelock -- run deploy-synthos.js's full timelock/governance " +
      "setup first so this sale's ownership has a real custody target to transfer to"
    );
  }
  if (isLocalNetwork && !contracts.synBridgeMinter) {
    throw new Error("Deployment file is missing contracts.synBridgeMinter (needed to bridge-mint on a local network)");
  }

  const confirmerAddress = requiredEnv("BITCOIN_SALE_CONFIRMER");
  if (!ethers.isAddress(confirmerAddress) || confirmerAddress === ethers.ZeroAddress) {
    throw new Error("BITCOIN_SALE_CONFIRMER must be a real address");
  }

  const allocation = ethers.parseUnits(process.env.BITCOIN_SALE_ALLOCATION || "50000000", 18);
  const minSynPurchase = ethers.parseUnits(process.env.BITCOIN_SALE_MIN_SYN_PURCHASE || "20", 18);
  const maxSynPerWallet = ethers.parseUnits(process.env.BITCOIN_SALE_MAX_SYN_PER_WALLET || "100000", 18);

  const [deployer] = await ethers.getSigners();
  const token = await ethers.getContractAt("SynCoin", contracts.synCoin);

  console.log("SYNTHOS Bitcoin adopter sale deploy");
  console.log(`Network: ${network.name}`);
  console.log(`Deployer: ${deployer.address}`);
  console.log(`Confirmer: ${confirmerAddress}`);
  console.log(`Allocation: ${ethers.formatUnits(allocation, 18)} SYN`);

  const Sale = await ethers.getContractFactory("SYNTHOSBitcoinAdopterSale");
  const sale = await Sale.deploy(
    contracts.synCoin,
    contracts.complianceRegistry,
    confirmerAddress,
    allocation,
    minSynPurchase,
    maxSynPerWallet
  );
  await sale.waitForDeployment();
  const saleAddress = await sale.getAddress();
  console.log(`SYNTHOSBitcoinAdopterSale: ${saleAddress}`);

  // SynCoin is a bridge-pegged wrapper: nothing can allocate it into
  // existence anymore. On a local network this script controls the sole
  // dev relayer, so it can bridge-mint the allocation directly to the sale
  // contract (dev convenience only). On a real network the allocation has
  // to already be sitting in the deployer's wallet, bridge-minted in ahead
  // of time against a real native-chain lock, and this script just moves it.
  if (isLocalNetwork) {
    const minter = await ethers.getContractAt("SYNTHOSSynBridgeMinter", contracts.synBridgeMinter);
    if (await minter.paused()) {
      await (await minter.unpause()).wait();
    }
    const sourceEventId = ethers.keccak256(ethers.toUtf8Bytes(`local-bitcoin-sale-funding-${Date.now()}`));
    const mintTx = await minter.approveMint(sourceEventId, saleAddress, allocation);
    await mintTx.wait();
    console.log(`Funded sale contract with ${ethers.formatUnits(allocation, 18)} SYN (bridge-minted, dev convenience)`);
  } else {
    const deployerBalance = await token.balanceOf(deployer.address);
    if (deployerBalance < allocation) {
      throw new Error(
        `Deployer wallet does not hold enough bridge-minted SYN to fund this sale ` +
        `(needs ${ethers.formatUnits(allocation, 18)} SYN, has ${ethers.formatUnits(deployerBalance, 18)}). ` +
        "Bridge-mint it in first (real relayer quorum over a real native-chain lock), then re-run."
      );
    }
    const transferTx = await token.transfer(saleAddress, allocation);
    await transferTx.wait();
    console.log(`Funded sale contract with ${ethers.formatUnits(allocation, 18)} SYN (transferred from deployer's bridge-minted balance)`);
  }

  // Ownable defaults the deployer as owner. Left as-is, the deploy key --
  // not a multisig-guarded timelock -- would permanently control
  // setConfirmer, setBtcUsdPrice, pause/unpause, and withdrawUnsoldSyn (able
  // to move the entire unsold allocation anywhere). Match every other
  // launch Ownable in deploy-synthos.js and hand ownership to the timelock.
  let saleOwner = deployer.address;
  if (contracts.timelock) {
    const transferOwnershipTx = await sale.transferOwnership(contracts.timelock);
    await transferOwnershipTx.wait();
    saleOwner = contracts.timelock;
    console.log(`SYNTHOSBitcoinAdopterSale ownership transferred to timelock: ${contracts.timelock}`);
  } else {
    console.log(
      "WARNING: no contracts.timelock in the deployment file -- SYNTHOSBitcoinAdopterSale ownership " +
      `left on the deployer (${deployer.address}). This is only acceptable on a local/dev deploy; ` +
      "transfer it to real custody before this sale ever touches a real network."
    );
  }

  const output = {
    network: network.name,
    deployedAt: new Date().toISOString(),
    deployer: deployer.address,
    contracts: {
      synCoin: contracts.synCoin,
      complianceRegistry: contracts.complianceRegistry,
      bitcoinAdopterSale: saleAddress,
    },
    bitcoinAdopterSale: {
      tokenPriceUsd: "0.10",
      confirmer: confirmerAddress,
      owner: saleOwner,
      allocation: ethers.formatUnits(allocation, 18),
      minSynPurchase: ethers.formatUnits(minSynPurchase, 18),
      maxSynPerWallet: ethers.formatUnits(maxSynPerWallet, 18),
    },
  };

  const outDir = path.join(__dirname, "..", "deployments");
  const outFile = path.join(outDir, `bitcoin-sale-${network.name}-${Date.now()}.json`);
  fs.writeFileSync(outFile, JSON.stringify(output, null, 2));
  console.log(`Deployment record saved: ${outFile}`);

  deployment.contracts.bitcoinAdopterSale = saleAddress;
  deployment.bitcoinAdopterSale = output.bitcoinAdopterSale;
  fs.writeFileSync(file, JSON.stringify(deployment, null, 2));
  console.log(`Updated ${file} with contracts.bitcoinAdopterSale`);
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
