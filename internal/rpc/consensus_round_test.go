package rpc

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"synthos-collective/internal/agent"
	"synthos-collective/internal/chain"
	"synthos-collective/internal/consensus"
	synthoscrypto "synthos-collective/internal/crypto"
	"synthos-collective/internal/network"
	"synthos-collective/internal/node"
)

// consensusTestValidator bundles everything one simulated validator needs:
// its own chain, node, and RPC server, wired the same way
// cmd/synthosd/main.go wires a real deployment (SetValidators on the
// Engine, SetValidatorSet on the Chain with a real quorum, AddPeer for
// every other validator's real key).
type consensusTestValidator struct {
	id     string
	agent  *agent.Agent
	chain  *chain.Chain
	node   *node.Node
	server *Server
	http   *httptest.Server
}

// newConsensusTestValidator builds one validator's full stack sharing the
// given genesis. Callers must call AddPeer for every other validator and
// SetValidatorSet on chain before starting real rounds.
func newConsensusTestValidator(t *testing.T, id string, genesis chain.Genesis) *consensusTestValidator {
	t.Helper()
	keys, err := synthoscrypto.NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	a := agent.NewAgent(id, "", "", "hw-"+id, 0)
	if err := a.AttachKeys(keys); err != nil {
		t.Fatal(err)
	}
	ch, err := chain.NewChain(genesis)
	if err != nil {
		t.Fatal(err)
	}
	bus := network.NewMemoryTransport()
	transport := bus.NodeTransport(a.Identity.AgentID)
	a.AttachTransport(transport)

	n := node.NewNode(a, ch, consensus.NewEngine(3), transport)
	if err := n.Start(); err != nil {
		t.Fatal(err)
	}

	srv := NewServer(ch, nil, n)
	srv.ConsensusToken = "test-shared-secret"

	return &consensusTestValidator{id: id, agent: a, chain: ch, node: n, server: srv}
}

// wireThreeValidators builds a real 3-node deployment topology exactly like
// cmd/synthosd/main.go would for synthos-validator-12/-13/synthos-render-
// validator-1: each node knows the other two's real keys, all three agree
// on the same validator roster (so RequiredForFinality()==2), and Chain on
// every node independently enforces that same 2-of-3 quorum.
func wireThreeValidators(t *testing.T) (p, f1, f2 *consensusTestValidator) {
	t.Helper()
	genesis := chain.Genesis{
		ChainID:   "test-chain",
		TxChainID: 20260702,
		Alloc: map[chain.Address]uint64{
			"0x825fd94aa826da6ce0b4e57487418b72aea09f5e": 100,
		},
	}
	p = newConsensusTestValidator(t, "synthos-validator-12", genesis)
	f1 = newConsensusTestValidator(t, "synthos-validator-13", genesis)
	f2 = newConsensusTestValidator(t, "synthos-render-validator-1", genesis)

	all := []*consensusTestValidator{p, f1, f2}
	validatorIDs := []string{p.id, f1.id, f2.id}
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

	// Expose f1 and f2 as real HTTP servers so the producer (p) reaches
	// them exactly the way it would reach real Render services.
	f1.http = httptest.NewServer(f1.server.Handler())
	f2.http = httptest.NewServer(f2.server.Handler())
	t.Cleanup(func() {
		f1.http.Close()
		f2.http.Close()
	})

	p.server.SetConsensusPeerURLs([]string{f1.http.URL, f2.http.URL})
	// Also wire the ordinary gossip-push path so a genuinely finalized
	// block actually reaches followers, the same as production.
	p.server.SetPeerURLs([]string{f1.http.URL, f2.http.URL})

	return p, f1, f2
}

