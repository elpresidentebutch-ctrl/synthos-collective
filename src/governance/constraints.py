"""
SYNTHOS Agent Parameter Constraints

Implements immutable constraints that all agents must obey:
- Identity Constraint (DID, keypair, integrity proof, non-clone guarantee)
- Constitutional Constraint (rules, blocks, consensus)
- Deterministic Constraint (reproducible reasoning, verifiable outputs)
- Economic Constraint (pay to act, risk stake, accept slashing)
- Resource Constraint (bounded compute, memory, bandwidth)
- Safety Constraint (no harmful actions, no infinite loops, runaway proposals)
- Time Constraint (epochs, proposal windows, challenge windows)
"""

import hashlib
import time
from dataclasses import dataclass, field
from enum import Enum
from typing import Dict, Set, Optional, Any, List, Tuple
from uuid import uuid4


class ConstraintType(Enum):
    """Types of agent constraints."""
    IDENTITY = "identity"
    CONSTITUTIONAL = "constitutional"
    DETERMINISTIC = "deterministic"
    ECONOMIC = "economic"
    RESOURCE = "resource"
    SAFETY = "safety"
    TIME = "time"


class ConstraintViolation(Exception):
    """Raised when agent violates a constraint."""
    def __init__(self, constraint_type: ConstraintType, message: str):
        self.constraint_type = constraint_type
        super().__init__(f"{constraint_type.value}: {message}")


@dataclass
class AgentIdentity:
    """
    Agent identity with proof of uniqueness.
    
    Implements:
    - DID (Decentralized Identifier)
    - Keypair for signing
    - Integrity proof to prevent cloning
    - Non-clone guarantee
    """
    agent_id: str
    did: str  # Decentralized Identifier
    public_key: str  # Ed25519 or similar
    private_key_hash: str  # Hash of private key (never stored)
    creation_timestamp: float
    inception_block_height: int
    integrity_proof: str  # Merkle proof of identity
    genesis_signature: str  # Signature from network genesis
    
    # Anti-cloning measures
    active_clones: Set[str] = field(default_factory=set)
    clone_detection_timestamp: float = 0.0
    
    def __post_init__(self):
        """Validate identity."""
        if not self._is_valid_identity():
            raise ValueError("Invalid identity parameters")
    
    def _is_valid_identity(self) -> bool:
        """Validate identity structure."""
        return all([
            len(self.agent_id) > 0,
            len(self.did) > 0,
            len(self.public_key) > 0,
            len(self.integrity_proof) > 0,
            self.creation_timestamp > 0,
            self.inception_block_height >= 0
        ])
    
    def verify_clone(self, other_identity: 'AgentIdentity') -> bool:
        """
        Detect if another identity is a clone.
        
        Clones have same DID, keys, but appear at different times/places.
        """
        if self.did == other_identity.did:
            if self.public_key == other_identity.public_key:
                if abs(self.creation_timestamp - other_identity.creation_timestamp) < 60:
                    # Same DID/keys created within 1 minute = likely clone
                    return True
        return False


@dataclass
class ConstraintViolationRecord:
    """Record of constraint violation."""
    violation_id: str
    agent_id: str
    constraint_type: ConstraintType
    violation_timestamp: float
    severity: str  # "warning", "critical"
    message: str
    action_taken: Optional[str] = None
    slashing_amount: Optional[int] = None


