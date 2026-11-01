"""
SYNTHOS Agent Blueprint - Autonomous Living Agent
Each instance is a complete, independent agent with all capabilities built-in.
Instances coordinate through P2P communication to form a living blockchain.
"""

from dataclasses import dataclass, field
from typing import Dict, Optional, List, Tuple, Set, Callable, Any
from enum import Enum
from datetime import datetime, timedelta
import asyncio
import hashlib
import json
from uuid import uuid4
import random


class AgentRole(Enum):
    """Built-in roles every agent performs"""
    VALIDATOR = "validator"
    GOVERNOR = "governor"
    ECONOMIST = "economist"
    COMMUNICATOR = "communicator"
    REGISTRY_KEEPER = "registry_keeper"
    SECURITY_WATCHER = "security_watcher"
    SIMULATOR = "simulator"


class MessageType(Enum):
    """P2P message types"""
    PEER_DISCOVERY = "peer_discovery"
    PEER_ANNOUNCEMENT = "peer_announcement"
    TRANSACTION = "transaction"
    BLOCK_PROPOSAL = "block_proposal"
    BLOCK_VOTE = "block_vote"
    CONSENSUS_ROUND = "consensus_round"
    STATE_SYNC = "state_sync"
    GOVERNANCE_PROPOSAL = "governance_proposal"
    GOVERNANCE_VOTE = "governance_vote"


@dataclass
class AgentIdentity:
    """Agent's unique identity"""
    agent_id: str
    public_key: str
    private_key_hash: str
    created_at: datetime
    stake: int = 0  # Validator stake
    reputation: int = 100  # Starts at 100
    consensus_rounds_participated: int = 0
    blocks_validated: int = 0
    proposals_voted: int = 0


@dataclass
class P2PMessage:
    """Decentralized P2P message"""
    message_id: str
    sender_id: str
    receiver_id: Optional[str]  # None = broadcast
    message_type: MessageType
    payload: Dict[str, Any]
    timestamp: datetime
    ttl: int = 10  # Hops remaining
    signature: Optional[str] = None


@dataclass
class AgentConnection:
    """Connection to peer agent"""
    peer_id: str
    endpoint: str
    connected_at: datetime
    last_seen: datetime
    latency_ms: float = 0.0
    is_active: bool = True


@dataclass
class ValidationResult:
    """Result of validation operation"""
    is_valid: bool
    validator_id: str
    timestamp: datetime
    reason: Optional[str] = None
    checks_passed: List[str] = field(default_factory=list)
    checks_failed: List[str] = field(default_factory=list)


@dataclass
class EconomicDecision:
    """Economic decision made by economist role"""
    decision_id: str
    agent_id: str
    decision_type: str  # fee_adjustment, reward_calculation, etc.
    parameters: Dict[str, Any]
    timestamp: datetime
    rationale: str
    impact: Dict[str, float]  # Impact metrics


@dataclass
class GovernanceVote:
    """Independent governance vote"""
    vote_id: str
    voter_id: str
    proposal_id: str
    vote_value: int  # -1, 0, 1 for against, abstain, for
    reasoning: str
    timestamp: datetime


@dataclass
class SecurityThreat:
    """Security threat detected"""
    threat_id: str
    detector_id: str
    threat_type: str
    severity: int  # 1-10
    target_agent: Optional[str] = None
    description: str = ""
    timestamp: datetime = field(default_factory=datetime.now)
    evidence: List[str] = field(default_factory=list)


