# SYNTHOS Collective - Agent-Native L1 (Go)

## Overview

SYNTHOS Collective is an agent-native Layer-1 blockchain written in Go. The system treats agents as first-class network participants: agents propose/validate blocks, relay consensus messages, and maintain ledger state.

This repository contains a minimal L1 ledger (`internal/chain`), a 2/3+ vote finality engine (`internal/consensus`), agent identity/envelope signing (`internal/agent`, `internal/crypto`), and transports for message passing (`internal/network`), wired together as a node (`internal/node`).

## Core Concept

A SYNTHOS Agent is a sovereign computational entity with seven integrated roles:

1. **Validator** - Verifies transactions and block validity
2. **Economist** - Manages incentives and resource allocation
3. **Governor** - Coordinates collective decision-making
4. **Communicator** - Enables peer-to-peer coordination
5. **Simulator** - Models scenarios and predicts outcomes
6. **Enforcer** - Monitors compliance and applies penalties
7. **Citizen** - Participates in the ecosystem

## Project Structure

```
SYNTHOS COLLECTIVE/
├── cmd/                          # Entrypoints
│   ├── node/                     # Minimal single-process demo node
│   ├── devnet/                   # In-memory multi-node demo (no open ports)
│   ├── rpcnode/                  # Local node with HTTP RPC (port 8080)
│   ├── agent/                    # Agent tool (work-in-progress)
│   ├── wallet/                   # Wallet tool (work-in-progress)
│   └── txgen/                    # Tx generator (work-in-progress)
├── internal/                     # Go implementation (L1 + agents + transport)
│   ├── agent/
│   ├── chain/
│   ├── consensus/
│   ├── crypto/
│   ├── network/
│   ├── node/
│   ├── rpc/
│   └── storage/
├── docs/                         # Concept + architecture docs
└── README.md
```

## Agent Architecture

### Core Systems

Each agent maintains **three core systems**:

1. **State Management** (`AgentState`)
   - Centralized state with versioning
   - Transactional consistency
   - Snapshot and rollback support

2. **Event Bus** (`EventBus`)
   - Event publishing and subscription
   - Role coordination
   - Event history and tracing

3. **Role Registry**
   - Flexible role composition
   - Runtime enable/disable
   - Per-role state management

### Execution Model

Agents operate with an **async/await** concurrency model:

```python
# Initialize agent
agent = SyntHOSAgent(config)

# Register roles
agent.register_role(ValidatorRole(agent))
agent.register_role(citizenRole(agent))
# ... other roles ...

# Initialize all
await agent.initialize()

# Run continuously
await agent.start()
```

## Intelligence Capabilities

SYNTHOS Agents possess four levels of intelligence:

### 1. Deterministic Reasoning
- Rule-based inference systems
- Verifiable outputs
- Reproducible decisions
- Logic-driven validation

### 2. Local Simulation
- Market outcome modeling
- Risk scenario analysis
- Consensus prediction
- Liquidity flow simulation
- Agent behavior modeling

### 3. Pattern Detection
- Anomaly detection
- Malicious behavior identification
- Sybil attack detection
- Invalid proposal detection
- Economic manipulation detection

### 4. Optimization
- Block proposal optimization
- Fee market optimization
- Liquidity pool optimization
- Staking allocation optimization
- Cross-chain routing optimization

## Quick start (Go)

Run the in-memory devnet (4 validators + 3 non-validator replicas):

```bash
go run ./cmd/devnet
```

Run a local RPC node (persists snapshot in `.synthos-data` by default):

```bash
go run ./cmd/rpcnode
```

## Audit packaging

If you’re preparing an audit/review, see `docs/audit/` (scope, architecture summary, threat model, and review checklist).

## Notes on documentation

Some documents in `docs/` describe the intended “agents are the blockchain” model at a higher level. The authoritative implementation is the Go code under `internal/` and the runnable entrypoints under `cmd/`.

## Usage Examples

### Creating an Agent

```python
from src.core import SyntHOSAgent, AgentConfig
from src.roles import ValidatorRole, CitizenRole

# Create configuration
config = AgentConfig(
    id="agent-001",
    network="testnet",
    log_level="INFO"
)

# Create agent
agent = SyntHOSAgent(config)

# Register roles
agent.register_role(ValidatorRole(agent))
agent.register_role(CitizenRole(agent))

# Initialize
await agent.initialize()
```

### Submitting a Transaction

```python
from src.models import Transaction

# Create transaction
tx = Transaction(
    sender="alice",
    recipient="bob",
    amount=100,
    fee=1
)

# Submit via agent
await agent.submit_transaction(tx)
```

### Creating a Governance Proposal

```python
from src.models import Proposal

# Create proposal
proposal = Proposal(
    id="proposal-001",
    proposer=agent.id,
    change_type="FEE_ADJUSTMENT",
    parameters={'new_base_fee': 2}
)

# Submit proposal
governor = agent.get_role("Governor")
proposal_id = await governor.propose_change(proposal)
```

### Monitoring Agent Status

```python
# Get agent status
status = agent.get_status()

# Get role status
role_status = status['roles']

# Get state
agent_state = status['state']

# Get event history
events = await agent.get_event_history(limit=100)
```

## Role Responsibilities

### Validator Role
- Validate transaction signatures and format
- Verify state transitions
- Validate block integrity
- Maintain ledger consistency

### Economist Role
- Calculate transaction fees
- Distribute validator rewards
- Manage token economics
- Monitor economic metrics

