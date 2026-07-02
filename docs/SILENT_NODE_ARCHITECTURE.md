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

For normal operators, use the installer:

```powershell
.\scripts\install_background_node.ps1
```

This builds and installs the node under the user's local app-data folder, starts
it hidden, creates Start Menu shortcuts, creates a Desktop "Start Synthos Node"
shortcut, and adds a launch-at-login shortcut. After this, the operator does not
need a terminal; the node runs in the background and keeps sending outbound
heartbeats.

Status:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File "$env:LOCALAPPDATA\SynthosCollective\BackgroundNode\Synthos Node Status.ps1"
```

Uninstall:

```powershell
.\scripts\uninstall_background_node.ps1
```

Developer direct-run path:

Run:

```powershell
.\scripts\start_silent_node.ps1
```

This starts `cmd/silentnode`, which does not bind a socket. It only sends outbound registration heartbeats and optional mailbox polls.

Environment:

```powershell
$env:SYNTHOS_RELAY_URLS="http://127.0.0.1:8090,https://relay-2.example,https://community-relay.example"
```

Backward-compatible single-relay variables are still accepted:

```powershell
$env:SYNTHOS_REGISTRY_URL="http://127.0.0.1:8090"
$env:SYNTHOS_MAILBOX_URL="http://127.0.0.1:8090"
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
