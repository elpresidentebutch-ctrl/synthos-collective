const fs = require("fs");
const path = require("path");
const https = require("https");
const http = require("http");
const { ethers } = require("ethers");

const DEFAULT_REGISTRY_URL = "https://82-29-54-7.sslip.io/api/nodes";
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

function resolveOperator(peer) {
  if (peer.payout_address && ethers.isAddress(peer.payout_address)) {
    return ethers.getAddress(peer.payout_address);
  }
  if (peer.public_key) {
    let pub = String(peer.public_key).trim().toLowerCase();
    if (pub.startsWith("0x")) pub = pub.slice(2);
    if (/^[a-f0-9]{64}$/.test(pub)) {
      const hash = ethers.sha256("0x" + pub);
      return ethers.getAddress(hash.slice(0, 42));
    }
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
  let skippedNoHw = 0;
  let skippedNoHeartbeats = 0;
  let skippedNoOperator = 0;

  for (const peer of peers) {
    const hw = normalizeHardwareCommitment(peer.hardware_commitment);
    if (!hw) {
      skippedNoHw++;
      continue;
    }
    const heartbeats = Number(peer.valid_heartbeats || 0);
    if (heartbeats <= 0) {
      skippedNoHeartbeats++;
      continue;
    }
    const operator = resolveOperator(peer);
    if (!operator) {
      skippedNoOperator++;
      continue;
    }

    const existing = candidateMap.get(hw);
    if (!existing || heartbeats > Number(existing.valid_heartbeats || 0)) {
      candidateMap.set(hw, {
        peer,
        operator,
        hardwareCommitment: hw,
        nodeType: resolveNodeType(peer),
        validHeartbeats: heartbeats,
        verifiedUptimeMs: Number(peer.verified_uptime_ms || 0),
        name: peer.name || "unknown",
      });
    }
  }

  console.log(`[Adopter Merkle] Filtering summary:`);
  console.log(`   - Skipped (missing/invalid HW commitment): ${skippedNoHw}`);
  console.log(`   - Skipped (0 valid heartbeats):            ${skippedNoHeartbeats}`);
  console.log(`   - Skipped (unresolvable operator address):  ${skippedNoOperator}`);
  console.log(`   - Unique verified hardware machines:       ${candidateMap.size}`);

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
    typeCounts[entry.nodeType] = (typeCounts[entry.nodeType] || 0) + 1;
  }

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
  buildMerkle.main();
}

main().catch((err) => {
  console.error("[Adopter Merkle] Error:", err);
  process.exit(1);
});
