# Advanced Patterns & Best Practices

## Advanced Architectural Patterns

### 1. Event Sourcing Pattern

**Problem**: Reconstructing agent history for debugging and analysis
**Solution**: Store all events immutably and replay them

```python
class EventSourcingEngine:
    """Event sourcing for complete history tracking."""
    
    def __init__(self):
        self.event_log = []
        self.snapshots = []
    
    async def append_event(self, event):
        """Append event to immutable log."""
        logged_event = {
            "id": str(uuid.uuid4()),
            "timestamp": time.time(),
            "event": event,
            "sequence_number": len(self.event_log) + 1
        }
        self.event_log.append(logged_event)
        return logged_event
    
    async def replay_events(self, from_sequence=0, to_sequence=None):
        """Replay events to reconstruct state."""
        events_to_replay = self.event_log[from_sequence:to_sequence]
        state = {}
        
        for event in events_to_replay:
            state = await self._apply_event(state, event["event"])
        
        return state
    
    async def create_snapshot(self, at_sequence):
        """Create snapshot at specific sequence."""
        snapshot = {
            "sequence": at_sequence,
            "timestamp": time.time(),
            "state": await self.replay_events(0, at_sequence)
        }
        self.snapshots.append(snapshot)
        return snapshot
    
    async def restore_from_snapshot(self, snapshot_id):
        """Restore state from snapshot and replay subsequent events."""
        snapshot = next(s for s in self.snapshots if s["sequence"] == snapshot_id)
        subsequent_events = self.event_log[snapshot["sequence"]:]
        
        state = snapshot["state"]
        for event in subsequent_events:
            state = await self._apply_event(state, event["event"])
        
        return state
```

### 2. Command Query Responsibility Segregation (CQRS)

**Problem**: Mixing transaction processing (writes) with state queries (reads)
**Solution**: Separate command and query paths with eventual consistency

```python
class CommandQueryResponsibilitySeparation:
    """CQRS implementation for scalability."""
    
    class CommandHandler:
        """Handle state-changing operations (writes)."""
        
        async def submit_transaction(self, tx):
            """Write operation."""
            await self._validate_transaction(tx)
            await self._apply_transaction(tx)
            await self.event_bus.publish({
                "type": "TRANSACTION_APPLIED",
                "transaction": tx
            })
        
        async def propose_block(self, transactions):
            """Write operation."""
            block = await self._assemble_block(transactions)
            await self._persist_block(block)
            await self.event_bus.publish({
                "type": "BLOCK_PROPOSED",
                "block": block
            })
    
    class QueryHandler:
        """Handle read-only operations (reads)."""
        
        def __init__(self):
            self.query_cache = {}
            self.cache_ttl = 5  # seconds
        
        async def get_account_balance(self, account):
            """Read operation."""
            cache_key = f"balance_{account}"
            
            if cache_key in self.query_cache:
                cached, timestamp = self.query_cache[cache_key]
                if time.time() - timestamp < self.cache_ttl:
                    return cached
            
            balance = await self._query_ledger(account)
            self.query_cache[cache_key] = (balance, time.time())
            return balance
        
        async def get_block_height(self):
            """Read operation."""
            return await self._query_blockchain_height()
        
        async def get_pending_transactions(self):
            """Read operation."""
            return await self._query_mempool()
        
        async def get_consensus_status(self):
            """Read operation."""
            return await self._query_consensus_round()
        
        async def get_governance_proposals(self):
            """Read operation."""
            return await self._query_proposals()
    
    def __init__(self):
        self.command_handler = self.CommandHandler()
        self.query_handler = self.QueryHandler()
        
        # Subscribe command handlers to events
        self.event_bus.subscribe(
            "COMMAND_RECEIVED",
            self.command_handler.process_command
        )
        
        # Async update read model on events
        self.event_bus.subscribe(
            "EVENT_OCCURRED",
            self.query_handler.update_read_model
        )
```

### 3. Saga Pattern for Distributed Transactions

**Problem**: Coordinating multi-step transactions across roles/chains
**Solution**: Saga pattern with compensating transactions