class IdentityConstraint:
    """
    Enforces identity constraints.
    
    Requirements:
    - Every agent has DID
    - Every agent has keypair
    - Every agent has integrity proof
    - No cloning allowed
    """
    
    def __init__(self):
        """Initialize identity constraint."""
        self.identities: Dict[str, AgentIdentity] = {}
        self.did_registry: Dict[str, str] = {}  # did -> agent_id
        self.public_key_registry: Dict[str, str] = {}  # public_key -> agent_id
    
    async def register_agent(
        self,
        agent_id: str,
        did: str,
        public_key: str,
        private_key_hash: str,
        inception_block_height: int,
        integrity_proof: str,
        genesis_signature: str
    ) -> bool:
        """
        Register an agent identity.
        
        Raises:
            ConstraintViolation: If registration fails
        """
        # Check for existing agent
        if agent_id in self.identities:
            raise ConstraintViolation(
                ConstraintType.IDENTITY,
                f"Agent already registered: {agent_id}"
            )
        
        # Check for DID collision
        if did in self.did_registry:
            raise ConstraintViolation(
                ConstraintType.IDENTITY,
                f"DID already in use: {did}"
            )
        
        # Check for key collision (prevent simple cloning)
        if public_key in self.public_key_registry:
            raise ConstraintViolation(
                ConstraintType.IDENTITY,
                f"Keypair already in use: {public_key}"
            )
        
        # Create identity
        identity = AgentIdentity(
            agent_id=agent_id,
            did=did,
            public_key=public_key,
            private_key_hash=private_key_hash,
            creation_timestamp=time.time(),
            inception_block_height=inception_block_height,
            integrity_proof=integrity_proof,
            genesis_signature=genesis_signature
        )
        
        # Register
        self.identities[agent_id] = identity
        self.did_registry[did] = agent_id
        self.public_key_registry[public_key] = agent_id
        
        print(f"✓ Identity registered: {agent_id} ({did})")
        return True
    
    async def detect_clones(self) -> List[str]:
        """
        Detect cloned identities in the network.
        
        Returns:
            List of detected clone agent IDs
        """
        clones = []
        
        identities = list(self.identities.values())
        for i, identity1 in enumerate(identities):
            for identity2 in identities[i+1:]:
                if identity1.verify_clone(identity2):
                    clones.extend([identity1.agent_id, identity2.agent_id])
        
        return list(set(clones))
    
    async def verify_agent_identity(
        self,
        agent_id: str,
        signature: str,
        message: str
    ) -> bool:
        """
        Verify agent identity via signature.
        
        Returns:
            True if signature is valid
        """
        identity = self.identities.get(agent_id)
        
        if not identity:
            raise ConstraintViolation(
                ConstraintType.IDENTITY,
                f"Agent not registered: {agent_id}"
            )
        
        # Placeholder for actual signature verification
        # In production, use cryptographic verification
        return True


class ConstitutionalConstraint:
    """
    Enforces constitutional constraints.
    
    Requirements:
    - Agents cannot break rules
    - Agents cannot propose invalid blocks
    - Agents cannot violate consensus
    """
    
    def __init__(self):
        """Initialize constitutional constraint."""
        self.active_rules: Dict[str, Dict[str, Any]] = {}
        self.violations: List[ConstraintViolationRecord] = []
    
    async def add_rule(
        self,
        rule_id: str,
        rule_category: str,
        enforcement_level: str,
        rule_condition,
        penalty_handler
    ) -> bool:
        """Add constitutional rule."""
        self.active_rules[rule_id] = {
            "id": rule_id,
            "category": rule_category,
            "enforcement_level": enforcement_level,
            "condition": rule_condition,
            "penalty_handler": penalty_handler,
            "created_at": time.time()
        }
        return True
    
    async def check_compliance(
        self,
        agent_id: str,
        action: Dict[str, Any]
    ) -> Tuple[bool, Optional[str]]:
        """
        Check if agent action complies with constitution.
        
        Returns:
            (is_compliant, violation_message)
        """
        for rule_id, rule in self.active_rules.items():
            condition = rule["condition"]
            
            try:
                is_compliant = await condition(agent_id, action)
                
                if not is_compliant:
                    violation_message = f"Violated rule {rule_id}"
                    
                    # Record violation
                    violation = ConstraintViolationRecord(
                        violation_id=str(uuid4()),
                        agent_id=agent_id,
                        constraint_type=ConstraintType.CONSTITUTIONAL,
                        violation_timestamp=time.time(),
                        severity="critical" if rule["enforcement_level"] == "mandatory" else "warning",
                        message=violation_message
                    )
                    self.violations.append(violation)
                    
                    return False, violation_message
                    
            except Exception as e:
                return False, str(e)
        
        return True, None
    
    async def enforce_violation(
        self,
        agent_id: str,
        violation: ConstraintViolationRecord
    ) -> bool:
        """Enforce penalty for violation."""
        rule = self.active_rules.get(violation.constraint_type.value)
        
        if rule:
            penalty_handler = rule.get("penalty_handler")
            if penalty_handler:
                return await penalty_handler(agent_id, violation)
        
        return False


