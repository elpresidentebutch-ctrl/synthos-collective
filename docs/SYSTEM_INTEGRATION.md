# SYNTHOS Agent System Integration Guide

## Complete System Integration Patterns

This guide provides detailed patterns for integrating all SYNTHOS Agent systems into cohesive workflows.

## 1. Agent Initialization Pipeline

### Full Initialization Sequence

```python
class AgentBootstrap:
    async def full_initialization(self):
        """Complete agent initialization with all subsystems."""
        
        # Phase 1: Core Infrastructure Setup
        self.agent = await self._initialize_core_infrastructure()
        
        # Phase 2: State Management Setup
        self.state = await self._initialize_state_management()
        
        # Phase 3: Role Registration
        self.roles = await self._initialize_all_roles()
        
        # Phase 4: Event System Setup
        self.event_bus = await self._setup_event_routing()
        
        # Phase 5: Network Components
        self.network = await self._initialize_network_layer()
        
        # Phase 6: Storage Setup
        self.storage = await self._initialize_storage()
        
        # Phase 7: Cross-Layer Integration
        await self._establish_cross_layer_connections()
        
        # Phase 8: Validation
        await self._validate_system_integrity()
        
        return self.agent

    async def _initialize_core_infrastructure(self):
        """Initialize Agent, State, EventBus."""
        from src.core.agent import SyntHOSAgent
        
        agent = SyntHOSAgent(
            agent_id="synthos-node-1",
            version="1.0.0",
            network_id="mainnet"
        )
        
        await agent.initialize()
        return agent

    async def _initialize_state_management(self):
        """Initialize AgentState with all state types."""
        from src.core.state import AgentState
        
        state = AgentState({
            "ledger": {},      # Account balances
            "consensus": {},   # Voting state
            "reputation": {},  # Peer scores
            "resources": {}    # CPU/bandwidth allocation
        })
        
        # Initialize with genesis state
        await state.set("genesis_initialized", True)
        return state

    async def _initialize_all_roles(self):
        """Initialize all 7 roles."""
        roles = {}
        
        from src.roles.validator import ValidatorRole
        from src.roles.economist import EconomistRole
        from src.roles.governor import GovernorRole
        from src.roles.communicator import CommunicatorRole
        from src.roles.simulator import SimulatorRole
        from src.roles.enforcer import EnforcerRole
        from src.roles.citizen import CitizenRole
        
        role_classes = [
            ValidatorRole,
            EconomistRole,
            GovernorRole,
            CommunicatorRole,
            SimulatorRole,
            EnforcerRole,
            CitizenRole
        ]
        
        for role_class in role_classes:
            role = role_class(agent=self.agent)
            await role.initialize()
            roles[role_class.__name__] = role
            self.agent.register_role(role)
        
        return roles

    async def _setup_event_routing(self):
        """Configure event subscriptions across all roles."""
        event_routes = {
            "TRANSACTION_SUBMITTED": [
                ("ValidatorRole", self.roles["ValidatorRole"].validate_transaction_event),
                ("EconomistRole", self.roles["EconomistRole"].process_transaction),
                ("CommunicatorRole", self.roles["CommunicatorRole"].relay_transaction)
            ],
            "BLOCK_PROPOSED": [
                ("ValidatorRole", self.roles["ValidatorRole"].validate_block_event),
                ("SimulatorRole", self.roles["SimulatorRole"].model_block_impact),
                ("EnforcerRole", self.roles["EnforcerRole"].check_block_compliance)
            ],
            "CONSENSUS_ROUND_STARTED": [
                ("Governor", self.roles["GovernorRole"].prepare_voting),
                ("Enforcer", self.roles["EnforcerRole"].prepare_monitoring)
            ],
            "PROPOSAL_SUBMITTED": [
                ("SimulatorRole", self.roles["SimulatorRole"].simulate_proposal),
                ("CommunicatorRole", self.roles["CommunicatorRole"].broadcast_proposal),
                ("GovernorRole", self.roles["GovernorRole"].track_proposal)
            ]
        }
        
        for event_type, handlers in event_routes.items():
            for role_name, handler in handlers:
                self.agent.event_bus.subscribe(event_type, handler)
        
        return self.agent.event_bus

    async def _initialize_network_layer(self):
        """Initialize networking components."""
        from src.network.constitution import Constitution
        from src.network.p2p_messaging import P2PMessenger, GossipProtocol, PeerNegotiator
        
        network = {
            "constitution": Constitution(),
            "p2p_messenger": P2PMessenger(),
            "gossip": GossipProtocol(),
            "negotiator": PeerNegotiator()
        }
        
        # Initialize networking
        network["constitution"].initialize_default_constitution()
        
        # Register message handlers
        network["p2p_messenger"].register_message_handler(
            "BLOCK", self.roles["ValidatorRole"].validate_block_event
        )
        network["p2p_messenger"].register_message_handler(
            "TRANSACTION", self.roles["ValidatorRole"].validate_transaction_event
        )
        network["p2p_messenger"].register_message_handler(
            "PROPOSAL", self.roles["GovernorRole"].process_proposal
        )
        
        return network

    async def _initialize_storage(self):
        """Initialize persistent storage."""
        from src.storage.state_store import LocalStateStore
        
        storage = LocalStateStore()
        await storage.initialize()
        return storage

    async def _establish_cross_layer_connections(self):
        """Connect all layers together."""
        # Validator → Storage
        self.roles["ValidatorRole"].state_store = self.storage
        
        # Economist → State
        self.roles["EconomistRole"].agent_state = self.agent.state
        
        # Governor → Storage
        self.roles["GovernorRole"].proposal_store = self.storage
        
        # Communicator → Network
        self.roles["CommunicatorRole"].network = self.network["p2p_messenger"]
        
        # All Roles → Event Bus
        for role in self.roles.values():
            role.event_bus = self.agent.event_bus
        
        # ConsensusEngine → Storage
        if hasattr(self, 'consensus_engine'):
            self.consensus_engine.state_store = self.storage

    async def _validate_system_integrity(self):
        """Verify all components are properly connected."""
        checks = [
            ("Core initialized", self.agent is not None),
            ("State initialized", self.agent.state is not None),
            ("All roles registered", len(self.roles) == 7),
            ("Event bus exists", self.agent.event_bus is not None),
            ("Network initialized", self.network is not None),
            ("Storage initialized", self.storage is not None),
        ]
        
        for check_name, result in checks:
            if not result:
                raise RuntimeError(f"Initialization check failed: {check_name}")
        
        print("✓ All system integrity checks passed")
```

