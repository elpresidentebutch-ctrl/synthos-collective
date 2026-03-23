# SYNTHOS Agent Immutable Parameters & Constraints

## Overview

These are the immutable laws that govern all SYNTHOS agents. No agent can violate these constraints, regardless of their role, stake, or governance votes. They form the foundation of the agent civilization.

---

## A. Identity Constraint

**Immutable Law**: Every agent must have a unique, verifiable identity.

### Requirements
- **DID (Decentralized Identifier)**: Unique identifier across all chains
- **Keypair**: Asymmetric cryptography for signing and verification
- **Integrity Proof**: Cryptographic proof of identity authenticity
- **Non-Clone Guarantee**: No two agents can have identical identity parameters

### Implementation

```python
from src.governance.constraints import (
    IdentityConstraint, 
    AgentIdentity,
    ConstraintViolation,
    ConstraintType
)

# Initialize identity constraint
identity_constraint = IdentityConstraint()

# Register agent identity
await identity_constraint.register_agent(
    agent_id="agent_synthos_001",
    did="did:synthos:abc123def456",
    public_key="ed25519_public_key_xyz",
    private_key_hash="hash_of_private_key",
    inception_block_height=0,
    integrity_proof="merkle_proof_xyz123",
    genesis_signature="genesis_sig_abc"
)

# Detect clones
clones = await identity_constraint.detect_clones()
if clones:
    print(f"⚠ Clone detected: {clones}")
    # Immediately slash and isolate clones

# Verify agent identity
is_valid = await identity_constraint.verify_agent_identity(
    agent_id="agent_synthos_001",
    signature="sig_action_xyz",
    message="action_data"
)
```

### Violation Penalties
- **Fine**: 10% of stake
- **Slashing**: Complete removal of all tokens
- **Network**: Permanent ban from network
- **Time**: 100 years (permanent)

### Anti-Cloning Measures
```python
class CloneDetectionSystem:
    """Detects cloned agents in real-time."""
    
    async def detect_clone(self, agent_id: str, origin: str) -> bool:
        """
        Detect if agent is operating from multiple origins simultaneously.
        
        Returns:
            True if clone detected
        """
        last_seen = self.agent_locations.get(agent_id)
        current_location = origin
        
        if last_seen and last_seen != current_location:
            # Check time since last activity
            time_diff = time.time() - self.agent_lasttime[agent_id]
            
            if time_diff < 10:  # Less than 10 seconds
                # Agent active in two places - clone detected!
                return True
        
        self.agent_locations[agent_id] = current_location
        self.agent_lasttime[agent_id] = time.time()
        return False
```

---

## B. Constitutional Constraint

**Immutable Law**: Agents cannot break rules, propose invalid blocks, or violate consensus.

### Requirements
- **Rule Enforcement**: Active constitutional rules are mandatory
- **Block Validity**: Blocks must conform to protocol rules
- **Consensus Adherence**: All agents must follow consensus procedure

### Implementation

```python
from src.governance.constraints import ConstitutionalConstraint

# Initialize constitutional constraint
constitutional = ConstitutionalConstraint()

# Add rules (immutable once added)
await constitutional.add_rule(
    rule_id="rule_001_valid_transactions",
    rule_category="validation",
    enforcement_level="mandatory",
    rule_condition=async_validate_transaction,
    penalty_handler=slash_for_invalid_block
)

# Check compliance before action
is_compliant, violation_msg = await constitutional.check_compliance(
    agent_id="validator_node_1",
    action={
        "type": "propose_block",
        "transactions": [tx1, tx2, tx3]
    }
)

if not is_compliant:
    # Action blocked - violates constitution
    print(f"✗ Violation: {violation_msg}")
```

### Constitutional Rules Examples

```python
# Rule: No double-spending
async def rule_no_double_spend(agent_id, action):
    """Ensure transaction doesn't double-spend."""
    if action["type"] == "submit_transaction":
        sender = action["transaction"].sender
        amount = action["transaction"].amount
        
        pending_balance = await get_pending_balance(sender)
        balance = await get_confirmed_balance(sender)
        
        if pending_balance < amount:
            return False  # Violates rule
    
    return True

# Rule: No invalid block proposer
async def rule_valid_proposer(agent_id, action):
    """Only validators can propose blocks."""
    if action["type"] == "propose_block":
        if not await is_validator(agent_id):
            return False  # Only validators can propose
    
    return True

# Rule: No consensus violation
async def rule_valid_consensus(agent_id, action):
    """Must follow consensus procedure."""
    if action["type"] == "vote_on_block":
        # Can only vote once per block
        if await has_already_voted(agent_id, action["block_height"]):
            return False
        
        # Must vote within voting period
        if not await is_in_voting_window(action["block_height"]):
            return False
    
    return True
```

