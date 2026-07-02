# B12 Website Backend Integration

You can keep the existing B12 website and give it a real backend by deploying
`workers/site-backend`.

## Backend Endpoints

Deploy the Worker, then point the B12 site at it:

```text
https://synthos-site-backend.jamesishamwilliams.workers.dev
```

Core endpoints:

- `GET /api/health`
- `GET /api/config`
- `GET /api/network/status`
- `GET /api/validators`
- `GET /api/validators/:name`
- `GET /api/blocks?from=0`
- `GET /api/dex/pools`
- `GET /api/node/windows-installer.ps1`
- `POST /api/contact`

## Deploy

### Recommended: Cloudflare dashboard

If the local CLI is blocked by npm, certificates, or API token scope, deploy the
backend directly from Cloudflare:

1. Open Cloudflare Workers & Pages.
2. Create a Worker named `synthos-site-backend`.
3. Open Edit Code.
4. Paste the full contents of `workers/site-backend/src/index.js`.
5. Save and deploy.

See `workers/site-backend/DASHBOARD_DEPLOY.md` for the exact verification URLs.

### CLI path

```powershell
cd workers/site-backend
Copy-Item wrangler.toml.example wrangler.toml
npm install
npm run deploy
```

Optional Worker vars:

- `CONTACT_WEBHOOK_URL`: where B12 contact form messages should be forwarded.
- `VALIDATORS_JSON`: override the validator list without editing source.
- `RELAY_URLS`: relay registry URL used by node installer responses.

## Paste Into B12 Custom Code

Add this before the closing `</body>` tag:

```html
<script>
  window.SYNTHOS_API_URL = "https://synthos-site-backend.jamesishamwilliams.workers.dev";
</script>
<script src="https://synthos-site-backend.jamesishamwilliams.workers.dev/snippet.js"></script>
```

If you do not host `snippet.js` from the Worker yet, paste the contents of
`website/b12-backend-snippet.js` directly into B12's custom code area.

## B12 Elements To Add

Add text elements with these custom attributes:

```html
data-synthos="network-verdict"
data-synthos="validators-reachable"
data-synthos="chain-height"
data-synthos="tip"
data-synthos="state-root"
data-synthos="next-proposer"
data-synthos="fresh-heartbeats"
data-synthos="validator-list"
data-synthos="windows-installer"
```

The backend intentionally reports truthfully. If validators are reachable but
heartbeats are stale, the website will say the network is converged but needs
heartbeat repair.
