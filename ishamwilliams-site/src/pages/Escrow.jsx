import { useState } from 'react'
import { submitTx, TOKENOMICS } from '../api'

const ESCROW_COMMISSION = TOKENOMICS.escrowCommission // 1%

const STAGES = ['Create', 'Review', 'Submit']

export default function Escrow() {
  const [stage, setStage] = useState(0)
  const [form, setForm] = useState({
    from: '',
    to: '',
    amount: '',
    purpose: '',
    publicKey: '',
    signature: '',
    nonce: '',
    chainId: 'synthos-1',
  })
  const [submitting, setSubmitting] = useState(false)
  const [result, setResult] = useState(null)
  const [activeEscrows] = useState([
    { id: '0xabc123...', party1: '0x2050...fbf2b', party2: '0xa1b2...5678', amount: 5_000_000, status: 'locked', created: '2 days ago', purpose: 'Software development milestone' },
    { id: '0xdef456...', party1: '0xcafe...c0de', party2: '0x1111...1111', amount: 12_500_000, status: 'locked', created: '5 hours ago', purpose: 'Real estate deposit' },
    { id: '0x789abc...', party1: '0xdead...0001', party2: '0x2050...fbf2b', amount: 200_000_000, status: 'released', created: '1 week ago', purpose: 'Inner Circle slot purchase' },
  ])

  const amount = parseFloat(form.amount) || 0
  const commission = Math.floor(amount * ESCROW_COMMISSION / 100)
  const totalDeducted = amount + commission

  async function handleSubmit() {
    setSubmitting(true)
    setResult(null)
    try {
      const tx = {
        id: '0x' + Array.from({ length: 64 }, () => Math.floor(Math.random() * 16).toString(16)).join(''),
        chain_id: 1,
        from: form.from,
        to: form.to,
        amount: parseInt(form.amount),
        fee: TOKENOMICS.minFee,
        nonce: parseInt(form.nonce) || 0,
        public_key: form.publicKey,
        signature: form.signature,
        metadata: [
          { key: 'type', value: 'escrow_lock' },
          { key: 'purpose', value: form.purpose },
        ],
      }
      const res = await submitTx(tx)
      setResult({ ok: true, txId: res.tx_id || tx.id })
    } catch (err) {
      // Show the constructed TX for manual submission when node is offline
      setResult({
        ok: false,
        manual: true,
        tx: {
          from: form.from,
          to: form.to,
          amount,
          commission,
          purpose: form.purpose,
          note: 'Node offline — copy this payload and submit to your RPC node at POST /submitTx'
        }
      })
    }
    setSubmitting(false)
  }

  return (
    <div className="page-content page-enter">
      <div style={{ background: 'var(--obsidian-800)', borderBottom: '1px solid var(--border-subtle)', padding: '48px 0 32px' }}>
        <div className="container">
          <span className="label">Protocol-Level Trustless Escrow</span>
          <h1 style={{ fontSize: '2.4rem', marginBottom: 8 }}>Sovereign Escrow</h1>
          <p style={{ color: 'var(--text-muted)', maxWidth: 620 }}>
            Lock SYN tokens in an immutable on-chain escrow. {ESCROW_COMMISSION}% commission goes to the Founder's address
            automatically. No intermediaries. No chargebacks. Cryptographically enforced.
          </p>
        </div>
      </div>

      <div className="container" style={{ padding: '40px 24px' }}>
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 400px', gap: 40, alignItems: 'start' }}>

          {/* Left — Create Escrow */}
          <div>
            {/* Progress */}
            <div style={{ display: 'flex', alignItems: 'center', gap: 0, marginBottom: 40 }}>
              {STAGES.map((s, i) => (
                <div key={s} style={{ display: 'flex', alignItems: 'center' }}>
                  <div
                    style={{
                      width: 36, height: 36, borderRadius: '50%',
                      background: i <= stage ? 'var(--syn-400)' : 'var(--obsidian-600)',
                      border: `2px solid ${i <= stage ? 'var(--syn-400)' : 'var(--border-subtle)'}`,
                      display: 'flex', alignItems: 'center', justifyContent: 'center',
                      fontWeight: 700, fontSize: '0.85rem',
                      color: i <= stage ? 'var(--obsidian-900)' : 'var(--text-muted)',
                      cursor: i < stage ? 'pointer' : 'default',
                      transition: 'all 0.3s',
                    }}
                    onClick={() => i < stage && setStage(i)}
                  >
                    {i < stage ? '✓' : i + 1}
                  </div>
                  <div style={{ marginLeft: 10, marginRight: i < STAGES.length - 1 ? 24 : 0 }}>
                    <div style={{ fontSize: '0.82rem', fontWeight: i === stage ? 600 : 400, color: i === stage ? 'var(--text-primary)' : 'var(--text-muted)' }}>{s}</div>
                  </div>
                  {i < STAGES.length - 1 && (
                    <div style={{ height: 2, width: 40, background: i < stage ? 'var(--syn-400)' : 'var(--border-subtle)', marginRight: 10, transition: 'background 0.3s' }} />
                  )}
                </div>
              ))}
            </div>

            {/* Stage 0 — Create */}
            {stage === 0 && (
              <div className="card">
                <h3 style={{ marginBottom: 24 }}>Escrow Details</h3>
                <div className="form-group">
                  <label>Your Address (From) *</label>
                  <input className="input input-mono" placeholder="0x..." value={form.from} onChange={e => setForm(f => ({ ...f, from: e.target.value }))} id="escrow-from" />
                </div>
                <div className="form-group">
                  <label>Counterparty Address (To) *</label>
                  <input className="input input-mono" placeholder="0x..." value={form.to} onChange={e => setForm(f => ({ ...f, to: e.target.value }))} id="escrow-to" />
                </div>
                <div className="form-group">
                  <label>Amount (SYN) *</label>
                  <input className="input input-mono" type="number" placeholder="e.g. 5000000" value={form.amount} onChange={e => setForm(f => ({ ...f, amount: e.target.value }))} id="escrow-amount" />
                  {amount > 0 && (
                    <div style={{ marginTop: 8, padding: '10px 14px', background: 'var(--obsidian-800)', borderRadius: 8, border: '1px solid var(--border-subtle)' }}>
                      <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '0.85rem', marginBottom: 4 }}>
                        <span style={{ color: 'var(--text-muted)' }}>Lock amount</span>
                        <span style={{ fontFamily: 'var(--font-mono)' }}>{amount.toLocaleString()} SYN</span>
                      </div>
                      <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '0.85rem', marginBottom: 4 }}>
                        <span style={{ color: 'var(--text-muted)' }}>Founder commission (1%)</span>
                        <span style={{ fontFamily: 'var(--font-mono)', color: 'var(--gold-400)' }}>{commission.toLocaleString()} SYN</span>
                      </div>
                      <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '0.88rem', fontWeight: 600, borderTop: '1px solid var(--border-subtle)', paddingTop: 8 }}>
                        <span>Total deducted</span>
                        <span style={{ fontFamily: 'var(--font-mono)', color: 'var(--text-primary)' }}>{totalDeducted.toLocaleString()} SYN</span>
                      </div>
                    </div>
                  )}
                </div>
                <div className="form-group">
                  <label>Purpose / Description</label>
                  <textarea className="input" rows={3} placeholder="Describe what this escrow secures..." value={form.purpose} onChange={e => setForm(f => ({ ...f, purpose: e.target.value }))} style={{ resize: 'vertical' }} id="escrow-purpose" />
                </div>
                <button
                  className="btn btn-primary w-full"
                  style={{ justifyContent: 'center' }}
                  onClick={() => {
                    if (!form.from || !form.to || !form.amount) return
                    setStage(1)
                  }}
                  disabled={!form.from || !form.to || !form.amount}
                >
                  Continue to Review →
                </button>
              </div>
            )}

            {/* Stage 1 — Review */}
            {stage === 1 && (
              <div className="card">
                <h3 style={{ marginBottom: 24 }}>Review Escrow Transaction</h3>
                <div style={{ display: 'flex', flexDirection: 'column', gap: 12, marginBottom: 28 }}>
                  {[
                    { label: 'From', value: form.from, mono: true },
                    { label: 'To', value: form.to, mono: true },
                    { label: 'Lock Amount', value: `${amount.toLocaleString()} SYN`, accent: true },
                    { label: 'Founder Commission (1%)', value: `${commission.toLocaleString()} SYN`, gold: true },
                    { label: 'Total Deducted', value: `${totalDeducted.toLocaleString()} SYN`, bold: true },
                    { label: 'Purpose', value: form.purpose || '—' },
                    { label: 'Metadata Type', value: 'escrow_lock', mono: true },
                    { label: 'Chain ID', value: 'synthos-1', mono: true },
                  ].map(({ label, value, mono, accent, gold, bold }) => (
                    <div key={label} style={{ display: 'flex', justifyContent: 'space-between', padding: '10px 0', borderBottom: '1px solid var(--border-subtle)' }}>
                      <span style={{ fontSize: '0.85rem', color: 'var(--text-muted)' }}>{label}</span>
                      <span style={{
                        fontFamily: mono ? 'var(--font-mono)' : 'inherit',
                        fontSize: mono ? '0.82rem' : '0.9rem',
                        fontWeight: bold ? 700 : 500,
                        color: accent ? 'var(--syn-400)' : gold ? 'var(--gold-400)' : 'var(--text-primary)',
                        wordBreak: 'break-all', textAlign: 'right', maxWidth: '60%',
                      }}>
                        {value}
                      </span>
                    </div>
                  ))}
                </div>

                <div className="alert alert-warning" style={{ marginBottom: 24 }}>
                  ⚠️ To submit this transaction, you need your Ed25519 private key to sign the payload. Provide your public key and signature below, or use the /submitTx RPC endpoint directly.
                </div>

                <div className="form-group">
                  <label>Public Key (0x hex)</label>
                  <input className="input input-mono" placeholder="0x..." value={form.publicKey} onChange={e => setForm(f => ({ ...f, publicKey: e.target.value }))} id="escrow-pubkey" />
                </div>
                <div className="form-group">
                  <label>Signature (0x hex)</label>
                  <input className="input input-mono" placeholder="0x..." value={form.signature} onChange={e => setForm(f => ({ ...f, signature: e.target.value }))} id="escrow-sig" />
                </div>
                <div className="form-group">
                  <label>Nonce (from your account state)</label>
                  <input className="input input-mono" type="number" placeholder="0" value={form.nonce} onChange={e => setForm(f => ({ ...f, nonce: e.target.value }))} id="escrow-nonce" />
                </div>

                <div style={{ display: 'flex', gap: 12 }}>
                  <button className="btn btn-secondary" onClick={() => setStage(0)} style={{ flex: '0 0 auto' }}>← Back</button>
                  <button className="btn btn-primary w-full" style={{ justifyContent: 'center' }} onClick={() => setStage(2)}>
                    Proceed to Submit →
                  </button>
                </div>
              </div>
            )}

            {/* Stage 2 — Submit */}
            {stage === 2 && (
              <div className="card">
                <h3 style={{ marginBottom: 16 }}>Submit to Chain</h3>
                <p style={{ color: 'var(--text-muted)', marginBottom: 24, fontSize: '0.9rem' }}>
                  This will broadcast your signed transaction to the Synthos L1 RPC node. The escrow will be locked on-chain upon confirmation.
                </p>

                {result && result.ok && (
                  <div className="alert alert-success" style={{ marginBottom: 20 }}>
                    ✅ Escrow locked on-chain! TX: <span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.82rem' }}>{result.txId}</span>
                  </div>
                )}
                {result && !result.ok && result.manual && (
                  <div className="alert alert-warning" style={{ marginBottom: 20, flexDirection: 'column', gap: 8 }}>
                    <div>⚠️ Node offline — submit manually to your RPC node at <span style={{ fontFamily: 'var(--font-mono)' }}>POST /submitTx</span></div>
                    <div style={{ fontFamily: 'var(--font-mono)', fontSize: '0.78rem', background: 'var(--obsidian-900)', padding: '10px', borderRadius: 6, marginTop: 8, overflowX: 'auto' }}>
                      {JSON.stringify({ type: 'escrow_lock', ...result.tx }, null, 2)}
                    </div>
                  </div>
                )}

                {!result && (
                  <button
                    className="btn btn-gold btn-lg w-full"
                    style={{ justifyContent: 'center' }}
                    onClick={handleSubmit}
                    disabled={submitting}
                  >
                    {submitting ? 'Broadcasting...' : '🔐 Lock Escrow On-Chain'}
                  </button>
                )}
                {result && (
                  <button className="btn btn-secondary w-full" style={{ justifyContent: 'center', marginTop: 12 }} onClick={() => { setStage(0); setResult(null); setForm({ from: '', to: '', amount: '', purpose: '', publicKey: '', signature: '', nonce: '', chainId: 'synthos-1' }) }}>
                    Create Another Escrow
                  </button>
                )}
              </div>
            )}
          </div>

          {/* Right — Active Escrows */}
          <div>
            <h3 style={{ marginBottom: 20, fontSize: '1.1rem' }}>Active Escrows</h3>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
              {activeEscrows.map(e => (
                <div key={e.id} className="card" style={{ padding: 20, borderLeft: `3px solid ${e.status === 'released' ? 'var(--green-400)' : 'var(--gold-400)'}` }}>
                  <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 8 }}>
                    <span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.82rem', color: 'var(--text-muted)' }}>{e.id}</span>
                    <span className={`badge ${e.status === 'released' ? 'badge-green' : 'badge-gold'}`} style={{ fontSize: '0.7rem' }}>
                      {e.status}
                    </span>
                  </div>
                  <div style={{ fontWeight: 700, fontSize: '1.1rem', color: 'var(--syn-400)', marginBottom: 4 }}>
                    {e.amount.toLocaleString()} SYN
                  </div>
                  <div style={{ fontSize: '0.8rem', color: 'var(--text-secondary)', marginBottom: 8 }}>{e.purpose}</div>
                  <div style={{ fontSize: '0.75rem', color: 'var(--text-muted)' }}>
                    {e.party1.slice(0, 12)}... ↔ {e.party2.slice(0, 12)}... · {e.created}
                  </div>
                </div>
              ))}
            </div>

            {/* Commission Info */}
            <div className="card" style={{ padding: 20, marginTop: 20, borderColor: 'var(--gold-400)', background: 'linear-gradient(135deg, rgba(255,213,79,0.04) 0%, transparent 100%)' }}>
              <div style={{ fontSize: '0.78rem', color: 'var(--gold-400)', fontWeight: 700, letterSpacing: '0.1em', textTransform: 'uppercase', marginBottom: 12 }}>
                Founder Commission Model
              </div>
              <p style={{ fontSize: '0.88rem', color: 'var(--text-secondary)', marginBottom: 12 }}>
                Every escrow transaction routes a <strong style={{ color: 'var(--gold-400)' }}>1% commission</strong> directly to the Founder's address:
              </p>
              <div style={{ fontFamily: 'var(--font-mono)', fontSize: '0.72rem', color: 'var(--text-muted)', background: 'var(--obsidian-900)', padding: '8px 12px', borderRadius: 6, wordBreak: 'break-all' }}>
                0x205042f06cd3aa7d9a88deec39b9d0ba6b9fbf2b
              </div>
              <p style={{ fontSize: '0.8rem', color: 'var(--text-muted)', marginTop: 10 }}>
                This is enforced at the protocol level in <span style={{ fontFamily: 'var(--font-mono)', color: 'var(--syn-400)' }}>core.go:ApplyTx()</span> — not a smart contract, but hardcoded consensus rules.
              </p>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
