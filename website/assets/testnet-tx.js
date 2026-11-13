// Builds, signs, and manages SYNTHOS L1 wallets in the browser:
// - BIP-39 Mnemonic Seed Phrases (12 and 24 words)
// - Ed25519 deterministic key derivation and address generation
// - AES-256-GCM + PBKDF2 password-encrypted keystore vaults for browser storage
// - Canonical transaction payload serialization matching internal/chain/core.go
(function (root) {
  "use strict";

  const enc = new TextEncoder();
  const dec = new TextDecoder();

  const hex = (bytes) => Array.from(new Uint8Array(bytes), (b) => b.toString(16).padStart(2, "0")).join("");
  const unhex = (s) => {
    s = (s || "").replace(/^0x/i, "").trim();
    if (!s || s.length % 2 !== 0 || !/^[0-9a-f]*$/i.test(s)) throw new Error("invalid hex string");
    const out = new Uint8Array(s.length / 2);
    for (let i = 0; i < out.length; i++) out[i] = parseInt(s.substr(i * 2, 2), 16);
    return out;
  };

  // Standard Ed25519 PKCS#8 ASN.1 prefix (16 bytes)
  const ED25519_PKCS8_PREFIX = new Uint8Array([
    0x30, 0x2e, 0x02, 0x01, 0x00, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x70, 0x04, 0x22, 0x04, 0x20
  ]);

  // Go encoding/json escapes <, >, & and line separators; match it byte-for-byte
  const goJSON = (v) =>
    JSON.stringify(v)
      .replace(/</g, "\\u003c")
      .replace(/>/g, "\\u003e")
      .replace(/&/g, "\\u0026")
      .replace(/\u2028/g, "\\u2028")
      .replace(/\u2029/g, "\\u2029");

  function checkUint(name, v) {
    if (!Number.isSafeInteger(v) || v < 0) throw new Error(name + " must be a non-negative whole number");
  }

  function signingPayload(tx) {
    const p = {
      version: 1,
      chain_id: tx.chain_id,
      from: tx.from,
      to: tx.to,
      amount: tx.amount,
      fee: tx.fee,
      nonce: tx.nonce,
      public_key: tx.public_key,
    };
    if (tx.asset_id) p.asset_id = tx.asset_id;
    if (tx.metadata && tx.metadata.length) {
      p.metadata = tx.metadata
        .slice()
        .sort((a, b) => (a.key < b.key ? -1 : a.key > b.key ? 1 : 0))
        .map((kv) => ({ key: kv.key, value: kv.value }));
    }
    return enc.encode(goJSON(p));
  }

  // RFC 8032 Ed25519 Curve Arithmetic for direct public key derivation from seed
  const _P = (1n << 255n) - 19n;
  function _modPow(b, e, m) {
    let r = 1n; b = b % m;
    while (e > 0n) {
      if (e & 1n) r = (r * b) % m;
      e >>= 1n;
      b = (b * b) % m;
    }
    return r;
  }
  function _inv(n) { return _modPow(n, _P - 2n, _P); }
  const _d = (-121665n * _inv(121666n)) % _P;
  const _I = _modPow(2n, (_P - 1n) / 4n, _P);

  function _xRecover(y) {
    const xx = (y * y - 1n) * _inv(_d * y * y + 1n);
    let x = _modPow(xx, (_P + 3n) / 8n, _P);
    if ((x * x - xx) % _P !== 0n) x = (x * _I) % _P;
    if (x % 2n !== 0n) x = (_P - x) % _P;
    return x;
  }

  const _By = (4n * _inv(5n)) % _P;
  const _Bx = _xRecover(_By);
  const _B = [_Bx, _By];

  function _edAdd(p1, p2) {
    const [x1, y1] = p1;
    const [x2, y2] = p2;
    const x3 = ((x1 * y2 + y1 * x2) * _inv(1n + _d * x1 * x2 * y1 * y2)) % _P;
    const y3 = ((y1 * y2 + x1 * x2) * _inv(1n - _d * x1 * x2 * y1 * y2)) % _P;
    return [(x3 + _P) % _P, (y3 + _P) % _P];
  }

  function _edMul(pt, n) {
    let r = [0n, 1n];
    let base = pt;
    while (n > 0n) {
      if (n & 1n) r = _edAdd(r, base);
      base = _edAdd(base, base);
      n >>= 1n;
    }
    return r;
  }

  async function ed25519PublicFromSeed(seed32) {
    const hash = new Uint8Array(await root.crypto.subtle.digest("SHA-512", seed32));
    hash[0] &= 248;
    hash[31] = (hash[31] & 127) | 64;
    let s = 0n;
    for (let i = 0; i < 32; i++) {
      s |= BigInt(hash[i]) << BigInt(8 * i);
    }
    const pt = _edMul(_B, s);
    const pub = new Uint8Array(32);
    let y = pt[1];
    for (let i = 0; i < 32; i++) {
      pub[i] = Number((y >> BigInt(8 * i)) & 0xffn);
    }
    if (pt[0] & 1n) pub[31] |= 0x80;
    return pub;
  }

  async function addressFromPublicKey(pub) {
    const sum = await root.crypto.subtle.digest("SHA-256", pub);
    return "0x" + hex(new Uint8Array(sum).slice(0, 20));
  }

  // Derive Ed25519 Web Crypto Key from a 32-byte seed
  async function keyFromSeedBytes(seed32) {
    if (seed32.length < 32) throw new Error("seed must be at least 32 bytes");
    const seed = seed32.slice(0, 32);
    const pkcs8 = new Uint8Array(48);
    pkcs8.set(ED25519_PKCS8_PREFIX, 0);
    pkcs8.set(seed, 16);

    const privateKey = await root.crypto.subtle.importKey("pkcs8", pkcs8, { name: "Ed25519" }, false, ["sign"]);
    const pub = await ed25519PublicFromSeed(seed);
    const address = await addressFromPublicKey(pub);

    return {
      pkcs8: hex(pkcs8),
      seed: "0x" + hex(seed),
      public_key: "0x" + hex(pub),
      address: address,
      privateKey: privateKey,
    };
  }

  // Generate BIP-39 mnemonic (12, 15, 18, 21, or 24 words)
  function generateMnemonic(wordCount = 12) {
    const ethers = root.ethers;
    if (!ethers || !ethers.Mnemonic) {
      throw new Error("ethers library with BIP-39 support required");
    }
    const entropyMap = { 12: 16, 15: 20, 18: 24, 21: 28, 24: 32 };
    const entropyBytes = entropyMap[wordCount] || 16;
    const entropy = root.crypto.getRandomValues(new Uint8Array(entropyBytes));
    const m = ethers.Mnemonic.fromEntropy(entropy);
    return m.phrase;
  }

  // Derive a full SYNTHOS wallet from a BIP-39 seed phrase
  async function fromMnemonic(mnemonic, passphrase = "") {
    const ethers = root.ethers;
    if (!ethers || !ethers.Mnemonic) {
      throw new Error("ethers library required for mnemonic derivation");
    }
    const clean = (mnemonic || "").trim().toLowerCase().replace(/\s+/g, " ");
    if (!ethers.Mnemonic.isValidMnemonic(clean)) {
      throw new Error("invalid BIP-39 seed phrase or checksum failed");
    }
    const m = ethers.Mnemonic.fromPhrase(clean, passphrase);
    const seedBytes = ethers.getBytes(m.computeSeed());

    // Use SLIP-0010 master derivation or direct 32-byte seed
    // Using HMAC-SHA512("ed25519 seed", seed) for standard Ed25519 root seed
    const keyMaterial = await root.crypto.subtle.importKey(
      "raw",
      enc.encode("ed25519 seed"),
      { name: "HMAC", hash: "SHA-512" },
      false,
      ["sign"]
    );
    const rootI = new Uint8Array(await root.crypto.subtle.sign("HMAC", keyMaterial, seedBytes));
    const edSeed = rootI.slice(0, 32);

    const wallet = await keyFromSeedBytes(edSeed);
    wallet.mnemonic = clean;
    wallet.wordCount = clean.split(" ").length;
    return wallet;
  }

  // Generate a random wallet with 12-word mnemonic
  async function generateKey(wordCount = 12) {
    try {
      const phrase = generateMnemonic(wordCount);
      return await fromMnemonic(phrase);
    } catch (_) {
      // Fallback to random raw Ed25519 if mnemonic provider unavailable
      const rawSeed = root.crypto.getRandomValues(new Uint8Array(32));
      return await keyFromSeedBytes(rawSeed);
    }
  }

  // Encrypt wallet data into an AES-256-GCM + PBKDF2 vault JSON string
  async function encryptVault(walletData, password) {
    if (!password) throw new Error("password required to encrypt wallet");
    const salt = root.crypto.getRandomValues(new Uint8Array(16));
    const iv = root.crypto.getRandomValues(new Uint8Array(12));

    const keyMaterial = await root.crypto.subtle.importKey("raw", enc.encode(password), "PBKDF2", false, [
      "deriveKey",
    ]);
    const aesKey = await root.crypto.subtle.deriveKey(
      { name: "PBKDF2", salt, iterations: 100000, hash: "SHA-256" },
      keyMaterial,
      { name: "AES-GCM", length: 256 },
      false,
      ["encrypt"]
    );

    const plaintext = enc.encode(typeof walletData === "string" ? walletData : JSON.stringify(walletData));
    const ciphertext = await root.crypto.subtle.encrypt({ name: "AES-GCM", iv }, aesKey, plaintext);

    return JSON.stringify({
      version: 1,
      cipher: "aes-256-gcm",
      kdf: "pbkdf2",
      iterations: 100000,
      salt: hex(salt),
      iv: hex(iv),
      ciphertext: hex(ciphertext),
      address: walletData.address || "",
    });
  }

  // Decrypt an AES-256-GCM vault JSON string with a password
  async function decryptVault(vaultJSON, password) {
    if (!password) throw new Error("password required to unlock wallet");
    const vault = typeof vaultJSON === "string" ? JSON.parse(vaultJSON) : vaultJSON;
    if (vault.cipher !== "aes-256-gcm" || vault.kdf !== "pbkdf2") {
      throw new Error("unsupported vault cipher or kdf");
    }

    const salt = unhex(vault.salt);
    const iv = unhex(vault.iv);
    const ciphertext = unhex(vault.ciphertext);

    const keyMaterial = await root.crypto.subtle.importKey("raw", enc.encode(password), "PBKDF2", false, [
      "deriveKey",
    ]);
    const aesKey = await root.crypto.subtle.deriveKey(
      { name: "PBKDF2", salt, iterations: vault.iterations || 100000, hash: "SHA-256" },
      keyMaterial,
      { name: "AES-GCM", length: 256 },
      false,
      ["decrypt"]
    );

    try {
      const decrypted = await root.crypto.subtle.decrypt({ name: "AES-GCM", iv }, aesKey, ciphertext);
      const str = dec.decode(decrypted);
      return JSON.parse(str);
    } catch (e) {
      throw new Error("incorrect password or corrupted keystore");
    }
  }

  async function signTransfer(wallet, { chainId, to, amount, fee, nonce, metadata }) {
    checkUint("amount", amount);
    checkUint("fee", fee);
    checkUint("nonce", nonce);
    checkUint("chain id", chainId);
    if (amount === 0) throw new Error("amount must be greater than zero");
    if (fee === 0) throw new Error("fee must be greater than zero");
    if (!/^0x[0-9a-f]{40}$/i.test(to)) throw new Error("recipient must be a 0x address with 40 hex characters");

    const tx = {
      chain_id: chainId,
      from: wallet.address,
      to: to.toLowerCase(),
      amount,
      fee,
      nonce,
      public_key: wallet.public_key,
      metadata: metadata || [],
    };
    const payload = signingPayload(tx);

    let key = wallet.privateKey;
    if (!key && wallet.pkcs8) {
      key = await root.crypto.subtle.importKey("pkcs8", unhex(wallet.pkcs8), { name: "Ed25519" }, false, ["sign"]);
    }
    if (!key) throw new Error("wallet has no signing key");

    const sig = await root.crypto.subtle.sign({ name: "Ed25519" }, key, payload);
    const id = await root.crypto.subtle.digest("SHA-256", payload);
    const out = {
      id: "0x" + hex(id),
      chain_id: tx.chain_id,
      from: tx.from,
      to: tx.to,
      amount,
      fee,
      nonce,
      public_key: tx.public_key,
      signature: "0x" + hex(sig),
      timestamp: Math.floor(Date.now() / 1000),
    };
    if (tx.metadata && tx.metadata.length) out.metadata = tx.metadata;
    return out;
  }

  root.SynthosTx = {
    signingPayload,
    addressFromPublicKey,
    generateMnemonic,
    fromMnemonic,
    keyFromSeedBytes,
    generateKey,
    encryptVault,
    decryptVault,
    signTransfer,
    hex,
    unhex,
  };
})(typeof window !== "undefined" ? window : globalThis);
