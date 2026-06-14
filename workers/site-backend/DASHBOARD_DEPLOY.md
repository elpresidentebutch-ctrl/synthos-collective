# Deploy from the Cloudflare dashboard

This is the simplest deploy path when local Wrangler, npm, or API token scopes
get in the way. It does not require a Cloudflare API token.

## Create the Worker

1. Open the Cloudflare dashboard.
2. Go to Workers & Pages.
3. Select Create.
4. Choose Worker.
5. Name it `synthos-site-backend`.
6. Create the Worker.

## Paste the backend

1. Open the new Worker.
2. Select Edit Code.
3. Delete the starter code.
4. Paste the full contents of `src/index.js`.
5. Save and deploy.

The expected Worker URL is:

```text
https://synthos-site-backend.jamesishamwilliams.workers.dev
```

## Add environment variables

The source has safe defaults for the current SYNTHOS validators, so these are
optional at first:

```text
CHAIN_ID=synthos-l1-devnet
RELAY_URLS=https://synthos-peer-registry.jamesishamwilliams.workers.dev
```

Add `CONTACT_WEBHOOK_URL` later if the B12 contact form should forward messages
to email, CRM, or another webhook target.

## Verify

After saving, open these URLs:

```text
https://synthos-site-backend.jamesishamwilliams.workers.dev/api/health
https://synthos-site-backend.jamesishamwilliams.workers.dev/api/network/status
https://synthos-site-backend.jamesishamwilliams.workers.dev/snippet.js
```

## B12 embed

Add this before the closing `</body>` tag in B12 custom code:

```html
<script>
  window.SYNTHOS_API_URL = "https://synthos-site-backend.jamesishamwilliams.workers.dev";
</script>
<script src="https://synthos-site-backend.jamesishamwilliams.workers.dev/snippet.js"></script>
```
