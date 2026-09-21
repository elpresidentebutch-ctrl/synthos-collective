package node_test

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"synthos-collective/internal/network"
)

// TestForgedEnvelopeCannotPinHardwareHashBeforeVerification guards against
// the exact bug the audit found in handleRaw: it used to pin
// PeerHardwareHash[env.FromAgentID] the first time any message claiming
// that agent ID arrived, BEFORE checking whether the envelope's signature
// actually came from that agent's real key. Naming a known peer's agent ID
// in a message's FromAgentID field doesn't require controlling that
// agent's key -- only Agent.VerifyEnvelope's signature check does -- so an
// attacker who got a single bogus, badly-signed envelope to this node
// first could permanently plant a fake hardware hash for a peer it had
// never actually heard from yet, silently condemning every future GENUINE,
// correctly-signed message from the real peer to be dropped as a
// "hardware hash changed" clone.
func TestForgedEnvelopeCannotPinHardwareHashBeforeVerification(t *testing.T) {
	bus := network.NewMemoryTransport()
	if err := bus.Start(); err != nil {
		t.Fatalf("bus.Start: %v", err)
	}

	nodeA, keysA := newTestNode(t, "agent-a", bus)
	nodeB, _ := newTestNode(t, "agent-b", bus)

	pubAHex := "0x" + hex.EncodeToString(keysA.Public)
	if err := nodeB.AddPeer("agent-a", pubAHex); err != nil {
		t.Fatalf("AddPeer on B: %v", err)
	}

	// The attacker is not a registered peer of B at all -- it doesn't need
	// to be, since handleRaw trusts the self-declared FromAgentID inside
	// the payload, not the transport-level sender. It forges a message
	// CLAIMING to be "agent-a" (already a known, trusted peer of B), with
	// a hardware hash the attacker made up and a signature that does not
	// verify.
	forged := network.Envelope{
		Version:                "1",
		MessageType:            network.MessageCommunicator,
		FromAgentID:            "agent-a",
		Nonce:                  "forged-nonce-1",
		Timestamp:              time.Now().UTC(),
		Payload:                json.RawMessage(`{"body":"forged, should never be recorded"}`),
		ProofOfComputationRoot: "0x00",
		HardwareIDHash:         "attacker-controlled-fake-hash",
		Signature:              "0xdeadbeef",
	}
	payload, err := json.Marshal(forged)
	if err != nil {
		t.Fatalf("marshal forged envelope: %v", err)
	}
	attacker := bus.NodeTransport("attacker-not-a-real-peer")
	if err := attacker.SendToAgent("agent-b", payload); err != nil {
		t.Fatalf("SendToAgent: %v", err)
	}

	// It must not have been recorded (bad signature).
	time.Sleep(100 * time.Millisecond)
	if inbox := nodeB.Inbox(); len(inbox) != 0 {
		t.Fatalf("forged envelope with an invalid signature was recorded: %+v", inbox)
	}

	// The real test: agent-a's REAL, correctly-signed message must still
	// be accepted afterward. If the forged envelope above had pinned its
	// fake hardware hash for "agent-a" (the pre-fix bug), this genuine
	// message -- carrying agent-a's real hardware hash -- would now be
	// dropped as a "hardware hash changed" clone.
	env, err := nodeA.Agent.BuildEnvelope(network.MessageCommunicator, "agent-b", "", network.CommunicatorPayload{Body: "real message from the real agent-a"})
	if err != nil {
		t.Fatalf("BuildEnvelope: %v", err)
	}
	if err := nodeA.Agent.SendEnvelope(env); err != nil {
		t.Fatalf("SendEnvelope: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	var gotBody string
	var gotCount int
	for time.Now().Before(deadline) {
		msgs := nodeB.Inbox()
		gotCount = len(msgs)
		if gotCount > 0 {
			gotBody = msgs[0].Body
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if gotCount != 1 {
		t.Fatalf("agent-a's genuine, correctly-signed message was rejected (got %d inbox messages) -- the forged envelope likely pinned a fake hardware hash first", gotCount)
	}
	if gotBody != "real message from the real agent-a" {
		t.Fatalf("unexpected inbox body: %q", gotBody)
	}
}

// TestHandleRawConcurrentFirstMessagesDoNotRace guards against the other
// half of the same audit finding: Peers, Validators, and PeerHardwareHash
// used to be plain, unsynchronized maps, even though handleRaw runs
// concurrently in a real deployment -- once per actively connected peer,
// since SecureTCPTransport.acceptLoop spawns one goroutine per accepted
// connection. This sends each of several distinct, never-before-seen
// peers' very first message to the same node at once, which is exactly
// the case that used to write PeerHardwareHash (and read Peers) from
// multiple goroutines with no lock at all. Run with -race, this fails
// loudly (a "DATA RACE" report) if that protection ever regresses; run
// without -race it still confirms every message was correctly recorded.
func TestHandleRawConcurrentFirstMessagesDoNotRace(t *testing.T) {
	bus := network.NewMemoryTransport()
	if err := bus.Start(); err != nil {
		t.Fatalf("bus.Start: %v", err)
	}

	hub, _ := newTestNode(t, "agent-hub", bus)

	const n = 20
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		agentID := fmt.Sprintf("agent-sender-%d", i)
		senderNode, keys := newTestNode(t, agentID, bus)
		pubHex := "0x" + hex.EncodeToString(keys.Public)
		if err := hub.AddPeer(agentID, pubHex); err != nil {
			t.Fatalf("AddPeer(%s): %v", agentID, err)
		}

		wg.Add(1)
		go func(agentID string) {
			defer wg.Done()
			env, err := senderNode.Agent.BuildEnvelope(network.MessageCommunicator, "agent-hub", "", network.CommunicatorPayload{Body: "hi from " + agentID})
			if err != nil {
				t.Errorf("BuildEnvelope(%s): %v", agentID, err)
				return
			}
			if err := senderNode.Agent.SendEnvelope(env); err != nil {
				t.Errorf("SendEnvelope(%s): %v", agentID, err)
			}
		}(agentID)
	}
	wg.Wait()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(hub.Inbox()) >= n {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := len(hub.Inbox()); got != n {
		t.Fatalf("hub inbox = %d messages, want %d", got, n)
	}
}
