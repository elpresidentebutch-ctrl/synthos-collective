package main

import (
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"synthos-collective/internal/agent"
	"synthos-collective/internal/chain"
	"synthos-collective/internal/consensus"
	synthoscrypto "synthos-collective/internal/crypto"
	"synthos-collective/internal/network"
	"synthos-collective/internal/node"
	"synthos-collective/internal/rpc"
)

// --- mergeValidatorRoster: pure-function unit tests -----------------------

func mustPubKeyHex(t *testing.T) (string, ed25519.PublicKey) {
	t.Helper()
	keys, err := synthoscrypto.NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	return synthoscrypto.PublicKeyHex(keys.Public), keys.Public
}

func TestMergeValidatorRoster_EmptyRosterReproducesStaticBaselineExactly(t *testing.T) {
	staticKeys := map[string]ed25519.PublicKey{"a": {1, 2, 3}}
	validators, keys, peers := mergeValidatorRoster(
		[]string{"a"}, staticKeys, []string{"https://a.example.com"}, "self", nil,
	)
	if len(validators) != 1 || validators[0] != "a" {
		t.Fatalf("expected validators unchanged, got %v", validators)
	}
	if len(keys) != 1 || string(keys["a"]) != string(staticKeys["a"]) {
		t.Fatalf("expected keys unchanged, got %v", keys)
	}
	if len(peers) != 1 || peers[0] != "https://a.example.com" {
		t.Fatalf("expected consensus peer URLs unchanged, got %v", peers)
	}
}

func TestMergeValidatorRoster_AddsNewApprovedValidator(t *testing.T) {
	hexKey, pubKey := mustPubKeyHex(t)
	roster := []rosterEntry{{NodeID: "home-1", PublicKey: hexKey, PublicURL: "", Reachable: false}}

	validators, keys, peers := mergeValidatorRoster(
		[]string{"self"}, map[string]ed25519.PublicKey{"self": {9}}, nil, "self", roster,
	)
	if len(validators) != 2 || validators[0] != "home-1" || validators[1] != "self" {
		t.Fatalf("expected [home-1 self] (sorted), got %v", validators)
	}
	if string(keys["home-1"]) != string(pubKey) {
		t.Fatalf("expected the new validator's real key to be recorded")
	}
	if len(peers) != 0 {
		t.Fatalf("a non-reachable entry with no public_url must not become a consensus peer URL, got %v", peers)
	}
}

func TestMergeValidatorRoster_ReachableEntryBecomesConsensusPeerURL(t *testing.T) {
	hexKey, _ := mustPubKeyHex(t)
	roster := []rosterEntry{{NodeID: "vps-1", PublicKey: hexKey, PublicURL: "https://vps-1.example.com", Reachable: true}}

	_, _, peers := mergeValidatorRoster(nil, nil, nil, "self", roster)
	if len(peers) != 1 || peers[0] != "https://vps-1.example.com" {
		t.Fatalf("expected the reachable entry's URL to appear, got %v", peers)
	}
}

func TestMergeValidatorRoster_SkipsSelfEmptyAndMalformedEntries(t *testing.T) {
	hexKey, _ := mustPubKeyHex(t)
	roster := []rosterEntry{
		{NodeID: "self", PublicKey: hexKey, Reachable: true, PublicURL: "https://self.example.com"}, // self -- skip
		{NodeID: "", PublicKey: hexKey},                                                             // empty id -- skip
		{NodeID: "bad-key", PublicKey: "not-hex", Reachable: true, PublicURL: "https://bad.example.com"},
		{NodeID: "short-key", PublicKey: "aabbcc", Reachable: true, PublicURL: "https://short.example.com"},
	}
	validators, keys, peers := mergeValidatorRoster([]string{"self"}, map[string]ed25519.PublicKey{"self": {1}}, nil, "self", roster)
	if len(validators) != 1 || validators[0] != "self" {
		t.Fatalf("expected only the static self entry to survive, got %v", validators)
	}
	if len(keys) != 1 {
		t.Fatalf("expected no malformed keys to be added, got %v", keys)
	}
	if len(peers) != 0 {
		t.Fatalf("a skipped entry must not still contribute a consensus peer URL, got %v", peers)
	}
}

