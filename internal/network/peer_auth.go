package network

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// PeerAuth manages authentication of node-to-node connections.
// Each peer is identified by their ED25519 public key.
// Peers must sign handshake messages to prove identity.
type PeerAuth struct {
	mu                sync.RWMutex
	nodeID            string
	privateKey        ed25519.PrivateKey // Our node's private key
	publicKey         ed25519.PublicKey  // Our node's public key
	trustedPeers      map[string]ed25519.PublicKey // Agent ID -> public key
	peerReputation    map[string]*PeerInfo // Agent ID -> connection metadata
	requireSignature  bool // If true, all handshakes must be signed
	replayProtection  map[string]int64 // Agent ID -> last handshake timestamp
	replayWindow      int64 // Seconds allowed between identical handshakes
}

// PeerInfo tracks historical connection metadata for reputation tracking.
type PeerInfo struct {
	AgentID          string
	PublicKeyHex     string
	FirstSeen        time.Time
	LastSeen         time.Time
	ConnectionCount  int
	FailedAttempts   int
	Banned           bool
	BanReason        string
}

// HandshakeMessage is sent by peers to identify themselves.
type HandshakeMessage struct {
	AgentID       string `json:"agent_id"`
	PublicKeyHex  string `json:"public_key_hex"`
	Timestamp     int64  `json:"timestamp"`
	Signature     string `json:"signature"` // Signature of "agentID|timestamp"
	ReplayNonce   string `json:"replay_nonce"` // Unique nonce per connection
}

// NewPeerAuth creates a new peer authentication manager.
func NewPeerAuth(nodeID string, privateKey ed25519.PrivateKey, requireSignature bool) *PeerAuth {
	return &PeerAuth{
		nodeID:           nodeID,
		privateKey:       privateKey,
		publicKey:        privateKey.Public().(ed25519.PublicKey),
		trustedPeers:     make(map[string]ed25519.PublicKey),
		peerReputation:   make(map[string]*PeerInfo),
		requireSignature: requireSignature,
		replayProtection: make(map[string]int64),
		replayWindow:     60, // 60 seconds
	}
}

// RegisterTrustedPeer adds a peer's public key to the trusted list.
func (pa *PeerAuth) RegisterTrustedPeer(agentID string, publicKeyHex string) error {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	pubKeyBytes, err := hex.DecodeString(publicKeyHex)
	if err != nil {
		return fmt.Errorf("invalid public key hex: %w", err)
	}
	if len(pubKeyBytes) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid public key size: %d bytes", len(pubKeyBytes))
	}

	pubKey := ed25519.PublicKey(pubKeyBytes)
	pa.trustedPeers[agentID] = pubKey

	return nil
}

// CreateHandshake creates a signed handshake message from this node.
func (pa *PeerAuth) CreateHandshake(nonce string) *HandshakeMessage {
	timestamp := time.Now().Unix()
	messageBytes := []byte(fmt.Sprintf("%s|%d", pa.nodeID, timestamp))
	signature := ed25519.Sign(pa.privateKey, messageBytes)

	return &HandshakeMessage{
		AgentID:      pa.nodeID,
		PublicKeyHex: hex.EncodeToString(pa.publicKey),
		Timestamp:    timestamp,
		Signature:    hex.EncodeToString(signature),
		ReplayNonce:  nonce,
	}
}

// VerifyHandshake authenticates an incoming handshake message.
// Returns the authenticated agent ID and nil if successful.
func (pa *PeerAuth) VerifyHandshake(msg *HandshakeMessage) (string, error) {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	agentID := msg.AgentID

	// Check if peer is banned.
	if info, exists := pa.peerReputation[agentID]; exists && info.Banned {
		return "", fmt.Errorf("peer %s is banned: %s", agentID, info.BanReason)
	}

	// Check timestamp is recent (prevent old replay attacks).
	now := time.Now().Unix()
	if now-msg.Timestamp > 300 { // 5 minutes max age
		return "", fmt.Errorf("handshake timestamp too old: %d seconds", now-msg.Timestamp)
	}

	// Check replay protection.
	if lastTs, exists := pa.replayProtection[agentID]; exists {
		if now-lastTs < pa.replayWindow && msg.Timestamp == lastTs {
			return "", fmt.Errorf("duplicate handshake from %s detected (replay attack)", agentID)
		}
	}

	// Verify signature if required.
	if pa.requireSignature {
		pubKey, trusted := pa.trustedPeers[agentID]
		if !trusted {
			return "", fmt.Errorf("peer %s is not trusted (unknown public key)", agentID)
		}

		sigBytes, err := hex.DecodeString(msg.Signature)
		if err != nil {
			return "", fmt.Errorf("invalid signature encoding: %w", err)
		}

		messageBytes := []byte(fmt.Sprintf("%s|%d", agentID, msg.Timestamp))
		if !ed25519.Verify(pubKey, messageBytes, sigBytes) {
			pa.recordFailedAttempt(agentID)
			return "", fmt.Errorf("peer %s signature verification failed", agentID)
		}
	}

	// Verify public key consistency.
	if msg.PublicKeyHex != "" {
		if trusted, exists := pa.trustedPeers[agentID]; exists {
			expectedKeyHex := hex.EncodeToString(trusted)
			if msg.PublicKeyHex != expectedKeyHex {
				return "", fmt.Errorf("peer %s public key mismatch (possible key compromise)", agentID)
			}
		}
	}

	// Update peer reputation (successful connection).
	pa.recordSuccessfulConnection(agentID, msg.PublicKeyHex)
	pa.replayProtection[agentID] = msg.Timestamp

	return agentID, nil
}

