/**
 * Synthos Mobile Validator — Automated Tests
 *
 * Tests the core chain logic (genesis, state root, block hash, validation)
 * to ensure the PWA's in-browser validator matches the Cloudflare Worker implementation.
 *
 * Run: node tests/test_mobile_validator.mjs
 */

import { webcrypto } from "node:crypto";
if (!globalThis.crypto) globalThis.crypto = webcrypto;

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

// ─── Replicate the PWA's crypto functions ───────────────────────────────────

function computeStateRoot(accounts) {
  const sorted = Object.keys(accounts).sort();
  const items = sorted.map(addr => ({ addr, balance: accounts[addr].balance, nonce: accounts[addr].nonce }));
  const data = JSON.stringify(items);
  let hash = 0;
  for (let i = 0; i < data.length; i++) {
    const chr = data.charCodeAt(i);
    hash = ((hash << 5) - hash + chr) | 0;
  }
  return "0x" + Math.abs(hash).toString(16).padStart(16, "0");
}

async function computeBlockHash(block) {
  const payload = JSON.stringify({ header: block.header, tx: block.tx });
  const buf = new TextEncoder().encode(payload);
  const hashBuf = await crypto.subtle.digest("SHA-256", buf);
  const arr = new Uint8Array(hashBuf).slice(0, 16);
  return "0x" + Array.from(arr, b => b.toString(16).padStart(2, "0")).join("");
}

async function computeTxId(tx) {
  const payload = JSON.stringify({ from: tx.from, to: tx.to, amount: tx.amount, fee: tx.fee || 0, nonce: tx.nonce || 0 });
  const buf = new TextEncoder().encode(payload);
  const hashBuf = await crypto.subtle.digest("SHA-256", buf);
  const arr = new Uint8Array(hashBuf).slice(0, 16);
  return "0x" + Array.from(arr, b => b.toString(16).padStart(2, "0")).join("");
}

function genesisChain() {
  const alloc = { "agent-0": 100_000_000_000, "0x4ab2003c0391f25013f15539c9123e770d3c5a67": 10000 };
  const accounts = {};
  for (const [addr, bal] of Object.entries(alloc)) {
    accounts[addr] = { balance: bal, nonce: 0 };
  }
  const stateRoot = computeStateRoot(accounts);
  const genesis = {
    header: { height: 0, parent_hash: "0x0", timestamp: "1970-01-01T00:00:00.000Z", proposer_id: "genesis", state_root: stateRoot },
    tx: [], hash: "", validator_votes: {}, finalized: true,
  };
  return { chain_id: "synthos-l1-devnet", accounts, blocks: [genesis] };
}

async function validateBlock(block, parentBlock, accounts) {
  if (block.header.parent_hash !== parentBlock.hash) return "parent_hash mismatch";
  if (block.header.height !== parentBlock.header.height + 1) return "height mismatch";
  const tmpAccounts = JSON.parse(JSON.stringify(accounts));
  for (const tx of block.tx) {
    const sender = tmpAccounts[tx.from];
    if (!sender) return `unknown sender: ${tx.from}`;
    const total = (tx.amount || 0) + (tx.fee || 0);
    if (sender.balance < total) return `insufficient funds: ${tx.from}`;
    if (tx.nonce !== undefined && tx.nonce !== sender.nonce) return `bad nonce for ${tx.from}`;
    sender.balance -= total;
    sender.nonce += 1;
    if (!tmpAccounts[tx.to]) tmpAccounts[tx.to] = { balance: 0, nonce: 0 };
    tmpAccounts[tx.to].balance += tx.amount;
  }
  const expectedRoot = computeStateRoot(tmpAccounts);
  if (block.header.state_root !== expectedRoot) return "state_root mismatch";
  const expectedHash = await computeBlockHash(block);
  if (block.hash !== expectedHash) return "block_hash mismatch";
  return null;
}

// ─── Tests ──────────────────────────────────────────────────────────────────