func TestMergeValidatorRoster_DoesNotDuplicateAnAlreadyStaticValidator(t *testing.T) {
	hexKey, pubKey := mustPubKeyHex(t)
	roster := []rosterEntry{{NodeID: "already-static", PublicKey: hexKey, Reachable: true, PublicURL: "https://dup.example.com"}}

	validators, keys, peers := mergeValidatorRoster(
		[]string{"already-static"}, map[string]ed25519.PublicKey{"already-static": {7}}, []string{"https://dup.example.com"},
		"self", roster,
	)
	if len(validators) != 1 || validators[0] != "already-static" {
		t.Fatalf("expected no duplicate entry, got %v", validators)
	}
	// The roster's key for an already-known ID still refreshes the stored
	// key (in case a candidate rotated keys and was re-approved) --
	// confirm that update actually happened rather than being silently
	// dropped in favor of the stale static one.
	if string(keys["already-static"]) != string(pubKey) {
		t.Fatal("expected the roster's key to refresh an already-known validator's stored key")
	}
	if len(peers) != 1 {
		t.Fatalf("expected the URL to appear exactly once despite being in both static and roster, got %v", peers)
	}
}

func TestMergeValidatorRoster_IsIdempotentAcrossRepeatedCalls(t *testing.T) {
	hexKey, _ := mustPubKeyHex(t)
	roster := []rosterEntry{{NodeID: "home-1", PublicKey: hexKey, Reachable: true, PublicURL: "https://home-1.example.com"}}

	v1, k1, p1 := mergeValidatorRoster([]string{"self"}, map[string]ed25519.PublicKey{"self": {1}}, nil, "self", roster)
	v2, k2, p2 := mergeValidatorRoster([]string{"self"}, map[string]ed25519.PublicKey{"self": {1}}, nil, "self", roster)

	if len(v1) != len(v2) || v1[0] != v2[0] || v1[1] != v2[1] {
		t.Fatalf("expected identical validator lists across repeated calls, got %v vs %v", v1, v2)
	}
	if len(k1) != len(k2) || string(k1["home-1"]) != string(k2["home-1"]) {
		t.Fatal("expected identical key maps across repeated calls")
	}
	if len(p1) != len(p2) || p1[0] != p2[0] {
		t.Fatalf("expected identical consensus peer URL lists across repeated calls, got %v vs %v", p1, p2)
	}
}

// --- fetchValidatorRoster: HTTP behavior -----------------------------------

func TestFetchValidatorRoster_ParsesRealResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/validators/roster" {
			t.Errorf("expected /api/validators/roster, got %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"validators": []map[string]any{
				{"node_id": "home-1", "public_key": "aabb", "public_url": "", "reachable": false},
			},
		})
	}))
	defer srv.Close()

	roster, err := fetchValidatorRoster(srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(roster) != 1 || roster[0].NodeID != "home-1" {
		t.Fatalf("unexpected roster: %+v", roster)
	}
}

func TestFetchValidatorRoster_ErrorsOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	if _, err := fetchValidatorRoster(srv.Client(), srv.URL); err == nil {
		t.Fatal("expected an error on a non-2xx registry response")
	}
}

func TestFetchValidatorRoster_ErrorsOnMalformedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	if _, err := fetchValidatorRoster(srv.Client(), srv.URL); err == nil {
		t.Fatal("expected an error decoding a malformed registry response")
	}
}

// --- startValidatorRosterSync: end-to-end wiring ---------------------------

// rosterFixture is a small, thread-safe stand-in for the registry's
// GET /api/validators/roster endpoint, so a test can change what it
// returns (an approval, then a revoke) while startValidatorRosterSync's
// background goroutine is actively polling it.
type rosterFixture struct {
	mu      sync.Mutex
	entries []rosterEntry
}

