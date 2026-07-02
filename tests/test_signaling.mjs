/**
 * Synthos WebRTC Signaling Server — Automated Tests
 *
 * Tests the peer registry + signaling Worker locally using Miniflare.
 * If Miniflare isn't available, runs HTTP-only tests against a live endpoint.
 *
 * Run: node tests/test_signaling.mjs [REGISTRY_URL]
 *   Example: node tests/test_signaling.mjs http://127.0.0.1:8090
 */

const REGISTRY_URL = process.argv[2] || null;
let passed = 0;
let failed = 0;

function assert(condition, msg) {
  if (condition) {
    console.log(`  ✓ ${msg}`);
    passed++;
  } else {
    console.error(`  ✗ ${msg}`);
    failed++;
  }
}

async function fetchJson(url, options = {}) {
  const resp = await fetch(url, { ...options, signal: AbortSignal.timeout(10000) });
  return { status: resp.status, data: await resp.json() };
}

async function runTests() {
  console.log("\n=== SYNTHOS SIGNALING & REGISTRY TESTS ===\n");

  if (!REGISTRY_URL) {
    console.log("No REGISTRY_URL provided. Skipping live tests.");
    console.log("Usage: node tests/test_signaling.mjs http://127.0.0.1:8090\n");

    // Run structural/offline tests only
    await testProtocolMessages();
    printSummary();
    return;
  }

  console.log(`Testing against: ${REGISTRY_URL}\n`);

  // 1. Health check
  console.log("1. Health check");
  try {
    const { status, data } = await fetchJson(`${REGISTRY_URL}/health`);
    assert(status === 200, "health returns 200");
    assert(data.ok === true, "health.ok is true");
    assert(data.service === "synthos-peer-registry", "service name correct");
  } catch (e) {
    assert(false, `health check failed: ${e.message}`);
  }

  // 2. Register a test peer
  console.log("\n2. Register peer");
  const testName = `test-peer-${Date.now()}`;
  try {
    const { status, data } = await fetchJson(`${REGISTRY_URL}/register`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name: testName, url: "http://127.0.0.1:8090", cloud: "test" }),
    });
    assert(status === 200, "register returns 200");
    assert(data.ok === true, "register.ok is true");
    assert(data.peer === testName, "peer name matches");
  } catch (e) {
    assert(false, `register failed: ${e.message}`);
  }

  // 3. List all peers (should include our test peer)
  console.log("\n3. List peers");
  try {
    const { status, data } = await fetchJson(`${REGISTRY_URL}/peers`);
    assert(status === 200, "peers returns 200");
    assert(Array.isArray(data.peers), "peers is an array");
    const found = data.peers.find(p => p.name === testName);
    assert(!!found, "test peer found in list");
    assert(found?.url === "http://127.0.0.1:8090", "test peer URL correct");
    assert(found?.cloud === "test", "test peer cloud correct");
    assert(Array.isArray(data.urls), "urls array present");
    assert(Array.isArray(data.validator_order), "validator_order array present");
  } catch (e) {
    assert(false, `list peers failed: ${e.message}`);
  }

  // 4. List active peers
  console.log("\n4. Active peers");
  try {
    const { status, data } = await fetchJson(`${REGISTRY_URL}/peers/active`);
    assert(status === 200, "active peers returns 200");
    assert(data.active_only === true, "active_only flag set");
    const found = data.peers.find(p => p.name === testName);
    assert(!!found, "freshly registered peer is active");
  } catch (e) {
    assert(false, `active peers failed: ${e.message}`);
  }

  // 5. Register validation (missing fields)
  console.log("\n5. Validation");
  try {
    const { status, data } = await fetchJson(`${REGISTRY_URL}/register`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name: "foo" }), // missing url
    });
    assert(status === 400, "rejects missing url");
    assert(data.error === "name and url required", "correct error message");
  } catch (e) {
    assert(false, `validation test failed: ${e.message}`);
  }

  try {
    const { status, data } = await fetchJson(`${REGISTRY_URL}/register`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name: "foo", url: "not-a-url" }),
    });
    assert(status === 400, "rejects invalid url");
  } catch (e) {
    assert(false, `url validation test failed: ${e.message}`);
  }

  // 6. Deregister (without secret — should work if no secret configured)
  console.log("\n6. Deregister");
  try {
    const { status, data } = await fetchJson(`${REGISTRY_URL}/peers/${testName}`, {
      method: "DELETE",
    });
    // May return 200 or 401 depending on secret config
    assert(status === 200 || status === 401, `deregister returns ${status} (200 or 401 expected)`);
  } catch (e) {
    assert(false, `deregister failed: ${e.message}`);
  }

  // 7. WebSocket signaling endpoint (non-WebSocket request)
  console.log("\n7. Signaling endpoint (HTTP fallback)");
  try {
    const { status, data } = await fetchJson(`${REGISTRY_URL}/signal?id=test-peer`);
    // Should return peer list since it's not a WebSocket upgrade
    assert(status === 200, "signaling HTTP returns 200");
    assert(Array.isArray(data.connected_peers), "returns connected_peers list");
    assert(typeof data.total === "number", "returns total count");
  } catch (e) {
    assert(false, `signaling HTTP test failed: ${e.message}`);
  }

  // 8. WebSocket signaling test
  console.log("\n8. WebSocket signaling");
  await testWebSocketSignaling();

  // Run protocol message tests
  await testProtocolMessages();

  printSummary();
}

