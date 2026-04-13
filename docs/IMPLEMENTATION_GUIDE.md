# SYNTHOS Agent Implementation Guide

## Project Structure

```
SYNTHOS COLLECTIVE/
├── docs/
│   ├── AGENTS_SPECIFICATION.md          # Detailed role specifications
│   ├── FRAMEWORK_DESIGN.md              # Architecture and design
│   ├── IMPLEMENTATION_GUIDE.md          # This file
│   └── API_REFERENCE.md                 # API documentation
├── src/
│   ├── __init__.py
│   ├── core/
│   │   ├── __init__.py
│   │   ├── agent.py                     # Main SyntHOSAgent class
│   │   ├── state.py                     # State management
│   │   ├── event.py                     # Event bus system
│   │   └── base_role.py                 # Base role interface
│   ├── roles/
│   │   ├── __init__.py
│   │   ├── validator.py                 # Validator role
│   │   ├── economist.py                 # Economist role
│   │   ├── governor.py                  # Governor role
│   │   ├── communicator.py              # Communicator role
│   │   ├── simulator.py                 # Simulator role
│   │   ├── enforcer.py                  # Enforcer role
│   │   └── citizen.py                   # Citizen role
│   ├── models/
│   │   ├── __init__.py
│   │   ├── transaction.py               # Transaction model
│   │   ├── block.py                     # Block model
│   │   ├── vote.py                      # Vote/proposal models
│   │   └── metrics.py                   # Metrics models
│   ├── consensus/
│   │   ├── __init__.py
│   │   └── engine.py                    # Consensus engine
│   ├── storage/
│   │   ├── __init__.py
│   │   └── state_store.py               # Persistent state
│   ├── network/
│   │   ├── __init__.py
│   │   └── peer_manager.py              # Peer management
│   ├── crypto/
│   │   ├── __init__.py
│   │   └── crypto.py                    # Cryptographic utilities
│   └── utils/
│       ├── __init__.py
│       ├── logger.py                    # Logging utilities
│       ├── config.py                    # Configuration loader
│       └── constants.py                 # Constants
├── tests/
│   ├── __init__.py
│   ├── test_agent.py                    # Agent tests
│   ├── test_roles/
│   │   ├── test_validator.py
│   │   ├── test_economist.py
│   │   ├── test_governor.py
│   │   └── ... (other roles)
│   └── integration/
│       └── test_integration.py
├── config/
│   ├── default.yaml                     # Default configuration
│   ├── mainnet.yaml                     # Mainnet config
│   ├── testnet.yaml                     # Testnet config
│   └── devnet.yaml                      # Development config
├── requirements.txt                     # Python dependencies
├── setup.py                             # Package setup
└── README.md                            # Project README
```

## Development Phases

### Phase 1: Core Infrastructure (Week 1)
- [x] Base classes and interfaces
- [x] State management system
- [x] Event bus implementation
- [ ] Configuration management
- [ ] Logging and monitoring

### Phase 2: Role Implementation (Week 2)
- [ ] Validator role
- [ ] Economist role
- [ ] Governor role
- [ ] Communicator role
- [ ] Simulator role
- [ ] Enforcer role
- [ ] Citizen role

### Phase 3: Integration (Week 3)
- [ ] Consensus engine integration
- [ ] Network layer integration
- [ ] End-to-end transaction flow
- [ ] Governance flow
- [ ] Cross-role communication

### Phase 4: Testing & Optimization (Week 4)
- [ ] Unit tests for all roles
- [ ] Integration tests
- [ ] Performance benchmarking
- [ ] Security audits

## Implementation Patterns

### Role Implementation Template

Every role should follow this pattern:

```python
from src.core.base_role import Role
from src.core.event import Event, EventType

class YourRole(Role):
    def __init__(self, agent):
        super().__init__(agent)
        self.name = "YourRole"
        self.version = "1.0.0"
        
    async def initialize(self):
        """Initialize role resources"""
        self.agent.event_bus.subscribe(EventType.SPECIFIC_EVENT, self.handle_event)
        
    async def execute(self):
        """Main role execution loop"""
        # Implement main logic
        pass
        
    async def finalize(self):
        """Cleanup resources"""
        pass
        
    async def handle_event(self, event: Event):
        """Handle incoming events"""
        await self.process_event(event)
        
    async def process_event(self, event: Event):
        """Process specific event types"""
        # Implement event processing
        pass
```

### State Access Pattern

