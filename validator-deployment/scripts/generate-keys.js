#!/usr/bin/env node

/**
 * Generate keypairs for SYNTHOS validators
 * Creates ed25519 keys using TweetNaCl
 */

const nacl = require('tweetnacl')
const fs = require('fs')
const path = require('path')

console.log('🔑 Generating validator keypairs...\n')

const validators = []

for (let i = 1; i <= 15; i++) {
	// Generate Ed25519 keypair
	const keypair = nacl.sign.keyPair()
	
	const validatorID = `validator-${i}`
	const publicKey = Buffer.from(keypair.publicKey).toString('hex')
	const privateKey = Buffer.from(keypair.secretKey).toString('hex')
	
	validators.push({
		id: validatorID,
		publicKey,
		privateKey,
		createdAt: new Date().toISOString()
	})
	
	console.log(`✓ ${validatorID}`)
	console.log(`  Public:  ${publicKey.substring(0, 32)}...`)
	console.log(`  Private: ${privateKey.substring(0, 32)}...`)
	console.log('')
}

// Save to file
const keysPath = path.join(__dirname, '..', 'validator-keys.json')
fs.writeFileSync(keysPath, JSON.stringify(validators, null, 2))

console.log(`\n✅ Keypairs saved to: validator-keys.json`)
console.log(`\n⚠️  IMPORTANT: Store validator-keys.json securely!`)
console.log(`   Next: Copy the private keys to wrangler.toml environment variables`)
