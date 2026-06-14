import React from 'react';
import Hero from './components/Hero';
import HeroAgent from './components/HeroAgent';
import ImmuneDashboard from './components/ImmuneDashboard';
import StoryAgent from './components/StoryAgent';

function App() {
  return (
    <main className="min-h-screen bg-obsidian flex flex-col items-center justify-start p-8">
      {/* Static Hero UI (branding) */}
      <Hero />
      <ImmuneDashboard />
      {/* AI‑generated tagline */}
      <HeroAgent />
      {/* AI‑generated story timeline */}
      <StoryAgent />
    </main>
  );
}

export default App;
