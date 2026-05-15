import { useState, useEffect, useRef } from 'react'
import { Link } from 'react-router-dom'
import { getMockStats, getMockBlocks, getMockTxs, TOKENOMICS } from '../api'
import { AreaChart, Area, XAxis, YAxis, Tooltip, ResponsiveContainer } from 'recharts'
import CryptoNoiseEngine from '../components/CryptoNoiseEngine'

// Animated counter hook
function useCounter(target, duration = 2000) {
  const [count, setCount] = useState(0)
  const started = useRef(false)
  const ref = useRef(null)

  useEffect(() => {
    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting && !started.current) {
          started.current = true
          const start = Date.now()
          const step = () => {
            const elapsed = Date.now() - start
            const progress = Math.min(elapsed / duration, 1)
            const eased = 1 - Math.pow(1 - progress, 3)
            setCount(Math.floor(eased * target))
            if (progress < 1) requestAnimationFrame(step)
          }
          requestAnimationFrame(step)
        }
      },
      { threshold: 0.2 }
    )
    if (ref.current) observer.observe(ref.current)
    return () => observer.disconnect()
  }, [target, duration])

  return [count, ref]
}

// Price chart data
function generateChartData() {
  let price = 0.0000380
  return Array.from({ length: 30 }, (_, i) => {
    price += (Math.random() - 0.44) * 0.000002
    price = Math.max(0.000030, Math.min(0.000060, price))
    return {
      day: i + 1,
      price: parseFloat(price.toFixed(8)),
      volume: Math.floor(Math.random() * 200000) + 80000,
    }
  })
}

const CHART_DATA = generateChartData()

function StatCard({ value, label, sub, accent = 'var(--syn-400)', prefix = '', suffix = '' }) {
  const [count, ref] = useCounter(value)
  return (
    <div className="card" ref={ref} style={{ textAlign: 'center' }}>
      <div style={{ fontFamily: 'var(--font-display)', fontSize: '2.4rem', fontWeight: 800, color: accent, lineHeight: 1.1 }}>
        {prefix}{typeof value === 'number' ? count.toLocaleString() : value}{suffix}
      </div>
      <div style={{ fontWeight: 600, color: 'var(--text-primary)', marginTop: 6, fontSize: '1rem' }}>{label}</div>
      {sub && <div style={{ fontSize: '0.8rem', color: 'var(--text-muted)', marginTop: 4 }}>{sub}</div>}
    </div>
  )
}