### Violation Penalties
- **Minor violation**: 5% stake slash
- **Major violation**: 50% stake slash
- **Critical violation**: 100% stake slash + ban

---

## C. Deterministic Constraint

**Immutable Law**: All reasoning must be reproducible and all outputs must be verifiable.

### Requirements
- **Reproducible Decisions**: Same input → same output always
- **Verifiable Output**: Output hash must match expected
- **Deterministic Execution**: No randomness in critical paths

### Implementation

```python
from src.governance.constraints import DeterministicConstraint

# Initialize deterministic constraint
deterministic = DeterministicConstraint()

# Log operation with determinism verification
is_deterministic, outputs = await deterministic.verify_determinism(
    agent_id="analyst_agent_1",
    operation_type="pattern_detection",
    inputs={"data": market_data, "threshold": 0.8},
    operation_func=detect_market_patterns
)

if not is_deterministic:
    raise ConstraintViolation(
        ConstraintType.DETERMINISTIC,
        "Operation produces non-deterministic results"
    )

# Verify output against expected hash
expected_hash = "sha256_abc123..."
await deterministic.verify_output_hash(outputs, expected_hash)
```

### Determinism Examples

```python
# ✓ DETERMINISTIC - Same input always produces same output
async def calculate_fee_deterministic(tx_size_bytes: int) -> int:
    """Deterministic fee calculation."""
    base_fee = 1  # constant
    per_byte_fee = 0  # constant
    return base_fee + (tx_size_bytes * per_byte_fee)

# ✗ NON-DETERMINISTIC - Uses random number
async def calculate_fee_random(tx_size_bytes: int) -> int:
    """Non-deterministic fee calculation."""
    random_multiplier = random.random()  # Changes each time!
    return int(tx_size_bytes * random_multiplier)

# ✓ DETERMINISTIC - Uses time for ordering but consistent within epoch
async def rank_proposals_deterministic(proposals: List) -> List:
    """Deterministic ranking based on deterministic factors."""
    # Sort by: stake (desc), creation time (asc), ID (asc)
    return sorted(
        proposals,
        key=lambda p: (-p.proposer_stake, p.timestamp, p.proposal_id)
    )
```

### Verification Process

```python
class DeterminismValidator:
    """Validates determinism of agent operations."""
    
    async def verify_operation(self, operation_id: str) -> bool:
        """Replay operation and verify determinism."""
        
        # Get original operation
        original_op = await fetch_operation(operation_id)
        
        # Replay with same inputs
        replayed_op = await original_op.function(*original_op.inputs)
        
        # Compare outputs
        if original_op.output != replayed_op:
            # Non-deterministic!
            raise ConstraintViolation(
                ConstraintType.DETERMINISTIC,
                f"Operation {operation_id} is non-deterministic"
            )
        
        return True
```

### Violation Penalties
- **First violation**: Warning + detailed audit required
- **Repeated violations**: 20% stake slash
- **Systematic violations**: 100% stake slash + ban

---

## D. Economic Constraint

**Immutable Law**: Agents must pay to act, risk stake, and accept slashing.

### Requirements
- **Cost of Action**: Every action has a minimum cost
- **Stake Requirements**: Critical actions require minimum stake
- **Slashing Acceptance**: Must accept penalties for violations

### Implementation

```python
from src.governance.constraints import EconomicConstraint

# Initialize economic constraint
economic = EconomicConstraint()

# Set up cost structure
economic.transaction_costs = {
    "submit_transaction": 1,      # 1 token
    "propose_block": 10,          # 10 tokens
    "propose_governance": 50,     # 50 tokens
    "challenge_block": 100,       # 100 tokens
    "become_validator": 100000    # 100k tokens
}

# Charge for action
await economic.charge_for_action(
    agent_id="citizen_1",
    action_type="submit_transaction"
)
# Deducts 1 token from citizen_1's balance

# Require minimum stake for validator role
await economic.require_stake(
    agent_id="validator_1",
    action_type="propose_block",
    minimum_stake=100000
)

# Apply slashing for violation
amount_slashed = await economic.apply_slashing(
    agent_id="malicious_validator",
    slash_percentage=50,
    reason="Double signing"
)
# Reduces malicious_validator's stake by 50%
```

### Cost Structure

