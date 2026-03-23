# SYNTHOS Agent Quick Reference Guide

## Essential Code Patterns

### 1. Basic Agent Initialization

```python
from src.core.agent import SyntHOSAgent
from src.roles.validator import ValidatorRole
from src.roles.economist import EconomistRole

# Initialize agent
agent = SyntHOSAgent(agent_id="node-1", version="1.0.0")
await agent.initialize()

# Register roles
validator = ValidatorRole(agent)
economist = EconomistRole(agent)
await validator.initialize()
await economist.initialize()

agent.register_role(validator)
agent.register_role(economist)

# Start agent
await agent.start()
```

### 2. Submit a Transaction

```python
from src.models import Transaction

# Create transaction
tx = Transaction(
    sender="alice",
    recipient="bob",
    amount=100,
    fee=1,
    nonce=0,
    signature="sig_alice",
    timestamp=time.time()
)

# Submit to agent
await agent.submit_transaction(tx)

# Check mempool
mempool = await state_store.get_mempool()
```

### 3. Propose a Block

```python
from src.roles.block_proposer import BlockProposer

# Get pending transactions
mempool = await state_store.get_mempool()

# Create proposer
proposer = BlockProposer()

# Propose block
block = await proposer.propose_block(
    transactions=mempool,
    proposer_id="validator_1",
    height=100
)

# Broadcast block
await gossip_protocol.publish_gossip(block)
```

### 4. Consensus Voting

```python
from src.consensus.consensus import ConsensusEngine

# Initialize consensus
consensus = ConsensusEngine()

# Start consensus round
await consensus.start_consensus_round(
    height=100,
    block_hash="hash_xyz"
)

# Cast vote
await consensus.vote(
    voter="validator_1",
    height=100,
    block_hash="hash_xyz",
    vote_value=True,
    stake=1000,
    signature="sig_validator_1"
)

# Check finality
is_final = await consensus.finalize_consensus(
    height=100,
    required_supermajority=67
)
```

### 5. Governance Proposal

```python
from src.roles.governor import GovernorRole

governor = GovernorRole(agent)

# Propose change
proposal_id = await governor.propose_change(
    change_type="CONSENSUS_PARAMETER",
    parameters={"block_time": 5000}
)

# Vote on proposal
await governor.vote(
    proposal_id=proposal_id,
    voter="citizen_1",
    vote_value=True,
    stake=1000
)

# Finalize vote
is_passed = await governor.finalize_vote(proposal_id)
```

### 6. Validate Transaction

```python
from src.roles.transaction_validator import TransactionValidator

validator = TransactionValidator()

# Validate transaction
result = await validator.validate_full_transaction(tx)

if result.is_valid:
    print("✓ Transaction valid")
else:
    print(f"✗ Validation errors: {result.validation_errors}")
```

### 7. Monitor Agent Health

```python
# Get agent status
status = agent.get_status()
print(f"Status: {status['current_state']}")

# Get role status
for role_name, role in agent.roles.items():
    print(f"{role_name}: {role.status.value}")

# Get network info
peer_count = len(p2p_messenger.peers)
print(f"Connected peers: {peer_count}")

# Get blockchain height
height = agent.state.get("current_height", 0)
print(f"Block height: {height}")
```

### 8. Create State Snapshot

```python
# Create snapshot
snapshot = await agent.state.create_snapshot()

# Store snapshot
with open("state_backup.json", "w") as f:
    json.dump(snapshot, f)

# Later: Restore snapshot
snapshot = json.load(open("state_backup.json"))
await agent.state.restore_snapshot(snapshot)
```

### 9. Subscribe to Events

```python
# Define event handler
async def handle_transaction_validated(event):
    print(f"Transaction validated: {event['transaction'].id}")

# Subscribe to event
agent.event_bus.subscribe("TRANSACTION_VALIDATED", handle_transaction_validated)

# Publish event
await agent.event_bus.publish({
    "type": "TRANSACTION_VALIDATED",
    "transaction": tx
})
```

### 10. Query Local State

```python
# Get balance
balance = await agent.state.get_balance("alice")

# Get all accounts
all_accounts = await state_store.get_all_accounts()

# Get peer reputations
peers = await state_store.get_all_peers()

# Get proposals
proposals = await state_store.get_all_proposals()

# Get governance decisions
decisions = await state_store.get_all_governance_decisions()
```

---

## Common Debugging Patterns

### Check Transaction Status

```python
# Is transaction in mempool?
mempool = await state_store.get_mempool()
tx_in_mempool = any(t.id == target_tx_id for t in mempool)

# Was transaction applied?
block = await state_store.get_block(block_height)
tx_in_block = any(t.id == target_tx_id for t in block.transactions)

# Check account balance change
balance_before = historical_state[account]
balance_after = current_state[account]
was_deducted = balance_after < balance_before
```

