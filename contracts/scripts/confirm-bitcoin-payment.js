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
 * Run by the confirmer after manually verifying a Bitcoin payment on a block
 * explorer (correct address, correct amount, enough confirmations). This
 * script does not check the Bitcoin network itself -- confirming a payment
 * that never happened will release real SYN, so verify off-chain first.
 */
async function main() {
  const file = deploymentPath();
  if (!fs.existsSync(file)) {
    throw new Error(`Deployment file not found: ${file}`);
  }

  const deployment = JSON.parse(fs.readFileSync(file, "utf8"));
  const saleAddress = deployment.contracts?.bitcoinAdopterSale;
  if (!saleAddress) {
    throw new Error("Deployment file is missing contracts.bitcoinAdopterSale");
  }

  const btcTxHash = requiredEnv("BTC_TX_HASH");
  const btcTxId = ethers.id(btcTxHash);
  const satoshis = ethers.getBigInt(requiredEnv("BTC_PAYMENT_SATOSHIS"));
  const beneficiary = requiredEnv("BTC_BENEFICIARY_ADDRESS");
  if (!ethers.isAddress(beneficiary) || beneficiary === ethers.ZeroAddress) {
    throw new Error("BTC_BENEFICIARY_ADDRESS must be a real address");
  }

  const [confirmer] = await ethers.getSigners();
  const sale = await ethers.getContractAt("SYNTHOSBitcoinAdopterSale", saleAddress);

  console.log("SYNTHOS Bitcoin adopter sale: confirm payment");
  console.log(`Network: ${network.name}`);
  console.log(`Confirmer: ${confirmer.address}`);
  console.log(`Bitcoin tx: ${btcTxHash}`);
  console.log(`Satoshis: ${satoshis}`);
  console.log(`Beneficiary: ${beneficiary}`);

  const [quotedSyn] = await sale.quoteBitcoinPurchase(satoshis);
  console.log(`Quote: ${ethers.formatUnits(quotedSyn, 18)} SYN`);

  const tx = await sale.confirmBitcoinPayment(btcTxId, satoshis, beneficiary);
  const receipt = await tx.wait();
  console.log(`Confirmed in tx: ${receipt.hash}`);
  console.log(`Total SYN sold so far: ${ethers.formatUnits(await sale.totalSynSold(), 18)}`);
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
