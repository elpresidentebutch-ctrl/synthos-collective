/**
 * SOVEREIGN B12 BRIDGE v2.0
 * Multi-Asset Integration for Momentum & NGOT
 */

const MomentumBridge = {
    relay: "http://synthos-anchor-1.world:8080",
    
    momentum_tiers: {
        0: { name: "Standard", features: ["tasks", "goals", "charts", "streaks"] },
        1: { name: "Pro", features: ["tasks", "goals", "charts", "streaks", "priority", "recurring", "tags", "analytics"] },
        2: { name: "Elite", features: ["tasks", "goals", "charts", "streaks", "priority", "recurring", "tags", "analytics", "collab", "time", "calendar"] }
    },

    ngot_trust_levels: {
        "none": { name: "Non-Partner", features: [] },
        "partner": { name: "Trust Partner", features: ["green_governance", "desal_access"] },
        "originator": { name: "Trust Originator", features: ["green_governance", "desal_access", "infrastructure_control"] }
    },

    async init(userAddr) {
        console.log(`[Sovereign] Initializing multi-asset check for ${userAddr}...`);
        const momentumTier = await this.getTierFromMesh(userAddr);
        const ngotStatus = await this.getNGOTFromMesh(userAddr);
        
        this.applyMomentum(momentumTier);
        this.applyNGOT(ngotStatus);
        
        return { momentum: momentumTier, ngot: ngotStatus };
    },

    async getTierFromMesh(userAddr) {
        try {
            const response = await fetch(`${this.relay}/status?addr=${userAddr}`);
            const state = await response.json();
            return state.account.assets?.momentum_level || 0;
        } catch (e) { return 0; }
    },

    async getNGOTFromMesh(userAddr) {
        try {
            const response = await fetch(`${this.relay}/status?addr=${userAddr}`);
            const state = await response.json();
            if (state.account.assets?.ngot_originator) return "originator";
            if ((state.account.assets?.ngot || 0) > 0) return "partner";
            return "none";
        } catch (e) { return "none"; }
    },

    applyMomentum(tierLevel) {
        const activeTier = this.momentum_tiers[tierLevel];
        const allFeatures = this.momentum_tiers[2].features;
        this._updateUI(allFeatures, activeTier.features, "momentum");
    },

    applyNGOT(status) {
        const activeTrust = this.ngot_trust_levels[status];
        const allFeatures = this.ngot_trust_levels["originator"].features;
        this._updateUI(allFeatures, activeTrust.features, "ngot");
    },

    _updateUI(allFeatures, activeFeatures, prefix) {
        allFeatures.forEach(feature => {
            const elements = document.querySelectorAll(`[data-${prefix}-feature="${feature}"]`);
            if (activeFeatures.includes(feature)) {
                elements.forEach(el => {
                    el.classList.remove(`${prefix}-locked`);
                    el.classList.add(`${prefix}-unlocked`);
                    if (el.tagName === 'BUTTON') el.disabled = false;
                });
            } else {
                elements.forEach(el => {
                    el.classList.add(`${prefix}-locked`);
                    el.classList.remove(`${prefix}-unlocked`);
                    if (el.tagName === 'BUTTON') el.disabled = true;
                });
            }
        });
    }
};

const style = document.createElement('style');
style.textContent = `
    .momentum-locked, .ngot-locked { opacity: 0.4; filter: grayscale(1); cursor: not-allowed !important; position: relative; transition: 0.3s; }
    .momentum-locked::after { content: '🔒'; position: absolute; top: 5px; right: 5px; font-size: 14px; }
    .ngot-locked::after { content: '🌿'; position: absolute; top: 5px; right: 5px; font-size: 14px; }
    .momentum-unlocked, .ngot-unlocked { opacity: 1; filter: none; }
`;
document.head.appendChild(style);

window.MomentumBridge = MomentumBridge;
