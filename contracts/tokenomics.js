/**
 * SYNTHOS tokenomics bucket sizes -- documentation and deploy-record data
 * only. These numbers describe how the native chain's genesis allocation
 * (see the Go side's cmd/opennet and internal/chain/genesis.go) divides up
 * the 100B SYN cap. They are NOT enforced by any EVM contract: the native
 * chain is the only place SYN is ever created, so it's the only place these
 * numbers are actually enforced. This file exists purely so deploy scripts
 * and record-keeping on the EVM side can print/reference the same figures
 * without duplicating magic numbers, and so a single `require` here is the
 * one place to update if the tokenomics ever change.
 */

const UNITS = "1000000000000000000"; // 10**18, kept as a string to avoid BigInt/ethers coupling here

const BUCKETS_SYN = {
  IMMUNE_NODE_REWARDS: "22000000000",
  LOCKED_DEX_LIQUIDITY: "20000000000",
  FOUNDER_VESTING: "17000000000",
  VALIDATOR_REWARDS: "12000000000",
  COMMUNITY: "12500000000",
  ECOSYSTEM_TREASURY: "13000000000",
  CMO_LAUNCH_GRANT: "0",
  STRATEGIC_RESERVE: "3000000000",
  FOUNDER_OPERATIONS_GRANT: "500000000",
};

const TOTAL_SUPPLY_SYN = "100000000000";

function assertBucketsSumToTotal() {
  const total = Object.values(BUCKETS_SYN).reduce(
    (sum, value) => sum + BigInt(value),
    0n
  );
  if (total !== BigInt(TOTAL_SUPPLY_SYN)) {
    throw new Error(
      `tokenomics buckets sum to ${total.toString()}, expected ${TOTAL_SUPPLY_SYN}`
    );
  }
}

module.exports = { UNITS, BUCKETS_SYN, TOTAL_SUPPLY_SYN, assertBucketsSumToTotal };
