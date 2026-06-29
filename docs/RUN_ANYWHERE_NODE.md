# SYNTHOS Run-Anywhere Node

## Product promise

One visible action enrolls a phone, tablet, PC, or Mac into SYNTHOS. The
software detects the strongest background runtime the platform permits and
shows the user exactly which role is active.

The button is universal. The runtime is capability-based.

## Runtime tiers

| Tier | Platforms | Background behavior | Consensus role |
|---|---|---|---|
| Continuous Immune Validator | Native Windows, macOS, Linux; native Android with foreground-service permission | Continuous process with signed health and state verification | Eligible after validator-set admission |
| Installed Immune Light Node | Installed desktop PWA | Foreground verification; best-effort background wakeups | Observer and evidence producer |
| Android Immune Sentinel | Installed Android PWA | Foreground verification plus opportunistic browser wakeups | Observer and evidence producer |
| iPhone/iPad Immune Sentinel | Installed iOS PWA | Sync on open/resume; iOS-controlled background opportunities | Observer and evidence producer |
| Browser Immune Sentinel | Any modern browser | Active-tab verification and catch-up on return | Observer and evidence producer |

No browser-only node is represented as continuously online or counted toward a
finality quorum. Validator eligibility requires a native runtime, secure key
storage, deterministic execution of the canonical state machine, and explicit
admission to the validator set.

## One-button lifecycle

1. Detect platform capabilities.
2. Create or unlock a device-local agent identity.
3. Select the runtime tier.
4. Register a signed capability statement.
5. Sync and verify finalized chain state.
6. Begin platform-appropriate heartbeat and immune observation work.
7. Catch up immediately after suspension or network loss.

The interface must display the node identity, selected tier, background
guarantee, consensus eligibility, last successful sync, and stop/pause controls.

## Native delivery targets

### Desktop

Package the Go node as a signed tray application using a Windows startup
service, macOS LaunchAgent, or Linux systemd user service. It should update from
signed releases and require no public inbound port by default.

### Android

Use a native foreground service, Android Keystore-backed keys, WorkManager
catch-up, and user controls for Wi-Fi, charging, battery, and mobile-data use.

### iOS and iPadOS

Use Keychain or Secure Enclave keys and BGProcessingTask for opportunistic
sync. Apple does not permit arbitrary continuous background execution, so iOS
devices remain immune sentinels unless the platform grants an appropriate
execution mode.

## Security requirements

- Private keys never enter JavaScript localStorage.
- Native validator keys live in OS secure storage or external hardware.
- Service workers receive only public identity and non-secret configuration.
- Every heartbeat and capability statement is signed.
- A device cannot self-assign validator authority.
- Rewards require verifiable work, not merely an online heartbeat.
- Battery suspension is not slashable behavior for sentinel tiers.
