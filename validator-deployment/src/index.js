/**
 * SYNTHOS Validator - Cloudflare Worker
 * 
 * This worker runs a SYNTHOS validator that:
 * 1. Polls R2 for new messages from other validators
 * 2. Validates blocks and transactions
 * 3. Publishes votes/blocks back to R2
 * 4. Participates in decentralized consensus
 * 
 * No listening ports, works from anywhere, permission-less
 */

export default {
	async fetch(request, env, ctx) {
		// HTTP trigger - can be called manually or via HTTP
		const validatorID = env.VALIDATOR_ID || 'unknown'
		
		try {
			const result = await pollAndProcess(env)
			return new Response(JSON.stringify({
				validator: validatorID,
				status: 'success',
				processed: result.processed,
				blocks_seen: result.blocksProcessed,
				timestamp: new Date().toISOString()
			}), {
				headers: { 'Content-Type': 'application/json' }
			})
		} catch (error) {
			return new Response(JSON.stringify({
				validator: validatorID,
				status: 'error',
				error: error.message
			}), {
				status: 500,
				headers: { 'Content-Type': 'application/json' }
			})
		}
	},

	async scheduled(event, env, ctx) {
		// Scheduled trigger - runs every 5 seconds via cron
		const validatorID = env.VALIDATOR_ID || 'unknown'
		
		try {
			const result = await pollAndProcess(env)
			console.log(`[${validatorID}] Poll complete: processed=${result.processed}, blocks=${result.blocksProcessed}`)
		} catch (error) {
			console.error(`[${validatorID}] Error: ${error.message}`)
		}
	}
}

/**
 * Main validator loop: poll R2, validate, vote
 */
async function pollAndProcess(env) {
	const validatorID = env.VALIDATOR_ID
	const bucket = env.VALIDATOR_BUCKET
	const network = env.NETWORK || 'mainnet'
	const privateKey = env.PRIVATE_KEY
	if (!privateKey) {
		throw new Error('PRIVATE_KEY secret is not configured; use wrangler secret put PRIVATE_KEY --env <validator>')
	}

	let processed = 0
	let blocksProcessed = 0

	// Step 1: List all messages in R2 bucket
	const prefix = `synthos/${network}/`
	
	try {
		const listResponse = await bucket.list({ prefix, limit: 100 })
		
		// Step 2: Process each message
		for (const object of listResponse.objects) {
			// Skip our own messages
			if (object.key.includes(validatorID)) {
				continue
			}

			// Download message
			const msgData = await bucket.get(object.key)
			if (!msgData) continue

			const msgText = await msgData.text()
			const message = JSON.parse(msgText)

			// Skip if already processed
			if (await isProcessed(bucket, message.id, validatorID)) {
				continue
			}

			// Validate message signature
			if (!verifySignature(message)) {
				console.log(`[${validatorID}] Signature invalid: ${message.id}`)
				continue
			}

			// Process based on type
			if (message.type === 'block') {
				blocksProcessed++
				processed++
			} else if (message.type === 'vote') {
				processed++
			}

			// Mark as processed
			await markProcessed(bucket, message.id, validatorID)
		}

		// Step 3: Try to propose a block if we're eligible
		const blockHeight = await getCurrentBlockHeight(bucket)
		const canPropose = await checkProposerRights(validatorID, blockHeight)

		if (canPropose) {
			const block = createBlock(validatorID, blockHeight + 1, privateKey)
			await publishMessage(bucket, block, privateKey, network)
			processed++
		}

		return { processed, blocksProcessed }

	} catch (error) {
		throw new Error(`Poll failed: ${error.message}`)
	}
}

/**
 * Create a new block proposal
 */
function createBlock(validatorID, blockHeight, privateKey) {
	throw new Error(
		`validator ${validatorID} cannot propose height ${blockHeight}: ` +
		'serverless proposal construction is disabled until it executes the canonical Go state machine'
	)
}

/**
 * Publish a message to R2
 */
async function publishMessage(bucket, message, privateKey, network) {
	const path = `synthos/${network}/${message.height}/${message.validator}/${message.id}.json`
	const msgData = JSON.stringify(message, null, 2)
	
	await bucket.put(path, msgData, {
		httpMetadata: {
			contentType: 'application/json'
		}
	})
}

/**
 * Get current block height from R2
 */
async function getCurrentBlockHeight(bucket) {
	try {
		const meta = await bucket.get('meta/current_height.json')
		if (!meta) return 0
		
		const data = JSON.parse(await meta.text())
		return data.height || 0
	} catch {
		return 0
	}
}

/**
 * Check if this validator can propose next block (simple round-robin)
 */
async function checkProposerRights(validatorID, blockHeight) {
	const validatorNum = parseInt(validatorID.split('-')[1] || '0')
	const expectedProposer = (blockHeight % 15) + 1
	
	return validatorNum === expectedProposer
}

/**
 * Check if we already processed this message
 */
async function isProcessed(bucket, messageID, validatorID) {
	try {
		const processed = await bucket.get(`processed/${validatorID}/${messageID}.json`)
		return processed !== null
	} catch {
		return false
	}
}

/**
 * Mark a message as processed
 */
async function markProcessed(bucket, messageID, validatorID) {
	const path = `processed/${validatorID}/${messageID}.json`
	const data = JSON.stringify({
		messageID,
		validator: validatorID,
		processedAt: new Date().toISOString()
	})
	
	await bucket.put(path, data)
}

/**
 * Simple message signing (would use ed25519 in production)
 */
function signMessage(message, privateKey) {
	throw new Error('stub signing disabled; use canonical Ed25519 envelope signing')
}

/**
 * Verify message signature
 */
function verifySignature(message) {
	// Fail closed. A non-empty string is not cryptographic authentication.
	return false
}
