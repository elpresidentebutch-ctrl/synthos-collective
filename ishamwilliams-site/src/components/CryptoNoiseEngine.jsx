import { useState, useEffect, useRef } from 'react'

/**
 * CryptoNoiseEngine
 * Generates real Ed25519-style cryptographic hash strings in the browser
 * and displays them as a live flood — visually demonstrating how
 * the Synthos Collective "poisons the well" of data broker databases.
 */

function generateHash() {
  const arr = new Uint8Array(32)
  crypto.getRandomValues(arr)
  return Array.from(arr).map(b => b.toString(16).padStart(2, '0')).join('')
}

function generateNoisePacket() {
  const types = ['COOKIE_POISON', 'FINGERPRINT_SPOOF', 'IDENTITY_TOKEN', 'SESSION_NOISE', 'TRACKER_CORRUPT', 'PROFILE_FLOOD']
  const type = types[Math.floor(Math.random() * types.length)]
  const hash = generateHash()
  const fakeId = `usr_${generateHash().slice(0, 12)}`
  return { type, hash, fakeId, ts: Date.now() }
}

export default function CryptoNoiseEngine({ compact = false }) {
  const [packets, setPackets] = useState([])
  const [running, setRunning] = useState(true)
  const [totalFired, setTotalFired] = useState(0)
  const intervalRef = useRef(null)

  const COLORS = {
    COOKIE_POISON:     'var(--syn-400)',
    FINGERPRINT_SPOOF: 'var(--purple-400)',
    IDENTITY_TOKEN:    'var(--gold-400)',
    SESSION_NOISE:     'var(--green-400)',
    TRACKER_CORRUPT:   '#ef5350',
    PROFILE_FLOOD:     '#26c6da',
  }

  useEffect(() => {
    if (running) {
      intervalRef.current = setInterval(() => {
        const count = Math.floor(Math.random() * 3) + 1
        const newPackets = Array.from({ length: count }, generateNoisePacket)
        setPackets(prev => [...newPackets, ...prev].slice(0, compact ? 12 : 40))
        setTotalFired(prev => prev + count)
      }, compact ? 600 : 400)
    } else {
      clearInterval(intervalRef.current)
    }
    return () => clearInterval(intervalRef.current)
  }, [running, compact])

  if (compact) {
    return (
      <div style={{
        background: 'var(--obsidian-900)',
        borderRadius: 'var(--radius-lg)',
        border: '1px solid var(--border-subtle)',
        padding: 20,
        fontFamily: 'var(--font-mono)',
        overflow: 'hidden',
      }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 14 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <div style={{ width: 8, height: 8, borderRadius: '50%', background: running ? 'var(--green-400)' : 'var(--red-400)', boxShadow: running ? '0 0 8px var(--green-400)' : 'none', animation: running ? 'pulse 1.5s infinite' : 'none' }} />
            <span style={{ fontSize: '0.72rem', color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.1em' }}>
              Live Noise Engine
            </span>
          </div>
          <span style={{ fontSize: '0.72rem', color: 'var(--syn-400)', fontWeight: 700 }}>
            {totalFired.toLocaleString()} packets fired
          </span>
        </div>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 4, maxHeight: 200, overflow: 'hidden' }}>
          {packets.slice(0, 8).map((p, i) => (
            <div key={p.hash + i} style={{
              fontSize: '0.68rem',
              color: i === 0 ? COLORS[p.type] : 'var(--text-muted)',
              opacity: 1 - i * 0.1,
              whiteSpace: 'nowrap',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              transition: 'all 0.3s',
            }}>
              <span style={{ color: COLORS[p.type] }}>[{p.type}]</span>{' '}
              {p.hash}
            </div>
          ))}
        </div>
      </div>
    )
  }

  return (
    <div style={{ width: '100%' }}>
      {/* Controls */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 20, flexWrap: 'wrap', gap: 12 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          <div style={{
            display: 'flex', alignItems: 'center', gap: 8,
            padding: '6px 14px', borderRadius: 20,
            background: running ? 'rgba(76,175,80,0.1)' : 'rgba(239,83,80,0.1)',
            border: `1px solid ${running ? 'rgba(76,175,80,0.3)' : 'rgba(239,83,80,0.3)'}`,
          }}>
            <div style={{ width: 8, height: 8, borderRadius: '50%', background: running ? 'var(--green-400)' : 'var(--red-400)', boxShadow: running ? '0 0 8px var(--green-400)' : 'none' }} />
            <span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.78rem', color: running ? 'var(--green-400)' : 'var(--red-400)', fontWeight: 700 }}>
              {running ? 'NOISE ENGINE ACTIVE' : 'ENGINE PAUSED'}
            </span>
          </div>
          <span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.82rem', color: 'var(--syn-400)', fontWeight: 700 }}>
            {totalFired.toLocaleString()} hashes fired
          </span>
        </div>
        <button
          onClick={() => setRunning(r => !r)}
          className={`btn btn-sm ${running ? 'btn-secondary' : 'btn-primary'}`}
          style={{ fontFamily: 'var(--font-mono)' }}
        >
          {running ? '⏸ Pause' : '▶ Activate'}
        </button>
      </div>

      {/* Legend */}
      <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', marginBottom: 16 }}>
        {Object.entries(COLORS).map(([type, color]) => (
          <div key={type} style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
            <div style={{ width: 8, height: 8, borderRadius: 2, background: color }} />
            <span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.65rem', color: 'var(--text-muted)' }}>{type}</span>
          </div>
        ))}
      </div>

      {/* Live hash stream */}
      <div style={{
        background: 'var(--obsidian-900)',
        borderRadius: 'var(--radius-lg)',
        border: '1px solid var(--border-subtle)',
        padding: '20px 24px',
        fontFamily: 'var(--font-mono)',
        fontSize: '0.75rem',
        lineHeight: 1.8,
        maxHeight: 360,
        overflowY: 'auto',
        position: 'relative',
      }}>
        {/* Fade overlay at top */}
        <div style={{ position: 'sticky', top: 0, left: 0, right: 0, height: 40, background: 'linear-gradient(to bottom, var(--obsidian-900), transparent)', zIndex: 2, marginBottom: -40 }} />

        {packets.length === 0 && (
          <div style={{ color: 'var(--text-muted)', textAlign: 'center', padding: '40px 0' }}>
            Click Activate to start the noise engine...
          </div>
        )}

        {packets.map((p, i) => (
          <div
            key={p.hash + i}
            style={{
              display: 'flex',
              gap: 12,
              marginBottom: 4,
              opacity: Math.max(0.2, 1 - i * 0.022),
              transition: 'opacity 0.4s',
              alignItems: 'baseline',
            }}
          >
            <span style={{ color: 'var(--text-muted)', fontSize: '0.65rem', flexShrink: 0 }}>
              {new Date(p.ts).toLocaleTimeString()}
            </span>
            <span style={{ color: COLORS[p.type], fontWeight: 700, flexShrink: 0, fontSize: '0.68rem' }}>
              [{p.type}]
            </span>
            <span style={{ color: 'var(--text-secondary)', wordBreak: 'break-all', fontSize: '0.72rem' }}>
              0x{p.hash}
            </span>
            <span style={{ color: 'var(--text-muted)', flexShrink: 0, fontSize: '0.65rem' }}>
              → {p.fakeId}
            </span>
          </div>
        ))}
      </div>

      <p style={{ marginTop: 14, fontSize: '0.78rem', color: 'var(--text-muted)', lineHeight: 1.65 }}>
        ⚔️ Each hash above is a <strong style={{ color: 'var(--text-secondary)' }}>real cryptographic value</strong> generated by your browser using{' '}
        <span style={{ color: 'var(--syn-400)' }}>crypto.getRandomValues()</span> — the same entropy source used in production cryptography.
        In an active immune node, these flood data broker APIs continuously, making real tracking statistically impossible.
      </p>
    </div>
  )
}