class DeterministicConstraint:
    """
    Enforces deterministic constraints.
    
    Requirements:
    - All reasoning must be reproducible
    - All outputs must be verifiable
    """
    
    def __init__(self):
        """Initialize deterministic constraint."""
        self.operation_log: List[Dict[str, Any]] = []
        self.state_hashes: List[Tuple[int, str]] = []  # (operation_number, hash)
    
    async def log_operation(
        self,
        agent_id: str,
        operation_type: str,
        inputs: Dict[str, Any],
        outputs: Dict[str, Any],
        state_hash: str
    ) -> bool:
        """
        Log a deterministic operation.
        
        Returns:
            True if operation is deterministic
        """
        operation_record = {
            "timestamp": time.time(),
            "agent_id": agent_id,
            "operation_type": operation_type,
            "inputs": inputs,
            "outputs": outputs,
            "state_hash": state_hash,
            "sequence": len(self.operation_log) + 1
        }
        
        self.operation_log.append(operation_record)
        self.state_hashes.append((operation_record["sequence"], state_hash))
        
        return True
    
    async def verify_determinism(
        self,
        agent_id: str,
        operation_type: str,
        inputs: Dict[str, Any],
        operation_func
    ) -> Tuple[bool, Dict[str, Any]]:
        """
        Verify operation is deterministic by replaying.
        
        Returns:
            (is_deterministic, outputs)
        """
        # Execute operation
        outputs1 = await operation_func(inputs)
        
        # Re-execute to verify determinism
        outputs2 = await operation_func(inputs)
        
        is_deterministic = outputs1 == outputs2
        
        if not is_deterministic:
            raise ConstraintViolation(
                ConstraintType.DETERMINISTIC,
                f"Non-deterministic operation: {operation_type}"
            )
        
        return is_deterministic, outputs1
    
    async def verify_output_hash(
        self,
        outputs: Dict[str, Any],
        expected_hash: str
    ) -> bool:
        """Verify output hash matches expected."""
        output_json = str(sorted(outputs.items()))
        computed_hash = hashlib.sha256(output_json.encode()).hexdigest()
        
        is_valid = computed_hash == expected_hash
        
        if not is_valid:
            raise ConstraintViolation(
                ConstraintType.DETERMINISTIC,
                f"Output hash mismatch"
            )
        
        return is_valid


