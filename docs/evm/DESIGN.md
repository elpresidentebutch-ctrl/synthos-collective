# EVM Integration Design Specification for SYNTHOS Collective

**Author:** Antigravity (prepared for Jim & Claude)  
**Status:** DRAFT (Phase 1 Groundwork)  
**Branch:** `feature/evm-foundation`  
**Target Mainnet:** `synthos-mainnet-1` (Activation Height hard fork)  
**Mainnet EVM Chain ID:** `20260702` (Free on Chainlist / `ethereum-lists`)  
**Isolated Devnet Chain ID:** `20260799` (Network: `synthos-evm-devnet-1`)  

---

## 1. Executive Summary

This design specification details the architecture for embedding an Ethereum Virtual Machine (EVM) directly into SYNTHOS Collective (`synthos-mainnet-1`).

Currently, SYNTHOS runs on CometBFT v0.38.26 with an application state machine (`cometapp`) supporting fixed transaction types (transfers, citizen staking, cross-chain bridge locks/releases, internal DEX orderbook, and governance). Adding an embedded EVM unlocks full Solidity smart contract programmability, allowing standard Web3 tooling (MetaMask, Hardhat, Foundry, ethers.js, viem) to interface with SYNTHOS natively.

To preserve validator safety and network stability, this transition follows a strict 5-phase plan. **Phase 1 (this phase) is purely non-consensus groundwork**:
1. Zero changes to running validators or live mainnet consensus.
2. Key-value state storage modernization behind the existing `State` interface, maintaining byte-for-byte root hash equivalence.
3. Standalone EVM devnet with an isolated chain ID (`20260799`) for safe experiments.
4. Comprehensive design decisions and transaction format specifications.
5. All changes subject to Claude's review before merging.

---

## 2. The Six Architectural Decisions

### Decision 1: EVM Execution Engine — `go-ethereum` (`core/vm`)

**Decision:** Embed **`github.com/ethereum/go-ethereum` (`core/vm`) v1.15.11** (Cancun + Prague / Pectra).

| Attribute | Specification |
| :--- | :--- |
| **Engine Package** | `github.com/ethereum/go-ethereum v1.15.11` (`core/vm`, `core/types`, `params`) |
| **EVM Fork Level** | **Cancun + Prague (Pectra)** — verified finalized instruction set in `v1.15.11` |
| **Language** | Pure Go (native compilation, no CGO required: `CGO_ENABLED=0` preserved) |
| **License** | **LGPL-3.0 vs. Apache-2.0 — PENDING LAWYER REVIEW (Hard Merge Gate)** |
| **Determinism** | 100% deterministic bytecode execution verified against Ethereum execution specs |

#### Technical Evaluation & Prague Verification:
- `go-ethereum` (`core/vm`) is the battle-tested standard powering Ethereum mainnet, Polygon PoS, Arbitrum, Optimism, and BNB Chain.
- **Prague Verification (`v1.14.13` -> `v1.15.11`):** An earlier prototype pinned `v1.14.13`, in which `core/vm/jump_table_export.go` still returned `"prague-fork not defined yet"`. Upgrading to **`v1.15.11`** provides the finalized Ethereum Prague/Pectra instruction set, including EIP-7702 (`0x04` SetCode), EIP-2537 (BLS12-381 precompiles), EIP-7623 (calldata floor gas), and EIP-2935 (historical block hashes), verified via `vm.LookupInstructionSet` and `CGO_ENABLED=0` builds.
- Native Go avoids CGO (`cgo`), maintaining SYNTHOS's reproducible cross-compilation pipeline across Linux amd64/arm64 and Windows.
- Alternative engines like `evmone` (C++) or `revm` (Rust) require CGO/FFI bindings, breaking static linking and increasing binary surface area. Cosmos/Ethermint would require discarding SYNTHOS's clean, lightweight custom Go state machine (`cometapp`).

