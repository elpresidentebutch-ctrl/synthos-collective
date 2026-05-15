import { useState, useEffect } from 'react'
import { getMockStats, TOKENOMICS } from '../api'
import CryptoNoiseEngine from '../components/CryptoNoiseEngine'

const VALIDATORS = [
  { id: 'validator-01', address: '0x205042f06cd3aa7d9a88deec39b9d0ba6b9fbf2b', name: 'Founder Node', stake: 1_000_000_000, uptime: 99.97, blocksProposed: 3842, status: 'active', since: '2024-01-01', slashCount: 0, isSelf: true },
  { id: 'validator-02', address: '0xa1b2c3d4e5f60718293a4b5c6d7e8f9012345678', name: 'Apex Validator', stake: 2_100_000_000, uptime: 99.91, blocksProposed: 2910, status: 'active', since: '2024-02-15', slashCount: 0 },
  { id: 'validator-03', address: '0xdeadbeef00000000000000000000000000000001', name: 'Obsidian Node', stake: 1_750_000_000, uptime: 99.44, blocksProposed: 2180, status: 'active', since: '2024-03-01', slashCount: 1 },
  { id: 'validator-04', address: '0xcafe0000000000000000000000000000deadc0de', name: 'Sovereign Stack', stake: 980_000_000, uptime: 98.82, blocksProposed: 1420, status: 'active', since: '2024-04-10', slashCount: 0 },
  { id: 'validator-05', address: '0x1111111111111111111111111111111111111111', name: 'Anon-V5', stake: 600_000_000, uptime: 97.10, blocksProposed: 887, status: 'active', since: '2024-05-01', slashCount: 2 },
  { id: 'validator-06', address: '0x2222222222222222222222222222222222222222', name: 'GridNode-06', stake: 450_000_000, uptime: 96.30, blocksProposed: 540, status: 'active', since: '2024-05-20', slashCount: 0 },
]