```python
# Tiered action costs
MINIMUM_FEES = {
    "transaction": 1,              # Base: 1 token
    "block_proposal": 10,          # Base: 10 tokens
    "governance_proposal": 50,     # Base: 50 tokens
    "governance_vote": 0,          # Free (encouraged)
    "stake_increase": 0,           # Free (encouraged)
    "stake_decrease": 10,          # 10 tokens (discourages exit)
    "challenge_block": 100,        # 100 tokens (high bar)
    "appeal_slashing": 1000        # 1000 tokens (high bar)
}

# Stake requirements
MINIMUM_STAKES = {
    "validator": 100000,           # 100k tokens
    "governor": 10000,             # 10k tokens
    "proposal_owner": 1000,        # 1k tokens
    "ecosystem_role": 10000        # 10k tokens
}

# Slashing rates
SLASHING_PENALTIES = {
    "invalid_transaction": 1,      # 1%
    "invalid_block": 10,           # 10%
    "double_signing": 50,          # 50%
    "downtime": 2,                 # 2%
    "governance_violation": 25     # 25%
}
```

### Violation Penalties
- **Insufficient balance**: Action blocked, no execution
- **Insufficient stake**: Action blocked until stake raised
- **Slashing**: Percentage of stake removed, coins burned

---

## E. Resource Constraint

**Immutable Law**: Agents have bounded compute, memory, and bandwidth.

### Requirements
- **CPU Time Limit**: Max 5 seconds per operation
- **Memory Limit**: Max 1GB per agent instance
- **Network Limit**: Max 100 Mbps sustained bandwidth

### Implementation

```python
from src.governance.constraints import ResourceConstraint

# Initialize with limits
resource = ResourceConstraint(
    max_compute_ms=5000,        # 5 seconds
    max_memory_mb=1024,         # 1GB
    max_bandwidth_mbps=100      # 100 Mbps
)

# Check compute budget
try:
    await resource.check_compute_budget(
        agent_id="simulator_1",
        operation_time_ms=4500
    )
except ConstraintViolation as e:
    print(f"✗ Compute budget exceeded: {e}")

# Check memory budget
try:
    await resource.check_memory_budget(
        agent_id="analyzer_1",
        memory_needed_mb=512
    )
except ConstraintViolation as e:
    print(f"✗ Memory budget exceeded: {e}")

# Check bandwidth budget
try:
    await resource.check_bandwidth_budget(
        agent_id="communicator_1",
        bandwidth_needed_mbps=50
    )
except ConstraintViolation as e:
    print(f"✗ Bandwidth budget exceeded: {e}")
```

### Resource Quotas

```python
# Per-operation limits
OPERATION_LIMITS = {
    "validate_transaction": {"compute_ms": 100, "memory_mb": 10, "bandwidth_mbps": 0.1},
    "validate_block": {"compute_ms": 500, "memory_mb": 50, "bandwidth_mbps": 1},
    "consensus_round": {"compute_ms": 1000, "memory_mb": 100, "bandwidth_mbps": 5},
    "governance_vote": {"compute_ms": 50, "memory_mb": 5, "bandwidth_mbps": 0.01},
    "pattern_detection": {"compute_ms": 4000, "memory_mb": 800, "bandwidth_mbps": 10}
}

# Enforcement mechanisms
class ResourceEnforcer:
    """Enforces resource limits."""
    
    async def enforce_operation(self, agent_id: str, operation_type: str):
        """Enforce resource limits for operation."""
        limits = OPERATION_LIMITS[operation_type]
        
        # Measure actual usage
        usage = await measure_operation_resources(agent_id, operation_type)
        
        for resource_type, limit in limits.items():
            if usage[resource_type] > limit:
                # Over limit - terminate operation
                await terminate_operation(agent_id, operation_type)
                raise ConstraintViolation(
                    ConstraintType.RESOURCE,
                    f"{resource_type} exceeded: {usage[resource_type]} > {limit}"
                )
```

### Violation Penalties
- **Minor overrun**: 1% penalties
- **Moderate overrun**: 10% penalties
- **Severe overrun**: Operation terminated + 25% slash
- **Systematic violations**: Ban from role

---

## F. Safety Constraint

**Immutable Law**: No harmful actions, no infinite loops, no runaway proposals.

### Requirements
- **Blocked Actions**: Certain actions are forbidden
- **Recursion Limits**: Maximum call stack depth
- **Rate Limiting**: Maximum proposals per time period

### Implementation