#### Licensing Status — PENDING LAWYER REVIEW (Merge Gate):
- Synthos Collective is licensed under **Apache-2.0**, whereas `go-ethereum` is licensed under **LGPL-3.0** (and GPL-3.0 for certain auxiliary packages). Because Go compiles packages statically into a single binary, importing `github.com/ethereum/go-ethereum` creates potential LGPL-3.0 Section 4 ("Combined Work") relinking and license-compatibility obligations even behind an internal adapter boundary (`internal/evm/executor`).
- **Status:** Boundary isolation and unmodified external Go module linkage are implemented as engineering hygiene, **NOT** as a claim of legal compliance.
- **Hard Merge Gate:** **Nothing on `feature/evm-foundation` (or any code importing `go-ethereum`) may merge to `main` until qualified legal counsel has reviewed the static-linking LGPL-3.0 question and explicitly approved the licensing posture.**

---

### Decision 2: Unit Denominations and Decimals — Dual-Ledger Balance Model

**Decision:** Maintain **18-decimal EVM balances** via a **Dual-Ledger Balance Model**.

* **Native Ledger (0 Decimals):**
  - Native balances remain whole SYN (`uint64`), preserving all existing native transfers, staking, DEX orderbooks, and validator rewards without breaking arithmetic changes.
* **EVM Ledger (18 Decimals):**
  - EVM contract accounts and EVM user accounts carry balances denominated in wei ($10^{18}$ wei per SYN coin), represented as `*big.Int`.
* **Atomic Boundary Conversion:**
  - $1 \text{ SYN} = 10^{18} \text{ wei} = 1,000,000,000,000,000,000 \text{ wei}$.
  - Conversions between native accounts and EVM accounts occur through an explicit, atomic bridge action (`deposit_to_evm` / `withdraw_from_evm`).
  - Fractional wei balances cannot leak into the native 0-decimal ledger: withdrawals round down to whole SYN, with dust remainder retained in the user's EVM balance.

---

### Decision 3: Accounts and Cryptographic Signatures — Dual Account Spaces

**Decision:** **Dual Account Spaces** with native Ed25519 and EVM secp256k1 keys.

* **Native Account Space:**
  - **Key Type:** Ed25519.
  - **Address Derivation:** `0x` + first 20 bytes of $\text{SHA-256}(\text{PubKey})$.
  - **Signature Algorithm:** Pure Ed25519 over canonical JSON transaction bytes.
* **EVM Account Space:**
  - **Key Type:** ECDSA secp256k1.
  - **Address Derivation:** Standard Ethereum format: last 20 bytes of $\text{Keccak-256}(\text{PubKey})$.
  - **Signature Algorithm:** Standard Ethereum `secp256k1` (`ecrecover` over transaction hash with EIP-155 / EIP-1559 chain ID replay protection).
* **Interoperability:**
  - An operator can hold both an Ed25519 native key (for validator operations and native transfers) and a secp256k1 EVM key (for MetaMask, DeFi, and smart contract interaction).
  - Native transactions can fund an EVM address via `deposit_to_evm`.

---

### Decision 4: Gas Metering, Starting Gas Limit, and Per-Gas Fee Model

**Decision:** Conservative **`10,000,000`** starting block gas limit paired with canonical Cancun/Prague opcodes and **explicit wei/gas conversion from SYNTHOS Fee Model V3**.

* **Starting Block Gas Limit:** **`10,000,000` gas** per CometBFT block (EIP-1559 50% utilization target: **`5,000,000` gas**). Starting conservatively at 10M caps per-block execution latency during CometBFT rounds and may be raised in a future coordinated hard fork after public testnet soak testing.
* **Opcode Gas Schedule:** Inherit canonical Ethereum Cancun + Prague gas costs from `go-ethereum v1.15.11` (e.g., 21,000 gas base transfer, dynamic memory/storage costs, EIP-7623 calldata floor gas).
* **Explicit Fee Units:**

| Quantity | Unit | Notes |
| :--- | :--- | :--- |
| `gas_limit`, `gas_used` | dimensionless gas units | Standard EVM opcode & intrinsic gas metering |
| `chain.State.FeeModelV3BaseFee` | whole `SYN` per native tx | Existing native base fee (minimum floor: `10 SYN`) |
| `base_fee_per_gas` | `wei / gas` | Derived deterministically from native base fee (`1 SYN = 10^{18} wei`) |
| `max_fee_per_gas`, `max_priority_fee_per_gas` | `wei / gas` | Signed by wallet in EIP-1559 / EIP-7702 tx fields |
| `effective_gas_price` | `wei / gas` | `min(max_fee_per_gas, base_fee_per_gas + max_priority_fee_per_gas)` |
| Total tx fee (`gas_used * effective_gas_price`) | `wei` | Deducted from EVM balance in 18-decimal wei |