All roles access shared state through the state manager:

```python
# Reading state
ledger = await self.agent.state.get('ledger')
consensus = await self.agent.state.get('consensus')

# Writing state
await self.agent.state.set('ledger', updated_ledger)

# Committing changes
await self.agent.state.commit()

# Rolling back on error
await self.agent.state.rollback()
```

### Event Publishing Pattern

Roles publish events to coordinate actions:

```python
# Publishing events
event = Event(
    type=EventType.TRANSACTION_RECEIVED,
    source='communicator',
    data={'transaction': tx},
    priority=1
)
await self.agent.event_bus.publish(event)

# Subscribing to events
self.agent.event_bus.subscribe(
    EventType.VALIDATION_COMPLETE,
    self.handle_validation_complete
)
```

## Data Models

### Transaction
```python
class Transaction:
    - id: str
    - sender: Address
    - recipient: Address
    - amount: int
    - fee: int
    - nonce: int
    - signature: bytes
    - timestamp: int
```

### Block
```python
class Block:
    - height: int
    - proposer: Address
    - previous_hash: bytes
    - transactions: List[Transaction]
    - timestamp: int
    - hash: bytes
    - signature: bytes
```

### Proposal
```python
class Proposal:
    - id: str
    - proposer: Address
    - change_type: str
    - parameters: dict
    - vote_deadline: int
    - votes_for: int
    - votes_against: int
```

## Error Handling

### Exception Hierarchy
```
SyntHOSException
├── ValidationException
├── ConsensusException
├── NetworkException
├── StateException
└── ConfigException
```

## Configuration Format

Default configuration uses YAML:

```yaml
agent:
  id: agent-001
  network: testnet
  
roles:
  validator:
    enabled: true
    timeout_ms: 5000
    
  economist:
    enabled: true
    fee_model: dynamic
    
  # ... other roles

consensus:
  engine: hotstuff
  timeout_ms: 4000
  
storage:
  backend: rocksdb
  path: ./data
```

## Async/Await Patterns

All I/O operations use async/await:

```python
async def validate_transaction(self, tx):
    # Verify signature
    is_valid = await self.verify_signature(tx)
    if not is_valid:
        raise ValidationException("Invalid signature")
    
    # Check balance
    balance = await self.agent.state.get_balance(tx.sender)
    if balance < tx.amount + tx.fee:
        raise ValidationException("Insufficient funds")
    
    return True
```

## Testing Strategy

### Unit Test Template
```python
import pytest
from src.roles.validator import ValidatorRole
from src.models.transaction import Transaction

@pytest.mark.asyncio
async def test_validate_transaction_valid():
    # Setup
    validator = ValidatorRole(mock_agent)
    tx = Transaction(...)
    
    # Execute
    result = await validator.validate_transaction(tx)
    
    # Assert
    assert result is True
```

### Integration Test Template
```python
@pytest.mark.asyncio
async def test_transaction_flow():
    # Setup
    agent = SyntHOSAgent(config)
    
    # Execute
    tx = Transaction(...)
    await agent.submit_transaction(tx)
    await agent.process_events()
    
    # Assert
    ledger = await agent.state.get('ledger')
    assert tx.id in ledger.transactions
```

## Performance Considerations

### Optimization Guidelines

1. **Caching**: Cache frequently accessed state
   ```python
   self._balance_cache = {}
   ```

2. **Batch Processing**: Process events in batches
   ```python
   events = await self.event_bus.get_batch(size=100)
   ```

3. **Async I/O**: Use async for all blocking operations
   ```python
   await storage.read(key)
   ```

4. **Connection Pooling**: Maintain persistent connections
   ```python
   self.db_pool = await create_pool(...)
   ```

## Deployment

### Docker Setup
```dockerfile
FROM python:3.10-slim
WORKDIR /app
COPY . .
RUN pip install -r requirements.txt
CMD ["python", "-m", "src.main"]
```

### Environment Variables
```
AGENT_ID=agent-001
NETWORK=testnet
LOG_LEVEL=INFO
CONSENSUS_TIMEOUT=4000
```

## Monitoring & Observability

### Metrics to Track
- Transaction validation rate
- Block proposal time
- Consensus finality time
- Network peer count
- Stake amount
- Reputation score

### Logging
```python
logger.info(f"Agent {self.agent.id} processing event", extra={
    'role': self.name,
    'event_type': event.type,
    'timestamp': time.time()
})
```

