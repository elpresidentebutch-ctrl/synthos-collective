const fs = require("fs");
const path = require("path");
const hre = require("hardhat");

const { ethers, network } = hre;

function requireValue(value, label) {
  if (!value) {
    throw new Error(`missing ${label} in deployment JSON`);
  }
  return value;
}

function assertEq(actual, expected, label) {
  if (actual !== expected) {
    throw new Error(`${label} mismatch: expected ${expected}, got ${actual}`);
  }
  console.log(`ok ${label}`);
}

async function main() {
  const deploymentFile = process.env.DEPLOYMENT_FILE || path.join(
    __dirname,
    "..",
    "deployments",
    "latest.json"
  );
  const deployment = JSON.parse(fs.readFileSync(deploymentFile, "utf8"));

  console.log("SYNTHOS post-deploy check");
  console.log(`Network: ${network.name}`);
  console.log(`Deployment: ${deploymentFile}`);

  const contracts = deployment.contracts || {};
  const wallets = deployment.wallets || {};
  const custody = deployment.custody || {};

  const multisigAddress = requireValue(contracts.multisig, "contracts.multisig");
  const tokenAddress = requireValue(contracts.synCoin, "contracts.synCoin");
  const timelockAddress = requireValue(contracts.timelock, "contracts.timelock");
  const governanceAddress = requireValue(contracts.governance, "contracts.governance");
  const dexAddress = requireValue(contracts.dex, "contracts.dex");

  const multisig = await ethers.getContractAt("SYNTHOSMultisig", multisigAddress);
  const token = await ethers.getContractAt("SynCoin", tokenAddress);
  const timelock = await ethers.getContractAt("SYNTHOSTimelock", timelockAddress);
  const dex = await ethers.getContractAt("SYNTHOSDex", dexAddress);

  const expectedOwners = wallets.multisigOwners || [];
  const actualOwners = await multisig.owners();
  assertEq(actualOwners.length.toString(), expectedOwners.length.toString(), "multisig owner count");
  for (let i = 0; i < expectedOwners.length; i++) {
    assertEq(actualOwners[i].toLowerCase(), expectedOwners[i].toLowerCase(), `multisig owner ${i}`);
  }
  assertEq(
    (await multisig.threshold()).toString(),
    String(wallets.multisigThreshold),
    "multisig threshold"
  );

  const adminRole = await timelock.TIMELOCK_ADMIN_ROLE();
  const proposerRole = await timelock.PROPOSER_ROLE();
  const cancellerRole = await timelock.CANCELLER_ROLE();
  const executorRole = await timelock.EXECUTOR_ROLE();
  assertEq(await timelock.hasRole(adminRole, multisigAddress), true, "timelock admin multisig");
  assertEq(await timelock.hasRole(proposerRole, governanceAddress), true, "timelock proposer governance");
  assertEq(await timelock.hasRole(cancellerRole, governanceAddress), true, "timelock canceller governance");
  assertEq(await timelock.hasRole(executorRole, ethers.ZeroAddress), true, "timelock executor open");

  assertEq((await token.owner()).toLowerCase(), timelockAddress.toLowerCase(), "SynCoin owner");
  assertEq((await token.treasury()).toLowerCase(), String(wallets.treasuryWallet).toLowerCase(), "treasury wallet");

  const spendTypes = [
    ["protocol spend", await token.SPEND_PROTOCOL()],
    ["node registration spend", await token.SPEND_NODE_REGISTRATION()],
    ["service fee spend", await token.SPEND_SERVICE_FEE()],
    ["marketplace spend", await token.SPEND_MARKETPLACE()],
  ];
  for (const [label, spendType] of spendTypes) {
    assertEq(await token.approvedTreasuryRecyclingSpendTypes(spendType), true, label);
  }

  if (contracts.adopterRewards) {
    const adopterRewards = await ethers.getContractAt("SYNTHOSAdopterRewards", contracts.adopterRewards);
    assertEq((await adopterRewards.owner()).toLowerCase(), timelockAddress.toLowerCase(), "adopter rewards owner");
  }
  if (contracts.complianceRegistry) {
    const compliance = await ethers.getContractAt("SYNTHOSComplianceRegistry", contracts.complianceRegistry);
    assertEq((await compliance.owner()).toLowerCase(), timelockAddress.toLowerCase(), "compliance owner");
  }
  assertEq((await dex.owner()).toLowerCase(), timelockAddress.toLowerCase(), "DEX owner");

  const expectedPoolCount = (deployment.dexPools || []).length;
  assertEq((await dex.poolCount()).toString(), String(expectedPoolCount), "DEX pool count");

  if (custody.timelockAdmin) {
    assertEq(custody.timelockAdmin.toLowerCase(), multisigAddress.toLowerCase(), "deployment custody timelock admin");
  }
  if (custody.timelockProposer) {
    assertEq(custody.timelockProposer.toLowerCase(), governanceAddress.toLowerCase(), "deployment custody timelock proposer");
  }

  console.log("SYNTHOS post-deploy check passed.");
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