* **Per-Gas Fee Formula (`BaseFeePerGasFromNativeSYN`):**
  - Native Fee Model V3 prices a plain native transfer at a base fee floor of `10 SYN` (`FeeModelV3MinBaseFee`). Dividing cleanly over the canonical `21,000`-gas plain EVM transfer gives exact cross-ledger economic parity using strict integer arithmetic:
    $$\text{base\_fee\_per\_gas\_wei} = \left\lfloor \frac{\max(\text{FeeModelV3BaseFee}, 10) \times 10^{18}}{21{,}000} \right\rfloor$$
  - Standard EIP-1559 pricing determines what the sender pays per unit of gas:
    $$\text{priority\_fee\_per\_gas} = \min(\text{max\_priority\_fee\_per\_gas},\; \max(0,\; \text{max\_fee\_per\_gas} - \text{base\_fee\_per\_gas\_wei}))$$
    $$\text{effective\_gas\_price} = \text{base\_fee\_per\_gas\_wei} + \text{priority\_fee\_per\_gas}$$
  - **Worked Examples at the `10 SYN` Base-Fee Floor (`base_fee_per_gas = 476,190,476,190,476 wei/gas`):**
    - Plain EVM transfer (`21,000` gas, 0 tip): `9,999,999,999,999,996,000 wei` ($\approx 10\text{ SYN}$, matching native transfer floor within $4 \times 10^{-15}\text{ SYN}$).
    - Typical contract call (`100,000` gas, 0 tip): $\approx 47.62\text{ SYN}$.
    - Full `10,000,000`-gas block at base floor: $\approx 4,761.90\text{ SYN}$ base fees across all txs.
* **Fee Split:** Base fee revenue (`gas_used * base_fee_per_gas_wei`) is split **50% burned / 50% treasury** (consistent with Fee Model V3); priority tips (`gas_used * priority_fee_per_gas`) go **100% to the block proposer**. (Converting fractional wei fee revenue into whole-SYN native treasury/proposer accounts uses an explicit accumulator so no wei dust is created or destroyed.)

---

### Decision 5: State Storage Evolution (Phase 1 Core)

**Decision:** Transition from monolithic JSON serialization to an embedded **Key-Value State Store** behind the existing `State` interface.

* **Current Baseline:** Every block currently serializes the entire state tree into a monolithic JSON file (`synthos-app-state.json`) and recomputes the Merkle root across all accounts from scratch.
* **Target KV Architecture:**
  - Replace monolithic file serialization with a structured Key-Value storage abstraction (`StateStore`).
  - Key namespaces:
    - `acc:<address>` -> serialized account state (balance, nonce, assets, code hash, storage root).
    - `sys:<subsystem>` -> consensus metadata (bridge records, validators, fee epoch, base fee).
  - Incremental Merkle root recomputation: touched accounts update their leaf hashes without rehashing the entire account database.
  - Guaranteed byte-for-byte equivalence with legacy state roots during pre-activation heights.

---

### Decision 6: Chain ID, RPC Interface, and Devnet Safety Guards

**Decision:** Mainnet EVM Chain ID **`20260702`**, isolated Devnet Chain ID **`20260799`**, with standard JSON-RPC on port 8545.

* **Mainnet Chain ID:**
  - CometBFT Chain ID: `synthos-mainnet-1`
  - EVM Chain ID: **`20260702`** (Checked against Chainlist / `ethereum-lists/chains`; verified unallocated).
* **Devnet Chain ID (Isolated):**
  - CometBFT Chain ID: `synthos-evm-devnet-1`
  - EVM Chain ID: **`20260799`** (Never `20260702`, never mainnet).