### Governor Role
- Propose protocol changes
- Coordinate voting
- Finalize governance decisions
- Implement approved changes

### Communicator Role
- Broadcast transactions and blocks
- Receive and relay messages
- Maintain peer connections
- Coordinate consensus

### Simulator Role
- Model protocol changes
- Simulate economic scenarios
- Predict network behavior
- Test security assumptions

### Enforcer Role
- Monitor protocol compliance
- Detect malicious behavior
- Apply penalties and slashing
- Enforce transaction limits

### Citizen Role
- Submit transactions
- Manage stake
- Participate in governance
- Claim rewards

## Event System

The framework uses a **publish-subscribe** event system:

```python
# Subscribe to events
agent.event_bus.subscribe(
    EventType.TRANSACTION_VALIDATED,
    handler_function
)

# Publish events
await agent.event_bus.publish(Event(
    type=EventType.TRANSACTION_VALIDATED,
    source="Validator",
    data={'transaction': tx}
))
```

### Event Types

- **Transaction Events**: SUBMITTED, VALIDATED, REJECTED, FINALIZED
- **Block Events**: PROPOSED, VALIDATED, FINALIZED
- **Consensus Events**: ROUND_START, VOTE, FINALITY
- **Governance Events**: PROPOSAL_SUBMITTED, VOTED, EXECUTED
- **Network Events**: PEER_CONNECTED, PEER_DISCONNECTED, MESSAGE_RECEIVED
- **Monitoring Events**: ANOMALY_DETECTED, SLASHING_TRIGGERED, REWARD_DISTRIBUTED

## Configuration

Agents are configured via `AgentConfig`:

```python
config = AgentConfig(
    id="agent-001",              # Unique agent identifier
    network="mainnet",           # Network to join
    log_level="INFO",            # Logging level
    consensus_timeout_ms=4000,   # Consensus timeout
    max_peers=50,                # Maximum peers
    storage_path="./data"        # Data storage location
)
```

## Testing

Run the Go unit tests (ledger, consensus threshold math, crypto):

```bash
go test ./...
```

Functional smoke test (in-memory multi-node demo):

```bash
go run ./cmd/devnet
```

## Incubator / launchpad readiness

For a concise checklist (demo, Docker, token contract pointers, and external items like legal/deck), see [docs/SEEDIFY_INCUBATION_READINESS.md](docs/SEEDIFY_INCUBATION_READINESS.md).

**After incubation** (testnet, verified contracts, audit, ops): [docs/POST_INCUBATION_LAUNCH_READINESS.md](docs/POST_INCUBATION_LAUNCH_READINESS.md).

### Smart contracts (Hardhat)

From the `contracts/` directory:

```bash
cd contracts
npm install
npm run compile
npx hardhat test
npm run deploy:synthos:local
```

See `contracts/.env.example` for RPC and deployer env vars on live networks.

### Docker (RPC node)

```bash
docker build -t synthos-collective:local .
docker run --rm -p 8080:8080 -v synthos-data:/data synthos-collective:local
```

## Development Roadmap

### Phase 1: Core Infrastructure ✓
- [x] Base classes and interfaces
- [x] State management system
- [x] Event bus implementation
- [x] Configuration management

### Phase 2: Role Implementation ✓
- [x] Validator role
- [x] Economist role
- [x] Governor role
- [x] Communicator role
- [x] Simulator role
- [x] Enforcer role
- [x] Citizen role

### Phase 3: Integration (In Progress)
- [ ] Consensus engine integration
- [ ] Network layer integration
- [ ] End-to-end testing
- [ ] Performance optimization

### Phase 4: Testing & Deployment
- [ ] Unit test suite
- [ ] Integration tests
- [ ] Stress testing
- [ ] Security audit
- [ ] Docker deployment

## Design Philosophy

### Principles

1. **Sovereignty** - Each agent is self-governing
2. **Determinism** - All decisions are reproducible and verifiable
3. **Modularity** - Roles are independent and composable
4. **Auditability** - All actions can be traced and verified
5. **Resilience** - Network survives arbitrary agent failures

### Technical Approach

- **Async-first**: All I/O is non-blocking
- **Event-driven**: Components coordinate via events
- **State versioning**: Full state history and snapshots
- **Deterministic simulation**: Predict outcomes before committing
- **Cryptographic proofs**: All decisions are externally verifiable

## Security Considerations

- **Byzantine Fault Tolerance**: Tolerates up to 1/3 malicious agents
- **Slashing Mechanisms**: Penalizes protocol violations
- **Rate Limiting**: Prevents DoS attacks
- **Anomaly Detection**: Identifies suspicious patterns
- **Compliance Monitoring**: Enforces protocol rules

## Performance Goals

- **Throughput**: 1000+ transactions per second
- **Latency**: <5 second finality
- **Scalability**: Linear scaling with number of agents
- **Simulation Speed**: 100x faster than actual execution
- **Memory**: <1GB per agent

## Contributing

This is an open framework for building sovereign blockchain agents. Contributions welcome!

## License

SYNTHOS Collective - 2026

## References

- [Agent Specification](docs/AGENTS_SPECIFICATION.md)
- [Framework Design](docs/FRAMEWORK_DESIGN.md)
- [Intelligence Capabilities](docs/INTELLIGENCE_CAPABILITIES.md)
- [Implementation Guide](docs/IMPLEMENTATION_GUIDE.md)
- [API Reference](docs/API_REFERENCE.md)

---

**Building the next generation of decentralized intelligence.**