```python
class SagaOrchestrator:
    """Saga pattern for distributed transactions."""
    
    class CrossChainTransactionSaga:
        """Execute cross-chain transaction with rollback."""
        
        def __init__(self, saga_id, source_chain, dest_chain, amount):
            self.saga_id = saga_id
            self.source_chain = source_chain
            self.dest_chain = dest_chain
            self.amount = amount
            self.state = "INITIATED"
            self.steps = []
        
        async def execute(self):
            """Execute saga with rollback on failure."""
            try:
                # Step 1: Lock funds on source chain
                await self._step_1_lock_source()
                
                # Step 2: Create proof on source chain
                await self._step_2_create_proof()
                
                # Step 3: Transfer on destination chain
                await self._step_3_transfer_dest()
                
                # Step 4: Unlock funds on destination chain
                await self._step_4_unlock_dest()
                
                self.state = "COMPLETED"
                return True
                
            except Exception as e:
                print(f"Saga failed at step {len(self.steps)}: {e}")
                await self._compensate()
                self.state = "COMPENSATED"
                return False
        
        async def _step_1_lock_source(self):
            """Lock funds on source chain."""
            try:
                await self.source_chain.lock_funds(self.amount)
                self.steps.append(("LOCK_SOURCE", True))
            except Exception as e:
                raise SagaStepFailure("Step 1 failed", step=1, error=e)
        
        async def _step_2_create_proof(self):
            """Create cross-chain proof."""
            try:
                proof = await self.source_chain.create_proof(self.amount)
                self.steps.append(("CREATE_PROOF", proof))
            except Exception as e:
                raise SagaStepFailure("Step 2 failed", step=2, error=e)
        
        async def _step_3_transfer_dest(self):
            """Transfer on destination chain."""
            try:
                proof = self.steps[1][1]
                await self.dest_chain.transfer(self.amount, proof)
                self.steps.append(("TRANSFER_DEST", True))
            except Exception as e:
                raise SagaStepFailure("Step 3 failed", step=3, error=e)
        
        async def _step_4_unlock_dest(self):
            """Unlock funds on destination chain."""
            try:
                await self.dest_chain.unlock_funds(self.amount)
                self.steps.append(("UNLOCK_DEST", True))
            except Exception as e:
                raise SagaStepFailure("Step 4 failed", step=4, error=e)
        
        async def _compensate(self):
            """Compensate in reverse order."""
            print("Starting saga compensation...")
            
            # Rollback in reverse
            if len(self.steps) >= 3:
                try:
                    await self.dest_chain.revert_transfer()
                except:
                    pass
            
            if len(self.steps) >= 1:
                try:
                    await self.source_chain.unlock_funds(self.amount)
                except:
                    pass
            
            print("Saga compensation complete")
```

### 4. Branch by Abstraction Pattern

**Problem**: Deploying new features gradually without full rollout
**Solution**: Feature flags and abstraction layers

```python
class FeatureBranchingLayer:
    """Feature branching for gradual rollout."""
    
    def __init__(self):
        self.feature_flags = {
            "new_consensus_engine": False,
            "enhanced_validation": False,
            "optimized_mempool": True,
            "cross_chain_support": False
        }
        self.rollout_percentages = {
            "new_consensus_engine": 0.1,  # 10% of traffic
            "enhanced_validation": 0.5,   # 50% of traffic
            "cross_chain_support": 0.0    # Not rolled out yet
        }
    
    async def execute_transaction_validation(self, tx):
        """Choose validation strategy based on feature flags."""
        
        if self._is_feature_enabled("enhanced_validation"):
            return await self._validate_transaction_v2(tx)
        else:
            return await self._validate_transaction_v1(tx)
    
    async def execute_consensus(self, block):
        """Choose consensus strategy based on feature flags."""
        
        if self._is_feature_enabled("new_consensus_engine"):
            return await self._consensus_v2(block)
        else:
            return await self._consensus_v1(block)
    
    def _is_feature_enabled(self, feature_name):
        """Check if feature is enabled with percentage rollout."""
        
        if not self.feature_flags.get(feature_name, False):
            return False
        
        rollout_pct = self.rollout_percentages.get(feature_name, 0)
        random_value = random.random()  # 0.0 to 1.0
        
        return random_value < rollout_pct  # Enable for this request with probability
    
    async def enable_feature(self, feature_name, percentage=1.0):
        """Enable feature with gradual rollout."""
        self.feature_flags[feature_name] = True
        self.rollout_percentages[feature_name] = percentage
        print(f"✓ Feature '{feature_name}' enabled at {percentage*100:.1f}%")
```

