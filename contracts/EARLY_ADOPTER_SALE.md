# SYNTHOS Early Adopter Crypto Sale

`SYNTHOSEarlyAdopterPresale` is the automated crypto-only pre-sale contract for the
early adopter allocation.

## Price

- 1 SYN = 0.05 USD
- 1 USD = 20 SYN
- 100 USD = 2,000 SYN
- First active tranche = 250,000,000 SYN
- First active tranche maximum value = 12,500,000 USD
- Source bucket = `COMMUNITY_EARLY_ADOPTER_CAMPAIGNS`
- Campaign reserve after tranche one = 1,750,000,000 SYN

The contract uses fixed USD math:

```text
SYN received = crypto payment USD value / 0.05
```

## Flow

1. Deploy `SynCoin`, `SYNTHOSComplianceRegistry`, and
   `SYNTHOSEarlyAdopterPresale`.
2. Fund the pre-sale contract with 250,000,000 SYN from
   `COMMUNITY_EARLY_ADOPTER_CAMPAIGNS`.
3. Mark an early adopter wallet eligible in `SYNTHOSComplianceRegistry` as
   category `Community`.
4. Enable accepted crypto payment assets, such as USDC, USDT, WBTC, WETH, or
   native ETH.
5. Buyer approves the pre-sale contract to spend the payment token.
6. Buyer calls `buyWithToken(paymentAsset, paymentAmount, beneficiary)`.
7. Payment goes directly to the treasury wallet and SYN transfers immediately
   to the beneficiary.

There is no manual payment confirmation status in the sale path. If the buyer
is eligible, the payment asset is enabled, the pre-sale contract has enough SYN, and
the transaction succeeds on-chain, SYN is delivered in the same transaction.

Native coin payments are optional. They require the owner to set
`nativeUsdPrice18` because the contract does not use a live price oracle.

## Receiving Wallet

Early adopter sale proceeds should be received by the SYNTHOS treasury wallet:

```text
0xdAE5DF4807274D7a115bB5078c94b023453A05F5
```

The pre-sale contract forwards ERC-20 payments and native-chain payments directly
to this treasury wallet.

## Accepted Crypto Assets

Automatic purchase settlement works with assets that exist on the same EVM
network as the pre-sale contract:

| Asset | Automatic path |
| --- | --- |
| USDC | ERC-20 payment asset |
| USDT | ERC-20 payment asset |
| ETH | Native coin payment, if enabled |
| WETH | ERC-20 payment asset |
| Bitcoin exposure | WBTC ERC-20 payment asset |

Native BTC is not an ERC-20 token and cannot directly trigger this EVM sale
contract without a bridge or payment processor. For fully automatic settlement,
use WBTC or another verified wrapped BTC asset on the sale network.

Ethereum mainnet reference addresses:

```text
USDC 0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48
USDT 0xdAC17F958D2ee523a2206206994597C13D831ec7
WBTC 0x2260FAC5E5542a773Aa44fBCfeDf7C193bc2C599
WETH 0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2
```

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
