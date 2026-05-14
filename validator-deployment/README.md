# 15 Cloudflare Worker Immune Nodes - Quick Commands

## Activate

```bash
# Install dependencies and generate keys
npm install
npm run generate-keys

# Activate all 15 immune nodes to Cloudflare
npm run deploy
```

## Monitor

```bash
# Watch immune node-1 logs in real-time
npm run logs:validator1

# Or use watch command (requires 'watch' package)
npm run monitor
```

## Test

```bash
# Simulate consensus locally
npm run test

# Trigger immune node API manually
curl https://synthos-validator-1.workers.dev/

# Check R2 bucket for messages
wrangler r2 object list synthos-validators
```

## View Deployed Workers

1. **Dashboard:** https://dash.cloudflare.com → Workers
2. **View logs:** Click any worker → Logs
3. **View metrics:** Click worker → Analytics

## Immune Node IDs

Each validator runs at:
- `https://synthos-validator-1.workers.dev/`
- `https://synthos-validator-2.workers.dev/`
- ...
- `https://synthos-validator-15.workers.dev/`

## Current Status

- **Immune Nodes:** 15
- **Storage:** Cloudflare R2
- **Network:** Mainnet
- **Cost:** $0/month (free tier)
- **Status:** Ready to deploy ✅

## Next Steps

1. Run: `npm install && npm run deploy`
2. Check: `npm run logs:validator1`
3. Monitor consensus in real-time
4. Scale to 100+ immune nodes (still free!)

---

Built with SYNTHOS Collective ⚡
