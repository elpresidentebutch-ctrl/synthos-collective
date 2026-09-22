package rpc

import (
	"net/http/httptest"
	"testing"

	"synthos-collective/internal/agent"
	"synthos-collective/internal/chain"
	"synthos-collective/internal/consensus"
	synthoscrypto "synthos-collective/internal/crypto"
	"synthos-collective/internal/network"
	"synthos-collective/internal/node"
)

func newProposeBlockTestServer(t *testing.T) *Server {
	t.Helper()
	keys, err := synthoscrypto.NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	a := agent.NewAgent("propose-validator-1", "", "", "test-hardware", 0)
	if err := a.AttachKeys(keys); err != nil {
		t.Fatal(err)
	}
	ch, err := chain.NewChain(chain.Genesis{
		ChainID:   "test-propose-chain",
		TxChainID: 20260921,
		Alloc: map[chain.Address]uint64{
			"0x825fd94aa826da6ce0b4e57487418b72aea09f5e": 100,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	bus := network.NewMemoryTransport()
	if err := bus.Start(); err != nil {
		t.Fatal(err)
	}
	transport := bus.NodeTransport(a.Identity.AgentID)
	a.AttachTransport(transport)
	n := node.NewNode(a, ch, consensus.NewEngine(1), transport)
	n.SetValidators([]string{a.Identity.AgentID})
	if err := n.Start(); err != nil {
		t.Fatal(err)
	}
	return NewServer(ch, nil, n)
}

// TestProposeBlock_RequiresToken guards against the exact bug the audit
// found: this endpoint used to have no authentication at all, letting any
// caller on the internet trigger a full block build+sign+finalize+persist+
// broadcast cycle on demand -- and, worse, letting it be triggered on a
// follower validator rather than the one node meant to run the automatic
// proposal loop, risking a real chain fork (see handleProposeBlock's own
// doc comment). Now it's disabled by default (no token configured) and
// requires an exact, operator-configured token when enabled, mirroring
// /communicator/send's fix.
func TestProposeBlock_RequiresToken(t *testing.T) {
	s := newProposeBlockTestServer(t)

	// No token configured at all -- disabled, not open.
	w := httptest.NewRecorder()
	s.handleProposeBlock(w, httptest.NewRequest("POST", "/proposeBlock", nil))
	if w.Code != 503 {
		t.Fatalf("expected 503 with no token configured, got %d: %s", w.Code, w.Body.String())
	}
	if s.Chain.Height() != 0 {
		t.Fatalf("chain height changed with no token configured: got %d, want 0", s.Chain.Height())
	}

	s.ProposeBlockToken = "real-token"

	// Configured, but caller supplies no token.
	w = httptest.NewRecorder()
	s.handleProposeBlock(w, httptest.NewRequest("POST", "/proposeBlock", nil))
	if w.Code != 401 {
		t.Fatalf("expected 401 with a missing token, got %d: %s", w.Code, w.Body.String())
	}

	// Configured, caller supplies the wrong token.
	w = httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/proposeBlock", nil)
	r.Header.Set("X-Propose-Block-Token", "wrong-token")
	s.handleProposeBlock(w, r)
	if w.Code != 401 {
		t.Fatalf("expected 401 with an incorrect token, got %d: %s", w.Code, w.Body.String())
	}

	if s.Chain.Height() != 0 {
		t.Fatalf("chain height changed despite every unauthorized attempt: got %d, want 0", s.Chain.Height())
	}

	// Configured, correct token: the block is actually proposed.
	w = httptest.NewRecorder()
	r = httptest.NewRequest("POST", "/proposeBlock", nil)
	r.Header.Set("X-Propose-Block-Token", "real-token")
	s.handleProposeBlock(w, r)
	if w.Code != 200 {
		t.Fatalf("expected 200 with the correct token, got %d: %s", w.Code, w.Body.String())
	}
	if s.Chain.Height() != 1 {
		t.Fatalf("chain height after an authorized propose: got %d, want 1", s.Chain.Height())
	}
}
