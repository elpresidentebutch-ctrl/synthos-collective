package rpc

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"synthos-collective/internal/chain"
	"synthos-collective/internal/consensus"
	synthoscrypto "synthos-collective/internal/crypto"
)

// rotationFailoverTrio mirrors wireThreeValidators, but deliberately does
// NOT wire producerA's gossip-push path to follower -- this is what lets
// this test simulate "producerA gathered a real quorum vote but then
// crashed/partitioned before ever broadcasting the finalized block",
// exactly the scenario SetProducerRotationLockTimeout's doc comment
// describes: follower voted for producerA's candidate, but its own chain
// height never advances, so nothing here is faking the hazard -- it's the
// real HTTP consensus path, just stopped short of the broadcast step A
// never got to perform.
func rotationFailoverTrio(t *testing.T, lockTimeout time.Duration) (producerA, producerB, follower *consensusTestValidator) {
	t.Helper()
	genesis := chain.Genesis{
		ChainID:   "test-chain",
		TxChainID: 20260702,
		Alloc: map[chain.Address]uint64{
			"0x825fd94aa826da6ce0b4e57487418b72aea09f5e": 100,
		},
	}
	producerA = newConsensusTestValidator(t, "synthos-validator-12", genesis)
	producerB = newConsensusTestValidator(t, "synthos-validator-13", genesis)
	follower = newConsensusTestValidator(t, "synthos-render-validator-1", genesis)

	all := []*consensusTestValidator{producerA, producerB, follower}
	validatorIDs := []string{producerA.id, producerB.id, follower.id}
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
		v.node.SetValidators(validatorIDs) // Engine.RequiredForFinality() == 2
		v.chain.SetValidatorSet(valKeys, 2)
	}

	// The safety-critical lock: only follower needs it for this test (it's
	// the one being asked to vote for two different producers' competing
	// candidates), but a real deployment sets it on every validator -- see
	// this test's package-level doc references to SetProducerRotationLockTimeout.
	follower.node.SetProducerRotationLockTimeout(lockTimeout)

	follower.http = httptest.NewServer(follower.server.Handler())
	t.Cleanup(func() { follower.http.Close() })

	// Both producers can reach follower and only follower -- each round's
	// quorum is exactly "self plus follower" (2 of 3), matching the doc
	// comment's scenario of a producer reaching quorum via itself and ONE
	// other validator.
	for _, p := range []*consensusTestValidator{producerA, producerB} {
		p.server.SetConsensusPeerURLs([]string{follower.http.URL})
		p.server.ConsensusPeerTokens = map[string]string{follower.http.URL: follower.server.ConsensusToken}
	}
	// producerA is deliberately NOT given SetPeerURLs: it never broadcasts
	// its finalized block, simulating a crash/partition right after
	// reaching quorum. producerB IS wired normally, so a genuine failover
	// round can be observed reaching follower for real.
	producerB.server.SetPeerURLs([]string{follower.http.URL})

	return producerA, producerB, follower
}

// TestRotationFailover_LockPreventsCompetingProducerFromReachingQuorum is
// the fork-prevention property end-to-end, through the real HTTP consensus
// path (not calling node.Node.HandleProposal directly, unlike
// internal/node/rotation_lock_test.go): once follower has voted for
// producerA's candidate at a height, producerB's round for a DIFFERENT
// candidate at that same height must fail to reach quorum while the lock
// is held -- because follower's vote is the only second signature either
// producer can ever get, and the lock makes follower refuse it.
func TestRotationFailover_LockPreventsCompetingProducerFromReachingQuorum(t *testing.T) {
	producerA, producerB, follower := rotationFailoverTrio(t, time.Hour) // long: must not expire mid-test

	// producerA's round: reaches real quorum (self + follower) and
	// finalizes on producerA's OWN chain, but -- since producerA has no
	// SetPeerURLs configured here -- never broadcasts it anywhere.
	hashA, finalizedA, err := producerA.server.ProposeBlockWithConsensus(2 * time.Second)
	if err != nil {
		t.Fatalf("producerA round: %v", err)
	}
	if !finalizedA {
		t.Fatalf("expected producerA to reach real quorum (self + follower), hash=%s", hashA)
	}
	if producerA.chain.Height() != 1 {
		t.Fatalf("producerA height = %d, want 1", producerA.chain.Height())
	}
	if follower.chain.Height() != 0 {
		t.Fatalf("follower height = %d, want 0 -- follower voted but was never sent the finalized block (simulated crash before broadcast)", follower.chain.Height())
	}

	// producerB's round for the SAME height: without the lock this would
	// also reach quorum via follower's vote, for a genuinely different
	// block -- a real fork the moment producerA's block ever resurfaces.
	// With the lock, follower must refuse to vote for producerB, so this
	// round cannot reach quorum.
	hashB, finalizedB, err := producerB.server.ProposeBlockWithConsensus(2 * time.Second)
	if err != nil {
		t.Fatalf("producerB round: %v", err)
	}
	if hashB == hashA {
		t.Fatal("test setup bug: expected producerB's candidate to differ from producerA's")
	}
	if finalizedB {
		t.Fatalf("producerB's round finalized while the lock should have blocked follower's vote -- a real fork just occurred (hashA=%s hashB=%s)", hashA, hashB)
	}
	if producerB.chain.Height() != 0 {
		t.Fatalf("producerB height = %d, want 0 (its round must not have finalized locally either, since quorum was never reached)", producerB.chain.Height())
	}
}