```python
from src.governance.constraints import SafetyConstraint

# Initialize safety constraint
safety = SafetyConstraint()

# Check action safety
try:
    await safety.check_action_safety(
        agent_id="agent_1",
        action_type="propose_slashing_all_validators"
    )
except ConstraintViolation as e:
    print(f"✗ Harmful action blocked: {e}")

# Check recursion depth
try:
    await safety.check_recursion_depth(current_depth=45)  # OK
    await safety.check_recursion_depth(current_depth=101)  # Exceeds limit
except ConstraintViolation as e:
    print(f"✗ Recursion limit exceeded: {e}")

# Check proposal rate
try:
    await safety.check_proposal_rate(agent_id="agent_1")
    # Called 10 times in one hour
    await safety.check_proposal_rate(agent_id="agent_1")
    # Now exceeds rate limit
except ConstraintViolation as e:
    print(f"✗ Proposal rate exceeded: {e}")
```

### Blocked Actions

```python
# Actions that are never allowed
BLOCKED_ACTIONS = {
    "delete_all_blocks",          # Erase blockchain
    "steal_tokens",               # Transfer others' tokens
    "fork_consensus",             # Create network split
    "drain_validator_pool",       # Empty staking pool
    "disable_byzantine_tolerance", # Break safety
    "revert_finalized_blocks",    # Undo finalized blocks
    "modify_genesis",             # Change genesis state
    "disable_identity_verification", # Remove DID verification
    "delete_constitutional_rules", # Remove all rules
    "infinite_loop_proposal"      # Intentionally infinite
}

# Rate limits
RATE_LIMITS = {
    "proposals_per_hour": 10,
    "governance_votes_per_hour": 100,
    "blocks_per_minute": 12,
    "transactions_per_second": 10000,
    "slashing_per_day": 5  # Max 5 slashing events per day
}
```

### Infinite Loop Detection

```python
class InfiniteLoopDetector:
    """Detects and prevents infinite loops."""
    
    async def detect_loop(self, operation_id: str, max_iterations: int = 1000000):
        """Detect if operation has infinite loop."""
        operation = await fetch_operation(operation_id)
        
        iteration_count = 0
        previous_states = set()
        
        while iteration_count < max_iterations:
            state = await get_operation_state(operation_id)
            state_hash = hash(str(sorted(state.items())))
            
            if state_hash in previous_states:
                # Same state seen before - infinite loop!
                raise ConstraintViolation(
                    ConstraintType.SAFETY,
                    f"Infinite loop detected in {operation_id}"
                )
            
            previous_states.add(state_hash)
            iteration_count += 1
        
        if iteration_count >= max_iterations:
            # Hit iteration limit - likely infinite
            raise ConstraintViolation(
                ConstraintType.SAFETY,
                f"Operation exceeded iteration limit: {max_iterations}"
            )
        
        return False  # No infinite loop
```

### Violation Penalties
- **Blocked action attempt**: Action blocked + 10% slash
- **Infinite loop**: Operation terminated + 25% slash
- **Rate limit exceeded**: Operation throttled + 5% slash
- **Repeated violations**: 100% slash + ban

---

## G. Time Constraint

**Immutable Law**: Agents operate in epochs and must respect proposal/challenge windows.

### Requirements
- **Epoch-based Operation**: Time divided into fixed epochs
- **Proposal Windows**: Proposals only accepted during specific windows
- **Challenge Windows**: Blocks can only be challenged within 7 days

### Implementation

```python
from src.governance.constraints import TimeConstraint

# Initialize with time parameters
time_constraint = TimeConstraint(
    epoch_duration_seconds=3600,        # 1 hour
    proposal_window_seconds=86400,      # 1 day
    challenge_window_seconds=604800     # 7 days
)

# Get current epoch
current_epoch = await time_constraint.get_current_epoch()
print(f"Current epoch: {current_epoch}")

# Check if in proposal window
try:
    await time_constraint.check_proposal_window()
    print("✓ Currently in proposal window")
except ConstraintViolation as e:
    print(f"✗ Not in proposal window: {e}")

# Check if block is within challenge window
try:
    await time_constraint.check_challenge_window(block_timestamp)
    print("✓ Block is within challenge window")
except ConstraintViolation as e:
    print(f"✗ Block is outside challenge window: {e}")

# Schedule proposal windows
await time_constraint.schedule_proposal_window(
    start_time=time.time() + 3600,
    duration_seconds=86400
)

# Advance epoch
next_epoch = await time_constraint.advance_epoch()
```

### Epoch Structure