### 5. Circuit Breaker Pattern

**Problem**: Cascading failures when downstream services fail
**Solution**: Circuit breaker to prevent cascading failures

```python
class CircuitBreakerPattern:
    """Circuit breaker for fault tolerance."""
    
    class CircuitBreaker:
        def __init__(self, name, failure_threshold=5, timeout=60):
            self.name = name
            self.failure_threshold = failure_threshold
            self.timeout = timeout
            self.state = "CLOSED"  # CLOSED, OPEN, HALF_OPEN
            self.failure_count = 0
            self.last_failure_time = None
            self.success_count = 0
        
        async def call(self, func, *args, **kwargs):
            """Execute function with circuit breaker protection."""
            
            if self.state == "OPEN":
                if self._should_attempt_reset():
                    self.state = "HALF_OPEN"
                    self.success_count = 0
                else:
                    raise CircuitBreakerOpenError(f"{self.name} circuit is OPEN")
            
            try:
                result = await func(*args, **kwargs)
                await self._on_success()
                return result
            except Exception as e:
                await self._on_failure()
                if self.state == "OPEN":
                    raise CircuitBreakerOpenError(f"{self.name} circuit is OPEN") from e
                else:
                    raise
        
        async def _on_success(self):
            """Handle successful call."""
            self.failure_count = 0
            
            if self.state == "HALF_OPEN":
                self.success_count += 1
                if self.success_count >= 3:  # 3 successes close circuit
                    self.state = "CLOSED"
                    print(f"✓ Circuit breaker '{self.name}' CLOSED")
        
        async def _on_failure(self):
            """Handle failed call."""
            self.failure_count += 1
            self.last_failure_time = time.time()
            
            if self.failure_count >= self.failure_threshold:
                self.state = "OPEN"
                print(f"✗ Circuit breaker '{self.name}' OPEN")
            
            if self.state == "HALF_OPEN":
                self.state = "OPEN"
                print(f"✗ Circuit breaker '{self.name}' OPEN (half-open failed)")
        
        def _should_attempt_reset(self):
            """Check if timeout has elapsed."""
            if self.last_failure_time is None:
                return True
            return time.time() - self.last_failure_time >= self.timeout
    
    def __init__(self):
        self.breakers = {
            "peer_network": self.CircuitBreaker("peer_network"),
            "consensus_engine": self.CircuitBreaker("consensus_engine"),
            "state_store": self.CircuitBreaker("state_store"),
            "validator": self.CircuitBreaker("validator")
        }
    
    async def connect_peer(self, peer):
        """Connect to peer with circuit breaker."""
        return await self.breakers["peer_network"].call(
            self._do_connect_peer,
            peer
        )
    
    async def finalize_consensus(self, block):
        """Finalize consensus with circuit breaker."""
        return await self.breakers["consensus_engine"].call(
            self._do_finalize_consensus,
            block
        )
```

## Advanced Optimization Patterns

### 6. Sharding Pattern

**Problem**: Single agent bottleneck on throughput
**Solution**: Shard transactions across multiple agents

