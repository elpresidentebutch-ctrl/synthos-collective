# SYNTHOS Bridge Relayer Deployment Runbook

2026-09-17

## Overview

SYNTHOS bridges real value between two independently verified ledgers:

- **The native SYNTHOS chain** is the sole sovereign, canonical ledger. All real SYN ultimately lives here.
- **Wrapped SynCoin** (`SynCoin.sol`) is an 18-decimal ERC20 on EVM (Base/Ethereum) that can only ever be minted 1:1 against SYN actually locked on the native chain. It starts at zero supply and has no path to independent minting.

Two relayers keep both directions of the bridge moving, and each is deliberately its own separate process so a bug or compromise in one can't touch the other's funds:

| Direction | What it watches | What it does | Implementation |
| --- | --- | --- | --- |
| EVM burn to native release | `SynBurnedForNativeRelease` events on `SYNTHOSSynBridgeMinter` | Submits a quorum-signed release of native SYN | Go, `cmd/bridgerelayer -mode watch-evm` |
| Native lock to EVM mint | `bridge_lock_native` events on the native chain | Calls `approveMint` on `SYNTHOSSynBridgeMinter` | Node.js, `scripts/relay-native-locks-to-syn-mint.js` |

This runbook walks through generating keys, configuring the contracts at deploy time, running both relayers, and the checks to run before trusting either one with real funds.

## Prerequisites

Before starting either relayer:

- **Go** (for `cmd/bridgerelayer`) and **Node.js** (for the native lock relayer) installed.
- **Contracts deployed** on the target EVM network via `deploy-synthos.js` / `deploy-token.js`, giving you the `SYNTHOSSynBridgeMinter` address and the vault/lock contract address.
- **A native SYNTHOS RPC endpoint** reachable by both relayers (default `http://127.0.0.1:8080`).
- **An EVM RPC endpoint** for the target network (a local Hardhat node, a Base/Ethereum testnet, or mainnet).
- **The native escrow/lock address** that real bridge locks must pay into — this is what the native lock relayer checks incoming locks against, so it must match whatever your native bridge module actually treats as escrow.
- Repo built and tested clean: `go build ./...` (excluding `cmd/node`, which needs a currently-blocked dependency) and `cd contracts && npx hardhat test`.

## Step 1: Generate keys

Each bridge direction needs its own key material. Keep them on separate machines/wallets where possible — a relayer key is a hot signing key by design, so treat it accordingly.

**Native release-authority key(s)** (used by the Go relayer to sign native releases):

```
go run ./cmd/wallet -show-private
```

This prints a new native ed25519 keypair. Generate one per relayer instance if you're running more than one for quorum. Record the public address for whatever the native chain's release-authority/threshold configuration expects.