## 2. Complete Transaction Flow Integration

### End-to-End Transaction Processing

```python
class TransactionProcessor:
    """Demonstrates complete transaction processing with all systems."""
    
    async def process_transaction_complete_flow(self, tx):
        """Process transaction through complete system."""
        
        # STEP 1: Submission
        print(f"[1] TRANSACTION SUBMISSION: {tx.id}")
        await self.event_bus.publish({
            "type": "TRANSACTION_SUBMITTED",
            "data": tx
        })
        
        # STEP 2: Economic Pre-Check
        print(f"[2] ECONOMIC PRE-CHECK")
        fee = await self.economist_role.calculate_fee(tx)
        if not await self._check_balance(tx.sender, tx.amount + fee):
            await self.event_bus.publish({
                "type": "TRANSACTION_REJECTED",
                "reason": "INSUFFICIENT_BALANCE"
            })
            return False
        
        # STEP 3: Full Validation
        print(f"[3] FULL VALIDATION")
        from src.roles.transaction_validator import TransactionValidator
        validator = TransactionValidator()
        result = await validator.validate_full_transaction(tx)
        
        if not result.is_valid:
            await self.event_bus.publish({
                "type": "TRANSACTION_REJECTED",
                "reason": result.validation_errors[0] if result.validation_errors else "UNKNOWN"
            })
            return False
        
        # STEP 4: Constitution Compliance
        print(f"[4] CONSTITUTION COMPLIANCE")
        if not await self.constitution.check_compliance("transaction_validation", tx):
            await self.event_bus.publish({
                "type": "CONSTITUTION_VIOLATION",
                "transaction_id": tx.id
            })
            return False
        
        # STEP 5: Double-Spend Check with Mempool
        print(f"[5] DOUBLE-SPEND CHECK")
        mempool = await self.state_store.get_mempool()
        if await self._is_double_spend(tx, mempool):
            await self.event_bus.publish({
                "type": "TRANSACTION_REJECTED",
                "reason": "DOUBLE_SPEND"
            })
            return False
        
        # STEP 6: Add to Mempool
        print(f"[6] MEMPOOL ADD")
        await self.state_store.add_to_mempool(tx)
        
        # STEP 7: Network Propagation
        print(f"[7] GOSSIP PROPAGATION")
        await self.gossip_protocol.publish_gossip({
            "type": "TRANSACTION",
            "data": tx
        })
        
        # STEP 8: Publish Success Event
        print(f"[8] SUCCESS EVENT")
        await self.event_bus.publish({
            "type": "TRANSACTION_VALIDATED",
            "transaction": tx,
            "fee": fee
        })
        
        return True

    async def _check_balance(self, account, amount):
        """Check account balance."""
        balance = await self.agent_state.get_balance(account)
        return balance >= amount

    async def _is_double_spend(self, tx, mempool):
        """Check if transaction is double-spend."""
        # Check same nonce exists in mempool for same sender
        for mem_tx in mempool:
            if mem_tx.sender == tx.sender and mem_tx.nonce == tx.nonce:
                return True
        return False
```

