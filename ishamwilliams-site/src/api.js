/**
 * api.js — Synthos Collective API Service
 * Connects to the Go RPC node backend.
 * In dev: proxied via Vite to localhost:8080
 * In prod: set VITE_API_URL env var to your node's endpoint
 */

const BASE = import.meta.env.VITE_API_URL || '/api'

async function request(path, options = {}) {
  try {
    const res = await fetch(`${BASE}${path}`, {
      headers: {
        'Content-Type': 'application/json',
        ...options.headers
      },
      ...options
    })
    if (!res.ok) {
      const text = await res.text()
      throw new Error(text || `HTTP ${res.status}`)
    }
    return await res.json()
  } catch (err) {
    console.warn(`[API] ${path} failed:`, err.message)
    throw err
  }
}

// ─── Chain Status ────────────────────────────────────────────────────────────
export async function getHealth() {
  return request('/health')
}

export async function getStatus() {
  return request('/status')
}

// ─── Accounts / Balances ─────────────────────────────────────────────────────
export async function getBalance(address) {
  return request(`/balance?address=${encodeURIComponent(address)}`)
}

// ─── Mempool ─────────────────────────────────────────────────────────────────
export async function getMempool() {
  return request('/mempool')
}

// ─── Transactions ─────────────────────────────────────────────────────────────
export async function submitTx(tx) {
  return request('/submitTx', {
    method: 'POST',
    body: JSON.stringify(tx)
  })
}

// ─── Block Proposal ──────────────────────────────────────────────────────────
export async function proposeBlock() {
  return request('/proposeBlock', { method: 'POST', body: '{}' })
}

// ─── Mock/Simulated data for when the node is offline ─────────────────────────
// These provide realistic numbers from your actual tokenomics
export const TOKENOMICS = {
  totalSupply: 100_000_000_000,
  founderStake: 1_000_000_000,
  vestingPayoutPerYear: 1_000_000_000,
  vestingPeriodYears: 20,
  innerCirclePrice: 200_000_000,  // in SYN
  innerCirclePriceUSD: 2000,       // $2,000 per slot
  escrowCommission: 1,             // 1%
  minFee: 10,
  burnPercent: 50,
  chainID: 'synthos-1',
  ticker: 'SYN',
  dexFee: 0.3,                     // 0.3% swap fee
}

// Simulated live network stats (shown when node offline)
export function getMockStats() {
  const now = Date.now()
  const baseHeight = 18_442 + Math.floor((now / 1000 - 1715724000) / 4)
  return {
    height: baseHeight,
    activeValidators: 24,
    totalStaked: 42_800_000_000,
    mempoolSize: Math.floor(Math.random() * 12),
    tps: (1.2 + Math.random() * 2.3).toFixed(2),
    blockTime: '~4s',
    stateRoot: '0x' + Array.from({length: 64}, () => Math.floor(Math.random() * 16).toString(16)).join(''),
    circulating: 58_200_000_000,
    founderBalance: 1_000_000_000,
    innerCircleSlotsFilled: 47,
    innerCircleSlotsTotal: 100,
    synPriceUSD: 0.0000412,
    marketCapUSD: 2_390_000,
    volumeUSD24h: 187_440,
  }
}

// Simulated recent blocks
export function getMockBlocks(count = 10) {
  const now = Date.now()
  return Array.from({ length: count }, (_, i) => {
    const height = 18_442 - i
    return {
      height,
      hash: '0x' + Array.from({length: 16}, () => Math.floor(Math.random() * 16).toString(16)).join('') + '...',
      proposer: 'validator-' + (Math.floor(Math.random() * 8) + 1).toString().padStart(2, '0'),
      txCount: Math.floor(Math.random() * 8),
      fees: Math.floor(Math.random() * 1200) + 50,
      age: `${i * 4 + Math.floor(Math.random() * 3)}s ago`,
      stateRoot: '0x' + Array.from({length: 12}, () => Math.floor(Math.random() * 16).toString(16)).join('') + '...',
    }
  })
}

// Simulated transaction feed
export function getMockTxs(count = 8) {
  const types = ['transfer', 'escrow_lock', 'inner_circle_purchase', 'stake', 'dex_swap']
  const addrs = [
    '0x205042f06cd3aa7d9a88deec39b9d0ba6b9fbf2b',
    '0xa1b2c3d4e5f60718293a4b5c6d7e8f9012345678',
    '0xdeadbeef00000000000000000000000000000001',
    '0xcafe0000000000000000000000000000deadc0de',
    '0x1111111111111111111111111111111111111111',
  ]
  return Array.from({ length: count }, (_, i) => ({
    id: '0x' + Array.from({length: 16}, () => Math.floor(Math.random() * 16).toString(16)).join('') + '...',
    type: types[Math.floor(Math.random() * types.length)],
    from: addrs[Math.floor(Math.random() * addrs.length)],
    to: addrs[Math.floor(Math.random() * addrs.length)],
    amount: Math.floor(Math.random() * 10_000_000) + 100,
    fee: Math.floor(Math.random() * 500) + 10,
    age: `${Math.floor(Math.random() * 60) + 1}s ago`,
  }))
}

// Simulated DEX pools
export function getMockPools() {
  return [
    { assetId: 'USDC', synReserve: 4_200_000_000, assetReserve: 173_040, synPrice: 0.0000412, apy: 18.4, volume24h: 87_200 },
    { assetId: 'ETH', synReserve: 2_100_000_000, assetReserve: 18.2, synPrice: 0.0000412, apy: 24.1, volume24h: 42_100 },
    { assetId: 'BTC', synReserve: 980_000_000, assetReserve: 0.94, synPrice: 0.0000412, apy: 31.7, volume24h: 28_800 },
    { assetId: 'SOL', synReserve: 750_000_000, assetReserve: 1240, synPrice: 0.0000412, apy: 22.9, volume24h: 19_300 },
  ]
}