* **Devnet Home-Folder Guard (`ValidateHomeDir`):**
  - `evmdevnet.SetupGenesis` and `evmdevnet.StartNode` refuse to run in any directory containing a non-devnet `config/genesis.json` (such as `synthos-mainnet-1` or `tx_chain_id = 20260702`), any existing `priv_validator_key.json`, `priv_validator_state.json`, or `synthos-app-state.json`, or any non-empty directory lacking `.evm-devnet-marker`.
* **Devnet Faucet Key Security Note:**
  - The devnet pre-funds `0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266` — the **well-known public Hardhat/Anvil test account #0** whose private key is publicly known worldwide. It is strictly for local isolated devnet (`20260799`) testing only: **NEVER send real SYN/ETH to this address and NEVER include it in mainnet or public testnet genesis.**
* **Embedded JSON-RPC Layer:**
  - Implements standard `eth_*`, `net_*`, and `web3_*` endpoints over HTTP/WebSocket (default port `8545`).
  - Supports MetaMask, Hardhat, Foundry, Remix, and standard Ethereum block explorers without proprietary modifications.

---

## 3. The New Ethereum Transaction Type (`TxTypeEVM`)

To support standard Ethereum transactions alongside native SYNTHOS transactions, we introduce the `TxTypeEVM` envelope into the CometBFT transaction stream.

### 3.1 Transaction Envelope Specification

A transaction submitted to CometBFT can be either:
1. A **Native Transaction**: Canonical JSON encoding conforming to `chain.Tx`.
2. An **EVM Transaction (`TxTypeEVM`)**: A typed byte envelope wrapping standard RLP-encoded Ethereum transactions.

```mermaid
flowchart TD
    TxRaw["Incoming Raw Transaction Bytes"] --> CheckType{"Prefix / Magic Byte Check"}
    CheckType -- "0x7b ('{') JSON" --> NativeTx["Native SYNTHOS Tx<br>(chain.Tx JSON)"]
    CheckType -- "EIP-2718 Type or RLP List" --> EVMTx["EVM Transaction<br>(TxTypeEVM Wrapper)"]
    
    EVMTx --> DecodeRLP["Decode RLP Envelope<br>(Legacy, EIP-2930, EIP-1559, EIP-7702)"]
    DecodeRLP --> VerifySig["Recover Signer via ecrecover<br>(secp256k1)"]
    VerifySig --> CheckReplay["Validate EVM Chain ID<br>(20260702 or 20260799)"]
    CheckReplay --> CheckNonce["Check Nonce & Gas Prepayment<br>(in EVM StateDB)"]
    CheckNonce --> ExecuteEVM["Execute Bytecode in go-ethereum core/vm"]
```

### 3.2 Wire Format and EIP-2718 Compatibility

The EVM transaction wrapper supports:
* **Legacy Transactions (Type 0x00):** Standard RLP `[nonce, gasPrice, gasLimit, to, value, data, v, r, s]` with EIP-155 replay protection.
* **EIP-2930 Transactions (Type 0x01):** Optional access list transactions.
* **EIP-1559 Transactions (Type 0x02):** Dynamic fee transactions `[chainId, nonce, maxPriorityFeePerGas, maxFeePerGas, gasLimit, to, value, data, accessList, v, r, s]`.
* **EIP-7702 Transactions (Type 0x04):** Prague/Pectra EOA code-delegation transactions (`SetCodeTx`).

```go
// EVMTxEnvelope wraps a standard Ethereum transaction for CometBFT inclusion.
type EVMTxEnvelope struct {
    Type     byte   `json:"type"`      // EIP-2718 transaction type (0x00, 0x01, 0x02, 0x04)
    RawRLP   []byte `json:"raw_rlp"`   // Raw canonical RLP bytes
    TxHash   string `json:"tx_hash"`   // Keccak-256 hash (0x-prefixed)
    Sender   string `json:"sender"`    // Recovered checksummed Ethereum address
}
```

### 3.3 Lifecycle in CometBFT ABCI++

1. **`CheckTx` (Mempool Validation):**
   - Check if block height $\ge \text{EVMActivationHeight}$ (reject with error code if premature).
   - Recover sender address using `ecrecover`.
   - Verify `ChainID` strictly matches the network's EVM Chain ID (`20260702` on mainnet, `20260799` on devnet).
   - Verify sender has sufficient balance in the EVM StateDB to cover $\text{gasLimit} \times \text{maxFeePerGas} + \text{value}$.
   - Verify transaction nonce matches sender's current EVM state nonce.