### Check Consensus Status

```python
# Get current consensus round
consensus_round = agent.state.get("current_consensus_round")

# Get votes collected
votes = consensus_round.votes if consensus_round else []

# Check if supermajority reached
total_stake = sum(v.stake for v in votes)
required_stake = total_stake * 2 // 3 + 1
votes_for = sum(v.stake for v in votes if v.value)
has_consensus = votes_for >= required_stake
```

### Check Network Connectivity

```python
# Get connected peers
connected = [p for p in p2p_messenger.peers if p.is_connected]
print(f"Connected peers: {len(connected)}/{len(p2p_messenger.peers)}")

# Check peer reputations
for peer in connected:
    reputation = state_store.get_peer_reputation(peer.id)
    print(f"{peer.id}: score={reputation.score}, latency={reputation.latency_ms}ms")

# Check if enough peers for consensus
min_required = len(all_validators) * 2 // 3 + 1
can_get_consensus = len(connected) >= min_required
```

### Check State Consistency

```python
# Verify state root
computed_root = await agent.state.compute_merkle_root()
stored_root = agent.state.get("current_state_root")
is_consistent = computed_root == stored_root

# Check all accounts sum
total = sum(
    balance for account, balance in agent.state["ledger"].items()
)

# Check for corruption
try:
    agent.state.validate()
    print("✓ State is valid")
except StateCorruptionError as e:
    print(f"✗ State corruption detected: {e}")
```

---

## Configuration Quick Reference

### Minimal Configuration

```yaml
agent:
  id: "node-1"
  network_id: "mainnet"

server:
  rpc_port: 8545
  p2p_port: 30303

consensus:
  block_interval: 5000
  byzantine_tolerance: 0.33

storage:
  path: "/var/lib/synthos/data"
```

### Production Configuration

```yaml
agent:
  id: "validator-node-1"
  version: "1.0.0"
  network_id: "mainnet"
  
  state:
    snapshot_interval: 1000
    compression_enabled: true

server:
  host: "0.0.0.0"
  rpc_port: 8545
  p2p_port: 30303
  
  tls:
    enabled: true
    cert_path: "/etc/synthos/certs/server.crt"
    key_path: "/etc/synthos/certs/server.key"

networking:
  max_peers: 50
  
  gossip:
    hop_limit: 3
    propagation_probability: 0.5

consensus:
  timeout_propose: 3000
  block_interval: 5000
  byzantine_tolerance: 0.33

economics:
  min_transaction_fee: 1
  base_block_reward: 100

storage:
  backend: "rocksdb"
  path: "/var/lib/synthos/data"
  cache_size: 2000000000

monitoring:
  prometheus_port: 9090
  log_level: "info"
```

---

## Common Issues & Solutions

### Transaction Rejected

**Problem**: `TRANSACTION_REJECTED - INSUFFICIENT_BALANCE`
**Solution**:
```python
# Check sender balance
balance = await agent.state.get_balance(tx.sender)
print(f"Balance: {balance}, Required: {tx.amount + tx.fee}")

# Check pending balance
mempool = await state_store.get_mempool()
pending = sum(t.amount + t.fee for t in mempool if t.sender == tx.sender)
available = balance - pending
print(f"Available: {available}")
```

### Consensus Timeout

**Problem**: Block not reaching consensus
**Solution**:
```python
# Check peer connectivity
peers = p2p_messenger.peers
connected = sum(1 for p in peers if p.is_connected)
print(f"Connected: {connected}/{len(peers)}")

# Check if peers are voting
consensus = agent.state.get("current_consensus_round")
votes = consensus.votes if consensus else []
print(f"Votes: {len(votes)}/{len(validators)}")

# Increase timeout if needed
config.consensus.timeout_propose = 5000  # ms
```

### High Latency

**Problem**: Block time exceeds expected
**Solution**:
```python
# Check if CPU-bound operations
# Optimize batch validation
batch_size = 100
for batch in [txs[i:i+batch_size] for i in range(0, len(txs), batch_size)]:
    results = await asyncio.gather(*[
        validator.validate_transaction(tx) for tx in batch
    ])

# Check if I/O bound
# Implement caching
state_cache = {}
async def get_with_cache(key):
    if key in state_cache:
        return state_cache[key]
    value = await agent.state.get(key)
    state_cache[key] = value
    return value
```

### Memory Leak

**Problem**: Memory usage growing over time
**Solution**:
```python
# Limit state history
MAX_HISTORY = 1000
if len(event_log) > MAX_HISTORY:
    event_log = event_log[-MAX_HISTORY:]

# Clear caches periodically
async def cleanup_caches():
    while True:
        state_cache.clear()
        await asyncio.sleep(3600)  # hourly
        await cleanup_caches()

# Monitor memory
import psutil
process = psutil.Process()
memory_mb = process.memory_info().rss / 1024 / 1024
print(f"Memory: {memory_mb}MB")
```

