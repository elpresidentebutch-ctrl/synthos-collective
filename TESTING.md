# SYNTHOS Collective - Testing Guide

## Overview

This document describes the comprehensive testing suite for the SYNTHOS Collective framework.

## Test Structure

Tests are organized into two main categories:

### 1. Core Tests (`tests/test_core.py`)

Tests for core framework components:

- **TestAgentConfig** - Configuration handling and defaults
- **TestAgentState** - State management, transactions, snapshots
- **TestEventSystem** - Event bus, subscriptions, history
- **TestSyntHOSAgent** - Agent initialization, role management, status

### 2. Role Tests (`tests/test_roles.py`)

Tests for all seven core roles:

- **TestValidatorRole** - Transaction/block validation
- **TestEconomistRole** - Fee calculation, reward distribution
- **TestGovernorRole** - Proposal creation, voting
- **TestCommunicatorRole** - Peer connections, messaging
- **TestEnforcerRole** - Penalty application, compliance
- **TestSimulatorRole** - Protocol and economic simulations
- **TestCitizenRole** - Transaction submission, staking, rewards

## Running Tests

### Prerequisites

Install test dependencies:

```bash
pip install pytest pytest-asyncio
```

### Run All Tests

```bash
pytest tests/ -v
```

### Run Specific Test File

```bash
# Core tests
pytest tests/test_core.py -v

# Role tests
pytest tests/test_roles.py -v
```

### Run Specific Test Class

```bash
pytest tests/test_core.py::TestAgentConfig -v
pytest tests/test_roles.py::TestValidatorRole -v
```

### Run Specific Test

```bash
pytest tests/test_core.py::TestAgentConfig::test_agent_config_creation -v
```

## Testing Coverage

### Core Infrastructure (test_core.py)

#### Configuration Tests
- ✓ AgentConfig creation with all parameters
- ✓ AgentConfig with default values
- ✓ Configuration validation

#### State Management Tests
- ✓ Set and get state values
- ✓ Balance operations
- ✓ Transaction begin/commit
- ✓ Transaction rollback
- ✓ State snapshots
- ✓ State restoration
- ✓ Fork at version

#### Event System Tests
- ✓ Event creation with attributes
- ✓ Event bus subscription
- ✓ Event publishing and dispatch
- ✓ Event history tracking
- ✓ Event filtering by type

#### Agent Tests
- ✓ Agent initialization
- ✓ Role registration
- ✓ Agent status reporting
- ✓ State checkpoints
- ✓ Event history retrieval
- ✓ Role enable/disable

### Roles (test_roles.py)

#### Validator Role
- ✓ Transaction validation logic
- ✓ Block validation
- ✓ Signature verification
- ✓ Nonce checking
- ✓ Fee validation

#### Economist Role
- ✓ Fee calculation based on transaction size
- ✓ Block reward calculation
- ✓ Reward distribution to addresses
- ✓ Economic metrics tracking

#### Governor Role
- ✓ Proposal creation and storage
- ✓ Voting mechanics (yes/no)
- ✓ Vote counting
- ✓ Proposal finalization
- ✓ Governance metrics

#### Communicator Role
- ✓ Peer connection management
- ✓ Peer disconnection
- ✓ Message sending (unicast)
- ✓ Message broadcasting
- ✓ Network metrics tracking

#### Enforcer Role
- ✓ Penalty application to violators
- ✓ Balance reduction on violations
- ✓ Slashing events
- ✓ Compliance checking

#### Simulator Role
- ✓ Protocol change simulation
- ✓ Economic scenario simulation
- ✓ Network condition simulation
- ✓ Simulation metrics tracking

#### Citizen Role
- ✓ Transaction submission
- ✓ Token staking
- ✓ Reward claiming
- ✓ Governance participation
- ✓ Citizen metrics

## Test Execution Examples

### Example 1: Test Configuration Management

```bash
pytest tests/test_core.py::TestAgentConfig -v

# Output:
# test_agent_config_creation PASSED
# test_agent_config_defaults PASSED
```

### Example 2: Test Role Functionality

```bash
pytest tests/test_roles.py::TestValidatorRole -v

# Output:
# test_validator_initialization PASSED
# test_transaction_validation PASSED
```

### Example 3: Run All Tests with Coverage

```bash
pytest tests/ -v --tb=short

# Shows:
# - Test results for all components
# - Failure details if any
# - Coverage summary
```

## Asynchronous Testing

All tests that use async/await are marked with `@pytest.mark.asyncio`:

```python
@pytest.mark.asyncio
async def test_state_transaction():
    """Test state transactions"""
    state = AgentState()
    await state.begin_transaction()
    # ... test code ...
```

The pytest-asyncio plugin automatically handles the event loop.

## Test Fixtures

Common test fixture patterns:

```python
async def create_test_agent():
    """Helper to create properly initialized agent"""
    config = AgentConfig(id="test-agent", network="testnet")
    agent = SyntHOSAgent(config)
    
    # Register all roles
    agent.register_role(ValidatorRole(agent))
    # ... register other roles ...
    
    await agent.initialize()
    return agent
```

## Performance Considerations

- Tests complete in < 2 seconds total
- Individual test cases typically < 100ms
- Async tests use proper event loop handling
- No blocking I/O operations in tests

## Extending the Test Suite

### Adding New Tests

1. Create test function in appropriate test file
2. Mark async tests with `@pytest.mark.asyncio`
3. Follow naming convention: `test_<feature_being_tested>`
4. Use descriptive docstrings
5. Include setup, execution, and assertions

Example:

```python
@pytest.mark.asyncio
async def test_new_feature():
    """Test description"""
    # Setup
    agent = await create_test_agent()
    
    # Execute
    result = await agent.some_method()
    
    # Assert
    assert result == expected_value
```

## Debugging Tests

### Verbose Output

```bash
pytest tests/ -v -s
```

The `-s` flag shows print statements and logging output.

### Stop on First Failure

```bash
pytest tests/ -x
```

### Show Variables in Traceback

```bash
pytest tests/ -l
```

### Run Tests in Parallel

```bash
pytest tests/ -n auto
```

(Requires `pytest-xdist` package)

## Continuous Integration

These tests are designed to run in CI/CD pipelines:

```yaml
# Example GitHub Actions workflow
- name: Run Tests
  run: |
    pip install pytest pytest-asyncio
    pytest tests/ -v --tb=short
```

## Known Limitations

1. Tests use placeholder implementations for some role methods
2. Network simulation is minimal (no real P2P)
3. Cryptographic operations are mocked
4. Storage is in-memory only

## Future Enhancements

- [ ] Add performance benchmarks
- [ ] Add integration tests with real consensus
- [ ] Add stress tests with many agents
- [ ] Add network simulation tests
- [ ] Add security audit tests

## Troubleshooting

### pytest-asyncio issues

If tests fail with "event loop" errors:

```bash
pip install --upgrade pytest-asyncio
export PYTEST_TIMEOUT=300
```

### Import errors

Ensure you run pytest from project root:

```bash
cd /path/to/SYNTHOS\ COLLECTIVE
pytest tests/
```

### Timeout errors

For slow systems, increase timeout:

```bash
pytest tests/ --timeout=5
```

---

**For more information, see [docs/IMPLEMENTATION_GUIDE.md](../docs/IMPLEMENTATION_GUIDE.md)**
