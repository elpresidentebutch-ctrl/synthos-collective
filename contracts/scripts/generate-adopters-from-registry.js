const fs = require("fs");
const path = require("path");
const https = require("https");
const http = require("http");
const { ethers } = require("ethers");

// Builds the community adopter list from the node registry, then the Merkle tree.
//
// Only OUTSIDE operators qualify. A node is skipped when:
//   - it is one of our own nodes (fleet cloud, fleet name prefix, or a home IP),
//   - it has no payout_address that is a real EVM address (the reward is
//     claimed on Base by msg.sender, so the operator must control that wallet),
//   - it has less than MIN_UPTIME_DAYS of verified uptime (default 30, the
//     node reward plan's first milestone), or fewer than MIN_HEARTBEATS.
//
// Settings (environment variables):
//   REGISTRY_URL     registry nodes endpoint (or pass it as the first argument)
//   FLEET_CLOUDS     comma list, default fleet,hosteons,gitlab,lovable,render
//   FLEET_PREFIXES   comma list, default synthos-,syn-fleet-,syn-gitlab-cloud-,desktop-
//   HOME_IPS         comma list of our own IP prefixes (matched against household_ip)
//   MIN_UPTIME_DAYS  default 30
//   MIN_HEARTBEATS   default 20
const DEFAULT_REGISTRY_URL = "https://82-29-54-7.sslip.io/api/nodes";
const DAY_MS = 24 * 60 * 60 * 1000;

function listFromEnv(name, fallback) {
  return String(process.env[name] ?? fallback)
    .split(",")
    .map((v) => v.trim().toLowerCase())
    .filter(Boolean);
}

const FLEET_CLOUDS = listFromEnv("FLEET_CLOUDS", "fleet,hosteons,gitlab,lovable,render");
const FLEET_PREFIXES = listFromEnv("FLEET_PREFIXES", "synthos-,syn-fleet-,syn-gitlab-cloud-,desktop-");
const HOME_IPS = listFromEnv("HOME_IPS", "");
const MIN_UPTIME_DAYS = Number(process.env.MIN_UPTIME_DAYS ?? 30);
const MIN_HEARTBEATS = Number(process.env.MIN_HEARTBEATS ?? 20);

function isOwnNode(peer) {
  const cloud = String(peer.cloud || "").toLowerCase();
  const name = String(peer.name || "").toLowerCase();
  const ip = String(peer.household_ip || "").toLowerCase();
  return (
    FLEET_CLOUDS.includes(cloud) ||
    FLEET_PREFIXES.some((p) => name.startsWith(p)) ||
    (ip !== "" && HOME_IPS.some((p) => ip.startsWith(p)))
  );
}
const DEFAULT_OUTPUT_ADOPTERS = path.resolve(__dirname, "../merkle/adopters.json");
const DEFAULT_OUTPUT_MERKLE = path.resolve(__dirname, "../merkle/adopter-merkle.json");

function fetchJSON(url) {
  return new Promise((resolve, reject) => {
    const client = url.startsWith("https") ? https : http;
    client.get(url, (res) => {
      if (res.statusCode < 200 || res.statusCode >= 300) {
        return reject(new Error(`HTTP ${res.statusCode} from ${url}`));
      }
      let raw = "";
      res.on("data", (chunk) => { raw += chunk; });
      res.on("end", () => {
        try {
          resolve(JSON.parse(raw));
        } catch (e) {
          reject(new Error(`Failed to parse JSON response: ${e.message}`));
        }
      });
    }).on("error", reject);
  });
}

function normalizeHardwareCommitment(hw) {
  if (!hw) return null;
  let clean = String(hw).trim().toLowerCase();
  if (!clean.startsWith("0x")) {
    clean = "0x" + clean;
  }
  if (!/^0x[a-f0-9]{64}$/.test(clean)) {
    return null;
  }
  return clean;
}

// The operator must be an EVM wallet the node owner controls. A SYNTHOS
// address derived from the node's Ed25519 key is NOT one (nobody holds a
// matching Base key), so it is never used as a fallback.
function resolveOperator(peer) {
  if (peer.payout_address && ethers.isAddress(peer.payout_address)) {
    return ethers.getAddress(peer.payout_address);
  }
  return null;
}

function resolveNodeType(peer) {
  const name = String(peer.name || "").toLowerCase();
  const kind = String(peer.kind || "").toLowerCase();
  const cloud = String(peer.cloud || "").toLowerCase();
  const mode = String(peer.mode || "").toLowerCase();

  if (name.includes("android") || kind.includes("android") || mode.includes("android")) {
    return "ANDROID";
  }
  if (cloud.includes("render") || cloud.includes("hosteons") || cloud.includes("cloud")) {
    return "CLOUD";
  }
  return "DESKTOP";
}

