import React from 'react';

const Hero = () => {
  return (
    <div className="relative min-h-screen bg-obsidian flex items-center justify-center overflow-hidden">
      {/* Background Glows */}
      <div className="absolute top-[-10%] left-[-10%] w-[50%] h-[50%] bg-collective/10 rounded-full blur-[120px]" />
      <div className="absolute bottom-[-10%] right-[-10%] w-[50%] h-[50%] bg-accent/5 rounded-full blur-[120px]" />

      <div className="container mx-auto px-6 relative z-10 text-center">
        <div className="inline-block px-4 py-1.5 mb-6 rounded-full border border-white/10 bg-white/5 backdrop-blur-md">
          <span className="text-sm font-semibold bg-gradient-to-r from-collective to-accent bg-clip-text text-transparent">
            The Collective 2.0 is Live
          </span>
        </div>
        
        <h1 className="text-6xl md:text-8xl font-bold text-white mb-8 tracking-tighter">
          Sovereign Identity <br /> 
          <span className="bg-gradient-to-r from-white via-white to-white/40 bg-clip-text text-transparent">
            Persistence Defined.
          </span>
        </h1>

        <p className="text-xl text-gray-400 max-w-2xl mx-auto mb-12">
          A decentralized L1 mesh with 100B SYN supply, 29B locked liquidity, 
          and an automated 20-year founder vesting schedule. Built for the community.
        </p>

        <div className="flex items-center justify-center gap-4">
          <button className="px-8 py-4 bg-collective hover:bg-collective/80 text-white rounded-2xl font-bold transition-all transform hover:scale-105 shadow-[0_0_40px_rgba(139,92,246,0.3)]">
            Launch Exchange
          </button>
          <button className="px-8 py-4 bg-white/5 hover:bg-white/10 text-white border border-white/10 rounded-2xl font-bold transition-all">
            View Manifesto
          </button>
        </div>

        <div className="mt-24 grid grid-cols-1 md:grid-cols-3 gap-8 max-w-4xl mx-auto">
          <div className="p-6 rounded-3xl bg-white/5 border border-white/10 backdrop-blur-xl">
            <div className="text-gray-400 text-sm mb-1 uppercase tracking-widest font-semibold">Treasury</div>
            <div className="text-2xl font-bold text-white">50.0B SYN</div>
          </div>
          <div className="p-6 rounded-3xl bg-white/5 border border-white/10 backdrop-blur-xl">
            <div className="text-gray-400 text-sm mb-1 uppercase tracking-widest font-semibold">Locked Liquidity</div>
            <div className="text-2xl font-bold text-white">29.0B SYN</div>
          </div>
          <div className="p-6 rounded-3xl bg-white/5 border border-white/10 backdrop-blur-xl">
            <div className="text-gray-400 text-sm mb-1 uppercase tracking-widest font-semibold">Staked Supply</div>
            <div className="text-2xl font-bold text-white">79.2B SYN</div>
          </div>
        </div>
      </div>

      {/* Persistence Mantra Footer */}
      <div className="absolute bottom-10 left-1/2 -translate-x-1/2 text-white/20 text-xs font-mono uppercase tracking-[0.5em]">
        Persistence is the only requirement for success
      </div>
    </div>
  );
};

export default Hero;
