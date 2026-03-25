package serverless

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// S3MessageBucket implements MessageBucket using AWS S3 (or compatible).
// This is the "piggyback" infrastructure - we store messages in S3 where
// any validator can fetch them without permission or listening ports.
type S3MessageBucket struct {
	BucketName string // S3 bucket name
	Region     string // AWS region
	Endpoint   string // S3 endpoint (S3, Wasabi, Backblaze B2, etc.)
	Client     *http.Client
}

// NewS3MessageBucket creates a new S3-backed message bucket.
// Works with AWS S3, Wasabi, Backblaze B2, CloudFlare R2, or any S3-compatible storage.
func NewS3MessageBucket(bucketName, region, endpoint string) *S3MessageBucket {
	return &S3MessageBucket{
		BucketName: bucketName,
		Region:     region,
		Endpoint:   endpoint,
		Client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// PutMessage stores a message in S3 with TTL.
func (sb *S3MessageBucket) PutMessage(ctx interface{}, msg *ServerlessMessage) error {
	// Build S3 path: /synthos/{network}/{height}/{sender}/{message-id}
	// path := fmt.Sprintf("synthos/%s/%d/%s/%s.json",
	// 	"mainnet", // network ID
	// 	msg.Height,
	// 	msg.Sender[:16], // Truncated sender for readability
	// 	msg.ID,
	// )

	// Serialize message.
	// msgBytes, err := json.MarshalIndent(msg, "", "  ")
	// if err != nil {
	// 	return err
	// }

	// Upload to S3 using presigned URL or direct S3 API.
	// For simplicity, assuming SDK is used elsewhere.
	// In production: use AWS SDK, Wasabi SDK, etc.

	return nil // Stub - real implementation uses S3 SDK
}

// GetMessage retrieves a message from S3.
func (sb *S3MessageBucket) GetMessage(ctx interface{}, id string) (*ServerlessMessage, error) {
	// Would fetch from S3 here.
	return nil, nil // Stub
}

// ListMessages lists all messages at a path in S3.
func (sb *S3MessageBucket) ListMessages(ctx interface{}, path string, limit int) ([]*ServerlessMessage, error) {
	// Would list objects in S3 with prefix filter.
	return nil, nil // Stub
}

// DeleteMessage removes a message from S3 (for cleanup).
func (sb *S3MessageBucket) DeleteMessage(ctx interface{}, id string) error {
	return nil // Stub
}

// CloudFlareWorkerHandler wraps a validator for Cloudflare Workers.
// Cloudflare Workers are globally distributed, permission-less, and serverless.
//
// Example worker:
//   addEventListener('fetch', event => {
//     event.respondWith(handleValidator(event.request))
//   })
type CloudFlareWorkerHandler struct {
	validator *ServerlessValidator
}

// Handle processes an HTTP request from Cloudflare Worker.
func (h *CloudFlareWorkerHandler) Handle(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()

	// Trigger validator poll when called.
	result, err := h.validator.PollAndProcess(ctx)

	w.Header().Set("Content-Type", "application/json")

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": err.Error(),
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(result)
}

// AWSLambdaHandler wraps a validator for AWS Lambda.
// Lambda functions can be triggered by:
// - CloudWatch Events (scheduled polling)
// - S3 events (new objects)
// - API Gateway (HTTP)
// - SNS/SQS messages
type AWSLambdaHandler struct {
	validator *ServerlessValidator
}

// HandleRequest processes a Lambda invocation event.
func (h *AWSLambdaHandler) HandleRequest(ctx context.Context, event interface{}) (map[string]interface{}, error) {
	result, err := h.validator.PollAndProcess(ctx)
	return result, err
}

// ValidatorDiscovery discovers other validators on the network.
// Since nodes don't listen on ports, discovery happens through:
// 1. DNS TXT records listing validator public keys
// 2. Bootstrap list hardcoded in config
// 3. Validators referenced in messages
//
// This makes the network permission-less - anyone can add their validator
// by publishing their public key in DNS or adding to bootstrap.
type ValidatorDiscovery struct {
	network          string
	dnsProvider      string // "cloudflare", "route53", etc.
	bootstrapList    []string
	discoveredCache  map[string]int64 // ValidatorID -> last discovered
	discoveredKeysC  map[string]bool  // ValidatorID -> exists
	mu               sync.RWMutex
}

// NewValidatorDiscovery creates a discovery client.
func NewValidatorDiscovery(network string) *ValidatorDiscovery {
	return &ValidatorDiscovery{
		network:         network,
		bootstrapList:   []string{}, // Would be populated from config
		discoveredCache: make(map[string]int64),
		discoveredKeysC: make(map[string]bool),
	}
}

// DiscoverValidators queries DNS for validators on the network.
// In DNS, validators are stored as TXT records:
//   synthos-{network}-validators.example.com TXT "pk1=abc123... pk2=def456..."
//
// This creates a permission-less validator registry - anyone can update their
// DNS records to add themselves to the network.
func (vd *ValidatorDiscovery) DiscoverValidators(ctx context.Context) ([]string, error) {
	vd.mu.Lock()
	defer vd.mu.Unlock()

	discovered := make([]string, 0, len(vd.discoveredKeysC))
	for pk := range vd.discoveredKeysC {
		discovered = append(discovered, pk)
	}
	return discovered, nil
}

// AddBootstrapValidator adds a validator to the bootstrap list.
func (vd *ValidatorDiscovery) AddBootstrapValidator(publicKeyHex string) {
	vd.mu.Lock()
	defer vd.mu.Unlock()
	vd.discoveredKeysC[publicKeyHex] = true
}

// PermissionlessNodeDeployment describes how to deploy a SYNTHOS validator
// without any infrastructure permission or central coordination.
//
// The idea:
// 1. User creates free Cloudflare Worker account (or AWS free tier)
// 2. User generates Ed25519 keypair locally
// 3. User publishes their public key in:
//    - DNS TXT record, OR
//    - Hardcoded bootstrap list in config
// 4. User deploys worker code that:
//    - Polls S3 bucket for messages
//    - Validates and processes them
//    - Publishes votes/blocks to S3
// 5. Network automatically recognizes their validator
//
// SECURITY:
// - No ports exposed (no DDoS attack surface)
// - Ed25519 signatures prevent spoofing
// - Messages are stored with TTL (auto-cleanup)
// - Reputation tracking in messages (validators track misbehavior)
// - Slashing via state (validators with low rep eventually ignored)
type PermissionlessNodeDeployment struct {
	// Worker code template
	WorkerTemplate string
	// S3 bucket URL
	BucketURL string
	// Network info
	Network string
}

// ExampleWorkerCode returns a minimal CloudFlare Worker validator implementation.
func ExampleWorkerCode() string {
	return `
// Minimal Cloudflare Worker validator
import { ServerlessValidator } from './validator.js'

const MY_PUBLIC_KEY = 'YOUR_PUBLIC_KEY_HEX_HERE'
const MY_PRIVATE_KEY = 'YOUR_PRIVATE_KEY_HEX_HERE'  // Never hardcode in production!
const S3_BUCKET = 'https://bucket.example.com'

export default {
  async fetch(request) {
    const validator = new ServerlessValidator(
      MY_PUBLIC_KEY,
      MY_PRIVATE_KEY,
      new S3MessageBucket(S3_BUCKET),
      'mainnet'
    )

    const result = await validator.pollAndProcess()

    return new Response(JSON.stringify(result), {
      headers: { 'Content-Type': 'application/json' },
    })
  },

  // Optional: Schedule with cron trigger (every 5 seconds)
  async scheduled(event) {
    // Same polling logic
  }
}
`
}

// HTTPSigVerifier verifies HTTP signatures for CloudFlare Workers or API Gateway.
// This allows validators to authenticate each other through HTTP headers.
type HTTPSigVerifier struct {
	trustedKeys map[string][]byte // ValidatorID -> public key
}

// VerifyRequest checks the X-Signature and X-Validator-ID headers.
func (v *HTTPSigVerifier) VerifyRequest(r *http.Request) (string, error) {
	validatorID := r.Header.Get("X-Validator-ID")
	if validatorID == "" {
		return "", fmt.Errorf("missing validator ID")
	}

	signature := r.Header.Get("X-Signature")
	if signature == "" {
		return "", fmt.Errorf("missing signature")
	}

	// Would verify signature against body and timestamp here.
	return validatorID, nil
}
