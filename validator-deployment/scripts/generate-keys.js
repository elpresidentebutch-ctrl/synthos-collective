#!/usr/bin/env node

const nacl = require('tweetnacl')
const fs = require('fs')
const path = require('path')

console.log('Generating validator keypairs locally. Never commit the output.\n')

const validators = []
for (let i = 1; i <= 15; i++) {
	const keypair = nacl.sign.keyPair()
	const validatorID = `validator-${i}`
	validators.push({
		id: validatorID,
		publicKey: Buffer.from(keypair.publicKey).toString('hex'),
		privateKey: Buffer.from(keypair.secretKey).toString('hex'),
		createdAt: new Date().toISOString()
	})
	console.log(`Generated ${validatorID}`)
}

const keysPath = path.join(__dirname, '..', 'validator-keys.json')
fs.writeFileSync(keysPath, JSON.stringify(validators, null, 2), { mode: 0o600 })

console.log(`\nKeypairs saved locally to: ${keysPath}`)
console.log('This ignored file contains secrets. Move it to an encrypted secret store.')
console.log('Provision each key with: wrangler secret put PRIVATE_KEY --env validatorN')
console.log('Never copy private keys into wrangler.toml or logs.')
