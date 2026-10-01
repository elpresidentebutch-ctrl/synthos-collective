// Builds and signs SYNTHOS transactions in the browser, byte-for-byte the
// way internal/chain/core.go does (Tx.signingBytes, Tx.Sign):
//   payload = JSON {version, chain_id, from, to, amount, fee, nonce,
//             public_key[, asset_id][, metadata sorted by key]}
//   signature = Ed25519(payload), id = sha256(payload)
// Used by testnet.html; also loadable in Node for tests (globalThis).
(function (root) {
  "use strict";
  const enc = new TextEncoder();
  const hex = (bytes) => Array.from(new Uint8Array(bytes), (b) => b.toString(16).padStart(2, "0")).join("");
  const unhex = (s) => {
    s = s.replace(/^0x/, "");
    if (!/^([0-9a-f]{2})*$/i.test(s)) throw new Error("bad hex");
    const out = new Uint8Array(s.length / 2);
    for (let i = 0; i < out.length; i++) out[i] = parseInt(s.substr(i * 2, 2), 16);
    return out;
  };
  // Go's encoding/json escapes <, > and & inside strings; match it so the
  // signed bytes are identical for any metadata text.
  const goJSON = (v) => JSON.stringify(v).replace(/</g, "\\u003c").replace(/>/g, "\\u003e").replace(/&/g, "\\u0026")
    .replace(/\u2028/g, "\\u2028").replace(/\u2029/g, "\\u2029");

  function checkUint(name, v) {
    if (!Number.isSafeInteger(v) || v < 0) throw new Error(name + " must be a whole number");
  }

  function signingPayload(tx) {
    const p = {
      version: 1, chain_id: tx.chain_id, from: tx.from, to: tx.to,
      amount: tx.amount, fee: tx.fee, nonce: tx.nonce, public_key: tx.public_key,
    };
    if (tx.asset_id) p.asset_id = tx.asset_id;
    if (tx.metadata && tx.metadata.length) {
      p.metadata = tx.metadata.slice().sort((a, b) => (a.key < b.key ? -1 : a.key > b.key ? 1 : 0))
        .map((kv) => ({ key: kv.key, value: kv.value }));
    }
    return enc.encode(goJSON(p));
  }

  async function addressFromPublicKey(pub) {
    const sum = await root.crypto.subtle.digest("SHA-256", pub);
    return "0x" + hex(new Uint8Array(sum).slice(0, 20));
  }

  async function generateKey() {
    const kp = await root.crypto.subtle.generateKey({ name: "Ed25519" }, true, ["sign", "verify"]);
    const pkcs8 = new Uint8Array(await root.crypto.subtle.exportKey("pkcs8", kp.privateKey));
    const spki = new Uint8Array(await root.crypto.subtle.exportKey("spki", kp.publicKey));
    const pub = spki.slice(spki.length - 32);
    return { pkcs8: hex(pkcs8), public_key: "0x" + hex(pub), address: await addressFromPublicKey(pub) };
  }

  async function signTransfer(wallet, { chainId, to, amount, fee, nonce, metadata }) {
    checkUint("amount", amount); checkUint("fee", fee); checkUint("nonce", nonce); checkUint("chain id", chainId);
    if (amount === 0) throw new Error("amount must be greater than zero");
    if (fee === 0) throw new Error("fee must be greater than zero");
    if (!/^0x[0-9a-f]{40}$/i.test(to)) throw new Error("recipient must be a 0x address with 40 hex characters");
    const tx = {
      chain_id: chainId, from: wallet.address, to: to.toLowerCase(), amount, fee, nonce,
      public_key: wallet.public_key, metadata: metadata || [],
    };
    const payload = signingPayload(tx);
    const key = await root.crypto.subtle.importKey("pkcs8", unhex(wallet.pkcs8), { name: "Ed25519" }, false, ["sign"]);
    const sig = await root.crypto.subtle.sign({ name: "Ed25519" }, key, payload);
    const id = await root.crypto.subtle.digest("SHA-256", payload);
    const out = {
      id: "0x" + hex(id), chain_id: tx.chain_id, from: tx.from, to: tx.to, amount, fee, nonce,
      public_key: tx.public_key, signature: "0x" + hex(sig), timestamp: Math.floor(Date.now() / 1000),
    };
    if (tx.metadata.length) out.metadata = tx.metadata;
    return out;
  }

  root.SynthosTx = { signingPayload, addressFromPublicKey, generateKey, signTransfer, hex, unhex };
})(typeof window !== "undefined" ? window : globalThis);
