import { useState } from 'react'

const API = 'http://localhost:4000/api'

export default function AdminPage() {
  const [password, setPassword] = useState('')
  const [token, setToken] = useState(null)
  const [loginErr, setLoginErr] = useState('')
  const [members, setMembers] = useState([])
  const [loading, setLoading] = useState(false)
  const [search, setSearch] = useState('')

  async function handleLogin(e) {
    e.preventDefault()
    setLoginErr('')
    try {
      const r = await fetch(`${API}/admin/login`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password })
      })
      const d = await r.json()
      if (!r.ok) { setLoginErr(d.error || 'Invalid password'); return }
      setToken(d.token)
      loadMembers(d.token)
    } catch {
      setLoginErr('Server unreachable. Make sure server.js is running.')
    }
  }

  async function loadMembers(t) {
    setLoading(true)
    try {
      const r = await fetch(`${API}/admin/members`, { headers: { 'x-admin-token': t } })
      const d = await r.json()
      setMembers(d.members || [])
    } catch { setMembers([]) }
    setLoading(false)
  }

  async function deleteMember(id) {
    if (!window.confirm('Delete this member?')) return
    await fetch(`${API}/admin/members/${id}`, { method: 'DELETE', headers: { 'x-admin-token': token } })
    loadMembers(token)
  }

  function exportCSV() {
    window.open(`${API}/admin/members/export`, '_blank')
  }

  const filtered = members.filter(m =>
    m.name?.toLowerCase().includes(search.toLowerCase()) ||
    m.email?.toLowerCase().includes(search.toLowerCase()) ||
    m.phone?.includes(search)
  )

  if (!token) {
    return (
      <div className="page-content page-enter" style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', minHeight: '100vh' }}>
        <div style={{ maxWidth: 420, width: '100%', padding: '0 24px' }}>
          <div style={{ textAlign: 'center', marginBottom: 40 }}>
            <div style={{ fontSize: '2.5rem', marginBottom: 12 }}>🔐</div>
            <h2 style={{ marginBottom: 8 }}>Founder Admin</h2>
            <p style={{ color: 'var(--text-muted)', fontSize: '0.9rem' }}>This page is restricted to the Founder only.</p>
          </div>
          <form onSubmit={handleLogin} className="card" style={{ padding: 32 }}>
            <div className="form-group">
              <label>Admin Password</label>
              <input
                className="input"
                type="password"
                placeholder="Enter founder password"
                value={password}
                onChange={e => setPassword(e.target.value)}
                required
                autoComplete="off"
                id="admin-password"
              />
            </div>
            {loginErr && <div className="alert alert-error" style={{ marginBottom: 16 }}>{loginErr}</div>}
            <button type="submit" className="btn btn-primary w-full" style={{ justifyContent: 'center' }}>
              Access Admin Dashboard
            </button>
          </form>
        </div>
      </div>
    )
  }

  return (
    <div className="page-content page-enter">
      <div style={{ background: 'var(--obsidian-800)', borderBottom: '1px solid var(--border-subtle)', padding: '40px 0 24px' }}>
        <div className="container">
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: 16 }}>
            <div>
              <div style={{ fontSize: '0.75rem', color: 'var(--gold-400)', fontWeight: 700, letterSpacing: '0.1em', textTransform: 'uppercase', marginBottom: 4 }}>
                🔐 FOUNDER ADMIN — PRIVATE
              </div>
              <h1 style={{ fontSize: '2rem' }}>Member Registry</h1>
              <p style={{ color: 'var(--text-muted)', marginTop: 4 }}>
                Total members: <span style={{ color: 'var(--syn-400)', fontWeight: 700 }}>{members.length}</span>
              </p>
            </div>
            <div style={{ display: 'flex', gap: 12 }}>
              <button className="btn btn-secondary" onClick={exportCSV}>⬇️ Export CSV</button>
              <button className="btn btn-secondary" onClick={() => loadMembers(token)} disabled={loading}>
                {loading ? 'Loading...' : '🔄 Refresh'}
              </button>
              <button className="btn btn-sm" style={{ background: 'transparent', border: '1px solid var(--border-subtle)', color: 'var(--text-muted)' }} onClick={() => setToken(null)}>
                Log Out
              </button>
            </div>
          </div>
        </div>
      </div>

      <div className="container" style={{ padding: '32px 24px' }}>

        {/* Stats row */}
        <div className="grid-4" style={{ marginBottom: 32 }}>
          {[
            { label: 'Total Members', val: members.length, color: 'var(--syn-400)' },
            { label: 'Verified', val: members.filter(m => m.verified).length, color: 'var(--green-400)' },
            { label: 'This Week', val: members.filter(m => Date.now() - new Date(m.joinedAt).getTime() < 7 * 86400000).length, color: 'var(--gold-400)' },
            { label: 'Today', val: members.filter(m => new Date(m.joinedAt).toDateString() === new Date().toDateString()).length, color: 'var(--purple-400)' },
          ].map(s => (
            <div key={s.label} className="card" style={{ textAlign: 'center', padding: 20 }}>
              <div style={{ fontFamily: 'var(--font-display)', fontSize: '2rem', fontWeight: 800, color: s.color }}>{s.val}</div>
              <div style={{ fontSize: '0.78rem', color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.08em', marginTop: 4 }}>{s.label}</div>
            </div>
          ))}
        </div>

        {/* Search */}
        <div style={{ marginBottom: 20 }}>
          <input
            className="input"
            placeholder="Search by name, email, or phone..."
            value={search}
            onChange={e => setSearch(e.target.value)}
            id="admin-search"
          />
        </div>

        {/* Members Table */}
        {members.length === 0 ? (
          <div className="card" style={{ textAlign: 'center', padding: 60, color: 'var(--text-muted)' }}>
            <div style={{ fontSize: '2.5rem', marginBottom: 12 }}>📋</div>
            No members registered yet. When people sign up via the Inner Circle page, they'll appear here.
          </div>
        ) : (
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>#</th>
                  <th>Name</th>
                  <th>Email</th>
                  <th>Phone</th>
                  <th>Joined</th>
                  <th>Verified</th>
                  <th>Action</th>
                </tr>
              </thead>
              <tbody>
                {filtered.map((m, i) => (
                  <tr key={m.id}>
                    <td style={{ color: 'var(--text-muted)', fontSize: '0.82rem' }}>{i + 1}</td>
                    <td style={{ fontWeight: 600 }}>{m.name}</td>
                    <td style={{ fontFamily: 'var(--font-mono)', fontSize: '0.85rem', color: 'var(--syn-400)' }}>{m.email}</td>
                    <td style={{ fontFamily: 'var(--font-mono)', fontSize: '0.85rem', color: 'var(--text-secondary)' }}>{m.phone || '—'}</td>
                    <td style={{ fontSize: '0.82rem', color: 'var(--text-muted)' }}>{new Date(m.joinedAt).toLocaleString()}</td>
                    <td><span className={`badge ${m.verified ? 'badge-green' : 'badge-red'}`} style={{ fontSize: '0.7rem' }}>{m.verified ? '✓ Yes' : '✗ No'}</span></td>
                    <td>
                      <button onClick={() => deleteMember(m.id)} style={{ background: 'transparent', border: '1px solid rgba(239,83,80,0.3)', borderRadius: 6, color: 'var(--red-400)', cursor: 'pointer', padding: '4px 12px', fontSize: '0.78rem' }}>
                        Remove
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  )
}
