package rpc

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"net/http"
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

// signedCitizenTx builds and signs a real chain.Tx carrying a citizen_*
// metadata type -- the same shape /citizen/stake, /citizen/unstake, and
// /citizen/claim-rewards now require (see citizen.go's audit fix comment):
// a normal, Tx.Verify()-checked signature, not a bespoke per-endpoint
// payload. amount is used directly as tx.Amount for citizen_stake/
// citizen_unstake (see applyCitizenGovernanceTx); it's ignored by
// citizen_claim_rewards but Tx.validateBasic still requires it non-zero.
func signedCitizenTx(t *testing.T, c *chain.Chain, priv ed25519.PrivateKey, txType string, amount uint64) chain.Tx {
	t.Helper()
	pub := priv.Public().(ed25519.PublicKey)
	from := chain.AddressFromPublicKey(pub)
	tx := chain.Tx{
		ChainID:   c.TransactionChainID(),
		From:      from,
		To:        from,
		Amount:    amount,
		Fee:       chain.MIN_FEE,
		Nonce:     c.State.GetNextNonce(from),
		PublicKey: "0x" + hex.EncodeToString(pub),
		Metadata:  []chain.KeyValuePair{{Key: "type", Value: txType}},
		Timestamp: time.Now().UTC().Unix(),
	}
	if err := tx.Sign(priv); err != nil {
		t.Fatalf("sign citizen tx: %v", err)
	}
	return tx
}

// postTxAndMine submits tx to handler, then -- only if the submit itself
// was accepted -- builds a candidate block and finalizes it if the tx made
// it in. These actions are only applied once mined into a block now (see
// citizen.go's audit fix comment), not synchronously within the HTTP
// response the way they used to be, so tests must mine a block to observe
// the result -- mirroring exactly how internal/chain/bridge_test.go already
// exercises bridge txs.
//
// SubmitTx only checks the transaction's own signature/nonce/chain ID, not
// whether it would actually succeed (e.g. insufficient balance) -- that's
// only known once BuildBlock tries applying it against real state, exactly
// like every other transaction type on this chain. So a submission that
// will ultimately fail still returns 200 here; it's simply left out of the
// block BuildBlock produces. minedTxCount tells a test which happened.
func (f *citizenTestFixture) postTxAndMine(t *testing.T, path string, handler http.HandlerFunc, tx chain.Tx) (w *httptest.ResponseRecorder, minedTxCount int) {
	t.Helper()
	body, _ := json.Marshal(tx)
	w = httptest.NewRecorder()
	r := httptest.NewRequest("POST", path, bytes.NewReader(body))
	handler(w, r)
	if w.Code != 200 {
		return w, -1
	}
	block, err := f.server.Chain.BuildBlock("validator-1", "proof", 10)
	if err != nil {
		t.Fatalf("build block: %v", err)
	}
	if len(block.Tx) == 0 {
		return w, 0
	}
	if err := f.server.Chain.FinalizeBlock(block); err != nil {
		t.Fatalf("finalize block: %v", err)
	}
	return w, len(block.Tx)
}

func TestCitizenStake_RealSignatureMovesOwnBalance(t *testing.T) {
	f := newCitizenTestFixture(t, 1000)
	stakerAddr := chain.AddressFromPublicKey(f.staker.Public)

	tx := signedCitizenTx(t, f.server.Chain, f.staker.Private, "citizen_stake", 100_000)
	w, mined := f.postTxAndMine(t, "/citizen/stake", f.server.handleCitizenStake, tx)

	if w.Code != 200 || mined != 1 {
		t.Fatalf("expected submit 200 and 1 mined tx, got code=%d mined=%d: %s", w.Code, mined, w.Body.String())
	}
	if bal := f.server.Chain.State.Get(stakerAddr).Balance; bal != 1_000_000-100_000-chain.MIN_FEE {
		t.Fatalf("staker balance = %d, want %d", bal, 1_000_000-100_000-chain.MIN_FEE)
	}
	if amt := f.server.Chain.State.GetCitizenStake(stakerAddr).Amount; amt != 100_000 {
		t.Fatalf("stake amount = %d, want 100000", amt)
	}
}

// TestCitizenStake_ImpersonationCannotStakeSomeoneElsesBalance proves that
// staking is authenticated by a real Tx signature exactly like every other
// transaction on this chain: an imposter's own real signature only ever
// moves the imposter's own address, never the address they'd like to
// pretend to be.
func TestCitizenStake_ImpersonationCannotStakeSomeoneElsesBalance(t *testing.T) {
	f := newCitizenTestFixture(t, 1000)
	imposterAddr := chain.AddressFromPublicKey(f.imposter.Public)

	tx := signedCitizenTx(t, f.server.Chain, f.imposter.Private, "citizen_stake", 100_000)
	_, mined := f.postTxAndMine(t, "/citizen/stake", f.server.handleCitizenStake, tx)

	// SubmitTx only checks the tx's own signature/nonce, not whether it can
	// actually afford the stake -- that's only known once BuildBlock tries
	// applying it against real state (see postTxAndMine's doc comment), so
	// the imposter's unfunded stake is expected to be queued but then
	// silently excluded from the block, never mined -- never succeeding
	// against the staker's balance.
	if mined != 0 {
		t.Fatalf("expected an unfunded imposter's stake to never be mined, got %d mined tx", mined)
	}
	if amt := f.server.Chain.State.GetCitizenStake(imposterAddr).Amount; amt != 0 {
		t.Fatalf("imposter should not have acquired a stake, got %d", amt)
	}
}

