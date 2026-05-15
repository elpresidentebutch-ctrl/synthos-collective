import { useState, useEffect, useRef } from 'react'
import { Link, useLocation } from 'react-router-dom'

const NAV_LINKS = [
  { to: '/', label: 'Home' },
  { to: '/explorer', label: 'Explorer' },
  { to: '/dex', label: 'DEX' },
  { to: '/escrow', label: 'Escrow' },
  { to: '/validators', label: 'Validators' },
  { to: '/inner-circle', label: 'Inner Circle' },
  { to: '/wallet', label: 'Wallet' },
]

export default function Navbar() {
  const [scrolled, setScrolled] = useState(false)
  const [mobileOpen, setMobileOpen] = useState(false)
  const { pathname } = useLocation()
  const menuRef = useRef(null)

  useEffect(() => {
    const onScroll = () => setScrolled(window.scrollY > 20)
    window.addEventListener('scroll', onScroll)
    return () => window.removeEventListener('scroll', onScroll)
  }, [])

  useEffect(() => {
    setMobileOpen(false)
  }, [pathname])

  return (
    <nav style={{
      position: 'fixed', top: 0, left: 0, right: 0, zIndex: 900,
      height: 'var(--nav-h)',
      display: 'flex', alignItems: 'center',
      background: scrolled
        ? 'rgba(8,11,15,0.95)'
        : 'rgba(8,11,15,0.7)',
      backdropFilter: 'blur(20px)',
      borderBottom: scrolled ? '1px solid var(--border-subtle)' : '1px solid transparent',
      transition: 'all 0.3s',
      padding: '0 32px',
    }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', width: '100%', maxWidth: 1280, margin: '0 auto' }}>

        {/* Logo */}
        <Link to="/" style={{ display: 'flex', alignItems: 'center', gap: 12, textDecoration: 'none' }}>
          <div style={{
            width: 40, height: 40, borderRadius: 10,
            background: 'linear-gradient(135deg, var(--syn-600), var(--syn-400))',
            display: 'flex', alignItems: 'center', justifyContent: 'center',
            boxShadow: '0 0 16px var(--syn-glow-strong)',
            fontSize: '1.1rem', fontWeight: 800, color: 'var(--obsidian-900)',
            fontFamily: 'var(--font-display)',
          }}>SC</div>
          <div>
            <div style={{ fontFamily: 'var(--font-display)', fontWeight: 700, fontSize: '1.05rem', color: 'var(--text-primary)', lineHeight: 1.2 }}>
              SYNTHOS COLLECTIVE
            </div>
            <div style={{ fontSize: '0.62rem', color: 'var(--syn-400)', letterSpacing: '0.1em', textTransform: 'uppercase', lineHeight: 1 }}>
              Your First Digital Weapon
            </div>
          </div>
        </Link>

        {/* Desktop Links */}
        <div style={{ display: 'flex', alignItems: 'center', gap: 4 }} className="desktop-nav">
          {NAV_LINKS.map(({ to, label }) => (
            <Link
              key={to}
              to={to}
              style={{
                padding: '8px 14px',
                borderRadius: 8,
                fontSize: '0.9rem',
                fontWeight: pathname === to ? 600 : 500,
                color: pathname === to ? 'var(--syn-400)' : 'var(--text-secondary)',
                background: pathname === to ? 'var(--syn-glow)' : 'transparent',
                border: pathname === to ? '1px solid var(--border-subtle)' : '1px solid transparent',
                transition: 'all 0.2s',
                textDecoration: 'none',
              }}
              onMouseEnter={e => {
                if (pathname !== to) {
                  e.target.style.color = 'var(--text-primary)'
                  e.target.style.background = 'rgba(255,255,255,0.04)'
                }
              }}
              onMouseLeave={e => {
                if (pathname !== to) {
                  e.target.style.color = 'var(--text-secondary)'
                  e.target.style.background = 'transparent'
                }
              }}
            >
              {label}
            </Link>
          ))}
        </div>

        {/* CTA + Mobile Toggle */}
        <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          <Link to="/inner-circle" className="btn btn-primary btn-sm" style={{ display: 'none' }} id="nav-cta-desktop">
            Join Inner Circle
          </Link>
          <button
            onClick={() => setMobileOpen(o => !o)}
            style={{
              background: 'transparent', border: '1px solid var(--border-subtle)',
              borderRadius: 8, width: 40, height: 40, cursor: 'pointer',
              display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center',
              gap: 5, padding: 10, color: 'var(--text-primary)'
            }}
            aria-label="Toggle menu"
            className="mobile-toggle"
          >
            <span style={{ width: 18, height: 2, background: mobileOpen ? 'var(--syn-400)' : 'currentColor', borderRadius: 2, transition: 'all 0.2s', transform: mobileOpen ? 'rotate(45deg) translateY(7px)' : 'none' }} />
            <span style={{ width: 18, height: 2, background: 'currentColor', borderRadius: 2, opacity: mobileOpen ? 0 : 1, transition: 'opacity 0.2s' }} />
            <span style={{ width: 18, height: 2, background: mobileOpen ? 'var(--syn-400)' : 'currentColor', borderRadius: 2, transition: 'all 0.2s', transform: mobileOpen ? 'rotate(-45deg) translateY(-7px)' : 'none' }} />
          </button>
        </div>
      </div>

      {/* Mobile Menu */}
      {mobileOpen && (
        <div style={{
          position: 'absolute', top: 'var(--nav-h)', left: 0, right: 0,
          background: 'rgba(8,11,15,0.98)', backdropFilter: 'blur(20px)',
          borderBottom: '1px solid var(--border-subtle)',
          padding: '16px 24px 24px',
          display: 'flex', flexDirection: 'column', gap: 4,
          animation: 'fadeSlideIn 0.2s ease-out both',
        }}>
          {NAV_LINKS.map(({ to, label }) => (
            <Link
              key={to}
              to={to}
              style={{
                padding: '12px 16px',
                borderRadius: 8,
                fontSize: '1rem',
                fontWeight: pathname === to ? 600 : 400,
                color: pathname === to ? 'var(--syn-400)' : 'var(--text-secondary)',
                background: pathname === to ? 'var(--syn-glow)' : 'transparent',
                textDecoration: 'none',
                borderLeft: pathname === to ? '2px solid var(--syn-400)' : '2px solid transparent',
              }}
            >
              {label}
            </Link>
          ))}
          <div style={{ marginTop: 16, paddingTop: 16, borderTop: '1px solid var(--border-subtle)' }}>
            <Link to="/inner-circle" className="btn btn-primary w-full" style={{ justifyContent: 'center' }}>
              Join the Inner Circle
            </Link>
          </div>
        </div>
      )}

      <style>{`
        @media (min-width: 768px) {
          .desktop-nav { display: flex !important; }
          #nav-cta-desktop { display: inline-flex !important; }
          .mobile-toggle { display: none !important; }
        }
        @media (max-width: 767px) {
          .desktop-nav { display: none !important; }
        }
      `}</style>
    </nav>
  )
}
