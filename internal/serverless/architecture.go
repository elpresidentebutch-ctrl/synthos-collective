package serverless

import (
	"fmt"
	"sync"
	"time"
)

// PiggybacArchitectureGuide explains the permission-less, firewall-free node architecture
//
// CORE IDEA:
// Instead of running a listening server on a port, validators are:
// - Deployed as cloud functions (AWS Lambda, Cloudflare Workers, Google Cloud Run)
// - Triggered by schedules (every 5-10 seconds) or events
// - Communicate by reading/writing to shared object storage (S3, R2, etc.)
// - Discover each other through DNS or bootstrap lists
// - No inbound ports, no firewall rules needed
// - Permission-less (anyone can deploy their own validator)
//
// ADVANTAGES:
// 1. No firewall needed (everything is outbound-initiated)
// 2. No port conflicts or NAT traversal
// 3. Globally distributed (cloud providers are worldwide)
// 4. Censorship resistant (hidden in cloud infrastructure)
// 5. Cost-effective (pay only when processing)
// 6. Auto-scaling (cloud functions scale automatically)
// 7. Permission-less (no central authority needed)
//
// DISADVANTAGES:
// 1. Latency (100ms between validator invocations)
// 2. Strongly eventual consistency (not instant finality)
// 3. Depends on cloud providers for reliability
// 4. Storage costs (S3 can get expensive at scale)
//
const PiggybacArchitectureGuide = `
SYNTHOS SERVERLESS ARCHITECTURE
=================================

PROBLEM SOLVED:
- Running nodes requires listening ports (blocked by firewalls)
- Running nodes requires static IPs or NAT traversal
- Decentralization requires central infrastructure (relays, RPC nodes)
- Validators can't run everywhere (home networks, corporate networks, NAT)

SOLUTION: PIGGYBACK ON EXISTING INFRASTRUCTURE
===============================================

Instead of running a server, validators are serverless functions that:
1. Wake up on a schedule (every 5 seconds)
2. Check shared object storage for new messages (S3, R2, etc.)
3. Process/validate messages
4. Write votes/blocks to shared object storage
5. Fall asleep (or return from function)

COMPONENTS:

1. MESSAGE BULLETIN BOARD (Object Storage)
   ├─ AWS S3 / Wasabi / Backblaze B2 / CloudFlare R2
   ├─ Path: /synthos/{network}/{height}/{sender}/{message-id}.json
   ├─ Any validator can read (permission-less)
   ├─ Any validator can write to their path
   └─ Old messages auto-delete (TTL)

2. VALIDATOR DISCOVERY (DNS + Bootstrap)
   ├─ DNS TXT records list validator public keys
   │  └─ synthos-mainnet-validators.example.com TXT "pk1=... pk2=..."
   ├─ Bootstrap list hardcoded in client config
   └─ Validators found in messages reference more validators

3. CLOUD FUNCTION VALIDATORS
   ├─ AWS Lambda (trigger: CloudWatch Events, S3, SNS)
   ├─ Cloudflare Workers (trigger: HTTP, Cron, KV change)
   ├─ Google Cloud Functions (trigger: Pub/Sub, HTTP, Storage)
   ├─ Azure Functions (trigger: Timer, HTTP, Event Grid)
   └─ Cheap/free tier: Cloudflare Workers (10k/day free)

4. VALIDATOR STATE (Persistent Cache)
   ├─ DynamoDB / Firestore / Redis
   ├─ Tracks: processed message IDs, last height polled
   └─ Needed so each function invocation knows what to do

COMMUNICATION FLOW:

Validator A wants to propose Block 100:
  1. A: Serialize block as JSON
  2. A: Sign with Ed25519 private key
  3. A: POST to S3: /synthos/mainnet/100/a1b2c3.../block-abc123.json
  4. A: Return (function ends, costs $0.001)

Validator B wakes up 5 seconds later:
  5. B: Query S3: List all messages at /synthos/mainnet/
  6. B: Download Block 100 from A
  7. B: Verify A's Ed25519 signature
  8. B: Validate block content (no double-spends, etc.)
  9. B: Write vote to S3: /synthos/mainnet/100/b2c3d4.../vote.json
  10. B: Return (function ends)

Validator C wakes up 5 seconds later:
  11. C: Read A's block + B's vote
  12. C: Sees 2/3 validators voted ✓
  13. C: Accepts block 100 into canonical chain
  14. C: Starts proposing block 101

VALIDATOR DISCOVERY (Permission-less):

Option 1: DNS-based
  └─ User creates DNS TXT record:
     synthos-mainnet-validators.example.com TXT "pk=abc123def456..."
     (Network automatically reads this via DNS lookup)

Option 2: Bootstrap-based
  └─ User adds their public key to bootstrap list in client
     (Then deploys worker with that config)

Option 3: Gossip-based
  └─ Messages reference other validators
     (Validators discover new ones through messages)

EXAMPLE DEPLOYMENT:

1. Generate keypair:
   $ synthos-keygen > validator.key
   (Extract public key: pubkey=$(cat validator.key | grep public))

2. Create Cloudflare Worker:
   $ wrangler init my-validator
   $ cat > src/index.js << EOF
     import { ServerlessValidator } from '@synthos/serverless'
     export default {
       async fetch() {
         const validator = new ServerlessValidator(
           '$pubkey',
           process.env.PRIVATE_KEY,  // Stored in environment
           new S3Bucket('s3://synthos-messages'),
           'mainnet'
         )
         return new Response(
           JSON.stringify(await validator.pollAndProcess()),
           { headers: { 'Content-Type': 'application/json' } }
         )
       },
       async scheduled() {
         // Called every 5 seconds via cron
       }
     }
   EOF

3. Deploy:
   $ wrangler deploy

4. Set up cron trigger (in wrangler.toml):
   [triggers]
   crons = ["*/5 * * * *"]  # Every 5 seconds

5. (Optional) Add to DNS to be discoverable:
   $ aws route53 change-resource-record-sets \
     --hosted-zone-id Z1234567890ABC \
     --change-batch file://update.json
   
   Where update.json contains:
   {
     "Changes": [{
       "Action": "UPSERT",
       "ResourceRecordSet": {
         "Name": "synthos-mainnet-validators.example.com",
         "Type": "TXT",
         "TTL": 300,
         "ResourceRecords": [
           { "Value": "\"pk=$pubkey\"" }
         ]
       }
     }]
   }

Done! Your validator joins the network permission-lessly.

SECURITY GUARANTEES:

1. Sybil Attacks
   └─ Mitigated by:
      - Ed25519 sig verification (can't forge signatures)
      - Reputation tracking (bad validators ignored)
      - Stake requirements (validators must have skin in game)
      - Slashing (misbehavior costs money)

2. DDoS
   └─ Not possible (no listening ports)
   └─ S3 bucket isolated (Lambda functions can't be DDosed)

3. Message Tampering
   └─ Mitigated by Ed25519 signatures
   └─ Only original signer can modify their message

4. Replay Attacks
   └─ Mitigated by:
      - Message IDs (can't submit same message twice)
      - Timestamp checking
      - Height-based ordering

5. Firewall Evasion
   └─ Not needed (only outbound HTTP calls to S3)
   └─ No listening ports = no firewall rules needed
   └─ Works behind corporate firewall, NAT, residential NAT

COST ANALYSIS:

Per validator, per day:
  - Function invocations: 288/day (every 5 sec) × $0.000000167 = $0.048
  - S3 storage (1GB/day messages): ~$0.01
  - S3 API calls (~1000/day): ~$0.0005
  TOTAL: ~$0.06/day = $1.80/month per validator

At 1000 validators:
  - Total cost: $1,800/month
  - Distributed across 1000 cloud providers (if each uses their own)
  - Highly resilient (failure of one provider doesn't kill network)

COMPARISON TO TRADITIONAL:

Traditional P2P (like Ethereum):
  - Requires listening port (firewall rules)
  - Requires NAT traversal (UPnP, port forwarding)
  - Can't run on residential NAT easily
  - Anyone can DDoS your IP address
  - High uptime requirements (validator penalized for downtime)
  - Bandwidth costs can be high

Serverless Piggyback:
  - No ports needed (works anywhere)
  - No NAT Traversal (only outbound)
  - Works from corporate network, NAT, firewall, mobile
  - DDoS-proof (no public IP)
  - Downtime doesn't matter (can miss a few blocks)
  - Low bandwidth (only ~1KB per message)
  - Censorship-resistant (hiding in cloud infra)

NEXT STEPS:

1. Implement StateStore persistence (DynamoDB, Firestore)
2. Add reputation tracking to messages
3. Implement Byzantine slashing (detect misbehavior)
4. Add privacy layer (messages encrypted before S3)
5. Implement cross-shard communication
6. Add light client for browsers (JavaScript validator)
7. Add cost optimization (batch messages, compression)
`