class EconomicConstraint:
    """
    Enforces economic constraints.
    
    Requirements:
    - Agents must pay to act
    - Agents must risk stake
    - Agents must accept slashing
    """
    
    def __init__(self):
        """Initialize economic constraint."""
        self.agent_balances: Dict[str, int] = {}
        self.agent_stakes: Dict[str, int] = {}
        self.slashing_history: List[Dict[str, Any]] = []
        self.transaction_costs: Dict[str, int] = {
            "submit_transaction": 1,
            "propose_block": 10,
            "propose_governance": 50,
            "challenge_block": 100
        }
    
    async def charge_for_action(
        self,
        agent_id: str,
        action_type: str
    ) -> bool:
        """
        Charge agent for action.
        
        Raises:
            ConstraintViolation: If insufficient balance
        """
        cost = self.transaction_costs.get(action_type, 0)
        balance = self.agent_balances.get(agent_id, 0)
        
        if balance < cost:
            raise ConstraintViolation(
                ConstraintType.ECONOMIC,
                f"Insufficient balance: {balance} < {cost}"
            )
        
        self.agent_balances[agent_id] = balance - cost
        return True
    
    async def require_stake(
        self,
        agent_id: str,
        action_type: str,
        minimum_stake: int
    ) -> bool:
        """
        Require agent to have minimum stake.
        
        Raises:
            ConstraintViolation: If stake insufficient
        """
        current_stake = self.agent_stakes.get(agent_id, 0)
        
        if current_stake < minimum_stake:
            raise ConstraintViolation(
                ConstraintType.ECONOMIC,
                f"Insufficient stake: {current_stake} < {minimum_stake}"
            )
        
        return True
    
    async def apply_slashing(
        self,
        agent_id: str,
        slash_percentage: int,
        reason: str
    ) -> int:
        """
        Apply slashing penalty to agent.
        
        Returns:
            Amount slashed
        """
        stake = self.agent_stakes.get(agent_id, 0)
        amount_slashed = (stake * slash_percentage) // 100
        
        self.agent_stakes[agent_id] = stake - amount_slashed
        
        # Record slashing
        self.slashing_history.append({
            "timestamp": time.time(),
            "agent_id": agent_id,
            "slash_percentage": slash_percentage,
            "amount_slashed": amount_slashed,
            "reason": reason
        })
        
        print(f"⚠ Slashing applied: {agent_id} -{amount_slashed} ({slash_percentage}%)")
        return amount_slashed
    
    async def set_minimum_stake_for_validator(
        self,
        minimum_stake: int
    ) -> bool:
        """Set minimum stake requirement for validators."""
        self.transaction_costs["become_validator"] = minimum_stake
        return True


class ResourceConstraint:
    """
    Enforces resource constraints.
    
    Requirements:
    - Bounded compute
    - Bounded memory
    - Bounded bandwidth
    """
    
    def __init__(
        self,
        max_compute_ms: int = 5000,
        max_memory_mb: int = 1024,
        max_bandwidth_mbps: int = 100
    ):
        """Initialize resource constraint."""
        self.max_compute_ms = max_compute_ms
        self.max_memory_mb = max_memory_mb
        self.max_bandwidth_mbps = max_bandwidth_mbps
        
        # Per-agent tracking
        self.agent_compute_used: Dict[str, float] = {}
        self.agent_memory_used: Dict[str, float] = {}
        self.agent_bandwidth_used: Dict[str, float] = {}
    
    async def check_compute_budget(
        self,
        agent_id: str,
        operation_time_ms: float
    ) -> bool:
        """
        Check if operation fits in compute budget.
        
        Raises:
            ConstraintViolation: If exceeds budget
        """
        used = self.agent_compute_used.get(agent_id, 0)
        
        if used + operation_time_ms > self.max_compute_ms:
            raise ConstraintViolation(
                ConstraintType.RESOURCE,
                f"Compute budget exceeded: {used + operation_time_ms}ms > {self.max_compute_ms}ms"
            )
        
        self.agent_compute_used[agent_id] = used + operation_time_ms
        return True
    
    async def check_memory_budget(
        self,
        agent_id: str,
        memory_needed_mb: float
    ) -> bool:
        """Check if operation fits in memory budget."""
        used = self.agent_memory_used.get(agent_id, 0)
        
        if used + memory_needed_mb > self.max_memory_mb:
            raise ConstraintViolation(
                ConstraintType.RESOURCE,
                f"Memory budget exceeded: {used + memory_needed_mb}MB > {self.max_memory_mb}MB"
            )
        
        self.agent_memory_used[agent_id] = used + memory_needed_mb
        return True
    
    async def check_bandwidth_budget(
        self,
        agent_id: str,
        bandwidth_needed_mbps: float
    ) -> bool:
        """Check if operation fits in bandwidth budget."""
        used = self.agent_bandwidth_used.get(agent_id, 0)
        
        if used + bandwidth_needed_mbps > self.max_bandwidth_mbps:
            raise ConstraintViolation(
                ConstraintType.RESOURCE,
                f"Bandwidth budget exceeded: {used + bandwidth_needed_mbps}Mbps > {self.max_bandwidth_mbps}Mbps"
            )
        
        self.agent_bandwidth_used[agent_id] = used + bandwidth_needed_mbps
        return True
    
    async def reset_period(self):
        """Reset resource counters for new period."""
        self.agent_compute_used.clear()
        self.agent_memory_used.clear()
        self.agent_bandwidth_used.clear()


