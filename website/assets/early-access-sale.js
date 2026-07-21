(function () {
  const TREASURY_WALLET = "0x5d6f8FbAAB199E788ed9Cfcb3F7Fe2ac9c0450d2";
  const HARDHAT_CHAIN_ID = 31337;
  const ETHERS_CDN = "https://cdn.jsdelivr.net/npm/ethers@6.13.4/dist/ethers.umd.min.js";
  const SYNTHOS_NATIVE_WALLET_STORAGE_KEY = "synthos.nativeWallet.v1";
  const SYNTHOS_NATIVE_WALLET_BACKUP_CONFIRMED_KEY = "synthos.nativeWallet.backupConfirmed.v1";

  const SALE_ABI = [
    "function USD_PRICE_PER_SYN_18() view returns (uint256)",
    "function treasury() view returns (address)",
    "function totalSynSold() view returns (uint256)",
    "function maxSaleAllocation() view returns (uint256)",
    "function minSynPurchase() view returns (uint256)",
    "function maxSynPerWallet() view returns (uint256)",
    "function purchasedByWallet(address) view returns (uint256)",
    "function paymentAssets(address) view returns (bool enabled,uint8 decimals,uint256 usdPricePerToken18)",
    "function quoteTokenPurchase(address paymentAsset,uint256 paymentAmount) view returns (uint256 synAmount,uint256 usdValue18)",
    "function quoteNativePurchase(uint256 nativeAmount) view returns (uint256 synAmount,uint256 usdValue18)",
    "function buyWithToken(address paymentAsset,uint256 paymentAmount,address beneficiary) returns (uint256)",
    "function buyWithNative(address beneficiary) payable returns (uint256)",
  ];

  const ERC20_ABI = [
    "function decimals() view returns (uint8)",
    "function allowance(address owner,address spender) view returns (uint256)",
    "function approve(address spender,uint256 amount) returns (bool)",
  ];

  const COMPLIANCE_ABI = [
    "function eligibleToReceive(address account,uint8 expectedCategory) view returns (bool)",
    "function communitySelfRegistrationOpen() view returns (bool)",
    "function selfRegisterCommunity(bytes32 disclosureHash,bytes32 jurisdictionHash)",
  ];

  const defaultConfig = {
    chainId: null,
    chainName: "SYNTHOS",
    rpcUrls: ["https://rpc.ishamwilliamsblockchains.com"],
    saleContract: "",
    complianceRegistry: "",
    treasuryWallet: TREASURY_WALLET,
    tokenPriceUsd: "0.10",
    activeTrancheSyn: "250,000,000",
    maxTrancheUsd: "$25,000,000",
    communitySourceBucket: "COMMUNITY_EARLY_ADOPTER_CAMPAIGNS",
    paymentRails: false,
    paymentIntentUrl: "/api/early-access/payment-intents",
    disclosureText: "SYNTHOS early access disclosure v1",
    jurisdictionCode: "US",
    assets: [
      { symbol: "USDC", address: "", decimals: 6, usdPrice: "1.00" },
      { symbol: "USDT", address: "", decimals: 6, usdPrice: "1.00" },
      { symbol: "WETH", address: "", decimals: 18, usdPrice: "" },
      { symbol: "WBTC", address: "", decimals: 8, usdPrice: "" },
      { symbol: "ETH", native: true, decimals: 18, usdPrice: "" },
    ],
  };

  let config = Object.assign({}, defaultConfig, window.SYNTHOS_EARLY_ACCESS_CONFIG || {});
  config.assets = (window.SYNTHOS_EARLY_ACCESS_CONFIG && window.SYNTHOS_EARLY_ACCESS_CONFIG.assets) || defaultConfig.assets;

  const state = {
    account: "",
    provider: null,
    signer: null,
    sale: null,
    compliance: null,
    paymentIntent: null,
    synthosWallet: loadStoredSynthosWallet(),
    synthosWalletBackupConfirmed: loadBackupConfirmation(),
  };

  const root = document.querySelector("[data-synthos-early-access]");
  if (!root) return;

  function html(value) {
    return String(value ?? "").replace(/[&<>"']/g, (char) => ({
      "&": "&amp;",
      "<": "&lt;",
      ">": "&gt;",
      "\"": "&quot;",
      "'": "&#39;",
    })[char]);
  }

  function setStatus(message, tone = "muted") {
    const node = root.querySelector("[data-sale-status]");
    if (!node) return;
    node.textContent = message;
    node.dataset.tone = tone;
  }

  function injectStyles() {
    if (document.getElementById("synthos-early-access-styles")) return;
    const style = document.createElement("style");
    style.id = "synthos-early-access-styles";
    style.textContent = `
      .synthos-early-access{display:grid;gap:20px;max-width:1080px;margin:0 auto;padding:40px 20px;color:#f5f8ff}
      .synthos-early-access h1{margin:0;font-size:clamp(2.4rem,6vw,5rem);line-height:.95;letter-spacing:0;text-transform:uppercase}
      .synthos-early-access p{color:#aeb9c8;line-height:1.65}
      .synthos-eyebrow{margin:0 0 10px;color:#31d39f;font-size:.78rem;font-weight:800;letter-spacing:.12em;text-transform:uppercase}
      .synthos-sale-facts{display:grid;grid-template-columns:repeat(5,minmax(0,1fr));gap:10px;margin:0}
      .synthos-sale-facts div,.synthos-sale-box{border:1px solid rgba(255,255,255,.14);border-radius:8px;background:rgba(10,14,22,.72)}
      .synthos-sale-facts div{padding:13px;min-width:0}
      .synthos-sale-facts dt{color:#7bdcff;font-size:.74rem;font-weight:800;text-transform:uppercase}
      .synthos-sale-facts dd{margin:5px 0 0;overflow-wrap:anywhere;font-family:ui-monospace,SFMono-Regular,Consolas,monospace}
      .synthos-sale-box{display:grid;gap:14px;padding:18px}
      .synthos-sale-box label{display:grid;gap:7px;color:#aeb9c8;font-size:.82rem;font-weight:800;text-transform:uppercase}
      .synthos-sale-box input,.synthos-sale-box select{min-height:44px;border:1px solid rgba(255,255,255,.16);border-radius:8px;padding:10px 12px;color:#f5f8ff;background:#07090d;font:inherit}
      .synthos-sale-quote{min-height:48px;display:flex;align-items:center;border:1px solid rgba(49,211,159,.35);border-radius:8px;padding:12px;color:#31d39f;background:rgba(49,211,159,.08);font-weight:800}
      .synthos-native-wallet{display:none;gap:10px;border:1px solid rgba(49,211,159,.28);border-radius:8px;padding:12px;background:rgba(49,211,159,.07)}
      .synthos-native-wallet[data-open=true]{display:grid}
      .synthos-native-wallet strong{color:#31d39f}
      .synthos-native-wallet code{overflow-wrap:anywhere;color:#f5f8ff}
      .synthos-native-wallet small{color:#f2c45d;line-height:1.45}
      .synthos-native-wallet label{display:flex!important;align-items:flex-start;gap:9px;color:#f5f8ff!important;font-size:.9rem!important;font-weight:750!important;text-transform:none!important}
      .synthos-native-wallet input[type=checkbox]{min-height:auto!important;width:18px;height:18px;margin-top:2px;accent-color:#31d39f}
      .synthos-mini-actions{display:flex;flex-wrap:wrap;gap:8px}
      .synthos-mini-actions button{min-height:36px!important;border:1px solid rgba(255,255,255,.14)!important;border-radius:7px!important;padding:8px 10px!important;background:rgba(255,255,255,.08)!important;color:#f5f8ff!important;font-size:.84rem!important}
      .synthos-sale-actions{display:flex;flex-wrap:wrap;gap:10px}
      .synthos-sale-actions button{min-height:44px;border:0;border-radius:8px;padding:11px 16px;font:inherit;font-weight:850;cursor:pointer}
      .synthos-sale-actions button:first-child{background:#31d39f;color:#04130e}
      .synthos-sale-actions button:last-child{background:#7bdcff;color:#031018}
      .synthos-sale-actions button:disabled{opacity:.45;cursor:not-allowed}
      .synthos-payment-instructions{display:none;gap:8px;border:1px solid rgba(123,220,255,.28);border-radius:8px;padding:12px;background:rgba(123,220,255,.07)}
      .synthos-payment-instructions[data-open=true]{display:grid}
      .synthos-payment-instructions code{overflow-wrap:anywhere;color:#f5f8ff}
      [data-sale-status]{margin:0;color:#aeb9c8}
      [data-sale-status][data-tone=warn]{color:#f2c45d}
      [data-sale-status][data-tone=ok]{color:#31d39f}
      @media (max-width:860px){.synthos-sale-facts{grid-template-columns:1fr 1fr}.synthos-early-access h1{font-size:2.6rem}}
      @media (max-width:560px){.synthos-sale-facts{grid-template-columns:1fr}.synthos-sale-actions{display:grid}}
    `;
    document.head.appendChild(style);
  }

  function ethersReady() {
    return new Promise((resolve, reject) => {
      if (window.ethers) {
        resolve(window.ethers);
        return;
      }
      const script = document.createElement("script");
      script.src = ETHERS_CDN;
      script.async = true;
      script.onload = () => resolve(window.ethers);
      script.onerror = () => reject(new Error("Unable to load wallet library"));
      document.head.appendChild(script);
    });
  }

  function activeAsset() {
    const select = root.querySelector("[data-sale-asset]");
    return config.assets[Number(select?.value || 0)] || config.assets[0];
  }

  function validAddress(value) {
    return typeof value === "string" && /^0x[a-fA-F0-9]{40}$/.test(value);
  }

  function normalizeConfig(nextConfig) {
    config = Object.assign({}, defaultConfig, config, nextConfig || {});
    config.assets = Array.isArray(config.assets) && config.assets.length ? config.assets : defaultConfig.assets;
    if (config.maxTrancheValueUsd && !config.maxTrancheUsd) {
      config.maxTrancheUsd = `$${Number(config.maxTrancheValueUsd).toLocaleString()}`;
    }
    config.paymentRails = Boolean(config.paymentRails || config.paymentIntentUrl);
  }

  async function loadBackendConfig() {
    const baseURL = window.SYNTHOS_API_URL || window.SYNTHOS_BACKEND_URL || config.apiURL || "";
    if (!baseURL) return;
    try {
      const response = await fetch(`${String(baseURL).replace(/\/$/, "")}/api/early-access/config`, {
        headers: { Accept: "application/json" },
      });
      if (!response.ok) return;
      const body = await response.json();
      if (body && body.ok !== false) {
        normalizeConfig(body);
      }
    } catch (error) {
      console.warn("SYNTHOS early access config fetch failed", error);
    }
  }

  function render() {
    injectStyles();
    root.innerHTML = `
      <section class="synthos-early-access">
        <div>
          <p class="synthos-eyebrow">Early Access</p>
          <h1>SYN early adopter tranche</h1>
          <p>Automatic crypto-only purchase flow. Eligible wallets receive SYN in the same on-chain transaction.</p>
        </div>
        <dl class="synthos-sale-facts">
          <div><dt>Price</dt><dd>$${html(config.tokenPriceUsd)} per SYN</dd></div>
          <div><dt>Active Tranche</dt><dd>${html(config.activeTrancheSyn)} SYN</dd></div>
          <div><dt>Max Tranche</dt><dd>${html(config.maxTrancheUsd)}</dd></div>
          <div><dt>Source</dt><dd>${html(config.communitySourceBucket)}</dd></div>
          <div><dt>Treasury</dt><dd>${html(config.treasuryWallet)}</dd></div>
        </dl>
        <div class="synthos-sale-box">
          <label>Payment Asset
            <select data-sale-asset>
              ${config.assets.map((asset, index) => `<option value="${index}">${html(asset.symbol)}</option>`).join("")}
            </select>
          </label>
          <label>${config.paymentRails && !validAddress(config.saleContract) ? "USD Amount To Spend" : "Amount To Spend"}
            <input data-sale-amount inputmode="decimal" autocomplete="off" />
          </label>
          ${config.paymentRails && !validAddress(config.saleContract) ? `
          <label>SYNTHOS Wallet Address
            <input data-synthos-address autocomplete="off" placeholder="0x..." value="${html(state.synthosWallet?.address || "")}" />
          </label>
          <div class="synthos-native-wallet" data-synthos-wallet-panel></div>
          <div class="synthos-payment-instructions" data-payment-instructions></div>
          <label>Payment Transaction Hash
            <input data-payment-tx autocomplete="off" placeholder="0x..." />
          </label>
          ` : ""}
          <div class="synthos-sale-quote" data-sale-quote>Connect wallet for quote.</div>
          <div class="synthos-sale-actions">
            <button type="button" data-sale-connect>Connect Wallet</button>
            <button type="button" data-sale-buy disabled>Create Payment Intent</button>
            ${config.paymentRails && !validAddress(config.saleContract) ? `<button type="button" data-generate-synthos-wallet>Generate SYNTHOS Wallet</button>` : ""}
            ${config.paymentRails && !validAddress(config.saleContract) ? `<button type="button" data-payment-verify>Verify Payment</button>` : ""}
          </div>
          <p data-sale-status data-tone="muted">Sale contract is checking configuration.</p>
        </div>
      </section>
    `;
    root.querySelector("[data-sale-connect]").addEventListener("click", connectWallet);
    root.querySelector("[data-sale-buy]").addEventListener("click", buySyn);
    root.querySelector("[data-sale-amount]").addEventListener("input", quote);
    root.querySelector("[data-sale-asset]").addEventListener("change", quote);
    root.querySelector("[data-generate-synthos-wallet]")?.addEventListener("click", generateSynthosWallet);
    root.querySelector("[data-payment-verify]")?.addEventListener("click", verifyPaymentIntent);
    root.querySelector("[data-synthos-address]")?.addEventListener("input", () => {
      renderSynthosWallet();
      validateConfig();
    });
    renderSynthosWallet();
    validateConfig();
  }

  function validateConfig() {
    const buy = root.querySelector("[data-sale-buy]");
    if (config.paymentRails && !validAddress(config.saleContract)) {
      const walletGate = nativeWalletGate();
      buy.disabled = !walletGate.ok;
      setStatus(walletGate.message, walletGate.ok ? "ok" : "warn");
      return walletGate.ok;
    }
    if (!validAddress(config.saleContract)) {
      setStatus("Early access contract is not deployed/configured yet.", "warn");
      buy.disabled = true;
      return false;
    }
    if (Number(config.chainId) === HARDHAT_CHAIN_ID) {
      setStatus("Local Hardhat sale address detected. Live purchases are disabled.", "warn");
      buy.disabled = true;
      return false;
    }
    if (!validAddress(config.complianceRegistry)) {
      setStatus("Compliance registry is not configured yet.", "warn");
      buy.disabled = true;
      return false;
    }
    setStatus("Ready. Connect wallet to continue.");
    return true;
  }

  function loadStoredSynthosWallet() {
    try {
      const raw = window.localStorage?.getItem(SYNTHOS_NATIVE_WALLET_STORAGE_KEY);
      if (!raw) return null;
      const wallet = JSON.parse(raw);
      if (!wallet || !validAddress(wallet.address) || !isHexBytes(wallet.publicKey, 32) || !isHexBytes(wallet.privateKey, 64)) {
        return null;
      }
      return wallet;
    } catch (_) {
      return null;
    }
  }

  function loadBackupConfirmation() {
    try {
      return window.localStorage?.getItem(SYNTHOS_NATIVE_WALLET_BACKUP_CONFIRMED_KEY) === "true";
    } catch (_) {
      return false;
    }
  }

  function storeSynthosWallet(wallet) {
    state.synthosWallet = wallet;
    state.synthosWalletBackupConfirmed = false;
    try {
      window.localStorage?.setItem(SYNTHOS_NATIVE_WALLET_STORAGE_KEY, JSON.stringify(wallet));
      window.localStorage?.removeItem(SYNTHOS_NATIVE_WALLET_BACKUP_CONFIRMED_KEY);
    } catch (_) {
      // Wallet still works for this page session even if localStorage is unavailable.
    }
  }

  function setBackupConfirmed(confirmed) {
    state.synthosWalletBackupConfirmed = Boolean(confirmed);
    try {
      if (confirmed) {
        window.localStorage?.setItem(SYNTHOS_NATIVE_WALLET_BACKUP_CONFIRMED_KEY, "true");
      } else {
        window.localStorage?.removeItem(SYNTHOS_NATIVE_WALLET_BACKUP_CONFIRMED_KEY);
      }
    } catch (_) {
      // Confirmation still applies for this page session.
    }
    validateConfig();
  }

  function activeSynthosAddress() {
    return root.querySelector("[data-synthos-address]")?.value.trim() || "";
  }

  function nativeWalletGate() {
    const address = activeSynthosAddress();
    if (!state.synthosWallet) {
      return { ok: false, message: "Create a SYNTHOS wallet before payment. No wallet, no payment intent." };
    }
    if (!validAddress(address)) {
      return { ok: false, message: "Create a valid SYNTHOS wallet before payment." };
    }
    if (!stringsEqual(address, state.synthosWallet.address)) {
      return { ok: false, message: "The receiving address must match the generated SYNTHOS wallet backup." };
    }
    if (!state.synthosWalletBackupConfirmed) {
      return { ok: false, message: "Download or copy the SYNTHOS private key, then confirm the backup before payment." };
    }
    return { ok: true, message: "Wallet backup confirmed. You can create the payment intent." };
  }

  function stringsEqual(a, b) {
    return String(a || "").toLowerCase() === String(b || "").toLowerCase();
  }

  function isHexBytes(value, bytes) {
    return typeof value === "string" && new RegExp(`^0x[a-fA-F0-9]{${bytes * 2}}$`).test(value);
  }

  function hex(bytes) {
    return "0x" + Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("");
  }

  function bytesToBase64(bytes) {
    let binary = "";
    bytes.forEach((byte) => { binary += String.fromCharCode(byte); });
    return btoa(binary);
  }

  function spkiToRawEd25519(spki) {
    const bytes = new Uint8Array(spki);
    // DER SubjectPublicKeyInfo for Ed25519 ends with the 32-byte raw public key.
    return bytes.slice(bytes.length - 32);
  }

  function pkcs8ToSeed(pkcs8) {
    const bytes = new Uint8Array(pkcs8);
    // DER PKCS#8 for Ed25519 generated by WebCrypto ends with a 32-byte seed.
    // SYNTHOS Go wallets store 64-byte private keys as seed || publicKey.
    return bytes.slice(bytes.length - 32);
  }

  async function sha256(bytes) {
    return new Uint8Array(await crypto.subtle.digest("SHA-256", bytes));
  }

  async function generateSynthosWallet() {
    if (!window.crypto?.subtle) {
      setStatus("This browser cannot generate a SYNTHOS wallet securely. Use a modern HTTPS browser.", "warn");
      return;
    }
    try {
      setStatus("Generating SYNTHOS native wallet...");
      const keyPair = await crypto.subtle.generateKey({ name: "Ed25519" }, true, ["sign", "verify"]);
      const publicKey = spkiToRawEd25519(await crypto.subtle.exportKey("spki", keyPair.publicKey));
      const seed = pkcs8ToSeed(await crypto.subtle.exportKey("pkcs8", keyPair.privateKey));
      const privateKey = new Uint8Array(64);
      privateKey.set(seed, 0);
      privateKey.set(publicKey, 32);
      const digest = await sha256(publicKey);
      const address = hex(digest.slice(0, 20));
      const wallet = {
        address,
        publicKey: hex(publicKey),
        privateKey: hex(privateKey),
        privateKeyBase64: bytesToBase64(privateKey),
        createdAt: new Date().toISOString(),
        format: "synthos-ed25519-v1",
      };
      storeSynthosWallet(wallet);
      const input = root.querySelector("[data-synthos-address]");
      if (input) input.value = wallet.address;
      renderSynthosWallet();
      validateConfig();
    } catch (error) {
      setStatus(error.message || "Could not generate SYNTHOS wallet.", "warn");
    }
  }

  function renderSynthosWallet() {
    const panel = root.querySelector("[data-synthos-wallet-panel]");
    if (!panel) return;
    const wallet = state.synthosWallet;
    if (!wallet) {
      panel.dataset.open = "true";
      panel.innerHTML = `
        <strong>No SYNTHOS receiving wallet selected</strong>
        <span>Generate a native SYNTHOS wallet before creating a payment intent.</span>
        <small>No exceptions: the page will not accept payment until a wallet exists and the private-key backup is confirmed.</small>
      `;
      return;
    }
    const addressMatches = stringsEqual(activeSynthosAddress(), wallet.address);
    const backupJSON = synthosWalletBackupJSON(wallet);
    panel.dataset.open = "true";
    panel.innerHTML = `
      <strong>SYNTHOS receiving wallet ready</strong>
      <span>Address: <code>${html(wallet.address)}</code></span>
      <span>Public key: <code>${html(wallet.publicKey)}</code></span>
      ${addressMatches ? "" : `<small>The address field does not match this generated wallet. Payment is locked until it matches.</small>`}
      <details>
        <summary>Show private key backup</summary>
        <small>This key controls the SYNTHOS address above. Save it offline. Do not share it.</small>
        <code>${html(wallet.privateKey)}</code>
      </details>
      <label>
        <input type="checkbox" data-confirm-synthos-backup ${state.synthosWalletBackupConfirmed ? "checked" : ""} />
        <span>I downloaded or copied this SYNTHOS private key and understand lost keys cannot be recovered.</span>
      </label>
      <div class="synthos-mini-actions">
        <button type="button" data-download-synthos-backup>Download wallet backup</button>
        <button type="button" data-copy-synthos-address>Copy address</button>
        <button type="button" data-copy-synthos-private>Copy private key</button>
      </div>
    `;
    panel.querySelector("[data-download-synthos-backup]")?.addEventListener("click", () => downloadText(
      `synthos-wallet-${wallet.address.slice(2, 10)}.json`,
      backupJSON,
      "Wallet backup downloaded. Now confirm you saved it."
    ));
    panel.querySelector("[data-copy-synthos-address]")?.addEventListener("click", () => copyText(wallet.address, "SYNTHOS address copied."));
    panel.querySelector("[data-copy-synthos-private]")?.addEventListener("click", () => copyText(wallet.privateKey, "Private key copied. Keep it secret."));
    panel.querySelector("[data-confirm-synthos-backup]")?.addEventListener("change", (event) => {
      setBackupConfirmed(event.target.checked);
    });
  }

  function synthosWalletBackupJSON(wallet) {
    return JSON.stringify({
      warning: "This private key controls the SYNTHOS address. Store offline. Do not share.",
      network: "SYNTHOS",
      format: wallet.format,
      address: wallet.address,
      publicKey: wallet.publicKey,
      privateKey: wallet.privateKey,
      createdAt: wallet.createdAt,
    }, null, 2);
  }

  function downloadText(filename, text, message) {
    const blob = new Blob([text], { type: "application/json" });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = filename;
    document.body.appendChild(link);
    link.click();
    link.remove();
    URL.revokeObjectURL(url);
    setStatus(message, "ok");
  }

  async function copyText(value, message) {
    try {
      await navigator.clipboard.writeText(value);
      setStatus(message, "ok");
    } catch (_) {
      setStatus("Copy failed. Select and copy the value manually.", "warn");
    }
  }

  async function connectWallet() {
    if (!validateConfig()) return;
    if (!window.ethereum) {
      setStatus("Wallet not found. Install MetaMask or another EVM wallet.", "warn");
      return;
    }
    const ethers = await ethersReady();
    state.provider = new ethers.BrowserProvider(window.ethereum);
    await state.provider.send("eth_requestAccounts", []);
    state.signer = await state.provider.getSigner();
    state.account = await state.signer.getAddress();

    const network = await state.provider.getNetwork();
    if (config.chainId && Number(network.chainId) !== Number(config.chainId)) {
      setStatus(`Switch wallet to ${config.chainName}.`, "warn");
      await requestNetworkSwitch();
      return;
    }

    state.sale = new ethers.Contract(config.saleContract, SALE_ABI, state.signer);
    state.compliance = new ethers.Contract(config.complianceRegistry, COMPLIANCE_ABI, state.provider);
    root.querySelector("[data-sale-connect]").textContent = `${state.account.slice(0, 6)}...${state.account.slice(-4)}`;
    root.querySelector("[data-sale-buy]").disabled = false;
    setStatus("Wallet connected. Enter amount to buy SYN.");
    await quote();
  }

  async function ensureEligible() {
    const ethers = await ethersReady();
    let eligible = await state.compliance.eligibleToReceive(state.account, 6);
    if (eligible) return true;

    const selfRegistrationOpen = await state.compliance.communitySelfRegistrationOpen();
    if (!selfRegistrationOpen) {
      setStatus("Early access self-registration is not open yet.", "warn");
      return false;
    }

    const writableCompliance = new ethers.Contract(config.complianceRegistry, COMPLIANCE_ABI, state.signer);
    const disclosureHash = config.disclosureHash || ethers.id(config.disclosureText);
    const jurisdictionHash = config.jurisdictionHash || ethers.id(config.jurisdictionCode);
    setStatus("Registering wallet for the early adopter tranche...");
    const tx = await writableCompliance.selfRegisterCommunity(disclosureHash, jurisdictionHash);
    await tx.wait();

    eligible = await state.compliance.eligibleToReceive(state.account, 6);
    if (!eligible) {
      setStatus("Wallet registration completed, but eligibility check still failed.", "warn");
      return false;
    }
    setStatus("Wallet registered. Continuing purchase...", "ok");
    return true;
  }

  async function requestNetworkSwitch() {
    if (!config.chainId || !window.ethereum) return;
    const chainIdHex = `0x${Number(config.chainId).toString(16)}`;
    try {
      await window.ethereum.request({ method: "wallet_switchEthereumChain", params: [{ chainId: chainIdHex }] });
    } catch (error) {
      if (error.code === 4902 && config.rpcUrls?.length) {
        await window.ethereum.request({
          method: "wallet_addEthereumChain",
          params: [{ chainId: chainIdHex, chainName: config.chainName, rpcUrls: config.rpcUrls }],
        });
      } else {
        throw error;
      }
    }
  }

  async function quote() {
    if (config.paymentRails && !state.sale) {
      const raw = root.querySelector("[data-sale-amount]").value.trim();
      const usd = Number(raw || 0);
      root.querySelector("[data-sale-quote]").textContent = usd > 0
        ? `${(usd * 10).toLocaleString()} SYN at $0.10`
        : "Enter a USD amount.";
      return;
    }
    if (!state.sale) return;
    const ethers = await ethersReady();
    const asset = activeAsset();
    const raw = root.querySelector("[data-sale-amount]").value.trim();
    if (!raw || Number(raw) <= 0) {
      root.querySelector("[data-sale-quote]").textContent = "Enter a payment amount.";
      return;
    }
    try {
      const paymentAmount = ethers.parseUnits(raw, asset.decimals);
      const result = asset.native
        ? await state.sale.quoteNativePurchase(paymentAmount)
        : await state.sale.quoteTokenPurchase(asset.address, paymentAmount);
      root.querySelector("[data-sale-quote]").textContent = `${ethers.formatUnits(result[0], 18)} SYN`;
    } catch (error) {
      root.querySelector("[data-sale-quote]").textContent = error.shortMessage || error.message;
    }
  }

  async function buySyn() {
    if (config.paymentRails && !state.sale) {
      await createPaymentIntent();
      return;
    }
    if (!state.sale || !state.account) {
      setStatus("Connect wallet first.", "warn");
      return;
    }
    const ethers = await ethersReady();
    if (!(await ensureEligible())) return;

    const asset = activeAsset();
    const raw = root.querySelector("[data-sale-amount]").value.trim();
    const paymentAmount = ethers.parseUnits(raw || "0", asset.decimals);
    if (paymentAmount <= 0n) {
      setStatus("Enter a payment amount.", "warn");
      return;
    }

    try {
      setStatus("Submitting purchase transaction...");
      let tx;
      if (asset.native) {
        tx = await state.sale.buyWithNative(state.account, { value: paymentAmount });
      } else {
        if (!validAddress(asset.address)) throw new Error(`${asset.symbol} address is not configured.`);
        const token = new ethers.Contract(asset.address, ERC20_ABI, state.signer);
        const allowance = await token.allowance(state.account, config.saleContract);
        if (allowance < paymentAmount) {
          const approval = await token.approve(config.saleContract, paymentAmount);
          setStatus(`Approving ${asset.symbol} spend...`);
          await approval.wait();
        }
        tx = await state.sale.buyWithToken(asset.address, paymentAmount, state.account);
      }
      setStatus("Purchase submitted. Waiting for chain finality...");
      await tx.wait();
      setStatus("SYN delivered automatically in the purchase transaction.", "ok");
      await quote();
    } catch (error) {
      setStatus(error.shortMessage || error.message || "Purchase failed", "warn");
    }
  }

  async function createPaymentIntent() {
    const asset = activeAsset();
    const raw = root.querySelector("[data-sale-amount]").value.trim();
    const synthosAddress = activeSynthosAddress();
    if (!raw || Number(raw) <= 0) {
      setStatus("Enter a USD amount.", "warn");
      return;
    }
    const walletGate = nativeWalletGate();
    if (!walletGate.ok) {
      setStatus(walletGate.message, "warn");
      return;
    }
    try {
      setStatus("Creating crypto payment intent...");
      const response = await fetch(resolveBackendURL(config.paymentIntentUrl), {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          buyerWallet: state.account || "",
          synthosAddress,
          assetSymbol: asset.symbol,
          usdValue: raw,
        }),
      });
      if (!response.ok) throw new Error(await response.text());
      const body = await response.json();
      state.paymentIntent = body.intent;
      renderPaymentInstructions(body.intent);
      setStatus("Payment intent created. Send the exact crypto amount, then paste the transaction hash.", "ok");
    } catch (error) {
      setStatus(error.shortMessage || error.message || "Payment intent failed", "warn");
    }
  }

  async function verifyPaymentIntent() {
    const txHash = root.querySelector("[data-payment-tx]")?.value.trim();
    if (!state.paymentIntent?.id) {
      setStatus("Create a payment intent first.", "warn");
      return;
    }
    if (!txHash) {
      setStatus("Paste the payment transaction hash.", "warn");
      return;
    }
    try {
      setStatus("Verifying payment on-chain...");
      const url = resolveBackendURL(`${config.paymentIntentUrl}/${state.paymentIntent.id}/verify`);
      const response = await fetch(url, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ txHash }),
      });
      const body = await response.json();
      state.paymentIntent = body.intent;
      renderPaymentInstructions(body.intent);
      setStatus(body.intent.status === "syn_allocated"
        ? "Payment verified and SYN allocated on the SYNTHOS network."
        : `Payment status: ${body.intent.status}`,
        body.ok ? "ok" : "warn");
    } catch (error) {
      setStatus(error.shortMessage || error.message || "Payment verification failed", "warn");
    }
  }

  function renderPaymentInstructions(intent) {
    const node = root.querySelector("[data-payment-instructions]");
    if (!node || !intent) return;
    node.dataset.open = "true";
    node.innerHTML = `
      <strong>Send ${html(intent.assetSymbol)} payment</strong>
      <span>Network: ${html(intent.network || "configured payment network")}</span>
      <span>Amount: <code>${html(intent.paymentAmount)}</code> base units</span>
      <span>To: <code>${html(intent.paymentAddress)}</code></span>
      <span>SYN allocation: ${html(intent.synAmount)} SYN</span>
      <span>Status: ${html(intent.status)}</span>
      ${intent.synthosTxId ? `<span>SYNTHOS tx: <code>${html(intent.synthosTxId)}</code></span>` : ""}
    `;
  }

  function resolveBackendURL(path) {
    if (/^https?:\/\//.test(path)) return path;
    const baseURL = window.SYNTHOS_API_URL || window.SYNTHOS_BACKEND_URL || config.apiURL || "";
    return `${String(baseURL).replace(/\/$/, "")}${path.startsWith("/") ? path : `/${path}`}`;
  }

  async function boot() {
    await loadBackendConfig();
    render();
  }

  boot();
})();
