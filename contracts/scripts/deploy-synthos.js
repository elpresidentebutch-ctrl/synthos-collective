const fs = require("fs");
const path = require("path");
const hre = require("hardhat");

const { ethers, network } = hre;

const FOUNDER_RELEASE_TIMESTAMPS = [
  1811548800, 1843171200, 1874707200, 1906243200, 1937779200,
  1969401600, 2000937600, 2032473600, 2064009600, 2095632000,
];

const LOCAL_DEX_POOLS = [
  { symbol: "B12", name: "B12 Test Asset", syn: "10000000", asset: "50000" },
  { symbol: "NGOT", name: "NGOT Test Asset", syn: "5000000", asset: "100000" },
  { symbol: "MOMENTUM", name: "Momentum Test Asset", syn: "2000000", asset: "10000" },
];

async function deployedAddress(contract) {
  if (typeof contract.getAddress === "function") {
    return contract.getAddress();
  }
  return contract.address;
}

async function waitForDeploy(contract) {
  if (typeof contract.waitForDeployment === "function") {
    await contract.waitForDeployment();
    return;
  }
  await contract.deployed();
}

async function deployContract(name, args = []) {
  const factory = await ethers.getContractFactory(name);
  const contract = await factory.deploy(...args);
  await waitForDeploy(contract);
  const address = await deployedAddress(contract);
  console.log(`${name}: ${address}`);
  return { contract, address };
}

function dexPoolConfig() {
  if (process.env.DEX_POOLS_JSON) {
    return JSON.parse(process.env.DEX_POOLS_JSON);
  }
  if (network.name === "hardhat" || network.name === "localhost") {
    return LOCAL_DEX_POOLS;
  }
  return [];
}

function envBool(name, fallback) {
  if (process.env[name] === undefined) return fallback;
  return process.env[name] === "true";
}

function parseAddressList(value) {
  return (value || "")
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean);
}

function parsePaymentAssets(value) {
  if (!value) return [];
  const parsed = JSON.parse(value);
  if (!Array.isArray(parsed)) {
    throw new Error("EARLY_ADOPTER_PAYMENT_ASSETS_JSON must be an array");
  }
  return parsed;
}

function requireBytes32(value, label) {
  if (!/^0x[a-fA-F0-9]{64}$/.test(value || "")) {
    throw new Error(`${label} must be a bytes32 hex string`);
  }
}

function adopterMerkleConfig() {
  const envRoot = process.env.ADOPTER_MERKLE_ROOT;
  if (envRoot) {
    requireBytes32(envRoot, "ADOPTER_MERKLE_ROOT");
    return {
      root: envRoot,
      gateRequired: envBool("ADOPTER_MERKLE_GATE_REQUIRED", envRoot !== ethers.ZeroHash),
      source: "ADOPTER_MERKLE_ROOT",
      count: null,
    };
  }

  const merkleFile = path.resolve(
    __dirname,
    "..",
    process.env.ADOPTER_MERKLE_FILE || "merkle/adopter-merkle.json"
  );
  if (fs.existsSync(merkleFile)) {
    const parsed = JSON.parse(fs.readFileSync(merkleFile, "utf8"));
    const root = parsed.merkleRoot || parsed.root;
    requireBytes32(root, merkleFile);
    return {
      root,
      gateRequired: envBool("ADOPTER_MERKLE_GATE_REQUIRED", root !== ethers.ZeroHash),
      source: merkleFile,
      count: parsed.count ?? null,
    };
  }

  const gateRequired = envBool("ADOPTER_MERKLE_GATE_REQUIRED", false);
  if (gateRequired) {
    throw new Error("ADOPTER_MERKLE_GATE_REQUIRED=true but no ADOPTER_MERKLE_ROOT or merkle/adopter-merkle.json was found");
  }

  return {
    root: ethers.ZeroHash,
    gateRequired: false,
    source: "none",
    count: 0,
  };
}

