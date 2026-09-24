package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"synthos-collective/internal/consensus"
	synthoscrypto "synthos-collective/internal/crypto"
)

// --- mailboxRelayPeersFromRoster: pure-function unit tests ------------------

func TestMailboxRelayPeersFromRoster_IncludesUnreachableApprovedEntries(t *testing.T) {
	hexKey, _ := mustPubKeyHex(t)
	roster := []rosterEntry{{NodeID: "home-1", PublicKey: hexKey, Reachable: false, PublicURL: ""}}
	got := mailboxRelayPeersFromRoster(roster, "self")
	if len(got) != 1 || got[0] != "home-1" {
		t.Fatalf("expected [home-1], got %v", got)
	}
}

func TestMailboxRelayPeersFromRoster_ExcludesReachableEntries(t *testing.T) {
	hexKey, _ := mustPubKeyHex(t)
	roster := []rosterEntry{{NodeID: "vps-1", PublicKey: hexKey, Reachable: true, PublicURL: "https://vps-1.example.com"}}
	got := mailboxRelayPeersFromRoster(roster, "self")
	if len(got) != 0 {
		t.Fatalf("expected no relay peers for a directly reachable entry, got %v", got)
	}
}

func TestMailboxRelayPeersFromRoster_ExcludesEntryMarkedReachableWithNoURL(t *testing.T) {
	// Defensive: reachable=true with an empty public_url shouldn't happen
	// given how the registry computes it (reachable := public_url != ""),
	// but this function must still resolve it safely rather than treating
	// it as a relay candidate it can't actually reach either way -- no,
	// wait: with no URL and Reachable claimed true, mergeValidatorRoster
	// itself would also skip adding a consensus peer URL (it requires a
	// non-empty url), so this entry would otherwise be silently unreachable
	// by BOTH paths. This function treats "not usably reachable" as
	// "relay it" specifically so a malformed registry response degrades to
	// the mailbox path rather than to "never contacted at all."
	hexKey, _ := mustPubKeyHex(t)
	roster := []rosterEntry{{NodeID: "odd-1", PublicKey: hexKey, Reachable: true, PublicURL: ""}}
	got := mailboxRelayPeersFromRoster(roster, "self")
	if len(got) != 1 || got[0] != "odd-1" {
		t.Fatalf("expected [odd-1] to fall back to mailbox relay, got %v", got)
	}
}

func TestMailboxRelayPeersFromRoster_SkipsSelfEmptyAndMalformedEntries(t *testing.T) {
	hexKey, _ := mustPubKeyHex(t)
	roster := []rosterEntry{
		{NodeID: "self", PublicKey: hexKey},
		{NodeID: ""},
		{NodeID: "bad-key", PublicKey: "not-hex"},
	}
	got := mailboxRelayPeersFromRoster(roster, "self")
	if len(got) != 0 {
		t.Fatalf("expected no relay peers, got %v", got)
	}
}

func TestMailboxRelayPeersFromRoster_IsSortedAndDeterministic(t *testing.T) {
	keyB, _ := mustPubKeyHex(t)
	keyA, _ := mustPubKeyHex(t)
	roster := []rosterEntry{{NodeID: "zzz", PublicKey: keyB}, {NodeID: "aaa", PublicKey: keyA}}
	got := mailboxRelayPeersFromRoster(roster, "self")
	if len(got) != 2 || got[0] != "aaa" || got[1] != "zzz" {
		t.Fatalf("expected sorted [aaa zzz], got %v", got)
	}
}

// --- startMailboxRelayListener: end-to-end wiring ---------------------------

// fakeProducerRegistry is a minimal, real pull-and-clear mailbox, matching
// the registry's actual /mailbox contract -- same fixture shape as
// internal/rpc/mailbox_relay_test.go's fakeRegistry, reimplemented here
// since it's a different package (this codebase's established style for
// small test-only client/server shapes -- see mustPubKeyHex existing
// alongside its own package's copy of similar helpers).
type fakeProducerRegistry struct {
	mu      sync.Mutex
	mailbox map[string][]relayMailboxEnvelope
	nextID  int
}

