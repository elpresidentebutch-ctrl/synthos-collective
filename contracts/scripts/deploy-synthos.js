// scripts/deploy-synthos.js

/**
 * SYNTHOS Network Deployment Script
 * 
 * Deploys all SYNTHOS contracts in correct order:
 * 1. SynCoin
 * 2. SYNTHOSGovernance  
 * 3. SYNTHOSStaking
 * 4. RewardDistributor
 * 
 * Usage: npx hardhat run scripts/deploy-synthos.js --network synthos
 */

const hre = require("hardhat");
const { ethers } = hre;

async function main() {
  console.log("🚀 Deploying SYNTHOS contracts...\n");

  const [deployer] = await ethers.getSigners();
  console.log(`📍 Deployer: ${deployer.address}\n`);

  // ============================================
  // 1. Deploy SynCoin
  // ============================================
  console.log("1️⃣ Deploying SynCoin...");
  const SynCoin = await ethers.getContractFactory("SynCoin");
  const synCoin = await SynCoin.deploy();
  await synCoin.deployed();
  console.log(`✅ SynCoin deployed to: ${synCoin.address}\n`);

  // ============================================
  // 2. Deploy SYNTHOSGovernance
  // ============================================
  console.log("2️⃣ Deploying SYNTHOSGovernance...");
  
  // Deploy timelock (2-day delay)
  const TimelockFactory = await ethers.getContractFactory("Timelock"); // You'll need Timelock contract
  // For now, we'll use a simple governance timelock address
  const timelockAddress = deployer.address; // Placeholder
  
  const SYNTHOSGovernance = await ethers.getContractFactory("SYNTHOSGovernance");
  const governance = await SYNTHOSGovernance.deploy(
    synCoin.address,
    timelockAddress
  );
  await governance.deployed();
  console.log(`✅ SYNTHOSGovernance deployed to: ${governance.address}\n`);

  // ============================================
  // 3. Deploy SYNTHOSStaking
  // ============================================
  console.log("3️⃣ Deploying SYNTHOSStaking...");
  const SYNTHOSStaking = await ethers.getContractFactory("SYNTHOSStaking");
  const staking = await SYNTHOSStaking.deploy(
    synCoin.address,
    governance.address
  );
  await staking.deployed();
  console.log(`✅ SYNTHOSStaking deployed to: ${staking.address}\n`);

  // ============================================
  // 4. Deploy RewardDistributor
  // ============================================
  console.log("4️⃣ Deploying RewardDistributor...");
  const RewardDistributor = await ethers.getContractFactory("RewardDistributor");
  const rewards = await RewardDistributor.deploy(governance.address);
  await rewards.deployed();
  console.log(`✅ RewardDistributor deployed to: ${rewards.address}\n`);

  // ============================================
  // 5. Configuration
  // ============================================
  console.log("⚙️  Configuring contracts...\n");

  // Transfer SynCoin ownership to governance (if needed)
  // console.log("   - Transferring SynCoin ownership to governance...");
  // let tx = await synCoin.transferOwnership(governance.address);
  // await tx.wait();
  // console.log("   ✓ Ownership transferred\n");

  // Approve SynCoin in RewardDistributor (if needed)
  // console.log("   - Approving SynCoin in RewardDistributor...");
  // tx = await rewards.approveToken(synCoin.address);
  // await tx.wait();
  // console.log("   ✓ Token approved\n");

  // ============================================
  // 6. Summary
  // ============================================
  console.log("=" .repeat(60));
  console.log("🎉 SYNTHOS DEPLOYMENT COMPLETE\n");
  console.log("Contract Addresses:");
  console.log("-".repeat(60));
  console.log(`SynCoin:            ${synCoin.address}`);
  console.log(`SYNTHOSGovernance:  ${governance.address}`);
  console.log(`SYNTHOSStaking:     ${staking.address}`);
  console.log(`RewardDistributor:  ${rewards.address}`);
  console.log("-".repeat(60));
  console.log(`\n📝 Save these addresses for future reference!\n`);

  // ============================================
  // 7. Verification Info
  // ============================================
  console.log("🔗 To verify contracts on block explorer:\n");
  console.log("npx hardhat verify --network synthos <CONTRACT_ADDRESS> <CONSTRUCTOR_ARGS>");
  console.log("");

  // ============================================
  // 8. Next Steps
  // ============================================
  console.log("📋 Next Steps:");
  console.log("   1. Update .env file with deployed addresses");
  console.log("   2. Run allocation script to distribute tokens");
  console.log("   3. Register initial validators");
  console.log("   4. Create first governance proposal");
  console.log("");

  return {
    synToken: synToken.address,
    governance: governance.address,
    staking: staking.address,
    rewards: rewards.address,
  };
}

main()
  .then(() => process.exit(0))
  .catch((error) => {
    console.error(error);
    process.exit(1);
  });
