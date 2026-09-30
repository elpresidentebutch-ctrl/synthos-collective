# Wire the live SYNTHOS backend into the B12 website

The live site at `ishamwilliamsblockchains.com` is hosted by B12. The repository cannot directly edit B12 pages, so the production integration is intentionally reduced to one custom-code paste.

## B12 custom code

Paste this immediately before the closing `</body>` tag in B12's site-wide custom code area:

```html
<script>
  window.SYNTHOS_API_URL = "https://synthos-site-backend.jamesishamwilliams.workers.dev";
</script>
<script defer src="https://cdn.jsdelivr.net/gh/elpresidentebutch-ctrl/synthos-collective@codex/wire-b12-live-backend/website/b12-live-widget.js"></script>
```

Publish the B12 site. A live network panel will appear in the lower-right corner on every page and refresh every 15 seconds.

The widget is self-contained. It does not require rebuilding B12 sections or adding custom `data-*` attributes.

## What it displays

- reachable validators;
- chain height;
- fresh heartbeat count;
- chain ID;
- finalized tip and state root;
- next proposer;
- truthful degraded/backend-unavailable states.

## Production pin after merge

After this branch is merged, replace the script URL with:

```html
<script defer src="https://cdn.jsdelivr.net/gh/elpresidentebutch-ctrl/synthos-collective@main/website/b12-live-widget.js"></script>
```

Pinning to a commit SHA is even safer when the design is considered stable.

## Important wording

Until a public validator network is independently verified, the website should say `testnet`, not `MAINNET LIVE`. The widget deliberately identifies the network as a testnet and reports backend failures instead of displaying invented metrics.
