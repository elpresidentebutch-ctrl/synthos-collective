# SYNTHOS Validator Deployment Guide

This guide deploys serverless validator workers without committing or printing
private keys. Any validator key that previously appeared in this repository is
compromised and must not be reused.

## Prerequisites

- Node.js and npm
- Cloudflare account
- Wrangler CLI authenticated with Cloudflare
- R2 access for the validator message bucket

## 1. Install Dependencies

```bash
npm install
```

## 2. Generate Fresh Validator Keys

Generate keys only on a trusted machine:

```bash
npm run generate-keys
```

The script writes `validator-keys.json`, which is ignored by Git and contains
secret material. Move private keys into an encrypted password manager, HSM, or
Cloudflare secret flow immediately.

## 3. Provision Secrets

Store each validator private key as a Wrangler secret:

```bash
wrangler secret put PRIVATE_KEY --env validator1
wrangler secret put PRIVATE_KEY --env validator2
```

Repeat for every validator environment. Do not place private keys in
`wrangler.toml`, shell history, logs, screenshots, or committed files.

## 4. Create the R2 Bucket

```bash
wrangler r2 bucket create synthos-validators
```

If the bucket already exists, verify that the account and binding match
`wrangler.toml`.

## 5. Deploy Validators

```bash
npm run deploy
```

The deploy script expects secrets to already be provisioned. It does not
generate, print, or persist private keys.

## 6. Verify Deployment

Check worker health and logs:

```bash
wrangler tail --env validator1
```

Before enabling validator membership, confirm:

- Each worker has a unique public identity in the registry.
- `PRIVATE_KEY` is configured as a secret for every environment.
- No validator can propose unless it uses the canonical Go state machine and
  canonical Ed25519 envelope signing.
- Old validator keys have been removed from active infrastructure.

## Security Notes

- Historical keys in this repository are compromised.
- `validator-keys.json` and `validators.txt` are ignored and should remain local
  only.
- Public keys may be published in an authenticated validator registry.
- Private keys must be rotated through a controlled network upgrade.
