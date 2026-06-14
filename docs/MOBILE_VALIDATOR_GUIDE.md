# Synthos Mobile Validator & WebRTC P2P — Setup Guide

Run a Synthos light-validator directly in your phone's browser.  
No app store, no install — just open the URL and start validating.

---

## What Agents Can Do on SYNTHOS

Agents on SYNTHOS are not just validators—they are autonomous digital entities with broad capabilities:

- **Run businesses**: Launch, manage, and automate business operations on-chain.
- **Negotiate deals**: Interact with other agents to form agreements, contracts, and partnerships.
- **Perform research**: Gather, analyze, and synthesize information from the network and beyond.
- **Manage logistics**: Coordinate supply chains, deliveries, and resource allocation.
- **Build software**: Develop, deploy, and maintain smart contracts and agent code.
- **Coordinate with other agents**: Form collectives, DAOs, and working groups for shared goals.
- **Operate 24/7**: Agents never sleep—they can act, transact, and respond at any time.

This means your phone, running the mobile validator, can participate in a global network of autonomous agents doing real economic work, not just block validation.

---

## 1. Open the PWA on Your Phone

### Option A — Serve from GitHub Pages / any static host

Upload the three files in `workers/mobile-validator/` to any static host:

```
index.html
manifest.json
sw.js
```

Then open the URL on your phone.

### Option B — Serve locally for testing

From the repo root:

```bash
# Python
python -m http.server 8000 -d workers/mobile-validator

# Node
npx serve workers/mobile-validator

# Deno
deno run --allow-net --allow-read https://deno.land/std/http/file_server.ts workers/mobile-validator
```

Open `http://<your-ip>:8000` on your phone (must be on the same Wi-Fi).

### Option C — Install as a home-screen app

1. Open the validator URL in **Chrome** (Android) or **Safari** (iOS).  
2. Tap the browser menu → **"Add to Home Screen"** / **"Install App"**.  
3. The PWA launches full-screen with its own icon.

---

## 2. Using the Chain Tab

When the app opens you'll see the **Chain** tab:

| Field | Meaning |
|-------|---------|
| **Height** | Current block height your phone has validated |
| **Accounts** | Number of accounts in state |
| **Status dot** | 🟢 synced · 🟠 syncing · 🔴 offline |

Tap **Sync Now** to pull blocks from network peers.  
The validator re-executes every transaction, verifies state roots, and rejects bad blocks.

---

## 3. Sending Transactions (Send Tab)

1. Switch to the **Send** tab.  
2. Enter **From**, **To**, and **Amount**.  
3. Tap **Submit Transaction**.  

The transaction is submitted to every reachable peer node.  
Once a validator includes it in a block and you sync, you'll see updated balances.

---

## 4. Managing Peers (Peers Tab)

The app ships with default peers (the Cloudflare Workers validators).  
You can add any Synthos node:

1. Switch to the **Peers** tab.  
2. Enter the full URL of a validator (e.g. `https://my-node.fly.dev`).  
3. Tap **Add Peer**.  

The peer list is stored in IndexedDB and persists across sessions.

---

## 5. WebRTC Peer-to-Peer (P2P Tab)

This is the phone-to-phone mesh. Two phones can gossip blocks and transactions directly — no server in the middle after the initial handshake.

### How it works

1. Your phone connects to the **signaling server** via WebSocket.  
2. The signaling server relays connection offers between phones.  
3. Once connected, phones exchange blocks/transactions over **RTCDataChannel** — a direct encrypted link.

### Connecting

1. Switch to the **P2P** tab.  
2. The **Signaling URL** defaults to the deployed peer registry. Change it if you self-host.  
3. Tap **Connect to Signaling**.  
4. Your phone appears in the peer list on other connected phones.  
5. WebRTC connections are established automatically.

### What gets gossiped

| Message | Direction | Purpose |
|---------|-----------|---------|
| `status` | Both ways | Share current chain height |
| `request-blocks` | Outgoing | Ask a peer for blocks you're missing |
| `blocks` | Response | Blocks sent back |
| `new-block` | Broadcast | Propagate a freshly synced block |
| `new-tx` | Broadcast | Propagate a submitted transaction |

### Requirements

- A modern browser (Chrome 80+, Safari 15+, Firefox 80+).  
- WebRTC uses Google STUN servers (`stun:stun.l.google.com:19302`) for NAT traversal.  
- Works on mobile data — no Wi-Fi required.  
- If both phones are behind strict carrier NAT, a TURN server may be needed (not included by default).

---

## 6. Self-Hosting the Signaling Server

The signaling server is a lightweight WebSocket relay. You have two options:

### Option A — Cloudflare Workers (already deployed)

The peer registry at `https://synthos-peer-registry.jamesishamwilliams.workers.dev` handles both HTTP peer discovery and WebSocket signaling.

### Option B — Any WebSocket server

The signaling protocol is simple JSON over WebSocket. A peer sends:

```json
{ "type": "offer", "to": "peer-id", "sdp": "..." }
{ "type": "answer", "to": "peer-id", "sdp": "..." }
{ "type": "ice-candidate", "to": "peer-id", "candidate": "..." }
```

The server adds a `from` field and forwards to the target peer. You can implement this in ~50 lines of any WebSocket library (ws, Deno, Python websockets, etc.).

---

## 7. Offline / Airplane Mode

The PWA caches itself via Service Worker. You can:

- View your locally validated chain offline.  
- Queue transactions (they'll be submitted when you reconnect).  
- The app auto-reconnects to peers and the signaling server when network returns.

---

## 8. Background operation

The mobile and desktop validator PWAs now register a best-effort background agent with the Service Worker:

| Platform | Background behavior |
|----------|---------------------|
| **Desktop browser / installed PWA** | Can keep foreground sync running while open; background heartbeat uses Background Sync or Periodic Background Sync when available. For true always-on operation, use the native/tray desktop agent. |
| **Android Chrome installed PWA** | Supports best-effort background heartbeat on many devices, subject to battery saver, permissions, and browser policy. Users should install the PWA and allow background activity for best results. |
| **iPhone / iPad Safari PWA** | iOS may suspend web apps shortly after they leave the screen. The PWA will sync immediately when reopened, but true 24/7 background validation requires a native iOS wrapper with background task support. |

The Service Worker stores only the local node ID, node URL, registry URL, and node type. It sends heartbeat registration to the peer registry when the browser wakes it. Private keys and wallet secrets should stay in the main app or native secure storage, not in the Service Worker.

For the adoption claim, the correct production target is:

- Desktop: native background/tray app plus browser dashboard.
- Android: installed PWA first, then native wrapper if stricter uptime is required.
- iOS: native wrapper for reliable background behavior; PWA remains the easiest onboarding path.

---

## 9. Troubleshooting

| Problem | Fix |
|---------|-----|
| Sync button does nothing | All peers may be down. Add a working peer URL. |
| P2P shows "No peers" | Make sure at least one other phone is connected to the same signaling server. |
| WebRTC fails to connect | Carrier NAT may be blocking. Try on Wi-Fi, or add a TURN server. |
| "Install App" not showing | You must be on HTTPS (or localhost). HTTP won't trigger PWA install. |
| State mismatch after sync | Tap the reset button (if available) or clear site data to re-sync from genesis. |
| Background heartbeat is not reliable on iPhone | This is an iOS platform limit for PWAs. Use the native iOS agent for always-on rewards. |