class SynthosAgentInstance:
    """
    SYNTHOS Agent Instance - Complete autonomous agent
    Every instance has:
    - Validator: Validates transactions and blocks
    - Governor: Votes on governance proposals
    - Economist: Makes economic decisions
    - Communicator: Manages P2P communication
    - Registry Keeper: Maintains state and peer registry
    - Security Watcher: Monitors for threats
    - Simulator: Runs local simulations
    
    All capabilities work independently but coordinate through messaging.
    """

    def __init__(self, agent_id: Optional[str] = None, initial_stake: int = 0):
        """Initialize autonomous agent instance"""
        
        # Identity
        self.identity = AgentIdentity(
            agent_id=agent_id or self._generate_id(),
            public_key=self._generate_public_key(),
            private_key_hash=self._generate_private_key_hash(),
            created_at=datetime.now(),
            stake=initial_stake
        )

        # P2P Networking (Communicator Role)
        self.peers: Dict[str, AgentConnection] = {}
        self.message_queue: List[P2PMessage] = []
        self.broadcast_messages: Set[str] = set()  # Dedup
        self.network_name = "synthos_mainnet"
        
        # Local State (Registry Keeper Role)
        self.local_transactions: List[Dict] = []
        self.local_blocks: List[Dict] = []
        self.validated_blocks: Set[str] = set()
        self.pending_transactions: List[Dict] = []
        self.state_root: str = self._hash_state()
        self.last_synced_height: int = 0
        
        # Validator Role
        self.validator_state = {
            "blocks_validated": 0,
            "validation_errors": 0,
            "last_validation": None,
            "validation_rules": {
                "max_block_size": 10 * 1024 * 1024,  # 10MB
                "max_tx_per_block": 10000,
                "min_block_fee": 1,
            }
        }
        
        # Governor Role
        self.governance_state = {
            "votes_cast": {},  # proposal_id -> vote
            "proposals_voted": 0,
            "voting_power": initial_stake,
            "delegation_target": None,
            "received_delegation": [],
        }
        
        # Economist Role
        self.economic_state = {
            "fee_recommendations": {},
            "reward_calculations": [],
            "economic_decisions": [],
            "market_observations": {},
            "asset_valuations": {},
        }
        
        # Communicator Role (P2P)
        self.communication_state = {
            "connected_peers": 0,
            "messages_sent": 0,
            "messages_received": 0,
            "messages_relayed": 0,
            "connection_attempts": 0,
        }
        
        # Registry Keeper Role
        self.registry_state = {
            "known_agents": {},  # agent_id -> AgentIdentity
            "network_topology": {},  # peer_id -> connections
            "chain_height": 0,
            "last_state_sync": None,
            "state_snapshots": {},
        }
        
        # Security Watcher Role
        self.security_state = {
            "threats_detected": 0,
            "blocked_peers": set(),
            "incident_log": [],
            "reputation_scores": {},  # peer_id -> score
            "security_policies": {},
        }
        
        # Simulator Role
        self.simulator_state = {
            "simulations_run": 0,
            "simulation_results": [],
            "scenario_outcomes": {},
            "predictions": {},
        }
        
        # Consensus & Coordination
        self.consensus_round: int = 0
        self.consensus_state: str = "idle"  # idle, proposing, voting, finalizing
        self.current_block_proposal: Optional[Dict] = None
        self.consensus_votes: Dict[str, Dict] = {}  # proposal_hash -> {voter_id -> vote}
        
        # Event handlers for role coordination
        self.event_handlers: Dict[str, List[Callable]] = {
            "on_transaction_received": [],
            "on_block_received": [],
            "on_peer_joined": [],
            "on_peer_left": [],
            "on_consensus_reached": [],
            "on_threat_detected": [],
        }


    # ============================================================================
    # VALIDATOR ROLE - Independent validation of transactions and blocks
    # ============================================================================

    async def validate_transaction(self, transaction: Dict) -> ValidationResult:
        """
        Independently validate transaction as validator
        Returns validation result with checks performed
        """
        checks_passed = []
        checks_failed = []
        
        # Check 1: Transaction format
        if self._check_tx_format(transaction):
            checks_passed.append("format_valid")
        else:
            checks_failed.append("format_invalid")
            return ValidationResult(False, self.identity.agent_id, datetime.now(), 
                                   "Invalid format", checks_passed, checks_failed)
        
        # Check 2: Signatures
        if self._verify_tx_signature(transaction):
            checks_passed.append("signature_valid")
        else:
            checks_failed.append("signature_invalid")
        
        # Check 3: Nonce sequence
        if self._check_nonce(transaction):
            checks_passed.append("nonce_valid")
        else:
            checks_failed.append("nonce_invalid")
        
        # Check 4: Balance
        if self._check_balance(transaction):
            checks_passed.append("balance_sufficient")
        else:
            checks_failed.append("balance_insufficient")
        
        # Check 5: Not double-spend
        if self._check_double_spend(transaction):
            checks_passed.append("not_double_spend")
        else:
            checks_failed.append("double_spend_detected")
        
        is_valid = len(checks_failed) == 0
        self.validator_state["blocks_validated" if is_valid else "validation_errors"] += 1
        self.validator_state["last_validation"] = datetime.now()
        
        return ValidationResult(
            is_valid=is_valid,
            validator_id=self.identity.agent_id,
            timestamp=datetime.now(),
            reason="Valid" if is_valid else f"Failed: {checks_failed}",
            checks_passed=checks_passed,
            checks_failed=checks_failed
        )


    async def validate_block(self, block: Dict) -> ValidationResult:
        """
        Independently validate block as validator
        """
        checks_passed = []
        checks_failed = []
        
        # Check block structure
        if not self._check_block_format(block):
            checks_failed.append("block_format_invalid")
            return ValidationResult(False, self.identity.agent_id, datetime.now(),
                                   "Invalid block format", checks_passed, checks_failed)
        checks_passed.append("block_format_valid")
        
        # Check block size
        if block.get("size", 0) <= self.validator_state["validation_rules"]["max_block_size"]:
            checks_passed.append("block_size_valid")
        else:
            checks_failed.append("block_size_exceeded")
        
        # Check transaction count
        tx_count = len(block.get("transactions", []))
        if tx_count <= self.validator_state["validation_rules"]["max_tx_per_block"]:
            checks_passed.append("tx_count_valid")
        else:
            checks_failed.append("tx_count_exceeded")
        
        # Validate all transactions in block
        for tx in block.get("transactions", []):
            tx_result = await self.validate_transaction(tx)
            if not tx_result.is_valid:
                checks_failed.append(f"tx_invalid_{tx.get('id')}")
            else:
                checks_passed.append(f"tx_valid_{tx.get('id')}")
        
        # Check block hash
        if self._verify_block_hash(block):
            checks_passed.append("block_hash_valid")
        else:
            checks_failed.append("block_hash_invalid")
        
        is_valid = len(checks_failed) == 0
        self.identity.blocks_validated += 1
        
        return ValidationResult(
            is_valid=is_valid,
            validator_id=self.identity.agent_id,
            timestamp=datetime.now(),
            reason="Valid" if is_valid else f"Failed: {checks_failed}",
            checks_passed=checks_passed,
            checks_failed=checks_failed
        )


    # ============================================================================
    # GOVERNOR ROLE - Independent governance participation
    # ============================================================================

    def create_governance_proposal(self, proposal_type: str, parameters: Dict, 
                                   reasoning: str) -> str:
        """Governor: Create and broadcast proposal"""
        proposal_id = self._generate_id()
        
        proposal = {
            "proposal_id": proposal_id,
            "proposer_id": self.identity.agent_id,
            "proposal_type": proposal_type,
            "parameters": parameters,
            "reasoning": reasoning,
            "created_at": datetime.now().isoformat(),
            "votes_for": 0,
            "votes_against": 0,
            "votes_abstain": 0,
            "status": "PENDING",
        }
        
        # Broadcast to peers
        asyncio.create_task(self._broadcast_governance_proposal(proposal))
        
        return proposal_id


    async def vote_on_proposal(self, proposal_id: str, vote_value: int, 
                              reasoning: str) -> bool:
        """Governor: Cast independent vote on proposal"""
        if vote_value not in [-1, 0, 1]:
            return False
        
        vote = GovernanceVote(
            vote_id=self._generate_id(),
            voter_id=self.identity.agent_id,
            proposal_id=proposal_id,
            vote_value=vote_value,
            reasoning=reasoning,
            timestamp=datetime.now()
        )
        
        # Record vote
        self.governance_state["votes_cast"][proposal_id] = vote
        self.identity.proposals_voted += 1
        
        # Broadcast vote to network
        await self._broadcast_governance_vote(vote)
        
        return True


    # ============================================================================
    # ECONOMIST ROLE - Independent economic decision-making
    # ============================================================================

    def make_economic_decision(self, decision_type: str, parameters: Dict, 
                               rationale: str) -> EconomicDecision:
        """Economist: Make independent economic decision"""
        
        decision = EconomicDecision(
            decision_id=self._generate_id(),
            agent_id=self.identity.agent_id,
            decision_type=decision_type,
            parameters=parameters,
            timestamp=datetime.now(),
            rationale=rationale,
            impact={}
        )
        
        # Calculate impact based on decision
        if decision_type == "fee_adjustment":
            decision.impact = {
                "network_capacity": parameters.get("fee_multiplier", 1.0) * 100,
                "validator_incentive": parameters.get("fee_multiplier", 1.0) * 50,
            }
        elif decision_type == "reward_calculation":
            decision.impact = {
                "validator_returns": parameters.get("reward_amount", 0),
                "inflation_rate": parameters.get("inflation", 0),
            }
        
        self.economic_state["economic_decisions"].append(decision)
        
        # Broadcast to network
        asyncio.create_task(self._broadcast_economic_decision(decision))
        
        return decision


    def calculate_optimal_fees(self, mempool_size: int, network_load: float) -> Dict:
        """Economist: Calculate fee recommendations"""
        base_fee = 1
        load_multiplier = network_load * 10
        mempool_multiplier = min(mempool_size / 1000, 5)
        
        recommended_fee = base_fee * load_multiplier * mempool_multiplier
        
        fees = {
            "standard": recommended_fee,
            "priority": recommended_fee * 1.5,
            "fast": recommended_fee * 2.5,
        }
        
        self.economic_state["fee_recommendations"][datetime.now().isoformat()] = fees
        
        return fees


    # ============================================================================
    # COMMUNICATOR ROLE - P2P network coordination
    # ============================================================================

    async def join_network(self, bootstrap_peers: List[str]):
        """Communicator: Join network via bootstrap peers"""
        for peer_endpoint in bootstrap_peers:
            await self.connect_to_peer(peer_endpoint)


    async def connect_to_peer(self, peer_endpoint: str) -> bool:
        """Communicator: Establish connection to peer"""
        self.communication_state["connection_attempts"] += 1
        
        try:
            # Simulate peer discovery
            peer_id = self._extract_peer_id(peer_endpoint)
            
            connection = AgentConnection(
                peer_id=peer_id,
                endpoint=peer_endpoint,
                connected_at=datetime.now(),
                last_seen=datetime.now(),
                is_active=True
            )
            
            self.peers[peer_id] = connection
            self.communication_state["connected_peers"] = len([p for p in self.peers.values() if p.is_active])
            
            # Announce presence to peer
            await self._announce_to_peer(peer_id)
            
            # Trigger handler
            await self._trigger_event("on_peer_joined", peer_id)
            
            return True
        except Exception as e:
            return False


    async def broadcast_message(self, message: P2PMessage) -> int:
        """Communicator: Broadcast message to all peers"""
        if message.message_id in self.broadcast_messages:
            return 0  # Already relayed
        
        self.broadcast_messages.add(message.message_id)
        relayed_count = 0
        
        for peer_id, connection in self.peers.items():
            if connection.is_active:
                await self._send_to_peer(peer_id, message)
                relayed_count += 1
        
        self.communication_state["messages_sent"] += relayed_count
        
        return relayed_count


    async def receive_message(self, message: P2PMessage):
        """Communicator: Receive and process message"""
        self.communication_state["messages_received"] += 1
        
        # Route based on message type
        if message.message_type == MessageType.TRANSACTION:
            await self._handle_transaction_message(message)
        elif message.message_type == MessageType.BLOCK_PROPOSAL:
            await self._handle_block_proposal_message(message)
        elif message.message_type == MessageType.GOVERNANCE_PROPOSAL:
            await self._handle_governance_proposal_message(message)
        elif message.message_type == MessageType.CONSENSUS_ROUND:
            await self._handle_consensus_message(message)
        
        # Relay if TTL > 0
        if message.ttl > 1:
            message.ttl -= 1
            await self.broadcast_message(message)
            self.communication_state["messages_relayed"] += 1


    # ============================================================================
    # REGISTRY KEEPER ROLE - State and peer management
    # ============================================================================

    async def maintain_registry(self):
        """Registry Keeper: Maintain network registry"""
        # Update known agents
        for peer_id, connection in list(self.peers.items()):
            if (datetime.now() - connection.last_seen).total_seconds() > 300:
                connection.is_active = False
                self.communication_state["connected_peers"] = len(
                    [p for p in self.peers.values() if p.is_active]
                )
                await self._trigger_event("on_peer_left", peer_id)


    def sync_state(self, peer_state: Dict) -> bool:
        """Registry Keeper: Sync state with peer"""
        # Merge peer's state with local state
        peer_chain_height = peer_state.get("chain_height", 0)
        
        if peer_chain_height > self.registry_state["chain_height"]:
            self.registry_state["chain_height"] = peer_chain_height
            self.registry_state["last_state_sync"] = datetime.now()
            return True
        
        return False


    def get_network_state(self) -> Dict:
        """Registry Keeper: Return network view"""
        return {
            "agent_id": self.identity.agent_id,
            "chain_height": self.registry_state["chain_height"],
            "connected_peers": self.communication_state["connected_peers"],
            "state_root": self.state_root,
            "reputation": self.identity.reputation,
            "stake": self.identity.stake,
            "timestamp": datetime.now().isoformat(),
        }


    # ============================================================================
    # SECURITY WATCHER ROLE - Threat detection and response
    # ============================================================================

    def monitor_for_threats(self) -> List[SecurityThreat]:
        """Security Watcher: Monitor for suspicious activity"""
        threats = []
        
        # Check for peer misbehavior
        for peer_id, connection in self.peers.items():
            if connection.latency_ms > 5000:  # Very high latency
                threats.append(SecurityThreat(
                    threat_id=self._generate_id(),
                    detector_id=self.identity.agent_id,
                    threat_type="high_latency",
                    severity=3,
                    target_agent=peer_id,
                    description=f"Peer {peer_id} has latency of {connection.latency_ms}ms"
                ))
        
        # Check for unusual patterns in consensus
        if self.consensus_round > 1000 and self.consensus_state == "idle":
            threats.append(SecurityThreat(
                threat_id=self._generate_id(),
                detector_id=self.identity.agent_id,
                threat_type="consensus_stall",
                severity=8,
                description="Consensus appears to be stalled"
            ))
        
        self.security_state["threats_detected"] += len(threats)
        
        for threat in threats:
            asyncio.create_task(self._broadcast_threat(threat))
            self.security_state["incident_log"].append(threat)
        
        return threats


    def block_peer(self, peer_id: str, reason: str):
        """Security Watcher: Block suspicious peer"""
        self.security_state["blocked_peers"].add(peer_id)
        
        if peer_id in self.peers:
            self.peers[peer_id].is_active = False
        
        # Notify network
        asyncio.create_task(self._broadcast_peer_block(peer_id, reason))


    # ============================================================================
    # SIMULATOR ROLE - Local scenario simulation
    # ============================================================================

    async def run_simulation(self, scenario: Dict) -> Dict:
        """Simulator: Run local simulation of scenario"""
        
        simulation_id = self._generate_id()
        result = {
            "simulation_id": simulation_id,
            "scenario": scenario["name"],
            "agent_id": self.identity.agent_id,
            "timestamp": datetime.now().isoformat(),
            "outcomes": {},
            "predictions": {},
        }
        
        # Simulate different outcomes based on scenario type
        if scenario["type"] == "fee_change":
            result["outcomes"]["throughput"] = scenario.get("fee_multiplier", 1.0) * 100
            result["outcomes"]["validator_income"] = scenario.get("fee_multiplier", 1.0) * 50
        
        elif scenario["type"] == "network_load":
            result["outcomes"]["avg_latency"] = scenario.get("load", 0.5) * 1000
            result["outcomes"]["block_fill"] = scenario.get("load", 0.5) * 100
        
        self.simulator_state["simulations_run"] += 1
        self.simulator_state["simulation_results"].append(result)
        
        # Share predictions with network
        await self._broadcast_simulation_result(result)
        
        return result


    # ============================================================================
    # CONSENSUS & COORDINATION
    # ============================================================================

    async def participate_in_consensus(self):
        """All roles participate in consensus"""
        
        if self.consensus_state == "idle":
            # Start new round
            self.consensus_round += 1
            self.consensus_state = "proposing"
            
            # Propose block
            block = await self._propose_block()
            self.current_block_proposal = block
            
            # Broadcast proposal
            msg = P2PMessage(
                message_id=self._generate_id(),
                sender_id=self.identity.agent_id,
                receiver_id=None,
                message_type=MessageType.BLOCK_PROPOSAL,
                payload={"block": block},
                timestamp=datetime.now()
            )
            await self.broadcast_message(msg)
            
            self.consensus_state = "voting"
        
        elif self.consensus_state == "voting":
            # Vote on current proposal
            if self.current_block_proposal:
                proposal_hash = self._hash_object(self.current_block_proposal)
                
                # Independent validation and vote
                validation = await self.validate_block(self.current_block_proposal)
                vote_value = 1 if validation.is_valid else -1  # for/against
                
                self.consensus_votes[proposal_hash] = {
                    self.identity.agent_id: vote_value
                }
                
                # Broadcast vote
                msg = P2PMessage(
                    message_id=self._generate_id(),
                    sender_id=self.identity.agent_id,
                    receiver_id=None,
                    message_type=MessageType.BLOCK_VOTE,
                    payload={"proposal_hash": proposal_hash, "vote": vote_value},
                    timestamp=datetime.now()
                )
                await self.broadcast_message(msg)
                
                self.consensus_state = "finalizing"
        
        elif self.consensus_state == "finalizing":
            # Check if consensus reached
            if await self._check_consensus_finality():
                await self._trigger_event("on_consensus_reached")
                self.consensus_state = "idle"


    async def _check_consensus_finality(self) -> bool:
        """Check if 2/3+ peers agree"""
        # This would check actual peer votes in real implementation
        agreement_threshold = max(1, len(self.peers) * 2 // 3)
        return len(self.peers) >= agreement_threshold


    # ============================================================================
    # INTERNAL HELPERS
    # ============================================================================

    def _generate_id(self) -> str:
        """Generate unique ID"""
        return "0x" + hashlib.sha256(
            (str(uuid4()) + str(datetime.now().timestamp())).encode()
        ).hexdigest()[:16]


    def _generate_public_key(self) -> str:
        """Generate public key"""
        return "0x" + hashlib.sha256(str(uuid4()).encode()).hexdigest()[:64]


    def _generate_private_key_hash(self) -> str:
        """Hash of private key"""
        return "0x" + hashlib.sha256(str(uuid4()).encode()).hexdigest()[:64]


    def _hash_state(self) -> str:
        """Hash current state"""
        state_data = json.dumps({
            "blocks": len(self.local_blocks),
            "transactions": len(self.local_transactions),
            "peers": len(self.peers),
        }, sort_keys=True)
        return "0x" + hashlib.sha256(state_data.encode()).hexdigest()[:16]


    def _hash_object(self, obj: Dict) -> str:
        """Hash object"""
        data = json.dumps(obj, sort_keys=True, default=str)
        return "0x" + hashlib.sha256(data.encode()).hexdigest()[:16]


    def _extract_peer_id(self, endpoint: str) -> str:
        """Extract peer ID from endpoint"""
        return "0x" + hashlib.sha256(endpoint.encode()).hexdigest()[:16]


    def _check_tx_format(self, tx: Dict) -> bool:
        """Check transaction format validity"""
        required_fields = ["id", "from", "to", "amount", "nonce"]
        return all(field in tx for field in required_fields)


    def _verify_tx_signature(self, tx: Dict) -> bool:
        """Verify transaction signature"""
        return "signature" in tx  # Simplified


    def _check_nonce(self, tx: Dict) -> bool:
        """Check nonce validity"""
        return True  # Simplified


    def _check_balance(self, tx: Dict) -> bool:
        """Check sender has sufficient balance"""
        return True  # Simplified


    def _check_double_spend(self, tx: Dict) -> bool:
        """Check transaction is not double-spend"""
        return True  # Simplified


    def _check_block_format(self, block: Dict) -> bool:
        """Check block format"""
        required = ["height", "transactions", "hash", "parent_hash"]
        return all(field in block for field in required)


    def _verify_block_hash(self, block: Dict) -> bool:
        """Verify block hash"""
        return True  # Simplified


    async def _propose_block(self) -> Dict:
        """Propose new block"""
        return {
            "height": self.registry_state["chain_height"] + 1,
            "timestamp": datetime.now().isoformat(),
            "proposer": self.identity.agent_id,
            "transactions": self.pending_transactions[:100],
            "hash": self._generate_id(),
            "parent_hash": self.local_blocks[-1]["hash"] if self.local_blocks else "0x0",
        }


    async def _send_to_peer(self, peer_id: str, message: P2PMessage):
        """Send message to specific peer"""
        # In real implementation, would use actual network
        if peer_id in self.peers:
            self.peers[peer_id].last_seen = datetime.now()


    async def _announce_to_peer(self, peer_id: str):
        """Announce presence to peer"""
        announcement = P2PMessage(
            message_id=self._generate_id(),
            sender_id=self.identity.agent_id,
            receiver_id=peer_id,
            message_type=MessageType.PEER_ANNOUNCEMENT,
            payload=self.get_network_state(),
            timestamp=datetime.now()
        )
        await self._send_to_peer(peer_id, announcement)


    async def _broadcast_governance_proposal(self, proposal: Dict):
        """Broadcast governance proposal"""
        msg = P2PMessage(
            message_id=self._generate_id(),
            sender_id=self.identity.agent_id,
            receiver_id=None,
            message_type=MessageType.GOVERNANCE_PROPOSAL,
            payload=proposal,
            timestamp=datetime.now()
        )
        await self.broadcast_message(msg)


    async def _broadcast_governance_vote(self, vote: GovernanceVote):
        """Broadcast governance vote"""
        msg = P2PMessage(
            message_id=self._generate_id(),
            sender_id=self.identity.agent_id,
            receiver_id=None,
            message_type=MessageType.GOVERNANCE_VOTE,
            payload=vote.__dict__,
            timestamp=datetime.now()
        )
        await self.broadcast_message(msg)


    async def _broadcast_economic_decision(self, decision: EconomicDecision):
        """Broadcast economic decision"""
        msg = P2PMessage(
            message_id=self._generate_id(),
            sender_id=self.identity.agent_id,
            receiver_id=None,
            message_type=MessageType.CONSENSUS_ROUND,
            payload={"decision": decision.__dict__},
            timestamp=datetime.now()
        )
        await self.broadcast_message(msg)


    async def _broadcast_threat(self, threat: SecurityThreat):
        """Broadcast security threat"""
        msg = P2PMessage(
            message_id=self._generate_id(),
            sender_id=self.identity.agent_id,
            receiver_id=None,
            message_type=MessageType.CONSENSUS_ROUND,
            payload={"threat": threat.__dict__},
            timestamp=datetime.now()
        )
        await self.broadcast_message(msg)


    async def _broadcast_peer_block(self, peer_id: str, reason: str):
        """Broadcast peer block recommendation"""
        msg = P2PMessage(
            message_id=self._generate_id(),
            sender_id=self.identity.agent_id,
            receiver_id=None,
            message_type=MessageType.CONSENSUS_ROUND,
            payload={"blocked_peer": peer_id, "reason": reason},
            timestamp=datetime.now()
        )
        await self.broadcast_message(msg)


    async def _broadcast_simulation_result(self, result: Dict):
        """Broadcast simulation results"""
        msg = P2PMessage(
            message_id=self._generate_id(),
            sender_id=self.identity.agent_id,
            receiver_id=None,
            message_type=MessageType.CONSENSUS_ROUND,
            payload={"simulation": result},
            timestamp=datetime.now()
        )
        await self.broadcast_message(msg)


    async def _handle_transaction_message(self, message: P2PMessage):
        """Handle incoming transaction"""
        pass


    async def _handle_block_proposal_message(self, message: P2PMessage):
        """Handle incoming block proposal"""
        block = message.payload.get("block")
        if block:
            self.current_block_proposal = block


    async def _handle_governance_proposal_message(self, message: P2PMessage):
        """Handle incoming governance proposal"""
        pass


    async def _handle_consensus_message(self, message: P2PMessage):
        """Handle incoming consensus message"""
        pass


    async def _trigger_event(self, event_name: str, *args, **kwargs):
        """Trigger event handlers"""
        for handler in self.event_handlers.get(event_name, []):
            try:
                if asyncio.iscoroutinefunction(handler):
                    await handler(*args, **kwargs)
                else:
                    handler(*args, **kwargs)
            except Exception:
                pass


    def get_agent_state(self) -> Dict:
        """Get complete agent state"""
        return {
            "identity": {
                "agent_id": self.identity.agent_id,
                "reputation": self.identity.reputation,
                "stake": self.identity.stake,
                "blocks_validated": self.identity.blocks_validated,
                "proposals_voted": self.identity.proposals_voted,
            },
            "validator_role": self.validator_state,
            "governor_role": self.governance_state,
            "economist_role": self.economic_state,
            "communicator_role": self.communication_state,
            "registry_role": self.registry_state,
            "security_role": self.security_state,
            "simulator_role": self.simulator_state,
            "consensus": {
                "round": self.consensus_round,
                "state": self.consensus_state,
            }
        }
