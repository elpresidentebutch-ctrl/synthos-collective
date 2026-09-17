package rpc

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
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
// actual RPC->TreasuryGovernance path, not a mock of it.
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
			founderAddr:  10,
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

func signHex(priv []byte, msg []byte) string {
	sig := synthoscrypto.Sign(priv, msg)
	return "0x" + hex.EncodeToString(sig)
}

func TestGovernancePropose_OnlyRealFounderSignatureSucceeds(t *testing.T) {
	f := newGovernanceTestFixture(t)

	req := governanceProposeRequest{
		ID:          "prop-1",
		Description: "test payout",
		Amount:      100,
		Recipient:   "0xrecipient00000000000000000000000000000",
		Timestamp:   time.Now().Unix(),
	}
	req.PublicKey = synthoscrypto.PublicKeyHex(f.founder.Public)
	req.Signature = signHex(f.founder.Private, req.signingPayload())

	body, _ := json.Marshal(req)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/governance/propose", bytes.NewReader(body))
	f.server.handleGovernancePropose(w, r)

	if w.Code != 200 {
		t.Fatalf("expected 200 from real founder signature, got %d: %s", w.Code, w.Body.String())
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
	// about who they are, since the caller's address is derived from
	// whichever key actually produced the signature. This proves signing as
	// yourself, even honestly, doesn't let you create proposals unless your
	// derived address is the configured founder.
	req := governanceProposeRequest{
		ID:          "prop-imposter",
		Description: "should not be allowed",
		Amount:      100,
		Recipient:   "0xrecipient00000000000000000000000000000",
		Timestamp:   time.Now().Unix(),
	}
	req.PublicKey = synthoscrypto.PublicKeyHex(f.imposter.Public)
	req.Signature = signHex(f.imposter.Private, req.signingPayload())

	body, _ := json.Marshal(req)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/governance/propose", bytes.NewReader(body))
	f.server.handleGovernancePropose(w, r)

	if w.Code == 200 {
		t.Fatalf("expected non-founder signer to be rejected, got 200: %s", w.Body.String())
	}
	if _, ok := f.server.Node.Governance.Get("prop-imposter"); ok {
		t.Fatal("proposal must not have been created by a non-founder signature")
	}
}

func TestGovernancePropose_TamperedSignatureFails(t *testing.T) {
	f := newGovernanceTestFixture(t)

	req := governanceProposeRequest{
		ID:          "prop-tamper",
		Description: "tampered",
		Amount:      100,
		Recipient:   "0xrecipient00000000000000000000000000000",
		Timestamp:   time.Now().Unix(),
	}
	req.PublicKey = synthoscrypto.PublicKeyHex(f.founder.Public)
	sig := synthoscrypto.Sign(f.founder.Private, req.signingPayload())
	sig[0] ^= 0xFF // corrupt one byte
	req.Signature = "0x" + hex.EncodeToString(sig)

	body, _ := json.Marshal(req)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/governance/propose", bytes.NewReader(body))
	f.server.handleGovernancePropose(w, r)

	if w.Code != 401 {
		t.Fatalf("expected 401 for a tampered signature, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGovernanceVote_WeightedByRealSignerStakeOnly(t *testing.T) {
	f := newGovernanceTestFixture(t)

	// Founder creates the proposal for real.
	propose := governanceProposeRequest{
		ID:          "prop-vote",
		Description: "vote test",
		Amount:      100,
		Recipient:   "0xrecipient00000000000000000000000000000",
		Timestamp:   time.Now().Unix(),
	}
	propose.PublicKey = synthoscrypto.PublicKeyHex(f.founder.Public)
	propose.Signature = signHex(f.founder.Private, propose.signingPayload())
	body, _ := json.Marshal(propose)
	w := httptest.NewRecorder()
	f.server.handleGovernancePropose(w, httptest.NewRequest("POST", "/governance/propose", bytes.NewReader(body)))
	if w.Code != 200 {
		t.Fatalf("setup: propose failed: %d %s", w.Code, w.Body.String())
	}

	// Real voter (1000 stake in genesis) votes FOR, signing for themselves.
	vote := governanceVoteRequest{
		ProposalID: "prop-vote",
		InFavor:    true,
		Timestamp:  time.Now().Unix(),
	}
	vote.PublicKey = synthoscrypto.PublicKeyHex(f.voter.Public)
	vote.Signature = signHex(f.voter.Private, vote.signingPayload())
	body, _ = json.Marshal(vote)
	w = httptest.NewRecorder()
	f.server.handleGovernanceVote(w, httptest.NewRequest("POST", "/governance/vote", bytes.NewReader(body)))
	if w.Code != 200 {
		t.Fatalf("expected real voter's signed vote to succeed, got %d: %s", w.Code, w.Body.String())
	}

	p, ok := f.server.Node.Governance.Get("prop-vote")
	if !ok || p.VotesFor != 1_000 {
		t.Fatalf("expected VotesFor=1000 (the voter's real balance), got %+v", p)
	}

	// An imposter cannot inflate the tally by signing their own (zero-stake)
	// key and voting again on behalf of themselves -- and definitely cannot
	// vote as the real voter without that voter's private key.
	imposterVote := governanceVoteRequest{ProposalID: "prop-vote", InFavor: true, Timestamp: time.Now().Unix()}
	imposterVote.PublicKey = synthoscrypto.PublicKeyHex(f.imposter.Public)
	imposterVote.Signature = signHex(f.imposter.Private, imposterVote.signingPayload())
	body, _ = json.Marshal(imposterVote)
	w = httptest.NewRecorder()
	f.server.handleGovernanceVote(w, httptest.NewRequest("POST", "/governance/vote", bytes.NewReader(body)))
	if w.Code == 200 {
		t.Fatalf("expected zero-stake imposter vote to fail (no stake), got 200: %s", w.Body.String())
	}
	p, _ = f.server.Node.Governance.Get("prop-vote")
	if p.VotesFor != 1_000 {
		t.Fatalf("tally must not have changed from the failed imposter vote, got %+v", p)
	}
}

func TestGovernanceExecute_MovesRealFundsOnceQuorumReachedAndNotTwice(t *testing.T) {
	f := newGovernanceTestFixture(t)

	propose := governanceProposeRequest{
		ID:          "prop-exec",
		Description: "execute test",
		Amount:      500,
		Recipient:   "0xrecipient00000000000000000000000000000",
		Timestamp:   time.Now().Unix(),
	}
	propose.PublicKey = synthoscrypto.PublicKeyHex(f.founder.Public)
	propose.Signature = signHex(f.founder.Private, propose.signingPayload())
	body, _ := json.Marshal(propose)
	w := httptest.NewRecorder()
	f.server.handleGovernancePropose(w, httptest.NewRequest("POST", "/governance/propose", bytes.NewReader(body)))
	if w.Code != 200 {
		t.Fatalf("setup: propose failed: %d %s", w.Code, w.Body.String())
	}

	vote := governanceVoteRequest{ProposalID: "prop-exec", InFavor: true, Timestamp: time.Now().Unix()}
	vote.PublicKey = synthoscrypto.PublicKeyHex(f.voter.Public)
	vote.Signature = signHex(f.voter.Private, vote.signingPayload())
	body, _ = json.Marshal(vote)
	w = httptest.NewRecorder()
	f.server.handleGovernanceVote(w, httptest.NewRequest("POST", "/governance/vote", bytes.NewReader(body)))
	if w.Code != 200 {
		t.Fatalf("setup: vote failed: %d %s", w.Code, w.Body.String())
	}

	treasuryBefore := f.server.Chain.State.Get("0xtreasury000000000000000000000000000000").Balance
	recipientBefore := f.server.Chain.State.Get("0xrecipient00000000000000000000000000000").Balance

	execBody, _ := json.Marshal(governanceExecuteRequest{ProposalID: "prop-exec"})
	w = httptest.NewRecorder()
	f.server.handleGovernanceExecute(w, httptest.NewRequest("POST", "/governance/execute", bytes.NewReader(execBody)))
	if w.Code != 200 {
		t.Fatalf("expected execute to succeed once quorum reached, got %d: %s", w.Code, w.Body.String())
	}

	treasuryAfter := f.server.Chain.State.Get("0xtreasury000000000000000000000000000000").Balance
	recipientAfter := f.server.Chain.State.Get("0xrecipient00000000000000000000000000000").Balance
	if treasuryBefore-treasuryAfter != 500 {
		t.Fatalf("expected treasury debited by 500, got %d -> %d", treasuryBefore, treasuryAfter)
	}
	if recipientAfter-recipientBefore != 500 {
		t.Fatalf("expected recipient credited by 500, got %d -> %d", recipientBefore, recipientAfter)
	}

	// Second execute must fail -- no double payout.
	w = httptest.NewRecorder()
	f.server.handleGovernanceExecute(w, httptest.NewRequest("POST", "/governance/execute", bytes.NewReader(execBody)))
	if w.Code == 200 {
		t.Fatal("expected second execute of the same proposal to fail")
	}
	treasuryFinal := f.server.Chain.State.Get("0xtreasury000000000000000000000000000000").Balance
	if treasuryFinal != treasuryAfter {
		t.Fatalf("treasury balance must not change on a rejected double-execute, got %d -> %d", treasuryAfter, treasuryFinal)
	}
}
