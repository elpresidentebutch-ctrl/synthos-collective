package serverless

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// ServerlessMessage is a message stored in object storage (S3, R2, etc.).
// Nodes poll object storage to fetch new messages instead of listening on ports.
//
// KEY INSIGHT: Messages are stored with deterministic paths, allowing nodes
// to discover and fetch them without central coordination. This is the "piggyback"
// - we're using existing object storage as shared bulletin boards.
type ServerlessMessage struct {
	ID           string    `json:"id"`              // Unique message ID (base path)
	Type         string    `json:"type"`            // "block", "transaction", "vote", etc.
	Sender       string    `json:"sender"`          // Validator/agent ID (public key hex)
	Height       uint64    `json:"height"`          // Block height for ordering
	Timestamp    int64     `json:"timestamp"`       // Unix timestamp
	Payload      string    `json:"payload"`         // JSON-encoded block/tx data
	Signature    string    `json:"signature"`       // Ed25519 signature of (height|timestamp|payload)
	TTL          int64     `json:"ttl"`             // Expiration timestamp (auto-cleanup)
	CreatedAt    int64     `json:"created_at"`      // When message was created
}

// MessageBucket represents a shared object storage location where nodes
// publish and discover messages. Nodes continuously poll specific paths.
//
// Path structure:
//   /synthos/{network}/{height}/{sender}/{message-id}
//
// This hierarchical structure allows nodes to:
// - Subscribe to specific heights ("give me all messages for height 100")
// - Subscribe to specific senders ("give me all from validator X")
// - Discover other validators through DNS or bootstrap list
type MessageBucket interface {
	// PutMessage stores a message in object storage
	PutMessage(ctx interface{}, msg *ServerlessMessage) error

	// GetMessage retrieves a specific message
	GetMessage(ctx interface{}, id string) (*ServerlessMessage, error)

	// ListMessages lists all messages at a path (with filtering)
	ListMessages(ctx interface{}, path string, limit int) ([]*ServerlessMessage, error)

	// DeleteMessage removes expired messages
	DeleteMessage(ctx interface{}, id string) error
}

// ServerlessValidator is a validator that runs as a cloud function.
//
// Instead of listening on a port, the validator:
// 1. Wakes up on trigger (HTTP, schedule, object storage event)
// 2. Polls object storage for new messages
// 3. Validates and processes them
// 4. Writes response/votes to object storage
// 5. Sleeps (or returns from function)
//
// The entire validator is stateless - any cloud instance can resume work.
type ServerlessValidator struct {
	ValidatorID    string                    // Public key hex (also the discovery identity)
	PrivateKey     ed25519.PrivateKey        // For signing messages
	Bucket         MessageBucket             // Object storage backend
	Network        string                    // Network name (mainnet, testnet, etc.)
	PollInterval   time.Duration             // How often to poll storage
	MessageTTL     int64                     // Seconds before messages expire
	LastPolledAtom map[string]int64          // Last polled height per sender
	blockValidator BlockValidator            // Validates blocks
	mu             sync.RWMutex
}

// BlockValidator validates incoming blocks (from any validator).
type BlockValidator interface {
	ValidateBlock(msg *ServerlessMessage) error
}

// NewServerlessValidator creates a new cloud-function validator.
func NewServerlessValidator(
	validatorID string,
	privateKey ed25519.PrivateKey,
	bucket MessageBucket,
	network string,
	blockValidator BlockValidator,
) *ServerlessValidator {
	return &ServerlessValidator{
		ValidatorID:   validatorID,
		PrivateKey:    privateKey,
		Bucket:        bucket,
		Network:       network,
		PollInterval:  100 * time.Millisecond,
		MessageTTL:    300, // 5 minutes
		LastPolledAtom: make(map[string]int64),
		blockValidator: blockValidator,
	}
}

// PublishMessage writes a message to object storage for other nodes to discover.
func (sv *ServerlessValidator) PublishMessage(
	ctx interface{},
	msgType string,
	height uint64,
	payload interface{},
) error {
	// Serialize payload.
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}
	payloadStr := string(payloadBytes)

	// Create message.
	now := time.Now()
	msg := &ServerlessMessage{
		Type:      msgType,
		Sender:    sv.ValidatorID,
		Height:    height,
		Timestamp: now.Unix(),
		Payload:   payloadStr,
		TTL:       now.Add(time.Duration(sv.MessageTTL) * time.Second).Unix(),
		CreatedAt: now.Unix(),
	}

	// Sign message.
	msgHash := sha256.Sum256([]byte(fmt.Sprintf("%d|%d|%s", msg.Height, msg.Timestamp, msg.Payload)))
	msg.Signature = hex.EncodeToString(ed25519.Sign(sv.PrivateKey, msgHash[:]))

	// Generate ID from height + timestamp + signature prefix.
	msg.ID = fmt.Sprintf("%d-%d-%s", msg.Height, msg.Timestamp, msg.Signature[:16])

	// Store in bucket.
	return sv.Bucket.PutMessage(ctx, msg)
}