## 3. Complete Consensus Integration

### End-to-End Consensus Flow

```python
class ConsensusIntegration:
    """Demonstrates complete consensus process."""
    
    async def complete_consensus_cycle(self, block_proposal):
        """Process block through complete consensus."""
        
        # STEP 1: Block Validation
        print(f"[1] BLOCK VALIDATION")
        block_validator_role = self.roles["ValidatorRole"]
        is_valid = await block_validator_role.validate_block(block_proposal)
        
        if not is_valid:
            print("Block rejected in validation")
            return False
        
        # STEP 2: Compliance Check
        print(f"[2] COMPLIANCE CHECK")
        if not await self.constitution.check_compliance("consensus", block_proposal):
            print("Block fails constitution compliance")
            return False
        
        # STEP 3: Start Consensus Round
        print(f"[3] CONSENSUS ROUND START")
        from src.consensus.consensus import ConsensusEngine
        consensus = ConsensusEngine()
        
        round_id = await consensus.start_consensus_round(
            height=block_proposal.height,
            block_hash=block_proposal.hash
        )
        
        # STEP 4: Voting Phase
        print(f"[4] VOTING PHASE")
        votes_collected = 0
        max_votes = len(self.validators)
        min_required = (max_votes * 2 // 3) + 1
        
        async for vote in self._collect_votes(block_proposal):
            # Record vote in consensus engine
            await consensus.vote(
                voter=vote.voter,
                height=block_proposal.height,
                block_hash=block_proposal.hash,
                vote_value=vote.value,
                stake=vote.stake,
                signature=vote.signature
            )
            votes_collected += 1
            print(f"  Vote {votes_collected}/{max_votes}")
            
            if votes_collected >= min_required:
                print("  Supermajority reached early")
                break
        
        # STEP 5: Finality Check
        print(f"[5] FINALITY CHECK")
        is_final = await consensus.finalize_consensus(
            height=block_proposal.height,
            required_supermajority=min_required
        )
        
        if not is_final:
            print("Block failed to reach consensus")
            return False
        
        # STEP 6: Apply Economic Incentives
        print(f"[6] ECONOMIC INCENTIVES")
        economist = self.roles["EconomistRole"]
        block_reward = await economist.calculate_block_reward(
            block_proposal,
            transaction_count=len(block_proposal.transactions)
        )
        await economist.distribute_reward(
            address=block_proposal.proposer,
            amount=block_reward
        )
        
        # STEP 7: Update State
        print(f"[7] STATE UPDATE")
        new_state_root = await self._compute_state_root(block_proposal)
        await self.agent_state.set("current_state_root", new_state_root)
        
        # STEP 8: Store Block
        print(f"[8] BLOCK STORAGE")
        await self.state_store.store_block(
            height=block_proposal.height,
            block=block_proposal,
            state_root=new_state_root
        )
        
        # STEP 9: Update Peer Reputations
        print(f"[9] PEER REPUTATIONS")
        for vote in votes_collected:
            await self.state_store.update_peer_reputation(
                peer_id=vote.voter,
                success=True,
                latency=vote.latency
            )
        
        # STEP 10: Broadcast Finality
        print(f"[10] BROADCAST FINALITY")
        await self.gossip_protocol.publish_gossip({
            "type": "BLOCK_FINALIZED",
            "block": block_proposal,
            "state_root": new_state_root
        })
        
        # STEP 11: Publish Success Event
        print(f"[11] SUCCESS EVENT")
        await self.event_bus.publish({
            "type": "BLOCK_FINALIZED",
            "block": block_proposal,
            "height": block_proposal.height
        })
        
        print(f"✓ Block finalized at height {block_proposal.height}")
        return True

    async def _collect_votes(self, block_proposal):
        """Generate votes from validators (placeholder)."""
        for validator in self.validators:
            yield {
                "voter": validator.id,
                "value": True,  # Validator approves
                "stake": validator.stake,
                "signature": f"sig_{validator.id}",
                "latency": 50  # ms
            }

    async def _compute_state_root(self, block_proposal):
        """Compute Merkle root of state after block application."""
        # Placeholder - would compute actual Merkle tree
        return f"state_root_{block_proposal.height}"
```

