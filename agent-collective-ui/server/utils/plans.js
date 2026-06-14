// server/utils/plans.js – definition of subscription tiers
export const PLANS = {
  FREE: {
    id: "free",
    name: "Free Trial",
    priceUSD: 0,
    quota: 200, // calls per month
    model: "gpt-3.5-turbo",
    description: "Limited access – 200 calls per month"
  },
  BASIC: {
    id: "basic",
    name: "Basic",
    priceUSD: 10,
    quota: 1000,
    model: "gpt-4o-mini",
    description: "Standard access – 1,000 calls per month"
  },
  PRO: {
    id: "pro",
    name: "Pro",
    priceUSD: 50,
    quota: 5000,
    model: "gpt-4o",
    description: "Professional access – 5,000 calls per month"
  },
  PREMIUM: {
    id: "premium",
    name: "Premium",
    priceUSD: 150,
    quota: 15000,
    model: "gpt-4o-mini-32k",
    description: "Premium access – 15,000 calls per month"
  },
  ENTERPRISE: {
    id: "enterprise",
    name: "Enterprise",
    priceUSD: 300,
    quota: 30000,
    model: "gpt-4o-mini-128k",
    description: "Enterprise access – 30,000 calls per month"
  }
};

/** Helper to get plan by id – throws if not found */
export function getPlan(id) {
  const plan = Object.values(PLANS).find(p => p.id === id);
  if (!plan) throw new Error(`Invalid plan id "${id}"`);
  return plan;
}
