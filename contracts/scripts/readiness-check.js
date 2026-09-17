const hre = require("hardhat");
const { BUCKETS_SYN, TOTAL_SUPPLY_SYN, assertBucketsSumToTotal } = require("../tokenomics");

const { ethers, network } = hre;

const expected = {
  totalSupply: TOTAL_SUPPLY_SYN,
  immuneNodeRewards: BUCKETS_SYN.IMMUNE_NODE_REWARDS,
  lockedDexLiquidity: BUCKETS_SYN.LOCKED_DEX_LIQUIDITY,
  founderVesting: BUCKETS_SYN.FOUNDER_VESTING,
  validatorRewards: BUCKETS_SYN.VALIDATOR_REWARDS,
  community: BUCKETS_SYN.COMMUNITY,
  ecosystemTreasury: BUCKETS_SYN.ECOSYSTEM_TREASURY,
  cmoLaunchGrant: BUCKETS_SYN.CMO_LAUNCH_GRANT,
  strategicReserve: BUCKETS_SYN.STRATEGIC_RESERVE,
  founderOperationsGrant: BUCKETS_SYN.FOUNDER_OPERATIONS_GRANT,
  founderAnnualRelease: "1700000000",
  immuneTargetOperators: 100000n,
  immuneActivationReward: "500",
  immuneHeartbeatReward: "1000",
  immuneHeartbeatPeriods: 120n,
  immuneTenYearMaxPerOperator: "120500",
  validatorTargetOperators: 5000n,
  validatorActivationReward: "10000",
  validatorMonthlyBaseReward: "5000",
  validatorMonthlyPerformanceBonusCap: "2500",
  validatorTenYearMaxPerValidator: "910000",
};

function units(value) {
  return ethers.parseUnits(value, 18);
}

function assertEq(actual, wanted, label) {
  if (actual !== wanted) {
    throw new Error(`${label} mismatch: expected ${wanted.toString()}, got ${actual.toString()}`);
  }
  console.log(`ok ${label}`);
}

async function main() {
  console.log("SYNTHOS token launch readiness check");
  console.log(`Network: ${network.name}`);

  // These tokenomics numbers are enforced on the native chain's genesis
  // allocation, not on SynCoin -- SynCoin no longer carries any bucket
  // bookkeeping at all. This just checks tokenomics.js (the EVM side's
  // documentation copy of those figures) hasn't drifted out of sum.
  assertBucketsSumToTotal();
  console.log(`ok tokenomics buckets sum to ${expected.totalSupply} SYN`);

  const [deployer] = await ethers.getSigners();

  const SynCoin = await ethers.getContractFactory("SynCoin");
  const token = await SynCoin.deploy(deployer.address);
  await token.waitForDeployment();

  assertEq(await token.totalSupply(), 0n, "SynCoin starts at zero supply");
  assertEq(await token.bridgeMinterInitialized(), false, "bridge minter starts uninitialized");
  assertEq(await token.bridgeMinter(), ethers.ZeroAddress, "bridge minter starts unset");

  try {
    await token.mint.staticCall(deployer.address, 1);
    throw new Error("SynCoin.mint() should be unreachable before the bridge minter is wired up");
  } catch (error) {
    if (!/not bridge minter/.test(error.message)) throw error;
  }
  console.log("ok SynCoin.mint() unreachable before bridge minter wiring, and not owner-callable");

  const Minter = await ethers.getContractFactory("SYNTHOSSynBridgeMinter");
  const minter = await Minter.deploy(await token.getAddress(), [deployer.address], 1);
  await minter.waitForDeployment();
  await (await token.initializeBridgeMinter(await minter.getAddress())).wait();

  assertEq(await token.bridgeMinterInitialized(), true, "bridge minter wired up");
  if ((await token.bridgeMinter()) !== (await minter.getAddress())) {
    throw new Error("bridgeMinter address mismatch after wiring");
  }
  console.log("ok bridgeMinter address matches deployed SYNTHOSSynBridgeMinter");

  try {
    await token.initializeBridgeMinter.staticCall(deployer.address);
    throw new Error("initializeBridgeMinter should be unreachable a second time");
  } catch (error) {
    if (!/bridge minter already initialized/.test(error.message)) throw error;
  }
  console.log("ok bridge minter wiring is permanent (no setter, cannot re-initialize)");

  if ((await token.treasury()) !== deployer.address) {
    throw new Error("treasury recycling burn treasury mismatch");
  }
  console.log("ok treasury recycling burn treasury");
  const approvedSpendTypes = [
    ["protocol spend", await token.SPEND_PROTOCOL()],
    ["node registration spend", await token.SPEND_NODE_REGISTRATION()],
    ["service fee spend", await token.SPEND_SERVICE_FEE()],
    ["marketplace spend", await token.SPEND_MARKETPLACE()],
  ];
  for (const [label, spendType] of approvedSpendTypes) {
    assertEq(
      await token.approvedTreasuryRecyclingSpendTypes(spendType),
      true,
      label
    );
  }

  const AdopterRewards = await ethers.getContractFactory("SYNTHOSAdopterRewards");
  const adopterRewards = await AdopterRewards.deploy(
    await token.getAddress(),
    units(expected.immuneActivationReward),
    units(expected.immuneHeartbeatReward),
    30n * 24n * 60n * 60n,
    expected.immuneHeartbeatPeriods
  );
  await adopterRewards.waitForDeployment();

  assertEq(await adopterRewards.TARGET_IMMUNE_OPERATORS(), expected.immuneTargetOperators, "immune target operator count");
  assertEq(await adopterRewards.DEFAULT_EARLY_OPERATOR_REWARD(), units(expected.immuneActivationReward), "immune early operator reward");
  assertEq(await adopterRewards.DEFAULT_HEARTBEAT_REWARD(), units(expected.immuneHeartbeatReward), "immune heartbeat reward");
  assertEq(await adopterRewards.TEN_YEAR_HEARTBEAT_PERIODS(), expected.immuneHeartbeatPeriods, "immune heartbeat period count");
  assertEq(await adopterRewards.TEN_YEAR_MAX_REWARD_PER_OPERATOR(), units(expected.immuneTenYearMaxPerOperator), "immune ten-year max per operator");

  const Staking = await ethers.getContractFactory("SYNTHOSStaking");
  const staking = await Staking.deploy(await token.getAddress(), deployer.address);
  await staking.waitForDeployment();

  assertEq(await staking.TARGET_VALIDATOR_OPERATORS(), expected.validatorTargetOperators, "validator target operator count");
  assertEq(await staking.VALIDATOR_ACTIVATION_REWARD(), units(expected.validatorActivationReward), "validator activation reward");
  assertEq(await staking.VALIDATOR_MONTHLY_BASE_REWARD(), units(expected.validatorMonthlyBaseReward), "validator monthly base reward");
  assertEq(await staking.VALIDATOR_MONTHLY_PERFORMANCE_BONUS_CAP(), units(expected.validatorMonthlyPerformanceBonusCap), "validator monthly performance bonus cap");
  assertEq(await staking.TEN_YEAR_MAX_REWARD_PER_VALIDATOR(), units(expected.validatorTenYearMaxPerValidator), "validator ten-year max per validator");

  console.log("SYNTHOS contract tokenomics readiness check passed.");
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
