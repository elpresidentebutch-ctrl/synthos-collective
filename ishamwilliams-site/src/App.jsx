import { BrowserRouter, Routes, Route } from 'react-router-dom'
import Navbar from './components/Navbar'
import Footer from './components/Footer'
import LiveStats from './components/LiveStats'
import Home from './pages/Home'
import Explorer from './pages/Explorer'
import DEX from './pages/DEX'
import Escrow from './pages/Escrow'
import Validators from './pages/Validators'
import InnerCircle from './pages/InnerCircle'
import Wallet from './pages/Wallet'
import Admin from './pages/Admin'

function NotFound() {
  return (
    <div className="page-content" style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', minHeight: '80vh', textAlign: 'center' }}>
      <div>
        <div style={{ fontSize: '5rem', marginBottom: 16 }}>⛓️</div>
        <h2 style={{ marginBottom: 12 }}>404 — Block Not Found</h2>
        <p style={{ color: 'var(--text-muted)', marginBottom: 28 }}>This chain doesn't have that block.</p>
        <a href="/" className="btn btn-primary">← Return Home</a>
      </div>
    </div>
  )
}

export default function App() {
  return (
    <BrowserRouter>
      <div className="bg-grid" />
      <div className="bg-mesh" />
      <Navbar />
      <LiveStats />
      <Routes>
        <Route path="/"             element={<Home />} />
        <Route path="/explorer"     element={<Explorer />} />
        <Route path="/dex"          element={<DEX />} />
        <Route path="/escrow"       element={<Escrow />} />
        <Route path="/validators"   element={<Validators />} />
        <Route path="/inner-circle" element={<InnerCircle />} />
        <Route path="/wallet"       element={<Wallet />} />
        {/* Founder admin — not linked anywhere in the nav */}
        <Route path="/admin"        element={<Admin />} />
        <Route path="*"             element={<NotFound />} />
      </Routes>
      <Footer />
    </BrowserRouter>
  )
}
