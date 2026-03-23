# SYNTHOS Agent Deployment & Production Guide

## Production Deployment Architecture

### System Requirements

#### Minimum Requirements
```
CPU: 4 cores
RAM: 8 GB
Storage: 100 GB SSD
Network: 10 Mbps stable connection
```

#### Recommended Requirements
```
CPU: 8+ cores (optimized for async I/O)
RAM: 32 GB (for state snapshots and caching)
Storage: 500 GB SSD RAID-1 (for redundancy)
Network: 100+ Mbps connection with low latency
```

#### Optional High-Performance Setup
```
CPU: 16+ cores (dedicated crypto operations)
RAM: 64+ GB
Storage: 1+ TB NVMe RAID-10
Network: 1+ Gbps dedicated connection
```

### Deployment Topology

```
┌─────────────────────────────────────────────────────────────┐
│                   PRODUCTION NETWORK                        │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  ┌────────────────────────────────────────────────────┐    │
│  │          Load Balancer / Reverse Proxy             │    │
│  │  (HAProxy / Nginx)                                 │    │
│  │  - API request routing                             │    │
│  │  - TLS termination                                 │    │
│  │  - Connection pooling                              │    │
│  └────────────────┬─────────────────────────────────┘     │
│                   │                                         │
│     ┌─────────────┼─────────────┐                          │
│     │             │             │                          │
│  ┌──▼──┐       ┌──▼──┐       ┌──▼──┐                       │
│  │Node1│       │Node2│       │Node3│  (Validator Nodes)   │
│  │     │       │     │       │     │                       │
│  │●●●●●│       │●●●●●│       │●●●●●│                       │
│  └──┬──┘       └──┬──┘       └──┬──┘                       │
│     │             │             │                          │
│     └─────────────┼─────────────┘                          │
│                   │                                         │
│        ┌──────────┴──────────┐                             │
│        │                     │                             │
│    ┌───▼────┐          ┌─────▼──┐                          │
│    │ Sentry │          │ Metrics│  (Monitoring)           │
│    │ (Logs) │          │(Prometheus)                       │
│    └────────┘          └────────┘                          │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### Configuration Management

#### Config File Structure (`config.yaml`)
```yaml
# config.yaml

agent:
  id: "synthos-validator-01"
  version: "1.0.0"
  network_id: "mainnet"
  
  state:
    snapshot_interval: 1000  # blocks
    state_size_limit: 10000000  # bytes
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
  peer_timeout: 30
  
  gossip:
    hop_limit: 3
    propagation_probability: 0.5
    max_message_size: 1000000

consensus:
  timeout_propose: 3000  # ms
  timeout_prevote: 1000
  timeout_precommit: 1000
  block_interval: 5000
  
  byzantine_tolerance: 0.33  # 1/3 tolerance
  min_voting_power: 100

economics:
  min_transaction_fee: 1
  base_block_reward: 100
  max_inflation_rate: 0.01
  tx_fee_percent: 0.05

storage:
  backend: "rocksdb"
  path: "/var/lib/synthos/data"
  cache_size: 2000000000  # bytes
  
  snapshots:
    enabled: true
    compression: "zstd"
    path: "/var/lib/synthos/snapshots"

monitoring:
  prometheus_port: 9090
  log_level: "info"
  
  sentry:
    enabled: true
    dsn: "https://xxx@sentry.io/project"
    
  metrics:
    export_interval: 60  # seconds
    detailed_metrics: false
```

## Monitoring & Observability

### Metrics Collection

#### Key Metrics to Monitor
```python
class MetricsCollector:
    """Comprehensive metrics collection."""
    
    METRICS = {
        # Transaction Metrics
        "transactions_submitted": 0,
        "transactions_validated": 0,
        "transactions_rejected": 0,
        "transaction_latency_ms": 0,
        "transaction_fee_avg": 0,
        
        # Block Metrics
        "blocks_proposed": 0,
        "blocks_finalized": 0,
        "blockchain_height": 0,
        "block_time_avg": 0,
        "block_size_avg": 0,
        
        # Consensus Metrics
        "consensus_rounds": 0,
        "consensus_finality_rate": 0,
        "consensus_timeout_count": 0,
        "slashing_events": 0,
        
        # Network Metrics
        "peers_connected": 0,
        "peers_total": 0,
        "network_bandwidth_in": 0,
        "network_bandwidth_out": 0,
        "message_latency_avg_ms": 0,
        "gossip_propagation_time_ms": 0,
        
        # State Metrics
        "state_root_updates": 0,
        "state_snapshots_taken": 0,
        "ledger_state_size": 0,
        "accounts_active": 0,
        
        # Role Metrics
        "validator_role_executions": 0,
        "economist_role_executions": 0,
        "governor_role_executions": 0,
        "communicator_role_executions": 0,
        "simulator_role_executions": 0,
        "enforcer_role_executions": 0,
        "citizen_role_executions": 0,
        
        # System Metrics
        "cpu_usage_percent": 0,
        "memory_usage_mb": 0,
        "disk_usage_mb": 0,
        "uptime_seconds": 0,
        "error_count": 0
    }
    
    async def export_prometheus(self):
        """Export metrics in Prometheus format."""
        metrics_text = ""
        
        for metric_name, value in self.METRICS.items():
            metrics_text += f"synthos_{metric_name} {value}\n"
        
        return metrics_text