## 4. Governance Integration

### Complete Governance Cycle

```python
class GovernanceIntegration:
    """Demonstrates complete governance process."""
    
    async def governance_proposal_lifecycle(self, proposal):
        """Process proposal through complete governance cycle."""
        
        # STEP 1: Proposal Submission
        print(f"[1] PROPOSAL SUBMISSION: {proposal.id}")
        governor = self.roles["GovernorRole"]
        proposal_id = await governor.propose_change(
            change_type=proposal.type,
            parameters=proposal.parameters
        )
        
        await self.event_bus.publish({
            "type": "PROPOSAL_SUBMITTED",
            "proposal": proposal
        })
        
        # STEP 2: Store Proposal
        print(f"[2] PROPOSAL STORAGE")
        await self.state_store.store_proposal(proposal_id, proposal)
        
        # STEP 3: Simulation
        print(f"[3] PROTOCOL SIMULATION")
        simulator = self.roles["SimulatorRole"]
        simulation_results = await simulator.simulate_protocol_change(
            change=proposal.parameters,
            duration_blocks=1000
        )
        
        print(f"  Predicted Impact: {simulation_results['impact']}")
        
        # STEP 4: Broadcast to Network
        print(f"[4] GOSSIP BROADCAST")
        await self.gossip_protocol.publish_gossip({
            "type": "PROPOSAL",
            "proposal": proposal,
            "simulation": simulation_results
        })
        
        # STEP 5: Voting Phase
        print(f"[5] VOTING PHASE")
        votes_for = 0
        votes_against = 0
        voting_deadline = proposal.vote_deadline
        
        async for vote in self._collect_governance_votes(proposal_id, voting_deadline):
            if vote.value:
                votes_for += await self._get_voting_weight(vote.voter)
            else:
                votes_against += await self._get_voting_weight(vote.voter)
            
            await governor.vote(proposal_id, vote.voter, vote.value, vote.stake)
            print(f"  Vote recorded: {vote.voter} → {vote.value}")
        
        # STEP 6: Consensus on Governance
        print(f"[6] GOVERNANCE CONSENSUS")
        total_weight = votes_for + votes_against
        if total_weight == 0:
            print("No votes received")
            return False
        
        percentage_for = (votes_for / total_weight) * 100
        min_required = 66.67  # 2/3 supermajority
        
        print(f"  For: {percentage_for:.1f}% (required: {min_required:.1f}%)")
        
        if percentage_for < min_required:
            await self.event_bus.publish({
                "type": "PROPOSAL_REJECTED",
                "proposal_id": proposal_id
            })
            return False
        
        # STEP 7: Finalize Vote
        print(f"[7] FINALIZE VOTE")
        is_passed = await governor.finalize_vote(proposal_id)
        
        # STEP 8: Apply Changes if Passed
        if is_passed:
            print(f"[8] APPLYING CHANGES")
            
            # Update constitution if applicable
            if proposal.type == "CONSTITUTION_AMENDMENT":
                await self.constitution.add_rule(proposal.parameters["rule"])
            
            # Update consensus parameters if applicable
            if proposal.type == "CONSENSUS_PARAMETER":
                await self._apply_consensus_parameter_change(proposal.parameters)
            
            # Update economic parameters if applicable
            if proposal.type == "ECONOMIC_PARAMETER":
                await self._apply_economic_parameter_change(proposal.parameters)
            
            await self.event_bus.publish({
                "type": "PROPOSAL_EXECUTED",
                "proposal_id": proposal_id
            })
        
        # STEP 9: Record Decision
        print(f"[9] RECORD DECISION")
        await self.state_store.record_governance_decision({
            "proposal_id": proposal_id,
            "passed": is_passed,
            "votes_for": votes_for,
            "votes_against": votes_against,
            "timestamp": time.time()
        })
        
        # STEP 10: Broadcast Result
        print(f"[10] BROADCAST RESULT")
        await self.gossip_protocol.publish_gossip({
            "type": "GOVERNANCE_RESULT",
            "proposal_id": proposal_id,
            "passed": is_passed
        })
        
        return is_passed

    async def _collect_governance_votes(self, proposal_id, deadline):
        """Collect votes until deadline."""
        # Placeholder - would wait for actual votes
        yield {
            "voter": "citizen_1",
            "value": True,
            "stake": 1000
        }

    async def _get_voting_weight(self, voter):
        """Get voting weight (stake, delegation, etc)."""
        # Placeholder
        return 1000
```

