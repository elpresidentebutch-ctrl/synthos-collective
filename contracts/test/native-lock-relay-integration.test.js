const http = require("http");
const os = require("os");
const path = require("path");
const fs = require("fs");
const { expect } = require("chai");
const { ethers } = require("hardhat");
const { deploySynWithBridge } = require("./helpers/syn");
const { pollOnce } = require("../scripts/relay-native-locks-to-syn-mint");
const { WRAPPED_SYN_UNIT_SCALE, computeSourceEventId } = require("../scripts/lib/native-lock-relay");

const LOCK_ADDRESS = "0x000000000000000000000000000000000000b07d";

/**
 * A minimal stand-in for the native chain's GET /bridge/events endpoint.
 * `events` can be swapped out between requests via the returned `setEvents`
 * so a single server can be reused across a test's multiple polls.
 */
function startMockNativeRpc(initialEvents) {
  let events = initialEvents;
  const server = http.createServer((req, res) => {
    if (req.url.startsWith("/bridge/events")) {
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ ok: true, events }));
      return;
    }
    res.writeHead(404);
    res.end("not found");
  });
  return new Promise((resolve) => {
    server.listen(0, "127.0.0.1", () => {
      const { port } = server.address();
      resolve({
        url: `http://127.0.0.1:${port}`,
        setEvents: (next) => {
          events = next;
        },
        close: () => new Promise((r) => server.close(r)),
      });
    });
  });
}

function tempStateFile() {
  return path.join(os.tmpdir(), `native-lock-mint-state-${Date.now()}-${Math.random().toString(16).slice(2)}.json`);
}

function nativeLockEvent(overrides = {}) {
  return {
    id: "native-lock-int-1",
    type: "bridge_lock_native",
    amount: 500,
    asset_id: "syn",
    recipient: LOCK_ADDRESS,
    destination_chain_id: "", // filled in per-test with the real chain id
    destination_recipient: "",
    ...overrides,
  };
}