```

### Logging Configuration

#### Structured Logging
```python
import logging
import json
from datetime import datetime

class StructuredLogger:
    """Structured JSON logging for observability."""
    
    def __init__(self, name, sentry_enabled=False):
        self.logger = logging.getLogger(name)
        self.sentry_enabled = sentry_enabled
    
    def log_event(self, level, event_type, data=None, error=None):
        """Log structured event."""
        log_entry = {
            "timestamp": datetime.utcnow().isoformat(),
            "level": level.upper(),
            "event_type": event_type,
            "data": data or {},
        }
        
        if error:
            log_entry["error"] = {
                "type": type(error).__name__,
                "message": str(error),
                "traceback": traceback.format_exc()
            }
        
        log_message = json.dumps(log_entry)
        
        if level == "debug":
            self.logger.debug(log_message)
        elif level == "info":
            self.logger.info(log_message)
        elif level == "warning":
            self.logger.warning(log_message)
        elif level == "error":
            self.logger.error(log_message)
            if self.sentry_enabled:
                self._report_to_sentry(error, log_entry)
    
    def _report_to_sentry(self, error, context):
        """Report error to Sentry."""
        import sentry_sdk
        with sentry_sdk.push_scope() as scope:
            scope.set_context("event", context)
            sentry_sdk.capture_exception(error)
```

### Health Checks

#### HTTP Health Endpoints
```python
class HealthCheckEndpoints:
    """Health check endpoints for monitoring."""
    
    async def liveness_check(self):
        """Liveness check - is the agent running?"""
        return {
            "status": "alive",
            "timestamp": time.time()
        }
    
    async def readiness_check(self):
        """Readiness check - is the agent ready to serve?"""
        checks = {
            "agent_initialized": self.agent.is_initialized,
            "consensus_synced": await self._is_consensus_synced(),
            "peers_connected": len(self.p2p_messenger.peers) > 0,
            "state_valid": await self._is_state_valid()
        }
        
        all_ready = all(checks.values())
        return {
            "status": "ready" if all_ready else "not_ready",
            "checks": checks
        }
    
    async def metrics_endpoint(self):
        """Metrics in Prometheus format."""
        return await self.metrics_collector.export_prometheus()
    
    async def debug_info(self):
        """Detailed debug information."""
        return {
            "version": self.agent.version,
            "uptime": self.agent.uptime,
            "roles": {
                name: role.status.value
                for name, role in self.roles.items()
            },
            "peers": len(self.p2p_messenger.peers),
            "memory_mb": self._get_memory_usage(),
            "state_height": self.agent.state.get("current_height")
        }
