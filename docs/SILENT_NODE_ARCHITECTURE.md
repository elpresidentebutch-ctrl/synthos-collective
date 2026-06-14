# SYNTHOS Silent Node Architecture

SYNTHOS nodes must not require inbound ports.

The correct node model is:

- No local HTTP server.
- No inbound P2P listener.
- No port forwarding.
- No public device URL.
- Outbound HTTPS heartbeat only.
- Outbound mailbox polling only.
- Multiple relay endpoints.
- Relay failures are tolerated.
- Relays are not trusted authorities.
- Local keys stay local.

## Desktop

Run:

```powershell
.\scripts\start_silent_node.ps1
```

This starts `cmd/silentnode`, which does not bind a socket. It only sends outbound registration heartbeats and optional mailbox polls.

Environment:

```powershell
$env:SYNTHOS_RELAY_URLS="https://synthos-peer-registry.jamesishamwilliams.workers.dev,https://relay-2.example,https://community-relay.example"
```

Backward-compatible single-relay variables are still accepted:

```powershell
$env:SYNTHOS_REGISTRY_URL="https://synthos-peer-registry.jamesishamwilliams.workers.dev"
$env:SYNTHOS_MAILBOX_URL="https://your-mailbox-relay.example"
```

The preferred variable is `SYNTHOS_RELAY_URLS`. The node sends heartbeats to every relay and polls every mailbox. If one relay disappears, the node keeps operating against the rest.

## Multi-Relay Trust Model

The mailbox layer is transport, not truth.

- A relay may be offline.
- A relay may be slow.
- A relay may censor messages.
- A relay may replay or inject garbage.
- A relay must not be able to rewrite balances, validator rewards, DEX contracts, or chain history.

SYNTHOS handles this by requiring node-side verification:

- Signed messages for commands, sync notices, reward proofs, and peer updates.
- Local persistence of node identity, last heartbeat, relay set, chain tip, and reward state.
- On-chain checkpoints for critical public facts like reward roots and DEX addresses.
- Relay rotation and multiple public/community relay endpoints.

That keeps the original SYNTHOS promise intact: users never open inbound ports, but no single mailbox owns the network.

## DEX

The DEX must not depend on a user node opening ports.

The real DEX path is:

- Smart contracts deployed to an EVM network.
- Static website served from the public website.
- Wallet talks outbound to its RPC provider.
- User node separately runs silent outbound heartbeats/polling.

Local Hardhat ports are only a developer rehearsal tool. They are not the SYNTHOS production node model.

## Launch Rule

If a SYNTHOS component requires a normal adopter to open a port, it is not ready for the public SYNTHOS node experience.
