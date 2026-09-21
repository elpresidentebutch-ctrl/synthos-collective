package agent_test

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"synthos-collective/internal/agent"
	"synthos-collective/internal/network"

	synthoscrypto "synthos-collective/internal/crypto"
)

// buildEnvelopeWithTimestamp mirrors Agent.BuildEnvelope exactly, except it
// lets the test pin an arbitrary signed Timestamp -- something
// BuildEnvelope itself deliberately doesn't expose, since it always uses
// time.Now(). Timestamp is part of Envelope.SigningBytes(), so this
// produces a genuinely, validly signed envelope for whatever timestamp is
// given, not a forged one.
func buildEnvelopeWithTimestamp(t *testing.T, sender *agent.Agent, keys synthoscrypto.KeyPair, ts time.Time) network.Envelope {
	t.Helper()
	// Mirrors BuildEnvelope's own call: a brand-new Agent's
	// ProofOfComputationRoot is empty until at least one proof is
	// recorded, and ValidateBasic requires it to be non-empty.
	sender.RecordComputation(map[string]any{"action": "test"})
	rawPayload, err := json.Marshal(network.CommunicatorPayload{Body: "hi"})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	env := network.Envelope{
		Version:                "v1",
		MessageType:            network.MessageCommunicator,
		FromAgentID:            sender.Identity.AgentID,
		Nonce:                  "0x" + time.Now().Format("150405.000000000"),
		Timestamp:              ts,
		Payload:                rawPayload,
		ProofOfComputationRoot: sender.ProofRoot(),
		HardwareIDHash:         network.HardwareIDHashHex(sender.Identity.HardwareID),
	}
	signBytes, err := env.SigningBytes()
	if err != nil {
		t.Fatalf("SigningBytes: %v", err)
	}
	sig := synthoscrypto.Sign(keys.Private, signBytes)
	env.Signature = "0x" + base64.StdEncoding.EncodeToString(sig)
	return env
}

func newTestAgent(t *testing.T, agentID string) (*agent.Agent, synthoscrypto.KeyPair) {
	t.Helper()
	keys, err := synthoscrypto.NewKeyPair()
	if err != nil {
		t.Fatalf("NewKeyPair: %v", err)
	}
	a := agent.NewAgent(agentID, "", "", "test-hw-"+agentID, 0)
	if err := a.AttachKeys(keys); err != nil {
		t.Fatalf("AttachKeys: %v", err)
	}
	return a, keys
}

// TestVerifyEnvelopeRejectsStaleTimestamp guards against the exact gap the
// audit found: consensus.FreshEnough existed, fully tested, with no caller
// anywhere -- so VerifyEnvelope's only defense against replaying an old,
// validly-signed envelope was the nonce-based replay cache, which only
// remembers a nonce for its own TTL (10 minutes). Past that TTL, a
// captured envelope with an old but otherwise untouched (still
// correctly-signed) timestamp could be replayed successfully, since
// nothing checked how old env.Timestamp actually was. This uses a
// never-before-seen nonce (so the replay cache alone would happily accept
// it) with a timestamp from an hour ago, and confirms VerifyEnvelope now
// rejects it purely on staleness.
func TestVerifyEnvelopeRejectsStaleTimestamp(t *testing.T) {
	sender, senderKeys := newTestAgent(t, "sender")
	verifier, _ := newTestAgent(t, "verifier")

	staleEnv := buildEnvelopeWithTimestamp(t, sender, senderKeys, time.Now().UTC().Add(-1*time.Hour))

	err := verifier.VerifyEnvelope(staleEnv, senderKeys.Public, time.Now().UTC())
	if err == nil {
		t.Fatal("expected a stale (1 hour old) envelope with a never-before-seen nonce to be rejected, got nil error")
	}
	if err != agent.ErrStaleEnvelope {
		t.Fatalf("expected ErrStaleEnvelope, got %v", err)
	}
}

// TestVerifyEnvelopeRejectsFarFutureTimestamp confirms the same check
// rejects a timestamp implausibly far in the future, not just a stale one.
func TestVerifyEnvelopeRejectsFarFutureTimestamp(t *testing.T) {
	sender, senderKeys := newTestAgent(t, "sender")
	verifier, _ := newTestAgent(t, "verifier")

	futureEnv := buildEnvelopeWithTimestamp(t, sender, senderKeys, time.Now().UTC().Add(1*time.Hour))

	err := verifier.VerifyEnvelope(futureEnv, senderKeys.Public, time.Now().UTC())
	if err != agent.ErrStaleEnvelope {
		t.Fatalf("expected ErrStaleEnvelope for a far-future timestamp, got %v", err)
	}
}

// TestVerifyEnvelopeAcceptsFreshTimestamp is the control: an envelope
// signed just now, with a real signature over real content, must still be
// accepted -- the freshness check must not be so strict it breaks ordinary
// traffic.
func TestVerifyEnvelopeAcceptsFreshTimestamp(t *testing.T) {
	sender, senderKeys := newTestAgent(t, "sender")
	verifier, _ := newTestAgent(t, "verifier")

	freshEnv := buildEnvelopeWithTimestamp(t, sender, senderKeys, time.Now().UTC())

	if err := verifier.VerifyEnvelope(freshEnv, senderKeys.Public, time.Now().UTC()); err != nil {
		t.Fatalf("expected a freshly-signed envelope to verify, got %v", err)
	}
}
