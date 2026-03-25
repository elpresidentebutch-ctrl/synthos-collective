# SYNTHOS Blockchain - Advanced Mechanics & Fork Handling

**Version:** 1.0 | **Date:** March 25, 2026

---

## 1. Fork & Consensus Mechanics

### 1.1 Fork Definition
A **fork** occurs when different nodes have different views of the ledger state. SYNTHOS prevents long-lived forks through:

- **Finality Rule:** A block is FINAL when it receives 2/3+ validator signatures
- **No Rollbacks:** Once a block is final, it cannot be reverted
- **Longest Chain Rule:** In absence of finality, nodes follow the block with highest block height + timestamp

### 1.2 Fork Prevention Strategies

**Strategy 1: BFT Finality**
```
Block Finality Process:
1. Proposer creates candidate block
2. Validators validate block
3. Validators vote on block (yes/no)
4. Block finalized when 2/3+ votes received
5. Transaction is irreversible (no fork possible)
```

**Strategy 2: Checkpoint Mechanism** (Recommended for Production)
```
Every N blocks (e.g., 100 blocks), create a state checkpoint.
Validators must register checkpoint commitment.
Prevents reorg deeper than N blocks even if consensus fails.
```

**Strategy 3: Validator Slashing** (Future)
```
Validators who sign conflicting blocks lose stake.
Creates economic disincentive for causing forks.
```

### 1.3 Fork Resolution

**If Fork Detected:**
1. Node detects two final blocks at same height
2. Node stops accepting new blocks immediately
3. Node alerts operators (log + metric)
4. Manual governance intervention required

**Prevention is Better:**
- Maintain 3+ validators (not just 2)
- Each validator on distinct infrastructure/geography
- Monitor validator liveness continuously
- Test failover procedures quarterly

---

## 2. Fee Market & Incentive Mechanism

### 2.1 Current Implementation
Each transaction includes:
- `Amount`: tokens transferred
- `Fee`: reward for block proposer

### 2.2 Fee Distribution
When a block is finalized:
```go
func (c *Chain) ExecuteBlock(b Block) error {
    totalFees := uint64(0)
    
    // Collect fees from all transactions
    for _, tx := range b.Transactions {
        totalFees += tx.Fee
    }
    
    // Award to block proposer
    proposer := b.ProposerID
    c.State.Balance[proposer] += totalFees
    
    // Log for auditing
    log.Info("Block finalized with fees",
        "height", b.Height,
        "proposer", proposer,
        "total_fees", totalFees)
    
    return nil
}
```

### 2.3 Fee Market Best Practices

**Minimum Fee Calculation (Client Side):**
```
MIN_FEE = 1 (hardcoded minimum)

Recommended Fee:
- For normal priority: 1-10 tokens
- For high priority: 10-100 tokens
- During congestion: observe mempool min fee
```

**Fee Scaling (Future Enhancement):**
```
Dynamic Fee = base_fee + priority_fee
- base_fee: increases when blocks are full
- priority_fee: user's bid for inclusion speed
- Auto-adjusts every block to maintain 80% target utilization
```

---

## 3. Double-Spend Prevention

### 3.1 Nonce-Based Protection

Every transaction includes a `nonce` (sequence number per sender).

**Rules:**
- First tx from address must have nonce=0
- Second tx must have nonce=1
- Nonces must be sequential (no gaps)
- Nonce per address is managed by state

**Prevention:**
```go
// State applies transactions sequentially
func (s *State) ApplyTx(tx Tx) error {
    from := s.Get(tx.From)
    
    // Nonce must be exact next sequential value
    if tx.Nonce != from.Nonce {
        return ErrBadNonce
    }
    
    // Apply tx...
    from.Nonce += 1  // Increment after applying
    return nil
}
```

### 3.2 Replay Attack Prevention (v2.1+)

**ChainID in Signature:**
Transaction signature now includes the ChainID:
```solidity
// Prevent testnet tx being replayed on mainnet
msgHashInput = keccak256(
    abi.encodePacked(
        "CHAIN_ID:", uint64(chainId),
        "FROM:", from,
        "TO:", to,
        "AMOUNT:", amount,
        "NONCE:", nonce
    )
)
```

This ensures:
- Testnet transactions cannot be relayed to mainnet
- Each fork has different tx signatures
- Every chain parameter change invalidates all pending txs

---

## 4. State Consistency

### 4.1 State Root Commitment

Every block includes `StateRoot`: SHA256 hash of all accounts.

**Guarantees:**
- All nodes with same block history have identical state
- Merkle-tree proof available for any account balance
- Light clients can verify state without full chain

