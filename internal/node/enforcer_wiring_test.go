package node_test

import (
	"testing"
	"time"

	"synthos-collective/internal/agent"
	"synthos-collective/internal/chain"
	"synthos-collective/internal/consensus"
	synthoscrypto "synthos-collective/internal/crypto"
	"synthos-collective/internal/network"
	"synthos-collective/internal/node"
)

// setupEnforcerPair builds two nodes sharing a MemoryTransport bus: a
// "watcher" (the node whose Slashing tracker we inspect) and a "bad"
// validator whose signed-but-corrupted proposal the watcher must detect.
// Both know about each other as peers and validators, mirroring a real
// two-validator network.
func setupEnforcerPair(t *testing.T) (watcher *node.Node, bad *node.Node, badAgentID string) {
	t.Helper()

	watcherKeys, err := synthoscrypto.NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	badKeys, err := synthoscrypto.NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	watcherAgent := agent.NewAgent("watcher", "", "", "hw-watcher", 0)
	if err := watcherAgent.AttachKeys(watcherKeys); err != nil {
		t.Fatal(err)
	}
	badAgent := agent.NewAgent("bad-validator", "", "", "hw-bad", 0)
	if err := badAgent.AttachKeys(badKeys); err != nil {
		t.Fatal(err)
	}

	// Fund the bad validator's own address so a slash has a real, visible
	// balance to reduce -- an unfunded address would already read 0 before
	// and after, which wouldn't actually prove the penalty landed.
	badAddr := chain.AddressFromPublicKey(badKeys.Public)
	genesis := chain.Genesis{
		ChainID:   "test-chain",
		TxChainID: 20260702,
		Alloc: map[chain.Address]uint64{
			"0x825fd94aa826da6ce0b4e57487418b72aea09f5e": 100,
			badAddr: 300,
		},
	}
	watcherChain, err := chain.NewChain(genesis)
	if err != nil {
		t.Fatal(err)
	}
	badChain, err := chain.NewChain(genesis)
	if err != nil {
		t.Fatal(err)
	}

	bus := network.NewMemoryTransport()
	watcherTransport := bus.NodeTransport(watcherAgent.Identity.AgentID)
	badTransport := bus.NodeTransport(badAgent.Identity.AgentID)
	watcherAgent.AttachTransport(watcherTransport)
	badAgent.AttachTransport(badTransport)

	watcherNode := node.NewNode(watcherAgent, watcherChain, consensus.NewEngine(2), watcherTransport)
	badNode := node.NewNode(badAgent, badChain, consensus.NewEngine(2), badTransport)

	// Each side must know the other's real public key to verify envelopes,
	// exactly like real peer discovery would populate n.Peers.
	if err := watcherNode.AddPeer(badAgent.Identity.AgentID, badAgent.Identity.PublicKey); err != nil {
		t.Fatal(err)
	}
	if err := badNode.AddPeer(watcherAgent.Identity.AgentID, watcherAgent.Identity.PublicKey); err != nil {
		t.Fatal(err)
	}

	validators := []string{watcherAgent.Identity.AgentID, badAgent.Identity.AgentID}
	watcherNode.SetValidators(validators)
	badNode.SetValidators(validators)

	if err := watcherNode.Start(); err != nil {
		t.Fatal(err)
	}
	if err := badNode.Start(); err != nil {
		t.Fatal(err)
	}

	return watcherNode, badNode, badAgent.Identity.AgentID
}

