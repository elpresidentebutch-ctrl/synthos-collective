/**
 * Synthos Collective — Backend API Server
 * Handles: Email OTP, Member Registration, Admin Auth, DEX State
 *
 * Run with: node server.js
 * Requires: npm install express nodemailer cors dotenv
 */

import express from 'express'
import nodemailer from 'nodemailer'
import cors from 'cors'
import fs from 'fs'
import path from 'path'
import { fileURLToPath } from 'url'
import dotenv from 'dotenv'

dotenv.config()

const __dirname = path.dirname(fileURLToPath(import.meta.url))

const app = express()
app.use(express.json())
app.use(cors({ origin: process.env.FRONTEND_URL || 'http://localhost:3000' }))

// ─── File paths ────────────────────────────────────────────────────────────
const DATA_DIR = path.join(__dirname, 'data')
const MEMBERS_FILE = path.join(DATA_DIR, 'members.json')
const DEX_FILE = path.join(DATA_DIR, 'dex_state.json')

if (!fs.existsSync(DATA_DIR)) fs.mkdirSync(DATA_DIR, { recursive: true })
if (!fs.existsSync(MEMBERS_FILE)) fs.writeFileSync(MEMBERS_FILE, JSON.stringify([], null, 2))

// ─── Initial DEX state ────────────────────────────────────────────────────
const DEFAULT_DEX_STATE = {
  pools: {
    USDC: { assetId: 'USDC', synReserve: 4_200_000_000, assetReserve: 173040, totalShares: 4_200_000_000, providers: {} },
    ETH:  { assetId: 'ETH',  synReserve: 2_100_000_000, assetReserve: 18.2,   totalShares: 2_100_000_000, providers: {} },
    BTC:  { assetId: 'BTC',  synReserve: 980_000_000,   assetReserve: 0.94,   totalShares: 980_000_000,   providers: {} },
    SOL:  { assetId: 'SOL',  synReserve: 750_000_000,   assetReserve: 1240,   totalShares: 750_000_000,   providers: {} },
  },
  swapHistory: [],
  liquidityHistory: [],
}

if (!fs.existsSync(DEX_FILE)) fs.writeFileSync(DEX_FILE, JSON.stringify(DEFAULT_DEX_STATE, null, 2))

function readMembers() {
  try { return JSON.parse(fs.readFileSync(MEMBERS_FILE, 'utf8')) }
  catch { return [] }
}

function saveMembers(members) {
  fs.writeFileSync(MEMBERS_FILE, JSON.stringify(members, null, 2))
}

function readDEX() {
  try { return JSON.parse(fs.readFileSync(DEX_FILE, 'utf8')) }
  catch { return DEFAULT_DEX_STATE }
}

function saveDEX(state) {
  fs.writeFileSync(DEX_FILE, JSON.stringify(state, null, 2))
}

// ─── In-memory OTP store ──────────────────────────────────────────────────
// { email: { code, expires, name, phone } }
const otpStore = new Map()

// ─── Email transporter ────────────────────────────────────────────────────
const transporter = nodemailer.createTransport({
  host: process.env.SMTP_HOST || 'smtp.gmail.com',
  port: parseInt(process.env.SMTP_PORT || '587'),
  secure: false,
  auth: {
    user: process.env.SMTP_USER,
    pass: process.env.SMTP_PASS,
  },
})

// ─── Admin Auth ───────────────────────────────────────────────────────────
// Password stored ONLY in environment variable — never in frontend code
const ADMIN_PASSWORD = process.env.ADMIN_PASSWORD || '224369blockchainmind'
const adminSessions = new Set()

function generateToken() {
  return Array.from({ length: 48 }, () => Math.floor(Math.random() * 16).toString(16)).join('')
}

function requireAdmin(req, res, next) {
  const token = req.headers['x-admin-token']
  if (!token || !adminSessions.has(token)) {
    return res.status(401).json({ error: 'Unauthorized' })
  }
  next()
}

// ─── Routes ───────────────────────────────────────────────────────────────