```

## Backup & Disaster Recovery

### State Snapshots

```python
class DisasterRecovery:
    """Disaster recovery and backup management."""
    
    async def create_backup(self):
        """Create complete system backup."""
        backup_id = f"backup_{int(time.time())}"
        backup_dir = self.backup_path / backup_id
        backup_dir.mkdir(parents=True)
        
        # 1. Backup state
        state_snapshot = await self.agent_state.create_snapshot()
        with open(backup_dir / "state.json", "w") as f:
            json.dump(state_snapshot, f)
        
        # 2. Backup storage
        storage_backup = await self.state_store.export_all()
        with open(backup_dir / "storage.json", "w") as f:
            json.dump(storage_backup, f)
        
        # 3. Backup configuration
        shutil.copy(self.config_path, backup_dir / "config.yaml")
        
        # 4. Create manifest
        manifest = {
            "backup_id": backup_id,
            "timestamp": time.time(),
            "version": self.agent.version,
            "height": self.agent.state.get("current_height"),
            "state_root": self.agent.state.get("current_state_root")
        }
        
        with open(backup_dir / "manifest.json", "w") as f:
            json.dump(manifest, f)
        
        print(f"✓ Backup created: {backup_id}")
        return backup_id
    
    async def restore_backup(self, backup_id):
        """Restore system from backup."""
        backup_dir = self.backup_path / backup_id
        
        if not backup_dir.exists():
            raise FileNotFoundError(f"Backup not found: {backup_id}")
        
        print(f"Restoring backup: {backup_id}")
        
        # 1. Stop agent
        await self.agent.stop()
        
        # 2. Restore state
        with open(backup_dir / "state.json") as f:
            state_snapshot = json.load(f)
        await self.agent_state.restore_snapshot(state_snapshot)
        
        # 3. Restore storage
        with open(backup_dir / "storage.json") as f:
            storage_backup = json.load(f)
        await self.state_store.import_all(storage_backup)
        
        # 4. Verify manifest
        with open(backup_dir / "manifest.json") as f:
            manifest = json.load(f)
        
        print(f"✓ Backup restored from height {manifest['height']}")
        
        # 5. Restart agent
        await self.agent.initialize()
        await self.agent.start()
    
    async def schedule_backups(self, interval_blocks=1000):
        """Schedule periodic backups."""
        last_backup_height = 0
        
        while True:
            current_height = self.agent.state.get("current_height", 0)
            
            if current_height - last_backup_height >= interval_blocks:
                await self.create_backup()
                last_backup_height = current_height
            
            await asyncio.sleep(60)  # Check every minute
```

## Security Hardening

### Key Management

```python
class KeyManagement:
    """Validator key management and rotation."""
    
    async def initialize_keys(self, key_path: str):
        """Initialize validator keys."""
        self.key_path = Path(key_path)
        self.key_path.mkdir(parents=True, exist_ok=True)
        
        # Ensure restrictive permissions
        self.key_path.chmod(0o700)
        
        # Load or generate keys
        self.private_key = await self._load_or_generate_private_key()
        self.public_key = self.private_key.public_key()
    
    async def _load_or_generate_private_key(self):
        """Load or generate signing key."""
        key_file = self.key_path / "validator.key"
        
        if key_file.exists():
            with open(key_file, "rb") as f:
                key_data = f.read()
            # Load from file
            from cryptography.hazmat.primitives import serialization
            key = serialization.load_pem_private_key(
                key_data,
                password=None,
                backend=None
            )
            return key
        else:
            # Generate new key
            from cryptography.hazmat.primitives.asymmetric import ed25519
            key = ed25519.Ed25519PrivateKey.generate()
            
            # Save with restricted permissions
            pem = key.private_bytes(
                encoding=serialization.Encoding.PEM,
                format=serialization.PrivateFormat.PKCS8,
                encryption_algorithm=serialization.NoEncryption()
            )
            
            with open(key_file, "wb") as f:
                f.write(pem)
            key_file.chmod(0o600)
            
            return key
    
    async def rotate_keys(self):
        """Rotate validator keys."""
        old_key = self.private_key
        
        # Generate new key
        from cryptography.hazmat.primitives.asymmetric import ed25519
        new_key = ed25519.Ed25519PrivateKey.generate()
        
        # Announce key rotation through governance
        await self.roles["GovernorRole"].propose_change(
            change_type="KEY_ROTATION",
            parameters={
                "old_key": str(old_key.public_key()),
                "new_key": str(new_key.public_key()),
                "rotation_timestamp": time.time()
            }
        )
        
        self.private_key = new_key
        print("✓ Key rotation initiated")
```

### Network Hardening

```python
class NetworkSecurity:
    """Network security hardening."""
    
    async def configure_network_security(self):
        """Configure network security features."""
        
        # 1. TLS/SSL for all connections
        self.tls_config = {
            "enabled": True,
            "cert_path": "/etc/synthos/certs/server.crt",
            "key_path": "/etc/synthos/certs/server.key",
            "verify_client": True,
            "min_tls_version": "1.3"
        }
        
        # 2. Rate limiting
        self.rate_limiter = RateLimiter({
            "transactions_per_second": 1000,
            "blocks_per_minute": 12,
            "messages_per_peer_per_second": 100
        })
        
        # 3. DDoS protection
        self.ddos_protection = {
            "connection_limit_per_ip": 10,
            "enable_syn_cookies": True,
            "blacklist_duration": 3600
        }
        
        # 4. Peer validation
        self.peer_whitelist_enabled = True
        self.peer_whitelist = set()
        
        # 5. Message validation
        self.enable_message_signing = True
        self.enable_message_timestamps = True