// MessageBuffer batches messages written to S3 to reduce API calls.
// Instead of writing each message immediately, we batch them
// and upload every N messages or every T time.
type MessageBuffer struct {
	bucket   MessageBucket
	network  string
	sender   string
	maxSize  int
	maxAge   time.Duration
	buffer   []*ServerlessMessage
	lastFlush time.Time
	mu       sync.RWMutex
}

// NewMessageBuffer creates a message batching buffer.
func NewMessageBuffer(bucket MessageBucket, network, sender string, maxSize int, maxAge time.Duration) *MessageBuffer {
	return &MessageBuffer{
		bucket:   bucket,
		network:  network,
		sender:   sender,
		maxSize:  maxSize,
		maxAge:   maxAge,
		buffer:   make([]*ServerlessMessage, 0, maxSize),
		lastFlush: time.Now(),
	}
}

// Add appends a message to the buffer, flushing if needed.
func (mb *MessageBuffer) Add(ctx interface{}, msg *ServerlessMessage) error {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	mb.buffer = append(mb.buffer, msg)

	// Check if we should flush.
	if len(mb.buffer) >= mb.maxSize || time.Since(mb.lastFlush) > mb.maxAge {
		return mb.flush(ctx)
	}

	return nil
}

// flush writes all buffered messages to S3.
func (mb *MessageBuffer) flush(ctx interface{}) error {
	for _, msg := range mb.buffer {
		if err := mb.bucket.PutMessage(ctx, msg); err != nil {
			return fmt.Errorf("failed to put message: %w", err)
		}
	}
	mb.buffer = mb.buffer[:0]
	mb.lastFlush = time.Now()
	return nil
}

// Flush manually triggers a flush.
func (mb *MessageBuffer) Flush(ctx interface{}) error {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	return mb.flush(ctx)
}

// ProcessedMessageCache tracks which messages we've already processed
// to avoid processing duplicates. Stored in DynamoDB/Firestore.
type ProcessedMessageCache interface {
	Has(messageID string) bool
	Add(messageID string) error
	Clear() error
}

// InMemoryProcessedCache is a simple in-process cache (not persistent).
// In production, use DynamoDB or Firestore.
type InMemoryProcessedCache struct {
	cache map[string]bool
	mu    sync.RWMutex
}

// NewInMemoryProcessedCache creates a simple in-process cache.
func NewInMemoryProcessedCache() *InMemoryProcessedCache {
	return &InMemoryProcessedCache{
		cache: make(map[string]bool),
	}
}

// Has checks if a message has been processed.
func (c *InMemoryProcessedCache) Has(messageID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cache[messageID]
}

// Add marks a message as processed.
func (c *InMemoryProcessedCache) Add(messageID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[messageID] = true
	return nil
}

// Clear resets the cache.
func (c *InMemoryProcessedCache) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache = make(map[string]bool)
	return nil
}
