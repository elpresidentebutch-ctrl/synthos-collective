import { useState, useEffect } from 'react'
import { getStatus, getMockStats } from '../api'

/**
 * LiveStats — Real-time chain stats bar at the top of pages
 * Pulls from the Go RPC node, falls back to simulated data
 */
export default function LiveStats() {
  const [stats, setStats] = useState(getMockStats())
  const [live, setLive] = useState(false)

  useEffect(() => {
    async function fetchStats() {
      try {
        const data = await getStatus()
        setStats(prev => ({
          ...prev,
          height: data.height,
          stateRoot: data.state_root,
        }))
        setLive(true)
      } catch {
        setLive(false)
      }
    }

    fetchStats()
    const interval = setInterval(fetchStats, 4000) // match ~4s block time
    return () => clearInterval(interval)
  }, [])

  // Animate height ticker
  const [displayHeight, setDisplayHeight] = useState(stats.height)
  useEffect(() => {
    const t = setTimeout(() => setDisplayHeight(stats.height), 100)
    return () => clearTimeout(t)
  }, [stats.height])

  const items = [
    { label: 'Block Height', value: `#${displayHeight.toLocaleString()}`, color: 'var(--syn-400)' },
    { label: 'Active Validators', value: stats.activeValidators, color: 'var(--green-400)' },
    { label: 'Total Staked', value: `${(stats.totalStaked / 1e9).toFixed(1)}B SYN`, color: 'var(--text-primary)' },
    { label: 'Mempool', value: `${stats.mempoolSize} tx`, color: 'var(--text-primary)' },
    { label: 'Block Time', value: stats.blockTime, color: 'var(--text-primary)' },
    { label: 'SYN Price', value: `$${stats.synPriceUSD.toFixed(7)}`, color: 'var(--gold-400)' },
    { label: 'Market Cap', value: `$${(stats.marketCapUSD / 1e6).toFixed(2)}M`, color: 'var(--text-primary)' },
    { label: '24h Volume', value: `$${stats.volumeUSD24h.toLocaleString()}`, color: 'var(--text-primary)' },
  ]

  return (
    <div style={{
      background: 'var(--obsidian-800)',
      borderBottom: '1px solid var(--border-subtle)',
      height: 40,
      overflow: 'hidden',
      display: 'flex',
      alignItems: 'center',
    }}>
      <div className="ticker-wrap" style={{ flex: 1, height: '100%', border: 'none', background: 'transparent' }}>
        <div className="ticker-track" style={{ height: '100%', alignItems: 'center' }}>
          {/* Duplicate for seamless loop */}
          {[...items, ...items].map((item, i) => (
            <div key={i} style={{ display: 'flex', alignItems: 'center', gap: 6, whiteSpace: 'nowrap' }}>
              <span style={{ fontSize: '0.72rem', color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.08em' }}>
                {item.label}:
              </span>
              <span style={{ fontSize: '0.82rem', fontWeight: 600, color: item.color, fontFamily: 'var(--font-mono)' }}>
                {item.value}
              </span>
              <span style={{ color: 'var(--border-medium)', marginLeft: 16 }}>·</span>
            </div>
          ))}
        </div>
      </div>
      {/* Live indicator */}
      <div style={{
        padding: '0 16px',
        display: 'flex', alignItems: 'center', gap: 6,
        borderLeft: '1px solid var(--border-subtle)',
        height: '100%',
        flexShrink: 0,
      }}>
        <div style={{
          width: 6, height: 6, borderRadius: '50%',
          background: live ? 'var(--green-400)' : 'var(--gold-400)',
          boxShadow: `0 0 6px ${live ? 'var(--green-400)' : 'var(--gold-400)'}`,
          animation: 'pulse-dot 2s infinite',
        }} />
        <span style={{ fontSize: '0.68rem', color: 'var(--text-muted)', letterSpacing: '0.06em', textTransform: 'uppercase' }}>
          {live ? 'LIVE' : 'SIM'}
        </span>
      </div>
    </div>
  )
}