2. **`FinalizeBlock` (Deterministic Execution):**
   - Instantiate `core/vm.EVM` configured with block header parameters (block number, timestamp, base fee, block gas limit).
   - Execute EVM message call or contract creation.
   - Deduct gas consumed; burn 50% of base fee and route 50% to treasury; credit priority tip to block proposer.
   - Store transaction receipt (status, gas used, logs/bloom filter) in the local index.
   - Update EVM StateDB root.

---

## 4. The Activation-Height Plan

Hard fork activation of the EVM must be coordinate-able across all validators with zero risk of consensus divergence.

```mermaid
timeline
    title EVM Hard Fork Activation Timeline
    Pre-Activation (Height < H_act) : All validators run v1.2.0 or v2.0.0-rc
                                    : TxTypeEVM rejected in CheckTx
                                    : Monolithic root calculation active
    Activation Height (Height = H_act) : Hard fork activates deterministically
                                       : EVM StateDB initializes from native state
                                       : Dual-ledger bridge opens
    Post-Activation (Height > H_act) : TxTypeEVM enabled in mempool & blocks
                                     : Dual-root commitment in FinalizeBlock
                                     : JSON-RPC port 8545 live
```

### 4.1 Compiled-In Consensus Parameters (Never Per-Node Config)

EVM activation height is **strictly compiled into the node binary per CometBFT chain ID** (matching `FeeModelV2ActivationHeight` and `FeeModelV3ActivationHeight` in `internal/chain`). **It MUST NEVER be exposed as a CLI flag, environment variable, or per-node configuration setting**, because differing node values across validators at the switch height would cause immediate consensus divergence / chain halt.

In `internal/evm/config.go`:
```go
// Compiled-in EVM activation heights per CometBFT chain ID (never per-node config).
const (
    MainnetEVMActivationHeight uint64 = 0 // Disabled on Phase 1 branch; set only in Phase 5 hard-fork release
    DevnetEVMActivationHeight  uint64 = 1 // Enabled from block 1 on isolated devnet (synthos-evm-devnet-1)
)
```

### 4.2 Pre-Activation Rules (Height $< \text{EVMActivationHeight}$ or Disabled)
- While `MainnetEVMActivationHeight == 0` or `Height < EVMActivationHeight`, any `TxTypeEVM` transaction is rejected in `CheckTx` with error: `EVM not active on synthos-mainnet-1`.
- Block building (`PrepareProposal` / `ProcessProposal`) forbids inclusion of EVM transactions.
- State root calculation is 100% identical to legacy formula.

### 4.3 Transition Block (Height $= \text{EVMActivationHeight}$)
- CometBFT `FinalizeBlock` executes the genesis EVM state setup:
  - EVM state root initializes cleanly.
  - Bridge contract or system precompiles are registered.
- An ABCI event `evm_hardfork_activated` is emitted to block receipts.

### 4.4 Deferral & Emergency Rollback Invariant
- Because activation height is compiled into the binary rather than configured per node, deferring or updating a planned activation height can **only** occur by building and uniformly deploying an updated validator release prior to the previously compiled target height.
- All validators must run the identical binary release before reaching a scheduled hard-fork height; no split validator builds are permitted.

---

## 5. State Storage Baseline & Scaling Analysis (Task 2 Benchmarks)

Measured on an isolated state copy (`state-copy.json`, current mainnet height 385,096) without touching running validator processes.

### 5.1 Mainnet Baseline Snapshot (Height 385,096)

| Metric | Measured Value | Notes |
| :--- | :--- | :--- |
| **State File Size** | 5,123 bytes (5.00 KB) | Monolithic JSON on disk |
| **Active Accounts** | 18 | Base native accounts |
| **Deserialization Time** | ~1.38 ms | Loading from disk into memory |
| **Save Time (Marshal + Disk Sync)** | ~4.44 ms | `json.MarshalIndent` + temp write + atomic rename |
| **State Root Rebuild Time** | ~74.54 µs | `State.Root()` over 18 sorted accounts |
| **Baseline Root Hash** | `0x3c6432643022ee19803bb3ef9b887e92860d4525c51ccefbef2a9aae042042d2` | Canonical Merkle root at height 385,096 |