export default function Validators() {
  const stats = getMockStats()
  const [joinForm, setJoinForm] = useState({ address: '', stake: '' })
  const [joining, setJoining] = useState(false)
  const [joinResult, setJoinResult] = useState(null)
  const totalStaked = VALIDATORS.reduce((sum, v) => sum + v.stake, 0)

  async function handleJoin(e) {
    e.preventDefault()
    setJoining(true)
    await new Promise(r => setTimeout(r, 1400))
    setJoinResult({
      ok: true,
      msg: `Validator onboarding request submitted for ${joinForm.address.slice(0, 16)}... with ${parseInt(joinForm.stake || 0).toLocaleString()} SYN stake. Your node will be visible after the next block finalization. Run synthosd.exe to activate your node.`
    })
    setJoining(false)
  }

  return (
    <div className="page-content page-enter">
      <div style={{ background: 'var(--obsidian-800)', borderBottom: '1px solid var(--border-subtle)', padding: '48px 0 32px' }}>
        <div className="container">
          <span className="label">DMAS Immune System · Auto-Slash Protocol</span>
          <h1 style={{ fontSize: '2.4rem', marginBottom: 8 }}>Validator Network</h1>
          <p style={{ color: 'var(--text-muted)', maxWidth: 620 }}>
            Sovereign validators secure the Synthos L1 chain. Earn block rewards. Protected
            by the DMAS immune system — malicious validators are automatically slashed.
          </p>
        </div>
      </div>

      <div className="container" style={{ padding: '40px 24px' }}>

        {/* Network Stats */}
        <div className="grid-4" style={{ marginBottom: 48 }}>
          {[
            { label: 'Active Validators', val: stats.activeValidators, color: 'var(--green-400)' },
            { label: 'Total Staked', val: `${(totalStaked / 1e9).toFixed(2)}B SYN`, color: 'var(--syn-400)' },
            { label: 'Avg Uptime', val: `${(VALIDATORS.reduce((s, v) => s + v.uptime, 0) / VALIDATORS.length).toFixed(2)}%`, color: 'var(--text-primary)' },
            { label: 'Total Blocks Proposed', val: VALIDATORS.reduce((s, v) => s + v.blocksProposed, 0).toLocaleString(), color: 'var(--gold-400)' },
          ].map(s => (
            <div key={s.label} className="card" style={{ textAlign: 'center', padding: 20 }}>
              <div style={{ fontFamily: 'var(--font-mono)', fontSize: '1.7rem', fontWeight: 700, color: s.color }}>{s.val}</div>
              <div style={{ fontSize: '0.78rem', color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.08em', marginTop: 4 }}>{s.label}</div>
            </div>
          ))}
        </div>

        <div style={{ display: 'grid', gridTemplateColumns: '1fr 360px', gap: 40, alignItems: 'start' }}>

          {/* Validator Table */}
          <div>
            <h3 style={{ marginBottom: 20 }}>Active Validators</h3>
            <div className="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Validator</th>
                    <th>Stake</th>
                    <th>Share</th>
                    <th>Uptime</th>
                    <th>Blocks</th>
                    <th>Slashes</th>
                    <th>Status</th>
                  </tr>
                </thead>
                <tbody>
                  {VALIDATORS.map(v => (
                    <tr key={v.id}>
                      <td>
                        <div>
                          <div style={{ fontWeight: 600, fontSize: '0.9rem', display: 'flex', alignItems: 'center', gap: 6 }}>
                            {v.name}
                            {v.isSelf && <span className="badge badge-gold" style={{ fontSize: '0.65rem', padding: '2px 8px' }}>Founder</span>}
                          </div>
                          <div style={{ fontFamily: 'var(--font-mono)', fontSize: '0.72rem', color: 'var(--text-muted)' }}>
                            {v.address.slice(0, 14)}...
                          </div>
                        </div>
                      </td>
                      <td>
                        <div style={{ fontFamily: 'var(--font-mono)', fontWeight: 600 }}>
                          {(v.stake / 1e9).toFixed(2)}B SYN
                        </div>
                      </td>
                      <td>
                        <div>
                          <div style={{ fontSize: '0.85rem', fontWeight: 600 }}>
                            {((v.stake / totalStaked) * 100).toFixed(1)}%
                          </div>
                          <div className="progress-bar" style={{ width: 60, marginTop: 4 }}>
                            <div className="progress-fill" style={{ width: `${(v.stake / totalStaked) * 100}%` }} />
                          </div>
                        </div>
                      </td>
                      <td>
                        <span style={{ color: v.uptime > 99 ? 'var(--green-400)' : v.uptime > 97 ? 'var(--gold-400)' : 'var(--red-400)', fontWeight: 600 }}>
                          {v.uptime}%
                        </span>
                      </td>
                      <td><span style={{ fontFamily: 'var(--font-mono)' }}>{v.blocksProposed.toLocaleString()}</span></td>
                      <td>
                        <span style={{ color: v.slashCount > 0 ? 'var(--red-400)' : 'var(--text-muted)', fontWeight: v.slashCount > 0 ? 600 : 400 }}>
                          {v.slashCount}
                        </span>
                      </td>
                      <td><span className="badge badge-green" style={{ fontSize: '0.7rem' }}>● Active</span></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>

            {/* DMAS Info */}
            <div className="card" style={{ marginTop: 24, borderLeft: '3px solid var(--red-400)', background: 'rgba(239,83,80,0.03)' }}>
              <div style={{ fontSize: '0.78rem', color: 'var(--red-400)', fontWeight: 700, letterSpacing: '0.1em', textTransform: 'uppercase', marginBottom: 10 }}>
                DMAS Immune System
              </div>
              <p style={{ fontSize: '0.88rem', color: 'var(--text-secondary)', marginBottom: 8 }}>
                The Decentralized Malicious Agent Slash system auto-enforces validator honesty.
                When cumulative stake from reporters exceeds the slash threshold (1M SYN), the target validator
                is automatically penalized by <strong style={{ color: 'var(--text-primary)' }}>10% of their balance</strong>.
              </p>
              <div style={{ fontFamily: 'var(--font-mono)', fontSize: '0.78rem', color: 'var(--text-muted)', background: 'var(--obsidian-900)', padding: '10px 14px', borderRadius: 6 }}>
                State.Slash(targetAddr, 10) // hardcoded in consensus
              </div>
            </div>
          </div>

          {/* Join Panel */}
          <div style={{ position: 'sticky', top: 'calc(var(--nav-h) + 24px)' }}>
            <div className="card">
              <h3 style={{ marginBottom: 8 }}>Become a Validator</h3>
              <p style={{ fontSize: '0.88rem', color: 'var(--text-muted)', marginBottom: 24 }}>
                Run <span style={{ fontFamily: 'var(--font-mono)', color: 'var(--syn-400)' }}>synthosd.exe</span> on your machine and register your address below. Minimum stake is required to participate in consensus.
              </p>

              {joinResult ? (
                <div>
                  <div className="alert alert-success" style={{ marginBottom: 16 }}>
                    ✅ {joinResult.msg}
                  </div>
                  <button className="btn btn-secondary w-full" style={{ justifyContent: 'center' }} onClick={() => { setJoinResult(null); setJoinForm({ address: '', stake: '' }) }}>
                    Register Another
                  </button>
                </div>
              ) : (
                <form onSubmit={handleJoin}>
                  <div className="form-group">
                    <label>Validator Address *</label>
                    <input className="input input-mono" placeholder="0x..." value={joinForm.address} onChange={e => setJoinForm(f => ({ ...f, address: e.target.value }))} required id="validator-address" />
                  </div>
                  <div className="form-group">
                    <label>Stake Amount (SYN) *</label>
                    <input className="input input-mono" type="number" placeholder="Minimum: 1,000,000" value={joinForm.stake} onChange={e => setJoinForm(f => ({ ...f, stake: e.target.value }))} required min="1000000" id="validator-stake" />
                  </div>
                  <div className="alert alert-info" style={{ marginBottom: 20, fontSize: '0.82rem' }}>
                    Download <strong>synthosd.exe</strong> from the project repo, then register your node address here to join consensus.
                  </div>
                  <button type="submit" className="btn btn-primary w-full" style={{ justifyContent: 'center' }} disabled={joining}>
                    {joining ? 'Submitting...' : '⚡ Register Validator Node'}
                  </button>
                </form>
              )}
            </div>

            {/* Reward model */}
            <div className="card" style={{ marginTop: 16, padding: 20 }}>
              <div style={{ fontSize: '0.78rem', color: 'var(--syn-400)', fontWeight: 700, letterSpacing: '0.1em', textTransform: 'uppercase', marginBottom: 12 }}>Reward Model</div>
              {[
                { label: 'Block Reward', value: '50% of collected fees' },
                { label: 'Fee Burn', value: '50% burned from circulation' },
                { label: 'Slash Penalty', value: '10% of balance' },
                { label: 'Slash Threshold', value: '1M SYN reported stake' },
              ].map(r => (
                <div key={r.label} style={{ display: 'flex', justifyContent: 'space-between', padding: '8px 0', borderBottom: '1px solid var(--border-subtle)', fontSize: '0.85rem' }}>
                  <span style={{ color: 'var(--text-muted)' }}>{r.label}</span>
                  <span style={{ fontWeight: 600, color: 'var(--text-primary)' }}>{r.value}</span>
                </div>
              ))}
            </div>

            {/* Live noise preview */}
            <div style={{ marginTop: 16 }}>
              <div style={{ fontSize: '0.72rem', color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.1em', marginBottom: 8 }}>Your Node's Output (Live Preview)</div>
              <CryptoNoiseEngine compact />
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
