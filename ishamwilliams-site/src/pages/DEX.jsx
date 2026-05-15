import { useState, useEffect } from 'react'
import { BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer, Cell } from 'recharts'

const API = 'http://localhost:4000/api'

const DEFAULT_POOLS = [
  { assetId: 'USDC', synReserve: 4_200_000_000, assetReserve: 173040, totalShares: 4_200_000_000, apy: 18.4, volume24h: 87200 },
  { assetId: 'ETH',  synReserve: 2_100_000_000, assetReserve: 18.2,   totalShares: 2_100_000_000, apy: 24.1, volume24h: 42100 },
  { assetId: 'BTC',  synReserve: 980_000_000,   assetReserve: 0.94,   totalShares: 980_000_000,   apy: 31.7, volume24h: 28800 },
  { assetId: 'SOL',  synReserve: 750_000_000,   assetReserve: 1240,   totalShares: 750_000_000,   apy: 22.9, volume24h: 19300 },
]

export default function DEX() {
  const [pools, setPools] = useState(DEFAULT_POOLS)
  const [selected, setSelected] = useState(DEFAULT_POOLS[0])
  const [tab, setTab] = useState('swap')
  const [fromSyn, setFromSyn] = useState(true)
  const [amountIn, setAmountIn] = useState('')
  const [quote, setQuote] = useState(null)
  const [swapResult, setSwapResult] = useState(null)
  const [swapping, setSwapping] = useState(false)
  const [addSyn, setAddSyn] = useState('')
  const [addAsset, setAddAsset] = useState('')
  const [liqResult, setLiqResult] = useState(null)
  const [addingLiq, setAddingLiq] = useState(false)
  const [removeShares, setRemoveShares] = useState('')
  const [removeAddr, setRemoveAddr] = useState('')
  const [removeResult, setRemoveResult] = useState(null)
  const [history, setHistory] = useState([])
  const [swapAddr, setSwapAddr] = useState('')
  const [liqAddr, setLiqAddr] = useState('')

  useEffect(() => {
    fetchPools()
    fetchHistory()
    const iv = setInterval(() => { fetchPools(); fetchHistory() }, 10000)
    return () => clearInterval(iv)
  }, [])

  async function fetchPools() {
    try {
      const r = await fetch(`${API}/dex/pools`)
      const d = await r.json()
      if (d.pools) {
        const arr = Object.values(d.pools).map((p, i) => ({ ...DEFAULT_POOLS[i] || {}, ...p }))
        setPools(arr)
        setSelected(prev => arr.find(p => p.assetId === prev.assetId) || arr[0])
      }
    } catch { /* use defaults */ }
  }

  async function fetchHistory() {
    try {
      const r = await fetch(`${API}/dex/history`)
      const d = await r.json()
      if (d.history) setHistory(d.history.slice(0, 8))
    } catch {}
  }

  // Real-time quote calculation (same AMM math as Go + server)
  function calcQuote(pool, amt, fSyn) {
    if (!amt || isNaN(amt) || parseFloat(amt) <= 0) return null
    const a = parseFloat(amt)
    const reserveIn  = fSyn ? pool.synReserve   : pool.assetReserve
    const reserveOut = fSyn ? pool.assetReserve  : pool.synReserve
    const aif = a * 997
    const amtOut = (aif * reserveOut) / (reserveIn * 1000 + aif)
    const impact = ((a / reserveIn) * 100).toFixed(4)
    const fee = a * 0.003
    return { amtOut, impact: parseFloat(impact), fee }
  }

  useEffect(() => {
    setQuote(calcQuote(selected, amountIn, fromSyn))
    setSwapResult(null)
  }, [amountIn, fromSyn, selected])

  async function executeSwap() {
    if (!quote || !amountIn) return
    setSwapping(true)
    setSwapResult(null)
    try {
      const r = await fetch(`${API}/dex/swap`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ assetId: selected.assetId, amountIn: parseFloat(amountIn), fromSyn, address: swapAddr || 'anonymous' })
      })
      const d = await r.json()
      if (!r.ok) throw new Error(d.error)
      setSwapResult({ ok: true, ...d.swap, pool: d.pool })
      setPools(prev => prev.map(p => p.assetId === selected.assetId ? { ...p, ...d.pool } : p))
      setSelected(prev => ({ ...prev, ...d.pool }))
      setAmountIn('')
      fetchHistory()
    } catch (err) {
      setSwapResult({ ok: false, error: err.message })
    }
    setSwapping(false)
  }

  async function executeLiquidity() {
    if (!addSyn || !addAsset) return
    setAddingLiq(true)
    setLiqResult(null)
    try {
      const r = await fetch(`${API}/dex/liquidity/add`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ assetId: selected.assetId, synAmount: parseFloat(addSyn), assetAmount: parseFloat(addAsset), address: liqAddr || 'anonymous' })
      })
      const d = await r.json()
      if (!r.ok) throw new Error(d.error)
      setLiqResult({ ok: true, ...d })
      setPools(prev => prev.map(p => p.assetId === selected.assetId ? { ...p, ...d.pool } : p))
      setSelected(prev => ({ ...prev, ...d.pool }))
      setAddSyn(''); setAddAsset('')
    } catch (err) {
      setLiqResult({ ok: false, error: err.message })
    }
    setAddingLiq(false)
  }

  async function executeRemove() {
    if (!removeShares || !removeAddr) return
    try {
      const r = await fetch(`${API}/dex/liquidity/remove`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ assetId: selected.assetId, shares: parseFloat(removeShares), address: removeAddr })
      })
      const d = await r.json()
      if (!r.ok) throw new Error(d.error)
      setRemoveResult({ ok: true, ...d })
      fetchPools()
    } catch (err) {
      setRemoveResult({ ok: false, error: err.message })
    }
  }

  const pool = selected
  const synPrice = 0.0000412
  const tvl = ((pool.synReserve * synPrice * 2) / 1000).toFixed(0)

  return (
    <div className="page-content page-enter">
      <div style={{ background: 'var(--obsidian-800)', borderBottom: '1px solid var(--border-subtle)', padding: '48px 0 32px' }}>
        <div className="container">
          <span className="label">x·y=k AMM · 0.3% Fee · Live State</span>
          <h1 style={{ fontSize: '2.4rem', marginBottom: 8 }}>Synthos DEX</h1>
          <p style={{ color: 'var(--text-muted)' }}>Real on-chain liquidity pools. Swaps execute and update pool reserves instantly.</p>
        </div>
      </div>

      <div className="container" style={{ padding: '40px 24px' }}>
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 400px', gap: 32, alignItems: 'start' }}>

          {/* Left */}
          <div>
            {/* Pool Cards */}
            <h3 style={{ marginBottom: 16 }}>Liquidity Pools</h3>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 10, marginBottom: 32 }}>
              {pools.map(p => (
                <div key={p.assetId} className="card" onClick={() => { setSelected(p); setQuote(null); setSwapResult(null); setLiqResult(null) }}
                  style={{ cursor: 'pointer', padding: '16px 20px', borderColor: selected.assetId === p.assetId ? 'var(--syn-400)' : 'var(--border-subtle)', boxShadow: selected.assetId === p.assetId ? 'var(--shadow-glow)' : 'none' }}>
                  <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: 12 }}>
                    <div style={{ fontWeight: 700 }}>SYN / {p.assetId}</div>
                    <div style={{ display: 'flex', gap: 24 }}>
                      <div style={{ textAlign: 'right' }}>
                        <div style={{ fontSize: '0.72rem', color: 'var(--text-muted)' }}>TVL</div>
                        <div style={{ fontWeight: 600, fontSize: '0.9rem' }}>${((p.synReserve * synPrice * 2) / 1000).toFixed(0)}K</div>
                      </div>
                      <div style={{ textAlign: 'right' }}>
                        <div style={{ fontSize: '0.72rem', color: 'var(--text-muted)' }}>APY</div>
                        <div style={{ fontWeight: 700, color: 'var(--green-400)' }}>{p.apy}%</div>
                      </div>
                      <div style={{ textAlign: 'right' }}>
                        <div style={{ fontSize: '0.72rem', color: 'var(--text-muted)' }}>SYN Reserve</div>
                        <div style={{ fontFamily: 'var(--font-mono)', fontSize: '0.82rem' }}>{(p.synReserve / 1e9).toFixed(3)}B</div>
                      </div>
                      <div style={{ textAlign: 'right' }}>
                        <div style={{ fontSize: '0.72rem', color: 'var(--text-muted)' }}>{p.assetId} Reserve</div>
                        <div style={{ fontFamily: 'var(--font-mono)', fontSize: '0.82rem' }}>{p.assetReserve.toLocaleString()}</div>
                      </div>
                    </div>
                  </div>
                </div>
              ))}
            </div>

            {/* Swap History */}
            <h3 style={{ marginBottom: 16 }}>Recent Swaps</h3>
            <div className="table-wrap">
              <table>
                <thead><tr><th>Pool</th><th>Direction</th><th>In</th><th>Out</th><th>Impact</th><th>Time</th></tr></thead>
                <tbody>
                  {history.length === 0 && (
                    <tr><td colSpan={6} style={{ textAlign: 'center', color: 'var(--text-muted)', padding: 24 }}>No swaps yet — be the first!</td></tr>
                  )}
                  {history.map(s => (
                    <tr key={s.id}>
                      <td style={{ fontFamily: 'var(--font-mono)', fontSize: '0.82rem' }}>SYN/{s.assetId}</td>
                      <td><span style={{ fontSize: '0.78rem', color: s.fromSyn ? 'var(--syn-400)' : 'var(--purple-400)' }}>{s.fromSyn ? 'SYN→' + s.assetId : s.assetId + '→SYN'}</span></td>
                      <td style={{ fontFamily: 'var(--font-mono)', fontSize: '0.82rem' }}>{parseFloat(s.amountIn).toLocaleString()}</td>
                      <td style={{ fontFamily: 'var(--font-mono)', fontSize: '0.82rem', color: 'var(--green-400)' }}>{parseFloat(s.amountOut).toFixed(6)}</td>
                      <td style={{ color: parseFloat(s.priceImpact) > 1 ? 'var(--red-400)' : 'var(--text-muted)', fontSize: '0.82rem' }}>{s.priceImpact}%</td>
                      <td style={{ fontSize: '0.78rem', color: 'var(--text-muted)' }}>{new Date(s.timestamp).toLocaleTimeString()}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>

          {/* Right — Trading Panel */}
          <div style={{ position: 'sticky', top: 'calc(var(--nav-h) + 24px)' }}>
            <div className="card" style={{ padding: 28 }}>

              {/* Tabs */}
              <div style={{ display: 'flex', gap: 4, marginBottom: 24, background: 'var(--obsidian-800)', borderRadius: 10, padding: 4 }}>
                {['swap', 'add', 'remove'].map(t => (
                  <button key={t} onClick={() => { setTab(t); setSwapResult(null); setLiqResult(null); setRemoveResult(null) }}
                    style={{ flex: 1, padding: '8px 0', borderRadius: 8, background: tab === t ? 'var(--obsidian-600)' : 'transparent', border: tab === t ? '1px solid var(--border-medium)' : 'none', color: tab === t ? 'var(--text-primary)' : 'var(--text-muted)', cursor: 'pointer', fontSize: '0.85rem', fontWeight: tab === t ? 600 : 400, transition: 'all 0.2s', textTransform: 'capitalize' }}>
                    {t === 'add' ? 'Add Liq.' : t === 'remove' ? 'Remove' : 'Swap'}
                  </button>
                ))}
              </div>

              {/* Selected pool display */}
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '10px 14px', background: 'var(--obsidian-800)', borderRadius: 8, border: '1px solid var(--border-subtle)', marginBottom: 20 }}>
                <span style={{ fontSize: '0.82rem', color: 'var(--text-muted)' }}>Pool: <span style={{ color: 'var(--syn-400)', fontWeight: 600 }}>SYN/{pool.assetId}</span></span>
                <span style={{ fontSize: '0.78rem', color: 'var(--text-muted)' }}>TVL: ${tvl}K</span>
              </div>

              {/* SWAP TAB */}
              {tab === 'swap' && (
                <div>
                  <div style={{ display: 'flex', gap: 8, marginBottom: 16 }}>
                    <button onClick={() => setFromSyn(true)} className="btn btn-sm" style={{ flex: 1, background: fromSyn ? 'var(--syn-glow)' : 'transparent', border: '1px solid var(--border-subtle)', color: fromSyn ? 'var(--syn-400)' : 'var(--text-muted)' }}>SYN → {pool.assetId}</button>
                    <button onClick={() => setFromSyn(false)} className="btn btn-sm" style={{ flex: 1, background: !fromSyn ? 'var(--syn-glow)' : 'transparent', border: '1px solid var(--border-subtle)', color: !fromSyn ? 'var(--syn-400)' : 'var(--text-muted)' }}>{pool.assetId} → SYN</button>
                  </div>

                  <div className="form-group">
                    <label>Amount ({fromSyn ? 'SYN' : pool.assetId})</label>
                    <input className="input input-mono" type="number" placeholder="0.00" value={amountIn} onChange={e => setAmountIn(e.target.value)} id="dex-amount-in" />
                  </div>
                  <div className="form-group">
                    <label>Your Address (optional)</label>
                    <input className="input input-mono" placeholder="0x..." value={swapAddr} onChange={e => setSwapAddr(e.target.value)} id="dex-swap-addr" />
                  </div>

                  {quote && amountIn && (
                    <div style={{ background: 'var(--obsidian-800)', borderRadius: 10, padding: 14, marginBottom: 16, border: '1px solid var(--border-subtle)' }}>
                      <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 8 }}>
                        <span style={{ fontSize: '0.82rem', color: 'var(--text-muted)' }}>You receive</span>
                        <span style={{ fontFamily: 'var(--font-mono)', fontWeight: 700, color: 'var(--syn-400)' }}>{quote.amtOut.toFixed(8)} {fromSyn ? pool.assetId : 'SYN'}</span>
                      </div>
                      <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 8 }}>
                        <span style={{ fontSize: '0.82rem', color: 'var(--text-muted)' }}>Fee (0.3%)</span>
                        <span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.82rem' }}>{quote.fee.toFixed(4)}</span>
                      </div>
                      <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                        <span style={{ fontSize: '0.82rem', color: 'var(--text-muted)' }}>Price impact</span>
                        <span style={{ color: quote.impact > 2 ? 'var(--red-400)' : quote.impact > 0.5 ? 'var(--gold-400)' : 'var(--green-400)', fontWeight: 600, fontSize: '0.82rem' }}>{quote.impact}%</span>
                      </div>
                    </div>
                  )}

                  {swapResult && swapResult.ok && (
                    <div className="alert alert-success" style={{ marginBottom: 12 }}>
                      ✅ Swapped! Received {parseFloat(swapResult.amountOut).toFixed(8)} {fromSyn ? pool.assetId : 'SYN'}
                    </div>
                  )}
                  {swapResult && !swapResult.ok && (
                    <div className="alert alert-error" style={{ marginBottom: 12 }}>⚠️ {swapResult.error}</div>
                  )}

                  <button className="btn btn-primary w-full" style={{ justifyContent: 'center' }} onClick={executeSwap} disabled={swapping || !quote}>
                    {swapping ? 'Executing...' : quote ? `Swap ${amountIn} ${fromSyn ? 'SYN' : pool.assetId}` : 'Enter Amount'}
                  </button>
                </div>
              )}

              {/* ADD LIQUIDITY TAB */}
              {tab === 'add' && (
                <div>
                  <div className="form-group">
                    <label>SYN Amount</label>
                    <input className="input input-mono" type="number" placeholder="0.00" value={addSyn} onChange={e => setAddSyn(e.target.value)} id="dex-add-syn" />
                  </div>
                  <div className="form-group">
                    <label>{pool.assetId} Amount</label>
                    <input className="input input-mono" type="number" placeholder="0.00" value={addAsset} onChange={e => setAddAsset(e.target.value)} id="dex-add-asset" />
                  </div>
                  <div className="form-group">
                    <label>Your Address</label>
                    <input className="input input-mono" placeholder="0x..." value={liqAddr} onChange={e => setLiqAddr(e.target.value)} id="dex-liq-addr" />
                  </div>

                  {addSyn && addAsset && (
                    <div style={{ background: 'var(--obsidian-800)', borderRadius: 10, padding: 14, marginBottom: 16, border: '1px solid var(--border-subtle)' }}>
                      <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 8 }}>
                        <span style={{ fontSize: '0.82rem', color: 'var(--text-muted)' }}>Current ratio</span>
                        <span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.82rem' }}>{(pool.synReserve / pool.assetReserve).toFixed(2)} SYN/{pool.assetId}</span>
                      </div>
                      <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                        <span style={{ fontSize: '0.82rem', color: 'var(--text-muted)' }}>Est. APY</span>
                        <span style={{ color: 'var(--green-400)', fontWeight: 600 }}>{pool.apy}%</span>
                      </div>
                    </div>
                  )}

                  {liqResult && liqResult.ok && (
                    <div className="alert alert-success" style={{ marginBottom: 12 }}>
                      ✅ Added! You received {parseFloat(liqResult.shares).toFixed(2)} LP shares
                    </div>
                  )}
                  {liqResult && !liqResult.ok && (
                    <div className="alert alert-error" style={{ marginBottom: 12 }}>⚠️ {liqResult.error}</div>
                  )}

                  <button className="btn btn-primary w-full" style={{ justifyContent: 'center' }} onClick={executeLiquidity} disabled={addingLiq || !addSyn || !addAsset}>
                    {addingLiq ? 'Adding...' : '➕ Add Liquidity'}
                  </button>
                </div>
              )}

              {/* REMOVE LIQUIDITY TAB */}
              {tab === 'remove' && (
                <div>
                  <div className="alert alert-info" style={{ marginBottom: 20, fontSize: '0.82rem' }}>
                    Enter the LP shares you received when adding liquidity. Your proportional SYN and {pool.assetId} will be returned.
                  </div>
                  <div className="form-group">
                    <label>Your Address</label>
                    <input className="input input-mono" placeholder="0x..." value={removeAddr} onChange={e => setRemoveAddr(e.target.value)} id="dex-remove-addr" />
                  </div>
                  <div className="form-group">
                    <label>LP Shares to Remove</label>
                    <input className="input input-mono" type="number" placeholder="0.00" value={removeShares} onChange={e => setRemoveShares(e.target.value)} id="dex-remove-shares" />
                  </div>

                  {removeResult && removeResult.ok && (
                    <div className="alert alert-success" style={{ marginBottom: 12 }}>
                      ✅ Removed! Got {parseFloat(removeResult.synOut).toFixed(2)} SYN + {parseFloat(removeResult.assetOut).toFixed(6)} {pool.assetId}
                    </div>
                  )}
                  {removeResult && !removeResult.ok && (
                    <div className="alert alert-error" style={{ marginBottom: 12 }}>⚠️ {removeResult.error}</div>
                  )}

                  <button className="btn btn-danger w-full" style={{ justifyContent: 'center' }} onClick={executeRemove} disabled={!removeShares || !removeAddr}>
                    Remove Liquidity
                  </button>
                </div>
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