func (f *rosterFixture) set(entries []rosterEntry) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries = entries
}

func (f *rosterFixture) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		entries := append([]rosterEntry(nil), f.entries...)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "validators": entries, "count": len(entries)})
	}
}

// rosterSyncTestNode is a minimal single-node chain+node+server stack, real
// enough to exercise startValidatorRosterSync's actual effect on live
// consensus state (not a mock).
type rosterSyncTestNode struct {
	id     string
	agent  *agent.Agent
	pubHex string
	pub    ed25519.PublicKey
	chain  *chain.Chain
	node   *node.Node
	server *rpc.Server
}

func newRosterSyncTestNode(t *testing.T, id string) *rosterSyncTestNode {
	t.Helper()
	keys, err := synthoscrypto.NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	a := agent.NewAgent(id, "", "", "hw-"+id, 0)
	if err := a.AttachKeys(keys); err != nil {
		t.Fatal(err)
	}
	genesis := chain.Genesis{
		ChainID:   "test-chain",
		TxChainID: 20260924,
		Alloc:     map[chain.Address]uint64{"0x825fd94aa826da6ce0b4e57487418b72aea09f5e": 100},
	}
	ch, err := chain.NewChain(genesis)
	if err != nil {
		t.Fatal(err)
	}
	bus := network.NewMemoryTransport()
	transport := bus.NodeTransport(a.Identity.AgentID)
	a.AttachTransport(transport)
	n := node.NewNode(a, ch, consensus.NewEngine(1), transport)
	if err := n.Start(); err != nil {
		t.Fatal(err)
	}
	srv := rpc.NewServer(ch, nil, n)

	pubBytes, err := synthoscrypto.PublicKeyBytes(a.Identity.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	pub := ed25519.PublicKey(pubBytes)

	n.SetValidators([]string{id})
	ch.SetValidatorSet(map[string]ed25519.PublicKey{id: pub}, 1)

	return &rosterSyncTestNode{id: id, agent: a, pubHex: a.Identity.PublicKey, pub: pub, chain: ch, node: n, server: srv}
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !cond() {
		t.Fatal("timed out waiting for condition")
	}
}

// TestStartValidatorRosterSync_GrowsAndShrinksLiveQuorumWithoutRedeploy is
// the core Phase 2 regression test: a validator approved through the
// registry (Phase 1's queue) must actually become a trusted signer and
// grow the live quorum on an already-running node, with no redeploy --
// and a later revoke (the roster shrinking back down) must be picked up
// too, not stick around forever once added. Run with -race: a second
// goroutine concurrently hammers the same Engine state the whole time,
// simulating real concurrent vote-handling traffic arriving while the
// refresh loop is writing.
func TestStartValidatorRosterSync_GrowsAndShrinksLiveQuorumWithoutRedeploy(t *testing.T) {
	producer := newRosterSyncTestNode(t, "producer")
	home := newRosterSyncTestNode(t, "home-candidate-1")

	fixture := &rosterFixture{}
	registry := httptest.NewServer(fixture.handler())
	t.Cleanup(registry.Close)

	if got := producer.node.Consensus.RequiredForFinality(); got != 1 {
		t.Fatalf("expected baseline quorum 1 with just the producer, got %d", got)
	}

	// Stress concurrent readers for the whole test, to empirically back up
	// (not just argue in a doc comment) that periodic SetValidators calls
	// from startValidatorRosterSync's goroutine don't race against normal
	// concurrent Engine reads from request-handling goroutines.
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = producer.node.Consensus.RequiredForFinality()
				_, _, _, _ = producer.node.Consensus.FinalityStatus("nonexistent-hash")
				time.Sleep(time.Millisecond)
			}
		}
	}()
	t.Cleanup(func() {
		close(stop)
		wg.Wait()
	})

	startValidatorRosterSync(
		producer.chain, producer.node, producer.server, producer.id,
		[]string{producer.id}, map[string]ed25519.PublicKey{producer.id: producer.pub},
		[]string{producer.id},
		nil,
		registry.URL,
		true,
		10*time.Millisecond,
	)

	// Approve: the registry now lists home-candidate-1 as a real
	// validator (not reachable -- the common home/laptop case).
	fixture.set([]rosterEntry{{
		NodeID:    home.id,
		PublicKey: home.pubHex,
		PublicURL: "",
		Reachable: false,
	}})

	waitFor(t, 2*time.Second, func() bool {
		return producer.node.Consensus.RequiredForFinality() == 2
	})
	if !producer.node.HasPeer(home.id) {
		t.Fatal("expected the newly-approved validator to be registered as a known peer")
	}
	if urls := producer.server.ConsensusPeerURLsSnapshot(); len(urls) != 0 {
		t.Fatalf("a non-reachable approval must not add a consensus peer URL, got %v", urls)
	}

	// Revoke: the registry now lists nobody. The refresh loop must pick
	// this up on its next tick and shrink back to the static baseline --
	// an added validator must not stay trusted forever once the registry
	// no longer lists it.
	fixture.set(nil)
	waitFor(t, 2*time.Second, func() bool {
		return producer.node.Consensus.RequiredForFinality() == 1
	})
}