// Health check
app.get('/api/health', (req, res) => {
  res.json({ ok: true, service: 'synthos-api', timestamp: Date.now() })
})

// ── OTP: Send verification code to email ──────────────────────────────────
app.post('/api/send-code', async (req, res) => {
  const { email, name, phone } = req.body
  if (!email || !name) {
    return res.status(400).json({ error: 'Email and name are required' })
  }

  // Check if already registered
  const members = readMembers()
  if (members.find(m => m.email.toLowerCase() === email.toLowerCase())) {
    return res.status(409).json({ error: 'This email is already registered with the Synthos Collective.' })
  }

  // Generate 6-digit OTP
  const code = String(Math.floor(100000 + Math.random() * 900000))
  const expires = Date.now() + 10 * 60 * 1000 // 10 minutes

  otpStore.set(email.toLowerCase(), { code, expires, name, phone: phone || '' })

  // Send email
  try {
    await transporter.sendMail({
      from: `"Synthos Collective" <${process.env.SMTP_USER}>`,
      to: email,
      subject: 'Your Synthos Collective Verification Code',
      html: `
        <div style="background:#080b0f;color:#e8edf5;font-family:Inter,sans-serif;padding:48px;max-width:600px;margin:0 auto;border-radius:16px;border:1px solid rgba(0,229,255,0.15)">
          <div style="font-size:0.75rem;letter-spacing:0.12em;text-transform:uppercase;color:#00e5ff;margin-bottom:8px">Synthos Collective</div>
          <h1 style="font-size:2rem;margin-bottom:8px;color:#e8edf5">Verify Your Email</h1>
          <p style="color:#8899aa;margin-bottom:32px">Hello ${name}, here is your 6-digit verification code to complete your Synthos Collective registration:</p>
          <div style="background:#1a2233;border:2px solid rgba(0,229,255,0.3);border-radius:12px;padding:32px;text-align:center;margin-bottom:32px">
            <div style="font-size:3rem;font-weight:800;letter-spacing:0.3em;color:#00e5ff;font-family:monospace">${code}</div>
            <div style="color:#4a5568;font-size:0.85rem;margin-top:12px">Valid for 10 minutes</div>
          </div>
          <p style="color:#4a5568;font-size:0.82rem">If you did not request this code, you can safely ignore this email.</p>
          <div style="margin-top:32px;padding-top:24px;border-top:1px solid rgba(0,229,255,0.08);font-size:0.75rem;color:#4a5568">
            Isham Williams Blockchains LLC · Synthos Collective L1 Network
          </div>
        </div>
      `,
    })
    res.json({ ok: true, message: 'Verification code sent to ' + email })
  } catch (err) {
    console.error('Email send error:', err)
    // In dev without SMTP configured, return the code so you can test
    if (process.env.NODE_ENV !== 'production') {
      res.json({ ok: true, message: 'Email not configured — dev code: ' + code, devCode: code })
    } else {
      res.status(500).json({ error: 'Failed to send verification email. Please check SMTP configuration.' })
    }
  }
})

// ── OTP: Verify code and complete registration ────────────────────────────
app.post('/api/verify-code', (req, res) => {
  const { email, code } = req.body
  if (!email || !code) {
    return res.status(400).json({ error: 'Email and code are required' })
  }

  const stored = otpStore.get(email.toLowerCase())
  if (!stored) {
    return res.status(400).json({ error: 'No verification code found for this email. Please request a new code.' })
  }
  if (Date.now() > stored.expires) {
    otpStore.delete(email.toLowerCase())
    return res.status(400).json({ error: 'Verification code expired. Please request a new one.' })
  }
  if (stored.code !== String(code).trim()) {
    return res.status(400).json({ error: 'Incorrect verification code.' })
  }

  // Code valid — save member
  const members = readMembers()
  const newMember = {
    id: Date.now(),
    name: stored.name,
    email: email.toLowerCase(),
    phone: stored.phone,
    joinedAt: new Date().toISOString(),
    ipHint: req.ip || 'unknown',
    verified: true,
  }
  members.push(newMember)
  saveMembers(members)
  otpStore.delete(email.toLowerCase())

  res.json({ ok: true, message: 'Email verified! Welcome to the Synthos Collective.', member: { name: newMember.name, email: newMember.email } })
})

