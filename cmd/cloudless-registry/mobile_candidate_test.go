package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"testing"
	"time"
)

// TestMobileCandidateCapabilityIsRecognizedAndNotUpgraded guards the exact
// regression that motivated adding "mobile_candidate" to normalizeCapabilities'
// allow-list: before that change, an Android candidate sending only
// ["mobile_candidate"] would have every value stripped (the string wasn't
// recognized), leaving Capabilities empty -- which would then trip the
// len(entry.Capabilities)==0 fallback in handleAPINodeHeartbeat and silently
// upgrade the phone to the full validator core-capability set (quorum,
// persistent_storage, validator_registry, ...), none of which it actually
// has. A mobile candidate must keep exactly the capabilities it declares.
func TestMobileCandidateCapabilityIsRecognizedAndNotUpgraded(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s := &server{
		state: registryState{
			Peers:               map[string]peer{},
			Mailbox:             map[string][]mailboxMessage{},
			Contacts:            []contactMessage{},
			EarlyAccessPayments: map[string]earlyAccessPaymentIntent{},
		},
	}

	declared := []string{"mobile_candidate", "ed25519", "replay_protection"}

	postJSON(t, s.handleAPINodeRegister, "/api/nodes/register", map[string]any{
		"publicId":     "syn-android-candidate-1",
		"public_key":   hex.EncodeToString(publicKey),
		"mode":         "candidate",
		"capabilities": declared,
	}, http.StatusOK)

	registered := getNodeStatus(t, s, "/api/nodes/syn-android-candidate-1/status")
	if len(registered.Node.Capabilities) != len(declared) {
		t.Fatalf("registered capabilities = %v, want exactly %v (mobile_candidate must not be silently dropped)", registered.Node.Capabilities, declared)
	}

	timestamp := time.Now().UTC().Format(time.RFC3339)
	nonce := "00000000000000000001"
	message := canonicalHeartbeatMessage("syn-android-candidate-1", 1, "tip-1", "state-1", timestamp, nonce)
	postJSON(t, s.handleAPINodeHeartbeat, "/api/nodes/heartbeat", map[string]any{
		"node_id":      "syn-android-candidate-1",
		"height":       1,
		"tip":          "tip-1",
		"state_root":   "state-1",
		"timestamp":    timestamp,
		"nonce":        nonce,
		"signature":    hex.EncodeToString(ed25519.Sign(privateKey, message)),
		"capabilities": declared,
	}, http.StatusOK)

	status := getNodeStatus(t, s, "/api/nodes/syn-android-candidate-1/status")
	if len(status.Node.Capabilities) != len(declared) {
		t.Fatalf("post-heartbeat capability count = %d (%v), want %d (%v) -- a mobile candidate must never be auto-upgraded to the full validator core-capability set",
			len(status.Node.Capabilities), status.Node.Capabilities, len(declared), declared)
	}
	if status.Node.CapabilityStatus["quorum"] {
		t.Fatal("mobile candidate must not be marked as having quorum capability -- it never sent that")
	}
	if status.Node.CapabilityStatus["persistent_storage"] {
		t.Fatal("mobile candidate must not be marked as having persistent_storage capability -- it never sent that")
	}
	if !status.Node.CapabilityStatus["mobile_candidate"] {
		t.Fatal("mobile_candidate tag should be present and marked true")
	}
	if !status.Node.RealSignedHeartbeat {
		t.Fatal("real_signed_heartbeat should be true after a valid Ed25519 heartbeat")
	}
}

// TestNormalizeCapabilitiesKeepsMobileCandidateTag is a narrower unit check
// directly on normalizeCapabilities, independent of the HTTP handlers above.
func TestNormalizeCapabilitiesKeepsMobileCandidateTag(t *testing.T) {
	got := normalizeCapabilities([]string{"mobile_candidate", "ed25519", "not_a_real_capability"})
	want := map[string]bool{"mobile_candidate": true, "ed25519": true}
	if len(got) != len(want) {
		t.Fatalf("normalizeCapabilities(...) = %v, want exactly %v", got, want)
	}
	for _, v := range got {
		if !want[v] {
			t.Fatalf("unexpected capability %q survived normalization", v)
		}
	}
}