```python
class ShardingLayer:
    """Horizontal sharding for throughput scaling."""
    
    def __init__(self, shard_count=4):
        self.shard_count = shard_count
        self.shards = [
            Shard(shard_id=i) for i in range(shard_count)
        ]
    
    async def route_transaction(self, tx):
        """Route transaction to appropriate shard."""
        shard_id = self._compute_shard(tx.sender)
        shard = self.shards[shard_id]
        return await shard.process_transaction(tx)
    
    def _compute_shard(self, account):
        """Compute shard ID from account."""
        account_hash = int(hashlib.sha256(
            account.encode()
        ).hexdigest(), 16)
        return account_hash % self.shard_count
    
    async def cross_shard_transfer(self, tx):
        """Handle cross-shard transactions."""
        
        source_shard_id = self._compute_shard(tx.sender)
        dest_shard_id = self._compute_shard(tx.recipient)
        
        if source_shard_id == dest_shard_id:
            return await self.shards[source_shard_id].process_transaction(tx)
        
        # Cross-shard transaction
        source_shard = self.shards[source_shard_id]
        dest_shard = self.shards[dest_shard_id]
        
        # Lock funds in source shard
        lock_proof = await source_shard.lock_funds(
            tx.sender, tx.amount
        )
        
        # Transfer in destination shard
        await dest_shard.unlock_funds(
            tx.recipient, tx.amount, lock_proof
        )
        
        return True
```

### 7. Batch Processing Pattern

**Problem**: Processing transactions one-by-one is slow
**Solution**: Batch processing with optimized ordering

```python
class BatchProcessingEngine:
    """Batch processing for transaction throughput."""
    
    def __init__(self, batch_size=1000, timeout_ms=1000):
        self.batch_size = batch_size
        self.timeout_ms = timeout_ms
        self.current_batch = []
        self.batch_timer = None
    
    async def submit_transaction(self, tx):
        """Submit transaction to batch."""
        self.current_batch.append(tx)
        
        if len(self.current_batch) >= self.batch_size:
            await self._flush_batch()
        else:
            self._reset_batch_timer()
    
    def _reset_batch_timer(self):
        """Reset batch timer to flush soon."""
        if self.batch_timer:
            self.batch_timer.cancel()
        
        self.batch_timer = asyncio.create_task(
            self._batch_timeout()
        )
    
    async def _batch_timeout(self):
        """Wait timeout and flush if batch exists."""
        try:
            await asyncio.sleep(self.timeout_ms / 1000.0)
            if self.current_batch:
                await self._flush_batch()
        except asyncio.CancelledError:
            pass
    
    async def _flush_batch(self):
        """Process batch optimally."""
        if not self.current_batch:
            return
        
        batch = self.current_batch
        self.current_batch = []
        
        # Optimize transaction order
        batch = self._optimize_batch(batch)
        
        # Validate in parallel
        results = await asyncio.gather(*[
            self.validator.validate_transaction(tx)
            for tx in batch
        ])
        
        # Separate valid and invalid
        valid_txs = [
            tx for tx, result in zip(batch, results)
            if result.is_valid
        ]
        
        # Apply valid transactions
        await self._apply_batch(valid_txs)
    
    def _optimize_batch(self, batch):
        """Optimize transaction order within batch."""
        # Sort by sender (reduce state accesses)
        return sorted(batch, key=lambda tx: tx.sender)
```

## Monitoring & Observability Patterns

### 8. Distributed Tracing

**Problem**: Understanding request flow across multiple async operations
**Solution**: Distributed tracing with correlation IDs

```python
class DistributedTracingPattern:
    """Distributed tracing for observability."""
    
    def __init__(self):
        self.tracer = None  # Initialize with Jaeger/Zipkin
    
    async def trace_transaction_flow(self, tx):
        """Trace complete transaction flow with correlation ID."""
        
        trace_id = str(uuid.uuid4())
        span = self.tracer.start_span(
            operation_name="transaction.process",
            tags={
                "transaction.id": tx.id,
                "trace.id": trace_id
            }
        )
        
        try:
            # Validation span
            with self.tracer.start_span("validation", child_of=span):
                is_valid = await self._validate_with_trace(tx, trace_id)
            
            if not is_valid:
                span.set_tag("transaction.status", "REJECTED")
                return False
            
            # Mempool span
            with self.tracer.start_span("mempool.add", child_of=span):
                await self._add_to_mempool_with_trace(tx, trace_id)
            
            # Gossip span
            with self.tracer.start_span("gossip.propagate", child_of=span):
                await self._gossip_with_trace(tx, trace_id)
            
            span.set_tag("transaction.status", "ACCEPTED")
            return True
            
        finally:
            span.finish()
    
    async def _validate_with_trace(self, tx, trace_id):
        """Validation with trace propagation."""
        return await self.validator.validate_transaction(tx, trace_id=trace_id)
```

