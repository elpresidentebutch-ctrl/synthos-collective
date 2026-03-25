# SYNTHOS Collective - Security Model

**Status:** Post-Audit Remediation (March 2026)

---

## Executive Summary

This document establishes the security model, threat framework, and controls for SYNTHOS Collective.

### Audit Issues Fixed
- ✅ Explicit threat model defined
- ✅ Agent authentication & RBAC implemented  
- ✅ Secrets management formalized
- ✅ Smart contract security hardened
- ✅ Input validation enhanced
- ✅ CI/CD security controls added

---

## 1. Security Boundaries

| Boundary | Controls | Level |
|----------|----------|-------|
| **Agent Identity** | Ed25519 sigs, Hardware binding | CRITICAL |
| **Consensus** | 2/3+ BFT, Signature verification | CRITICAL |
| **Smart Contracts** | EVM, AccessControl, ReentrancyGuard | HIGH |
| **Network** | Authenticated peer discovery | HIGH |
| **RPC** | Rate limiting required | MEDIUM |
| **Storage** | Unencrypted local state | MEDIUM |

---

## 2. Threat Model

### CRITICAL
- **Malicious Agent**: Compromised signing key → forge txs/votes
  - *Fix:* `Agent.VerifyIdentity()` + hardware binding
- **Consensus Takeover**: 2/3+ agents collude → blockchain rewrite
  - *Fix:* Decentralized validator set
- **Smart Contract Bug**: Unaudited code → token theft
  - *Fix:* ReentrancyGuard + AccessControl + audit
- **RPC Hijacking**: No auth → arbitrary txs
  - *Fix:* Auth layer + TLS (planned)

### HIGH
- **Network Sybil**: Fake agents → DoS
  - *Fix:* Rate limiting + reputation checks
- **State Divergence**: Different agent state → consensus failure
  - *Fix:* State checkpoints every N blocks
- **Nonce Replay**: Tx reused → double-spend
  - *Fix:* Nonce validation in `ValidateTx()`
- **Private Key Exposure**: Hardcoded keys → compromise
  - *Fix:* HSM/Vault + .env template

### MEDIUM
- **DoS on RPC**: Resource exhaustion → unavailability
  - *Fix:* Rate limits + pooling
- **Config Injection**: Malicious .env → code execution
  - *Fix:* Config validation + signing

---

## 3. Agent Authentication & Authorization

### Identity Verification (CRITICAL)

```go
// Usage: if err := agent.VerifyIdentity(); err != nil { ... }
// Checks:
// - Public key valid ED25519 (32 bytes)
// - Agent ID matches public key hash
// - Hardware ID binding present
// - Reputation >= 0
```

### Role-Based Access Control

| Role | Min Rep | Permissions |
|------|---------|-------------|
| Validator | 100 | Validate blocks, vote |
| Governor | 200 | Create proposals |
| Economist | 50 | Calculate fees |
| Enforcer | 150 | Monitor compliance |
| Simulator | 100 | Run simulations |
| Communicator | 0 | Relay messages |
| Citizen | 0 | Submit txs |

**Usage:** `if err := agent.CanPerformRole(RoleValidator); err != nil { ... }`

---

## 4. Secrets Management

### Requirements
- **NEVER hardcode keys** (zero exceptions)
- Load from `AGENT_PRIVATE_KEY` env var
- Support HSM with `HSM_ENABLED=true`
- Key rotation every 90 days with grace period

### Format
- ED25519 private: 32 bytes = 64 hex chars
- Public key: derived from private
- Example: `AGENT_PRIVATE_KEY=00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff`

---

## 5. Smart Contract Security

### SYNToken.sol Enhancements
- ✅ **ReentrancyGuard**: Prevents re-entrancy on transfers
- ✅ **AccessControl**: MINTER_ROLE, PAUSER_ROLE, BURNER_ROLE
- ✅ **MAX_SUPPLY**: Enforced on all mints
- ✅ **nonReentrant**: On critical functions
- ✅ **whenNotPaused**: State checks active

---

## 6. Input Validation

### Transaction Validation (Enhanced)

Every tx passes `ValidateTx()`:
- ✅ ID not empty
- ✅ From/To addresses present
- ✅ Amount > 0, < MAX_AMOUNT
- ✅ Fee >= MIN_FEE, <= amount
- ✅ Public key 32 bytes (ED25519)
- ✅ Signature 64 bytes
- ✅ Signature verifies
- ✅ From matches public key
- ✅ Nonce matches sequence

### Error Handling
- No silent failures
- Log error context (no sensitive data)
- Return specific error types
- Audit failed attempts

---

## 7. Dependency Security

### Go
```bash
go list -json -m all | nancy sleuth --severity high
go mod verify
go mod tidy
```

### Solidity
- ✅ OpenZeppelin (audited)
- ✅ Uniswap v3 (if needed)
- ❌ Unknown/unmaintained projects

---

## 8. CI/CD Security

**Automated checks:**
- `gosec` (Go static analysis)
- `nancy` (Go dependency audit)
- `npm audit` (Node deps)
- `slither` (Solidity analysis)
- Coverage >= 70%
- Race condition detection

---

## 9. Production Checklist

- [ ] Testnet: 3+ validators, 1+ month, zero rollbacks
- [ ] Audits: Internal + external (Trail of Bits, OpenZeppelin)
- [ ] Keys: HSM/Vault, zero hardcoded
- [ ] Monitoring: 24/7 alerts
- [ ] Runbooks: Incident response
- [ ] Insurance: Protocol insurance
- [ ] Legal: Securities review
- [ ] Rate Limits: RPC endpoints
- [ ] Network Access: Known validators only
- [ ] Key Rotation: Every 90 days, tested

---

## 10. Incident Response

1. **Assess** (15 min) - Scope & exploitability
2. **Contain** (1 hour) - Pause network, notify validators
3. **Remediate** (4-24 hours) - Deploy patch, re-test
4. **Disclose** (1-7 days) - Public advisory + timeline

**Report:** security@synthos.collective

---

**Last Updated:** March 25, 2026 | **Review:** Quarterly