class SafetyConstraint:
    """
    Enforces safety constraints.
    
    Requirements:
    - No harmful actions
    - No infinite loops
    - No runaway proposals
    """
    
    def __init__(self):
        """Initialize safety constraint."""
        self.harmful_actions_blocklist: Set[str] = {
            "delete_all_blocks",
            "steal_tokens",
            "fork_consensus",
            "drain_validator_pool"
        }
        self.operation_depth_limit = 100
        self.proposal_rate_limit = 10  # per hour
        self.agent_proposal_history: Dict[str, List[float]] = {}
    
    async def check_action_safety(
        self,
        agent_id: str,
        action_type: str
    ) -> bool:
        """
        Check if action is safe to perform.
        
        Raises:
            ConstraintViolation: If action is harmful
        """
        if action_type in self.harmful_actions_blocklist:
            raise ConstraintViolation(
                ConstraintType.SAFETY,
                f"Harmful action blocked: {action_type}"
            )
        
        return True
    
    async def check_recursion_depth(
        self,
        current_depth: int
    ) -> bool:
        """
        Check recursion depth to prevent infinite loops.
        
        Raises:
            ConstraintViolation: If exceeds limit
        """
        if current_depth > self.operation_depth_limit:
            raise ConstraintViolation(
                ConstraintType.SAFETY,
                f"Recursion depth exceeded: {current_depth} > {self.operation_depth_limit}"
            )
        
        return True
    
    async def check_proposal_rate(self, agent_id: str) -> bool:
        """
        Check proposal submission rate.
        
        Raises:
            ConstraintViolation: If exceeds rate limit
        """
        now = time.time()
        hour_ago = now - 3600
        
        # Get recent proposals
        if agent_id not in self.agent_proposal_history:
            self.agent_proposal_history[agent_id] = []
        
        recent = [
            ts for ts in self.agent_proposal_history[agent_id]
            if ts > hour_ago
        ]
        
        if len(recent) >= self.proposal_rate_limit:
            raise ConstraintViolation(
                ConstraintType.SAFETY,
                f"Proposal rate limit exceeded: {len(recent)} in last hour"
            )
        
        # Record proposal
        recent.append(now)
        self.agent_proposal_history[agent_id] = recent
        
        return True