### 9. Structured Logging with Context

**Problem**: Logs lack context making debugging difficult
**Solution**: Structured logging with correlation IDs

```python
class ContextualLogging:
    """Context-aware structured logging."""
    
    class LogContext:
        def __init__(self):
            self.correlation_id = str(uuid.uuid4())
            self.user_id = None
            self.request_id = None
            self.span_id = None
    
    _context = contextvars.ContextVar('log_context')
    
    @staticmethod
    def get_context():
        """Get current log context."""
        try:
            return ContextualLogging._context.get()
        except LookupError:
            ctx = ContextualLogging.LogContext()
            ContextualLogging._context.set(ctx)
            return ctx
    
    @staticmethod
    def log_with_context(level, message, **data):
        """Log with automatic context inclusion."""
        ctx = ContextualLogging.get_context()
        
        log_entry = {
            "timestamp": datetime.utcnow().isoformat(),
            "level": level,
            "message": message,
            "correlation_id": ctx.correlation_id,
            "span_id": ctx.span_id,
            **data
        }
        
        print(json.dumps(log_entry))
```

## Testing Patterns

### 10. Chaos Engineering

**Problem**: Finding failures that only occur under specific conditions
**Solution**: Chaos engineering to deliberately introduce failures

```python
class ChaosEngineeringFramework:
    """Chaos engineering for resilience testing."""
    
    def __init__(self):
        self.chaos_scenarios = []
        self.enabled = False
    
    def inject_peer_failure(self, peer_id, duration=10):
        """Inject peer failure."""
        self.chaos_scenarios.append({
            "type": "peer_failure",
            "peer_id": peer_id,
            "duration": duration,
            "start_time": time.time()
        })
    
    def inject_network_latency(self, min_latency=100, max_latency=500):
        """Inject network latency."""
        self.chaos_scenarios.append({
            "type": "network_latency",
            "min": min_latency,
            "max": max_latency,
            "active": True
        })
    
    def inject_validation_failure(self, failure_rate=0.1):
        """Inject validation failures."""
        self.chaos_scenarios.append({
            "type": "validation_failure",
            "failure_rate": failure_rate,
            "active": True
        })
    
    async def apply_chaos(self):
        """Apply active chaos scenarios."""
        if not self.enabled:
            return
        
        for scenario in self.chaos_scenarios:
            if scenario["type"] == "peer_failure":
                await self._simulate_peer_failure(scenario)
            elif scenario["type"] == "network_latency":
                await self._simulate_network_latency(scenario)
            elif scenario["type"] == "validation_failure":
                await self._simulate_validation_failure(scenario)
    
    async def run_resilience_test(self):
        """Run full resilience test."""
        test_results = {
            "timestamp": time.time(),
            "scenarios": [],
            "agent_survived": True,
            "recovery_time": 0
        }
        
        # Record initial state
        initial_height = self.agent.state.get("current_height")
        start_time = time.time()
        
        # Run chaos
        await self.apply_chaos()
        
        # Wait for recovery
        await asyncio.sleep(30)
        
        # Check recovery
        final_height = self.agent.state.get("current_height")
        recovery_time = time.time() - start_time
        
        test_results["scenarios_run"] = len(self.chaos_scenarios)
        test_results["recovery_time"] = recovery_time
        test_results["height_recovered"] = final_height > initial_height
        
        return test_results
```

---

These advanced patterns enable building production-grade distributed systems with the SYNTHOS Agent framework.

---

## Legal notice

SYNTHOS Collective, SYNTHOS, and related names, marks, documentation, and technical materials in this document are the **exclusive property of James G. Isham Williams, Sr.** Unauthorized reproduction, distribution, or commercial use without express written permission is prohibited except as allowed under applicable open-source licenses for identified files. No rights are waived.

This document is informational only and is not legal, financial, or investment advice. The canonical legal notice is in **docs/LEGAL_NOTICE.md** in the SYNTHOS Collective repository.
