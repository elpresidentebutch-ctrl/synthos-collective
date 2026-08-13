# SYNTHOS Cross-Chain Bridge

This is the phase-one bridge design for SYNTHOS. It is a guarded bridge
foundation, not a mainnet-ready autonomous bridge.

## What exists now

`contracts/src/synthos/SYNTHOSBridgeVault.sol` provides an EVM-side bridge vault:

- approved ERC20 assets can be locked for outbound bridging;
- approved destination chains are enforced;
- every outbound lock emits a deterministic `BridgeLocked` event;
- inbound releases require a quorum of approved relayers;
- each source event can release funds only once;
- duplicate relayer approvals are rejected;
- unsupported assets and disabled chains are rejected;
- the bridge starts paused by default;
- the owner can pause/unpause, add chains, add assets, add relayers, and set the
  relayer threshold.

## Intended SYN bridge flow

### EVM to SYNTHOS native

1. User approves SYN to the bridge vault.
2. User calls `lock(asset, amount, synthosChainId, synthosRecipientBytes)`.
3. The bridge vault transfers SYN into custody.
4. The bridge vault emits `BridgeLocked`.
5. SYNTHOS bridge observers wait for the configured source-chain confirmations.
6. Observers submit the lock proof to the SYNTHOS native side.
7. SYNTHOS releases or credits native SYN to the recipient.

### SYNTHOS native to EVM

1. User burns or locks native SYN on SYNTHOS.
2. SYNTHOS emits or records a final source event.
3. Independent relayers observe the finalized source event.
4. Relayers call `approveRelease(...)` on the EVM bridge vault.
5. Once quorum is reached, the bridge vault releases pre-funded ERC20 SYN to the
   recipient.
6. The source event is marked processed forever.

## Safety model

The bridge is not based on a single trusted relayer. It requires relayer quorum.
The minimum production posture should be:

- at least 5 independent relayers;
- threshold of at least 3-of-5;
- ownership held by multisig or timelock governance;
- bridge paused until chains/assets/relayers are verified;
- per-chain confirmation requirements published;
- public monitoring for lock/release mismatch;
- withdrawal/rate limits before real value is bridged;
- external audit before mainnet liquidity.

## Current limitations

This phase does not yet include:

- native SYNTHOS-side burn/lock module;
- automated relayer service;
- validator-signed bridge proofs from SYNTHOS consensus;
- rate limits;
- emergency withdrawal delay;
- Merkle/light-client verification;
- website bridge UI;
- production deployment scripts.

Until those are complete, this bridge should be treated as testnet/devnet
infrastructure only.

## Test command

From `contracts/`:

```bash
npm test -- --grep SYNTHOSBridgeVault
```

On Windows PowerShell, use `npm.cmd` if script execution blocks `npm.ps1`:

```powershell
npm.cmd test -- --grep SYNTHOSBridgeVault
```

## Deployment rehearsal

The deploy script intentionally deploys the bridge paused.

```bash
BRIDGE_RELAYERS=0xRelayer1,0xRelayer2,0xRelayer3 \
BRIDGE_THRESHOLD=2 \
BRIDGE_ASSET=0xSynToken \
BRIDGE_REMOTE_CHAIN_ID=20260702 \
npm run bridge:deploy -- --network baseSepolia
```

For Windows PowerShell:

```powershell
$env:BRIDGE_RELAYERS="0xRelayer1,0xRelayer2,0xRelayer3"
$env:BRIDGE_THRESHOLD="2"
$env:BRIDGE_ASSET="0xSynToken"
$env:BRIDGE_REMOTE_CHAIN_ID="20260702"
npm.cmd run bridge:deploy -- --network baseSepolia
```

Do not unpause a deployed bridge until the token address, relayers, threshold,
remote chain ID, and monitoring are verified.