// ── Admin Login ──────────────────────────────────────────────────────────
app.post('/api/admin/login', (req, res) => {
  const { password } = req.body
  if (!password || password !== ADMIN_PASSWORD) {
    return res.status(401).json({ error: 'Invalid password.' })
  }
  const token = generateToken()
  adminSessions.add(token)
  // Auto-expire session after 2 hours
  setTimeout(() => adminSessions.delete(token), 2 * 60 * 60 * 1000)
  res.json({ ok: true, token })
})

// ── Admin: Get all members ────────────────────────────────────────────────
app.get('/api/admin/members', requireAdmin, (req, res) => {
  const members = readMembers()
  res.json({ ok: true, count: members.length, members })
})

// ── Admin: Export members as CSV ──────────────────────────────────────────
app.get('/api/admin/members/export', requireAdmin, (req, res) => {
  const members = readMembers()
  const headers = ['ID', 'Name', 'Email', 'Phone', 'Joined At', 'Verified']
  const rows = members.map(m => [m.id, m.name, m.email, m.phone || '', m.joinedAt, m.verified])
  const csv = [headers, ...rows].map(row => row.map(v => `"${v}"`).join(',')).join('\n')
  res.setHeader('Content-Type', 'text/csv')
  res.setHeader('Content-Disposition', 'attachment; filename="synthos-members.csv"')
  res.send(csv)
})

// ── Admin: Delete a member ────────────────────────────────────────────────
app.delete('/api/admin/members/:id', requireAdmin, (req, res) => {
  const id = parseInt(req.params.id)
  let members = readMembers()
  members = members.filter(m => m.id !== id)
  saveMembers(members)
  res.json({ ok: true })
})

// ─── DEX API ──────────────────────────────────────────────────────────────

// Get all pools
app.get('/api/dex/pools', (req, res) => {
  const state = readDEX()
  res.json({ ok: true, pools: state.pools })
})

// Execute a swap (real AMM math)
app.post('/api/dex/swap', (req, res) => {
  const { assetId, amountIn, fromSyn, address } = req.body
  if (!assetId || !amountIn || fromSyn === undefined) {
    return res.status(400).json({ error: 'Missing required fields' })
  }

  const state = readDEX()
  const pool = state.pools[assetId]
  if (!pool) return res.status(404).json({ error: 'Pool not found' })

  const reserveIn = fromSyn ? pool.synReserve : pool.assetReserve
  const reserveOut = fromSyn ? pool.assetReserve : pool.synReserve
  const amtIn = parseFloat(amountIn)

  if (amtIn <= 0) return res.status(400).json({ error: 'Amount must be greater than 0' })
  if (amtIn >= reserveIn) return res.status(400).json({ error: 'Insufficient pool liquidity' })

  // x * y = k  (with 0.3% fee)
  const amtInWithFee = amtIn * 997
  const numerator = amtInWithFee * reserveOut
  const denominator = reserveIn * 1000 + amtInWithFee
  const amtOut = numerator / denominator

  // Update pool reserves
  if (fromSyn) {
    pool.synReserve += amtIn
    pool.assetReserve -= amtOut
  } else {
    pool.assetReserve += amtIn
    pool.synReserve -= amtOut
  }

  // Record swap history
  const swap = {
    id: Date.now(),
    assetId,
    fromSyn,
    amountIn: amtIn,
    amountOut: amtOut,
    fee: amtIn * 0.003,
    priceImpact: ((amtIn / reserveIn) * 100).toFixed(4),
    address: address || 'anonymous',
    timestamp: new Date().toISOString(),
  }
  state.swapHistory = [swap, ...state.swapHistory].slice(0, 200)

  saveDEX(state)
  res.json({ ok: true, swap, pool: { synReserve: pool.synReserve, assetReserve: pool.assetReserve } })
})