// TestStartValidatorRosterSync_ReachableApprovalAddsConsensusPeerURL
// confirms the other half of an approval: a validator approved WITH a
// public_url (an operator on a real server, not a home PC) is added to
// the producer's consensus-peer URL list, so it's actually asked to vote
// -- not just trusted as a signer.
func TestStartValidatorRosterSync_ReachableApprovalAddsConsensusPeerURL(t *testing.T) {
	producer := newRosterSyncTestNode(t, "producer")
	vps := newRosterSyncTestNode(t, "vps-validator-1")

	fixture := &rosterFixture{}
	registry := httptest.NewServer(fixture.handler())
	t.Cleanup(registry.Close)
	fixture.set([]rosterEntry{{
		NodeID:    vps.id,
		PublicKey: vps.pubHex,
		PublicURL: "https://vps-validator-1.example.com",
		Reachable: true,
	}})

	startValidatorRosterSync(
		producer.chain, producer.node, producer.server, producer.id,
		[]string{producer.id}, map[string]ed25519.PublicKey{producer.id: producer.pub},
		[]string{producer.id},
		nil,
		registry.URL,
		true,
		10*time.Millisecond,
	)

	waitFor(t, 2*time.Second, func() bool {
		urls := producer.server.ConsensusPeerURLsSnapshot()
		return len(urls) == 1 && urls[0] == "https://vps-validator-1.example.com"
	})
}

// TestStartValidatorRosterSync_InertWhenConsensusDisabledOrNoRegistryURL
// guards the rollout-safety gate: this must do nothing at all -- not even
// start a goroutine that could later panic on a nil dependency -- when
// consensus isn't enabled or no registry URL is configured, mirroring
// resolveConsensusValidators/resolveChainQuorum's own gating for the
// static config path.
func TestStartValidatorRosterSync_InertWhenConsensusDisabledOrNoRegistryURL(t *testing.T) {
	producer := newRosterSyncTestNode(t, "producer")

	// consensusEnabled=false: passing a deliberately-invalid registryURL
	// and nil chain/node/server would panic immediately if this failed to
	// return early, proving the gate actually short-circuits before doing
	// anything.
	startValidatorRosterSync(nil, nil, nil, "producer", nil, nil, nil, nil, "http://127.0.0.1:0", false, time.Millisecond)

	// registryURL="": same proof, with consensusEnabled=true this time.
	startValidatorRosterSync(nil, nil, nil, "producer", nil, nil, nil, nil, "", true, time.Millisecond)

	// Give either case a moment to prove it truly started nothing.
	time.Sleep(50 * time.Millisecond)
	if got := producer.node.Consensus.RequiredForFinality(); got != 1 {
		t.Fatalf("producer's own state must be untouched by either inert call, got quorum %d", got)
	}
}
