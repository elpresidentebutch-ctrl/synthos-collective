# Website-to-L1 Implementation Plan

This document maps the public website promises at `www.ishamwilliamsblockchains.com`
to backend and L1 implementation work in this repository.

## Website Vision

The website describes SYNTHOS Collective as an agent-native L1 and distributed
immune system for data sovereignty. The core public commitments are:

- Immune nodes that activate for free, bind to operator hardware, and run with
  zero inbound ports.
- A cloudless or outbound-only network where nodes coordinate without a
  centralized application backend.
- Proof of Immunity / DMAS-style consensus using Ed25519 signatures, validator
  attestations, and 2/3 stake-weighted finality.
- SYN token mechanics for staking, governance, rewards, slashing, and DEX fuel.
- A DEX with constant-product AMM pools, 0.3% fees, liquidity provision, and
  live pool data.
- Operator UX for join, login, node activation, validator status, governance,
  network status, documentation, API reference, and SDK access.
- Agent-native modules for threat detection, collective response, governance,
  communication, simulation, enforcement, and citizen participation.

## Current Repository Fit

Already present or partially present:

- Go chain core: accounts, signed transactions, blocks, state roots, mempool,
  DEX state, and persistence.
- Consensus concepts: block proposals, votes, finality threshold, and slashing
  primitives.
- RPC node: health/status, balances, mempool, block proposal, DEX pool/quote/swap
  endpoints.
- Validator/immune node concepts: desktop/silent node entrypoints, mobile and
  desktop validator workers, relay transport, peer registry worker, and
  serverless validator experiments.
- Contracts: SYN/SynCoin, adopter rewards, vesting, DEX, deployment scripts, and
  local deployment snapshots.
- Frontend/static pages: DEX, chain status, mobile validator, desktop validator,
  and agent UI pieces.

## Gaps To Close

### 1. Canonical Backend API

Build one supported API surface for the website and tools:

- `GET /health`
- `GET /status`
- `GET /network/status`
- `GET /validators`
- `GET /validators/:id`
- `POST /join`
- `POST /login`
- `POST /nodes/activate`
- `POST /nodes/heartbeat`
- `GET /governance/proposals`
- `POST /governance/proposals`
- `POST /governance/votes`
- `GET /dex/pools`
- `GET /dex/quote`
- `POST /dex/swap`
- `GET /docs/api`

### 2. Node Activation

Make the website's "Activate Immune Node" flow real:

- Generate or import an Ed25519 keypair.
- Derive an operator/node ID from the public key.
- Store a local encrypted vault for node identity.
- Register the node with the chain or peer registry.
- Start outbound-only heartbeat messages.
- Display active/inactive status and last proof time.

The hardware-binding story should be implemented as a local encrypted device
attestation, not as invasive fingerprinting. Operators should understand what is
stored and be able to back it up.

### 3. Consensus Proof

Turn the L1 from prototype to demonstrable devnet:

- Add a deterministic devnet health check.
- Start N validators.
- Submit a signed transaction.
- Propose a block.
- Collect attestations.
- Finalize at 2/3+ threshold.
- Verify all nodes converge on height, block hash, and state root.
- Restart nodes and prove state recovery.

This should become the baseline "yes, this L1 works" test.

### 4. Stake Weighting And Governance

Website claims require stake-weighted mechanics:

- Validator set derived from staked SYN.
- 32 SYN minimum validator stake, if that remains the chosen parameter.
- Weighted attestation counting.
- Proposal deposits.
- Emergency and standard voting windows.
- Slashing records and appeal/inspection history.

### 5. DEX Backend

The public DEX page says no pools are available, while the repo has local DEX
and contract pool work. Close that gap:

- Serve real pool list from local chain or contract deployment.
- Support quotes with transparent fee and slippage.
- Support swaps in local/dev mode.
- Separate native L1 swaps from ERC-20 smart contract swaps.
- Publish config for frontend wallet/RPC connection.

### 6. Network Status And Validators

The website needs live status pages:

- Active validators.
- Current height.
- Latest finalized block.
- State root.
- Peer/relay health.
- Heartbeat/proof counts.
- DEX pools.
- Governance proposals.

### 7. Agent Runtime

Map the seven agent roles into concrete modules:

- Validator: block verification and attestation.
- Economist: rewards, fees, emissions, and staking.
- Governor: proposals and vote execution.
- Communicator: peer messages and registry/relay protocols.
- Simulator: dry-run proposals and network changes.
- Enforcer: slashing and rule enforcement.
- Citizen: operator dashboard and participation layer.

### 8. Safety Boundary

The public site uses strong language around data sovereignty and cryptographic
noise. Implementation should stay inside lawful, consent-based, defensive
privacy tooling:

- Local-only privacy simulation is acceptable.
- Operator-owned data and browser storage are acceptable with consent.
- Do not build tools that attack, degrade, bypass, or poison third-party systems.
- Threat detection should focus on the SYNTHOS network itself unless a user has
  explicit authorization for an external environment.

## Build Order

1. Create a local L1 health check command that proves block finality and state
   convergence.
2. Make `cmd/rpcnode` expose the website-facing status, validator, DEX, and node
   activation endpoints.
3. Wire `THE_COLLECTIVE_DEX_LIVE.html` and status pages to the local RPC API.
4. Implement persistent node identity and outbound heartbeat.
5. Add stake-weighted validator selection and attestation counting.
6. Add governance proposals/votes and status pages.
7. Add SDK/API docs generated from the actual RPC routes.
8. Prepare a testnet launch runbook with exact commands and pass/fail checks.

## First Proof Milestone

The first local proof command now exists:

```powershell
go run ./cmd/l1check
```

It verifies the L1 kernel by:

- Starting four validator ledgers from one genesis.
- Submitting a real Ed25519-signed SYN transfer.
- Building a deterministic candidate block.
- Collecting 2/3+ validator attestations.
- Finalizing the block on every validator.
- Confirming height, tip, state root, and balances converge.
- Saving and reloading finalized state from disk.

This is not yet a public production network, but it is the first executable
proof that the local L1 ledger and finality path can work end-to-end.

## Definition Of Production Working L1

SYNTHOS should not be called a production working L1 until this command passes
reliably against real node processes and network transport:

```powershell
go run ./cmd/l1check
```

Expected proof:

- Starts at least four validators.
- Submits at least one signed SYN transfer.
- Finalizes at least one block with 2/3+ validator attestations.
- Confirms all validators have the same height, tip, and state root.
- Restarts from disk and preserves the finalized state.
- Prints a JSON summary that the website can display.