**EVM relayer key(s)** (used by the Node.js relayer to call `approveMint`, and by whichever address submits native releases from the Go side's EVM leg):

```
node -e "const {Wallet}=require('ethers'); const w=Wallet.createRandom(); console.log('address:', w.address); console.log('private key:', w.privateKey)"
```

Each EVM relayer address you generate must then be added as an authorized relayer on `SYNTHOSSynBridgeMinter` (via its owner/governance) before it can call `approveMint` and have it count toward quorum.

**Native escrow address**: this isn't a key you generate — it's the existing native address your bridge module treats as the lock/escrow destination. Confirm it before configuring the Node relayer's `SYNTHOS_BRIDGE_NATIVE_LOCK_ADDRESS`; a mismatch here means real locks will silently fail the relayer's recipient check (see Security, below).

**Storage**: never commit private keys to the repo or put them in `.env` files that get synced to a connected folder. Use your OS's secret manager, a hardware wallet, or at minimum an untracked, permission-restricted local file, and pass keys to the relayers via environment variables at process-start time.

## Step 2: Deploy-time configuration

When you run `deploy-token.js` (or `deploy-synthos.js`, which calls it), `SynCoin` and `SYNTHOSSynBridgeMinter` are deployed together and wired via `initializeBridgeMinter` — a one-time call with no setter, so the wiring is permanent from that point on.

**Relayer set and threshold** are configured at this point too: the minter is deployed with the list of authorized relayer addresses and the quorum threshold (M-of-N) required before `approveMint`/release actually executes. Add every EVM relayer address you generated in Step 1 here.

**Important gotchas to check with `post-deploy-check.js` and `readiness-check.js` before relying on either relayer:**

- **Ownership goes to the timelock, not the deployer.** On any real network (including testnets), `SYNTHOSSynBridgeMinter`'s owner-gated functions (pause/unpause, relayer-set changes) end up controlled by the governance timelock, not the deploying wallet. Only a local Hardhat/devnet deploy leaves the deployer with direct control.
- **The minter starts paused on real networks.** `approveMint` will revert until a governance proposal unpauses it. Both relayers print a warning on startup if they detect `paused() == true`, but they'll keep retrying against a paused contract rather than failing loudly — don't mistake "relayer running" for "relayer working" on a fresh deploy.
- **`CANCELLER_ROLE`** on the timelock is granted to governance alongside `PROPOSER_ROLE` (fixed during this work — an earlier version of the deploy script left the deployer holding a lingering `CANCELLER_ROLE`). `post-deploy-check.js` verifies this; re-run it after any timelock-related deploy-script change.
- **On real networks, the deployer needs SYN before funding a sale/DEX pool.** Unlike local devnet (which can bridge-mint the SYN side of these directly for testing), a real deploy requires the deployer to already hold bridge-minted SYN, or the scripts will throw rather than silently minting anything outside the bridge.

Always run, in order, after deploying: `readiness-check.js` (contract-level invariants) then `post-deploy-check.js` (live deployed-state checks).

## Step 3: Run the Go relayer (EVM burn → native release)

`cmd/bridgerelayer` in `watch-evm` mode watches both the bridge vault and `SYNTHOSSynBridgeMinter` for burn events and submits matching native releases, automatically converting the 18-decimal wrapped amount down to the native chain's zero-decimal SYN.

**Build and run:**

```
go build -o bridgerelayer ./cmd/bridgerelayer
./bridgerelayer -mode watch-evm \
  -rpc $SYNTHOS_NATIVE_RPC_URL \
  -evm-rpc $SYNTHOS_BRIDGE_EVM_RPC_URL \
  -evm-vault $SYNTHOS_BRIDGE_EVM_VAULT \
  -evm-syn-minter $SYNTHOS_BRIDGE_EVM_SYN_MINTER \
  -priv $SYNTHOS_BRIDGE_AUTHORITY_PRIVATE_KEY
```

Every flag has a matching environment variable, so a `.env`/systemd-style setup works just as well as flags:

| Flag | Env var | Purpose |
| --- | --- | --- |
| `-mode` | `SYNTHOS_BRIDGE_RELAYER_MODE` | set to `watch-evm` for this direction |
| `-rpc` | `SYNTHOS_NATIVE_RPC_URL` | native chain RPC |
| `-evm-rpc` | `SYNTHOS_BRIDGE_EVM_RPC_URL` | EVM RPC |
| `-evm-vault` | `SYNTHOS_BRIDGE_EVM_VAULT` | bridge vault contract address |
| `-evm-syn-minter` | `SYNTHOS_BRIDGE_EVM_SYN_MINTER` | `SYNTHOSSynBridgeMinter` address |
| `-from-block` | `SYNTHOS_BRIDGE_EVM_FROM_BLOCK` | vault log scan start block |
| `-syn-minter-from-block` | `SYNTHOS_BRIDGE_EVM_SYN_MINTER_FROM_BLOCK` | minter log scan start block |
| `-min-confirmations` | `SYNTHOS_BRIDGE_MIN_CONFIRMATIONS` | EVM confirmations before acting on a burn |
| `-auto-submit-native` | `SYNTHOS_BRIDGE_AUTO_SUBMIT_NATIVE` | submit releases automatically vs. log only |
| `-poll` | `SYNTHOS_BRIDGE_POLL_INTERVAL` | polling interval |
| `-once` | `SYNTHOS_BRIDGE_ONCE` | run a single pass and exit (useful for testing/cron) |
| `-priv` | `SYNTHOS_BRIDGE_AUTHORITY_PRIVATE_KEY` | native release-authority private key |

Run `go test ./cmd/bridgerelayer/...` before pointing it at anything real — it covers the decimal-conversion and log-decoding logic this relayer depends on.

## Step 4: Run the Node.js relayer (native lock → EVM mint)

`contracts/scripts/relay-native-locks-to-syn-mint.js` polls the native chain for `bridge_lock_native` events, verifies each one actually paid into the configured escrow address, and calls `approveMint` on `SYNTHOSSynBridgeMinter` with the amount scaled up to 18 decimals.

**Run:**

```
cd contracts
npm run relayer:native-to-syn-mint
```

(or `node scripts/relay-native-locks-to-syn-mint.js` directly)

**Required environment variables:**

| Env var | Purpose |
| --- | --- |
| `SYNTHOS_BRIDGE_NATIVE_LOCK_ADDRESS` | native escrow address a lock must pay into to be treated as real backing |
| `SYNTHOS_BRIDGE_EVM_RPC_URL` | EVM RPC endpoint |
| `SYNTHOS_BRIDGE_EVM_SYN_MINTER` | `SYNTHOSSynBridgeMinter` address |
| `SYNTHOS_BRIDGE_EVM_RELAYER_PRIVATE_KEY` | EVM private key of an address authorized as a relayer on the minter |

**Optional (defaults shown):**

| Env var | Default |
| --- | --- |
| `SYNTHOS_NATIVE_RPC_URL` | `http://127.0.0.1:8080` |
| `SYNTHOS_BRIDGE_POLL_INTERVAL_MS` | `15000` |
| `SYNTHOS_BRIDGE_ONCE` | unset (loop forever); `"true"` runs one pass and exits |
| `SYNTHOS_BRIDGE_NATIVE_LOCK_STATE_FILE` | `.synthos/native-lock-mint-state.json` |

The state file tracks which native lock event ids this relayer instance has already handled, so a restart doesn't resubmit; the contract itself also refuses a duplicate `sourceEventId`, which is what lets multiple independent relayer instances converge on quorum without double-minting.

Run `npx hardhat test` in `contracts/` (61 tests, including 24 for this relayer specifically — library-level and full integration against a live local network) before pointing it at anything real.

## Step 5: Running both relayers continuously

The two relayers are independent processes; run them separately, and run more than one instance of each (with different relayer keys) once you're relying on quorum rather than a single trusted signer.

**For local testing:** two terminals, or a simple process manager like `pm2` / `tmux`, running the Go binary and `npm run relayer:native-to-syn-mint` respectively. Both support `-once`/`SYNTHOS_BRIDGE_ONCE=true` for a single pass, which is useful for driving them from a test script or a coarse cron job instead of a long-lived loop.

**For production:** run each as its own systemd service (or equivalent — a container, a supervisor process) with:

- `Restart=on-failure` so a transient RPC error doesn't take the relayer down permanently.
- Secrets (the private keys) injected via the service manager's secret/environment mechanism, never written into the unit file or a world-readable `.env`.
- Logs shipped somewhere durable — both relayers log every mint/release attempt and outcome, which is your audit trail if something needs to be reconciled later.
- Independent restart/monitoring per relayer, since one direction failing (e.g. the minter is paused pending a governance vote) shouldn't be confused with the other direction being down.

## Security checklist

**Key handling**

- Relayer private keys (native and EVM) are hot signing keys by construction — they need to sign automatically, unattended. Isolate them from any key that also controls funds, treasury, or governance.
- Never commit a key, put one in a tracked `.env`, or paste one into chat/logs.
- Rotate a key immediately if the machine running its relayer is ever compromised, and remove it from the minter's authorized-relayer set at the same time.

**Threshold recommendations**

- Run enough independent relayer instances, on separate infrastructure, that no single compromised machine can reach quorum alone. A threshold of 1 means any one relayer key can mint/release on its own — fine for local testing, not for anything holding real value.
- Independent here means independent: different operators or at least different hosts/keys/network paths, not just separate processes on the same box.

**Blast radius if a relayer key is compromised**

- *Go relayer (EVM burn → native release) key*: can sign fraudulent native releases up to whatever the native release-authority threshold allows from one signer. It cannot mint wrapped SYN on EVM and cannot touch `SynCoin`'s bridge-only minting invariant.
- *Node relayer (native lock → EVM mint) key*: can call `approveMint` on the minter, but `isMintableNativeLock`'s recipient check means it can only act on locks that actually reach your configured native escrow address — a compromised key can't fabricate a lock, only push through minting for an event the relayer software itself validated. It's still bounded by the minter's own quorum threshold, same as above.
- Neither relayer key can change contract configuration, pause state, or the relayer/threshold set — those stay behind the timelock/governance, not the relayer keys.

## Testing on testnet first

Recommended order before either relayer ever touches mainnet:

1. **Local Hardhat + local native devnet.** Deploy contracts locally, run both relayers with `-once`/`SYNTHOS_BRIDGE_ONCE=true` against a manually triggered lock and a manually triggered burn. Confirm the decimal conversion is exact in both directions (whole native SYN ↔ 18-decimal wrapped SYN) and that balances match by hand-checking the numbers, not just "no error thrown."
2. **Public EVM testnet + your native testnet.** Deploy for real, confirm the minter comes up paused and owned by the timelock as expected, run the governance unpause flow end-to-end, then run both relayers continuously (not `-once`) for at least a full day watching logs for repeated retries or silent stalls.
3. **Adversarial checks on testnet**, before trusting the relayers with anything real:
   - Submit a native transfer to your escrow address tagged with lock-like metadata but sent by an unrelated wallet — confirm the Node relayer's recipient check still requires funds to have actually landed at the escrow address, and rejects anything that doesn't.
   - Kill and restart each relayer mid-poll — confirm no double-mint/double-release on restart (state file + on-chain duplicate-id rejection both hold).
   - Run two relayer instances on the same direction simultaneously — confirm they converge on the same `sourceEventId` and only one mint/release actually executes.
4. **Small real-value pilot on mainnet** before removing any amount caps or opening the bridge to the public, if your rollout plan has one.

Only after all of the above pass cleanly should the relayer keys' EVM addresses be added to the production authorized-relayer set.

## Troubleshooting

| Symptom | Likely cause | Fix |
| --- | --- | --- |
| `approveMint`/release reverts on every attempt | `SYNTHOSSynBridgeMinter` is paused (normal on a fresh real-network deploy) | Check `paused()`; submit/pass the governance proposal to unpause. Both relayers log a warning about this at startup. |
| Relayer logs "minted"/"submitted" but the recipient's balance doesn't change | Quorum threshold not yet met — your instance's approval was recorded but other relayers haven't approved yet | Confirm other relayer instances are running and reachable; check the minter's recorded approval count for that `sourceEventId` |
| Node relayer logs `skipped:not-mintable` for a lock you expect to be real | `event.recipient` doesn't match `SYNTHOS_BRIDGE_NATIVE_LOCK_ADDRESS`, wrong `destination_chain_id`, non-address `destination_recipient`, zero amount, or `asset_id !== "syn"` | Check the raw event from `/bridge/events`; this is `isMintableNativeLock` doing its job — don't relax the check, fix the lock's metadata or the relayer's configured escrow address |
| Amounts look off by a factor of 10^18 (or its inverse) | Decimal-scale bug — native SYN is 0-decimal, wrapped SYN is 18-decimal | Confirm you're using `wrappedAmountFromNativeUnits`/`nativeAmountFromWrappedSynUnits` rather than passing amounts through unscaled; this should only happen in custom code, not the shipped relayers |
| Go relayer never picks up a burn event | `-from-block`/`-syn-minter-from-block` set after the event's block, or `-min-confirmations` not yet satisfied | Lower the from-block to before the event, or wait for confirmations |
| Relayer won't start: missing env var error | A required env var wasn't set | Check the Required environment variables tables in Steps 3 and 4 |
| `npm install`/`npx hardhat` extremely slow or fails with `ENOTEMPTY` on a synced/mounted folder | Package installs are many-small-file operations that mounted/network folders handle poorly | Install into local disk (e.g. a scratch directory outside the synced folder) and symlink `node_modules` into place, rather than installing directly into the mounted folder |
| `git push` fails with no credentials in an automated/sandboxed shell | The environment has no stored GitHub credentials by design | Push from a shell that has your real GitHub credentials (e.g. your own terminal/PowerShell) |
