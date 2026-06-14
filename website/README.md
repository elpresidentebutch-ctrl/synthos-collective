# SYNTHOS Website Console

This folder is the replacement public website surface for SYNTHOS Collective.

It is a static site that connects directly to the live Cloudflare Worker
validator endpoints listed in `assets/app.js`.

## Local Preview

From the repository root:

```powershell
node website/server.mjs
```

Open:

```text
http://127.0.0.1:4177
```

## Deploy

Deploy the `website/` folder to Cloudflare Pages or any static host.

Suggested Cloudflare Pages settings:

- Build command: none
- Build output directory: `website`
- Root directory: repository root

## Current Live Truth

The site reports the validator network honestly:

- reachable validators
- current height
- tip hash
- state root
- next proposer
- heartbeat freshness

If validators are reachable but heartbeats are stale, the site says so instead
of claiming the network is fully healthy.
