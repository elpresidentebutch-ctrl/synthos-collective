# SYNTHOS Liquidity Plan

The DEX launch should seed only intentional pools. Production deployments must
provide `DEX_POOLS_JSON`; otherwise no production pools are created.

## Allocation

`LOCKED_DEX_LIQUIDITY_ALLOCATION` is:

```text
20,000,000,000 SYN
```

The deploy script uses part of this allocation to seed configured pools. Any
unseeded remainder is sent to `DEX_LIQUIDITY_WALLET` as the liquidity reserve.

## Production Pool Config

Set `DEX_POOLS_JSON` in `.env`. Until real verified asset contracts exist,
launch with no initial pools:

```json
[]
```

When a verified asset exists, use that real deployed contract address:

```json
[
  {
    "symbol": "B12",
    "name": "B12 Asset",
    "address": "0x0000000000000000000000000000000000000000",
    "syn": "10000000",
    "asset": "50000",
    "decimals": 18
  }
]
```

Rules:

- `address` is required on production networks.
- `syn` is denominated in whole SYN before `18` decimals are applied.
- `asset` is denominated in whole asset units before `decimals` are applied.
- Pool seed amounts should be small enough to avoid launch-price distortion.
- Remaining liquidity allocation should stay in reserve until governance or
  multisig/timelock approves additional deployment.

## Launch Checks

Before launch:

- Confirm each asset token is real and verified.
- Confirm each asset token uses the expected decimals.
- Confirm the SYN side of all pools does not exceed the liquidity allocation.
- Confirm `DEX_LIQUIDITY_WALLET` is controlled by the intended custody path.
- Confirm pool quote outputs are nonzero after seeding.
- Confirm LP ownership expectations are documented.

After launch:

- Record each pool asset address.
- Record initial SYN and asset reserves.
- Record the unseeded liquidity reserve balance.
- Monitor large swaps and reserve imbalance during the first 24 hours.
