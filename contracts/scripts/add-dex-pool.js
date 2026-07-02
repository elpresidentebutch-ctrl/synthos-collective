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

async function main() {
  const file = deploymentPath();
  if (!fs.existsSync(file)) {
    throw new Error(`Deployment file not found: ${file}`);
  }

  const deployment = JSON.parse(fs.readFileSync(file, "utf8"));
  const contracts = deployment.contracts || {};
  if (!contracts.synCoin || !contracts.dex) {
    throw new Error(`Deployment file is missing contracts.synCoin or contracts.dex`);
  }

  const assetAddress = requiredEnv("DEX_ASSET_ADDRESS");
  if (!ethers.isAddress(assetAddress) || assetAddress === ethers.ZeroAddress) {
    throw new Error("DEX_ASSET_ADDRESS must be a real ERC-20 address");
  }
  if (assetAddress.toLowerCase() === contracts.synCoin.toLowerCase()) {
    throw new Error("DEX_ASSET_ADDRESS cannot be the SYN token address");
  }

  const symbol = process.env.DEX_ASSET_SYMBOL || "ASSET";
  const decimals = Number(process.env.DEX_ASSET_DECIMALS || "18");
  const synLiquidity = ethers.parseUnits(requiredEnv("DEX_SYN_LIQUIDITY"), 18);
  const assetLiquidity = ethers.parseUnits(requiredEnv("DEX_ASSET_LIQUIDITY"), decimals);

  const [operator] = await ethers.getSigners();
  const syn = await ethers.getContractAt("SynCoin", contracts.synCoin);
  const dex = await ethers.getContractAt("SYNTHOSDex", contracts.dex);
  const asset = await ethers.getContractAt(
    ["function approve(address spender, uint256 amount) external returns (bool)"],
    assetAddress
  );

  console.log("SYNTHOS DEX pool add");
  console.log(`Network: ${network.name}`);
  console.log(`Operator: ${operator.address}`);
  console.log(`SYN: ${contracts.synCoin}`);
  console.log(`DEX/router: ${contracts.dex}`);
  console.log(`${symbol}: ${assetAddress}`);

  const pool = await dex.pools(assetAddress);
  if (pool.asset === ethers.ZeroAddress) {
    const createTx = await dex.createPool(assetAddress);
    await createTx.wait();
    console.log(`Pool created: SYN/${symbol}`);
  } else {
    console.log(`Pool already exists: SYN/${symbol}`);
  }

  const approveSyn = await syn.approve(contracts.dex, synLiquidity);
  await approveSyn.wait();
  const approveAsset = await asset.approve(contracts.dex, assetLiquidity);
  await approveAsset.wait();

  const addTx = await dex.addLiquidity(assetAddress, synLiquidity, assetLiquidity);
  await addTx.wait();

  const updatedPool = await dex.pools(assetAddress);
  console.log(`Liquidity added: ${ethers.formatUnits(synLiquidity, 18)} SYN + ${ethers.formatUnits(assetLiquidity, decimals)} ${symbol}`);
  console.log(`SYN reserve: ${ethers.formatUnits(updatedPool.synReserve, 18)}`);
  console.log(`${symbol} reserve: ${ethers.formatUnits(updatedPool.assetReserve, decimals)}`);

  const output = {
    network: network.name,
    deploymentFile: file,
    dex: contracts.dex,
    synCoin: contracts.synCoin,
    pool: {
      symbol,
      asset: assetAddress,
      decimals,
      synLiquidity: ethers.formatUnits(synLiquidity, 18),
      assetLiquidity: ethers.formatUnits(assetLiquidity, decimals),
      synReserve: ethers.formatUnits(updatedPool.synReserve, 18),
      assetReserve: ethers.formatUnits(updatedPool.assetReserve, decimals),
    },
  };

  const outDir = path.join(__dirname, "..", "deployments");
  const outFile = path.join(outDir, `dex-pool-${symbol.toLowerCase()}-${Date.now()}.json`);
  fs.writeFileSync(outFile, JSON.stringify(output, null, 2));
  console.log(`Pool record saved: ${outFile}`);
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
