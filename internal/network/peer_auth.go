package network

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

// PeerAuth manages authentication of node-to-node connections.
// Each peer is identified by their ED25519 public key.
// Peers must sign handshake messages to prove identity.
type PeerAuth struct {
	mu               sync.RWMutex
	nodeID           string
	privateKey       ed25519.PrivateKey           // Our node's private key
	publicKey        ed25519.PublicKey            // Our node's public key
	trustedPeers     map[string]ed25519.PublicKey // Agent ID -> public key
	peerReputation   map[string]*PeerInfo         // Agent ID -> connection metadata
	requireSignature bool                         // If true, all handshakes must be signed
	replayProtection map[string]int64             // Agent ID -> last handshake timestamp
	replayWindow     int64                        // Seconds allowed between identical handshakes
}

// PeerInfo tracks historical connection metadata for reputation tracking.
type PeerInfo struct {
	AgentID         string
	PublicKeyHex    string
	FirstSeen       time.Time
	LastSeen        time.Time
	ConnectionCount int
	FailedAttempts  int
	Banned          bool
	BanReason       string
}

// HandshakeMessage is sent by peers to identify themselves.
type HandshakeMessage struct {
	AgentID      string `json:"agent_id"`
	PublicKeyHex string `json:"public_key_hex"`
	Timestamp    int64  `json:"timestamp"`
	Signature    string `json:"signature"`    // Signature of "agentID|timestamp"
	ReplayNonce  string `json:"replay_nonce"` // Unique nonce per connection
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
// publicKeyHex may have an optional "0x" prefix, matching every other
// hex-encoded public key convention in this codebase (e.g.
// synthoscrypto.PublicKeyBytes, cfg.PeerKeys) -- this used to reject a
// "0x"-prefixed key with "invalid public key hex" outright, which is
// exactly the format cfg.PeerKeys stores its values in, and the format
// buildValidatorKeySet/Node.AddPeer already expect and strip elsewhere.
func (pa *PeerAuth) RegisterTrustedPeer(agentID string, publicKeyHex string) error {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	publicKeyHex = strings.TrimPrefix(publicKeyHex, "0x")
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
//
// channelBinding, when non-nil, is folded into the signed bytes. It should
// be the TLS connection's own exported keying material (see
// secure_transport.go's channelBinding helper) -- a value both ends of a
// given TLS session derive identically from that session's own secrets,
// and which differs for every distinct TLS session, including two separate
// sessions an active man-in-the-middle sets up while transparently
// relaying traffic between them.
//
// Without this, TLS-with-self-signed-certs-and-no-real-CA (which is what
// this transport uses -- see tls_cert.go) only stops a passive eavesdropper
// from reading traffic; it does nothing against an active attacker who
// terminates TLS separately with each side and relays the plaintext
// between the two legs, since neither leg's certificate is checked against
// a known identity. That attacker can still faithfully relay this
// signature (it's valid, just signed by the real peer, for the real
// peer's session with the attacker) without being able to forge one of
// their own -- but binding the signed bytes to the specific TLS session
// they arrived on means the signature the server actually receives was
// computed over a DIFFERENT channel-binding value (the client's session
// with the attacker) than the one the server computes for its own session
// (with the attacker), so verification fails. A relayed handshake stops
// verifying the moment there are two different TLS sessions involved
// instead of one continuous one, which is exactly the MITM case.
func (pa *PeerAuth) CreateHandshake(nonce string, channelBinding []byte) *HandshakeMessage {
	timestamp := time.Now().Unix()
	messageBytes := handshakeSignedBytes(pa.nodeID, timestamp, channelBinding)
	signature := ed25519.Sign(pa.privateKey, messageBytes)

	return &HandshakeMessage{
		AgentID:      pa.nodeID,
		PublicKeyHex: hex.EncodeToString(pa.publicKey),
		Timestamp:    timestamp,
		Signature:    hex.EncodeToString(signature),
		ReplayNonce:  nonce,
	}
}

// handshakeSignedBytes builds the exact byte sequence CreateHandshake signs
// and VerifyHandshake checks against. channelBinding is appended only when
// non-empty, so a caller that never uses TLS (channelBinding always nil,
// e.g. enableTLS=false, or in unit tests exercising PeerAuth in isolation)
// gets byte-for-byte the same message this function has always signed.
func handshakeSignedBytes(agentID string, timestamp int64, channelBinding []byte) []byte {
	msg := fmt.Sprintf("%s|%d", agentID, timestamp)
	if len(channelBinding) > 0 {
		msg += "|" + hex.EncodeToString(channelBinding)
	}
	return []byte(msg)
}

// VerifyHandshake authenticates an incoming handshake message.
// Returns the authenticated agent ID and nil if successful.
//
// channelBinding must be this verifier's own exported keying material for
// the TLS session the handshake was just read from (nil if TLS is
// disabled), matching what the sender folded into its signature in
// CreateHandshake -- see that function's doc comment for why this matters.
func (pa *PeerAuth) VerifyHandshake(msg *HandshakeMessage, channelBinding []byte) (string, error) {
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

		messageBytes := handshakeSignedBytes(agentID, msg.Timestamp, channelBinding)
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
