package rpc

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"synthos-collective/internal/agent"
	"synthos-collective/internal/chain"
	"synthos-collective/internal/consensus"
	synthoscrypto "synthos-collective/internal/crypto"
	"synthos-collective/internal/network"
	"synthos-collective/internal/node"
)

// governanceTestFixture builds a real chain + node with governance wired up
// exactly the way cmd/synthosd/main.go wires it, so these tests exercise the
// actual RPC->mempool->block->State.GovernanceProposals path, not a mock of
// it.
type governanceTestFixture struct {
	server   *Server
	founder  synthoscrypto.KeyPair
	voter    synthoscrypto.KeyPair
	imposter synthoscrypto.KeyPair
}

func newGovernanceTestFixture(t *testing.T) *governanceTestFixture {
	t.Helper()

	founderKeys, err := synthoscrypto.NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	voterKeys, err := synthoscrypto.NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	imposterKeys, err := synthoscrypto.NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	founderAddr := chain.AddressFromPublicKey(founderKeys.Public)
	voterAddr := chain.AddressFromPublicKey(voterKeys.Public)
	treasuryAddr := chain.Address("0xtreasury000000000000000000000000000000")

	ch, err := chain.NewChain(chain.Genesis{
		ChainID:   "test-governance-chain",
		TxChainID: 999,
		Alloc: map[chain.Address]uint64{
			founderAddr:  1_000,
			voterAddr:    1_000,
			treasuryAddr: 5_000,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	a := agent.NewAgent("gov-test-node", "", "", "test-hw", 0)
	bus := network.NewMemoryTransport()
	a.AttachTransport(bus.NodeTransport(a.Identity.AgentID))

	n := node.NewNode(a, ch, consensus.NewEngine(1), bus.NodeTransport(a.Identity.AgentID))
	n.InitGovernance(founderAddr, treasuryAddr)

	srv := NewServer(ch, nil, n)

	return &governanceTestFixture{
		server:   srv,
		founder:  founderKeys,
		voter:    voterKeys,
		imposter: imposterKeys,
	}
}

// signedGovernanceTx builds and signs a real chain.Tx carrying a
// governance_* metadata type -- the same shape /governance/propose,
// /governance/vote, and /governance/execute now require (see governance.go's
// audit fix comment): a normal, Tx.Verify()-checked signature, not a
// bespoke per-endpoint payload.
func signedGovernanceTx(t *testing.T, c *chain.Chain, priv ed25519.PrivateKey, txType string, metadata []chain.KeyValuePair) chain.Tx {
	t.Helper()
	pub := priv.Public().(ed25519.PublicKey)
	from := chain.AddressFromPublicKey(pub)
	tx := chain.Tx{
		ChainID:   c.TransactionChainID(),
		From:      from,
		To:        from,
		Amount:    1, // nominal: these actions don't move tx.Amount, Tx.validateBasic just requires it non-zero
		Fee:       chain.MIN_FEE,
		Nonce:     c.State.GetNextNonce(from),
		PublicKey: "0x" + hex.EncodeToString(pub),
		Metadata:  append([]chain.KeyValuePair{{Key: "type", Value: txType}}, metadata...),
		Timestamp: time.Now().UTC().Unix(),
	}
	if err := tx.Sign(priv); err != nil {
		t.Fatalf("sign governance tx: %v", err)
	}
	return tx
}

func proposeMetadata(id, description string, amount uint64, recipient chain.Address) []chain.KeyValuePair {
	return []chain.KeyValuePair{
		{Key: "id", Value: id},
		{Key: "description", Value: description},
		{Key: "amount", Value: strconv.FormatUint(amount, 10)},
		{Key: "recipient", Value: string(recipient)},
	}
}

func voteMetadata(proposalID string, inFavor bool) []chain.KeyValuePair {
	return []chain.KeyValuePair{
		{Key: "proposal_id", Value: proposalID},
		{Key: "in_favor", Value: strconv.FormatBool(inFavor)},
	}
}

func executeMetadata(proposalID string) []chain.KeyValuePair {
	return []chain.KeyValuePair{{Key: "proposal_id", Value: proposalID}}
}

// submitAndMine posts tx to the given handler, then -- only when the
// submission itself was accepted -- mines it into a real block, mirroring
// postTxAndMine in citizen_test.go. Returns both the submit response and
// how many tx actually landed in the mined block, so a test can tell a
// rejected submission (never queued) apart from one that was queued but
// excluded at build time (invalid once the real chain logic ran).
func submitAndMine(t *testing.T, srv *Server, path string, handler func(w http.ResponseWriter, r *http.Request), tx chain.Tx) (submitCode int, minedTxCount int) {
	t.Helper()
	body, _ := json.Marshal(tx)
	w := httptest.NewRecorder()
	handler(w, httptest.NewRequest("POST", path, bytes.NewReader(body)))
	if w.Code != 200 {
		return w.Code, -1
	}
	block, err := srv.Chain.BuildBlock("validator-1", "proof", 10)
	if err != nil {
		t.Fatalf("build block: %v", err)
	}
	if len(block.Tx) == 0 {
		return w.Code, 0
	}
	if err := srv.Chain.FinalizeBlock(block); err != nil {
		t.Fatalf("finalize block: %v", err)
	}
	return w.Code, len(block.Tx)
}

func TestGovernancePropose_OnlyRealFounderSignatureSucceeds(t *testing.T) {
	f := newGovernanceTestFixture(t)

	tx := signedGovernanceTx(t, f.server.Chain, f.founder.Private, "governance_propose",
		proposeMetadata("prop-1", "test payout", 100, "0xrecipient00000000000000000000000000000"))
	submitCode, mined := submitAndMine(t, f.server, "/governance/propose", f.server.handleGovernancePropose, tx)
	if submitCode != 200 {
		t.Fatalf("expected submission to be accepted, got %d", submitCode)
	}
	if mined != 1 {
		t.Fatalf("expected the founder's proposal to be mined, got %d tx in block", mined)
	}

	p, ok := f.server.Node.Governance.Get("prop-1")
	if !ok {
		t.Fatal("expected proposal to be created")
	}
	if !p.IsActive {
		t.Fatal("expected proposal to be active")
	}
}

func TestGovernancePropose_ImpersonationFails(t *testing.T) {
	f := newGovernanceTestFixture(t)

	// The imposter signs with their OWN real key -- there's no field to lie
	// about who they are, since tx.From must match the key that produced
	// tx.Signature (Tx.Verify enforces this for every transaction type).
	// This proves signing as yourself, even honestly, doesn't let you
	// create proposals unless your address is the configured founder.
	tx := signedGovernanceTx(t, f.server.Chain, f.imposter.Private, "governance_propose",
		proposeMetadata("prop-imposter", "should not be allowed", 100, "0xrecipient00000000000000000000000000000"))
	submitCode, mined := submitAndMine(t, f.server, "/governance/propose", f.server.handleGovernancePropose, tx)
	if submitCode == 200 && mined > 0 {
		t.Fatalf("expected non-founder proposal to never be mined, got submitCode=%d mined=%d", submitCode, mined)
	}
	if _, ok := f.server.Node.Governance.Get("prop-imposter"); ok {
		t.Fatal("proposal must not have been created by a non-founder signature")
	}
}

func TestGovernancePropose_TamperedSignatureFails(t *testing.T) {
	f := newGovernanceTestFixture(t)

	tx := signedGovernanceTx(t, f.server.Chain, f.founder.Private, "governance_propose",
		proposeMetadata("prop-tamper", "tampered", 100, "0xrecipient00000000000000000000000000000"))
	tx.Amount = 999 // tamper after signing

	body, _ := json.Marshal(tx)
	w := httptest.NewRecorder()
	f.server.handleGovernancePropose(w, httptest.NewRequest("POST", "/governance/propose", bytes.NewReader(body)))
	if w.Code != 400 {
		t.Fatalf("expected 400 for a tampered signature, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGovernanceVote_WeightedByRealSignerStakeOnly(t *testing.T) {
	f := newGovernanceTestFixture(t)

	// Founder creates the proposal for real.
	propose := signedGovernanceTx(t, f.server.Chain, f.founder.Private, "governance_propose",
		proposeMetadata("prop-vote", "vote test", 100, "0xrecipient00000000000000000000000000000"))
	submitCode, mined := submitAndMine(t, f.server, "/governance/propose", f.server.handleGovernancePropose, propose)
	if submitCode != 200 || mined != 1 {
		t.Fatalf("setup: propose failed: submitCode=%d mined=%d", submitCode, mined)
	}

	// Real voter (1000 stake in genesis) votes FOR, signing for themselves.
	vote := signedGovernanceTx(t, f.server.Chain, f.voter.Private, "governance_vote", voteMetadata("prop-vote", true))
	submitCode, mined = submitAndMine(t, f.server, "/governance/vote", f.server.handleGovernanceVote, vote)
	if submitCode != 200 || mined != 1 {
		t.Fatalf("expected real voter's signed vote to be mined, got submitCode=%d mined=%d", submitCode, mined)
	}

	p, ok := f.server.Node.Governance.Get("prop-vote")
	if !ok || p.VotesFor != 1_000 {
		t.Fatalf("expected VotesFor=1000 (the voter's real balance), got %+v", p)
	}

	// An imposter cannot inflate the tally by signing their own (zero-stake)
	// key and voting again on behalf of themselves -- and definitely cannot
	// vote as the real voter without that voter's private key.
	imposterVote := signedGovernanceTx(t, f.server.Chain, f.imposter.Private, "governance_vote", voteMetadata("prop-vote", true))
	submitCode, mined = submitAndMine(t, f.server, "/governance/vote", f.server.handleGovernanceVote, imposterVote)
	if submitCode == 200 && mined > 0 {
		t.Fatalf("expected zero-stake imposter vote to never be mined, got submitCode=%d mined=%d", submitCode, mined)
	}
	p, _ = f.server.Node.Governance.Get("prop-vote")
	if p.VotesFor != 1_000 {
		t.Fatalf("tally must not have changed from the failed imposter vote, got %+v", p)
	}
}

func TestGovernanceExecute_MovesRealFundsOnceQuorumReachedAndNotTwice(t *testing.T) {
	f := newGovernanceTestFixture(t)

	propose := signedGovernanceTx(t, f.server.Chain, f.founder.Private, "governance_propose",
		proposeMetadata("prop-exec", "execute test", 500, "0xrecipient00000000000000000000000000000"))
	submitCode, mined := submitAndMine(t, f.server, "/governance/propose", f.server.handleGovernancePropose, propose)
	if submitCode != 200 || mined != 1 {
		t.Fatalf("setup: propose failed: submitCode=%d mined=%d", submitCode, mined)
	}

	vote := signedGovernanceTx(t, f.server.Chain, f.voter.Private, "governance_vote", voteMetadata("prop-exec", true))
	submitCode, mined = submitAndMine(t, f.server, "/governance/vote", f.server.handleGovernanceVote, vote)
	if submitCode != 200 || mined != 1 {
		t.Fatalf("setup: vote failed: submitCode=%d mined=%d", submitCode, mined)
	}

	treasuryBefore := f.server.Chain.State.Get("0xtreasury000000000000000000000000000000").Balance
	recipientBefore := f.server.Chain.State.Get("0xrecipient00000000000000000000000000000").Balance

	// Anyone can submit the execute transaction -- here, the voter, who has
	// no special execute permission; executeGovernanceProposal's own checks
	// (passed + quorum) are the real gate, not who signs this tx.
	exec := signedGovernanceTx(t, f.server.Chain, f.voter.Private, "governance_execute", executeMetadata("prop-exec"))
	submitCode, mined = submitAndMine(t, f.server, "/governance/execute", f.server.handleGovernanceExecute, exec)
	if submitCode != 200 || mined != 1 {
		t.Fatalf("expected execute to be mined once quorum reached, got submitCode=%d mined=%d", submitCode, mined)
	}

	treasuryAfter := f.server.Chain.State.Get("0xtreasury000000000000000000000000000000").Balance
	recipientAfter := f.server.Chain.State.Get("0xrecipient00000000000000000000000000000").Balance
	if treasuryBefore-treasuryAfter != 500 {
		t.Fatalf("expected treasury debited by 500, got %d -> %d", treasuryBefore, treasuryAfter)
	}
	if recipientAfter-recipientBefore != 500 {
		t.Fatalf("expected recipient credited by 500, got %d -> %d", recipientBefore, recipientAfter)
	}

	// Second execute of the same proposal must never be mined -- no double
	// payout. Use a fresh nonce/tx (same signer) since the first execute
	// tx's nonce is now spent.
	exec2 := signedGovernanceTx(t, f.server.Chain, f.voter.Private, "governance_execute", executeMetadata("prop-exec"))
	submitCode, mined = submitAndMine(t, f.server, "/governance/execute", f.server.handleGovernanceExecute, exec2)
	if submitCode == 200 && mined > 0 {
		t.Fatalf("expected second execute of the same proposal to never be mined, got submitCode=%d mined=%d", submitCode, mined)
	}
	treasuryFinal := f.server.Chain.State.Get("0xtreasury000000000000000000000000000000").Balance
	if treasuryFinal != treasuryAfter {
		t.Fatalf("treasury balance must not change on a rejected double-execute, got %d -> %d", treasuryAfter, treasuryFinal)
	}
}
