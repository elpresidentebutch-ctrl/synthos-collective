# SYNTHOS Early Adopter Crypto Sale

`SYNTHOSEarlyAdopterSale` is the automated crypto-only sale contract for the
early adopter allocation.

## Price

- 1 SYN = 0.05 USD
- 1 USD = 20 SYN
- 100 USD = 2,000 SYN

The contract uses fixed USD math:

```text
SYN received = crypto payment USD value / 0.05
```

## Flow

1. Deploy `SynCoin`, `SYNTHOSComplianceRegistry`, and
   `SYNTHOSEarlyAdopterSale`.
2. Fund the sale contract with SYN from `COMMUNITY_EARLY_ADOPTER_SALE`.
3. Mark an early adopter wallet eligible in `SYNTHOSComplianceRegistry` as
   category `Community`.
4. Enable accepted crypto payment assets, such as USDC.
5. Buyer approves the sale contract to spend the payment token.
6. Buyer calls `buyWithToken(paymentAsset, paymentAmount, beneficiary)`.
7. Payment goes directly to the treasury wallet and SYN transfers immediately
   to the beneficiary.

Native coin payments are optional. They require the owner to set
`nativeUsdPrice18` because the contract does not use a live price oracle.

## Controls

- `pause()` stops purchases.
- `setPaymentAsset()` enables or disables an ERC-20 payment asset.
- `setNativePaymentConfig()` enables or disables native coin payments.
- `setSaleLimits()` changes allocation, minimum purchase, and wallet cap.
- `withdrawUnsoldSyn()` recovers unsold SYN after the sale.

## Compliance Gate

The contract requires:

```text
SYNTHOSComplianceRegistry.eligibleToReceive(beneficiary, Community) == true
```

This keeps the purchase automatic while still requiring wallet verification,
jurisdiction eligibility, and disclosure acknowledgement before distribution.