// TestRotationFailover_SucceedsAfterLockTimeout proves the failover this
// whole feature exists to deliver: once the lock's timeout has genuinely
// elapsed with no further activity from producerA, producerB's round for
// the same height must succeed -- reach real 2-of-3 quorum, finalize on
// producerB's own chain, AND actually propagate to follower via the
// ordinary broadcast path, exactly like a real recovered network.
func TestRotationFailover_SucceedsAfterLockTimeout(t *testing.T) {
	producerA, producerB, follower := rotationFailoverTrio(t, 30*time.Millisecond) // short: test must not take long

	hashA, finalizedA, err := producerA.server.ProposeBlockWithConsensus(2 * time.Second)
	if err != nil {
		t.Fatalf("producerA round: %v", err)
	}
	if !finalizedA {
		t.Fatalf("expected producerA to reach real quorum, hash=%s", hashA)
	}

	time.Sleep(60 * time.Millisecond) // past the lock timeout, with no further activity from producerA

	hashB, finalizedB, err := producerB.server.ProposeBlockWithConsensus(2 * time.Second)
	if err != nil {
		t.Fatalf("producerB round: %v", err)
	}
	if !finalizedB {
		t.Fatalf("expected producerB's round to succeed once the lock timed out (genuine failover), hash=%s", hashB)
	}
	if producerB.chain.Height() != 1 {
		t.Fatalf("producerB height = %d, want 1", producerB.chain.Height())
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && follower.chain.Height() != 1 {
		time.Sleep(10 * time.Millisecond)
	}
	if follower.chain.Height() != 1 {
		t.Fatalf("follower height = %d, want 1 -- must have independently accepted producerB's block via the ordinary broadcast path after failover", follower.chain.Height())
	}
	tip := follower.chain.Tip()
	if tip == nil || tip.Hash != hashB {
		t.Fatalf("follower tip = %v, want producerB's block %s -- follower must be on the failed-over producer's chain, not stuck on producerA's abandoned one", tip, hashB)
	}
}

// TestRotationFailover_DirectHTTPRejectionMentionsLock is a narrower,
// faster check that the real /consensus/propose HTTP path (not just
// ProposeBlockWithConsensus's higher-level retry/quorum logic) surfaces the
// lock rejection the way node.Node.checkAndUpdateVoteLock produces it --
// confirming Server.handleConsensusPropose's existing generic
// any-non-200-is-a-skipped-vote handling (see the internal/rpc doc note in
// this feature's design) needed no changes to carry this new error through.
func TestRotationFailover_DirectHTTPRejectionMentionsLock(t *testing.T) {
	producerA, producerB, follower := rotationFailoverTrio(t, time.Hour)

	proposalA, err := producerA.node.BuildAndSignProposal()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := follower.node.HandleProposal(proposalA); err != nil {
		t.Fatalf("follower's first vote (producerA's round) must succeed: %v", err)
	}

	proposalB, err := producerB.node.BuildAndSignProposal()
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"proposal": consensus.BlockProposal{Block: *proposalB, Height: proposalB.Header.Height},
	})
	req, err := http.NewRequest(http.MethodPost, follower.http.URL+"/consensus/propose", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Consensus-Token", follower.server.ConsensusToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (competing proposer must be rejected while the lock is held)", resp.StatusCode)
	}
	var respBody bytes.Buffer
	_, _ = respBody.ReadFrom(resp.Body)
	if !strings.Contains(respBody.String(), "locked") {
		t.Fatalf("expected the rejection body to mention the lock, got: %s", respBody.String())
	}
}
