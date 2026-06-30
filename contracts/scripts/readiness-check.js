const hre = require("hardhat");

const { ethers, network } = hre;

const expected = {
  totalSupply: "100000000000",
  immuneNodeRewards: "22000000000",
  lockedDexLiquidity: "20000000000",
  founderVesting: "17000000000",
  validatorRewards: "12000000000",
  community: "12500000000",
  ecosystemTreasury: "10000000000",
  cmoLaunchGrant: "3000000000",
  strategicReserve: "3000000000",
  founderOperationsGrant: "500000000",
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

  const SynCoin = await ethers.getContractFactory("SynCoin");
  const token = await SynCoin.deploy();
  await token.waitForDeployment();

  assertEq(await token.INITIAL_SUPPLY(), units(expected.totalSupply), "total supply");
  assertEq(await token.IMMUNE_NODE_REWARDS_ALLOCATION(), units(expected.immuneNodeRewards), "immune node rewards bucket");
  assertEq(await token.LOCKED_DEX_LIQUIDITY_ALLOCATION(), units(expected.lockedDexLiquidity), "locked DEX liquidity bucket");
  assertEq(await token.FOUNDER_VESTING_ALLOCATION(), units(expected.founderVesting), "founder vesting bucket");
  assertEq(await token.VALIDATOR_REWARDS_ALLOCATION(), units(expected.validatorRewards), "validator rewards bucket");
  assertEq(await token.COMMUNITY_ALLOCATION(), units(expected.community), "community/adopter bucket");
  assertEq(await token.ECOSYSTEM_TREASURY_ALLOCATION(), units(expected.ecosystemTreasury), "ecosystem treasury bucket");
  assertEq(await token.CMO_LAUNCH_GRANT(), units(expected.cmoLaunchGrant), "CMO launch grant");
  assertEq(await token.STRATEGIC_RESERVE_ALLOCATION(), units(expected.strategicReserve), "strategic reserve bucket");
  assertEq(await token.FOUNDER_OPERATIONS_GRANT(), units(expected.founderOperationsGrant), "founder launch allocation");
  assertEq(await token.FOUNDER_ANNUAL_RELEASE(), units(expected.founderAnnualRelease), "founder annual release");
  assertEq(await token.tokenomicsTotal(), await token.INITIAL_SUPPLY(), "tokenomics total equals supply");
  assertEq(await token.immuneRewardsBreakdownTotal(), await token.IMMUNE_NODE_REWARDS_ALLOCATION(), "immune sub-buckets total");
  assertEq(await token.validatorRewardsBreakdownTotal(), await token.VALIDATOR_REWARDS_ALLOCATION(), "validator sub-buckets total");
  assertEq(await token.communityRewardsBreakdownTotal(), await token.COMMUNITY_ALLOCATION(), "community sub-buckets total");

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

  const [deployer] = await ethers.getSigners();
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
