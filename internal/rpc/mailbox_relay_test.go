package rpc

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

	"synthos-collective/internal/chain"
	"synthos-collective/internal/consensus"
	synthoscrypto "synthos-collective/internal/crypto"
)

// --- sanitizeMailboxRelayPeers: pure-function unit tests -------------------

func TestSanitizeMailboxRelayPeers_TrimsDedupsAndDropsEmpty(t *testing.T) {
	got := sanitizeMailboxRelayPeers([]string{" home-1 ", "home-1", "", "  ", "home-2"})
	want := []string{"home-1", "home-2"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestMailboxRelayPeersSnapshot_ReturnsAnIndependentCopy(t *testing.T) {
	s := &Server{}
	s.SetMailboxRelayPeers([]string{"home-1"})
	snap := s.MailboxRelayPeersSnapshot()
	s.SetMailboxRelayPeers([]string{"home-1", "home-2"})
	if len(snap) != 1 || snap[0] != "home-1" {
		t.Fatalf("earlier snapshot was mutated by a later SetMailboxRelayPeers call: %v", snap)
	}
	if fresh := s.MailboxRelayPeersSnapshot(); len(fresh) != 2 {
		t.Fatalf("expected the new snapshot to reflect the update, got %v", fresh)
	}
}

// --- collectMailboxVotes: no-op gating --------------------------------------

func TestCollectMailboxVotes_NoOpWithNoRelayPeersOrNoRegistryURL(t *testing.T) {
	s := &Server{RegistryURL: "http://127.0.0.1:0"}
	if got := s.collectMailboxVotes(&chain.Block{}, nil, time.Second); got != nil {
		t.Fatalf("expected nil with no relay peers, got %v", got)
	}
	s2 := &Server{RegistryURL: ""}
	if got := s2.collectMailboxVotes(&chain.Block{}, []string{"home-1"}, time.Second); got != nil {
		t.Fatalf("expected nil with no registry URL, got %v", got)
	}
}

// --- fake registry: a minimal, real pull-and-clear mailbox -----------------

// fakeMailboxEnvelope mirrors the registry's own mailboxMessage JSON shape,
// same as mailboxEnvelope in mailbox_relay.go -- kept separate since this
// one is only ever used to encode a canned response in this test file.
type fakeMailboxEnvelope struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	From      string `json:"from"`
	Payload   any    `json:"payload"`
	CreatedAt int64  `json:"created_at"`
}

// fakeRegistry is a small, thread-safe stand-in for cmd/cloudless-registry's
// real /mailbox endpoint, implementing the identical contract: POST queues
// a message for `to`, GET pulls and clears everything queued for `name`.
type fakeRegistry struct {
	mu      sync.Mutex
	mailbox map[string][]fakeMailboxEnvelope
	nextID  int
}

func newFakeRegistry() *fakeRegistry {
	return &fakeRegistry{mailbox: map[string][]fakeMailboxEnvelope{}}
}

func (f *fakeRegistry) handler() http.HandlerFunc {
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
				To      string `json:"to"`
				From    string `json:"from"`
				Type    string `json:"type"`
				Payload any    `json:"payload"`
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
			msg := fakeMailboxEnvelope{
				ID:        fmt.Sprintf("%d", f.nextID),
				Type:      body.Type,
				From:      body.From,
				Payload:   body.Payload,
				CreatedAt: time.Now().UnixMilli(),
			}
			f.mailbox[body.To] = append(f.mailbox[body.To], msg)
			f.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "queued": true})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