```

## Performance Optimization

### State Caching

```python
class PerformanceOptimization:
    """Performance optimization strategies."""
    
    def __init__(self):
        self.state_cache = {}
        self.cache_ttl = 300  # seconds
        self.cache_size_limit = 100000000  # bytes
    
    async def get_with_cache(self, key):
        """Get value with caching."""
        # Check cache first
        if key in self.state_cache:
            cached = self.state_cache[key]
            if time.time() - cached["timestamp"] < self.cache_ttl:
                return cached["value"]
            else:
                del self.state_cache[key]
        
        # Fetch from persistent storage
        value = await self.agent_state.get(key)
        
        # Cache the value
        if sys.getsizeof(value) < self.cache_size_limit / 10:
            self.state_cache[key] = {
                "value": value,
                "timestamp": time.time()
            }
        
        return value
    
    async def batch_validate_transactions(self, transactions):
        """Batch transaction validation for efficiency."""
        # Run validations in parallel
        results = await asyncio.gather(*[
            self.validator_role.validate_transaction(tx)
            for tx in transactions
        ])
        return results
    
    async def optimize_mempool(self):
        """Optimize mempool sorting."""
        mempool = await self.state_store.get_mempool()
        
        # Sort by fee-per-byte descending
        sorted_mempool = sorted(
            mempool,
            key=lambda tx: tx.fee / len(tx.data),
            reverse=True
        )
        
        return sorted_mempool[:self.block_size_limit]
```

## Rolling Deployments

### Graceful Upgrade Path

```python
class RollingDeployment:
    """Rolling deployment strategy."""
    
    async def deploy_new_version(self, new_version):
        """Deploy new agent version with zero downtime."""
        
        print(f"Starting rolling deployment to {new_version}")
        
        # 1. Create snapshot
        snapshot_id = await self.create_backup()
        
        # 2. Stop accepting new transactions
        await self._set_read_only_mode(True)
        
        # 3. Wait for pending consensus to finalize
        await self._wait_for_consensus_finality(timeout=60)
        
        # 4. Create final checkpoint
        await self.agent_state.create_snapshot()
        
        # 5. Download and verify new code
        new_code = await self._download_version(new_version)
        if not await self._verify_code_signature(new_code):
            raise ValueError("Code signature verification failed")
        
        # 6. Perform migration if needed
        if new_version in ["2.0.0", "3.0.0"]:  # Major versions
            await self._perform_state_migration()
        
        # 7. Replace binary
        await self._backup_current_binary()
        await self._install_new_binary(new_code)
        
        # 8. Restart agent
        await self.agent.stop()
        await self.agent.initialize()
        await self.agent.start()
        
        # 9. Verify new version
        if self.agent.version != new_version:
            raise RuntimeError("Version mismatch after deployment")
        
        # 10. Resume normal operation
        await self._set_read_only_mode(False)
        
        print(f"✓ Deployment to {new_version} complete")

    async def rollback_deployment(self, previous_version):
        """Rollback to previous version if needed."""
        print(f"Rolling back to {previous_version}")
        
        # 1. Stop current instance
        await self.agent.stop()
        
        # 2. Restore from backup
        backup_id = f"pre_{previous_version}"
        await self.restore_backup(backup_id)
        
        # 3. Restore previous binary
        await self._restore_binary(previous_version)
        
        print(f"✓ Rollback to {previous_version} complete")
```

---

This deployment guide provides production-ready patterns for deploying, monitoring, and maintaining SYNTHOS Agents in enterprise environments.

---

## Legal notice

SYNTHOS Collective, SYNTHOS, and related names, marks, documentation, and technical materials in this document are the **exclusive property of James G. Isham Williams, Sr.** Unauthorized reproduction, distribution, or commercial use without express written permission is prohibited except as allowed under applicable open-source licenses for identified files. No rights are waived.

This document is informational only and is not legal, financial, or investment advice. The canonical legal notice is in **docs/LEGAL_NOTICE.md** in the SYNTHOS Collective repository.