## 5. Cross-Layer Monitoring

### Holistic System Monitoring

```python
class SystemMonitoring:
    """Comprehensive monitoring across all layers."""
    
    async def generate_system_health_report(self):
        """Generate complete system health report."""
        
        report = {
            "timestamp": time.time(),
            "agent_status": {},
            "role_status": {},
            "network_status": {},
            "consensus_status": {},
            "economic_status": {},
            "storage_status": {}
        }
        
        # Agent Level
        report["agent_status"] = {
            "uptime": self.agent.uptime,
            "memory_usage": self._get_memory_usage(),
            "cpu_usage": self._get_cpu_usage(),
            "state_version": self.agent.state.version
        }
        
        # Role Status
        for role_name, role in self.roles.items():
            report["role_status"][role_name] = {
                "status": role.status.value,
                "executed_actions": role.action_count,
                "errors": len(role.error_log),
                "last_execution": role.last_execution_time
            }
        
        # Network Status
        report["network_status"] = {
            "peer_count": len(self.p2p_messenger.peers),
            "connected_peers": sum(1 for p in self.p2p_messenger.peers if p.is_connected),
            "gossip_stats": await self.gossip_protocol.get_gossip_stats(),
            "reputation_average": self._compute_average_reputation()
        }
        
        # Consensus Status
        report["consensus_status"] = {
            "current_height": self.agent.state.get("current_height", 0),
            "last_finalized": self.agent.state.get("last_finalized_height", 0),
            "pending_proposals": len(self.governor_role.pending_proposals),
            "consensus_rounds": await self.state_store.count_consensus_rounds()
        }
        
        # Economic Status
        report["economic_status"] = {
            "total_supply": self.economist_role.total_supply,
            "inflation_rate": self.economist_role.inflation_rate,
            "average_fee": self.economist_role.average_fee,
            "rewarded_validators": self.economist_role.rewarded_validators_count
        }
        
        # Storage Status
        report["storage_status"] = {
            "mempool_size": len(await self.state_store.get_mempool()),
            "blocks_stored": await self.state_store.count_blocks(),
            "proposals_stored": await self.state_store.count_proposals(),
            "governance_decisions": await self.state_store.count_governance_decisions()
        }
        
        return report

    def _get_memory_usage(self):
        """Get memory usage in MB."""
        import psutil
        return psutil.Process().memory_info().rss / 1024 / 1024

    def _get_cpu_usage(self):
        """Get CPU usage percentage."""
        import psutil
        return psutil.Process().cpu_percent()

    def _compute_average_reputation(self):
        """Compute average peer reputation."""
        peers = self.state_store.get_peer_reputations()
        if not peers:
            return 1.0
        scores = [p.score for p in peers.values()]
        return sum(scores) / len(scores)
```

