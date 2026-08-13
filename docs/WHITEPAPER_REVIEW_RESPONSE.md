# SYNTHOS Whitepaper Review Response

This document captures launch-blocking issues raised during review and the
language the public whitepaper/site should use until the implementation catches
up.

## 1. Relay / R2 / cloudless contradiction

Definition to include prominently in the whitepaper:

> Proof-of-Operation is a SYNTHOS-specific bootstrap eligibility system. It is
> not the consensus algorithm. It proves that an operator registered a node,
> controls the node key, and kept sending valid heartbeat proofs over time.

If any design uses Cloudflare R2, a single Worker, one Render service, one
Supabase project, or any other single managed provider as the required message
path, it must be described as bootstrap infrastructure.

It is not accurate to call that design fully cloudless, un-DDoSable, or free of
single points of failure.

Public language should say:

> SYNTHOS is in Proof-of-Operation bootstrap. Nodes and heartbeats are signed
> and independently verifiable, but the current public registry/relay is a
> bootstrap service. The roadmap is to move consensus traffic to direct
> validator gossip with multiple independent relays used only for discovery,
> outbound-only nodes, and fallback mailboxes.

## 2. Required decentralization path

The migration path is:

1. one public bootstrap registry for onboarding;
2. multiple independent registries/relays;
3. direct validator-to-validator gossip as primary consensus transport;
4. relays used only for discovery and outbound-only participation;
5. public status pages showing relay diversity and validator peer reachability;
6. no public "cloudless" or "no single point of failure" claim until this is
   verified.

## 3. Immune-node sybil resistance

Free, permissionless, rewarded immune nodes create a sybil risk. Rewards must
not be paid for registrations alone.

Minimum rule:

- one standard reward stream per verified operator;
- rewards paid one month in arrears;
- signed heartbeat proofs required;
- duplicate endpoints, wallets, devices, and repeated submissions do not create
  duplicate reward rights;
- suspicious, replayed, incomplete, or false-positive reports can be rejected;
- bounties should require corroboration, evidence, or governance approval.

Public language should avoid suggesting that anyone can spin up unlimited immune
nodes and receive unlimited SYN.

Verified operation should be described mechanically:

- node keys are Ed25519;
- production private keys are generated locally or client-side;
- the backend registers only public keys;
- heartbeat messages use canonical `SYNTHOS_HEARTBEAT_V1` serialization;
- each heartbeat is signed by the node key;
- replayed or non-increasing nonces are rejected;
- stale heartbeat gaps do not count as uninterrupted operation;
- reward eligibility requires a complete reward epoch, currently one month;
- hosted bootstrap/provisioning sessions are onboarding aids only and do not
  qualify for rewards until rotated to real signed node operation.

## 4. Tokenomics

The public whitepaper should reference `docs/TOKENOMICS.md` and include, at
minimum:

- token name: SYNTHOS;
- token symbol: SYN;
- maximum supply: 100,000,000,000 SYN;
- fixed-supply policy unless changed by formal governance/legal process;
- allocation table;
- validator/security rewards: 12,000,000,000 SYN;
- immune node rewards: 22,000,000,000 SYN;
- minimum validator stake: 100,000 SYN;
- validator reward policy: 5,000 SYN/month base, up to 2,500 SYN/month bonus,
  paid monthly in arrears after verified uptime;
- no guaranteed income or investment return.

## 5. Execution layer

Until a production smart-contract VM or execution environment is specified and
implemented, the whitepaper should not imply broad arbitrary computation on the
L1.

Accurate wording:

> The current Go L1 implements a deterministic ledger/state-machine prototype,
> validator consensus, block storage, RPC, and proof/status flows. General
> purpose smart-contract execution is not yet a production L1 feature. EVM token,
> sale, vesting, and governance contracts are separate Solidity components.

## 6. Key custody

Production node private keys should be generated and stored client-side or in
local node software. The public backend should register public keys and verify
signed heartbeats. It should not ask operators to paste production private keys
or rely on server-side key generation for public validator custody.

Any legacy endpoint or compatibility path that generated keys server-side must
be labeled development/bootstrap only. Existing candidates from that path should
be flagged for key rotation before they can receive operator or validator
rewards.

## 7. Missing diligence materials

Before a serious public/mainnet launch, publish:

- threat model;
- testnet/mainnet status;
- actual live node count and verification method;
- audit status;
- benchmarks for throughput/finality;
- execution-layer scope;
- team/operator disclosures appropriate for the launch path;
- known limitations and migration roadmap.