### Network Partition

**Problem**: Node isolated from peers
**Solution**:
```python
# Enable read-only mode
until_connected = True
await agent._set_read_only_mode(True)

# Attempt reconnection
for peer in known_peers:
    try:
        await p2p_messenger.connect_peer(peer)
    except:
        pass

# Check if re-connected
if len(connected_peers) >= min_required:
    await agent._set_read_only_mode(False)
```

---

## Performance Tuning

### Transaction Throughput

```python
# Enable batch validation
batch_size = 1000
validator.batch_mode = True
validator.batch_size = batch_size

# Optimize mempool ordering
mempool = sorted(mempool, key=lambda tx: tx.fee / len(tx.data), reverse=True)

# Parallel gossip propagation
tasks = [
    gossip_protocol.publish_gossip(block, peer)
    for peer in connected_peers
]
await asyncio.gather(*tasks)
```

### Block Finality Time

```python
# Reduce timeout
consensus.timeout_propose = 1000  # 1 second
consensus.block_interval = 3000   # 3 seconds

# Increase gossiping speed
gossip.propagation_probability = 1.0  # Always propagate
gossip.peer_selection_count = 10

# Optimize state root computation
use_incremental_merkle = True  # Only update changed leaves
```

### Network Bandwidth

```python
# Enable message compression
p2p_messenger.compression_enabled = True

# Batch transactions in gossip
gossip.batch_size = 100

# Reduce message frequency
heartbeat_interval = 5000  # ms
health_check_interval = 60000  # ms
```

---

## Monitoring Queries

### Real-Time Metrics

```python
# Get TPS (transactions per second)
txs_per_second = agent.metrics.transaction_rate

# Get block time
block_time_ms = agent.metrics.block_time_avg

# Get consensus finality
finality_time_ms = agent.metrics.finality_time

# Get validator uptime
uptime_percent = validator.uptime * 100
```

### Health Check Endpoint

```bash
# Liveness
curl http://localhost:8545/health/liveness

# Readiness
curl http://localhost:8545/health/readiness

# Metrics
curl http://localhost:9090/metrics

# Debug info
curl http://localhost:8545/debug/info
```

### Log Analysis

```bash
# Find errors
grep "ERROR" logs/agent.log

# Find slow operations
grep "latency_ms" logs/agent.log | grep -E "[5-9][0-9]{2,}"

# Find consensus issues
grep "CONSENSUS\|VOTE\|FINALITY" logs/agent.log

# Count transaction rejections
grep "TRANSACTION_REJECTED" logs/agent.log | wc -l
```

---

## Testing Checklists

### Before Deployment

- [ ] All unit tests pass
- [ ] Integration tests pass
- [ ] Load testing completed
- [ ] Network partition testing done
- [ ] Byzantine validator testing done
- [ ] State consistency verified
- [ ] Backup/restore tested
- [ ] Configuration validated
- [ ] Security audit completed
- [ ] Documentation reviewed

### Daily Operations

- [ ] Check agent health status
- [ ] Monitor memory usage
- [ ] Check consensus finality rate
- [ ] Verify transaction throughput
- [ ] Check peer connectivity
- [ ] Review error logs
- [ ] Verify block rewards
- [ ] Check governance proposals

### Monthly Tasks

- [ ] Update consensus parameters
- [ ] Review and optimize fees
- [ ] Analyze pattern detection logs
- [ ] Update peer whitelist
- [ ] Backup state snapshots
- [ ] Rotate validator keys
- [ ] Review security logs
- [ ] Plan upgrades

---

## External Resources

### Documentation Files
- Architecture: [COMPLETE_ARCHITECTURE.md](COMPLETE_ARCHITECTURE.md)
- Integration: [SYSTEM_INTEGRATION.md](SYSTEM_INTEGRATION.md)
- Production: [DEPLOYMENT_PRODUCTION.md](DEPLOYMENT_PRODUCTION.md)
- Patterns: [ADVANCED_PATTERNS.md](ADVANCED_PATTERNS.md)

### Code Examples
- Basic usage: [example.py](../example.py)
- Test patterns: [tests/](../tests/)
- Configuration: [config.yaml](../config.yaml)

---

This quick reference provides essential patterns, debugging approaches, configuration templates, and common solutions for working with SYNTHOS Agents.

---

## Legal notice

SYNTHOS Collective, SYNTHOS, and related names, marks, documentation, and technical materials in this document are the **exclusive property of James G. Isham Williams, Sr.** Unauthorized reproduction, distribution, or commercial use without express written permission is prohibited except as allowed under applicable open-source licenses for identified files. No rights are waived.

This document is informational only and is not legal, financial, or investment advice. The canonical legal notice is in **docs/LEGAL_NOTICE.md** in the SYNTHOS Collective repository.