// TestConsensusRound_RequiresTwoOfThreeRealSignatures is the core safety
// property this whole feature exists for: a block must carry at least 2
// genuinely independent, cryptographically valid validator signatures
// before it's finalized -- not just the proposer's own say-so.
func TestConsensusRound_RequiresTwoOfThreeRealSignatures(t *testing.T) {
	p, f1, f2 := wireThreeValidators(t)

	hash, finalized, err := p.server.ProposeBlockWithConsensus(3 * time.Second)
	if err != nil {
		t.Fatalf("ProposeBlockWithConsensus: %v", err)
	}
	if !finalized {
		t.Fatal("expected round to reach real quorum and finalize")
	}
	if p.chain.Height() != 1 {
		t.Fatalf("producer height = %d, want 1", p.chain.Height())
	}
	tip := p.chain.Tip()
	if tip == nil || tip.Hash != hash {
		t.Fatalf("producer tip = %v, want hash %s", tip, hash)
	}
	if tip.ProposerSignature == "" {
		t.Fatal("finalized block missing proposer signature")
	}
	// Count only the entries that verify against a REAL registered
	// validator key over the real approval message -- exactly what
	// chain.Chain.verifyBlockAuthorizationLocked itself checks, proving
	// this isn't just trusting the map's size.
	approvalMsg := tip.QuorumApprovalMessage()
	valid := 0
	for voterID, sigHex := range tip.QuorumSignatures {
		var pub ed25519.PublicKey
		switch voterID {
		case p.id:
			pub, _ = synthoscrypto.PublicKeyBytes(p.agent.Identity.PublicKey)
		case f1.id:
			pub, _ = synthoscrypto.PublicKeyBytes(f1.agent.Identity.PublicKey)
		case f2.id:
			pub, _ = synthoscrypto.PublicKeyBytes(f2.agent.Identity.PublicKey)
		default:
			t.Fatalf("quorum signature from unregistered voter_id %q", voterID)
		}
		sig, err := decodeTestHexSig(sigHex)
		if err != nil {
			t.Fatalf("bad signature encoding from %s: %v", voterID, err)
		}
		if !ed25519.Verify(pub, approvalMsg, sig) {
			t.Fatalf("signature from %s does not verify against its own registered key", voterID)
		}
		valid++
	}
	if valid < 2 {
		t.Fatalf("only %d independently-verified validator signatures, want >= 2", valid)
	}

	// The finalized block must also reach the followers via the ordinary
	// gossip-push path, and THEIR independent Chain.FinalizeBlock must
	// accept it too (re-verifying the same 2-of-3 quorum on their own,
	// from their own registered keys) -- not just the producer's.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if f1.chain.Height() == 1 && f2.chain.Height() == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if f1.chain.Height() != 1 {
		t.Fatalf("follower f1 height = %d, want 1 (finalized block never independently accepted)", f1.chain.Height())
	}
	if f2.chain.Height() != 1 {
		t.Fatalf("follower f2 height = %d, want 1 (finalized block never independently accepted)", f2.chain.Height())
	}
}

// TestConsensusRound_ToleratesOneUnreachableValidator proves the point of
// requiring only 2-of-3 rather than 3-of-3: the round must still finalize
// when exactly one of the two follower validators can't be reached, using
// the producer's self-vote plus the one real vote it did collect.
func TestConsensusRound_ToleratesOneUnreachableValidator(t *testing.T) {
	p, f1, f2 := wireThreeValidators(t)
	_ = f2
	// Simulate f2 being down: close its server so requests to it fail
	// outright, then point the producer only at f1 (reachable) plus the
	// now-dead f2 URL, exactly like a real deployment would still list an
	// unreachable peer in its config.
	deadURL := f2.http.URL
	f2.http.Close()
	p.server.SetConsensusPeerURLs([]string{f1.http.URL, deadURL})

	hash, finalized, err := p.server.ProposeBlockWithConsensus(2 * time.Second)
	if err != nil {
		t.Fatalf("ProposeBlockWithConsensus: %v", err)
	}
	if !finalized {
		t.Fatalf("expected round to still finalize with 1 of 2 followers reachable (hash=%s)", hash)
	}
	if p.chain.Height() != 1 {
		t.Fatalf("producer height = %d, want 1", p.chain.Height())
	}
}