async function main() {
  const registryUrl = process.argv[2] || process.env.REGISTRY_URL || DEFAULT_REGISTRY_URL;
  console.log(`[Adopter Merkle] Fetching active nodes from ${registryUrl}...`);

  let data;
  try {
    data = await fetchJSON(registryUrl);
  } catch (err) {
    console.error(`[Adopter Merkle] Failed to fetch from registry: ${err.message}`);
    process.exit(1);
  }

  const peers = Array.isArray(data.peers) ? data.peers : [];
  console.log(`[Adopter Merkle] Retrieved ${peers.length} total registered peers.`);

  // Filter and deduplicate nodes
  // Requirements: valid hardware commitment (32 bytes), valid heartbeats (> 0)
  const candidateMap = new Map(); // hardwareCommitment -> best peer
  let skippedOwn = 0;
  let skippedNoHw = 0;
  let skippedUptime = 0;
  let skippedNoOperator = 0;

  for (const peer of peers) {
    if (isOwnNode(peer)) {
      skippedOwn++;
      continue;
    }
    const hw = normalizeHardwareCommitment(peer.hardware_commitment);
    if (!hw) {
      skippedNoHw++;
      continue;
    }
    const heartbeats = Number(peer.valid_heartbeats || 0);
    const uptimeMs = Number(peer.verified_uptime_ms || 0);
    if (heartbeats < MIN_HEARTBEATS || uptimeMs < MIN_UPTIME_DAYS * DAY_MS) {
      skippedUptime++;
      continue;
    }
    const operator = resolveOperator(peer);
    if (!operator) {
      skippedNoOperator++;
      continue;
    }

    const existing = candidateMap.get(hw);
    if (!existing || heartbeats > Number(existing.validHeartbeats || 0)) {
      candidateMap.set(hw, {
        peer,
        operator,
        hardwareCommitment: hw,
        nodeType: resolveNodeType(peer),
        validHeartbeats: heartbeats,
        verifiedUptimeMs: uptimeMs,
        name: peer.name || "unknown",
      });
    }
  }

  console.log(`[Adopter Merkle] Filtering summary:`);
  console.log(`   - Skipped (our own fleet/home nodes):                 ${skippedOwn}`);
  console.log(`   - Skipped (missing/invalid HW commitment):            ${skippedNoHw}`);
  console.log(`   - Skipped (under ${MIN_UPTIME_DAYS} days uptime or ${MIN_HEARTBEATS} heartbeats): ${skippedUptime}`);
  console.log(`   - Skipped (no EVM payout_address):                    ${skippedNoOperator}`);
  console.log(`   - Eligible outside operators:                         ${candidateMap.size}`);

  // An empty tree has a zero root, and set-adopter-merkle-root.js turns the
  // proof gate OFF for a zero root, which would let anyone register. Stop here.
  if (candidateMap.size === 0) {
    console.error("[Adopter Merkle] No eligible outside operators yet. Nothing written; do not set a Merkle root.");
    process.exit(2);
  }

  const adopters = [];
  const typeCounts = { DESKTOP: 0, ANDROID: 0, CLOUD: 0 };

  for (const entry of candidateMap.values()) {
    adopters.push({
      operator: entry.operator,
      hardwareCommitment: entry.hardwareCommitment,
      nodeType: entry.nodeType,
      _nodeName: entry.name,
      _heartbeats: entry.validHeartbeats,
      _uptimeHours: Math.round((entry.verifiedUptimeMs / (1000 * 3600)) * 10) / 10,
    });
  }

  // The contract pays one reward stream per operator wallet, so keep one
  // machine per wallet (the one with the most heartbeats).
  adopters.sort((a, b) => b._heartbeats - a._heartbeats);
  const seenOperators = new Set();
  const onePerOperator = [];
  for (const a of adopters) {
    if (seenOperators.has(a.operator)) continue;
    seenOperators.add(a.operator);
    onePerOperator.push(a);
  }
  adopters.length = 0;
  adopters.push(...onePerOperator);
  for (const a of adopters) typeCounts[a.nodeType] = (typeCounts[a.nodeType] || 0) + 1;

  // Sort deterministically by operator address
  adopters.sort((a, b) => a.operator.localeCompare(b.operator));

  const cleanAdopters = adopters.map(({ operator, hardwareCommitment, nodeType }) => ({
    operator,
    hardwareCommitment,
    nodeType,
  }));

  fs.mkdirSync(path.dirname(DEFAULT_OUTPUT_ADOPTERS), { recursive: true });
  fs.writeFileSync(
    DEFAULT_OUTPUT_ADOPTERS,
    JSON.stringify({ adopters: cleanAdopters }, null, 2) + "\n"
  );
  console.log(`[Adopter Merkle] Wrote ${cleanAdopters.length} adopter records to ${DEFAULT_OUTPUT_ADOPTERS}`);
  console.log(`[Adopter Merkle] Node type breakdown:`, typeCounts);

  // Run the Merkle tree builder
  const buildMerkle = require("./build-adopter-merkle.js");
  buildMerkle.main(DEFAULT_OUTPUT_ADOPTERS, DEFAULT_OUTPUT_MERKLE);
}

main().catch((err) => {
  console.error("[Adopter Merkle] Error:", err);
  process.exit(1);
});