// BanPeer blocks all future connections from a peer.
func (pa *PeerAuth) BanPeer(agentID string, reason string) {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	if info, exists := pa.peerReputation[agentID]; exists {
		info.Banned = true
		info.BanReason = reason
	} else {
		pa.peerReputation[agentID] = &PeerInfo{
			AgentID:   agentID,
			FirstSeen: time.Now(),
			Banned:    true,
			BanReason: reason,
		}
	}
}

// GetPeerReputation returns a copy of peer reputation data.
func (pa *PeerAuth) GetPeerReputation(agentID string) *PeerInfo {
	pa.mu.RLock()
	defer pa.mu.RUnlock()

	if info, exists := pa.peerReputation[agentID]; exists {
		// Return a copy to prevent external mutation.
		copy := *info
		return &copy
	}
	return nil
}

// recordSuccessfulConnection updates peer reputation after successful auth.
func (pa *PeerAuth) recordSuccessfulConnection(agentID, publicKeyHex string) {
	if info, exists := pa.peerReputation[agentID]; exists {
		info.LastSeen = time.Now()
		info.ConnectionCount++
		// Reset failed attempts on successful connection.
		info.FailedAttempts = 0
	} else {
		pa.peerReputation[agentID] = &PeerInfo{
			AgentID:         agentID,
			PublicKeyHex:    publicKeyHex,
			FirstSeen:       time.Now(),
			LastSeen:        time.Now(),
			ConnectionCount: 1,
			FailedAttempts:  0,
			Banned:          false,
		}
	}
}

// recordFailedAttempt increments failed connection count and auto-bans after threshold.
func (pa *PeerAuth) recordFailedAttempt(agentID string) {
	const failureThreshold = 5

	if info, exists := pa.peerReputation[agentID]; exists {
		info.FailedAttempts++
		if info.FailedAttempts >= failureThreshold {
			info.Banned = true
			info.BanReason = fmt.Sprintf("exceeded failure threshold (%d attempts)", failureThreshold)
		}
	} else {
		info := &PeerInfo{
			AgentID:        agentID,
			FirstSeen:      time.Now(),
			LastSeen:       time.Now(),
			FailedAttempts: 1,
			Banned:         false,
		}
		pa.peerReputation[agentID] = info
	}
}

// GetAuthenticityProof returns a signed message proving this node's identity.
// This can be used in handshakes or other contexts requiring cryptographic proof.
func (pa *PeerAuth) GetAuthenticityProof(data []byte) string {
	hash := sha256.Sum256(data)
	signature := ed25519.Sign(pa.privateKey, hash[:])
	return hex.EncodeToString(signature)
}

// VerifyAuthenticityProof validates a signed message from a trusted peer.
func (pa *PeerAuth) VerifyAuthenticityProof(agentID string, data []byte, proofHex string) error {
	pa.mu.RLock()
	pubKey, trusted := pa.trustedPeers[agentID]
	pa.mu.RUnlock()

	if !trusted {
		return fmt.Errorf("peer %s is not in trusted list", agentID)
	}

	sigBytes, err := hex.DecodeString(proofHex)
	if err != nil {
		return fmt.Errorf("invalid signature format: %w", err)
	}

	hash := sha256.Sum256(data)
	if !ed25519.Verify(pubKey, hash[:], sigBytes) {
		return fmt.Errorf("authenticity proof verification failed for %s", agentID)
	}

	return nil
}

// GetOurPublicKey returns this node's public key in hex format.
func (pa *PeerAuth) GetOurPublicKey() string {
	return hex.EncodeToString(pa.publicKey)
}