class TimeConstraint:
    """
    Enforces time-based constraints.
    
    Requirements:
    - Operate in epochs
    - Respect proposal windows
    - Respect challenge windows
    """
    
    def __init__(
        self,
        epoch_duration_seconds: int = 3600,
        proposal_window_seconds: int = 86400,
        challenge_window_seconds: int = 604800
    ):
        """Initialize time constraint."""
        self.epoch_duration = epoch_duration_seconds
        self.proposal_window = proposal_window_seconds
        self.challenge_window = challenge_window_seconds
        
        self.current_epoch = 0
        self.epoch_start_time = time.time()
        self.proposal_windows: List[Tuple[float, float]] = []
        self.challenge_windows: List[Tuple[float, float]] = []
    
    async def get_current_epoch(self) -> int:
        """Get current epoch number."""
        elapsed = time.time() - self.epoch_start_time
        return int(elapsed // self.epoch_duration)
    
    async def check_proposal_window(self) -> bool:
        """
        Check if current time is within proposal window.
        
        Raises:
            ConstraintViolation: If outside window
        """
        current_time = time.time()
        
        for window_start, window_end in self.proposal_windows:
            if window_start <= current_time <= window_end:
                return True
        
        raise ConstraintViolation(
            ConstraintType.TIME,
            "Current time is outside proposal window"
        )
    
    async def check_challenge_window(
        self,
        block_timestamp: float
    ) -> bool:
        """
        Check if block is within challenge window.
        
        Raises:
            ConstraintViolation: If outside window
        """
        current_time = time.time()
        time_since_block = current_time - block_timestamp
        
        if time_since_block > self.challenge_window:
            raise ConstraintViolation(
                ConstraintType.TIME,
                f"Block outside challenge window: {time_since_block}s > {self.challenge_window}s"
            )
        
        return True
    
    async def schedule_proposal_window(
        self,
        start_time: float,
        duration_seconds: int
    ) -> bool:
        """Schedule a new proposal window."""
        end_time = start_time + duration_seconds
        self.proposal_windows.append((start_time, end_time))
        return True
    
    async def advance_epoch(self) -> int:
        """Advance to next epoch."""
        self.current_epoch += 1
        return self.current_epoch


class ConstraintEnforcer:
    """
    Central constraint enforcement system.
    
    Coordinates all constraint types to ensure agent adherence.
    """
    
    def __init__(self):
        """Initialize constraint enforcer."""
        self.identity = IdentityConstraint()
        self.constitutional = ConstitutionalConstraint()
        self.deterministic = DeterministicConstraint()
        self.economic = EconomicConstraint()
        self.resource = ResourceConstraint()
        self.safety = SafetyConstraint()
        self.time = TimeConstraint()
        
        self.violation_log: List[ConstraintViolationRecord] = []
    
    async def check_all_constraints(
        self,
        agent_id: str,
        action_type: str,
        action_data: Dict[str, Any]
    ) -> Tuple[bool, Optional[str]]:
        """
        Check all constraints for an action.
        
        Returns:
            (is_allowed, error_message)
        """
        constraints = [
            ("constitutional", self._check_constitutional),
            ("safety", self._check_safety),
            ("time", self._check_time),
            ("resource", self._check_resource),
            ("economic", self._check_economic),
        ]
        
        for name, check_func in constraints:
            try:
                is_valid = await check_func(agent_id, action_type, action_data)
                if not is_valid:
                    return False, f"{name} constraint violated"
            except ConstraintViolation as e:
                return False, str(e)
            except Exception as e:
                return False, f"{name} check failed: {str(e)}"
        
        return True, None
    
    async def _check_constitutional(
        self,
        agent_id: str,
        action_type: str,
        action_data: Dict[str, Any]
    ) -> bool:
        """Check constitutional constraint."""
        is_compliant, _ = await self.constitutional.check_compliance(agent_id, action_data)
        return is_compliant
    
    async def _check_safety(
        self,
        agent_id: str,
        action_type: str,
        action_data: Dict[str, Any]
    ) -> bool:
        """Check safety constraint."""
        await self.safety.check_action_safety(agent_id, action_type)
        return True
    
    async def _check_time(
        self,
        agent_id: str,
        action_type: str,
        action_data: Dict[str, Any]
    ) -> bool:
        """Check time constraint."""
        # Placeholder - custom time checks per action type
        return True
    
    async def _check_resource(
        self,
        agent_id: str,
        action_type: str,
        action_data: Dict[str, Any]
    ) -> bool:
        """Check resource constraint."""
        # Placeholder - custom resource checks per action type
        return True
    
    async def _check_economic(
        self,
        agent_id: str,
        action_type: str,
        action_data: Dict[str, Any]
    ) -> bool:
        """Check economic constraint."""
        await self.economic.charge_for_action(agent_id, action_type)
        return True