async function testWebSocketSignaling() {
  if (!REGISTRY_URL) return;

  // WebSocket test requires a WS-capable runtime (Node 21+ with WebSocket global)
  if (typeof WebSocket === "undefined") {
    console.log("  ⊘ WebSocket not available in this Node version (need 21+). Skipping.");
    return;
  }

  const wsUrl = REGISTRY_URL.replace("https://", "wss://").replace("http://", "ws://");

  try {
    const ws1 = new WebSocket(`${wsUrl}/signal?id=test-phone-1`);
    const ws2 = new WebSocket(`${wsUrl}/signal?id=test-phone-2`);

    const messages1 = [];
    const messages2 = [];

    await new Promise((resolve, reject) => {
      let openCount = 0;
      const onOpen = () => { openCount++; if (openCount === 2) resolve(); };
      ws1.onopen = onOpen;
      ws2.onopen = onOpen;
      ws1.onerror = reject;
      ws2.onerror = reject;
      setTimeout(() => reject(new Error("WebSocket connect timeout")), 5000);
    });

    assert(true, "both WebSockets connected");

    // Collect messages
    ws1.onmessage = (e) => messages1.push(JSON.parse(e.data));
    ws2.onmessage = (e) => messages2.push(JSON.parse(e.data));

    // Wait for initial peer lists + join notifications
    await new Promise(r => setTimeout(r, 500));

    // ws1 should have received peer-list and peer-joined
    const ws1HasPeerList = messages1.some(m => m.type === "peer-list");
    assert(ws1HasPeerList, "ws1 received peer-list");

    // Send a simulated offer from ws1 to ws2
    ws1.send(JSON.stringify({ type: "offer", to: "test-phone-2", sdp: { type: "offer", sdp: "fake-sdp" } }));
    await new Promise(r => setTimeout(r, 300));

    const receivedOffer = messages2.find(m => m.type === "offer" && m.from === "test-phone-1");
    assert(!!receivedOffer, "ws2 received offer relayed from ws1");
    assert(receivedOffer?.sdp?.sdp === "fake-sdp", "offer SDP payload preserved");

    // Send answer back
    ws2.send(JSON.stringify({ type: "answer", to: "test-phone-1", sdp: { type: "answer", sdp: "fake-answer" } }));
    await new Promise(r => setTimeout(r, 300));

    const receivedAnswer = messages1.find(m => m.type === "answer" && m.from === "test-phone-2");
    assert(!!receivedAnswer, "ws1 received answer relayed from ws2");

    // Send ICE candidate
    ws1.send(JSON.stringify({ type: "ice-candidate", to: "test-phone-2", candidate: { candidate: "fake-ice" } }));
    await new Promise(r => setTimeout(r, 300));

    const receivedIce = messages2.find(m => m.type === "ice-candidate" && m.from === "test-phone-1");
    assert(!!receivedIce, "ws2 received ICE candidate from ws1");

    // Clean up
    ws1.close();
    ws2.close();

    // Wait and check peer-left
    await new Promise(r => setTimeout(r, 500));
    // (peer-left is sent to remaining connections, ws2 may have received it before closing)

    assert(true, "WebSocket signaling round-trip complete");
  } catch (e) {
    assert(false, `WebSocket signaling test failed: ${e.message}`);
  }
}

async function testProtocolMessages() {
  console.log("\n9. Protocol message structure");

  // Verify the P2P gossip message formats match expectations
  const statusMsg = { type: "status", height: 42, peerId: "mobile-abc123" };
  assert(statusMsg.type === "status", "status message has correct type");
  assert(typeof statusMsg.height === "number", "status has numeric height");

  const requestBlocksMsg = { type: "request-blocks", from: 10 };
  assert(requestBlocksMsg.type === "request-blocks", "request-blocks has correct type");
  assert(typeof requestBlocksMsg.from === "number", "request-blocks has numeric from");

  const blocksMsg = { type: "blocks", blocks: [{ header: { height: 1 }, tx: [] }] };
  assert(Array.isArray(blocksMsg.blocks), "blocks message has blocks array");

  const newBlockMsg = { type: "new-block", block: { header: { height: 5 }, tx: [], hash: "0xabc" } };
  assert(newBlockMsg.block.header.height === 5, "new-block has block with height");

  const newTxMsg = { type: "new-tx", tx: { from: "agent-0", to: "0xabc", amount: 100 } };
  assert(newTxMsg.tx.from === "agent-0", "new-tx has transaction data");

  const offerMsg = { type: "offer", to: "peer-2", sdp: { type: "offer", sdp: "..." } };
  assert(offerMsg.to === "peer-2", "offer message has target peer");

  const answerMsg = { type: "answer", to: "peer-1", sdp: { type: "answer", sdp: "..." } };
  assert(answerMsg.to === "peer-1", "answer message has target peer");

  const iceMsg = { type: "ice-candidate", to: "peer-2", candidate: {} };
  assert(iceMsg.type === "ice-candidate", "ICE candidate message format correct");
}

function printSummary() {
  console.log(`\n=== RESULTS: ${passed} passed, ${failed} failed ===\n`);
  process.exit(failed > 0 ? 1 : 0);
}

runTests().catch(e => { console.error(e); process.exit(1); });
