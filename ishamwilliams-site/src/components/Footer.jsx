import { Link } from 'react-router-dom'

const LINKS = {
  Platform: [
    { label: 'Block Explorer', to: '/explorer' },
    { label: 'DEX', to: '/dex' },
    { label: 'Escrow', to: '/escrow' },
    { label: 'Wallet', to: '/wallet' },
  ],
  Network: [
    { label: 'Validators', to: '/validators' },
    { label: 'Inner Circle', to: '/inner-circle' },
    { label: 'Tokenomics', to: '/#tokenomics' },
    { label: 'Governance', to: '/#governance' },
  ],
  Resources: [
    { label: 'Documentation', to: '/docs' },
    { label: 'Whitepaper', to: '/whitepaper' },
    { label: 'Security', to: '/security' },
    { label: 'API Reference', to: '/api-docs' },
  ],
}

export default function Footer() {
  return (
    <footer style={{
      borderTop: '1px solid var(--border-subtle)',
      background: 'var(--obsidian-800)',
      padding: '64px 0 32px',
      position: 'relative',
      zIndex: 1,
    }}>
      <div className="container">
        <div style={{ display: 'grid', gridTemplateColumns: '2fr 1fr 1fr 1fr', gap: 48, marginBottom: 64 }}>

          {/* Brand */}
          <div>
            <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 16 }}>
              <div style={{
                width: 44, height: 44, borderRadius: 12,
                background: 'linear-gradient(135deg, var(--syn-600), var(--syn-400))',
                display: 'flex', alignItems: 'center', justifyContent: 'center',
                fontFamily: 'var(--font-display)', fontWeight: 800, fontSize: '1.1rem',
                color: 'var(--obsidian-900)',
                boxShadow: '0 0 20px var(--syn-glow)',
              }}>SC</div>
              <div>
                <div style={{ fontFamily: 'var(--font-display)', fontWeight: 700, fontSize: '1rem', color: 'var(--text-primary)' }}>
                  SYNTHOS COLLECTIVE
                </div>
                <div style={{ fontSize: '0.72rem', color: 'var(--syn-400)', letterSpacing: '0.1em', textTransform: 'uppercase' }}>
                  Isham Williams Blockchains
                </div>
              </div>
            </div>
            <p style={{ fontSize: '0.9rem', color: 'var(--text-muted)', lineHeight: 1.75, maxWidth: 320, marginBottom: 24 }}>
              Your first digital weapon in the war over data sovereignty.
              A cloudless, cryptographically secure L1 network governed by a Distributed Immune System.
              Absolute Silence. Absolute Security.
            </p>
            {/* Chain Status Pill */}
            <div style={{
              display: 'inline-flex', alignItems: 'center', gap: 8,
              padding: '6px 14px', borderRadius: 999,
              background: 'rgba(102,187,106,0.08)',
              border: '1px solid rgba(102,187,106,0.2)',
            }}>
              <div className="glow-dot" style={{ background: 'var(--green-400)', boxShadow: '0 0 8px var(--green-400)' }} />
              <span style={{ fontSize: '0.8rem', color: 'var(--green-400)', fontWeight: 600 }}>
                Network Operational
              </span>
            </div>
          </div>

          {/* Link columns */}
          {Object.entries(LINKS).map(([section, links]) => (
            <div key={section}>
              <div style={{
                fontSize: '0.75rem', fontWeight: 700,
                letterSpacing: '0.12em', textTransform: 'uppercase',
                color: 'var(--syn-400)', marginBottom: 16,
              }}>
                {section}
              </div>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                {links.map(({ label, to }) => (
                  <Link
                    key={label}
                    to={to}
                    style={{
                      fontSize: '0.9rem', color: 'var(--text-muted)',
                      textDecoration: 'none', transition: 'color 0.2s',
                    }}
                    onMouseEnter={e => e.target.style.color = 'var(--text-primary)'}
                    onMouseLeave={e => e.target.style.color = 'var(--text-muted)'}
                  >
                    {label}
                  </Link>
                ))}
              </div>
            </div>
          ))}
        </div>

        {/* Divider */}
        <div className="divider" style={{ margin: '0 0 24px' }} />

        {/* Bottom row */}
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: 16 }}>
          <p style={{ fontSize: '0.82rem', color: 'var(--text-muted)', margin: 0 }}>
            © {new Date().getFullYear()} Isham Williams Blockchains LLC. All rights reserved.
            &nbsp;·&nbsp; Chain ID: <span style={{ fontFamily: 'var(--font-mono)', color: 'var(--syn-400)' }}>synthos-1</span>
            &nbsp;·&nbsp; Ticker: <span style={{ fontFamily: 'var(--font-mono)', color: 'var(--syn-400)' }}>$SYN</span>
          </p>
          <div style={{ display: 'flex', gap: 20 }}>
            {['Privacy Policy', 'Terms of Service', 'Legal'].map(item => (
              <Link
                key={item}
                to={`/${item.toLowerCase().replace(/ /g, '-')}`}
                style={{ fontSize: '0.82rem', color: 'var(--text-muted)', textDecoration: 'none' }}
                onMouseEnter={e => e.target.style.color = 'var(--text-secondary)'}
                onMouseLeave={e => e.target.style.color = 'var(--text-muted)'}
              >
                {item}
              </Link>
            ))}
          </div>
        </div>

        {/* Antigravity footnote */}
        <div style={{
          marginTop: 20,
          paddingTop: 16,
          borderTop: '1px solid var(--border-subtle)',
          textAlign: 'center',
        }}>
          <p style={{
            fontSize: '0.72rem',
            color: 'var(--text-muted)',
            margin: 0,
            letterSpacing: '0.04em',
          }}>
            This site was made to exact specs by{' '}
            <span style={{
              background: 'linear-gradient(90deg, var(--syn-400), var(--purple-400))',
              WebkitBackgroundClip: 'text',
              WebkitTextFillColor: 'transparent',
              backgroundClip: 'text',
              fontWeight: 700,
              letterSpacing: '0.06em',
            }}>
              Antigravity
            </span>
          </p>
        </div>
      </div>

      <style>{`
        @media (max-width: 1024px) {
          footer .container > div:first-child {
            grid-template-columns: 1fr 1fr !important;
          }
        }
        @media (max-width: 640px) {
          footer .container > div:first-child {
            grid-template-columns: 1fr !important;
          }
        }
      `}</style>
    </footer>
  )
}
