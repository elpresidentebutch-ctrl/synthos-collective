const fs = require("fs");
const path = require("path");
const hre = require("hardhat");

const { ethers, network } = hre;
const ZERO_HASH = `0x${"0".repeat(64)}`;

function envBool(name, fallback) {
  if (process.env[name] === undefined) return fallback;
  return process.env[name] === "true";
}

function loadMerkleConfig() {
  const file = path.resolve(
    process.cwd(),
    process.env.ADOPTER_MERKLE_FILE || "merkle/adopter-merkle.json"
  );
  const envRoot = process.env.ADOPTER_MERKLE_ROOT;

  if (envRoot) {
    return {
      root: envRoot,
      gateRequired: envBool("ADOPTER_MERKLE_GATE_REQUIRED", envRoot !== ZERO_HASH),
      source: "ADOPTER_MERKLE_ROOT",
    };
  }

  if (!fs.existsSync(file)) {
    throw new Error(`Merkle file not found: ${file}`);
  }

  const parsed = JSON.parse(fs.readFileSync(file, "utf8"));
  const root = parsed.merkleRoot || parsed.root;
  if (!/^0x[a-fA-F0-9]{64}$/.test(root || "")) {
    throw new Error(`Invalid Merkle root in ${file}`);
  }

  return {
    root,
    gateRequired: envBool("ADOPTER_MERKLE_GATE_REQUIRED", root !== ZERO_HASH),
    source: file,
  };
}

function loadAdopterRewardsAddress() {
  if (process.env.ADOPTER_REWARDS_ADDRESS) {
    return process.env.ADOPTER_REWARDS_ADDRESS;
  }

  const deploymentFile = path.resolve(
    process.cwd(),
    process.env.DEPLOYMENT_FILE || "deployments/latest.json"
  );
  if (!fs.existsSync(deploymentFile)) {
    throw new Error(`Deployment file not found: ${deploymentFile}`);
  }

  const deployment = JSON.parse(fs.readFileSync(deploymentFile, "utf8"));
  const address = deployment?.contracts?.adopterRewards;
  if (!address) {
    throw new Error(`No contracts.adopterRewards in ${deploymentFile}`);
  }
  return address;
}

async function main() {
  const merkle = loadMerkleConfig();
  const adopterRewardsAddress = loadAdopterRewardsAddress();
  const [sender] = await ethers.getSigners();

  console.log(`Network: ${network.name}`);
  console.log(`Sender: ${sender.address}`);
  console.log(`AdopterRewards: ${adopterRewardsAddress}`);
  console.log(`Merkle root: ${merkle.root}`);
  console.log(`Gate required: ${merkle.gateRequired}`);
  console.log(`Source: ${merkle.source}`);

  const adopterRewards = await ethers.getContractAt(
    "SYNTHOSAdopterRewards",
    adopterRewardsAddress
  );
  const tx = await adopterRewards.setAdopterMerkleRoot(
    merkle.root,
    merkle.gateRequired
  );
  await tx.wait();

  console.log(`Set adopter Merkle root on-chain: ${tx.hash}`);
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
