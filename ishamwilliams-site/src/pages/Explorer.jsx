import { useState, useEffect } from 'react'
import { getStatus, getMempool, getMockBlocks, getMockTxs, getMockStats } from '../api'

const TX_TYPE_MAP = {
  transfer: { label: 'Transfer', color: 'var(--syn-400)', bg: 'var(--syn-glow)' },
  escrow_lock: { label: 'Escrow Lock', color: 'var(--gold-400)', bg: 'var(--gold-glow)' },
  inner_circle_purchase: { label: 'Inner Circle', color: 'var(--purple-400)', bg: 'var(--purple-glow)' },
  dex_swap: { label: 'DEX Swap', color: 'var(--green-400)', bg: 'rgba(102,187,106,0.1)' },
  stake: { label: 'Stake', color: 'var(--text-primary)', bg: 'rgba(255,255,255,0.05)' },
}

function truncate(str, len = 18) {
  if (!str || str.length <= len) return str
  return str.slice(0, len) + '...'
}

export default function Explorer() {
  const [blocks, setBlocks] = useState(getMockBlocks(20))
  const [txs, setTxs] = useState(getMockTxs(20))
  const [chainStatus, setChainStatus] = useState(getMockStats())
  const [mempool, setMempool] = useState({ size: 0, tx: [] })
  const [search, setSearch] = useState('')
  const [searchResult, setSearchResult] = useState(null)
  const [activeTab, setActiveTab] = useState('blocks')
  const [live, setLive] = useState(false)

  useEffect(() => {
    async function refresh() {
      try {
        const [status, pool] = await Promise.all([getStatus(), getMempool()])
        setChainStatus(prev => ({ ...prev, height: status.height, stateRoot: status.state_root }))
        setMempool(pool)
        setLive(true)
      } catch {
        setLive(false)
      }
      setBlocks(getMockBlocks(20))
      setTxs(getMockTxs(20))
    }
    refresh()
    const iv = setInterval(refresh, 4000)
    return () => clearInterval(iv)
  }, [])

  function handleSearch(e) {
    e.preventDefault()
    const q = search.trim().toLowerCase()
    if (!q) return

    if (q.startsWith('#') || /^\d+$/.test(q)) {
      const h = parseInt(q.replace('#', ''))
      const b = blocks.find(b => b.height === h)
      setSearchResult(b ? { type: 'block', data: b } : { type: 'not_found' })
    } else if (q.startsWith('0x')) {
      const tx = txs.find(t => t.id.startsWith(q))
      if (tx) setSearchResult({ type: 'tx', data: tx })
      else setSearchResult({ type: 'address', data: { address: search.trim(), note: 'Address found — use the Wallet page to check balance.' } })
    } else {
      setSearchResult({ type: 'not_found' })
    }
  }

  return (
    <div className="page-content page-enter">
      <div style={{ background: 'var(--obsidian-800)', borderBottom: '1px solid var(--border-subtle)', padding: '48px 0 32px' }}>
        <div className="container">
          <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 8 }}>
            <span className="label" style={{ margin: 0 }}>Synthos L1</span>
            <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
              <div style={{ width: 7, height: 7, borderRadius: '50%', background: live ? 'var(--green-400)' : 'var(--gold-400)', boxShadow: `0 0 6px ${live ? 'var(--green-400)' : 'var(--gold-400)'}`, animation: 'pulse-dot 2s infinite' }} />
              <span style={{ fontSize: '0.75rem', color: 'var(--text-muted)' }}>{live ? 'Live' : 'Simulated'}</span>
            </div>
          </div>
          <h1 style={{ fontSize: '2.4rem', marginBottom: 8 }}>Block Explorer</h1>
          <p style={{ color: 'var(--text-muted)', marginBottom: 32 }}>
            Chain ID: <span style={{ fontFamily: 'var(--font-mono)', color: 'var(--syn-400)' }}>synthos-1</span>
            &nbsp;·&nbsp; Height: <span style={{ fontFamily: 'var(--font-mono)', color: 'var(--syn-400)' }}>#{chainStatus.height.toLocaleString()}</span>
            &nbsp;·&nbsp; State Root: <span style={{ fontFamily: 'var(--font-mono)', color: 'var(--text-secondary)', fontSize: '0.8rem' }}>{chainStatus.stateRoot?.slice(0, 32)}...</span>
          </p>

          {/* Search */}
          <form onSubmit={handleSearch} style={{ display: 'flex', gap: 12 }}>
            <input
              className="input"
              placeholder="Search block height, tx hash (0x...), or address..."
              value={search}
              onChange={e => setSearch(e.target.value)}
              style={{ fontSize: '0.95rem' }}
              id="explorer-search"
            />
            <button type="submit" className="btn btn-primary" style={{ flexShrink: 0 }}>Search</button>
          </form>

          {searchResult && (
            <div style={{ marginTop: 16 }}>
              {searchResult.type === 'not_found' && (
                <div className="alert alert-error">No result found for: "{search}"</div>
              )}
              {searchResult.type === 'block' && (
                <div className="alert alert-info">
                  Block #{searchResult.data.height} — Proposer: {searchResult.data.proposer} · {searchResult.data.txCount} txs · {searchResult.data.fees} SYN fees · {searchResult.data.age}
                </div>
              )}
              {searchResult.type === 'tx' && (
                <div className="alert alert-info">
                  TX {truncate(searchResult.data.id, 24)} — {searchResult.data.amount.toLocaleString()} SYN · fee: {searchResult.data.fee} · {searchResult.data.age}
                </div>
              )}
              {searchResult.type === 'address' && (
                <div className="alert alert-info">{searchResult.data.note}</div>
              )}
            </div>
          )}
        </div>
      </div>

      <div className="container" style={{ padding: '40px 24px' }}>

        {/* Chain Stats Row */}
        <div className="grid-4" style={{ marginBottom: 40 }}>
          {[
            { label: 'Chain Height', val: `#${chainStatus.height.toLocaleString()}`, color: 'var(--syn-400)' },
            { label: 'Validators', val: chainStatus.activeValidators, color: 'var(--green-400)' },
            { label: 'Mempool Txs', val: mempool.size || chainStatus.mempoolSize, color: 'var(--text-primary)' },
            { label: 'Block Time', val: chainStatus.blockTime, color: 'var(--gold-400)' },
          ].map(s => (
            <div key={s.label} className="card" style={{ textAlign: 'center', padding: '20px' }}>
              <div style={{ fontFamily: 'var(--font-mono)', fontSize: '1.6rem', fontWeight: 700, color: s.color }}>{s.val}</div>
              <div style={{ fontSize: '0.78rem', color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.08em', marginTop: 4 }}>{s.label}</div>
            </div>
          ))}
        </div>

        {/* Tabs */}
        <div style={{ display: 'flex', gap: 4, marginBottom: 24, borderBottom: '1px solid var(--border-subtle)', paddingBottom: 0 }}>
          {['blocks', 'transactions', 'mempool'].map(tab => (
            <button
              key={tab}
              onClick={() => setActiveTab(tab)}
              style={{
                padding: '10px 20px',
                background: 'transparent',
                border: 'none',
                borderBottom: activeTab === tab ? '2px solid var(--syn-400)' : '2px solid transparent',
                color: activeTab === tab ? 'var(--syn-400)' : 'var(--text-muted)',
                fontWeight: activeTab === tab ? 600 : 400,
                cursor: 'pointer',
                fontSize: '0.9rem',
                textTransform: 'capitalize',
                letterSpacing: '0.03em',
                marginBottom: -1,
                transition: 'all 0.2s',
              }}
            >
              {tab === 'mempool' ? `Mempool (${mempool.size || chainStatus.mempoolSize})` : tab}
            </button>
          ))}
        </div>

        {/* Blocks Table */}
        {activeTab === 'blocks' && (
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Block</th>
                  <th>Hash</th>
                  <th>Proposer</th>
                  <th>Txns</th>
                  <th>Fees (SYN)</th>
                  <th>Age</th>
                </tr>
              </thead>
              <tbody>
                {blocks.map(b => (
                  <tr key={b.height}>
                    <td>
                      <span style={{ fontFamily: 'var(--font-mono)', color: 'var(--syn-400)', fontWeight: 600 }}>#{b.height.toLocaleString()}</span>
                    </td>
                    <td><span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.82rem', color: 'var(--text-muted)' }}>{b.hash}</span></td>
                    <td><span style={{ fontSize: '0.88rem', color: 'var(--text-secondary)' }}>{b.proposer}</span></td>
                    <td><span className="badge badge-syn">{b.txCount}</span></td>
                    <td><span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.88rem' }}>{b.fees.toLocaleString()}</span></td>
                    <td><span style={{ fontSize: '0.82rem', color: 'var(--text-muted)' }}>{b.age}</span></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}

        {/* Transactions Table */}
        {activeTab === 'transactions' && (
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Type</th>
                  <th>TX Hash</th>
                  <th>From</th>
                  <th>To</th>
                  <th>Amount (SYN)</th>
                  <th>Fee</th>
                  <th>Age</th>
                </tr>
              </thead>
              <tbody>
                {txs.map(tx => {
                  const t = TX_TYPE_MAP[tx.type] || TX_TYPE_MAP.transfer
                  return (
                    <tr key={tx.id}>
                      <td>
                        <span style={{ fontSize: '0.75rem', fontWeight: 600, padding: '3px 10px', borderRadius: 999, background: t.bg, color: t.color, whiteSpace: 'nowrap' }}>
                          {t.label}
                        </span>
                      </td>
                      <td><span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.8rem', color: 'var(--text-muted)' }}>{tx.id}</span></td>
                      <td><span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.78rem', color: 'var(--text-secondary)' }}>{truncate(tx.from, 14)}</span></td>
                      <td><span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.78rem', color: 'var(--text-secondary)' }}>{truncate(tx.to, 14)}</span></td>
                      <td><span style={{ fontWeight: 600 }}>{tx.amount.toLocaleString()}</span></td>
                      <td><span style={{ color: 'var(--text-muted)', fontSize: '0.85rem' }}>{tx.fee}</span></td>
                      <td><span style={{ color: 'var(--text-muted)', fontSize: '0.82rem' }}>{tx.age}</span></td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}

        {/* Mempool */}
        {activeTab === 'mempool' && (
          <div>
            {(mempool.size || chainStatus.mempoolSize) === 0 ? (
              <div className="card" style={{ textAlign: 'center', padding: 48, color: 'var(--text-muted)' }}>
                <div style={{ fontSize: '2rem', marginBottom: 12 }}>🧹</div>
                Mempool is clean — no pending transactions
              </div>
            ) : (
              <div className="table-wrap">
                <table>
                  <thead>
                    <tr><th>TX ID</th><th>From</th><th>To</th><th>Amount</th><th>Fee</th></tr>
                  </thead>
                  <tbody>
                    {Object.values(mempool.tx || {}).slice(0, 20).map((tx, i) => (
                      <tr key={i}>
                        <td style={{ fontFamily: 'var(--font-mono)', fontSize: '0.8rem', color: 'var(--text-muted)' }}>{truncate(tx.id, 20)}</td>
                        <td style={{ fontFamily: 'var(--font-mono)', fontSize: '0.78rem' }}>{truncate(tx.from, 16)}</td>
                        <td style={{ fontFamily: 'var(--font-mono)', fontSize: '0.78rem' }}>{truncate(tx.to, 16)}</td>
                        <td>{(tx.amount || 0).toLocaleString()} SYN</td>
                        <td>{tx.fee}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  )
}
