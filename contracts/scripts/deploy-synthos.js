/**
 * SYNTHOS — ordered deployment for testnets / internal rehearsals.
 *
 * Order: SYNToken → SYNTHOSTimelock → SYNTHOSGovernance (grant timelock roles) →
 *        SYNTHOSStaking → RewardDistributor → token ownership → governance setup notes
 *
 * Usage:
 *   cd contracts && npx hardhat run scripts/deploy-synthos.js --network hardhat
 *   npx hardhat run scripts/deploy-synthos.js --network synthos
 *
 * Env:
 *   TIMELOCK_MIN_DELAY — seconds (default: 60 on hardhat, 172800 elsewhere = 2 days)
 */

const hre = require("hardhat");

async function main() {
  const { ethers, network } = hre;
  const [deployer] = await ethers.getSigners();
  const isHardhat = network.name === "hardhat";

  const minDelay = Number(
    process.env.TIMELOCK_MIN_DELAY ?? (isHardhat ? 60 : 172800)
  );

  console.log("SYNTHOS deploy");
  console.log("  network:", network.name);
  console.log("  deployer:", deployer.address);
  console.log("  timelock minDelay (s):", minDelay);

  console.log("\n1/5 SYNToken");
  const SYNToken = await ethers.getContractFactory("SYNToken");
  const synToken = await SYNToken.deploy();
  await synToken.waitForDeployment();
  const synTokenAddr = await synToken.getAddress();
  console.log("  SYNToken:", synTokenAddr);

  console.log("\n2/5 SYNTHOSTimelock");
  const Timelock = await ethers.getContractFactory("SYNTHOSTimelock");
  const timelock = await Timelock.deploy(
    minDelay,
    [deployer.address],
    [deployer.address],
    deployer.address
  );
  await timelock.waitForDeployment();
  const timelockAddr = await timelock.getAddress();
  console.log("  Timelock:", timelockAddr);

  console.log("\n3/5 SYNTHOSGovernance");
  const Gov = await ethers.getContractFactory("SYNTHOSGovernance");
  const governance = await Gov.deploy(synTokenAddr, timelockAddr);
  await governance.waitForDeployment();
  const govAddr = await governance.getAddress();
  console.log("  Governance:", govAddr);

  const proposerRole = await timelock.PROPOSER_ROLE();
  await (await timelock.grantRole(proposerRole, govAddr)).wait();
  await (await timelock.revokeRole(proposerRole, deployer.address)).wait();
  console.log("  Timelock: PROPOSER_ROLE → governance, revoked deployer proposer");

  console.log("\n4/5 SYNTHOSStaking");
  const Staking = await ethers.getContractFactory("SYNTHOSStaking");
  const staking = await Staking.deploy(synTokenAddr, govAddr);
  await staking.waitForDeployment();
  const stakingAddr = await staking.getAddress();
  console.log("  Staking:", stakingAddr);

  console.log("\n5/5 RewardDistributor");
  const Rewards = await ethers.getContractFactory("RewardDistributor");
  const rewards = await Rewards.deploy(govAddr);
  await rewards.waitForDeployment();
  const rewardsAddr = await rewards.getAddress();
  console.log("  RewardDistributor:", rewardsAddr);

  console.log("\nToken ownership → governance");
  await (await synToken.transferOwnership(govAddr)).wait();
  console.log("  done");

  console.log("\n--- Post-deploy (governance must execute) ---");
  console.log(
    "  RewardDistributor.approveToken(synToken) — only callable by governance address."
  );
  console.log(
    "  Plan: queue via timelock + governance proposal, or use multisig-as-governance pattern."
  );
  console.log(
    "  Explorer verify: npx hardhat verify --network <net> <address> [constructor args]"
  );

  const net = await ethers.provider.getNetwork();

  console.log("\nAddresses JSON (copy to deployment registry):");
  console.log(
    JSON.stringify(
      {
        network: network.name,
        chainId: Number(net.chainId),
        SYNToken: synTokenAddr,
        SYNTHOSTimelock: timelockAddr,
        SYNTHOSGovernance: govAddr,
        SYNTHOSStaking: stakingAddr,
        RewardDistributor: rewardsAddr,
      },
      null,
      2
    )
  );
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
