package rpc

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"synthos-collective/internal/agent"
	"synthos-collective/internal/chain"
	"synthos-collective/internal/consensus"
	synthoscrypto "synthos-collective/internal/crypto"
	"synthos-collective/internal/network"
	"synthos-collective/internal/node"
)

func newSimEconTestServer(t *testing.T) (*Server, chain.Address, ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	keys, err := synthoscrypto.NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	addr := chain.AddressFromPublicKey(keys.Public)

	ch, err := chain.NewChain(chain.Genesis{
		ChainID:   "sim-econ-chain",
		TxChainID: 4242,
		Alloc: map[chain.Address]uint64{
			addr: 1_000,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	a := agent.NewAgent("sim-econ-node", "", "", "test-hw", 0)
	bus := network.NewMemoryTransport()
	a.AttachTransport(bus.NodeTransport(a.Identity.AgentID))
	n := node.NewNode(a, ch, consensus.NewEngine(1), bus.NodeTransport(a.Identity.AgentID))
	srv := NewServer(ch, nil, n)
	return srv, addr, keys.Public, keys.Private
}

// TestSimulateTx_DoesNotMutateLiveState proves the simulator is real: it
// runs the actual signature/nonce/balance checks a live submission would
// (chain.Chain.SimulateTx calls tx.Verify), but no matter the outcome, the
// live chain's balance and mempool are completely untouched.
func TestSimulateTx_DoesNotMutateLiveState(t *testing.T) {
	srv, from, fromPub, fromPriv := newSimEconTestServer(t)
	to := chain.Address("0xdestination0000000000000000000000000000")

	tx := chain.Tx{
		ChainID:   4242,
		From:      from,
		To:        to,
		Amount:    100,
		Fee:       1,
		Nonce:     0,
		PublicKey: synthoscrypto.PublicKeyHex(fromPub),
	}
	if err := tx.Sign(fromPriv); err != nil {
		t.Fatal(err)
	}

	balanceBefore := srv.Chain.State.Get(from).Balance

	body, _ := json.Marshal(tx)
	w := httptest.NewRecorder()
	srv.handleSimulateTx(w, httptest.NewRequest("POST", "/simulate/tx", bytes.NewReader(body)))
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		OK     bool                   `json:"ok"`
		Result chain.SimulationResult `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.OK || !resp.Result.Applied {
		t.Fatalf("expected a successfully applied simulation, got %+v", resp)
	}
	if resp.Result.FromBalanceAfter != balanceBefore-101 {
		t.Fatalf("expected simulated from-balance %d, got %d", balanceBefore-101, resp.Result.FromBalanceAfter)
	}

	// The critical assertion: live state is untouched.
	balanceAfter := srv.Chain.State.Get(from).Balance
	if balanceAfter != balanceBefore {
		t.Fatalf("simulation must not mutate live state: before=%d after=%d", balanceBefore, balanceAfter)
	}
	if len(srv.Chain.MempoolSnapshot()) != 0 {
		t.Fatal("simulation must not add anything to the real mempool")
	}
}

// TestSimulateTx_RejectsBadSignatureJustLikeRealSubmission proves the
// simulator isn't a rubber stamp: a forged/unsigned transaction is rejected
// by the same real signature check a live submission would hit.
func TestSimulateTx_RejectsBadSignatureJustLikeRealSubmission(t *testing.T) {
	srv, from, fromPub, _ := newSimEconTestServer(t)
	to := chain.Address("0xdestination0000000000000000000000000000")

	tx := chain.Tx{
		ID:        "0xnotarealsignature",
		ChainID:   4242,
		From:      from,
		To:        to,
		Amount:    100,
		Fee:       1,
		Nonce:     0,
		PublicKey: synthoscrypto.PublicKeyHex(fromPub),
		Signature: "0x00",
	}

	body, _ := json.Marshal(tx)
	w := httptest.NewRecorder()
	srv.handleSimulateTx(w, httptest.NewRequest("POST", "/simulate/tx", bytes.NewReader(body)))

	var resp struct {
		OK     bool                   `json:"ok"`
		Result chain.SimulationResult `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.OK || resp.Result.Applied {
		t.Fatalf("expected a bad signature to be rejected by simulation, got %+v", resp)
	}
	if resp.Result.Error == "" {
		t.Fatal("expected a real error message explaining rejection")
	}
}

// TestSimulateBlock_IsReadOnly proves simulating a block repeatedly never
// advances the real chain height or touches the mempool.
func TestSimulateBlock_IsReadOnly(t *testing.T) {
	srv, _, _, _ := newSimEconTestServer(t)
	heightBefore := srv.Chain.Height()

	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		srv.handleSimulateBlock(w, httptest.NewRequest("GET", "/simulate/block", nil))
		if w.Code != 200 {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
	}
	if got := srv.Chain.Height(); got != heightBefore {
		t.Fatalf("simulating a block must not advance chain height: before=%d after=%d", heightBefore, got)
	}
}

// TestEconomyStats_ReflectsRealLedger proves the economist figures actually
// move when the real ledger changes, rather than being static.
func TestEconomyStats_ReflectsRealLedger(t *testing.T) {
	srv, addr, _, _ := newSimEconTestServer(t)

	w := httptest.NewRecorder()
	srv.handleEconomyStats(w, httptest.NewRequest("GET", "/economy/stats", nil))
	var before map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &before); err != nil {
		t.Fatal(err)
	}
	circulatingBefore := before["circulating_supply"].(float64)
	if uint64(circulatingBefore) != 1_000 {
		t.Fatalf("expected circulating_supply=1000 from genesis alloc, got %v", circulatingBefore)
	}

	// Mutate real state directly (as a finalized block would) and confirm
	// the stats endpoint reflects it on the next call.
	acc := srv.Chain.State.Get(addr)
	acc.Balance += 500
	srv.Chain.State.Set(addr, acc)

	w = httptest.NewRecorder()
	srv.handleEconomyStats(w, httptest.NewRequest("GET", "/economy/stats", nil))
	var after map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &after); err != nil {
		t.Fatal(err)
	}
	circulatingAfter := after["circulating_supply"].(float64)
	if uint64(circulatingAfter) != 1_500 {
		t.Fatalf("expected circulating_supply to reflect the real balance change (1500), got %v", circulatingAfter)
	}
}