```python
# Timeline for each epoch (1 hour)
EPOCH_TIMELINE = {
    "0-15 min": "Proposal window open",
    "15-30 min": "Proposal deadline - no new proposals",
    "30-45 min": "Consensus voting on proposals",
    "45-60 min": "Enforcement and block finalization"
}

# Block finality timeline
BLOCK_FINALITY_TIMELINE = {
    "0-10 min": "Block proposed - challenge window open",
    "10 min - 7 days": "Challenge window open - validators can challenge",
    "7 days+": "Block finalized - cannot be challenged"
}

# Challenge-dispute timeline for governance
CHALLENGE_DISPUTE_TIMELINE = {
    "proposal_passed": "t=0",
    "challenge_period_open": "t=0 to t+7days",
    "challenge_filing_deadline": "t+7days",
    "dispute_resolution_period": "t+7days to t+14days",
    "execution_allowed": "t+14days"
}
```

### Enforcement

```python
class TimeConstraintEnforcer:
    """Enforces time constraints."""
    
    async def enforce_proposal_window(self):
        """Only accept proposals during proposal window."""
        
        if not await time_constraint.check_proposal_window():
            raise ConstraintViolation(
                ConstraintType.TIME,
                "Proposals only accepted during proposal window"
            )
    
    async def enforce_challenge_window(self, block_timestamp: float):
        """Only allow challenges within window."""
        
        if not await time_constraint.check_challenge_window(block_timestamp):
            raise ConstraintViolation(
                ConstraintType.TIME,
                f"Block outside 7-day challenge window"
            )
    
    async def epoch_transition(self):
        """Handle transition between epochs."""
        
        new_epoch = await time_constraint.advance_epoch()
        
        # Reset resources
        await resource_constraint.reset_period()
        
        # Clear old proposals
        await clear_expired_proposals()
        
        # Finalize blocks
        await finalize_old_blocks()
```

### Violation Penalties
- **Proposal outside window**: Proposal blocked
- **Challenge too late**: Challenge rejected
- **Timing manipulation**: 10% slash + investigation

---

## Unified Constraint System

### Central Enforcer

```python
from src.governance.constraints import ConstraintEnforcer

# Initialize central constraint enforcer
enforcer = ConstraintEnforcer()

# Enforce all constraints before any agent action
async def execute_agent_action(agent_id: str, action_type: str, action_data: Dict):
    """Execute action with full constraint checking."""
    
    # Check all constraints
    is_allowed, error_msg = await enforcer.check_all_constraints(
        agent_id=agent_id,
        action_type=action_type,
        action_data=action_data
    )
    
    if not is_allowed:
        print(f"✗ Action blocked: {error_msg}")
        return False
    
    # All constraints passed - execute action
    print(f"✓ Executing: {action_type}")
    return await execute_action(agent_id, action_type, action_data)
```

### Constraint Violation Escalation

```
Level 1: Warning (log + broadcast)
  ├─ Agent publishes apology + corrective action

Level 2: Light Penalty (1-5% slash)
  ├─ Agent loses some stake
  ├─ But can continue operating

Level 3: Medium Penalty (10-25% slash)
  ├─ Significant stake loss
  ├─ May affect role capacity

Level 4: Heavy Penalty (50% slash)
  ├─ Majority stake lost
  ├─ May force role removal

Level 5: Severe Penalty (100% slash + ban)
  ├─ Complete stake loss
  ├─ Permanent network ban
  ├─ Identity revocation
```

---

## Constraint Auditing

### Audit System

```python
class ConstraintAuditSystem:
    """Audit constraint compliance."""
    
    async def audit_agent_compliance(self, agent_id: str) -> Dict:
        """Check if agent is following all constraints."""
        
        audit_result = {
            "agent_id": agent_id,
            "timestamp": time.time(),
            "constraints": {}
        }
        
        # Audit each constraint
        constraints = [
            ("identity", enforcer.identity),
            ("constitutional", enforcer.constitutional),
            ("deterministic", enforcer.deterministic),
            ("economic", enforcer.economic),
            ("resource", enforcer.resource),
            ("safety", enforcer.safety),
            ("time", enforcer.time)
        ]
        
        for name, constraint_system in constraints:
            audit_result["constraints"][name] = await constraint_system.audit(agent_id)
        
        return audit_result
```

---

These seven immutable constraints form the foundation of agent governance, ensuring that all agents, regardless of power or position, must obey the laws of the civilization.

---

## Legal notice

SYNTHOS Collective, SYNTHOS, and related names, marks, documentation, and technical materials in this document are the **exclusive property of James G. Isham Williams, Sr.** Unauthorized reproduction, distribution, or commercial use without express written permission is prohibited except as allowed under applicable open-source licenses for identified files. No rights are waived.

This document is informational only and is not legal, financial, or investment advice. The canonical legal notice is in **docs/LEGAL_NOTICE.md** in the SYNTHOS Collective repository.
