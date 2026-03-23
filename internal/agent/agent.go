package agent

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	synthoscrypto "synthos-collective/internal/crypto"
	"synthos-collective/internal/network"
)

// Role represents the built-in roles every agent performs.
type Role string

const (
	RoleValidator       Role = "validator"
	RoleGovernor        Role = "governor"
	RoleEconomist       Role = "economist"
	RoleCommunicator    Role = "communicator"
	RoleRegistryKeeper  Role = "registry_keeper"
	RoleSecurityWatcher Role = "security_watcher"
	RoleSimulator       Role = "simulator"
)

// MessageType represents high-level P2P message types.
type MessageType string

const (
	MessagePeerDiscovery    MessageType = "peer_discovery"
	MessagePeerAnnouncement MessageType = "peer_announcement"
	MessageTransaction      MessageType = "transaction"
	MessageBlockProposal    MessageType = "block_proposal"
	MessageBlockVote        MessageType = "block_vote"
	MessageConsensusRound   MessageType = "consensus_round"
	MessageStateSync        MessageType = "state_sync"
	MessageGovernanceProp   MessageType = "governance_proposal"
	MessageGovernanceVote   MessageType = "governance_vote"
)

// Identity holds an agent's unique identity.
type Identity struct {
	AgentID                    string    `json:"agent_id"`
	PublicKey                  string    `json:"public_key"`
	PrivateKeyHash             string    `json:"private_key_hash"`
	HardwareID                 string    `json:"hardware_id"`
	ProofOfComputationRoot     string    `json:"proof_of_computation_root"`
	CreatedAt                  time.Time `json:"created_at"`
	Stake                      int       `json:"stake"`
	Reputation                 int       `json:"reputation"`
	ConsensusRoundsParticipated int      `json:"consensus_rounds_participated"`
	BlocksValidated            int       `json:"blocks_validated"`
	ProposalsVoted             int       `json:"proposals_voted"`
}

// ProofOfComputation is a per-agent, hardware-bound record of work.
type ProofOfComputation struct {
	// HardwareID is a stable identifier from the host (TPM, CPU serial,
	// secure enclave identifier, etc.). In real deployments this should be
	// provided by a trusted hardware binding mechanism.
	HardwareID string `json:"hardware_id"`
	// Nonce is a random value ensuring uniqueness for each computation proof.
	Nonce string `json:"nonce"`
	// Payload describes what was actually computed (e.g. "validate_block",
	// "simulate_scenario", etc.) and can embed arbitrary structured data.
	Payload map[string]any `json:"payload"`
	// Timestamp is when this proof was created.
	Timestamp time.Time `json:"timestamp"`
	// Hash is a SHA-256 hash over (hardware_id, nonce, payload, timestamp).
	Hash string `json:"hash"`
}

// Agent is the core SYNTHOS agent instance in Go.
// Each instance is meant to be a full, independent agent with all roles
// built in and a local proof-of-computation ledger bound to its hardware ID.
type Agent struct {
	Identity  Identity
	NetworkID string

	// Simple, in-memory ledger of computation proofs for this agent instance.
	ProofLog []ProofOfComputation

	keys        synthoscrypto.KeyPair
	transport   network.Transport
	replayCache *network.ReplayCache
}

// NewAgent constructs a new Agent with the given IDs and stake.
// hardwareID must be provided by the caller from a trusted source.
func NewAgent(agentID, publicKey, privateKeyHash, hardwareID string, initialStake int) *Agent {
	now := time.Now().UTC()

	id := Identity{
		AgentID:        agentID,
		PublicKey:      publicKey,
		PrivateKeyHash: privateKeyHash,
		HardwareID:     hardwareID,
		CreatedAt:      now,
		Stake:          initialStake,
		Reputation:     100,
	}

	a := &Agent{
		Identity:  id,
		NetworkID: "synthos_mainnet",
		ProofLog:  make([]ProofOfComputation, 0, 64),
		replayCache: network.NewReplayCache(10 * time.Minute),
	}

	// Initialize empty proof-of-computation root.
	a.recomputeProofRoot()
	return a
}