async function main() {
  console.log("Deploying SYNTHOS contracts");
  console.log(`Network: ${network.name}`);

  const signers = await ethers.getSigners();
  const [deployer] = signers;
  console.log(`Deployer: ${deployer.address}`);

  const founderWallet = process.env.FOUNDER_WALLET || deployer.address;
  const founderOpsWallet = process.env.FOUNDER_OPS_WALLET || founderWallet;
  const cmoWallet = process.env.CMO_WALLET || deployer.address;
  const immuneNodeRewardsWallet = process.env.IMMUNE_NODE_REWARDS_WALLET || null;
  const validatorRewardsWallet = process.env.VALIDATOR_REWARDS_WALLET || null;
  const dexLiquidityWallet = process.env.DEX_LIQUIDITY_WALLET || deployer.address;
  const communityWallet = process.env.COMMUNITY_WALLET || deployer.address;
  const configuredTreasuryWallet = process.env.TREASURY_WALLET || null;
  const earlyAdopterSaleAllocation = ethers.parseUnits(
    process.env.EARLY_ADOPTER_SALE_ALLOCATION || "250000000",
    18
  );
  const earlyAdopterMinPurchase = ethers.parseUnits(
    process.env.EARLY_ADOPTER_MIN_SYN_PURCHASE || "20",
    18
  );
  const earlyAdopterMaxPerWallet = ethers.parseUnits(
    process.env.EARLY_ADOPTER_MAX_SYN_PER_WALLET || "100000",
    18
  );

  const activationReward = ethers.parseUnits(process.env.ADOPTER_ACTIVATION_REWARD || "500", 18);
  const heartbeatReward = ethers.parseUnits(process.env.ADOPTER_HEARTBEAT_REWARD || "1000", 18);
  const heartbeatInterval = BigInt(process.env.ADOPTER_HEARTBEAT_INTERVAL || "2592000");
  const maxHeartbeatClaims = BigInt(process.env.ADOPTER_MAX_HEARTBEAT_CLAIMS_PER_OPERATOR || process.env.ADOPTER_MAX_HEARTBEAT_CLAIMS || "120");
  const merkle = adopterMerkleConfig();

  const defaultMultisigOwners = signers.slice(0, Math.min(signers.length, 3)).map((signer) => signer.address);
  const multisigOwners = parseAddressList(process.env.MULTISIG_OWNERS);
  const launchMultisigOwners = multisigOwners.length > 0 ? multisigOwners : defaultMultisigOwners;
  const launchMultisigThreshold = BigInt(
    process.env.MULTISIG_THRESHOLD || (launchMultisigOwners.length >= 2 ? "2" : "1")
  );
  const launchMultisig = await deployContract("SYNTHOSMultisig", [
    launchMultisigOwners,
    launchMultisigThreshold,
  ]);
  console.log(`MULTISIG_OWNERS: ${launchMultisigOwners.join(",")}`);
  console.log(`MULTISIG_THRESHOLD: ${launchMultisigThreshold.toString()}`);

  const treasuryWallet = configuredTreasuryWallet || launchMultisig.address;
  const strategicReserveWallet = process.env.STRATEGIC_RESERVE_WALLET || treasuryWallet;

  const syn = await deployContract("SynCoin");
  const token = syn.contract;
  const earlyAdopterCampaignBudget = await token.COMMUNITY_EARLY_ADOPTER_CAMPAIGNS();
  if (earlyAdopterSaleAllocation > earlyAdopterCampaignBudget) {
    throw new Error("EARLY_ADOPTER_SALE_ALLOCATION exceeds COMMUNITY_EARLY_ADOPTER_CAMPAIGNS");
  }
  const earlyAdopterCampaignReserve = earlyAdopterCampaignBudget - earlyAdopterSaleAllocation;
  const communityOperatingAllocation = (await token.COMMUNITY_ALLOCATION()) - earlyAdopterCampaignBudget;

  if ((await token.treasury()) !== treasuryWallet) {
    const tx = await token.setTreasury(treasuryWallet);
    await tx.wait();
    console.log(`TREASURY_RECYCLING_BURN_TREASURY: ${treasuryWallet}`);
  }

  const timelockMinDelay = BigInt(
    process.env.TIMELOCK_MIN_DELAY || (network.name === "hardhat" ? "60" : "172800")
  );
  const timelock = await deployContract("SYNTHOSTimelock", [
    timelockMinDelay,
    [deployer.address],
    [ethers.ZeroAddress],
    deployer.address,
  ]);

  const governance = await deployContract("SYNTHOSGovernance", [
    syn.address,
    timelock.address,
  ]);

  console.log("Configuring timelock custody");
  const proposerRole = await timelock.contract.PROPOSER_ROLE();
  const adminRole = await timelock.contract.TIMELOCK_ADMIN_ROLE();
  let tx = await timelock.contract.grantRole(proposerRole, governance.address);
  await tx.wait();
  tx = await timelock.contract.revokeRole(proposerRole, deployer.address);
  await tx.wait();
  tx = await timelock.contract.grantRole(adminRole, launchMultisig.address);
  await tx.wait();
  tx = await timelock.contract.renounceRole(adminRole, deployer.address);
  await tx.wait();
  console.log(`TIMELOCK_ADMIN: ${launchMultisig.address}`);
  console.log(`TIMELOCK_PROPOSER: ${governance.address}`);
  console.log("TIMELOCK_EXECUTOR: open");

  const staking = await deployContract("SYNTHOSStaking", [
    syn.address,
    governance.address,
  ]);

  const rewardDistributor = await deployContract("RewardDistributor", [
    governance.address,
  ]);

  const complianceRegistry = await deployContract("SYNTHOSComplianceRegistry");

  const earlyAdopterSale = await deployContract("SYNTHOSEarlyAdopterPresale", [
    syn.address,
    complianceRegistry.address,
    treasuryWallet,
    earlyAdopterSaleAllocation,
    earlyAdopterMinPurchase,
    earlyAdopterMaxPerWallet,
  ]);

  const adopterRewards = await deployContract("SYNTHOSAdopterRewards", [
    syn.address,
    activationReward,
    heartbeatReward,
    heartbeatInterval,
    maxHeartbeatClaims,
  ]);

  const dex = await deployContract("SYNTHOSDex", [syn.address]);

  if (merkle.root !== ethers.ZeroHash || merkle.gateRequired) {
    tx = await adopterRewards.contract.setAdopterMerkleRoot(
      merkle.root,
      merkle.gateRequired
    );
    await tx.wait();
    console.log(`ADOPTER_MERKLE_ROOT: ${merkle.root} gateRequired=${merkle.gateRequired} source=${merkle.source}`);
  }

  const founderVesting = await deployContract("SYNTHOSFounderAnnualVesting", [
    syn.address,
    founderWallet,
    await token.FOUNDER_ANNUAL_RELEASE(),
    FOUNDER_RELEASE_TIMESTAMPS,
  ]);

  console.log("Allocating genesis supply");
  const immuneNodeRewardsRecipient = immuneNodeRewardsWallet || adopterRewards.address;
  const validatorRewardsRecipient = validatorRewardsWallet || staking.address;
  const allocations = [
    [founderVesting.address, await token.FOUNDER_VESTING_ALLOCATION(), "FOUNDER_VESTING"],
    [cmoWallet, await token.CMO_LAUNCH_GRANT(), "CMO_LAUNCH_GRANT"],
    [founderOpsWallet, await token.FOUNDER_OPERATIONS_GRANT(), "FOUNDER_OPERATIONS_GRANT"],
    [immuneNodeRewardsRecipient, await token.IMMUNE_NODE_REWARDS_ALLOCATION(), "IMMUNE_NODE_REWARDS"],
    [validatorRewardsRecipient, await token.VALIDATOR_REWARDS_ALLOCATION(), "VALIDATOR_REWARDS"],
    [earlyAdopterSale.address, earlyAdopterSaleAllocation, "COMMUNITY_EARLY_ADOPTER_SALE_TRANCHE_1"],
    [communityWallet, earlyAdopterCampaignReserve, "COMMUNITY_EARLY_ADOPTER_CAMPAIGN_RESERVE"],
    [communityWallet, communityOperatingAllocation, "COMMUNITY"],
    [treasuryWallet, await token.ECOSYSTEM_TREASURY_ALLOCATION(), "ECOSYSTEM_TREASURY"],
    [strategicReserveWallet, await token.STRATEGIC_RESERVE_ALLOCATION(), "STRATEGIC_RESERVE"],
  ];

  for (const [recipient, amount, label] of allocations) {
    const tx = await token.allocateTokens(recipient, amount, label);
    await tx.wait();
    console.log(`${label}: ${ethers.formatUnits(amount, 18)} SYN -> ${recipient}`);
  }

  console.log("Configuring early adopter crypto sale");
  const earlyAdopterPaymentAssets = parsePaymentAssets(process.env.EARLY_ADOPTER_PAYMENT_ASSETS_JSON);
  for (const asset of earlyAdopterPaymentAssets) {
    if (!asset.address || !asset.usdPrice) {
      throw new Error("Each early adopter payment asset needs address and usdPrice");
    }
    tx = await earlyAdopterSale.contract.setPaymentAsset(
      asset.address,
      asset.enabled !== false,
      ethers.parseUnits(String(asset.usdPrice), 18)
    );
    await tx.wait();
    console.log(`EARLY_ADOPTER_PAYMENT_ASSET ${asset.symbol || asset.address}: ${asset.address} @ $${asset.usdPrice}`);
  }
  if (process.env.EARLY_ADOPTER_NATIVE_PAYMENTS_ENABLED === "true") {
    const nativeUsdPrice = ethers.parseUnits(
      process.env.EARLY_ADOPTER_NATIVE_USD_PRICE || "0",
      18
    );
    tx = await earlyAdopterSale.contract.setNativePaymentConfig(true, nativeUsdPrice);
    await tx.wait();
    console.log(`EARLY_ADOPTER_NATIVE_PAYMENTS: enabled @ $${process.env.EARLY_ADOPTER_NATIVE_USD_PRICE || "0"}`);
  }

  console.log("Configuring DEX pools");
  const dexPools = [];
  const poolConfig = dexPoolConfig();
  let seededSynLiquidity = 0n;

  for (const pool of poolConfig) {
    let assetAddress = pool.address;
    if (!assetAddress) {
      if (!(network.name === "hardhat" || network.name === "localhost")) {
        throw new Error(`DEX pool ${pool.symbol} is missing production asset address`);
      }
      const initialSupply = ethers.parseUnits(pool.asset, pool.decimals || 18);
      const mock = await deployContract("MockERC20", [
        pool.name || `${pool.symbol} Test Asset`,
        pool.symbol,
        deployer.address,
        initialSupply,
      ]);
      assetAddress = mock.address;
    }

    const synAmount = ethers.parseUnits(pool.syn, 18);
    const assetAmount = ethers.parseUnits(pool.asset, pool.decimals || 18);
    seededSynLiquidity += synAmount;

    tx = await token.allocateTokens(deployer.address, synAmount, "LOCKED_DEX_LIQUIDITY");
    await tx.wait();
    tx = await dex.contract.createPool(assetAddress);
    await tx.wait();
    tx = await token.approve(dex.address, synAmount);
    await tx.wait();
    const asset = new ethers.Contract(
      assetAddress,
      ["function approve(address spender, uint256 amount) external returns (bool)"],
      deployer
    );
    tx = await asset.approve(dex.address, assetAmount);
    await tx.wait();
    tx = await dex.contract.addLiquidity(assetAddress, synAmount, assetAmount);
    await tx.wait();

    dexPools.push({
      symbol: pool.symbol,
      asset: assetAddress,
      synLiquidity: pool.syn,
      assetLiquidity: pool.asset,
    });
    console.log(`DEX pool SYN/${pool.symbol}: ${pool.syn} SYN + ${pool.asset} ${pool.symbol}`);
  }

  const remainingDexLiquidity = (await token.LOCKED_DEX_LIQUIDITY_ALLOCATION()) - seededSynLiquidity;
  if (remainingDexLiquidity > 0n) {
    tx = await token.allocateTokens(dexLiquidityWallet, remainingDexLiquidity, "LOCKED_DEX_LIQUIDITY_RESERVE");
    await tx.wait();
    console.log(`LOCKED_DEX_LIQUIDITY_RESERVE: ${ethers.formatUnits(remainingDexLiquidity, 18)} SYN -> ${dexLiquidityWallet}`);
  }

  console.log("Transferring launch contract ownership to timelock");
  const ownableTransfers = [
    ["SynCoin", token],
    ["SYNTHOSAdopterRewards", adopterRewards.contract],
    ["SYNTHOSEarlyAdopterPresale", earlyAdopterSale.contract],
    ["SYNTHOSDex", dex.contract],
    ["SYNTHOSComplianceRegistry", complianceRegistry.contract],
  ];
  for (const [label, contract] of ownableTransfers) {
    if ((await contract.owner()) !== timelock.address) {
      tx = await contract.transferOwnership(timelock.address);
      await tx.wait();
    }
    console.log(`${label}_OWNER: ${timelock.address}`);
  }

  const deployment = {
    network: network.name,
    deployedAt: new Date().toISOString(),
    deployer: deployer.address,
    tokenomics: {
      totalSupply: "100000000000",
      immuneNodeRewards: "22000000000",
      dexLiquidity: "20000000000",
      founderVesting: "17000000000",
      validatorRewards: "12000000000",
      communityAdopterRewards: "12500000000",
      ecosystemTreasury: "10000000000",
      cmoLaunchGrant: "3000000000",
      strategicReserve: "3000000000",
      founderLaunchAllocation: "500000000",
      treasuryRecyclingBurn: {
        protocolSpendBurnShare: "50%",
        protocolSpendTreasuryShare: "50%",
        treasury: treasuryWallet,
        approvedSpendTypes: [
          "PROTOCOL_SPEND",
          "NODE_REGISTRATION",
          "SERVICE_FEE",
          "MARKETPLACE",
        ],
      },
    },
    wallets: {
      multisigOwners: launchMultisigOwners,
      multisigThreshold: launchMultisigThreshold.toString(),
      founderWallet,
      founderOpsWallet,
      cmoWallet,
      immuneNodeRewardsWallet: immuneNodeRewardsRecipient,
      validatorRewardsWallet: validatorRewardsRecipient,
      dexLiquidityWallet,
      communityWallet,
      treasuryWallet,
      strategicReserveWallet,
    },
    contracts: {
      multisig: launchMultisig.address,
      synCoin: syn.address,
      timelock: timelock.address,
      governance: governance.address,
      staking: staking.address,
      rewardDistributor: rewardDistributor.address,
      complianceRegistry: complianceRegistry.address,
      earlyAdopterSale: earlyAdopterSale.address,
      adopterRewards: adopterRewards.address,
      dex: dex.address,
      founderVesting: founderVesting.address,
    },
    dexPools,
    adopterRewards: {
      activationReward: ethers.formatUnits(activationReward, 18),
      heartbeatReward: ethers.formatUnits(heartbeatReward, 18),
      heartbeatIntervalSeconds: heartbeatInterval.toString(),
      maxHeartbeatClaimsPerOperator: maxHeartbeatClaims.toString(),
      merkleRoot: merkle.root,
      merkleGateRequired: merkle.gateRequired,
      merkleSource: merkle.source,
      merkleLeafCount: merkle.count,
    },
    earlyAdopterSale: {
      tokenPriceUsd: "0.10",
      maxTrancheValueUsd: "25000000",
      sourceBucket: "COMMUNITY_EARLY_ADOPTER_CAMPAIGNS",
      allocation: ethers.formatUnits(earlyAdopterSaleAllocation, 18),
      campaignReserve: ethers.formatUnits(earlyAdopterCampaignReserve, 18),
      minSynPurchase: ethers.formatUnits(earlyAdopterMinPurchase, 18),
      maxSynPerWallet: ethers.formatUnits(earlyAdopterMaxPerWallet, 18),
      treasuryWallet,
      paymentAssets: earlyAdopterPaymentAssets,
      nativePaymentsEnabled: process.env.EARLY_ADOPTER_NATIVE_PAYMENTS_ENABLED === "true",
      nativeUsdPrice: process.env.EARLY_ADOPTER_NATIVE_USD_PRICE || "0",
    },
    custody: {
      timelockAdmin: launchMultisig.address,
      timelockProposer: governance.address,
      timelockExecutor: "open",
      ownableContractOwner: timelock.address,
    },
    founderReleaseTimestamps: FOUNDER_RELEASE_TIMESTAMPS,
  };

  const outDir = path.join(__dirname, "..", "deployments");
  fs.mkdirSync(outDir, { recursive: true });
  const file = path.join(outDir, `${network.name}-${Date.now()}.json`);
  fs.writeFileSync(file, JSON.stringify(deployment, null, 2));
  fs.writeFileSync(path.join(outDir, "latest.json"), JSON.stringify(deployment, null, 2));
  fs.writeFileSync(
    path.join(__dirname, "..", "..", "dex-config.json"),
    JSON.stringify({
      network: deployment.network,
      chainId: network.config.chainId ? `0x${Number(network.config.chainId).toString(16)}` : "",
      chainName: deployment.network,
      rpcUrls: network.config.url ? [network.config.url] : [],
      blockExplorerUrls: [],
      contracts: {
        synCoin: deployment.contracts.synCoin,
        dex: deployment.contracts.dex,
      },
      dexPools: deployment.dexPools,
    }, null, 2)
  );

  console.log(`Deployment saved: ${file}`);
  console.log("DEX config saved: ../dex-config.json");
  console.log("SYNTHOS deployment complete");
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