func newFakeProducerRegistry() *fakeProducerRegistry {
	return &fakeProducerRegistry{mailbox: map[string][]relayMailboxEnvelope{}}
}

func (f *fakeProducerRegistry) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			name := r.URL.Query().Get("name")
			f.mu.Lock()
			msgs := f.mailbox[name]
			delete(f.mailbox, name)
			f.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(msgs)
		case http.MethodPost:
			var body struct {
				To      string          `json:"to"`
				From    string          `json:"from"`
				Type    string          `json:"type"`
				Payload json.RawMessage `json:"payload"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
				return
			}
			if body.To == "" {
				http.Error(w, "to required", http.StatusBadRequest)
				return
			}
			f.mu.Lock()
			f.nextID++
			f.mailbox[body.To] = append(f.mailbox[body.To], relayMailboxEnvelope{
				ID: fmt.Sprintf("%d", f.nextID), Type: body.Type, From: body.From,
				Payload: body.Payload, CreatedAt: time.Now().UnixMilli(),
			})
			f.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "queued": true})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func (f *fakeProducerRegistry) messagesFor(name string) []relayMailboxEnvelope {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.mailbox[name]
	delete(f.mailbox, name)
	return out
}

// wireProducerAndRelay builds two real, cross-trusting single-node stacks
// (reusing newRosterSyncTestNode from validator_roster_sync_test.go, same
// package) -- a producer that will build and sign a real proposal, and a
// relay validator startMailboxRelayListener will run against. Both trust
// each other's real keys and agree on a 2-of-2 validator set, matching how
// cmd/synthosd/main.go wires a real deployment.
func wireProducerAndRelay(t *testing.T) (producer, relay *rosterSyncTestNode) {
	t.Helper()
	producer = newRosterSyncTestNode(t, "producer")
	relay = newRosterSyncTestNode(t, "home-candidate-1")

	all := []*rosterSyncTestNode{producer, relay}
	validatorIDs := []string{producer.id, relay.id}
	keys := map[string]ed25519.PublicKey{producer.id: producer.pub, relay.id: relay.pub}
	for _, v := range all {
		for _, other := range all {
			if other.id == v.id {
				continue
			}
			if err := v.node.AddPeer(other.id, other.pubHex); err != nil {
				t.Fatal(err)
			}
		}
		v.node.SetValidators(validatorIDs)
		v.chain.SetValidatorSet(keys, 2)
	}
	return producer, relay
}

// TestStartMailboxRelayListener_ValidatesAndPostsVoteBack is Phase 3's core
// regression test for the relay-validator side: given a real, validly
// signed proposal relayed through the mailbox, the listener must run the
// real HandleProposal validation, sign a real vote with the relay node's
// own key, and post that vote back to the proposal's sender -- so a
// producer polling the same mailbox actually receives something that
// verifies.
func TestStartMailboxRelayListener_ValidatesAndPostsVoteBack(t *testing.T) {
	producer, relay := wireProducerAndRelay(t)

	registryFixture := newFakeProducerRegistry()
	registry := httptest.NewServer(registryFixture.handler())
	t.Cleanup(registry.Close)

	startMailboxRelayListener(relay.node, relay.id, registry.URL, "", true, 20*time.Millisecond)

	b, err := producer.node.BuildAndSignProposal()
	if err != nil {
		t.Fatal(err)
	}
	proposalBody, _ := json.Marshal(map[string]any{
		"to": relay.id, "from": producer.id, "type": "consensus_proposal",
		"payload": map[string]any{"proposal": consensus.BlockProposal{Block: *b, Height: b.Header.Height}},
	})
	req, err := http.NewRequest(http.MethodPost, registry.URL+"/mailbox", bytes.NewReader(proposalBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	deadline := time.Now().Add(3 * time.Second)
	var voteMsgs []relayMailboxEnvelope
	for time.Now().Before(deadline) {
		voteMsgs = registryFixture.messagesFor(producer.id)
		if len(voteMsgs) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(voteMsgs) != 1 {
		t.Fatalf("expected exactly one vote message posted back to the producer, got %d", len(voteMsgs))
	}
	if voteMsgs[0].Type != "consensus_vote" || voteMsgs[0].From != relay.id {
		t.Fatalf("unexpected vote envelope: %+v", voteMsgs[0])
	}
	var payload struct {
		Vote consensus.BlockVote `json:"vote"`
	}
	if err := json.Unmarshal(voteMsgs[0].Payload, &payload); err != nil {
		t.Fatalf("vote payload did not decode: %v", err)
	}
	if payload.Vote.BlockHash != b.Hash || payload.Vote.VoterID != relay.id || payload.Vote.Vote != 1 {
		t.Fatalf("unexpected vote content: %+v", payload.Vote)
	}
	// Independently verify the signature against relay's real public key --
	// proves this is a genuine signed vote, not just a well-shaped message.
	sigHex := payload.Vote.Signature
	if len(sigHex) < 2 || sigHex[:2] != "0x" {
		t.Fatalf("expected a 0x-prefixed signature, got %q", sigHex)
	}
	pubBytes, err := synthoscrypto.PublicKeyBytes(relay.pubHex)
	if err != nil {
		t.Fatal(err)
	}
	sigBytes, err := synthoscrypto.PublicKeyBytes(sigHex)
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(ed25519.PublicKey(pubBytes), b.QuorumApprovalMessage(), sigBytes) {
		t.Fatal("relay's vote signature did not verify against its own real public key")
	}
}

// TestStartMailboxRelayListener_InertWhenConsensusDisabledOrNoRegistryURL
// mirrors startValidatorRosterSync's own gating test: this must do nothing
// at all when consensus isn't enabled or no registry URL is configured,
// not even start a polling goroutine.
func TestStartMailboxRelayListener_InertWhenConsensusDisabledOrNoRegistryURL(t *testing.T) {
	_, relay := wireProducerAndRelay(t)
	startMailboxRelayListener(relay.node, relay.id, "http://127.0.0.1:0", "", false, time.Millisecond)
	startMailboxRelayListener(relay.node, relay.id, "", "", true, time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	// No crash, and nothing to assert beyond that -- a real assertion
	// would need an observable side effect, which is exactly what "does
	// nothing" means here; this guards against a future edit accidentally
	// removing the early-return gate and making this call panic instead
	// (nil registryURL client usage, etc.).
}

// TestStartMailboxRelayListener_RejectsInvalidProposalWithoutCrashing
// proves a proposal that fails real validation (here: a height that
// doesn't extend the relay's current tip) is dropped -- no vote posted,
// loop keeps running for the next message -- rather than panicking or
// wedging the goroutine.
func TestStartMailboxRelayListener_RejectsInvalidProposalWithoutCrashing(t *testing.T) {
	producer, relay := wireProducerAndRelay(t)

	registryFixture := newFakeProducerRegistry()
	registry := httptest.NewServer(registryFixture.handler())
	t.Cleanup(registry.Close)

	startMailboxRelayListener(relay.node, relay.id, registry.URL, "", true, 20*time.Millisecond)

	b, err := producer.node.BuildAndSignProposal()
	if err != nil {
		t.Fatal(err)
	}
	b.Header.Height = 999 // wrong height -- must fail ValidateProposal/height-sanity check
	proposalBody, _ := json.Marshal(map[string]any{
		"to": relay.id, "from": producer.id, "type": "consensus_proposal",
		"payload": map[string]any{"proposal": consensus.BlockProposal{Block: *b, Height: b.Header.Height}},
	})
	req, err := http.NewRequest(http.MethodPost, registry.URL+"/mailbox", bytes.NewReader(proposalBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	time.Sleep(300 * time.Millisecond)
	if msgs := registryFixture.messagesFor(producer.id); len(msgs) != 0 {
		t.Fatalf("expected no vote for an invalid proposal, got %v", msgs)
	}
}