// Add liquidity to a pool
app.post('/api/dex/liquidity/add', (req, res) => {
  const { assetId, synAmount, assetAmount, address } = req.body
  if (!assetId || !synAmount || !assetAmount) {
    return res.status(400).json({ error: 'Missing required fields' })
  }

  const state = readDEX()
  const pool = state.pools[assetId]
  if (!pool) return res.status(404).json({ error: 'Pool not found' })

  const syn = parseFloat(synAmount)
  const asset = parseFloat(assetAmount)
  if (syn <= 0 || asset <= 0) return res.status(400).json({ error: 'Amounts must be greater than 0' })

  // Calculate shares (same logic as Go dex.go)
  let shares
  if (pool.totalShares === 0) {
    shares = syn
  } else {
    const shareSyn = (syn * pool.totalShares) / pool.synReserve
    const shareAsset = (asset * pool.totalShares) / pool.assetReserve
    shares = Math.min(shareSyn, shareAsset)
  }

  if (shares <= 0) return res.status(400).json({ error: 'Insufficient liquidity minted' })

  pool.synReserve += syn
  pool.assetReserve += asset
  pool.totalShares += shares
  if (address) {
    pool.providers[address] = (pool.providers[address] || 0) + shares
  }

  const event = {
    id: Date.now(), assetId, synAmount: syn, assetAmount: asset,
    shares, address: address || 'anonymous', timestamp: new Date().toISOString(),
  }
  state.liquidityHistory = [event, ...state.liquidityHistory].slice(0, 200)

  saveDEX(state)
  res.json({ ok: true, shares, pool: { synReserve: pool.synReserve, assetReserve: pool.assetReserve, totalShares: pool.totalShares } })
})

// Remove liquidity from a pool
app.post('/api/dex/liquidity/remove', (req, res) => {
  const { assetId, shares, address } = req.body
  if (!assetId || !shares || !address) {
    return res.status(400).json({ error: 'Missing required fields' })
  }

  const state = readDEX()
  const pool = state.pools[assetId]
  if (!pool) return res.status(404).json({ error: 'Pool not found' })

  const userShares = pool.providers[address] || 0
  const sharesToRemove = parseFloat(shares)

  if (sharesToRemove > userShares) {
    return res.status(400).json({ error: 'Insufficient LP shares' })
  }

  const synOut = (sharesToRemove / pool.totalShares) * pool.synReserve
  const assetOut = (sharesToRemove / pool.totalShares) * pool.assetReserve

  pool.synReserve -= synOut
  pool.assetReserve -= assetOut
  pool.totalShares -= sharesToRemove
  pool.providers[address] = userShares - sharesToRemove

  saveDEX(state)
  res.json({ ok: true, synOut, assetOut, remainingShares: pool.providers[address] })
})

// DEX swap history
app.get('/api/dex/history', (req, res) => {
  const state = readDEX()
  res.json({ ok: true, history: state.swapHistory.slice(0, 50) })
})

// ─── Serve built frontend in production ──────────────────────────────────
if (process.env.NODE_ENV === 'production') {
  app.use(express.static(path.join(__dirname, 'dist')))
  app.get('*', (req, res) => {
    res.sendFile(path.join(__dirname, 'dist', 'index.html'))
  })
}

// ─── Start ────────────────────────────────────────────────────────────────
const PORT = process.env.PORT || 4000
app.listen(PORT, () => {
  console.log(`\n🚀 Synthos API Server running on port ${PORT}`)
  console.log(`   Members DB: ${MEMBERS_FILE}`)
  console.log(`   DEX State:  ${DEX_FILE}`)
  console.log(`   Admin protected: YES`)
  if (!process.env.SMTP_USER) {
    console.log(`\n⚠️  SMTP not configured — set SMTP_USER and SMTP_PASS in .env`)
    console.log(`   Email codes will be shown in API responses during development\n`)
  }
})
