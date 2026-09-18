package node_test

import (
	"encoding/hex"
	"testing"
	"time"

	"synthos-collective/internal/agent"
	"synthos-collective/internal/chain"
	"synthos-collective/internal/consensus"
	synthoscrypto "synthos-collective/internal/crypto"
	"synthos-collective/internal/network"
	"synthos-collective/internal/node"
)

func newTestNode(t *testing.T, agentID string, bus *network.MemoryTransport) (*node.Node, synthoscrypto.KeyPair) {
	t.Helper()
	keys, err := synthoscrypto.NewKeyPair()
	if err != nil {
		t.Fatalf("NewKeyPair: %v", err)
	}
	a := agent.NewAgent(agentID, "", "", "test-hw-"+agentID, 0)
	if err := a.AttachKeys(keys); err != nil {
		t.Fatalf("AttachKeys: %v", err)
	}
	transport := bus.NodeTransport(agentID)
	a.AttachTransport(transport)

	ch, err := chain.NewChain(chain.Genesis{
		ChainID:   "test-chain",
		TxChainID: 20260702,
		Alloc:     map[chain.Address]uint64{"0x825fd94aa826da6ce0b4e57487418b72aea09f5e": 100},
	})
	if err != nil {
		t.Fatalf("NewChain: %v", err)
	}

	n := node.NewNode(a, ch, consensus.NewEngine(1), transport)
	if err := n.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return n, keys
}

// TestCommunicatorMessageDeliveredToKnownPeer covers the Communicator role's
// send_message/handle_incoming_message path end to end: agent A sends a
// real, signed envelope over a real transport, and it should land in B's
// Inbox with the sender correctly attributed.
func TestCommunicatorMessageDeliveredToKnownPeer(t *testing.T) {
	bus := network.NewMemoryTransport()
	if err := bus.Start(); err != nil {
		t.Fatalf("bus.Start: %v", err)
	}

	nodeA, keysA := newTestNode(t, "agent-a", bus)
	nodeB, keysB := newTestNode(t, "agent-b", bus)

	// Each side must know the other's public key to accept messages from
	// them (see node.go handleRaw: unknown peers are dropped).
	pubAHex := "0x" + hex.EncodeToString(keysA.Public)
	pubBHex := "0x" + hex.EncodeToString(keysB.Public)
	if err := nodeB.AddPeer("agent-a", pubAHex); err != nil {
		t.Fatalf("AddPeer on B: %v", err)
	}
	if err := nodeA.AddPeer("agent-b", pubBHex); err != nil {
		t.Fatalf("AddPeer on A: %v", err)
	}

	env, err := nodeA.Agent.BuildEnvelope(network.MessageCommunicator, "agent-b", "", network.CommunicatorPayload{Body: "hello from A"})
	if err != nil {
		t.Fatalf("BuildEnvelope: %v", err)
	}
	if err := nodeA.Agent.SendEnvelope(env); err != nil {
		t.Fatalf("SendEnvelope: %v", err)
	}

	// MemoryTransport delivers synchronously via a goroutine dispatch; give
	// it a moment rather than asserting on a data race against Inbox().
	deadline := time.Now().Add(2 * time.Second)
	var inbox []node.CommunicatorMessage
	for time.Now().Before(deadline) {
		inbox = nodeB.Inbox()
		if len(inbox) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if len(inbox) != 1 {
		t.Fatalf("nodeB inbox = %d messages, want 1", len(inbox))
	}
	if inbox[0].FromAgentID != "agent-a" {
		t.Fatalf("FromAgentID = %q, want agent-a", inbox[0].FromAgentID)
	}
	if inbox[0].Body != "hello from A" {
		t.Fatalf("Body = %q, want %q", inbox[0].Body, "hello from A")
	}

	// A never registered B as a known peer... wait, it did above; instead
	// confirm the unknown-sender path separately below.
	_ = nodeA
}

// TestCommunicatorMessageFromUnknownPeerIsDropped confirms a message from a
// peer never added via AddPeer is silently dropped rather than recorded --
// the same trust boundary every other message type in handleRaw already
// enforces.
func TestCommunicatorMessageFromUnknownPeerIsDropped(t *testing.T) {
	bus := network.NewMemoryTransport()
	if err := bus.Start(); err != nil {
		t.Fatalf("bus.Start: %v", err)
	}

	nodeA, _ := newTestNode(t, "stranger-a", bus)
	nodeB, _ := newTestNode(t, "stranger-b", bus)
	// Deliberately do NOT call nodeB.AddPeer for stranger-a.

	env, err := nodeA.Agent.BuildEnvelope(network.MessageCommunicator, "stranger-b", "", network.CommunicatorPayload{Body: "should be dropped"})
	if err != nil {
		t.Fatalf("BuildEnvelope: %v", err)
	}
	if err := nodeA.Agent.SendEnvelope(env); err != nil {
		t.Fatalf("SendEnvelope: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	if inbox := nodeB.Inbox(); len(inbox) != 0 {
		t.Fatalf("expected message from unknown peer to be dropped, got %d messages", len(inbox))
	}
}