**Calculation:**
```
1. Sort accounts by address
2. Encode account {address, balance, nonce} structure
3. SHA256(accounts[]) = StateRoot
```

### 4.2 State Divergence Detection

If nodes calculate different StateRoot:
1. Consensus fails (block rejected)
2. Indicates deterministic execution bug
3. Requires client software upgrade
4. Previous final blocks remain valid

---

## 5. Transaction Lifecycle

```
┌─────────────────────────────────────────────────────────┐
│ 1. SUBMIT PHASE                                         │
├─────────────────────────────────────────────────────────┤
│ • Client creates Tx with ChainID, Nonce, Signature     │
│ • Sends to RPC endpoint                                │
│ • RPC validates: signature, balance, nonce sequence    │
│ └─> Add to Mempool (if valid)                          │
└─────────────────────────────────────────────────────────┘
                         ↓
┌─────────────────────────────────────────────────────────┐
│ 2. MEMPOOL PHASE                                        │
├─────────────────────────────────────────────────────────┤
│ • Tx sits in mempool waiting for block proposal        │
│ • Proposer sorts by fee (descending) + nonce (ascending)
│ • High-fee txs get included first                       │
│ └─> BuildBlock() includes TX in next block             │
└─────────────────────────────────────────────────────────┘
                         ↓
┌─────────────────────────────────────────────────────────┐
│ 3. BLOCK INCLUSION PHASE                                │
├─────────────────────────────────────────────────────────┤
│ • Block created with Tx                                 │
│ • Block broadcast to validators                         │
│ • Validators validate block (signatures, fees, etc.)   │
│ └─> Validators vote YES or NO                          │
└─────────────────────────────────────────────────────────┘
                         ↓
┌─────────────────────────────────────────────────────────┐
│ 4. CONSENSUS PHASE                                      │
├─────────────────────────────────────────────────────────┤
│ • Votes collected                                       │
│ • Block finalized when 2/3+ votes received            │
│ • State root committed to chain                         │
│ └─> Tx is now IMMUTABLE (no rollback possible)         │
└─────────────────────────────────────────────────────────┘
                         ↓
┌─────────────────────────────────────────────────────────┐
│ 5. FINAL PHASE                                          │
├─────────────────────────────────────────────────────────┤
│ • Block finalized (2/3+ signatures)                    │
│ • Proposer receives fees                                │
│ • Tx effects visible to all clients                     │
│ └─> Client can rely on result                          │
└─────────────────────────────────────────────────────────┘
```

---

## 6. Blockchain Parameters (Immutable)

These define the chain identity:

| Parameter | Value | Notes |
|-----------|-------|-------|
| ChainID | 1+ | Unique per network (testnet != mainnet) |
| GenesisHash | 0x... | First block hash, immutable |
| GenesisTime | 1970-01-01 | Deterministic, not wall-clock |
| MaxBlockSize | 1MB | Max bytes per block |
| BlockInterval | 5-12s | Target time between blocks |
| MaxGasPerBlock | TBD | (Future: EVM integration) |
| MinFeePerTx | 1 | Minimum fee in base units |

Changing any of these requires:
1. Governance proposal (off-chain)
2. 2/3+ validator approval
3. Scheduled hard fork block height
4. All nodes upgrade by fork height

---

## 7. Security Considerations

### 7.1 What Can Go Wrong?

| Scenario | Guard | Fallback |
|----------|-------|----------|
| Proposer censors txs | Validators rotate | RPC can request from mempool |
| Validator goes offline | Need 3+ validators | Network continues (2/3 threshold) |
| Majority colludes | Off-chain governance | Social consensus to replace validators |
| Nonce mismatch | Sequential validation | Tx rejected, client retries |
| Fee too low | Dynamic fee market | Tx stays in mempool, eventually mined |

### 7.2 Production Hardening

Before mainnet:
- [ ] Fork scenario testing: stop 1/3 of validators, verify chain continues
- [ ] Long-range attack testing: attempt to reorg 100 blocks
- [ ] Nonce wraparound testing: test with nonce > 2^32
- [ ] Fee market stress: submit 1000s of txs with varying fees
- [ ] State divergence testing: intentional buggy node, verify detection

---

## References

- **Tendermint BFT:** https://github.com/tendermint/tendermint/blob/master/docs/
- **Ethereum Finality:** https://ethereum.org/en/developers/docs/consensus-mechanisms/pos/#finality
- **EIP-1559 (Fee Markets):** https://eips.ethereum.org/EIPS/eip-1559

---

**Last Updated:** March 25, 2026  
**Owner:** SYNTHOS Development Team  
**Review Frequency:** Quarterly or after major upgrades