// PollForMessages polls object storage for new messages and processes them.
// This is the main validator loop - called by the cloud function handler.
func (sv *ServerlessValidator) PollForMessages(ctx interface{}, maxMessages int) (int, error) {
	sv.mu.Lock()
	lastPolled := sv.LastPolledAtom
	sv.mu.Unlock()

	processed := 0

	// Poll all known validators for new messages.
	// In practice, validator list comes from:
	// 1. DNS TXT records with validator public keys
	// 2. Bootstrap list hardcoded in config
	// 3. Messages from other validators that reference more validators

	// For now, query all messages in the current batch.
	path := fmt.Sprintf("synthos/%s/messages", sv.Network)
	messages, err := sv.Bucket.ListMessages(ctx, path, maxMessages)
	if err != nil {
		return 0, fmt.Errorf("failed to list messages: %w", err)
	}

	for _, msg := range messages {
		// Skip our own messages.
		if msg.Sender == sv.ValidatorID {
			continue
		}

		// Check if already processed.
		if sv.isProcessed(msg.ID) {
			continue
		}

		// Verify sender signature.
		if err := sv.verifySignature(msg); err != nil {
			// Spam/attack - record for reputation
			continue
		}

		// Validate message content.
		if err := sv.blockValidator.ValidateBlock(msg); err != nil {
			continue
		}

		// Process message (apply to local state, vote, etc.).
		// This is application-specific.
		processed++

		// Mark as processed.
		sv.markProcessed(msg.ID)
	}

	// Update last polled.
	sv.mu.Lock()
	for _, msg := range messages {
		key := fmt.Sprintf("%s:%d", msg.Sender, msg.Height)
		if int64(msg.Height) > lastPolled[key] {
			lastPolled[key] = int64(msg.Height)
		}
	}
	sv.mu.Unlock()

	return processed, nil
}

// verifySignature checks the message signature against the sender's public key.
func (sv *ServerlessValidator) verifySignature(msg *ServerlessMessage) error {
	// Reconstruct what was signed.
	msgHash := sha256.Sum256([]byte(fmt.Sprintf("%d|%d|%s", msg.Height, msg.Timestamp, msg.Payload)))

	// Decode sender's public key from their ValidatorID.
	pubKeyBytes, err := hex.DecodeString(msg.Sender)
	if err != nil {
		return fmt.Errorf("invalid sender ID format: %w", err)
	}
	if len(pubKeyBytes) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid public key size")
	}

	// Decode signature.
	sigBytes, err := hex.DecodeString(msg.Signature)
	if err != nil {
		return fmt.Errorf("invalid signature format: %w", err)
	}

	// Verify.
	if !ed25519.Verify(ed25519.PublicKey(pubKeyBytes), msgHash[:], sigBytes) {
		return fmt.Errorf("invalid signature from %s", msg.Sender)
	}

	return nil
}

// isProcessed checks if we've already seen this message.
func (sv *ServerlessValidator) isProcessed(messageID string) bool {
	// In real implementation, this would check a persistent cache
	// (DynamoDB, Redis, or another database).
	// For now, this is a stub.
	return false
}

// markProcessed records that we've processed a message.
func (sv *ServerlessValidator) markProcessed(messageID string) {
	// Stub - would write to persistent cache.
}

// PollAndProcess is the main entry point for cloud function handler.
// Call this from AWS Lambda, Cloudflare Worker, or Google Cloud Function.
func (sv *ServerlessValidator) PollAndProcess(ctx interface{}) (map[string]interface{}, error) {
	start := time.Now()

	// Poll for messages.
	processed, err := sv.PollForMessages(ctx, 100)
	if err != nil {
		return map[string]interface{}{
			"error":     err.Error(),
			"processed": 0,
		}, err
	}

	// Try to propose a block if we're the leader for this height.
	// (This is application-specific consensus logic.)

	return map[string]interface{}{
		"processed":      processed,
		"duration_ms":    time.Since(start).Milliseconds(),
		"validator_id":   sv.ValidatorID[:8],
		"network":        sv.Network,
	}, nil
}