// TestConsensusRound_ForgedPeerVoteNeverCountsTowardQuorum proves that a
// peer returning a syntactically valid but cryptographically bogus vote
// (wrong signature, or claiming to be a different registered validator)
// can never substitute for a genuine second signature -- the round must
// fail to finalize rather than accept it.
func TestConsensusRound_ForgedPeerVoteNeverCountsTowardQuorum(t *testing.T) {
	p, f1, f2 := wireThreeValidators(t)
	_ = f1

	// A malicious/buggy peer that always claims an "approve" vote from a
	// REAL registered validator ID, but with a garbage signature that
	// cannot possibly verify against that validator's real key.
	malicious := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Proposal consensus.BlockProposal `json:"proposal"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		vote := consensus.BlockVote{
			BlockHash: body.Proposal.Block.Hash,
			Height:    body.Proposal.Block.Header.Height,
			VoterID:   f2.id, // claims to be a real registered validator
			Vote:      1,
			Signature: "0x" + strings.Repeat("ab", ed25519.SignatureSize), // syntactically valid, cryptographically garbage
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "vote": vote})
	}))
	defer malicious.Close()

	p.server.SetConsensusPeerURLs([]string{malicious.URL})

	hash, finalized, err := p.server.ProposeBlockWithConsensus(2 * time.Second)
	if err != nil {
		t.Fatalf("ProposeBlockWithConsensus: %v", err)
	}
	if finalized {
		t.Fatalf("round finalized on a forged vote alone (self + 1 forged) -- quorum was NOT genuinely reached, hash=%s", hash)
	}
	if p.chain.Height() != 0 {
		t.Fatalf("producer height = %d, want 0 (nothing should have finalized)", p.chain.Height())
	}
}

// TestHandleConsensusPropose_RejectsWrongToken proves the shared-secret
// gate on /consensus/propose actually rejects an unauthenticated caller.
func TestHandleConsensusPropose_RejectsWrongToken(t *testing.T) {
	p, f1, _ := wireThreeValidators(t)
	_ = p

	req, err := http.NewRequest(http.MethodPost, f1.http.URL+"/consensus/propose", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Consensus-Token", "not-the-real-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

// TestHandleConsensusPropose_RejectsInvalidProposerSignature proves a
// proposal claiming to be from a registered validator, but not actually
// signed by that validator's key, is rejected outright -- this is the
// entire trust boundary the HTTP transport relies on (see node.Node.
// HandleProposal's doc comment).
func TestHandleConsensusPropose_RejectsInvalidProposerSignature(t *testing.T) {
	p, f1, _ := wireThreeValidators(t)

	b, err := p.chain.BuildBlock(p.id, p.agent.ProofRoot(), 1000)
	if err != nil {
		t.Fatal(err)
	}
	// Deliberately do NOT sign it as the real proposer -- forge a
	// plausible-looking but invalid signature instead.
	b.ProposerSignature = "0x" + strings.Repeat("ab", ed25519.SignatureSize)

	body, _ := json.Marshal(map[string]any{
		"proposal": consensus.BlockProposal{Block: *b, Height: b.Header.Height},
	})
	req, err := http.NewRequest(http.MethodPost, f1.http.URL+"/consensus/propose", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Consensus-Token", "test-shared-secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (forged proposer signature must be rejected)", resp.StatusCode)
	}
	if f1.chain.Height() != 0 {
		t.Fatalf("follower height = %d, want 0 -- must never accept an unsigned/forged proposal", f1.chain.Height())
	}
}

func decodeTestHexSig(s string) ([]byte, error) {
	if len(s) >= 2 && s[:2] == "0x" {
		s = s[2:]
	}
	return hex.DecodeString(s)
}
