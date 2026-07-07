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
 * Enables WBTC as a payment asset on the already-deployed early adopter sale
 * contract. WBTC is an ERC-20 token, so it settles through the same
 * buyWithToken() path as USDC/USDT/WETH -- no new contract logic is needed,
 * only this one-time configuration call by the sale contract owner.
 */
async function main() {
  const file = deploymentPath();
  if (!fs.existsSync(file)) {
    throw new Error(`Deployment file not found: ${file}`);
  }

  const deployment = JSON.parse(fs.readFileSync(file, "utf8"));
  const saleAddress = deployment.contracts?.earlyAdopterSale;
  if (!saleAddress) {
    throw new Error("Deployment file is missing contracts.earlyAdopterSale");
  }

  const wbtcAddress = requiredEnv("WBTC_ADDRESS");
  if (!ethers.isAddress(wbtcAddress) || wbtcAddress === ethers.ZeroAddress) {
    throw new Error("WBTC_ADDRESS must be a real ERC-20 address");
  }

  const usdPrice = requiredEnv("WBTC_USD_PRICE");
  const usdPrice18 = ethers.parseUnits(usdPrice, 18);

  const [owner] = await ethers.getSigners();
  const sale = await ethers.getContractAt("SYNTHOSEarlyAdopterSale", saleAddress);

  console.log("SYNTHOS early adopter sale: enable WBTC");
  console.log(`Network: ${network.name}`);
  console.log(`Owner: ${owner.address}`);
  console.log(`Sale contract: ${saleAddress}`);
  console.log(`WBTC: ${wbtcAddress}`);
  console.log(`WBTC/USD price: $${usdPrice}`);

  const tx = await sale.setPaymentAsset(wbtcAddress, true, usdPrice18);
  await tx.wait();

  const asset = await sale.paymentAssets(wbtcAddress);
  console.log(
    `WBTC payment asset enabled: enabled=${asset.enabled} decimals=${asset.decimals} usdPricePerToken18=${asset.usdPricePerToken18}`
  );
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