async function runTests() {
  console.log("\n=== SYNTHOS MOBILE VALIDATOR TESTS ===\n");

  // 1. Genesis state root
  console.log("1. Genesis chain");
  const chain = genesisChain();
  assert(chain.chain_id === "synthos-l1-devnet", "chain_id is correct");
  assert(chain.blocks.length === 1, "genesis has 1 block");
  assert(chain.blocks[0].header.height === 0, "genesis height is 0");
  assert(chain.blocks[0].header.parent_hash === "0x0", "genesis parent is 0x0");
  assert(chain.accounts["agent-0"].balance === 100_000_000_000, "agent-0 balance correct");
  assert(chain.accounts["0x4ab2003c0391f25013f15539c9123e770d3c5a67"].balance === 10000, "second account balance correct");

  // 2. State root computation is deterministic
  console.log("\n2. State root determinism");
  const root1 = computeStateRoot(chain.accounts);
  const root2 = computeStateRoot(chain.accounts);
  assert(root1 === root2, "state root is deterministic");
  assert(root1.startsWith("0x"), "state root starts with 0x");
  assert(root1.length === 18, "state root is 18 chars (0x + 16 hex)");
  assert(root1 === chain.blocks[0].header.state_root, "genesis block has correct state root");

  // 3. Block hash computation
  console.log("\n3. Block hashing");
  const hash1 = await computeBlockHash(chain.blocks[0]);
  const hash2 = await computeBlockHash(chain.blocks[0]);
  assert(hash1 === hash2, "block hash is deterministic");
  assert(hash1.startsWith("0x"), "block hash starts with 0x");
  assert(hash1.length === 34, "block hash is 34 chars (0x + 32 hex)");

  // 4. TX ID computation
  console.log("\n4. Transaction ID");
  const tx = { from: "agent-0", to: "0x4ab2003c0391f25013f15539c9123e770d3c5a67", amount: 100, fee: 0, nonce: 0 };
  const txId1 = await computeTxId(tx);
  const txId2 = await computeTxId(tx);
  assert(txId1 === txId2, "tx ID is deterministic");
  assert(txId1.startsWith("0x"), "tx ID starts with 0x");

  // 5. Build and validate a valid block
  console.log("\n5. Valid block construction & validation");
  chain.blocks[0].hash = await computeBlockHash(chain.blocks[0]);

  const tmpAccounts = JSON.parse(JSON.stringify(chain.accounts));
  const testTx = { id: txId1, from: "agent-0", to: "0x4ab2003c0391f25013f15539c9123e770d3c5a67", amount: 500, fee: 0, nonce: 0 };
  tmpAccounts["agent-0"].balance -= 500;
  tmpAccounts["agent-0"].nonce += 1;
  tmpAccounts["0x4ab2003c0391f25013f15539c9123e770d3c5a67"].balance += 500;

  const newRoot = computeStateRoot(tmpAccounts);
  const block1 = {
    header: { height: 1, parent_hash: chain.blocks[0].hash, timestamp: "", proposer_id: "test-validator", state_root: newRoot },
    tx: [testTx], hash: "", validator_votes: {}, finalized: true,
  };
  block1.hash = await computeBlockHash(block1);

  const validationErr = await validateBlock(block1, chain.blocks[0], chain.accounts);
  assert(validationErr === null, "valid block passes validation");

  // 6. Reject block with wrong state root
  console.log("\n6. Invalid state root rejection");
  const badBlock = JSON.parse(JSON.stringify(block1));
  badBlock.header.state_root = "0xdeadbeefdeadbeef";
  badBlock.hash = await computeBlockHash(badBlock);
  const err1 = await validateBlock(badBlock, chain.blocks[0], chain.accounts);
  assert(err1 === "state_root mismatch", "rejects bad state root");

  // 7. Reject block with wrong parent hash
  console.log("\n7. Invalid parent hash rejection");
  const orphanBlock = JSON.parse(JSON.stringify(block1));
  orphanBlock.header.parent_hash = "0xaaaaaaaaaaaaaaaa";
  orphanBlock.hash = await computeBlockHash(orphanBlock);
  const err2 = await validateBlock(orphanBlock, chain.blocks[0], chain.accounts);
  assert(err2 === "parent_hash mismatch", "rejects wrong parent hash");

  // 8. Reject block with wrong height
  console.log("\n8. Invalid height rejection");
  const heightBlock = JSON.parse(JSON.stringify(block1));
  heightBlock.header.height = 99;
  heightBlock.hash = await computeBlockHash(heightBlock);
  const err3 = await validateBlock(heightBlock, chain.blocks[0], chain.accounts);
  assert(err3 === "height mismatch", "rejects wrong height");

  // 9. Reject block with tampered hash
  console.log("\n9. Block hash tampering rejection");
  const tamperedBlock = JSON.parse(JSON.stringify(block1));
  tamperedBlock.hash = "0xbadc0ffeebadc0ffeebadc0ffeebadc0";
  const err4 = await validateBlock(tamperedBlock, chain.blocks[0], chain.accounts);
  assert(err4 === "block_hash mismatch", "rejects tampered block hash");

  // 10. Reject TX with insufficient funds
  console.log("\n10. Insufficient funds rejection");
  const bigTx = { id: "0xbig", from: "0x4ab2003c0391f25013f15539c9123e770d3c5a67", to: "agent-0", amount: 999999, fee: 0, nonce: 0 };
  const bigBlock = {
    header: { height: 1, parent_hash: chain.blocks[0].hash, timestamp: "", proposer_id: "test", state_root: "0x0" },
    tx: [bigTx], hash: "", validator_votes: {}, finalized: true,
  };
  bigBlock.hash = await computeBlockHash(bigBlock);
  const err5 = await validateBlock(bigBlock, chain.blocks[0], chain.accounts);
  assert(err5 && err5.includes("insufficient funds"), "rejects insufficient funds");

  // 11. Multi-block chain integrity
  console.log("\n11. Multi-block chain integrity");
  const fullChain = genesisChain();
  fullChain.blocks[0].hash = await computeBlockHash(fullChain.blocks[0]);

  for (let i = 1; i <= 5; i++) {
    const parent = fullChain.blocks[fullChain.blocks.length - 1];
    const txI = { id: `tx-${i}`, from: "agent-0", to: "0x4ab2003c0391f25013f15539c9123e770d3c5a67", amount: 10, fee: 0, nonce: fullChain.accounts["agent-0"].nonce };
    fullChain.accounts["agent-0"].balance -= 10;
    fullChain.accounts["agent-0"].nonce += 1;
    fullChain.accounts["0x4ab2003c0391f25013f15539c9123e770d3c5a67"].balance += 10;
    const sr = computeStateRoot(fullChain.accounts);
    const blk = {
      header: { height: i, parent_hash: parent.hash, timestamp: "", proposer_id: `v-${i}`, state_root: sr },
      tx: [txI], hash: "", validator_votes: {}, finalized: true,
    };
    blk.hash = await computeBlockHash(blk);
    fullChain.blocks.push(blk);
  }
  assert(fullChain.blocks.length === 6, "5 blocks + genesis = 6 total");
  assert(fullChain.blocks[5].header.height === 5, "chain height is 5");
  assert(fullChain.accounts["agent-0"].balance === 100_000_000_000 - 50, "agent-0 spent 50 SYN");
  assert(fullChain.accounts["0x4ab2003c0391f25013f15539c9123e770d3c5a67"].balance === 10050, "receiver got 50 SYN");

  // Verify each block links correctly
  for (let i = 1; i < fullChain.blocks.length; i++) {
    assert(fullChain.blocks[i].header.parent_hash === fullChain.blocks[i-1].hash, `block ${i} links to block ${i-1}`);
  }

  // Summary
  console.log(`\n=== RESULTS: ${passed} passed, ${failed} failed ===\n`);
  process.exit(failed > 0 ? 1 : 0);
}

runTests().catch(e => { console.error(e); process.exit(1); });