export default function Home() {
  const stats = getMockStats()
  const blocks = getMockBlocks(5)
  const txs = getMockTxs(5)

  return (
    <div className="page-content page-enter">

      {/* ── HERO ── */}
      <section style={{
        minHeight: '92vh',
        display: 'flex', alignItems: 'center',
        position: 'relative', overflow: 'hidden',
        padding: '80px 0',
      }}>
        {/* Radial glows */}
        <div style={{ position: 'absolute', top: '10%', left: '10%', width: 600, height: 600, borderRadius: '50%', background: 'radial-gradient(circle, rgba(0,229,255,0.06) 0%, transparent 70%)', pointerEvents: 'none' }} />
        <div style={{ position: 'absolute', bottom: '5%', right: '8%', width: 500, height: 500, borderRadius: '50%', background: 'radial-gradient(circle, rgba(171,71,188,0.05) 0%, transparent 70%)', pointerEvents: 'none' }} />

        <div className="container" style={{ position: 'relative', zIndex: 1 }}>
          <div style={{ maxWidth: 860 }}>
            <div className="badge badge-syn" style={{ marginBottom: 24 }}>
              <div className="glow-dot" style={{ width: 6, height: 6 }} />
              ⚔️ Synthos Collective · The War For Data Sovereignty Has Begun
            </div>

            <h1 style={{ marginBottom: 16, background: 'linear-gradient(135deg, #e8edf5 0%, var(--syn-400) 55%, var(--purple-400) 100%)', WebkitBackgroundClip: 'text', WebkitTextFillColor: 'transparent', backgroundClip: 'text' }}>
              Your First Digital<br />Weapon
            </h1>

            <p style={{ fontSize: '1.25rem', fontWeight: 600, color: 'var(--syn-400)', marginBottom: 16, lineHeight: 1.5, fontFamily: 'var(--font-display)', letterSpacing: '0.02em' }}>
              Let the Synthos Collective drown out surveillance capitalism.
            </p>

            <p style={{ fontSize: '1.05rem', color: 'var(--text-secondary)', maxWidth: 680, marginBottom: 16, lineHeight: 1.85 }}>
              A cloudless, cryptographically secure L1 network governed by a Distributed Immune System.
              Poisoning the Well — one immune node at a time. Absolute Silence. Absolute Security.
            </p>

            <p style={{ fontSize: '0.95rem', color: 'var(--text-muted)', maxWidth: 620, marginBottom: 44, lineHeight: 1.75 }}>
              No fees. No subscriptions. No paywalls. Deploy your cryptographic weapons immediately.
              The SYN token is both the fuel of the Distributed Immune System <em>and</em> a legitimate financial instrument.
            </p>

            <div style={{ display: 'flex', gap: 16, flexWrap: 'wrap', marginBottom: 64 }}>
              <Link to="/validators" className="btn btn-primary btn-lg">
                ⚡ Activate Immune Node — FREE
              </Link>
              <Link to="/inner-circle" className="btn btn-gold btn-lg">
                🏆 Join the Inner Circle
              </Link>
              <Link to="/explorer" className="btn btn-secondary btn-lg">
                Explore the Chain
              </Link>
            </div>

            {/* Live mini-stats */}
            <div style={{ display: 'flex', gap: 32, flexWrap: 'wrap' }}>
              {[
                { label: 'Block Height', value: `#${stats.height.toLocaleString()}` },
                { label: 'Active Immune Nodes', value: `${stats.activeValidators}` },
                { label: 'Total Supply', value: '100B SYN' },
                { label: 'Inner Circle Slots', value: `${stats.innerCircleSlotsFilled}/100` },
              ].map(({ label, value }) => (
                <div key={label}>
                  <div style={{ fontFamily: 'var(--font-mono)', fontSize: '1.3rem', fontWeight: 700, color: 'var(--syn-400)' }}>{value}</div>
                  <div style={{ fontSize: '0.78rem', color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.08em', marginTop: 2 }}>{label}</div>
                </div>
              ))}
            </div>
          </div>
        </div>
      </section>

      {/* ── LIVE CHAIN FEED ── */}
      <section className="section-sm" style={{ background: 'var(--obsidian-800)', borderTop: '1px solid var(--border-subtle)', borderBottom: '1px solid var(--border-subtle)' }}>
        <div className="container">
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 32 }}>

            {/* Recent Blocks */}
            <div>
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 20 }}>
                <h3 style={{ fontSize: '1.1rem' }}>Recent Blocks</h3>
                <Link to="/explorer" style={{ fontSize: '0.85rem', color: 'var(--syn-400)' }}>View all →</Link>
              </div>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                {blocks.map(b => (
                  <div key={b.height} className="card" style={{ padding: '14px 18px', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
                      <div style={{ width: 36, height: 36, borderRadius: 8, background: 'var(--syn-glow)', border: '1px solid var(--border-medium)', display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: '0.7rem', color: 'var(--syn-400)', fontWeight: 700, fontFamily: 'var(--font-mono)' }}>BLK</div>
                      <div>
                        <div style={{ fontFamily: 'var(--font-mono)', fontWeight: 600, fontSize: '0.9rem', color: 'var(--syn-400)' }}>#{b.height.toLocaleString()}</div>
                        <div style={{ fontSize: '0.75rem', color: 'var(--text-muted)' }}>{b.proposer} · {b.age}</div>
                      </div>
                    </div>
                    <div style={{ textAlign: 'right' }}>
                      <div style={{ fontSize: '0.85rem', fontWeight: 600, color: 'var(--text-primary)' }}>{b.txCount} txs</div>
                      <div style={{ fontSize: '0.75rem', color: 'var(--text-muted)' }}>{b.fees} SYN fees</div>
                    </div>
                  </div>
                ))}
              </div>
            </div>

            {/* Recent Transactions */}
            <div>
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 20 }}>
                <h3 style={{ fontSize: '1.1rem' }}>Recent Transactions</h3>
                <Link to="/explorer" style={{ fontSize: '0.85rem', color: 'var(--syn-400)' }}>View all →</Link>
              </div>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                {txs.map(tx => (
                  <div key={tx.id} className="card" style={{ padding: '14px 18px', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
                      <div style={{
                        width: 36, height: 36, borderRadius: 8,
                        background: tx.type === 'escrow_lock' ? 'var(--gold-glow)' : tx.type === 'inner_circle_purchase' ? 'var(--purple-glow)' : 'var(--syn-glow)',
                        border: `1px solid ${tx.type === 'escrow_lock' ? 'rgba(255,213,79,0.2)' : tx.type === 'inner_circle_purchase' ? 'rgba(206,147,216,0.2)' : 'var(--border-medium)'}`,
                        display: 'flex', alignItems: 'center', justifyContent: 'center',
                        fontSize: '0.65rem', fontWeight: 700,
                        color: tx.type === 'escrow_lock' ? 'var(--gold-400)' : tx.type === 'inner_circle_purchase' ? 'var(--purple-400)' : 'var(--syn-400)',
                      }}>
                        {tx.type === 'transfer' ? 'TFR' : tx.type === 'escrow_lock' ? 'ESC' : tx.type === 'inner_circle_purchase' ? 'IC' : tx.type === 'dex_swap' ? 'DEX' : 'STK'}
                      </div>
                      <div>
                        <div style={{ fontFamily: 'var(--font-mono)', fontSize: '0.78rem', color: 'var(--text-primary)' }}>{tx.id}</div>
                        <div style={{ fontSize: '0.72rem', color: 'var(--text-muted)' }}>{tx.age}</div>
                      </div>
                    </div>
                    <div style={{ textAlign: 'right' }}>
                      <div style={{ fontSize: '0.85rem', fontWeight: 600, color: 'var(--text-primary)' }}>{(tx.amount).toLocaleString()} SYN</div>
                      <div style={{ fontSize: '0.72rem', color: 'var(--text-muted)' }}>fee: {tx.fee}</div>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          </div>
        </div>
      </section>

      {/* ── POISONING THE WELL MANIFESTO ── */}
      <section className="section" style={{ background: 'var(--obsidian-900)', borderTop: '1px solid var(--border-subtle)', borderBottom: '1px solid var(--border-subtle)', position: 'relative', overflow: 'hidden' }}>
        <div style={{ position: 'absolute', top: 0, left: '50%', transform: 'translateX(-50%)', width: '120%', height: '100%', background: 'radial-gradient(ellipse 80% 60% at 50% 0%, rgba(0,229,255,0.04) 0%, transparent 70%)', pointerEvents: 'none' }} />
        <div className="container" style={{ position: 'relative', zIndex: 1 }}>
          <div className="text-center" style={{ marginBottom: 72 }}>
            <span className="badge badge-syn" style={{ marginBottom: 20, fontSize: '0.78rem' }}>⚔️ Operation: Poison The Well</span>
            <h2 style={{ background: 'linear-gradient(135deg, #e8edf5, var(--syn-400))', WebkitBackgroundClip: 'text', WebkitTextFillColor: 'transparent', backgroundClip: 'text', marginBottom: 20 }}>
              How We Poison The Well
            </h2>
            <p style={{ maxWidth: 620, margin: '0 auto', fontSize: '1.05rem', lineHeight: 1.8 }}>
              Data brokers make money on behavioral prediction. They build profiles by linking cookies,
              device IDs, and browsing patterns. Our immune node renders this worthless by flooding
              their datasets with cryptographic noise.
            </p>
          </div>

          {/* 4 Steps */}
          <div className="grid-4" style={{ marginBottom: 80 }}>
            {[
              {
                step: '01',
                title: 'Generate Sovereign Identities',
                desc: 'Every time your immune node activates, it generates cryptographically random identity tokens. These look like legitimate user profiles to tracking systems — but they\'re pure noise. Your browser becomes a thousand different users simultaneously.',
                icon: '🧬',
                color: 'var(--syn-400)',
              },
              {
                step: '02',
                title: 'Corrupt the Dataset',
                desc: 'Data brokers ingest millions of tracking events per second. When your immune node contributes cryptographic noise to their datasets, it corrupts their behavioral models. False device fingerprints mix with real ones. Predictive algorithms break.',
                icon: '☣️',
                color: 'var(--purple-400)',
              },
              {
                step: '03',
                title: 'Break Attribution Chains',
                desc: 'Tracking networks depend on linking identities across devices and time. Your cookies and tokens include false signals that break these chains. When a data broker tries to link your real activity to a profile, they encounter cryptographic noise instead.',
                icon: '🔗',
                color: 'var(--gold-400)',
              },
              {
                step: '04',
                title: 'Collective Scale',
                desc: 'At individual scale, one node is a whisper. Multiply by thousands of operators and the whisper becomes a roar. Data broker datasets contain so much poisoned noise that behavioral prediction becomes statistically unreliable. Their models fail.',
                icon: '🌐',
                color: 'var(--green-400)',
              },
            ].map(s => (
              <div key={s.step} className="card" style={{ borderTop: `2px solid ${s.color}`, position: 'relative' }}>
                <div style={{ fontFamily: 'var(--font-mono)', fontSize: '0.72rem', color: s.color, fontWeight: 700, letterSpacing: '0.15em', marginBottom: 12 }}>STEP {s.step}</div>
                <div style={{ fontSize: '1.8rem', marginBottom: 12 }}>{s.icon}</div>
                <h3 style={{ fontSize: '1rem', marginBottom: 10, color: 'var(--text-primary)' }}>{s.title}</h3>
                <p style={{ fontSize: '0.85rem', lineHeight: 1.7 }}>{s.desc}</p>
              </div>
            ))}
          </div>

          {/* Immune Node Features */}
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 32, alignItems: 'center' }}>
            <div>
              <span className="label">What Is an Immune Node?</span>
              <h2 style={{ marginBottom: 20, fontSize: '2rem' }}>Your Weaponized<br />Antibody</h2>
              <p style={{ fontSize: '1rem', lineHeight: 1.85, marginBottom: 24 }}>
                An Immune Node is your cryptographic defense artifact that runs on your hardware,
                generating continuous streams of sovereign identity tokens and poisoning data broker
                databases with cryptographic noise.
              </p>
              <p style={{ fontSize: '0.95rem', color: 'var(--text-muted)', lineHeight: 1.8, marginBottom: 32 }}>
                When you activate an immune node, your physical device becomes part of the collective
                defense system. You join thousands of operators worldwide in a coordinated data poisoning
                campaign — breaking tracking chains, disrupting behavioral profiling, and rendering
                surveillance datasets worthless.
              </p>
              <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap' }}>
                <Link to="/validators" className="btn btn-primary">
                  ⚡ Activate Your Node — FREE
                </Link>
                <Link to="/inner-circle" className="btn btn-secondary">
                  Join the Inner Circle
                </Link>
              </div>
            </div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
              {[
                { icon: '🛡️', title: 'Cryptographic Noise Generation', desc: 'Your immune node generates continuous Ed25519 cryptographic signatures that mimic legitimate user behavior. Every cookie, every session token — noise designed to confuse tracking networks.', color: 'var(--syn-400)' },
                { icon: '🤫', title: 'Cryptographic Silence Protocol', desc: 'Zero inbound ports. Zero telemetry. Zero fingerprints. Your immune node communicates only through outbound channels you control, rendering it invisible to external surveillance.', color: 'var(--purple-400)' },
                { icon: '⚔️', title: 'Data Poisoning Arsenal', desc: 'Your node stores encrypted cryptographic cookies and sovereignty tokens. Each one is a weapon — false behavioral signals that corrupt data broker models and render profiles worthless.', color: 'var(--gold-400)' },
                { icon: '🧬', title: 'Collective Threat Response', desc: 'When surveillance patterns emerge, the collective mobilizes a coordinated immune response — updating defensive protocols and deploying countermeasures across the entire operator network.', color: 'var(--green-400)' },
              ].map(f => (
                <div key={f.title} className="card" style={{ padding: '18px 20px', display: 'flex', gap: 16, alignItems: 'flex-start', borderLeft: `3px solid ${f.color}` }}>
                  <div style={{ fontSize: '1.4rem', flexShrink: 0 }}>{f.icon}</div>
                  <div>
                    <div style={{ fontWeight: 700, fontSize: '0.9rem', marginBottom: 4, color: 'var(--text-primary)' }}>{f.title}</div>
                    <div style={{ fontSize: '0.82rem', color: 'var(--text-muted)', lineHeight: 1.65 }}>{f.desc}</div>
                  </div>
                </div>
              ))}
            </div>
          </div>

          {/* Closing war cry */}
          <div style={{ marginTop: 80, padding: '40px', background: 'rgba(0,229,255,0.03)', borderRadius: 'var(--radius-lg)', border: '1px solid var(--border-medium)', textAlign: 'center' }}>
            <p style={{ fontSize: '1.1rem', fontFamily: 'var(--font-display)', letterSpacing: '0.02em', color: 'var(--text-primary)', lineHeight: 1.8, maxWidth: 720, margin: '0 auto' }}>
              "This is why cryptographic noise is a weapon. Each cookie you deploy weakens the surveillance apparatus.
              Each immune node activated brings us closer to{' '}
              <span style={{ color: 'var(--syn-400)', fontWeight: 700 }}>complete cryptographic silence.</span>"
            </p>
            <div style={{ marginTop: 16, fontSize: '0.82rem', color: 'var(--text-muted)', letterSpacing: '0.08em', textTransform: 'uppercase' }}>
              — Isham Williams, Founder · Synthos Collective
            </div>
          </div>

          {/* Live Noise Engine Demo */}
          <div style={{ marginTop: 64 }}>
            <div className="text-center" style={{ marginBottom: 32 }}>
              <span className="label">Live Demo — Running In Your Browser Right Now</span>
              <h3 style={{ fontSize: '1.6rem', marginBottom: 12 }}>The Well Is Being Poisoned</h3>
              <p style={{ color: 'var(--text-muted)', maxWidth: 560, margin: '0 auto', fontSize: '0.95rem', lineHeight: 1.7 }}>
                These are <strong style={{ color: 'var(--text-primary)' }}>real cryptographic hashes</strong> generated
                autonomously in your browser. This is exactly what an active immune node floods into data broker
                databases — encrypted nonsense that makes tracking anyone statistically impossible.
              </p>
            </div>
            <CryptoNoiseEngine />
          </div>

        </div>
      </section>

      {/* ── STATS ── */}
      <section className="section" id="tokenomics">
        <div className="container">
          <div className="text-center" style={{ marginBottom: 64 }}>
            <span className="label">Tokenomics</span>
            <h2>Built on Immutable Economics</h2>
            <p style={{ maxWidth: 560, margin: '16px auto 0' }}>
              Fixed supply. No inflation. No arbitrary minting.
              The SYN token economy is hardcoded at the protocol level.
            </p>
          </div>
          <div className="grid-4" style={{ marginBottom: 48 }}>
            <StatCard value={100} suffix="B SYN" label="Total Supply" sub="Hard cap, forever" accent="var(--syn-400)" />
            <StatCard value={stats.activeValidators} label="Active Validators" sub="Sovereign nodes" accent="var(--green-400)" />
            <StatCard value={20} suffix=" yr" label="Vesting Period" sub="1B SYN/year to Founder" accent="var(--gold-400)" />
            <StatCard value={TOKENOMICS.escrowCommission} suffix="%" label="Escrow Commission" sub="Founder earns on locks" accent="var(--purple-400)" />
          </div>

          {/* Price chart */}
          <div className="card" style={{ padding: '32px' }}>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 24, flexWrap: 'wrap', gap: 16 }}>
              <div>
                <div style={{ fontSize: '0.8rem', color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.08em', marginBottom: 4 }}>SYN/USD — 30 Day</div>
                <div style={{ fontFamily: 'var(--font-display)', fontSize: '2rem', fontWeight: 700, color: 'var(--syn-400)' }}>
                  ${stats.synPriceUSD.toFixed(7)}
                </div>
              </div>
              <div style={{ display: 'flex', gap: 24 }}>
                <div style={{ textAlign: 'right' }}>
                  <div style={{ fontSize: '0.78rem', color: 'var(--text-muted)' }}>Market Cap</div>
                  <div style={{ fontWeight: 700, color: 'var(--text-primary)' }}>${(stats.marketCapUSD / 1e6).toFixed(2)}M</div>
                </div>
                <div style={{ textAlign: 'right' }}>
                  <div style={{ fontSize: '0.78rem', color: 'var(--text-muted)' }}>24h Volume</div>
                  <div style={{ fontWeight: 700, color: 'var(--text-primary)' }}>${stats.volumeUSD24h.toLocaleString()}</div>
                </div>
              </div>
            </div>
            <ResponsiveContainer width="100%" height={220}>
              <AreaChart data={CHART_DATA}>
                <defs>
                  <linearGradient id="synGrad" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="var(--syn-400)" stopOpacity={0.25} />
                    <stop offset="95%" stopColor="var(--syn-400)" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <XAxis dataKey="day" hide />
                <YAxis hide domain={['auto', 'auto']} />
                <Tooltip
                  contentStyle={{ background: 'var(--obsidian-700)', border: '1px solid var(--border-medium)', borderRadius: 8, fontSize: '0.82rem' }}
                  formatter={v => [`$${v.toFixed(8)}`, 'SYN Price']}
                  labelFormatter={d => `Day ${d}`}
                />
                <Area type="monotone" dataKey="price" stroke="var(--syn-400)" strokeWidth={2} fill="url(#synGrad)" dot={false} />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        </div>
      </section>

      {/* ── FEATURES ── */}
      <section className="section" style={{ background: 'var(--obsidian-800)', borderTop: '1px solid var(--border-subtle)', borderBottom: '1px solid var(--border-subtle)' }}>
        <div className="container">
          <div className="text-center" style={{ marginBottom: 64 }}>
            <span className="label">Platform Features</span>
            <h2>Everything You Need. Nothing You Don't.</h2>
          </div>
          <div className="grid-3">
            {[
              {
                icon: '⛓️',
                title: 'L1 Block Explorer',
                desc: 'Real-time blocks, transactions, state roots, and Merkle proofs. Search any address, block, or TX hash.',
                link: '/explorer', cta: 'Open Explorer',
                accent: 'var(--syn-400)',
              },
              {
                icon: '🔄',
                title: 'Native DEX',
                desc: 'AMM-based DEX using the x·y=k invariant with a 0.3% swap fee. Add liquidity and earn yield on SYN pairs.',
                link: '/dex', cta: 'Start Trading',
                accent: 'var(--purple-400)',
              },
              {
                icon: '🔐',
                title: 'Sovereign Escrow',
                desc: 'Protocol-level escrow with 1% commission routed to the Founder. Immutable, trustless, and on-chain.',
                link: '/escrow', cta: 'Create Escrow',
                accent: 'var(--gold-400)',
              },
              {
                icon: '🛡️',
                title: 'Validator Network',
                desc: 'Run a sovereign validator. Earn block rewards. Protected by the DMAS Immune System auto-slash protocol.',
                link: '/validators', cta: 'Run a Validator',
                accent: 'var(--green-400)',
              },
              {
                icon: '👁️',
                title: 'Inner Circle',
                desc: `Limited to 100 founding members. ${stats.innerCircleSlotsFilled} slots filled. 200M SYN per slot. Direct protocol governance rights.`,
                link: '/inner-circle', cta: 'Claim Your Slot',
                accent: 'var(--syn-400)',
              },
              {
                icon: '💼',
                title: 'SYN Wallet',
                desc: 'Check balances, build and submit signed transactions, view your nonce and asset holdings — directly in the browser.',
                link: '/wallet', cta: 'Open Wallet',
                accent: 'var(--gold-400)',
              },
            ].map(f => (
              <Link key={f.title} to={f.link} style={{ textDecoration: 'none' }}>
                <div className="card" style={{ height: '100%', cursor: 'pointer', borderTop: `2px solid ${f.accent}` }}>
                  <div style={{ fontSize: '2rem', marginBottom: 16 }}>{f.icon}</div>
                  <h3 style={{ fontSize: '1.2rem', marginBottom: 10, color: 'var(--text-primary)' }}>{f.title}</h3>
                  <p style={{ fontSize: '0.9rem', marginBottom: 20 }}>{f.desc}</p>
                  <span style={{ fontSize: '0.85rem', fontWeight: 600, color: f.accent }}>
                    {f.cta} →
                  </span>
                </div>
              </Link>
            ))}
          </div>
        </div>
      </section>

      {/* ── INNER CIRCLE CTA ── */}
      <section className="section">
        <div className="container">
          <div style={{
            borderRadius: 'var(--radius-xl)',
            background: 'linear-gradient(135deg, rgba(0,229,255,0.05) 0%, rgba(171,71,188,0.05) 100%)',
            border: '1px solid var(--border-medium)',
            padding: '80px 64px',
            textAlign: 'center',
            position: 'relative', overflow: 'hidden',
            boxShadow: 'var(--shadow-glow)',
          }}>
            <div style={{ position: 'absolute', top: -100, right: -100, width: 400, height: 400, borderRadius: '50%', background: 'radial-gradient(circle, rgba(0,229,255,0.07) 0%, transparent 70%)', pointerEvents: 'none' }} />
            <span className="badge badge-gold" style={{ marginBottom: 24 }}>
              🏆 {100 - stats.innerCircleSlotsFilled} Slots Remaining
            </span>
            <h2 style={{ marginBottom: 16 }}>Join the Synthos Inner Circle</h2>
            <p style={{ maxWidth: 560, margin: '0 auto 40px', fontSize: '1.05rem' }}>
              200,000,000 SYN per slot. Only 100 founding members ever.
              Inner Circle members receive governance rights, priority validator access,
              and direct participation in network economics.
            </p>
            <div style={{ display: 'flex', gap: 16, justifyContent: 'center', flexWrap: 'wrap' }}>
              <Link to="/inner-circle" className="btn btn-gold btn-lg">
                Claim Your Slot — 200M SYN
              </Link>
              <Link to="/inner-circle" className="btn btn-secondary btn-lg">
                Learn More
              </Link>
            </div>
          </div>
        </div>
      </section>

    </div>
  )
}