// TestInvalidBlockProposalGetsSlashed is the core safety property for the
// Enforcer wiring: a validator that broadcasts a validly-signed but
// genuinely invalid block (correct envelope signature, wrong tx-merkle
// root) must be automatically detected and slashed by a peer that
// receives it -- this is the previously-dead RecordInvalidBlock path.
//
// This deliberately corrupts Header.TxMerkleRoot rather than
// Header.StateRoot: a tx-merkle-root mismatch is a structural fact about
// the block's own bytes (TxMerkleRoot(b.Tx) is deterministic from b.Tx
// alone), true for any validator regardless of their own chain state, so
// any honest validator who receives this exact block independently
// reaches the same "invalid" verdict -- real, provable misbehavior, safe
// to slash for. A state-root mismatch is NOT that (see
// TestStateRootMismatchIsRejectedButNotSlashed below and
// chain.ErrStateRootMismatch's doc comment) -- it's checked separately by
// the receiving node's ValidateProposal and deliberately excluded from
// RecordInvalidBlock's real balance penalty.
func TestInvalidBlockProposalGetsSlashed(t *testing.T) {
	watcherNode, badNode, badAgentID := setupEnforcerPair(t)

	b, err := badNode.Chain.BuildBlock(badAgentID, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	// Corrupt the block so it's self-consistent (hash matches its own
	// header) but its declared tx-merkle root doesn't match what
	// TxMerkleRoot(b.Tx) actually computes from its (empty) tx list -- a
	// real, non-trivial, objectively-invalid block, not just a mangled
	// hash, and one every honest validator would reject identically
	// regardless of their own chain state.
	b.Header.TxMerkleRoot = "0xdeadbeef-tampered-tx-merkle-root"
	newHash, err := b.CalculateHash()
	if err != nil {
		t.Fatal(err)
	}
	b.Hash = newHash

	env, err := badNode.Agent.BuildEnvelope("block_proposal", "", consensus.TopicProposals, consensus.BlockProposal{
		Block:  *b,
		Height: b.Header.Height,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := badNode.Agent.SendEnvelope(env); err != nil {
		t.Fatal(err)
	}

	if got := watcherNode.Slashing.TotalSlashEvents(); got != 1 {
		t.Fatalf("expected 1 slash event recorded, got %d", got)
	}
	history := watcherNode.Slashing.HistoryFor(badAgentID)
	if len(history) != 1 {
		t.Fatalf("expected 1 slash event for %s, got %d", badAgentID, len(history))
	}
	if history[0].EventType != consensus.InvalidBlock {
		t.Fatalf("expected InvalidBlock event type, got %s", history[0].EventType)
	}
	if history[0].BlockHeight != b.Header.Height {
		t.Fatalf("expected block height %d recorded, got %d", b.Header.Height, history[0].BlockHeight)
	}

	if !watcherNode.Slashing.IsJailed(badAgentID) {
		t.Fatal("expected slashed validator to be jailed")
	}

	// The penalty must actually land on the bad validator's real chain
	// balance in the watcher's own state -- not just live in the
	// tracker's internal bookkeeping. Started funded at 300; InvalidBlock
	// penalty is 250 (see the SlashingParams wired in node.NewNode).
	addr := chain.AddressFromPublicKey(mustPubKeyBytes(t, badNode))
	// ExecuteSlash runs asynchronously (go st.executeSlash(...)), so poll
	// briefly rather than racing it.
	var acc chain.Account
	deadline := time.Now().Add(2 * time.Second)
	for {
		acc = watcherNode.Chain.State.Get(addr)
		if acc.Balance == 50 || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if acc.Balance != 50 {
		t.Fatalf("expected slashed validator's balance to drop from 300 to 50 (penalty 250), got %d", acc.Balance)
	}
}

// TestValidBlockProposalIsNotSlashed guards against false positives: a
// normal, genuinely valid proposal must never trigger a slash.
func TestValidBlockProposalIsNotSlashed(t *testing.T) {
	watcherNode, badNode, _ := setupEnforcerPair(t)

	hash, err := badNode.ProposeBlockHash()
	if err != nil {
		t.Fatal(err)
	}
	if hash == "" {
		t.Fatal("expected a real proposed block hash")
	}
	if got := watcherNode.Slashing.TotalSlashEvents(); got != 0 {
		t.Fatalf("valid proposal must not be slashed, got %d events", got)
	}
}

// TestStateRootMismatchIsNotSlashed reproduces the live incident that
// motivated chain.ErrStateRootMismatch: a follower whose own chain state
// has already, for some unrelated reason, drifted from the network's
// (a transient race, a prior soft desync, still catching up mid a
// roster change -- the cause doesn't matter here) receives a completely
// honest, untampered proposal from a perfectly honest proposer. The
// proposal's declared StateRoot is correct relative to the PROPOSER's own
// state, but the watcher's own drifted baseline makes it independently
// recompute a different root -- a real state-root mismatch, but not
// evidence the proposer did anything wrong.
//
// Before this fix, HandleProposal treated this exactly like a genuinely
// tampered block and called RecordInvalidBlock unconditionally, which
// real-slashes the (honest) proposer's balance in the watcher's own local
// state only -- a phantom debit that permanently and unilaterally
// diverges the watcher's own state root further, guaranteeing every
// future block fails the identical check forever (nothing un-corrupts
// in-memory state on its own, and the corruption survives a restart too,
// since it gets persisted to disk by the next unrelated Store.Save, not
// only by a successful finalize). Live impact: validator-13's state root
// permanently diverged from validator-12's this way at height ~42042,
// which silently halted block production, since a permanently-diverged
// validator can never cast a valid vote on any future block again either.
func TestStateRootMismatchIsNotSlashed(t *testing.T) {
	watcherNode, badNode, badAgentID := setupEnforcerPair(t)

	// Simulate the watcher's own state having already, benignly diverged
	// from the network's, entirely independent of anything the proposer
	// does below.
	driftedAddr := chain.AddressFromPublicKey(mustPubKeyBytes(t, badNode))
	acc := watcherNode.Chain.State.Get(driftedAddr)
	acc.Balance += 1
	watcherNode.Chain.State.Set(driftedAddr, acc)
	stateRootBefore := watcherNode.Chain.State.Root()

	// badNode now proposes a completely ordinary, untampered block --
	// built the same way TestValidBlockProposalIsNotSlashed's does.
	b, err := badNode.Chain.BuildBlock(badAgentID, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	env, err := badNode.Agent.BuildEnvelope("block_proposal", "", consensus.TopicProposals, consensus.BlockProposal{
		Block:  *b,
		Height: b.Header.Height,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := badNode.Agent.SendEnvelope(env); err != nil {
		t.Fatal(err)
	}

	if got := watcherNode.Slashing.TotalSlashEvents(); got != 0 {
		t.Fatalf("a state-root mismatch caused by the WATCHER's own drifted state must not slash the honest proposer, got %d event(s)", got)
	}
	if watcherNode.Slashing.IsJailed(badAgentID) {
		t.Fatal("honest proposer must not be jailed over the watcher's own state drift")
	}
	// The watcher's state must be completely untouched by handling this
	// proposal (still rejected -- it never votes for it -- but not
	// further mutated by a phantom slash on top of the pre-existing
	// drift).
	if got := watcherNode.Chain.State.Root(); got != stateRootBefore {
		t.Fatalf("watcher's state root changed while merely evaluating a rejected proposal: before=%s after=%s", stateRootBefore, got)
	}
}

// TestStaleInvalidBlockIsNotSlashed guards the other safety property: a
// block that no longer extends the current tip (because the chain has
// already moved on) must be rejected by the height check before it ever
// reaches ValidateBlock/RecordInvalidBlock -- otherwise a formerly-valid,
// already-superseded block could wrongly slash an honest validator after
// the fact.
func TestStaleInvalidBlockIsNotSlashed(t *testing.T) {
	watcherNode, badNode, badAgentID := setupEnforcerPair(t)

	// Advance the watcher's chain past height 1 first, using a genuinely
	// valid, honestly-produced block from the watcher itself.
	if _, err := watcherNode.ProposeBlockHash(); err != nil {
		t.Fatal(err)
	}
	if watcherNode.Chain.Height() != 1 {
		t.Fatalf("expected watcher height 1 after its own proposal, got %d", watcherNode.Chain.Height())
	}

	// Now the "bad" validator broadcasts a corrupted block still claiming
	// height 1 (the tip has already moved to height 1, so the next valid
	// height is 2) -- this must be dropped on the height check, not
	// recorded as a slash.
	b, err := badNode.Chain.BuildBlock(badAgentID, "", 10) // badNode's own chain is still at height 0
	if err != nil {
		t.Fatal(err)
	}
	b.Header.StateRoot = "0xdeadbeef-tampered-state-root"
	newHash, err := b.CalculateHash()
	if err != nil {
		t.Fatal(err)
	}
	b.Hash = newHash

	env, err := badNode.Agent.BuildEnvelope("block_proposal", "", consensus.TopicProposals, consensus.BlockProposal{
		Block:  *b,
		Height: b.Header.Height,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := badNode.Agent.SendEnvelope(env); err != nil {
		t.Fatal(err)
	}

	if got := watcherNode.Slashing.TotalSlashEvents(); got != 0 {
		t.Fatalf("stale/off-frontier proposal must not be slashed, got %d events", got)
	}
}

func mustPubKeyBytes(t *testing.T, n *node.Node) []byte {
	t.Helper()
	b, err := synthoscrypto.PublicKeyBytes(n.Agent.Identity.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
