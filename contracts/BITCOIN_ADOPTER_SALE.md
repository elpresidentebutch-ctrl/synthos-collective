# SYNTHOS Bitcoin Early Adopter Sale

`SYNTHOSBitcoinAdopterSale` lets early adopters buy SYN with native Bitcoin.
Unlike the crypto-native sale (`SYNTHOSEarlyAdopterSale`), this is not
atomic: Bitcoin settles on its own chain, which this EVM contract has no way
to observe. Payment and SYN delivery are two separate steps connected by a
trusted confirmer.

## Why This Is Different From the ETH/USDC/USDT/WBTC Sale

The crypto-native sale contract delivers SYN in the same transaction as
payment because the payment (ETH or an ERC-20) and the SYN transfer happen
on the same chain, in the same call. A Solidity contract cannot read the
Bitcoin blockchain, so it cannot verify a BTC payment on its own. Wrapped
Bitcoin (WBTC) sidesteps this entirely because WBTC is itself an ERC-20 on
the EVM chain -- it already works through the existing sale contract with no
new code, just `setPaymentAsset()`. See `scripts/enable-wbtc-payment.js`.

Native BTC has no such shortcut. This contract is the deliberate, disclosed
alternative: a human (the confirmer) verifies the Bitcoin payment off-chain,
then calls a contract function to release SYN under the same eligibility,
minimum-purchase, wallet-cap, and allocation rules as the main sale.

## Trust Model

This is not trustless. The confirmer role can release SYN for any Bitcoin
transaction it claims occurred, whether or not it actually did. Practical
implications:

- Hold the confirmer private key to the same custody standard as a treasury
  signer -- not a routine hot wallet used for anything else.
- Consider making the confirmer a multisig rather than a single EOA once
  volume justifies it.
- Every confirmation is public on-chain (`BitcoinPurchaseConfirmed` event),
  so buyers and auditors can cross-check confirmer behavior against the
  Bitcoin blockchain after the fact.
- The contract owner (separate from the confirmer) can rotate the confirmer
  address at any time via `setConfirmer()`, and can pause the contract.

## Flow

1. Deploy `SynCoin`, `SYNTHOSComplianceRegistry`, and
   `SYNTHOSEarlyAdopterSale` first (see `EARLY_ADOPTER_SALE.md`), then run
   `scripts/deploy-bitcoin-sale.js` to deploy and fund
   `SYNTHOSBitcoinAdopterSale` from the same
   `COMMUNITY_EARLY_ADOPTER_CAMPAIGNS` bucket.
2. Publish a SYNTHOS-controlled Bitcoin receiving address to buyers. This
   address lives outside this contract -- it is an ordinary Bitcoin wallet,
   not something Solidity can generate or hold.
3. Mark the buyer's EVM wallet eligible in `SYNTHOSComplianceRegistry` as
   category `Community`, same as the crypto-native sale.
4. Buyer sends BTC to the published address and shares the resulting
   transaction id plus the EVM address that should receive SYN.
5. The confirmer checks the Bitcoin transaction on a block explorer: correct
   address, correct amount, enough confirmations for the amount involved.
6. The confirmer calls `confirmBitcoinPayment(btcTxId, satoshis,
   beneficiary)` (see `scripts/confirm-bitcoin-payment.js`). `btcTxId` is
   `keccak256` of the Bitcoin transaction hash, used only to block
   double-crediting the same payment.
7. If the beneficiary is eligible, the sale allocation and wallet cap are
   not exceeded, and the contract holds enough SYN, SYN transfers to the
   beneficiary in that same call.

## Pricing

Bitcoin has no on-chain price feed available to this contract, so the owner
sets `btcUsdPrice18` manually via `setBtcUsdPrice()` before confirmations,
the same pattern the crypto-native sale uses for native-coin payments. Keep
this price current; a stale price either overcharges or undercharges
buyers in SYN terms.

```text
usdValue = satoshis * btcUsdPrice18 / 100,000,000
synAmount = usdValue / 0.10
```

## Controls

- `pause()` / `unpause()` stop or resume confirmations.
- `setConfirmer()` rotates who can confirm payments.
- `setBtcUsdPrice()` updates the BTC/USD price used for quoting.
- `setSaleLimits()` changes allocation, minimum purchase, and wallet cap.
- `withdrawUnsoldSyn()` recovers unsold SYN after the sale.

## Compliance Gate

The contract requires:

```text
SYNTHOSComplianceRegistry.eligibleToReceive(beneficiary, Community) == true
```

The same wallet-verification, jurisdiction, and disclosure requirements
apply as the crypto-native sale.
