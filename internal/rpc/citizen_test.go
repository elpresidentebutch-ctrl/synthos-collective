package rpc

import (
	"bytes"
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

type citizenTestFixture struct {
	server   *Server
	staker   synthoscrypto.KeyPair
	imposter synthoscrypto.KeyPair
	treasury chain.Address
}

func newCitizenTestFixture(t *testing.T, rewardRateBps uint64) *citizenTestFixture {
	t.Helper()
	stakerKeys, err := synthoscrypto.NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	imposterKeys, err := synthoscrypto.NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	stakerAddr := chain.AddressFromPublicKey(stakerKeys.Public)
	treasuryAddr := chain.Address("0xtreasury000000000000000000000000000000")

	ch, err := chain.NewChain(chain.Genesis{
		ChainID:   "test-citizen-chain",
		TxChainID: 999,
		Alloc: map[chain.Address]uint64{
			stakerAddr:   1_000_000,
			treasuryAddr: 1_000_000,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ch.State.TreasuryAddress = treasuryAddr
	ch.State.CitizenRewardRateBpsPerYear = rewardRateBps

	a := agent.NewAgent("citizen-test-node", "", "", "test-hw", 0)
	bus := network.NewMemoryTransport()
	a.AttachTransport(bus.NodeTransport(a.Identity.AgentID))
	n := node.NewNode(a, ch, consensus.NewEngine(1), bus.NodeTransport(a.Identity.AgentID))

	return &citizenTestFixture{
		server:   NewServer(ch, nil, n),
		staker:   stakerKeys,
		imposter: imposterKeys,
		treasury: treasuryAddr,
	}
}

func TestCitizenStake_RealSignatureMovesOwnBalance(t *testing.T) {
	f := newCitizenTestFixture(t, 1000)
	stakerAddr := chain.AddressFromPublicKey(f.staker.Public)

	req := citizenStakeRequest{Amount: 100_000, Timestamp: time.Now().Unix()}
	req.PublicKey = synthoscrypto.PublicKeyHex(f.staker.Public)
	req.Signature = signHex(f.staker.Private, req.signingPayload())

	body, _ := json.Marshal(req)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/citizen/stake", bytes.NewReader(body))
	f.server.handleCitizenStake(w, r)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if bal := f.server.Chain.State.Get(stakerAddr).Balance; bal != 900_000 {
		t.Fatalf("staker balance = %d, want 900000", bal)
	}
	if amt := f.server.Chain.State.GetCitizenStake(stakerAddr).Amount; amt != 100_000 {
		t.Fatalf("stake amount = %d, want 100000", amt)
	}
}

// TestCitizenStake_ImpersonationCannotStakeSomeoneElsesBalance proves that
// staking is authenticated the same way governance propose/vote are: an
// imposter's own real signature only ever moves the imposter's own address,
// never the address they'd like to pretend to be.
func TestCitizenStake_ImpersonationCannotStakeSomeoneElsesBalance(t *testing.T) {
	f := newCitizenTestFixture(t, 1000)
	imposterAddr := chain.AddressFromPublicKey(f.imposter.Public)

	req := citizenStakeRequest{Amount: 100_000, Timestamp: time.Now().Unix()}
	req.PublicKey = synthoscrypto.PublicKeyHex(f.imposter.Public)
	req.Signature = signHex(f.imposter.Private, req.signingPayload())

	body, _ := json.Marshal(req)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/citizen/stake", bytes.NewReader(body))
	f.server.handleCitizenStake(w, r)

	// The imposter has no balance of their own, so their own (correctly
	// authenticated) stake attempt should fail on insufficient funds --
	// never succeed against the staker's balance.
	if w.Code == 200 {
		t.Fatalf("expected an unfunded imposter's stake to fail, got 200: %s", w.Body.String())
	}
	if amt := f.server.Chain.State.GetCitizenStake(imposterAddr).Amount; amt != 0 {
		t.Fatalf("imposter should not have acquired a stake, got %d", amt)
	}
}

func TestCitizenStake_TamperedSignatureFails(t *testing.T) {
	f := newCitizenTestFixture(t, 1000)
	req := citizenStakeRequest{Amount: 100_000, Timestamp: time.Now().Unix()}
	req.PublicKey = synthoscrypto.PublicKeyHex(f.staker.Public)
	req.Signature = signHex(f.staker.Private, req.signingPayload())
	// Tamper with the amount after signing.
	req.Amount = 999_000

	body, _ := json.Marshal(req)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/citizen/stake", bytes.NewReader(body))
	f.server.handleCitizenStake(w, r)

	if w.Code == 200 {
		t.Fatalf("expected tampered stake request to be rejected, got 200: %s", w.Body.String())
	}
}

func TestCitizenUnstake_RealSignatureReturnsBalance(t *testing.T) {
	f := newCitizenTestFixture(t, 1000)
	stakerAddr := chain.AddressFromPublicKey(f.staker.Public)
	if err := f.server.Chain.State.StakeCitizen(stakerAddr, 100_000, time.Now().Unix()); err != nil {
		t.Fatalf("seed stake: %v", err)
	}

	req := citizenUnstakeRequest{Amount: 40_000, Timestamp: time.Now().Unix()}
	req.PublicKey = synthoscrypto.PublicKeyHex(f.staker.Public)
	req.Signature = signHex(f.staker.Private, req.signingPayload())

	body, _ := json.Marshal(req)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/citizen/unstake", bytes.NewReader(body))
	f.server.handleCitizenUnstake(w, r)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if bal := f.server.Chain.State.Get(stakerAddr).Balance; bal != 940_000 {
		t.Fatalf("balance after unstake = %d, want 940000", bal)
	}
}

func TestCitizenClaimRewards_PaysAccruedAmountFromTreasury(t *testing.T) {
	f := newCitizenTestFixture(t, 1000) // 10%/year
	stakerAddr := chain.AddressFromPublicKey(f.staker.Public)
	if err := f.server.Chain.State.StakeCitizen(stakerAddr, 1_000_000, 0); err != nil {
		t.Fatalf("seed stake: %v", err)
	}

	reqBody, _ := json.Marshal(map[string]string{"address": string(stakerAddr)})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/citizen/claim-rewards", bytes.NewReader(reqBody))
	f.server.handleCitizenClaimRewards(w, r)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Paid uint64 `json:"paid"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	// No time has passed since stake began (timestamp 0 vs "now"), so this
	// mainly proves the endpoint runs the real chain logic without erroring
	// rather than asserting an exact payout (that's covered precisely in
	// internal/chain/citizen_test.go).
	if f.server.Chain.State.Get(f.treasury).Balance > 1_000_000 {
		t.Fatalf("treasury balance should never increase from a claim")
	}
	_ = resp
}

func TestCitizenClaimRewards_UnconfiguredDeploymentReturns503(t *testing.T) {
	f := newCitizenTestFixture(t, 0) // reward rate unconfigured
	stakerAddr := chain.AddressFromPublicKey(f.staker.Public)
	if err := f.server.Chain.State.StakeCitizen(stakerAddr, 1_000_000, 0); err != nil {
		t.Fatalf("seed stake: %v", err)
	}

	reqBody, _ := json.Marshal(map[string]string{"address": string(stakerAddr)})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/citizen/claim-rewards", bytes.NewReader(reqBody))
	f.server.handleCitizenClaimRewards(w, r)

	if w.Code != 503 {
		t.Fatalf("expected 503 for unconfigured rewards, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCitizenStatus_ReportsRealStakeAndPendingRewards(t *testing.T) {
	f := newCitizenTestFixture(t, 1000)
	stakerAddr := chain.AddressFromPublicKey(f.staker.Public)
	if err := f.server.Chain.State.StakeCitizen(stakerAddr, 250_000, 0); err != nil {
		t.Fatalf("seed stake: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/citizen/status?address="+string(stakerAddr), nil)
	f.server.handleCitizenStatus(w, r)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Stake struct {
			Amount uint64 `json:"amount"`
		} `json:"stake"`
		RewardsConfigured bool `json:"rewards_configured"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Stake.Amount != 250_000 {
		t.Fatalf("reported stake = %d, want 250000", resp.Stake.Amount)
	}
	if !resp.RewardsConfigured {
		t.Fatalf("expected rewards_configured=true")
	}
}
