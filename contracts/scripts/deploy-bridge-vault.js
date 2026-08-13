const fs = require("fs");
const path = require("path");
const { ethers, network } = require("hardhat");

function parseAddresses(value, name) {
  const addresses = (value || "")
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean);
  if (addresses.length === 0) {
    throw new Error(`${name} is required as a comma-separated address list`);
  }
  for (const address of addresses) {
    if (!ethers.isAddress(address)) {
      throw new Error(`${name} contains invalid address: ${address}`);
    }
  }
  return addresses;
}

function parseUint(value, name) {
  const parsed = Number(value || "0");
  if (!Number.isSafeInteger(parsed) || parsed <= 0) {
    throw new Error(`${name} must be a positive integer`);
  }
  return parsed;
}

async function main() {
  const [deployer] = await ethers.getSigners();
  const relayers = parseAddresses(process.env.BRIDGE_RELAYERS, "BRIDGE_RELAYERS");
  const threshold = parseUint(process.env.BRIDGE_THRESHOLD, "BRIDGE_THRESHOLD");
  const asset = process.env.BRIDGE_ASSET;
  const remoteChainId = process.env.BRIDGE_REMOTE_CHAIN_ID;
  const remoteConfirmations = Number(process.env.BRIDGE_REMOTE_CONFIRMATIONS || "12");

  console.log(`Deploying SYNTHOSBridgeVault to ${network.name}`);
  console.log(`Deployer: ${deployer.address}`);
  console.log(`Relayers: ${relayers.join(", ")}`);
  console.log(`Threshold: ${threshold}`);

  const Bridge = await ethers.getContractFactory("SYNTHOSBridgeVault");
  const bridge = await Bridge.deploy(relayers, threshold);
  await bridge.waitForDeployment();

  const bridgeAddress = await bridge.getAddress();
  console.log(`Bridge vault: ${bridgeAddress}`);
  console.log("Bridge is paused by default.");

  if (asset) {
    if (!ethers.isAddress(asset)) {
      throw new Error(`BRIDGE_ASSET is invalid: ${asset}`);
    }
    await (await bridge.setAssetSupported(asset, true)).wait();
    console.log(`Supported asset enabled: ${asset}`);
  }

  if (remoteChainId) {
    await (
      await bridge.setChain(
        BigInt(remoteChainId),
        true,
        Number.isFinite(remoteConfirmations) ? remoteConfirmations : 12
      )
    ).wait();
    console.log(`Remote chain enabled: ${remoteChainId}`);
  }

  const deployment = {
    network: network.name,
    chainId: network.config.chainId,
    bridgeVault: bridgeAddress,
    deployer: deployer.address,
    relayers,
    threshold,
    pausedByDefault: true,
    asset: asset || "",
    remoteChainId: remoteChainId || "",
    remoteConfirmations: remoteChainId ? remoteConfirmations : "",
    deployedAt: new Date().toISOString(),
  };

  const deploymentsDir = path.join(__dirname, "..", "deployments");
  fs.mkdirSync(deploymentsDir, { recursive: true });
  const file = path.join(
    deploymentsDir,
    `bridge-${network.name}-${Date.now()}.json`
  );
  fs.writeFileSync(file, `${JSON.stringify(deployment, null, 2)}\n`);
  console.log(`Deployment written: ${file}`);
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
