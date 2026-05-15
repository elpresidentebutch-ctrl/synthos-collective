import { useState } from 'react'
import { submitTx, TOKENOMICS, getMockStats } from '../api'

const TOTAL_SLOTS = 100
const API = 'http://localhost:4000/api'

const PERKS = [
  { icon: '🗳️', title: 'Protocol Governance', desc: 'Vote on chain parameter changes, fee structures, and upgrade proposals.' },
  { icon: '⚡', title: 'Priority Validator Access', desc: 'Fast-tracked validator registration and dedicated block proposer slots.' },
  { icon: '💰', title: 'Fee Revenue Share', desc: 'Access to on-chain escrow commissions and DEX fee distributions.' },
  { icon: '🔐', title: 'Sovereign Identity', desc: 'On-chain credential proving founding membership — non-transferable.' },
  { icon: '📡', title: 'Direct RPC Access', desc: 'Priority access to the founder node RPC endpoint.' },
  { icon: '🛡️', title: 'DMAS Protection', desc: 'Elevated immune system protection thresholds for Inner Circle addresses.' },
]

export default function InnerCircle() {
  const stats = getMockStats()
  const slotsLeft = TOTAL_SLOTS - stats.innerCircleSlotsFilled

  // Registration form state
  const [name, setName] = useState('')
  const [email, setEmail] = useState('')
  const [phone, setPhone] = useState('')
  const [code, setCode] = useState('')
  const [codeSent, setCodeSent] = useState(false)
  const [verified, setVerified] = useState(false)
  const [sending, setSending] = useState(false)
  const [verifying, setVerifying] = useState(false)
  const [msg, setMsg] = useState(null) // { type: 'success'|'error', text }
  const [devCode, setDevCode] = useState(null)

  // Chain purchase state
  const [walletAddr, setWalletAddr] = useState('')
  const [publicKey, setPublicKey] = useState('')
  const [signature, setSignature] = useState('')
  const [nonce, setNonce] = useState('')
  const [purchasing, setPurchasing] = useState(false)
  const [purchaseResult, setPurchaseResult] = useState(null)

  async function sendCode(e) {
    e.preventDefault()
    if (!name || !email) return
    setSending(true)
    setMsg(null)
    try {
      const r = await fetch(`${API}/send-code`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, name, phone })
      })
      const d = await r.json()
      if (!r.ok) { setMsg({ type: 'error', text: d.error }); setSending(false); return }
      setCodeSent(true)
      setMsg({ type: 'success', text: d.message })
      if (d.devCode) setDevCode(d.devCode) // dev mode only
    } catch {
      setMsg({ type: 'error', text: 'Server not reachable. Make sure server.js is running on port 4000.' })
    }
    setSending(false)
  }

  async function verifyCode(e) {
    e.preventDefault()
    setVerifying(true)
    setMsg(null)
    try {
      const r = await fetch(`${API}/verify-code`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, code })
      })
      const d = await r.json()
      if (!r.ok) { setMsg({ type: 'error', text: d.error }); setVerifying(false); return }
      setVerified(true)
      setMsg({ type: 'success', text: `✅ ${d.message}` })
    } catch {
      setMsg({ type: 'error', text: 'Server not reachable.' })
    }
    setVerifying(false)
  }

  async function handlePurchase(e) {
    e.preventDefault()
    setPurchasing(true)
    setPurchaseResult(null)
    try {
      const founderAddr = '0x205042f06cd3aa7d9a88deec39b9d0ba6b9fbf2b'
      const tx = {
        id: '0x' + Array.from({ length: 64 }, () => Math.floor(Math.random() * 16).toString(16)).join(''),
        chain_id: 1,
        from: walletAddr,
        to: founderAddr,
        amount: TOKENOMICS.innerCirclePrice,
        fee: TOKENOMICS.minFee,
        nonce: parseInt(nonce) || 0,
        public_key: publicKey,
        signature,
        metadata: [{ key: 'type', value: 'inner_circle_purchase' }],
      }
      const res = await submitTx(tx)
      setPurchaseResult({ ok: true, txId: res.tx_id || tx.id })
    } catch {
      setPurchaseResult({ ok: false, payload: `POST /submitTx to your RPC node\n{\n  "from": "${walletAddr}",\n  "to": "0x205042f06cd3aa7d9a88deec39b9d0ba6b9fbf2b",\n  "amount": 200000000,\n  "metadata": [{"key":"type","value":"inner_circle_purchase"}]\n}` })
    }
    setPurchasing(false)
  }

  return (
    <div className="page-content page-enter">

      {/* Hero */}
      <section style={{ minHeight: '55vh', display: 'flex', alignItems: 'center', position: 'relative', overflow: 'hidden', background: 'linear-gradient(180deg, rgba(171,71,188,0.06) 0%, transparent 100%)', borderBottom: '1px solid var(--border-subtle)', padding: '80px 0' }}>
        <div style={{ position: 'absolute', top: '20%', right: '5%', width: 500, height: 500, borderRadius: '50%', background: 'radial-gradient(circle, rgba(171,71,188,0.07) 0%, transparent 70%)', pointerEvents: 'none' }} />
        <div className="container" style={{ position: 'relative', zIndex: 1, textAlign: 'center' }}>
          <div className="badge badge-gold" style={{ marginBottom: 24 }}>🏆 {slotsLeft} of {TOTAL_SLOTS} Slots Remaining</div>
          <h1 style={{ background: 'linear-gradient(135deg, var(--gold-400), var(--text-primary))', WebkitBackgroundClip: 'text', WebkitTextFillColor: 'transparent', backgroundClip: 'text', marginBottom: 20 }}>
            The Synthos Inner Circle
          </h1>
          <p style={{ fontSize: '1.1rem', color: 'var(--text-secondary)', maxWidth: 600, margin: '0 auto 40px', lineHeight: 1.8 }}>
            Founding membership in the Synthos Collective. 200M SYN per slot. 100 members. Forever.
          </p>
          {/* Slot progress */}
          <div style={{ maxWidth: 480, margin: '0 auto' }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 8 }}>
              <span style={{ fontSize: '0.82rem', color: 'var(--text-muted)' }}>Slots claimed</span>
              <span style={{ fontSize: '0.82rem', fontWeight: 700, color: 'var(--gold-400)' }}>{stats.innerCircleSlotsFilled}/{TOTAL_SLOTS}</span>
            </div>
            <div className="progress-bar" style={{ height: 10 }}>
              <div className="progress-fill" style={{ width: `${(stats.innerCircleSlotsFilled / TOTAL_SLOTS) * 100}%`, background: 'linear-gradient(90deg, var(--gold-600), var(--gold-400))' }} />
            </div>
          </div>
        </div>
      </section>

      {/* Perks */}
      <section className="section">
        <div className="container">
          <div className="text-center" style={{ marginBottom: 48 }}>
            <span className="label">Member Benefits</span>
            <h2>What Inner Circle Members Receive</h2>
          </div>
          <div className="grid-3">
            {PERKS.map(p => (
              <div key={p.title} className="card" style={{ borderTop: '2px solid var(--gold-400)' }}>
                <div style={{ fontSize: '2rem', marginBottom: 12 }}>{p.icon}</div>
                <h3 style={{ fontSize: '1.1rem', marginBottom: 8 }}>{p.title}</h3>
                <p style={{ fontSize: '0.9rem' }}>{p.desc}</p>
              </div>
            ))}
          </div>
        </div>
      </section>

      {/* Registration + Purchase */}
      <section className="section" style={{ background: 'var(--obsidian-800)', borderTop: '1px solid var(--border-subtle)' }}>
        <div className="container">
          <div style={{ maxWidth: 660, margin: '0 auto' }}>
            <div className="text-center" style={{ marginBottom: 40 }}>
              <span className="label">Claim Your Slot</span>
              <h2>Join the Inner Circle</h2>
              <p style={{ marginTop: 12 }}>Step 1: Verify your email. Step 2: Send 200M SYN on-chain.</p>
            </div>

            {/* STEP 1 — Email Verification */}
            <div className="card" style={{ marginBottom: 24, borderLeft: `3px solid ${verified ? 'var(--green-400)' : 'var(--syn-400)'}` }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 20 }}>
                <div style={{ width: 28, height: 28, borderRadius: '50%', background: verified ? 'var(--green-400)' : 'var(--syn-400)', display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: '0.8rem', fontWeight: 700, color: 'var(--obsidian-900)', flexShrink: 0 }}>{verified ? '✓' : '1'}</div>
                <h3 style={{ fontSize: '1.1rem' }}>Verify Your Email</h3>
                {verified && <span className="badge badge-green" style={{ marginLeft: 'auto', fontSize: '0.72rem' }}>Verified ✓</span>}
              </div>

              {!verified && (
                <>
                  {!codeSent ? (
                    <form onSubmit={sendCode}>
                      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16 }}>
                        <div className="form-group">
                          <label>Full Name *</label>
                          <input className="input" placeholder="Your name" value={name} onChange={e => setName(e.target.value)} required id="ic-name" />
                        </div>
                        <div className="form-group">
                          <label>Phone Number</label>
                          <input className="input" placeholder="+1 (555) 000-0000" value={phone} onChange={e => setPhone(e.target.value)} id="ic-phone" />
                        </div>
                      </div>
                      <div className="form-group">
                        <label>Email Address *</label>
                        <input className="input" type="email" placeholder="your@email.com" value={email} onChange={e => setEmail(e.target.value)} required id="ic-email" />
                      </div>
                      {msg && <div className={`alert alert-${msg.type === 'error' ? 'error' : 'success'}`} style={{ marginBottom: 16 }}>{msg.text}</div>}
                      <button type="submit" className="btn btn-primary w-full" style={{ justifyContent: 'center' }} disabled={sending}>
                        {sending ? 'Sending...' : '📧 Send 6-Digit Verification Code'}
                      </button>
                    </form>
                  ) : (
                    <form onSubmit={verifyCode}>
                      <p style={{ color: 'var(--text-muted)', fontSize: '0.9rem', marginBottom: 16 }}>
                        A 6-digit code was sent to <strong style={{ color: 'var(--text-primary)' }}>{email}</strong>. Check your inbox.
                      </p>
                      {devCode && (
                        <div className="alert alert-warning" style={{ marginBottom: 16, fontSize: '0.82rem' }}>
                          🛠️ Dev mode — SMTP not configured. Your code is: <strong style={{ fontFamily: 'var(--font-mono)', fontSize: '1.1rem' }}>{devCode}</strong>
                        </div>
                      )}
                      <div className="form-group">
                        <label>6-Digit Code</label>
                        <input
                          className="input input-mono"
                          placeholder="000000"
                          maxLength={6}
                          value={code}
                          onChange={e => setCode(e.target.value.replace(/\D/g, ''))}
                          required
                          style={{ fontSize: '1.5rem', letterSpacing: '0.4em', textAlign: 'center' }}
                          id="ic-code"
                        />
                      </div>
                      {msg && <div className={`alert alert-${msg.type === 'error' ? 'error' : 'success'}`} style={{ marginBottom: 16 }}>{msg.text}</div>}
                      <div style={{ display: 'flex', gap: 12 }}>
                        <button type="button" className="btn btn-secondary" onClick={() => { setCodeSent(false); setMsg(null); setDevCode(null) }}>← Back</button>
                        <button type="submit" className="btn btn-primary w-full" style={{ justifyContent: 'center' }} disabled={verifying || code.length !== 6}>
                          {verifying ? 'Verifying...' : 'Verify Code'}
                        </button>
                      </div>
                      <button type="button" onClick={sendCode} style={{ background: 'none', border: 'none', color: 'var(--syn-400)', cursor: 'pointer', fontSize: '0.82rem', marginTop: 12 }} disabled={sending}>
                        {sending ? 'Sending...' : 'Resend code'}
                      </button>
                    </form>
                  )}
                </>
              )}
              {verified && (
                <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>
                  ✅ <strong style={{ color: 'var(--text-primary)' }}>{name}</strong> ({email}) is verified and saved to the member registry.
                </div>
              )}
            </div>

            {/* STEP 2 — Chain Purchase */}
            <div className="card" style={{ borderLeft: `3px solid ${verified ? 'var(--gold-400)' : 'var(--border-subtle)'}`, opacity: verified ? 1 : 0.5, pointerEvents: verified ? 'auto' : 'none' }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 20 }}>
                <div style={{ width: 28, height: 28, borderRadius: '50%', background: purchaseResult?.ok ? 'var(--green-400)' : 'var(--gold-400)', display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: '0.8rem', fontWeight: 700, color: 'var(--obsidian-900)', flexShrink: 0 }}>{purchaseResult?.ok ? '✓' : '2'}</div>
                <h3 style={{ fontSize: '1.1rem' }}>Send 200M SYN On-Chain</h3>
              </div>

              {!purchaseResult ? (
                <form onSubmit={handlePurchase}>
                  <div className="alert alert-warning" style={{ marginBottom: 20, fontSize: '0.82rem' }}>
                    ⚠️ You need 200,000,000 SYN in your wallet. Sign the transaction with your Ed25519 key first.
                  </div>
                  <div className="form-group">
                    <label>Your SYN Address *</label>
                    <input className="input input-mono" placeholder="0x..." value={walletAddr} onChange={e => setWalletAddr(e.target.value)} required id="ic-wallet" />
                  </div>
                  <div className="form-group">
                    <label>Public Key (0x hex) *</label>
                    <input className="input input-mono" placeholder="0x..." value={publicKey} onChange={e => setPublicKey(e.target.value)} required id="ic-pubkey" />
                  </div>
                  <div className="form-group">
                    <label>Signature (0x hex) *</label>
                    <input className="input input-mono" placeholder="0x..." value={signature} onChange={e => setSignature(e.target.value)} required id="ic-sig" />
                  </div>
                  <div className="form-group">
                    <label>Nonce</label>
                    <input className="input input-mono" type="number" placeholder="0" value={nonce} onChange={e => setNonce(e.target.value)} id="ic-nonce" />
                  </div>
                  <div style={{ background: 'var(--obsidian-800)', borderRadius: 10, padding: 16, marginBottom: 20, border: '1px solid var(--border-medium)' }}>
                    <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 8 }}>
                      <span style={{ fontSize: '0.85rem', color: 'var(--text-muted)' }}>Amount</span>
                      <span style={{ fontFamily: 'var(--font-mono)', fontWeight: 700, color: 'var(--gold-400)' }}>200,000,000 SYN</span>
                    </div>
                    <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                      <span style={{ fontSize: '0.85rem', color: 'var(--text-muted)' }}>Destination</span>
                      <span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.72rem', color: 'var(--text-secondary)' }}>0x2050...fbf2b (Founder)</span>
                    </div>
                  </div>
                  <button type="submit" className="btn btn-gold btn-lg w-full" style={{ justifyContent: 'center' }} disabled={purchasing || slotsLeft === 0}>
                    {slotsLeft === 0 ? 'All Slots Claimed' : purchasing ? 'Processing...' : '🏆 Claim Slot — 200M SYN'}
                  </button>
                </form>
              ) : purchaseResult.ok ? (
                <div style={{ textAlign: 'center' }}>
                  <div style={{ fontSize: '3rem', marginBottom: 12 }}>🏆</div>
                  <h3 style={{ color: 'var(--gold-400)', marginBottom: 8 }}>Welcome to the Inner Circle!</h3>
                  <div className="alert alert-success">TX: <span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.8rem' }}>{purchaseResult.txId}</span></div>
                </div>
              ) : (
                <div>
                  <div className="alert alert-warning" style={{ marginBottom: 12 }}>⚠️ RPC node offline — submit manually:</div>
                  <pre style={{ fontFamily: 'var(--font-mono)', fontSize: '0.78rem', background: 'var(--obsidian-900)', padding: 12, borderRadius: 8, overflowX: 'auto', color: 'var(--text-secondary)', border: '1px solid var(--border-subtle)', whiteSpace: 'pre-wrap' }}>{purchaseResult.payload}</pre>
                  <button className="btn btn-secondary w-full" style={{ marginTop: 12, justifyContent: 'center' }} onClick={() => setPurchaseResult(null)}>Try Again</button>
                </div>
              )}
            </div>
          </div>
        </div>
      </section>
    </div>
  )
}