// AttachKeys stores the signing keys for this agent.
// This keeps the transport layer stateless and untrusted.
func (a *Agent) AttachKeys(keys synthoscrypto.KeyPair) {
	a.keys = keys
	// Keep Identity.PublicKey in sync for discovery/sharing.
	a.Identity.PublicKey = synthoscrypto.PublicKeyHex(keys.Public)
	a.Identity.PrivateKeyHash = synthoscrypto.PrivateKeyHashHex(keys.Private)
}

// AttachTransport sets the outbound-only transport implementation.
func (a *Agent) AttachTransport(t network.Transport) {
	a.transport = t
}

var (
	ErrNoTransport   = errors.New("agent has no transport attached")
	ErrNoKeys        = errors.New("agent has no signing keys attached")
	ErrReplay        = errors.New("replay detected")
	ErrBadSignature  = errors.New("bad signature")
	ErrBadEnvelope   = errors.New("invalid envelope")
)

// BuildEnvelope creates and signs an envelope for the given message type.
func (a *Agent) BuildEnvelope(messageType string, toAgentID string, topic string, payload any) (network.Envelope, error) {
	if a.keys.Private == nil || a.keys.Public == nil {
		return network.Envelope{}, ErrNoKeys
	}

	poc := a.RecordComputation(map[string]any{
		"action":       "network_send",
		"message_type": messageType,
		"to_agent":     toAgentID,
		"topic":        topic,
	})
	_ = poc

	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return network.Envelope{}, err
	}

	nonce := randomNonce()
	env := network.Envelope{
		Version:               "v1",
		MessageType:           messageType,
		FromAgentID:           a.Identity.AgentID,
		ToAgentID:             toAgentID,
		Topic:                 topic,
		Nonce:                 "0x" + nonce,
		Timestamp:             time.Now().UTC(),
		Payload:               rawPayload,
		ProofOfComputationRoot: a.ProofRoot(),
		HardwareIDHash:        network.HardwareIDHashHex(a.Identity.HardwareID),
	}

	signBytes, err := env.SigningBytes()
	if err != nil {
		return network.Envelope{}, err
	}
	sig := synthoscrypto.Sign(a.keys.Private, signBytes)
	env.Signature = "0x" + base64.StdEncoding.EncodeToString(sig)

	return env, nil
}

// SendEnvelope serializes and sends an envelope through the attached transport.
func (a *Agent) SendEnvelope(env network.Envelope) error {
	if a.transport == nil {
		return ErrNoTransport
	}
	b, err := json.Marshal(env)
	if err != nil {
		return err
	}
	if env.ToAgentID != "" {
		return a.transport.SendToAgent(env.ToAgentID, b)
	}
	if env.Topic != "" {
		return a.transport.Broadcast(env.Topic, b)
	}
	// If neither is set, broadcast to a default topic.
	return a.transport.Broadcast("broadcast", b)
}

// VerifyEnvelope performs basic validation, replay protection, and signature verification.
// The caller supplies the expected public key bytes for the sender.
func (a *Agent) VerifyEnvelope(env network.Envelope, senderPublicKeyBytes []byte, now time.Time) error {
	if err := env.ValidateBasic(); err != nil {
		return ErrBadEnvelope
	}

	// Replay protection.
	if a.replayCache != nil && a.replayCache.SeenBefore(env.FromAgentID, env.Nonce, now) {
		return ErrReplay
	}

	// Hardware binding check: require that the hash matches the sender’s claimed hardware.
	// We can't validate the raw hardware ID here; we can only ensure the sender is at
	// least consistent (peers should track per-agent expected HardwareIDHash).
	if env.HardwareIDHash == "" {
		return ErrBadEnvelope
	}

	// Signature verification.
	signBytes, err := env.SigningBytes()
	if err != nil {
		return ErrBadEnvelope
	}
	// Signature is base64 with 0x prefix.
	sigStr := env.Signature
	if len(sigStr) >= 2 && sigStr[:2] == "0x" {
		sigStr = sigStr[2:]
	}
	sig, err := base64.StdEncoding.DecodeString(sigStr)
	if err != nil {
		return ErrBadSignature
	}
	if !synthoscrypto.Verify(senderPublicKeyBytes, signBytes, sig) {
		return ErrBadSignature
	}
	return nil
}

