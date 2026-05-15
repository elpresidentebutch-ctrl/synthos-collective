import { useState } from 'react'
import { getBalance, submitTx, TOKENOMICS } from '../api'

export default function Wallet() {
  const [lookupAddr, setLookupAddr] = useState('')
  const [balResult, setBalResult] = useState(null)
  const [balLoading, setBalLoading] = useState(false)

  const [sendForm, setSendForm] = useState({ from: '', to: '', amount: '', fee: TOKENOMICS.minFee, nonce: '', publicKey: '', signature: '' })
  const [sendResult, setSendResult] = useState(null)
  const [sendLoading, setSendLoading] = useState(false)

  async function handleBalLookup(e) {
    e.preventDefault()
    setBalLoading(true)
    setBalResult(null)
    try {
      const data = await getBalance(lookupAddr)
      setBalResult({ ok: true, balance: data.balance, address: data.address })
    } catch {
      // Simulate for demo when node offline
      const mockBal = Math.floor(Math.random() * 500_000_000) + 1_000_000
      setBalResult({ ok: true, balance: mockBal, address: lookupAddr, simulated: true })
    }
    setBalLoading(false)
  }

  async function handleSend(e) {
    e.preventDefault()
    setSendLoading(true)
    setSendResult(null)
    try {
      const tx = {
        id: '0x' + Array.from({ length: 64 }, () => Math.floor(Math.random() * 16).toString(16)).join(''),
        chain_id: 1,
        from: sendForm.from,
        to: sendForm.to,
        amount: parseInt(sendForm.amount),
        fee: parseInt(sendForm.fee) || TOKENOMICS.minFee,
        nonce: parseInt(sendForm.nonce) || 0,
        public_key: sendForm.publicKey,
        signature: sendForm.signature,
        asset_id: 'syn',
      }
      const res = await submitTx(tx)
      setSendResult({ ok: true, txId: res.tx_id || tx.id, msg: 'Transaction broadcast to the Synthos L1 mempool.' })
    } catch (err) {
      // Generate the unsigned payload for manual signing
      setSendResult({
        ok: false,
        payload: JSON.stringify({
          chain_id: 1,
          from: sendForm.from,
          to: sendForm.to,
          amount: parseInt(sendForm.amount),
          fee: parseInt(sendForm.fee) || TOKENOMICS.minFee,
          nonce: parseInt(sendForm.nonce) || 0,
          asset_id: 'syn',
        }, null, 2),
        msg: err.message,
      })
    }
    setSendLoading(false)
  }

  return (
    <div className="page-content page-enter">
      <div style={{ background: 'var(--obsidian-800)', borderBottom: '1px solid var(--border-subtle)', padding: '48px 0 32px' }}>
        <div className="container">
          <span className="label">Ed25519 · synthos-1</span>
          <h1 style={{ fontSize: '2.4rem', marginBottom: 8 }}>SYN Wallet</h1>
          <p style={{ color: 'var(--text-muted)' }}>
            Check balances, build transactions, and submit signed payloads directly to the Synthos L1 network.
          </p>
        </div>
      </div>

      <div className="container" style={{ padding: '40px 24px' }}>
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 40 }}>

          {/* Balance Lookup */}
          <div>
            <h3 style={{ marginBottom: 20 }}>Check Balance</h3>
            <div className="card">
              <form onSubmit={handleBalLookup}>
                <div className="form-group">
                  <label>Wallet Address</label>
                  <input
                    className="input input-mono"
                    placeholder="0x..."
                    value={lookupAddr}
                    onChange={e => setLookupAddr(e.target.value)}
                    required
                    id="wallet-lookup-address"
                  />
                </div>
                <button type="submit" className="btn btn-primary w-full" style={{ justifyContent: 'center' }} disabled={balLoading}>
                  {balLoading ? 'Querying...' : 'Look Up Balance'}
                </button>
              </form>

              {balResult && (
                <div style={{ marginTop: 24, padding: 20, background: 'var(--obsidian-800)', borderRadius: 10, border: '1px solid var(--border-medium)' }}>
                  {balResult.simulated && (
                    <div style={{ fontSize: '0.75rem', color: 'var(--gold-400)', marginBottom: 12 }}>⚡ Node offline — showing simulated balance</div>
                  )}
                  <div style={{ fontSize: '0.78rem', color: 'var(--text-muted)', marginBottom: 6 }}>Address</div>
                  <div style={{ fontFamily: 'var(--font-mono)', fontSize: '0.82rem', color: 'var(--text-secondary)', wordBreak: 'break-all', marginBottom: 16 }}>
                    {balResult.address}
                  </div>
                  <div style={{ fontSize: '0.78rem', color: 'var(--text-muted)', marginBottom: 4 }}>Balance</div>
                  <div style={{ fontFamily: 'var(--font-display)', fontSize: '2.4rem', fontWeight: 800, color: 'var(--syn-400)' }}>
                    {balResult.balance.toLocaleString()}
                  </div>
                  <div style={{ fontSize: '0.88rem', color: 'var(--text-muted)' }}>SYN</div>
                </div>
              )}
            </div>

            {/* Founder Address Quick Check */}
            <div className="card" style={{ marginTop: 16, padding: 20 }}>
              <div style={{ fontSize: '0.78rem', color: 'var(--syn-400)', fontWeight: 700, letterSpacing: '0.1em', textTransform: 'uppercase', marginBottom: 12 }}>Founder Address</div>
              <div style={{ fontFamily: 'var(--font-mono)', fontSize: '0.78rem', color: 'var(--text-muted)', wordBreak: 'break-all', marginBottom: 12 }}>
                0x205042f06cd3aa7d9a88deec39b9d0ba6b9fbf2b
              </div>
              <button
                className="btn btn-secondary btn-sm"
                onClick={() => { setLookupAddr('0x205042f06cd3aa7d9a88deec39b9d0ba6b9fbf2b') }}
              >
                Check Founder Balance
              </button>
            </div>
          </div>

          {/* Send SYN */}
          <div>
            <h3 style={{ marginBottom: 20 }}>Send SYN</h3>
            <div className="card">
              <div className="alert alert-info" style={{ marginBottom: 24, fontSize: '0.82rem' }}>
                You must sign the transaction with your Ed25519 private key before submitting.
                Use <span style={{ fontFamily: 'var(--font-mono)' }}>wallet.exe</span> to generate a signed payload.
              </div>
              <form onSubmit={handleSend}>
                <div className="form-group">
                  <label>From Address *</label>
                  <input className="input input-mono" placeholder="0x..." value={sendForm.from} onChange={e => setSendForm(f => ({ ...f, from: e.target.value }))} required id="wallet-from" />
                </div>
                <div className="form-group">
                  <label>To Address *</label>
                  <input className="input input-mono" placeholder="0x..." value={sendForm.to} onChange={e => setSendForm(f => ({ ...f, to: e.target.value }))} required id="wallet-to" />
                </div>
                <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16 }}>
                  <div className="form-group">
                    <label>Amount (SYN) *</label>
                    <input className="input input-mono" type="number" placeholder="0" value={sendForm.amount} onChange={e => setSendForm(f => ({ ...f, amount: e.target.value }))} required id="wallet-amount" />
                  </div>
                  <div className="form-group">
                    <label>Fee (SYN)</label>
                    <input className="input input-mono" type="number" placeholder="10" value={sendForm.fee} onChange={e => setSendForm(f => ({ ...f, fee: e.target.value }))} id="wallet-fee" />
                  </div>
                </div>
                <div className="form-group">
                  <label>Nonce</label>
                  <input className="input input-mono" type="number" placeholder="0" value={sendForm.nonce} onChange={e => setSendForm(f => ({ ...f, nonce: e.target.value }))} id="wallet-nonce" />
                </div>
                <div className="form-group">
                  <label>Public Key (0x hex)</label>
                  <input className="input input-mono" placeholder="0x..." value={sendForm.publicKey} onChange={e => setSendForm(f => ({ ...f, publicKey: e.target.value }))} id="wallet-pubkey" />
                </div>
                <div className="form-group">
                  <label>Signature (0x hex)</label>
                  <input className="input input-mono" placeholder="0x..." value={sendForm.signature} onChange={e => setSendForm(f => ({ ...f, signature: e.target.value }))} id="wallet-sig" />
                </div>

                <button type="submit" className="btn btn-primary w-full" style={{ justifyContent: 'center' }} disabled={sendLoading}>
                  {sendLoading ? 'Broadcasting...' : '⚡ Broadcast Transaction'}
                </button>
              </form>

              {sendResult && sendResult.ok && (
                <div className="alert alert-success" style={{ marginTop: 16 }}>
                  ✅ {sendResult.msg}<br />
                  <span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.78rem' }}>{sendResult.txId}</span>
                </div>
              )}
              {sendResult && !sendResult.ok && (
                <div style={{ marginTop: 16 }}>
                  <div className="alert alert-warning" style={{ marginBottom: 10 }}>
                    ⚠️ Node offline — unsigned payload ready for manual signing:
                  </div>
                  <pre style={{ fontFamily: 'var(--font-mono)', fontSize: '0.78rem', background: 'var(--obsidian-900)', padding: 14, borderRadius: 8, overflowX: 'auto', color: 'var(--text-secondary)', border: '1px solid var(--border-subtle)' }}>
                    {sendResult.payload}
                  </pre>
                </div>
              )}
            </div>
          </div>
        </div>

        {/* Network info */}
        <div className="card" style={{ marginTop: 32, padding: 24 }}>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, 1fr)', gap: 24 }}>
            {[
              { label: 'Chain ID', value: 'synthos-1', mono: true },
              { label: 'Min Fee', value: `${TOKENOMICS.minFee} SYN`, mono: true },
              { label: 'Signature Scheme', value: 'Ed25519', mono: true },
              { label: 'Tx Format', value: 'JSON / SHA-256 ID', mono: false },
            ].map(i => (
              <div key={i.label} style={{ textAlign: 'center' }}>
                <div style={{ fontSize: '0.75rem', color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.08em', marginBottom: 6 }}>{i.label}</div>
                <div style={{ fontFamily: i.mono ? 'var(--font-mono)' : 'inherit', fontWeight: 600, color: 'var(--syn-400)', fontSize: '0.95rem' }}>{i.value}</div>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  )
}
