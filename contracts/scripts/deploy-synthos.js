const fs = require("fs");
const path = require("path");
const hre = require("hardhat");
const { BUCKETS_SYN, TOTAL_SUPPLY_SYN, assertBucketsSumToTotal } = require("../tokenomics");

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

  assertBucketsSumToTotal();
  const earlyAdopterCampaignBudget = ethers.parseUnits(BUCKETS_SYN.COMMUNITY, 18) / 5n; // documented sub-bucket, see note below
  // NOTE: COMMUNITY_EARLY_ADOPTER_CAMPAIGNS is a native-chain-side sub-bucket
  // of the broader COMMUNITY allocation. It is not read from this contract
  // (SynCoin no longer carries tokenomics bucket bookkeeping at all -- see
  // tokenomics.js for why), so treat this constant as documentation to be
  // kept in sync with the native chain's genesis allocation, not as an
  // on-chain-enforced figure the way it used to be.
  if (earlyAdopterSaleAllocation > earlyAdopterCampaignBudget) {
    throw new Error("EARLY_ADOPTER_SALE_ALLOCATION exceeds the documented COMMUNITY_EARLY_ADOPTER_CAMPAIGNS budget");
  }
  const earlyAdopterCampaignReserve = earlyAdopterCampaignBudget - earlyAdopterSaleAllocation;
  const communityOperatingAllocation = ethers.parseUnits(BUCKETS_SYN.COMMUNITY, 18) - earlyAdopterCampaignBudget;

  // SynCoin is a bridge-pegged wrapper: it starts at zero supply and can
  // only ever be minted by the SYNTHOSSynBridgeMinter contract, wired up
  // once, immediately below, and never changeable again.
  const syn = await deployContract("SynCoin", [treasuryWallet]);
  const token = syn.contract;

  const isLocalBridgeNetwork = network.name === "hardhat" || network.name === "localhost";
  const synBridgeRelayers = parseAddressList(process.env.SYN_BRIDGE_RELAYERS);
  // This used to silently fall back to [deployer.address] -- a single
  // relayer, which is also the deploy key -- with a matching threshold of
  // 1, on ANY network, real or local, whenever SYN_BRIDGE_RELAYERS wasn't
  // set. That's a single point of failure on the contract that mints every
  // wrapped SYN on this chain: whoever holds that one key (or whoever ran
  // this script) could mint an unlimited amount on their own, no quorum
  // involved, and an operator could ship that configuration to a real
  // network just by forgetting to set an env var. The local/dev fallback
  // stays -- tests and local rehearsals need a deterministic single-signer
  // setup they can drive with one hardhat account -- but a real network
  // deploy now refuses to proceed without an explicit, real relayer set.
  if (!isLocalBridgeNetwork && synBridgeRelayers.length < 2) {
    throw new Error(
      "SYN_BRIDGE_RELAYERS must be set to a real, comma-separated list of at least 2 independent " +
      "relayer addresses on a real network -- refusing to default to a single deploy-key relayer " +
      "for the contract that mints every wrapped SYN on this chain"
    );
  }
  const bridgeRelayers = synBridgeRelayers.length > 0 ? synBridgeRelayers : [deployer.address];
  const synBridgeThreshold = BigInt(
    process.env.SYN_BRIDGE_THRESHOLD || (bridgeRelayers.length >= 2 ? "2" : "1")
  );
  if (!isLocalBridgeNetwork && synBridgeThreshold < 2n) {
    throw new Error(
      "SYN_BRIDGE_THRESHOLD must be at least 2 on a real network -- a threshold of 1 means any single " +
      "relayer can mint unilaterally, defeating the point of having more than one"
    );
  }
  const synBridgeMinter = await deployContract("SYNTHOSSynBridgeMinter", [
    syn.address,
    bridgeRelayers,
    synBridgeThreshold,
  ]);
  let tx = await token.initializeBridgeMinter(synBridgeMinter.address);
  await tx.wait();
  console.log(`SYN_BRIDGE_MINTER: ${synBridgeMinter.address} (relayers=${bridgeRelayers.join(",")} threshold=${synBridgeThreshold})`);
  console.log("SYN_BRIDGE_MINTER is paused by default -- unpause it once relayers are confirmed live.");

  // The contract already supports a per-mint cap and a rolling epoch cap
  // (setMintLimits) -- this script just never called it, so both stayed at
  // their default of 0, which SYNTHOSSynBridgeMinter treats as
  // "unlimited". Even with a real relayer quorum in place, an unlimited
  // per-mint/per-epoch cap means a quorum of compromised or colluding
  // relayers (or a bug in the off-chain relayer software agreeing with
  // itself) can mint any amount in one shot with no circuit breaker.
  // Setting real caps here bounds the blast radius of that scenario to
  // something an operator can notice and pause before it's catastrophic.
  // All three are still operator-tunable after deploy via setMintLimits.
  //
  // Local/hardhat deploys skip this: this same script bridge-mints large
  // lump-sum allocation buckets directly (the early adopter sale alone
  // defaults to 250M SYN in one approveMint call, and deploy-bitcoin-sale.js
  // mints another 50M), well above any real-network operational cap, so
  // applying it locally would break the dev/test flow this script is also
  // used for rather than protect anything -- there's no real relayer
  // quorum to bound on a local network anyway.
  if (!isLocalBridgeNetwork) {
    const synBridgeMaxMintAmount = ethers.parseUnits(
      process.env.SYN_BRIDGE_MAX_MINT_AMOUNT || "10000000", // 10M SYN per single mint (0.01% of 100B supply)
      18
    );
    const synBridgeEpochMintLimit = ethers.parseUnits(
      process.env.SYN_BRIDGE_EPOCH_MINT_LIMIT || "50000000", // 50M SYN per epoch
      18
    );
    const synBridgeEpochDuration = BigInt(process.env.SYN_BRIDGE_EPOCH_MINT_DURATION_SECONDS || "86400"); // 1 day
    tx = await synBridgeMinter.contract.setMintLimits(
      synBridgeMaxMintAmount,
      synBridgeEpochMintLimit,
      synBridgeEpochDuration
    );
    await tx.wait();
    console.log(
      `SYN_BRIDGE_MINT_LIMITS: max ${ethers.formatUnits(synBridgeMaxMintAmount, 18)} SYN per mint, ` +
      `${ethers.formatUnits(synBridgeEpochMintLimit, 18)} SYN per ${synBridgeEpochDuration}s epoch`
    );
  } else {
    console.log("SYN_BRIDGE_MINT_LIMITS: left unlimited on a local/dev network (see comment above)");
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

  // Governance needs to call SynCoin.createSnapshot() once per proposal
  // (see SYNTHOSGovernance.createProposal / castVote's doc comments for
  // why) -- this has to happen while `deployer` still owns SynCoin, since
  // setGovernance is onlyOwner and ownership moves to the timelock further
  // below.
  tx = await token.setGovernance(governance.address);
  await tx.wait();
  console.log(`SYN_GOVERNANCE (snapshot authority): ${governance.address}`);

  console.log("Configuring timelock custody");
  const proposerRole = await timelock.contract.PROPOSER_ROLE();
  const cancellerRole = await timelock.contract.CANCELLER_ROLE();
  const adminRole = await timelock.contract.TIMELOCK_ADMIN_ROLE();
  tx = await timelock.contract.grantRole(proposerRole, governance.address);
  await tx.wait();
  tx = await timelock.contract.revokeRole(proposerRole, deployer.address);
  await tx.wait();
  // OZ's TimelockController grants CANCELLER_ROLE to every constructor
  // proposer, so the deployer picked it up alongside PROPOSER_ROLE above.
  // Move it to governance the same way, and take it off the deployer --
  // nobody outside governance should be able to cancel a queued proposal.
  tx = await timelock.contract.grantRole(cancellerRole, governance.address);
  await tx.wait();
  tx = await timelock.contract.revokeRole(cancellerRole, deployer.address);
  await tx.wait();
  tx = await timelock.contract.grantRole(adminRole, launchMultisig.address);
  await tx.wait();
  tx = await timelock.contract.renounceRole(adminRole, deployer.address);
  await tx.wait();
  console.log(`TIMELOCK_ADMIN: ${launchMultisig.address}`);
  console.log(`TIMELOCK_PROPOSER: ${governance.address}`);
  console.log(`TIMELOCK_CANCELLER: ${governance.address}`);
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

  const founderAnnualRelease = ethers.parseUnits("1700000000", 18);
  const founderVesting = await deployContract("SYNTHOSFounderAnnualVesting", [
    syn.address,
    founderWallet,
    founderAnnualRelease,
    FOUNDER_RELEASE_TIMESTAMPS,
  ]);

  const immuneNodeRewardsRecipient = immuneNodeRewardsWallet || adopterRewards.address;
  const validatorRewardsRecipient = validatorRewardsWallet || staking.address;
  const allocations = [
    [founderVesting.address, ethers.parseUnits(BUCKETS_SYN.FOUNDER_VESTING, 18), "FOUNDER_VESTING"],
    [founderOpsWallet, ethers.parseUnits(BUCKETS_SYN.FOUNDER_OPERATIONS_GRANT, 18), "FOUNDER_OPERATIONS_GRANT"],
    [immuneNodeRewardsRecipient, ethers.parseUnits(BUCKETS_SYN.IMMUNE_NODE_REWARDS, 18), "IMMUNE_NODE_REWARDS"],
    [validatorRewardsRecipient, ethers.parseUnits(BUCKETS_SYN.VALIDATOR_REWARDS, 18), "VALIDATOR_REWARDS"],
    [earlyAdopterSale.address, earlyAdopterSaleAllocation, "COMMUNITY_EARLY_ADOPTER_SALE_TRANCHE_1"],
    [communityWallet, earlyAdopterCampaignReserve, "COMMUNITY_EARLY_ADOPTER_CAMPAIGN_RESERVE"],
    [communityWallet, communityOperatingAllocation, "COMMUNITY"],
    [treasuryWallet, ethers.parseUnits(BUCKETS_SYN.ECOSYSTEM_TREASURY, 18), "ECOSYSTEM_TREASURY"],
    [strategicReserveWallet, ethers.parseUnits(BUCKETS_SYN.STRATEGIC_RESERVE, 18), "STRATEGIC_RESERVE"],
  ];

  const isLocalNetwork = network.name === "hardhat" || network.name === "localhost";
  if (isLocalNetwork) {
    // Local/dev convenience only: this deploy script controls the sole
    // relayer key on a local devnet, so it can complete a real (if
    // single-signer) bridge mint for each bucket, exercising the exact
    // same approveMint path production relayers use. This branch never
    // runs on a real network -- there, minting requires real relayer
    // quorum over a real native-chain lock, which no deploy script can
    // manufacture on its own.
    console.log("Local network: minting genesis buckets through the real bridge-mint path (dev convenience)");
    tx = await synBridgeMinter.contract.unpause();
    await tx.wait();
    let seq = 0;
    for (const [recipient, amount, label] of allocations) {
      seq++;
      const sourceEventId = ethers.keccak256(ethers.toUtf8Bytes(`local-genesis-${label}-${seq}`));
      tx = await synBridgeMinter.contract.approveMint(sourceEventId, recipient, amount);
      await tx.wait();
      console.log(`${label}: ${ethers.formatUnits(amount, 18)} SYN -> ${recipient} (bridge-minted)`);
    }
  } else {
    console.log("");
    console.log("No SYN has been minted on this network. SynCoin is a bridge-pegged");
    console.log("wrapper: every bucket below needs to be funded by locking the");
    console.log("matching real SYN on the native chain and having the configured");
    console.log("relayers (" + bridgeRelayers.join(",") + ") approve the mint on");
    console.log("SYNTHOSSynBridgeMinter at " + synBridgeMinter.address + ".");
    console.log("Documented bucket sizes (see tokenomics.js):");
    for (const [recipient, amount, label] of allocations) {
      console.log(`  ${label}: ${ethers.formatUnits(amount, 18)} SYN -> ${recipient}`);
    }
    console.log("");
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

  if (poolConfig.length > 0 && !isLocalNetwork) {
    console.log("Real network: the deployer wallet must already hold the SYN needed to");
    console.log("seed these pools (funded through a real bridge mint) -- this script");
    console.log("cannot mint it, only spend what is already there.");
  }

  for (const pool of poolConfig) {
    let assetAddress = pool.address;
    if (!assetAddress) {
      if (!isLocalNetwork) {
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

    if (isLocalNetwork) {
      // Dev convenience only: mint this pool's SYN side through the real
      // bridge-mint path (see the genesis allocation branch above for why
      // this never runs on a real network).
      const sourceEventId = ethers.keccak256(
        ethers.toUtf8Bytes(`local-dex-liquidity-${pool.symbol}`)
      );
      tx = await synBridgeMinter.contract.approveMint(sourceEventId, deployer.address, synAmount);
      await tx.wait();
    } else {
      const deployerBalance = await token.balanceOf(deployer.address);
      if (deployerBalance < synAmount) {
        throw new Error(
          `Deployer wallet does not hold enough bridge-minted SYN to seed the ${pool.symbol} pool ` +
          `(needs ${ethers.formatUnits(synAmount, 18)} SYN, has ${ethers.formatUnits(deployerBalance, 18)}). ` +
          "Bridge-mint it in first (real relayer quorum over a real native-chain lock), then re-run."
        );
      }
    }

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

  const dexLiquidityBucket = ethers.parseUnits(BUCKETS_SYN.LOCKED_DEX_LIQUIDITY, 18);
  const remainingDexLiquidity = dexLiquidityBucket - seededSynLiquidity;
  if (remainingDexLiquidity > 0n) {
    if (isLocalNetwork) {
      const sourceEventId = ethers.keccak256(ethers.toUtf8Bytes("local-dex-liquidity-reserve"));
      tx = await synBridgeMinter.contract.approveMint(sourceEventId, dexLiquidityWallet, remainingDexLiquidity);
      await tx.wait();
      console.log(`LOCKED_DEX_LIQUIDITY_RESERVE: ${ethers.formatUnits(remainingDexLiquidity, 18)} SYN -> ${dexLiquidityWallet} (bridge-minted)`);
    } else {
      console.log(
        `LOCKED_DEX_LIQUIDITY_RESERVE: ${ethers.formatUnits(remainingDexLiquidity, 18)} SYN documented for ` +
        `${dexLiquidityWallet} -- fund via a real bridge mint, this script does not mint it.`
      );
    }
  }

  console.log("Transferring launch contract ownership to timelock");
  const ownableTransfers = [
    ["SynCoin", token],
    ["SYNTHOSSynBridgeMinter", synBridgeMinter.contract],
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
      totalSupply: TOTAL_SUPPLY_SYN,
      immuneNodeRewards: BUCKETS_SYN.IMMUNE_NODE_REWARDS,
      dexLiquidity: BUCKETS_SYN.LOCKED_DEX_LIQUIDITY,
      founderVesting: BUCKETS_SYN.FOUNDER_VESTING,
      validatorRewards: BUCKETS_SYN.VALIDATOR_REWARDS,
      communityAdopterRewards: BUCKETS_SYN.COMMUNITY,
      ecosystemTreasury: BUCKETS_SYN.ECOSYSTEM_TREASURY,
      cmoLaunchGrant: BUCKETS_SYN.CMO_LAUNCH_GRANT,
      strategicReserve: BUCKETS_SYN.STRATEGIC_RESERVE,
      founderLaunchAllocation: BUCKETS_SYN.FOUNDER_OPERATIONS_GRANT,
      note: "These figures describe the native chain's genesis allocation, which is the only place SYN is ever created. SynCoin on this network is a bridge-pegged wrapper with zero independent supply -- see contracts.synBridgeMinter.",
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
      synBridgeMinter: synBridgeMinter.address,
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
