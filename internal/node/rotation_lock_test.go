package node_test

import (
	"strings"
	"testing"
	"time"

	"synthos-collective/internal/agent"
	"synthos-collective/internal/chain"
	"synthos-collective/internal/consensus"
	synthoscrypto "synthos-collective/internal/crypto"
	"synthos-collective/internal/node"
)

// setupRotationTrio builds three nodes sharing identical genesis state and
// mutually aware of each other as peers/validators: "follower" (the node
// whose vote-lock behavior these tests inspect) and two independent
// "producer" nodes (producerA, producerB) that each build and sign their
// own real proposals for follower.HandleProposal to receive, mirroring
// what two different validators taking turns under rotation would each
// send over /consensus/propose.
func setupRotationTrio(t *testing.T) (follower, producerA, producerB *node.Node) {
	t.Helper()

	mk := func(id, hw string) *node.Node {
		keys, err := synthoscrypto.NewKeyPair()
		if err != nil {
			t.Fatal(err)
		}
		a := agent.NewAgent(id, "", "", hw, 0)
		if err := a.AttachKeys(keys); err != nil {
			t.Fatal(err)
		}
		genesis := chain.Genesis{
			ChainID:   "test-chain",
			TxChainID: 20260702,
			Alloc:     map[chain.Address]uint64{"0x825fd94aa826da6ce0b4e57487418b72aea09f5e": 100},
		}
		c, err := chain.NewChain(genesis)
		if err != nil {
			t.Fatal(err)
		}
		return node.NewNode(a, c, consensus.NewEngine(3), nil)
	}

	follower = mk("follower", "hw-follower")
	producerA = mk("producer-a", "hw-a")
	producerB = mk("producer-b", "hw-b")

	all := []*node.Node{follower, producerA, producerB}
	for _, self := range all {
		for _, other := range all {
			if self == other {
				continue
			}
			if err := self.AddPeer(other.Agent.Identity.AgentID, other.Agent.Identity.PublicKey); err != nil {
				t.Fatal(err)
			}
		}
	}

	validators := []string{"follower", "producer-a", "producer-b"}
	for _, n := range all {
		n.SetValidators(validators)
	}

	return follower, producerA, producerB
}

// TestHandleProposal_LocksAgainstADifferentProposerAtSameHeight is the
// core safety property: once follower has voted for producer-a's
// candidate at a height, it must refuse to vote for producer-b's
// DIFFERENT candidate at that same height while the lock is held -- this
// is what actually prevents two different producers from each
// independently gathering enough votes to finalize two different blocks
// at one height (a real fork), not just the round-scheduling logic on the
// sending side.
func TestHandleProposal_LocksAgainstADifferentProposerAtSameHeight(t *testing.T) {
	follower, producerA, producerB := setupRotationTrio(t)
	follower.SetProducerRotationLockTimeout(time.Hour) // long: must not expire during this test

	blockA, err := producerA.BuildAndSignProposal()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := follower.HandleProposal(blockA); err != nil {
		t.Fatalf("first vote (producer-a's round) must succeed: %v", err)
	}

	blockB, err := producerB.BuildAndSignProposal()
	if err != nil {
		t.Fatal(err)
	}
	if blockB.Hash == blockA.Hash {
		t.Fatal("test setup bug: expected producer-b's candidate to differ from producer-a's")
	}
	_, err = follower.HandleProposal(blockB)
	if err == nil {
		t.Fatal("expected the locked follower to refuse voting for a different proposer's competing candidate at the same height")
	}
	if !strings.Contains(err.Error(), "locked") {
		t.Fatalf("expected a lock-related error, got: %v", err)
	}
}

// TestHandleProposal_UnlocksAfterTimeoutAndAllowsFailover proves the other
// half: this is meant to allow real failover, not wedge a height forever
// if producer-a genuinely never comes back. Once the lock's timeout has
// passed with no further activity from producer-a, follower must accept
// producer-b's candidate.
func TestHandleProposal_UnlocksAfterTimeoutAndAllowsFailover(t *testing.T) {
	follower, producerA, producerB := setupRotationTrio(t)
	follower.SetProducerRotationLockTimeout(20 * time.Millisecond) // short: test must not take long

	blockA, err := producerA.BuildAndSignProposal()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := follower.HandleProposal(blockA); err != nil {
		t.Fatalf("first vote (producer-a's round) must succeed: %v", err)
	}

	time.Sleep(30 * time.Millisecond) // past the lock timeout

	blockB, err := producerB.BuildAndSignProposal()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := follower.HandleProposal(blockB); err != nil {
		t.Fatalf("expected the lock to have expired, allowing failover to producer-b, got: %v", err)
	}
}

// TestHandleProposal_SameProposerRetryNeverLocked proves the lock never
// blocks the one case that must keep working exactly as it does today:
// the SAME producer abandoning its own earlier candidate for a fresh one
// within its own round (what ProposeBlockWithConsensus already does on
// every retry that doesn't reach quorum in time) -- regardless of how
// long (or short) the lock timeout is, and with no need to wait for it.
func TestHandleProposal_SameProposerRetryNeverLocked(t *testing.T) {
	follower, producerA, _ := setupRotationTrio(t)
	follower.SetProducerRotationLockTimeout(time.Hour) // long: a retry must not need to wait for this at all

	for i := 0; i < 3; i++ {
		b, err := producerA.BuildAndSignProposal()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := follower.HandleProposal(b); err != nil {
			t.Fatalf("retry %d from the SAME proposer must never be blocked by the lock, got: %v", i, err)
		}
	}
}

// TestHandleProposal_LockDisabledByDefault confirms the lock is strictly
// opt-in: a follower that never calls SetProducerRotationLockTimeout must
// accept a different proposer's competing candidate exactly as every
// existing single-producer-deployment test already relies on.
func TestHandleProposal_LockDisabledByDefault(t *testing.T) {
	follower, producerA, producerB := setupRotationTrio(t)
	// No SetProducerRotationLockTimeout call: locking stays off.

	blockA, err := producerA.BuildAndSignProposal()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := follower.HandleProposal(blockA); err != nil {
		t.Fatalf("first vote must succeed: %v", err)
	}

	blockB, err := producerB.BuildAndSignProposal()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := follower.HandleProposal(blockB); err != nil {
		t.Fatalf("with locking disabled (the default), a second proposer's candidate must still be accepted exactly as today: %v", err)
	}
}