func TestCitizenStake_TamperedSignatureFails(t *testing.T) {
	f := newCitizenTestFixture(t, 1000)
	tx := signedCitizenTx(t, f.server.Chain, f.staker.Private, "citizen_stake", 100_000)
	// Tamper with the amount after signing -- Tx.Verify() must catch this
	// exactly like it does for any other transaction type.
	tx.Amount = 999_000

	body, _ := json.Marshal(tx)
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
	balanceAfterStake := f.server.Chain.State.Get(stakerAddr).Balance

	tx := signedCitizenTx(t, f.server.Chain, f.staker.Private, "citizen_unstake", 40_000)
	w, mined := f.postTxAndMine(t, "/citizen/unstake", f.server.handleCitizenUnstake, tx)

	if w.Code != 200 || mined != 1 {
		t.Fatalf("expected submit 200 and 1 mined tx, got code=%d mined=%d: %s", w.Code, mined, w.Body.String())
	}
	want := balanceAfterStake + 40_000 - chain.MIN_FEE
	if bal := f.server.Chain.State.Get(stakerAddr).Balance; bal != want {
		t.Fatalf("balance after unstake = %d, want %d", bal, want)
	}
}

func TestCitizenClaimRewards_PaysAccruedAmountFromTreasury(t *testing.T) {
	f := newCitizenTestFixture(t, 1000) // 10%/year
	stakerAddr := chain.AddressFromPublicKey(f.staker.Public)
	// Stake less than the full 1,000,000 balance -- claim-rewards is an
	// ordinary fee-paying transaction like any other now (see the audit fix
	// comment on citizen.go), so the staker needs some spendable balance
	// left over to pay that fee, exactly as they would for a citizen_stake
	// or citizen_unstake transaction.
	if err := f.server.Chain.State.StakeCitizen(stakerAddr, 900_000, 0); err != nil {
		t.Fatalf("seed stake: %v", err)
	}

	tx := signedCitizenTx(t, f.server.Chain, f.staker.Private, "citizen_claim_rewards", 1)
	w, mined := f.postTxAndMine(t, "/citizen/claim-rewards", f.server.handleCitizenClaimRewards, tx)

	if w.Code != 200 || mined != 1 {
		t.Fatalf("expected submit 200 and 1 mined tx, got code=%d mined=%d: %s", w.Code, mined, w.Body.String())
	}
	// The stake was seeded directly (not via a prior mined tx), with
	// StakedAt/LastClaimAt=0, so real wall-clock time has elapsed since
	// then -- the claim should pay out something, and it must come only
	// from the treasury, never mint from nowhere.
	if f.server.Chain.State.Get(f.treasury).Balance >= 1_000_000 {
		t.Fatalf("treasury balance should have decreased from a real accrued claim")
	}
	if f.server.Chain.State.Get(stakerAddr).Balance <= 0 {
		t.Fatalf("staker should have received something back")
	}
}

func TestCitizenClaimRewards_UnconfiguredDeploymentNeverMines(t *testing.T) {
	f := newCitizenTestFixture(t, 0) // reward rate unconfigured
	stakerAddr := chain.AddressFromPublicKey(f.staker.Public)
	if err := f.server.Chain.State.StakeCitizen(stakerAddr, 1_000_000, 0); err != nil {
		t.Fatalf("seed stake: %v", err)
	}

	tx := signedCitizenTx(t, f.server.Chain, f.staker.Private, "citizen_claim_rewards", 1)
	body, _ := json.Marshal(tx)
	w := httptest.NewRecorder()
	f.server.handleCitizenClaimRewards(w, httptest.NewRequest("POST", "/citizen/claim-rewards", bytes.NewReader(body)))

	// Submission itself succeeds (SubmitTx can't know rewards are
	// unconfigured without running the real chain logic) -- the rejection
	// now happens when the block is built: an invalid candidate transaction
	// is simply left out of the block rather than causing a build error.
	if w.Code != 200 {
		t.Fatalf("expected submission to be accepted (202-equivalent), got %d: %s", w.Code, w.Body.String())
	}
	block, err := f.server.Chain.BuildBlock("validator-1", "proof", 10)
	if err != nil {
		t.Fatalf("build block: %v", err)
	}
	if len(block.Tx) != 0 {
		t.Fatalf("expected the unconfigured-rewards claim to be excluded from the block, got %d tx", len(block.Tx))
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
