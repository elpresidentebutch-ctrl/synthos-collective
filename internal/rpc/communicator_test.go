package rpc

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"synthos-collective/internal/agent"
	"synthos-collective/internal/chain"
	"synthos-collective/internal/consensus"
	synthoscrypto "synthos-collective/internal/crypto"
	"synthos-collective/internal/network"
	"synthos-collective/internal/node"
)

func newCommunicatorTestServers(t *testing.T) (a *Server, b *Server, bus *network.MemoryTransport) {
	t.Helper()
	bus = network.NewMemoryTransport()
	if err := bus.Start(); err != nil {
		t.Fatalf("bus.Start: %v", err)
	}

	mkServer := func(agentID string) (*Server, synthoscrypto.KeyPair) {
		keys, err := synthoscrypto.NewKeyPair()
		if err != nil {
			t.Fatal(err)
		}
		ag := agent.NewAgent(agentID, "", "", "test-hw-"+agentID, 0)
		if err := ag.AttachKeys(keys); err != nil {
			t.Fatal(err)
		}
		transport := bus.NodeTransport(agentID)
		ag.AttachTransport(transport)
		ch, err := chain.NewChain(chain.Genesis{
			ChainID:   "test-comm-chain",
			TxChainID: 999,
			Alloc:     map[chain.Address]uint64{"0x825fd94aa826da6ce0b4e57487418b72aea09f5e": 100},
		})
		if err != nil {
			t.Fatal(err)
		}
		n := node.NewNode(ag, ch, consensus.NewEngine(1), transport)
		if err := n.Start(); err != nil {
			t.Fatal(err)
		}
		return NewServer(ch, nil, n), keys
	}

	sa, keysA := mkServer("comm-a")
	sb, keysB := mkServer("comm-b")

	if err := sb.Node.AddPeer("comm-a", "0x"+hex.EncodeToString(keysA.Public)); err != nil {
		t.Fatal(err)
	}
	if err := sa.Node.AddPeer("comm-b", "0x"+hex.EncodeToString(keysB.Public)); err != nil {
		t.Fatal(err)
	}
	return sa, sb, bus
}

func TestCommunicatorSend_DeliversToKnownPeerInbox(t *testing.T) {
	sa, sb, _ := newCommunicatorTestServers(t)

	reqBody, _ := json.Marshal(communicatorSendRequest{ToAgentID: "comm-b", Body: "hi B"})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/communicator/send", bytes.NewReader(reqBody))
	sa.handleCommunicatorSend(w, r)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(sb.Node.Inbox()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	inbox := sb.Node.Inbox()
	if len(inbox) != 1 || inbox[0].Body != "hi B" || inbox[0].FromAgentID != "comm-a" {
		t.Fatalf("unexpected inbox contents: %+v", inbox)
	}

	// And the inbox RPC endpoint should report the same thing.
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("GET", "/communicator/inbox", nil)
	sb.handleCommunicatorInbox(w2, r2)
	var resp struct {
		Messages []node.CommunicatorMessage `json:"messages"`
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode inbox response: %v", err)
	}
	if len(resp.Messages) != 1 || resp.Messages[0].Body != "hi B" {
		t.Fatalf("unexpected /communicator/inbox response: %+v", resp)
	}
}

func TestCommunicatorSend_RejectsUnknownPeer(t *testing.T) {
	sa, _, _ := newCommunicatorTestServers(t)

	reqBody, _ := json.Marshal(communicatorSendRequest{ToAgentID: "someone-not-a-peer", Body: "hi"})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/communicator/send", bytes.NewReader(reqBody))
	sa.handleCommunicatorSend(w, r)

	if w.Code == 200 {
		t.Fatalf("expected send to an unregistered peer to be rejected, got 200: %s", w.Body.String())
	}
}
