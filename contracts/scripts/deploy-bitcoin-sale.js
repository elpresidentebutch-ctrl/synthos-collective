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

  const allocateTx = await token.allocateTokens(
    saleAddress,
    allocation,
    "COMMUNITY_EARLY_ADOPTER_BITCOIN_SALE"
  );
  await allocateTx.wait();
  console.log(`Funded sale contract with ${ethers.formatUnits(allocation, 18)} SYN`);

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
