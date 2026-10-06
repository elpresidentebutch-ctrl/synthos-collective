# SYNTHOS Collective — Native Layer-1 Coin Exchange Integration Specification

**Confidential & Technical Integration Dossier for CEX & DEX Listing Teams**  
*(MEXC, Bitget, Gate.io, KuCoin, THORChain, Maya Protocol, Chainflip)*

---

## 1. Asset Executive Summary

| Parameter | Specification | Notes |
| :--- | :--- | :--- |
| **Asset Name** | SYNTHOS | Full project name: SYNTHOS Collective |
| **Asset Symbol / Ticker** | **SYN** | Native Layer-1 Coin |
| **Asset Type** | **Native Layer-1 Coin** | Sovereign BFT blockchain; **not** a smart-contract token |
| **EVM Bridge Token** | SynCoin (`SYN`) | Optional EVM mirror on Base/Ethereum via trust-minimized bridge |
| **Fixed Total Supply** | **100,000,000,000 SYN** | 100 Billion SYN fixed cap; hardcoded at genesis, no inflation minting |
| **Native Decimal Places** | **0** | Integer-denominated whole SYN on L1 ledger |
| **Bridge Decimal Places** | **18** | For EVM/ERC-20 wrapped synthetic representation |
| **Genesis Hash** | `0x0173b6a906e328e7ee4ef3efde46f7ceefa13fafbb22f3abd75607d9d22d927e` | Pinned immutable genesis |
| **Genesis State Root** | `0x19d2ad03ed52a28e5cd2302b898e6e950069a54da8b782c6a4ea3782cb447f49` | Merkle state root at height 0 |
| **Mainnet Chain ID** | `synthos-mainnet-1` | Numerical TX Chain ID: `20260702` |
| **Testnet Chain ID** | `synthos-testnet-1` | Numerical TX Chain ID: `20261002` |
| **Official Website** | [https://www.ishamwilliamsblockchains.com](https://www.ishamwilliamsblockchains.com) | Always include `www.` |
| **Official Explorer** | [https://www.ishamwilliamsblockchains.com/explorer](https://www.ishamwilliamsblockchains.com/explorer) | Live block, transaction, and address inspection |
| **Technical White Paper** | [https://www.ishamwilliamsblockchains.com/whitepaper](https://www.ishamwilliamsblockchains.com/whitepaper) | Comprehensive architecture & security proof |
| **Institutional Deck** | [https://www.ishamwilliamsblockchains.com/pitch-deck](https://www.ishamwilliamsblockchains.com/pitch-deck) | 12-slide institutional presentation |
| **Source Repositories** | GitHub: `elpresidentebutch-ctrl/synthos-collective`<br>GitLab: `synthos-collective-group/synthos-collective` | Dual-mirrored open source codebases |

---

## 2. Core Consensus & Ledger Architecture

SYNTHOS operates a sovereign Layer-1 ledger optimized for deterministic autonomous agent execution, cross-organization governance, and verifiable state transitions:

1. **Consensus Engine:** CometBFT Byzantine Fault Tolerant (BFT) consensus paired with a Go application state machine.
2. **Quorum & Finality:** 
   - 2/3+ voting power stake requirement for block commitment.
   - **Single-block deterministic finality**: Once a block is committed to the blockchain, it cannot be reorganized or orphaned. No probabilistic confirmations required.
   - Recommended Exchange Deposit Confirmations: **2 blocks** (safe buffer).
3. **Cryptography & Signature Schemes:**
   - **Ed25519**: High-performance elliptic curve used for validator voting, consensus signatures, and outbound immune node heartbeats.
   - **ECDSA (secp256k1)**: Standard EVM-compatible address generation (`0x...` 20-byte addresses) and transaction signing for user accounts and institutional custody systems.
4. **Network Topology (Dual-Tier):**
   - **Tier 1 (Public Validators & RPC Relays):** Active consensus nodes maintaining state sync, handling incoming JSON-RPC traffic, and producing blocks.
   - **Tier 2 (Silent Immune Node Fleet):** Outbound-only zero-inbound-port nodes performing decentralized integrity verification, state proof attestation, and watchdog monitoring without attack surface exposure.

---

## 3. Node Software & Custody Integration

### Node Binaries

- `synthosd`: Full validator and archival node daemon with JSON-RPC interface.
- `rpcnode`: Read/Write JSON-RPC node for external custody and exchange connectivity.
- `silentnode`: Lightweight background proof-of-uptime node client.

### Pre-Built Binaries & Docker Deployment

Exchanges can run a dedicated, isolated node via Docker or native Go binaries:

```bash
# Pull official exchange node image
docker pull registry.gitlab.com/synthos-collective-group/synthos-collective:latest

# Run synchronized mainnet node for exchange wallet operations
docker run -d \
  --name synthos-exchange-node \
  -p 8080:8080 \
  -p 26656:26656 \
  -v /var/data/synthos:/data \
  registry.gitlab.com/synthos-collective-group/synthos-collective:latest \
  synthosd start --data-dir=/data --network=mainnet
```

### Hardware Requirements for Exchange Nodes

- **CPU:** 4 vCPU cores (x86_64 or ARM64)
- **RAM:** 8 GB DDR4/DDR5
- **Storage:** 200 GB NVMe SSD (fast I/O for state database)
- **Network:** 100 Mbps symmetric connection

---

## 4. JSON-RPC API Specification

The SYNTHOS node exposes standard JSON-RPC 2.0 endpoints on port `8080` (or `https://rpc.ishamwilliamsblockchains.com`).

### 1. Get Node & Sync Status
```json
// Request
{
  "jsonrpc": "2.0",
  "method": "synthos_status",
  "params": [],
  "id": 1
}

// Response
{
  "jsonrpc": "2.0",
  "result": {
    "node_info": {
      "network": "synthos-mainnet-1",
      "version": "1.0.0",
      "channels": "40202122233038"
    },
    "sync_info": {
      "latest_block_hash": "0x7a89...",
      "latest_block_height": 63920,
      "latest_block_time": "2026-10-06T23:45:00Z",
      "catching_up": false
    }
  },
  "id": 1
}
```

### 2. Query Account Balance (Deposits)
```json
// Request
{
  "jsonrpc": "2.0",
  "method": "synthos_getBalance",
  "params": ["0x170D6650347ff4DaAC78B359e09C59a0e2D9758c", "latest"],
  "id": 2
}

// Response
{
  "jsonrpc": "2.0",
  "result": "500000000",
  "id": 2
}
```

### 3. Broadcast Signed Raw Transaction (Withdrawals)
```json
// Request
{
  "jsonrpc": "2.0",
  "method": "synthos_sendRawTransaction",
  "params": ["0xf86c...signed_hex..."],
  "id": 3
}

// Response
{
  "jsonrpc": "2.0",
  "result": "0x4b7c8932ef1248...",
  "id": 3
}
```

### 4. Query Transaction Receipt
```json
// Request
{
  "jsonrpc": "2.0",
  "method": "synthos_getTransactionReceipt",
  "params": ["0x4b7c8932ef1248..."],
  "id": 4
}

// Response
{
  "jsonrpc": "2.0",
  "result": {
    "transactionHash": "0x4b7c8932ef1248...",
    "blockHeight": 63922,
    "blockHash": "0x9812...",
    "from": "0xExchangeHotWallet...",
    "to": "0xUserDepositAddress...",
    "amount": "10000",
    "status": "0x1"
  },
  "id": 4
}
```

---

## 5. Cold Storage & Institutional Custody Compatibility

- **Key Generation:** Standard BIP-39 mnemonic phrases, BIP-32/BIP-44 derivation path `m/44'/60'/0'/0/x` (compatible with Ledger, Trezor, Fireblocks, BitGo, and standard KMS hardware modules).
- **Address Format:** Standard checksummed hex addresses prefixed with `0x` (20 bytes).
- **Offline Signing:** Fully supported. Unsigned transactions can be constructed via standard tooling, serialized to raw bytes, signed offline in cold storage environments, and broadcasted via the RPC node.

---

## 6. Official Tokenomics & Supply Distribution

The 100,000,000,000 SYN fixed supply is immutably allocated at genesis:

```
Total Genesis Supply: 100,000,000,000 SYN (100 Billion)
├── Immune Node Uptime Rewards:     22,000,000,000 SYN (22.0%)  [Decentralized uptime proof pool]
├── Locked DEX & CEX Liquidity:    20,000,000,000 SYN (20.0%)  [Exchange market-making & pairing]
├── Founder 10-Year Vesting:       17,000,000,000 SYN (17.0%)  [1.7B/yr on May 29 via Timelock Vault]
├── Ecosystem Treasury:            13,000,000,000 SYN (13.0%)  [Core development, grants, partnerships]
├── Community Adopter Rewards:     12,500,000,000 SYN (12.5%)  [Ecosystem onboarding & adoption]
├── Validator Security Rewards:    12,000,000,000 SYN (12.0%)  [Consensus node staking & block rewards]
├── Strategic Reserve:              3,000,000,000 SYN ( 3.0%)  [Long-term institutional runway]
└── Founder Launch Allocation:        500,000,000 SYN ( 0.5%)  [Initial operational deployment]
```

---

## 7. Compliance & Regulatory Classification

1. **Utility & Governance Classification:** SYN is the sovereign native utility coin of the SYNTHOS Layer-1 protocol, required for network transaction gas, consensus staking, peer verification incentives, and decentralized autonomous agent execution.
2. **Decentralized Distribution:** Fair issuance model with 0% venture capital presale dumping risk and transparent 10-year lockups on founder allocations.
3. **Legal Documentation:**
   - Formal Legal Memorandum & Howey Analysis: Available upon request under NDA.
   - Terms of Service & Legal Notices: [https://www.ishamwilliamsblockchains.com/legal](https://www.ishamwilliamsblockchains.com/legal)

---

## 8. Integration Contacts

- **Lead Founder & Protocol Architect:** James G. Isham Williams, Sr.
- **Organization:** SYNTHOS Collective
- **Official Portal:** [https://www.ishamwilliamsblockchains.com](https://www.ishamwilliamsblockchains.com)
- **Technical Support Channel:** [support@ishamwilliamsblockchains.com](mailto:support@ishamwilliamsblockchains.com)
- **Telegram / Institutional Liaison:** Contact available via official website intake.
