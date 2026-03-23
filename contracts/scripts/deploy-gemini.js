// scripts/deploy-gemini.js
// NOTE: Legacy script — uses older Hardhat/ethers patterns. Update for @nomicfoundation/hardhat-toolbox / ethers v6 before production use.

/**
 * Gemini Megachain 2.0 Deployment Script
 * 
 * Deploys all Gemini contracts in correct order:
 * 1. GEMToken
 * 2. CrossChainBridge
 * 3. GeminiLiquidityPool
 * 4. GeminiOracle
 * 5. RewardDistributor
 * 
 * Usage: npx hardhat run scripts/deploy-gemini.js --network gemini
 */

const hre = require("hardhat");
const { ethers } = hre;

async function main() {
  console.log("🚀 Deploying Gemini Megachain 2.0 contracts...\n");

  const [deployer] = await ethers.getSigners();
  console.log(`📍 Deployer: ${deployer.address}\n`);

  // ============================================
  // 1. Deploy GEMToken
  // ============================================
  console.log("1️⃣ Deploying GEMToken...");
  const GEMToken = await ethers.getContractFactory("GEMToken");
  const gem = await GEMToken.deploy(deployer.address); // Ecosystem fund = deployer initially
  await gem.deployed();
  console.log(`✅ GEMToken deployed to: ${gem.address}`);
  console.log(`   Total Supply: 2 billion GEM\n`);

  // ============================================
  // 2. Deploy CrossChainBridge
  // ============================================
  console.log("2️⃣ Deploying CrossChainBridge...");
  const CrossChainBridge = await ethers.getContractFactory("CrossChainBridge");
  const bridge = await CrossChainBridge.deploy();
  await bridge.deployed();
  console.log(`✅ CrossChainBridge deployed to: ${bridge.address}\n`);

  // ============================================
  // 3. Deploy GeminiLiquidityPool
  // ============================================
  console.log("3️⃣ Deploying GeminiLiquidityPool...");
  const GeminiLiquidityPool = await ethers.getContractFactory(
    "GeminiLiquidityPool"
  );
  const liquidityPool = await GeminiLiquidityPool.deploy();
  await liquidityPool.deployed();
  console.log(`✅ GeminiLiquidityPool deployed to: ${liquidityPool.address}\n`);

  // ============================================
  // 4. Deploy GeminiOracle
  // ============================================
  console.log("4️⃣ Deploying GeminiOracle...");
  const GeminiOracle = await ethers.getContractFactory("GeminiOracle");
  const oracle = await GeminiOracle.deploy();
  await oracle.deployed();
  console.log(`✅ GeminiOracle deployed to: ${oracle.address}\n`);

  // ============================================
  // 5. Deploy RewardDistributor
  // ============================================
  console.log("5️⃣ Deploying RewardDistributor...");
  const RewardDistributor = await ethers.getContractFactory(
    "RewardDistributor"
  );
  const rewards = await RewardDistributor.deploy(deployer.address); // Deployer as governance initially
  await rewards.deployed();
  console.log(`✅ RewardDistributor deployed to: ${rewards.address}\n`);

  // ============================================
  // 6. Configuration
  // ============================================
  console.log("⚙️  Configuring contracts...\n");

  // Set bridge in GEMToken
  console.log("   - Setting bridge in GEMToken...");
  let tx = await gem.setBridgeContract(bridge.address);
  await tx.wait();
  console.log("   ✓ Bridge set\n");

  // Register GEM token in bridge
  console.log("   - Registering GEM token in bridge...");
  const GEMINI_CHAIN_ID = 2048;
  const ETHEREUM_CHAIN_ID = 1;
  const POLYGON_CHAIN_ID = 137;
  const SYNTHOS_CHAIN_ID = 1234;

  tx = await bridge.registerToken(GEMINI_CHAIN_ID, gem.address);
  await tx.wait();
  console.log("   ✓ GEM registered on Gemini\n");

  // Set ecosystem fund address (multi-sig or governance in production)
  console.log("   - Setting ecosystem fund...");
  const ecosystemFund = deployer.address; // Should be a multi-sig
  tx = await gem.setEcosystemFund(ecosystemFund);
  await tx.wait();
  console.log("   ✓ Ecosystem fund set\n");

  // Approve GEM in RewardDistributor
  console.log("   - Approving GEM token in RewardDistributor...");
  tx = await rewards.approveToken(gem.address);
  await tx.wait();
  console.log("   ✓ Token approved\n");

  // ============================================
  // 7. Add Initial Validators for Bridge
  // ============================================
  console.log("👷 Adding initial validators for bridge...");

  // In production, these would be actual validator addresses
  const [val1, val2, val3] = await ethers.getSigners();

  tx = await bridge.addValidator(val1.address);
  await tx.wait();
  console.log(`   ✓ Validator 1: ${val1.address}`);

  tx = await bridge.addValidator(val2.address);
  await tx.wait();
  console.log(`   ✓ Validator 2: ${val2.address}`);

  tx = await bridge.addValidator(val3.address);
  await tx.wait();
  console.log(`   ✓ Validator 3: ${val3.address}`);

  // Set required signatures to 2-of-3
  console.log("\n   - Setting required signatures to 2-of-3...");
  tx = await bridge.setRequiredSignatures(2);
  await tx.wait();
  console.log("   ✓ Signatures set\n");

  // ============================================
  // 8. Register Price Providers for Oracle
  // ============================================
  console.log("🔮 Registering price providers...");

  // Note: Providers need to stake 10k ETH
  // Not doing this in automated script - must be done separately

  console.log("   ℹ️  Price providers must be registered separately");
  console.log("   Minimum stake: 10,000 ETH per provider\n");

  // ============================================
  // 9. Summary
  // ============================================
  console.log("=" .repeat(60));
  console.log("🎉 GEMINI MEGACHAIN 2.0 DEPLOYMENT COMPLETE\n");
  console.log("Contract Addresses:");
  console.log("-".repeat(60));
  console.log(`GEMToken:           ${gem.address}`);
  console.log(`CrossChainBridge:   ${bridge.address}`);
  console.log(`LiquidityPool:      ${liquidityPool.address}`);
  console.log(`GeminiOracle:       ${oracle.address}`);
  console.log(`RewardDistributor:  ${rewards.address}`);
  console.log("-".repeat(60));
  console.log(`\n📝 Save these addresses for future reference!\n`);

  // ============================================
  // 10. Verification Info
  // ============================================
  console.log("🔗 To verify contracts on block explorer:\n");
  console.log("npx hardhat verify --network gemini <CONTRACT_ADDRESS> <CONSTRUCTOR_ARGS>");
  console.log("");

  // ============================================
  // 11. Next Steps
  // ============================================
  console.log("📋 Next Steps:");
  console.log("   1. Register bridge validators (requires voting)");
  console.log("   2. Register price oracle providers");
  console.log("   3. Create initial liquidity pools");
  console.log("   4. Set up cross-chain bridge with SYNTHOS");
  console.log("   5. Distribute initial GEM allocations");
  console.log("");

  return {
    gem: gem.address,
    bridge: bridge.address,
    liquidityPool: liquidityPool.address,
    oracle: oracle.address,
    rewards: rewards.address,
  };
}

main()
  .then(() => process.exit(0))
  .catch((error) => {
    console.error(error);
    process.exit(1);
  });
