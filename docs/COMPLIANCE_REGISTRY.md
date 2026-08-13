# SYNTHOS Compliance Registry

`SYNTHOSComplianceRegistry.sol` is a compliance-by-design control layer for token distributions. It does not make SYN legally risk-free and it does not replace legal review. It creates an auditable on-chain gate that launch scripts, reward contracts, treasury contracts, and future distribution contracts can check before releasing SYN.

## What It Records

For each recipient wallet, the registry records:

- recipient category: Founder, CMO, Immune Operator, Validator, Treasury, Community, Liquidity, Strategic Reserve, or Ecosystem
- wallet verification status
- jurisdiction eligibility status
- disclosure acknowledgment status
- revocation or disqualification status
- lockup end timestamp
- disclosure document hash
- jurisdiction hash
- last update timestamp

The registry stores hashes for disclosures and jurisdiction labels so the chain can prove what was acknowledged without publishing sensitive personal data directly on-chain.

## Eligibility Rule

A recipient is eligible only if all of the following are true:

1. the wallet has a compliance record;
2. the wallet category matches the expected distribution category;
3. the wallet is verified;
4. the jurisdiction is eligible;
5. the recipient acknowledged the required disclosure;
6. the recipient has not been revoked or disqualified; and
7. any lockup timestamp has passed.

If any condition fails, `eligibleToReceive()` returns false and `requireEligible()` reverts.

## Intended Use

The registry should be checked before:

- confirming there is no current CMO launch grant;
- releasing founder or team allocations;
- approving immune node operator rewards;
- approving validator reward distributions;
- sending community grants;
- sending treasury, liquidity, or strategic reserve funds to operating wallets.

## Why This Matters

The registry supports a safer launch posture:

- rewards are tied to verified categories and procedures;
- wallet verification is explicit;
- disclosure acknowledgment is documented;
- restricted or disqualified recipients can be revoked;
- lockups can be enforced by distribution contracts;
- public tokenomics can be backed by auditable controls.

This is not a guarantee of regulatory treatment. It is evidence that SYNTHOS uses documented controls rather than uncontrolled token distribution.

## Contract

Primary contract:

- `contracts/src/synthos/SYNTHOSComplianceRegistry.sol`

Smoke test coverage:

- deploys the registry;
- creates an immune operator compliance record;
- verifies the operator is ineligible before disclosure acknowledgment;
- verifies the operator becomes eligible after acknowledgment;
- verifies revocation blocks eligibility;
- verifies restoration restores eligibility.

## Production Notes

Before production launch:

- ownership should move to a multisig, timelock, or governance-controlled address;
- disclosure hashes should match final signed or published disclosure documents;
- jurisdiction eligibility should be set using a documented policy;
- sensitive personal data should stay off-chain;
- distribution contracts should call `requireEligible()` before releasing SYN.