// RecordComputation creates and appends a new hardware-bound proof-of-computation
// entry for this agent and updates the rolling root hash.
func (a *Agent) RecordComputation(payload map[string]any) ProofOfComputation {
	nonce := randomNonce()

	poc := ProofOfComputation{
		HardwareID: a.Identity.HardwareID,
		Nonce:      nonce,
		Payload:    payload,
		Timestamp:  time.Now().UTC(),
	}

	poc.Hash = hashProof(poc)

	a.ProofLog = append(a.ProofLog, poc)
	a.recomputeProofRoot()

	return poc
}

// ProofRoot returns the current rolling root hash over all computation
// proofs for this agent.
func (a *Agent) ProofRoot() string {
	return a.Identity.ProofOfComputationRoot
}

// VerifyProof checks that a single proof is correctly bound to this agent's
// hardware ID and that its hash is self-consistent.
func (a *Agent) VerifyProof(p ProofOfComputation) bool {
	if p.HardwareID != a.Identity.HardwareID {
		return false
	}
	expected := hashProof(p)
	return p.Hash == expected
}

// recomputeProofRoot derives a single hash representing all proofs to date.
// This can be embedded in blocks or consensus messages to make an agent's
// compute history tamper-evident.
func (a *Agent) recomputeProofRoot() {
	if len(a.ProofLog) == 0 {
		a.Identity.ProofOfComputationRoot = ""
		return
	}

	type compact struct {
		HardwareID string    `json:"hardware_id"`
		Hash       string    `json:"hash"`
		Timestamp  time.Time `json:"timestamp"`
	}

	compacted := make([]compact, 0, len(a.ProofLog))
	for _, p := range a.ProofLog {
		compacted = append(compacted, compact{
			HardwareID: p.HardwareID,
			Hash:       p.Hash,
			Timestamp:  p.Timestamp,
		})
	}

	data, _ := json.Marshal(compacted)
	sum := sha256.Sum256(data)
	a.Identity.ProofOfComputationRoot = "0x" + hex.EncodeToString(sum[:16])
}

// hashProof builds the deterministic hash over a proof-of-computation entry.
func hashProof(p ProofOfComputation) string {
	data, _ := json.Marshal(struct {
		HardwareID string         `json:"hardware_id"`
		Nonce      string         `json:"nonce"`
		Payload    map[string]any `json:"payload"`
		Timestamp  time.Time      `json:"timestamp"`
	}{
		HardwareID: p.HardwareID,
		Nonce:      p.Nonce,
		Payload:    p.Payload,
		Timestamp:  p.Timestamp,
	})

	sum := sha256.Sum256(data)
	return "0x" + hex.EncodeToString(sum[:])
}

// randomNonce is a minimal placeholder for generating nonces.
// For production use, this should be replaced with a cryptographically secure
// random source (e.g. crypto/rand).
func randomNonce() string {
	// Use cryptographically secure randomness to avoid nonce collisions,
	// which would trigger replay protection incorrectly.
	buf := make([]byte, 32)
	_, err := rand.Read(buf)
	if err != nil {
		// Fallback to time-based entropy if crypto/rand fails (should be rare).
		now := time.Now().UTC().UnixNano()
		b, _ := json.Marshal(now)
		sum := sha256.Sum256(b)
		return hex.EncodeToString(sum[:16])
	}
	return hex.EncodeToString(buf[:16])
}

