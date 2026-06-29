# Validator Key Rotation

All validator keys previously committed to this repository are compromised.

Before any deployment:

1. Disable or remove every deployed validator using an exposed key.
2. Generate a new Ed25519 keypair for each validator on a trusted machine.
3. Store each private key as a Cloudflare secret:

   `wrangler secret put PRIVATE_KEY --env validator1`

4. Publish only public keys in the validator registry.
5. Replace the validator set through an authenticated network upgrade.
6. Purge exposed material from Git history and invalidate old clones/artifacts.

The serverless validator currently fails closed because its old signing and
state-transition code was a simulation stub. Do not re-enable proposals until
the worker executes the same deterministic state transition and canonical
Ed25519 envelope format as the Go implementation.