## 6. Error Handling & Recovery

### Comprehensive Error Handling

```python
class SystemErrorHandling:
    """Centralized error handling and recovery."""
    
    async def handle_critical_error(self, error, context):
        """Handle critical system errors."""
        
        error_info = {
            "timestamp": time.time(),
            "error_type": type(error).__name__,
            "message": str(error),
            "context": context
        }
        
        # Log error
        await self._log_error(error_info)
        
        # Publish error event
        await self.event_bus.publish({
            "type": "SYSTEM_ERROR",
            "error": error_info
        })
        
        # Attempt recovery
        if isinstance(error, StateCorruptionError):
            await self._recover_from_state_corruption()
        
        elif isinstance(error, ConsensusFailureError):
            await self._recover_from_consensus_failure()
        
        elif isinstance(error, NetworkPartitionError):
            await self._recover_from_network_partition()
        
        elif isinstance(error, RoleFailureError):
            await self._recover_from_role_failure(context['role_name'])
        
        else:
            # Generic recovery
            await self._attempt_generic_recovery()

    async def _recover_from_state_corruption(self):
        """Recover from corrupted state."""
        print("Attempting state recovery...")
        
        # Try to restore from latest snapshot
        snapshots = await self.state_store.get_snapshots()
        if snapshots:
            latest = max(snapshots, key=lambda s: s.version)
            await self.agent_state.restore_snapshot(latest)
            print(f"✓ State restored from snapshot {latest.version}")
        else:
            # Fall back to genesis
            await self._reset_to_genesis()

    async def _recover_from_consensus_failure(self):
        """Recover from consensus failure."""
        print("Attempting consensus recovery...")
        
        # Wait for network to stabilize
        await asyncio.sleep(5)
        
        # Verify peer connectivity
        connected = await self._check_peer_connectivity()
        if connected >= len(self.validators) * 2 // 3:
            print("✓ Sufficient peers connected, retrying consensus")
            # Retry consensus
        else:
            print("✗ Insufficient peers, entering observer mode")

    async def _recover_from_network_partition(self):
        """Recover from network partition."""
        print("Network partition detected...")
        
        # Disable state changes
        await self._set_read_only_mode(True)
        
        # Attempt to reconnect to peers
        await self._reconnect_to_peers()
        
        # Check if partition is resolved
        await asyncio.sleep(10)
        connected = await self._check_peer_connectivity()
        
        if connected >= len(self.validators) * 2 // 3:
            print("✓ Network partition resolved")
            await self._set_read_only_mode(False)
        else:
            print("✗ Network partition persists")

    async def _recover_from_role_failure(self, role_name):
        """Recover from individual role failure."""
        print(f"Recovering role: {role_name}")
        
        role = self.roles.get(role_name)
        if not role:
            return
        
        # Attempt restart
        try:
            await role.finalize()
            await role.initialize()
            await role.execute()
            print(f"✓ Role {role_name} restarted successfully")
        except Exception as e:
            print(f"✗ Failed to restart {role_name}: {e}")
            # Mark role as unavailable
            role.status = RoleStatus.ERROR
```

---

These integration patterns provide complete end-to-end workflows demonstrating how all SYNTHOS Agent systems work together harmoniously.

