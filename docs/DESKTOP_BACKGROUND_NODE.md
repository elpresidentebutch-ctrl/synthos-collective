# SYNTHOS Desktop Background Node

The desktop node has two operator modes:

- Silent background node: one-click install, no local ports, hidden process,
  outbound heartbeats only.
- Desktop agent dashboard: local browser dashboard with start/stop controls and
  local RPC.

For public adoption, the silent background node is the default path.

## One-Click Operator Install

From the repo root:

```powershell
.\scripts\install_background_node.ps1
```

The installer:

- Builds `synthos-silent-node.exe`.
- Installs it to `%LOCALAPPDATA%\SynthosCollective\BackgroundNode`.
- Starts it hidden immediately.
- Creates a Desktop shortcut named `Start Synthos Node`.
- Creates Start Menu shortcuts for start, stop, and status.
- Adds a Startup shortcut so the node launches at login.
- Writes status to `%APPDATA%\SynthosCollective\silent-node-status.json`.

Operators can remove it with:

```powershell
.\scripts\uninstall_background_node.ps1
```

The node does not open inbound ports. It sends outbound HTTPS heartbeats to the
configured SYNTHOS relay registry.

## Dashboard Agent

It runs a local SYNTHOS agent dashboard on `http://127.0.0.1:8788`. From that dashboard, a user can start or stop the local RPC node. When started, the node:

- Runs in the background as a local process.
- Serves chain RPC at `http://127.0.0.1:8080`.
- Persists chain data under the user's config directory.
- Creates a local hardware commitment without exposing raw device identity.
- Registers heartbeat presence with the SYNTHOS peer registry.
- Exposes status needed for adopter reward wiring.

## Run On Windows

From the repo root:

```powershell
.\scripts\start_desktop_agent.ps1
```

The script builds `synthos-desktop-agent.exe` if needed, starts it hidden, and opens the local dashboard.

## Manual Run

```powershell
go build -o synthos-desktop-agent.exe ./cmd/desktopagent
.\synthos-desktop-agent.exe
```

Open:

```text
http://127.0.0.1:8788
```

## Ports

| Service | Default |
|---------|---------|
| Agent dashboard | `127.0.0.1:8788` |
| Chain RPC | `127.0.0.1:8080` |

Override with:

```powershell
$env:SYNTHOS_DESKTOP_AGENT_ADDR="127.0.0.1:8788"
$env:SYNTHOS_DESKTOP_RPC_ADDR="127.0.0.1:8080"
$env:SYNTHOS_DESKTOP_DATA_DIR="$env:APPDATA\SynthosCollective\desktop-node"
```

## Reward Wiring

The desktop node is ready to connect to `SYNTHOSAdopterRewards.sol`.

Deployment and local configuration flow:

```powershell
cd contracts
npm run deploy:synthos:local
cd ..
.\scripts\configure_desktop_rewards.ps1
```

This reads `contracts/deployments/latest.json` and posts the deployed `SynCoin` / `SYNTHOSAdopterRewards` addresses into the desktop agent at `http://127.0.0.1:8788`.

Full wiring path:

1. Deploy `SynCoin`.
2. Deploy `SYNTHOSAdopterRewards`.
3. Fund adopter rewards from `VALIDATOR_REWARDS`.
4. Configure the desktop agent with `scripts/configure_desktop_rewards.ps1`.
5. Have the desktop agent create or connect a wallet.
6. Submit `registerAndClaim(hardwareCommitment, "DESKTOP")`.
7. Submit heartbeat proofs on reward intervals.

Current status: the agent persists and exposes reward contract configuration. The next implementation step is transaction signing/submission from the desktop agent to call `registerAndClaim`.

## Tauri Tray Wrapper

The Go agent is the background core. The Tauri tray app should wrap this process and provide:

- Start Node
- Stop Node
- Open Dashboard
- Open RPC Status
- Launch at Login
- Earning / heartbeat status

Rust/Tauri tooling must be repaired before building that wrapper on this machine.