// runFakeRelayListener stands in for cmd/synthosd's startMailboxRelayListener
// -- deliberately reimplemented here rather than imported (it's unexported,
// in a different package) -- but drives the exact same real
// node.Node.HandleProposal call a real relay validator would, so this test
// still proves the real cryptographic validate-then-vote path works over a
// pull-and-clear mailbox, not just that some mock returns a canned vote.
func runFakeRelayListener(t *testing.T, v *consensusTestValidator, registryURL string, stop <-chan struct{}) {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(20 * time.Millisecond):
			}
			req, err := http.NewRequest(http.MethodGet, registryURL+"/mailbox?name="+v.id, nil)
			if err != nil {
				continue
			}
			resp, err := client.Do(req)
			if err != nil {
				continue
			}
			var msgs []struct {
				Type    string          `json:"type"`
				From    string          `json:"from"`
				Payload json.RawMessage `json:"payload"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&msgs)
			resp.Body.Close()
			for _, m := range msgs {
				if m.Type != "consensus_proposal" {
					continue
				}
				var payload struct {
					Proposal consensus.BlockProposal `json:"proposal"`
				}
				if err := json.Unmarshal(m.Payload, &payload); err != nil {
					continue
				}
				b := payload.Proposal.Block
				vote, err := v.node.HandleProposal(&b)
				if err != nil {
					t.Logf("fake relay listener: rejected proposal: %v", err)
					continue
				}
				voteBody, _ := json.Marshal(map[string]any{
					"to": m.From, "from": v.id, "type": "consensus_vote",
					"payload": map[string]any{"vote": vote},
				})
				req2, err := http.NewRequest(http.MethodPost, registryURL+"/mailbox", bytes.NewReader(voteBody))
				if err != nil {
					continue
				}
				req2.Header.Set("Content-Type", "application/json")
				if resp2, err := client.Do(req2); err == nil {
					resp2.Body.Close()
				}
			}
		}
	}()
}

// wireTwoValidatorsOneUnreachable builds a real 2-node deployment where the
// second validator has no rpc.Server / httptest listener at all -- proving
// this test isn't accidentally exercising the direct-HTTP consensus path
// alongside the mailbox one. Quorum for 2 validators is 2 (unanimous), so
// reaching finality genuinely requires the relay validator's vote to
// actually arrive.
func wireTwoValidatorsOneUnreachable(t *testing.T) (producer, relay *consensusTestValidator) {
	t.Helper()
	genesis := chain.Genesis{
		ChainID:   "test-chain",
		TxChainID: 20260924,
		Alloc: map[chain.Address]uint64{
			"0x825fd94aa826da6ce0b4e57487418b72aea09f5e": 100,
		},
	}
	producer = newConsensusTestValidator(t, "producer", genesis)
	relay = newConsensusTestValidator(t, "home-candidate-1", genesis)

	all := []*consensusTestValidator{producer, relay}
	validatorIDs := []string{producer.id, relay.id}
	valKeys := map[string]ed25519.PublicKey{}
	for _, v := range all {
		pub, err := synthoscrypto.PublicKeyBytes(v.agent.Identity.PublicKey)
		if err != nil {
			t.Fatal(err)
		}
		valKeys[v.id] = ed25519.PublicKey(pub)
	}
	for _, v := range all {
		for _, other := range all {
			if other.id == v.id {
				continue
			}
			if err := v.node.AddPeer(other.id, other.agent.Identity.PublicKey); err != nil {
				t.Fatal(err)
			}
		}
		v.node.SetValidators(validatorIDs)  // Engine.RequiredForFinality() == 2
		v.chain.SetValidatorSet(valKeys, 2) // Chain independently requires 2 real signatures
	}
	return producer, relay
}

// --- end-to-end: the real mailbox relay round trip --------------------------

// TestProposeBlockWithConsensus_ReachesQuorumViaMailboxRelay is Phase 3's
// core regression test: a validator with no directly-callable URL (the
// home-PC case) must still be able to cast a real, cryptographically valid
// vote that counts toward quorum, routed entirely through the registry's
// generic mailbox rather than a direct HTTPS call. Quorum for these 2
// validators is 2 (unanimous), so finalization here is only possible if
// the relay validator's vote genuinely arrived and verified.
func TestProposeBlockWithConsensus_ReachesQuorumViaMailboxRelay(t *testing.T) {
	producer, relay := wireTwoValidatorsOneUnreachable(t)

	registryFixture := newFakeRegistry()
	registry := httptest.NewServer(registryFixture.handler())
	t.Cleanup(registry.Close)

	producer.server.RegistryURL = registry.URL
	producer.server.SetMailboxRelayPeers([]string{relay.id})

	stop := make(chan struct{})
	runFakeRelayListener(t, relay, registry.URL, stop)
	t.Cleanup(func() { close(stop) })

	hash, finalized, err := producer.server.ProposeBlockWithConsensus(3 * time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !finalized {
		t.Fatal("expected quorum to be reached via the mailbox relay, but the round did not finalize")
	}
	if producer.chain.Tip() == nil || producer.chain.Tip().Hash != hash {
		t.Fatalf("expected the chain tip to advance to the finalized block %s", hash)
	}
	// The relay validator independently ran the real HandleProposal check
	// (via runFakeRelayListener), not a canned response -- confirm its own
	// Consensus engine genuinely recorded this exact block as the
	// candidate for its height (RecordReceivedProposal), the same
	// bookkeeping a directly-reachable validator's handleConsensusPropose
	// path leaves behind. (A validator that answers a proposal doesn't
	// also feed its own vote back into its own local tally -- only the
	// producer, which collects everyone's votes including its own
	// SelfVote, ends up with a complete count -- so finalized==true above,
	// from the producer's side, is what actually proves the vote arrived
	// and verified; this just confirms the relay side did real work to
	// produce it, not a shortcut.)
	if _, ok := relay.node.Consensus.Proposal(hash); !ok {
		t.Fatal("expected the relay validator to have independently recorded the proposal it voted on")
	}
}

// TestCollectMailboxVotes_DiscardsForgedVote proves the mailbox path gets
// no more trust than the direct-HTTP one: a message claiming to be a valid
// vote from the relay peer, but with a bogus signature, must be discarded
// by verifyPeerVote exactly like a forged direct-HTTP response would be --
// never counted toward quorum. This is the same defense-in-depth property
// requestConsensusVote's own path already has; the mailbox is an
// unauthenticated transport (see docs/VALIDATOR_ONBOARDING.md's Phase 3),
// so this check is what actually keeps it safe to use.
func TestCollectMailboxVotes_DiscardsForgedVote(t *testing.T) {
	producer, relay := wireTwoValidatorsOneUnreachable(t)

	registryFixture := newFakeRegistry()
	registry := httptest.NewServer(registryFixture.handler())
	t.Cleanup(registry.Close)

	producer.server.RegistryURL = registry.URL

	b, err := producer.node.BuildAndSignProposal()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := producer.node.SelfVote(b); err != nil {
		t.Fatal(err)
	}

	// A forged reply: claims to be relay's vote, but the signature is just
	// made up -- never went through relay's real private key at all.
	forged := consensus.BlockVote{
		BlockHash: b.Hash,
		Height:    b.Header.Height,
		VoterID:   relay.id,
		Vote:      1,
		Signature: "0x" + fmt.Sprintf("%0128d", 0),
	}
	body, _ := json.Marshal(map[string]any{
		"to": producer.id, "from": relay.id, "type": "consensus_vote",
		"payload": map[string]any{"vote": forged},
	})
	req, err := http.NewRequest(http.MethodPost, registry.URL+"/mailbox", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	votes := producer.server.collectMailboxVotes(b, []string{relay.id}, 500*time.Millisecond)
	if len(votes) != 0 {
		t.Fatalf("expected the forged vote to be discarded, got %v", votes)
	}
}

// TestCollectMailboxVotes_UnansweredRelayPeerReturnsWithinTimeout proves
// the fail-soft contract: a relay peer that never answers must not hang
// the round past its configured timeout, and must simply be absent from
// the result -- matching collectConsensusVotes' handling of an unreachable
// direct-HTTP peer.
func TestCollectMailboxVotes_UnansweredRelayPeerReturnsWithinTimeout(t *testing.T) {
	producer, relay := wireTwoValidatorsOneUnreachable(t)
	_ = relay

	registryFixture := newFakeRegistry()
	registry := httptest.NewServer(registryFixture.handler())
	t.Cleanup(registry.Close)
	producer.server.RegistryURL = registry.URL

	b, err := producer.node.BuildAndSignProposal()
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	votes := producer.server.collectMailboxVotes(b, []string{"nobody-home"}, 300*time.Millisecond)
	elapsed := time.Since(start)
	if len(votes) != 0 {
		t.Fatalf("expected no votes from an unanswered relay peer, got %v", votes)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("expected collectMailboxVotes to return promptly after its timeout, took %s", elapsed)
	}
}
