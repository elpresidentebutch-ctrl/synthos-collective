# SYNTHOS Collective - Quick Start Guide

## 5-Minute Setup

### 1. Prerequisites

```bash
# Python 3.8+
python --version

# Install dependencies
pip install -r requirements.txt
```

### 2. Run Example

```bash
# From project root
python example.py
```

This will:
- Create a test agent
- Demonstrate transaction flow
- Show governance voting
- Monitor agent status
- Review performance metrics

### 3. Run Tests

```bash
# Install test dependencies
pip install pytest pytest-asyncio

# Run all tests
pytest tests/ -v

# Run core tests only
pytest tests/test_core.py -v

# Run role tests only
pytest tests/test_roles.py -v
```

## Project Structure

```
SYNTHOS COLLECTIVE/
├── src/
│   ├── core/              # Core framework (agent, state, events)
│   ├── roles/             # Seven core roles
│   ├── models/            # Data models
│   ├── consensus/         # Consensus engine
│   ├── governance/        # Governance module
│   ├── network/           # Network layer
│   ├── storage/           # Storage layer
│   └── utils/             # Utilities
├── config/
│   └── validator.py       # Configuration validation
├── tests/
│   ├── test_core.py       # Core framework tests
│   └── test_roles.py      # Role tests
├── docs/                  # Documentation
├── example.py             # Working examples
├── TESTING.md             # Testing guide
└── README.md              # Project overview
```

## Usage Examples

### Create an Agent

```python
import asyncio
from src.core import SyntHOSAgent, AgentConfig
from src.roles import ValidatorRole, CitizenRole

async def main():
    # Create config
    config = AgentConfig(
        id="my-agent",
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
    
    # Use agent
    citizen = agent.get_role("Citizen")
    await citizen.stake_tokens(100)
    
    # Check status
    status = agent.get_status()
    print(status)
    
    # Cleanup
    await agent.stop()

asyncio.run(main())
```

### Submit a Transaction

```python
from src.models import Transaction

tx = Transaction(
    sender="alice",
    recipient="bob",
    amount=100,
    fee=1
)

# Via agent
await agent.submit_transaction(tx)

# Or via Citizen role
citizen = agent.get_role("Citizen")
await citizen.submit_transaction(tx)
```

### Create a Proposal

```python
from src.models import Proposal

proposal = Proposal(
    id="prop-001",
    proposer=agent.id,
    change_type="FEE_ADJUSTMENT",
    parameters={"new_fee": 2}
)

# Submit via Governor
governor = agent.get_role("Governor")
proposal_id = await governor.propose_change(proposal)

# Vote
await governor.vote(proposal_id, vote_value=True)

# Finalize
passed = await governor.finalize_vote(proposal_id)
```

### Check Agent Status

```python
status = agent.get_status()

print(f"ID: {status['id']}")
print(f"Network: {status['network']}")
print(f"Roles: {list(status['roles'].keys())}")
print(f"State version: {status['state']['version']}")
```

## Core Components

### Agent (`src/core/agent.py`)
- Central orchestrator for all roles
- Manages state and event bus
- Coordinates role execution

### State (`src/core/state.py`)
- Transactional state management
- Versioning and snapshots
- Rollback support

### EventBus (`src/core/event.py`)
- Pub/sub event system
- Event history tracking
- Priority queuing

### Roles (7 total)
1. **Validator** - Transaction/block validation
2. **Economist** - Fee and reward management
3. **Governor** - Governance and voting
4. **Communicator** - P2P communication
5. **Simulator** - Scenario modeling
6. **Enforcer** - Compliance and penalties
7. **Citizen** - User participation

## Configuration

### Validate Configuration

```python
from config.validator import ConfigValidator
from src.core import AgentConfig

config = AgentConfig(
    id="my-agent",
    network="testnet",
    consensus_timeout_ms=4000,
    max_peers=50
)

is_valid, errors = ConfigValidator.validate_config(config)

if not is_valid:
    for error in errors:
        print(f"Error: {error}")
else:
    print("Configuration valid!")
```

### Get Recommended Config

```python
from config.validator import ConfigValidator

# For testnet
config = ConfigValidator.get_recommended_config("testnet")

# For mainnet
config = ConfigValidator.get_recommended_config("mainnet")

# For devnet
config = ConfigValidator.get_recommended_config("devnet")
```

## Testing

### Run All Tests

```bash
pytest tests/ -v
```

### Run Specific Test Category

```bash
# Core tests
pytest tests/test_core.py -v

# Role tests
pytest tests/test_roles.py -v

# Specific role
pytest tests/test_roles.py::TestValidatorRole -v
```

### Test Coverage

- Core framework: 50+ tests
- Roles: 30+ tests
- Configuration: 10+ tests
- Event system: 15+ tests
- Total: 100+ test cases

## Common Tasks

### Monitor Agent

```python
# Real-time status
status = agent.get_status()

# Recent events
events = await agent.get_event_history(limit=20)

# Role metrics
validator = agent.get_role("Validator")
print(validator._validation_stats)
```

### Checkpoint State

```python
# Create checkpoint
checkpoint = await agent.create_state_checkpoint()

# Restore checkpoint
await agent.restore_state_checkpoint(version=42)
```

### Enable/Disable Roles

```python
# Disable a role
agent.disable_role("Validator")

# Enable a role
agent.enable_role("Validator")
```

## Performance Tips

- Use appropriate consensus_timeout_ms for your network
- Adjust max_peers based on hardware constraints
- Monitor event queue size with `event_bus.get_event_history()`
- Create periodic checkpoints for large deployments

## Troubleshooting

### Agent won't initialize
```python
# Check config validation
from config.validator import ConfigValidator
ConfigValidator.print_validation_report(config)
```

### Role not responding
```python
# Check role status
role = agent.get_role("RoleName")
if role:
    print(role.get_state())
else:
    print("Role not registered")
```

### High memory usage
```python
# Reduce event history size
agent.event_bus.max_history = 1000  # default is 10000

# Create periodic checkpoints
checkpoint = await agent.create_state_checkpoint()
```

## Next Steps

1. **Read the docs**
   - [Architecture](docs/FRAMEWORK_DESIGN.md)
   - [Agent Specs](docs/AGENTS_SPECIFICATION.md)
   - [Implementation Guide](docs/IMPLEMENTATION_GUIDE.md)

2. **Explore examples**
   - Run `python example.py`
   - Review example scenarios
   - Customize for your use case

3. **Run tests**
   - `pytest tests/ -v`
   - Review test coverage
   - Add custom tests

4. **Deploy**
   - Validate configuration
   - Deploy agent nodes
   - Monitor metrics

## Getting Help

- Check [TESTING.md](TESTING.md) for test documentation
- Review [docs/](docs/) for architecture details
- Look at [example.py](example.py) for usage patterns
- Review role implementations in [src/roles/](src/roles/)

---

**Happy decentralized agent building! 🚀**

---

## Legal notice

SYNTHOS Collective, SYNTHOS, and related names, marks, documentation, and technical materials in this document are the **exclusive property of James G. Isham Williams, Sr.** Unauthorized reproduction, distribution, or commercial use without express written permission is prohibited except as allowed under applicable open-source licenses for identified files. No rights are waived.

This document is informational only and is not legal, financial, or investment advice. The canonical legal notice is in **docs/LEGAL_NOTICE.md** in the SYNTHOS Collective repository.