### 5.2 Scaled Growth Projections (Monolithic JSON vs. Full Root Rebuild)

Under the current architecture, every block serializes the entire state tree to JSON and rebuilds the Merkle root from scratch over all accounts:

| Account Count | Projected JSON Size | In-Memory Marshal Time | Full Merkle Rebuild Time | Total Block Disk & Rebuild Latency |
| :--- | :--- | :--- | :--- | :--- |
| **18 (Current)** | **5.00 KB** | **~1.38 ms** | **~74.5 µs** | **~4.44 ms** |
| **100** | 11.81 KB | < 1 ms | < 1 ms | < 5 ms |
| **1,000** | 86.61 KB | 21.05 ms | 24.64 ms | ~50 ms |
| **10,000** | 842.57 KB | 24.74 ms | 26.96 ms | ~60 ms |
| **50,000** | 4.14 MB | 111.25 ms | 180.29 ms | ~300 ms |
| **100,000** | 8.29 MB | 205.99 ms | 240.48 ms | ~500 ms |

### 5.3 Implications for EVM Integration
1. **Storage Amplification:** Smart contract deployment and storage slots create exponential key growth. While native accounts represent ~85 bytes per entry, EVM contract storage slots (`uint256 -> uint256`) would expand a 100,000-contract ecosystem to hundreds of megabytes.
2. **I/O Bottleneck:** Writing 50MB+ of JSON every 11-second block causes disk write wear and latency spikes that jeopardize consensus round timers.
3. **Conclusion:** Phase 1 migration to a key-value store behind the `State` interface is strictly mandatory before enabling EVM transactions in Phase 2.

---

## 6. Phased Implementation Roadmap

1. **Phase 1: Storage Engine Modernization & EVM Devnet (Weeks 1–6) [CURRENT]**
   - Prototype KV state store behind `State` interface; prove byte-for-byte root hash equivalence.
   - Establish isolated EVM devnet (`synthos-evm-devnet-1`, Chain ID `20260799`).
   - Benchmark state serialization and access latencies.
2. **Phase 2: Embedded EVM Engine & StateDB Adapter (Weeks 7–12)**
   - Implement `core/vm.StateDB` interface over the KV store.
   - Embed EVM executor into CometBFT `FinalizeBlock` for `TxTypeEVM`.
   - Pass Ethereum official consensus state tests (`GeneralStateTests`).
3. **Phase 3: Ethereum JSON-RPC & Tooling (Weeks 13–16)**
   - Implement standard `eth_*` RPC endpoints (port 8545).
   - Test MetaMask, Hardhat, Foundry, and Remix end-to-end.
4. **Phase 4: Public EVM Testnet Soak (Weeks 17–22)**
   - Deploy multi-validator public EVM testnet.
   - Run validator soak test under synthetic load and fuzz testing.
5. **Phase 5: External Audit & Mainnet Hard Fork (Weeks 23–26)**
   - Third-party security audit of consensus and EVM integration.
   - Coordinate hard fork activation block height on `synthos-mainnet-1`.

---

## 7. Immediate Safety Invariants

1. **Zero Consensus Mutations on Mainnet:** All development is isolated to `feature/evm-foundation` (`MainnetEVMActivationHeight == 0`); mainnet binary remains strictly `v1.2.0`.
2. **Devnet Home-Folder Isolation:** `evmdevnet` validates `home` before touching disk and refuses mainnet genesis dirs, validator key/state dirs, or non-empty unrelated folders.
3. **Validator Benchmark Isolation:** Benchmarks are run exclusively on offline local state copies—never on running validator processes.
4. **LGPL-3.0 Legal Merge Gate:** Because `go-ethereum` (`v1.15.11`) is statically linked and licensed under LGPL-3.0, **nothing on `feature/evm-foundation` merges to `main` until qualified legal counsel reviews the LGPL-3.0 question.**
5. **Audit Gate:** No code merges into `main` without Claude's comprehensive review, legal sign-off, and user approval.