describe("native lock -> SYN mint relayer (integration)", function () {
  async function deployFixture() {
    const [owner, relayerA, relayerB, recipient, treasury] = await ethers.getSigners();
    const { syn, minter } = await deploySynWithBridge(treasury, [owner, relayerA, relayerB], 1);
    const network = await ethers.provider.getNetwork();
    return { owner, relayerA, relayerB, recipient, syn, minter, evmChainId: network.chainId.toString() };
  }

  it("mints wrapped SYN for a well-formed native lock, in 18-decimal units", async function () {
    const { minter, syn, recipient, evmChainId } = await deployFixture();
    const server = await startMockNativeRpc([]);
    try {
      const event = nativeLockEvent({
        destination_chain_id: evmChainId,
        destination_recipient: recipient.address,
      });
      server.setEvents([event]);

      const stateFile = tempStateFile();
      const results = await pollOnce({
        nativeRpcUrl: server.url,
        lockAddress: LOCK_ADDRESS,
        evmChainId,
        minter,
        processed: new Set(),
        stateFile,
      });

      expect(results).to.deep.equal(["minted"]);
      expect(await syn.balanceOf(recipient.address)).to.equal(500n * WRAPPED_SYN_UNIT_SCALE);
      expect(JSON.parse(fs.readFileSync(stateFile, "utf8"))).to.include(event.id);
    } finally {
      await server.close();
    }
  });

  it("ignores a lock whose funds did not actually reach the configured escrow address", async function () {
    const { minter, syn, recipient, evmChainId } = await deployFixture();
    const server = await startMockNativeRpc([]);
    try {
      const fakeLock = nativeLockEvent({
        id: "native-lock-fake",
        recipient: "0x000000000000000000000000000000000000dead", // NOT the configured lock address
        destination_chain_id: evmChainId,
        destination_recipient: recipient.address,
      });
      server.setEvents([fakeLock]);

      const results = await pollOnce({
        nativeRpcUrl: server.url,
        lockAddress: LOCK_ADDRESS,
        evmChainId,
        minter,
        processed: new Set(),
        stateFile: tempStateFile(),
      });

      expect(results).to.deep.equal(["skipped:not-mintable"]);
      expect(await syn.balanceOf(recipient.address)).to.equal(0n);
    } finally {
      await server.close();
    }
  });

  it("does not re-mint a lock a different relayer instance already pushed to quorum", async function () {
    const { minter, syn, recipient, evmChainId, relayerA } = await deployFixture();
    const server = await startMockNativeRpc([]);
    try {
      const event = nativeLockEvent({
        id: "native-lock-shared",
        destination_chain_id: evmChainId,
        destination_recipient: recipient.address,
      });
      server.setEvents([event]);

      // Simulate a second relayer instance already having minted this via
      // the exact same deterministic sourceEventId (this is what makes
      // independent relayers converge on the same approveMint call).
      const sourceEventId = computeSourceEventId(event.id);
      const amount = 500n * WRAPPED_SYN_UNIT_SCALE;
      await minter.connect(relayerA).approveMint(sourceEventId, recipient.address, amount);
      expect(await syn.balanceOf(recipient.address)).to.equal(amount);

      // This relayer instance has never seen this event before (fresh
      // `processed` set), so it will try -- and must hit "already
      // processed" from the contract itself, not double-mint.
      const results = await pollOnce({
        nativeRpcUrl: server.url,
        lockAddress: LOCK_ADDRESS,
        evmChainId,
        minter,
        processed: new Set(),
        stateFile: tempStateFile(),
      });

      expect(results[0]).to.match(/^done:/);
      expect(await syn.balanceOf(recipient.address)).to.equal(amount); // unchanged, no double-mint
    } finally {
      await server.close();
    }
  });

  it("does not re-submit a lock it has already processed itself, across polls", async function () {
    const { minter, syn, recipient, evmChainId } = await deployFixture();
    const server = await startMockNativeRpc([]);
    try {
      const event = nativeLockEvent({
        id: "native-lock-repoll",
        destination_chain_id: evmChainId,
        destination_recipient: recipient.address,
      });
      server.setEvents([event]);
      const stateFile = tempStateFile();
      const processed = new Set();

      const first = await pollOnce({ nativeRpcUrl: server.url, lockAddress: LOCK_ADDRESS, evmChainId, minter, processed, stateFile });
      expect(first).to.deep.equal(["minted"]);

      // Same event still returned by the native RPC on the next poll (it
      // doesn't disappear from the endpoint); the in-memory `processed`
      // set from the first poll must skip it without calling the contract
      // again.
      const second = await pollOnce({ nativeRpcUrl: server.url, lockAddress: LOCK_ADDRESS, evmChainId, minter, processed, stateFile });
      expect(second).to.deep.equal([]);
      expect(await syn.balanceOf(recipient.address)).to.equal(500n * WRAPPED_SYN_UNIT_SCALE);
    } finally {
      await server.close();
    }
  });

  it("requires real relayer quorum before minting when threshold > 1", async function () {
    const [owner, relayerA, relayerB, recipient, treasury] = await ethers.getSigners();
    const { syn, minter } = await deploySynWithBridge(treasury, [relayerA, relayerB], 2);
    const network = await ethers.provider.getNetwork();
    const evmChainId = network.chainId.toString();

    const server = await startMockNativeRpc([]);
    try {
      const event = nativeLockEvent({
        id: "native-lock-quorum",
        destination_chain_id: evmChainId,
        destination_recipient: recipient.address,
      });
      server.setEvents([event]);

      // This relayer instance signs as relayerA only -- one vote, below
      // the threshold of 2, so approveMint succeeds (an approval is
      // recorded) but nothing mints yet.
      const minterAsRelayerA = minter.connect(relayerA);
      const results = await pollOnce({
        nativeRpcUrl: server.url,
        lockAddress: LOCK_ADDRESS,
        evmChainId,
        minter: minterAsRelayerA,
        processed: new Set(),
        stateFile: tempStateFile(),
      });
      expect(results).to.deep.equal(["minted"]); // our own call succeeded
      expect(await syn.balanceOf(recipient.address)).to.equal(0n); // but quorum not yet reached

      const sourceEventId = computeSourceEventId(event.id);
      await minter.connect(relayerB).approveMint(sourceEventId, recipient.address, 500n * WRAPPED_SYN_UNIT_SCALE);
      expect(await syn.balanceOf(recipient.address)).to.equal(500n * WRAPPED_SYN_UNIT_SCALE);
    } finally {
      await server.close();
    }
  });
});
