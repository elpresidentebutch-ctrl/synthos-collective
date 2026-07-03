(function () {
  const TREASURY_WALLET = "0xdAE5DF4807274D7a115bB5078c94b023453A05F5";
  const HARDHAT_CHAIN_ID = 31337;
  const ETHERS_CDN = "https://cdn.jsdelivr.net/npm/ethers@6.13.4/dist/ethers.umd.min.js";

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
    tokenPriceUsd: "0.05",
    activeTrancheSyn: "250,000,000",
    maxTrancheUsd: "$12,500,000",
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
            <input data-synthos-address autocomplete="off" placeholder="0x..." />
          </label>
          <div class="synthos-payment-instructions" data-payment-instructions></div>
          <label>Payment Transaction Hash
            <input data-payment-tx autocomplete="off" placeholder="0x..." />
          </label>
          ` : ""}
          <div class="synthos-sale-quote" data-sale-quote>Connect wallet for quote.</div>
          <div class="synthos-sale-actions">
            <button type="button" data-sale-connect>Connect Wallet</button>
            <button type="button" data-sale-buy disabled>Buy SYN</button>
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
    root.querySelector("[data-payment-verify]")?.addEventListener("click", verifyPaymentIntent);
    validateConfig();
  }

  function validateConfig() {
    const buy = root.querySelector("[data-sale-buy]");
    if (config.paymentRails && !validAddress(config.saleContract)) {
      setStatus("Native SYNTHOS payment rails are ready. Create a payment intent to continue.");
      buy.disabled = false;
      return true;
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
        ? `${(usd * 20).toLocaleString()} SYN at $0.05`
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
    const synthosAddress = root.querySelector("[data-synthos-address]")?.value.trim();
    if (!raw || Number(raw) <= 0) {
      setStatus("Enter a USD amount.", "warn");
      return;
    }
    if (!synthosAddress) {
      setStatus("Enter the SYNTHOS wallet address that should receive SYN.", "warn");
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
