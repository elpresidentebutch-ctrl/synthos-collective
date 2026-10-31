package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"synthos-collective/internal/chain"
	"synthos-collective/internal/wallet"
)

// fakeRewardChain records transfers and lets a test decide when they land.
type fakeRewardChain struct {
	nonce     uint64 // committed nonce of the rewards wallet
	submitted []chain.Tx
	failNext  bool
}

func (f *fakeRewardChain) Nonce(string) (uint64, error) { return f.nonce, nil }

func (f *fakeRewardChain) Submit(tx chain.Tx) (string, error) {
	if f.failNext {
		f.failNext = false
		return "", errors.New("insufficient funds")
	}
	if err := tx.Verify(); err != nil {
		return "", err
	}
	f.submitted = append(f.submitted, tx)
	return tx.ID, nil
}

// commit simulates every submitted transfer landing in a block.
func (f *fakeRewardChain) commit() { f.nonce = uint64(len(f.submitted)) }

const (
	addrA = "0x00000000000000000000000000000000000000aa"
	addrB = "0x00000000000000000000000000000000000000bb"
)

var keeperNow = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

func days(d float64) int64 { return int64(d * 24 * float64(time.Hour/time.Millisecond)) }

func rewardPeer(name string, uptimeDays float64, payout string) peer {
	return peer{
		Name: name, PublicKey: "ab", LastSeen: keeperNow.Add(-time.Minute).UnixMilli(),
		LastNonce: "n1", ValidHeartbeats: 100, VerifiedUptimeMS: days(uptimeDays),
		PayoutAddress: payout, HouseholdIP: "", HardwareCommitment: "hw-" + name,
	}
}

func keeperServer(peers ...peer) *server {
	s := &server{state: registryState{Peers: map[string]peer{}}}
	for _, p := range peers {
		s.state.Peers[p.Name] = p
	}
	return s
}

func keeperConfig(t *testing.T, approved map[string]string, capSYN uint64) rewardKeeperConfig {
	t.Helper()
	w, err := wallet.New()
	if err != nil {
		t.Fatal(err)
	}
	addr, _ := w.Address()
	return rewardKeeperConfig{Enabled: true, Wallet: w, Address: strings.ToLower(string(addr)), MonthlyCap: capSYN, Approved: approved, ChainID: 20260702}
}

func TestKeeperRecordsPaymentOnlyAfterChainConfirms(t *testing.T) {
	s := keeperServer(rewardPeer("node-a", 31, addrA))
	cfg := keeperConfig(t, map[string]string{"node-a": addrA}, 100000)
	ch := &fakeRewardChain{}

	s.rewardKeeperCycle(keeperNow, cfg, ch)
	if len(ch.submitted) != 1 || string(ch.submitted[0].To) != addrA || ch.submitted[0].Amount != 10000 {
		t.Fatalf("expected one 10,000 SYN first-month transfer to %s, got %+v", addrA, ch.submitted)
	}
	if p := s.state.Peers["node-a"]; p.TotalRewardsPaidSYN != 0 || p.LastRewardPaidEpoch != "" {
		t.Fatalf("recorded as paid before confirmation: %+v", p)
	}
	if s.state.RewardPayouts[0].Status != payoutStatusSubmitted {
		t.Fatalf("status %q", s.state.RewardPayouts[0].Status)
	}

	// Next cycle before the block lands: nothing new is sent.
	s.rewardKeeperCycle(keeperNow.Add(2*time.Minute), cfg, ch)
	if len(ch.submitted) != 1 {
		t.Fatalf("sent again while unconfirmed: %d transfers", len(ch.submitted))
	}

	ch.commit()
	s.rewardKeeperCycle(keeperNow.Add(4*time.Minute), cfg, ch)
	p := s.state.Peers["node-a"]
	if p.TotalRewardsPaidSYN != 10000 || p.LastRewardPaidEpoch != "2026-11" || p.LastRewardTxHash != ch.submitted[0].ID {
		t.Fatalf("not recorded after confirmation: %+v", p)
	}
	if len(ch.submitted) != 1 {
		t.Fatalf("paid twice in one month: %d transfers", len(ch.submitted))
	}
}

func TestKeeperFailedTransferIsNotRecorded(t *testing.T) {
	s := keeperServer(rewardPeer("node-a", 31, addrA))
	cfg := keeperConfig(t, map[string]string{"node-a": addrA}, 100000)
	ch := &fakeRewardChain{failNext: true}
	s.rewardKeeperCycle(keeperNow, cfg, ch)
	if len(s.state.RewardPayouts) != 0 || s.state.Peers["node-a"].TotalRewardsPaidSYN != 0 {
		t.Fatalf("failed transfer was recorded: %+v", s.state.RewardPayouts)
	}
	s.rewardKeeperCycle(keeperNow.Add(2*time.Minute), cfg, ch)
	if len(ch.submitted) != 1 {
		t.Fatalf("expected a retry on the next cycle, got %d transfers", len(ch.submitted))
	}
}

func TestKeeperUsesConsecutiveNonces(t *testing.T) {
	s := keeperServer(rewardPeer("node-a", 31, addrA), rewardPeer("node-b", 40, addrB))
	cfg := keeperConfig(t, map[string]string{"node-a": addrA, "node-b": addrB}, 100000)
	ch := &fakeRewardChain{nonce: 7}
	s.rewardKeeperCycle(keeperNow, cfg, ch)
	if len(ch.submitted) != 2 || ch.submitted[0].Nonce != 7 || ch.submitted[1].Nonce != 8 {
		t.Fatalf("want nonces 7 and 8, got %+v", ch.submitted)
	}
}

func TestKeeperSkipsNodesThatHaventEarnedIt(t *testing.T) {
	stale := rewardPeer("stale", 40, addrA)
	stale.LastSeen = keeperNow.Add(-time.Hour).UnixMilli()
	young := rewardPeer("young", 29, addrA)
	unapproved := rewardPeer("unapproved", 40, addrA)
	swapped := rewardPeer("swapped", 40, addrB) // payout changed after approval
	repaid := rewardPeer("repaid", 50, addrA)
	repaid.RewardUptimeAtPaidMS = days(31) // only 19 new days
	repaid.LastRewardPaidEpoch = "2026-10"

	s := keeperServer(stale, young, unapproved, swapped, repaid)
	cfg := keeperConfig(t, map[string]string{
		"stale": addrA, "young": addrA, "swapped": addrA, "repaid": addrA,
	}, 100000)
	ch := &fakeRewardChain{}
	s.rewardKeeperCycle(keeperNow, cfg, ch)
	if len(ch.submitted) != 0 {
		var names []string
		for _, tx := range ch.submitted {
			names = append(names, tx.Metadata[1].Value)
		}
		t.Fatalf("paid nodes that shouldn't be: %v", names)
	}
}

func TestKeeperPaysOneNodePerHousehold(t *testing.T) {
	a := rewardPeer("home-1", 40, addrA)
	a.HouseholdIP = "76.158.149.182"
	b := rewardPeer("home-2", 40, addrB)
	b.HouseholdIP = "76.158.149.182"
	s := keeperServer(a, b)
	cfg := keeperConfig(t, map[string]string{"home-1": addrA, "home-2": addrB}, 100000)
	ch := &fakeRewardChain{}
	s.rewardKeeperCycle(keeperNow, cfg, ch)
	if len(ch.submitted) != 1 {
		t.Fatalf("two nodes on one home connection should earn one reward, got %d", len(ch.submitted))
	}
}

func TestKeeperPaysToApprovedAddressAndHonorsMonthlyCap(t *testing.T) {
	a := rewardPeer("node-a", 31, "") // never set a payout address: approval decides
	b := rewardPeer("node-b", 31, addrB)
	s := keeperServer(a, b)
	cfg := keeperConfig(t, map[string]string{"node-a": addrA, "node-b": addrB}, 15000)
	ch := &fakeRewardChain{}
	s.rewardKeeperCycle(keeperNow, cfg, ch)
	if len(ch.submitted) != 1 || string(ch.submitted[0].To) != addrA {
		t.Fatalf("cap of 15,000 should allow exactly one 10,000 transfer to %s, got %+v", addrA, ch.submitted)
	}
}

func TestKeeperPausesOnStuckTransfer(t *testing.T) {
	s := keeperServer(rewardPeer("node-a", 31, addrA), rewardPeer("node-b", 31, addrB))
	cfg := keeperConfig(t, map[string]string{"node-a": addrA}, 100000)
	ch := &fakeRewardChain{}
	s.rewardKeeperCycle(keeperNow, cfg, ch)
	cfg.Approved["node-b"] = addrB
	s.rewardKeeperCycle(keeperNow.Add(2*time.Hour), cfg, ch)
	if s.state.RewardPayouts[0].Status != payoutStatusStuck {
		t.Fatalf("want stuck, got %q", s.state.RewardPayouts[0].Status)
	}
	if len(ch.submitted) != 1 {
		t.Fatalf("kept sending while a transfer was stuck: %d", len(ch.submitted))
	}
}

func TestKeeperDisabledWithoutSettings(t *testing.T) {
	for _, k := range []string{"SYNTHOS_REWARD_KEEPER_PRIVATE_KEY", "SYNTHOS_REWARD_MONTHLY_CAP_SYN"} {
		t.Setenv(k, "")
	}
	t.Setenv("SYNTHOS_DISTRIBUTION_AGENT_PRIVATE_KEY", strings.Repeat("11", 32)) // sale wallet must not be used
	if cfg := loadRewardKeeperConfig(); cfg.Enabled {
		t.Fatal("keeper enabled without its own key, cap and approval file")
	}
	w, _ := wallet.New()
	key, _ := w.PrivateKeyHex()
	t.Setenv("SYNTHOS_REWARD_KEEPER_PRIVATE_KEY", key)
	t.Setenv("SYNTHOS_REWARD_MONTHLY_CAP_SYN", "50000")
	file := filepath.Join(t.TempDir(), "approved.json")
	t.Setenv("SYNTHOS_REWARD_APPROVED_FILE", file)
	if cfg := loadRewardKeeperConfig(); cfg.Enabled {
		t.Fatal("keeper enabled without an approval file")
	}
	_ = os.WriteFile(file, []byte(`{"approved":[{"node_id":"node-a","payout_address":"`+strings.ToUpper(addrA[2:])+`"}]}`), 0o600)
	if cfg := loadRewardKeeperConfig(); cfg.Enabled {
		t.Fatal("accepted an address without 0x")
	}
	_ = os.WriteFile(file, []byte(`{"approved":[{"node_id":"node-a","payout_address":"`+addrA+`"}]}`), 0o600)
	cfg := loadRewardKeeperConfig()
	if !cfg.Enabled || cfg.Approved["node-a"] != addrA || cfg.MonthlyCap != 50000 {
		t.Fatalf("keeper should be enabled: %+v", cfg.Reason)
	}

	// Key from a file made by -new-reward-key, cap from the approval file.
	t.Setenv("SYNTHOS_REWARD_KEEPER_PRIVATE_KEY", "")
	t.Setenv("SYNTHOS_REWARD_MONTHLY_CAP_SYN", "")
	keyFile := filepath.Join(t.TempDir(), "keys", "reward-keeper.key")
	t.Setenv("SYNTHOS_REWARD_KEEPER_KEY_FILE", keyFile)
	addr, err := createRewardKeeperKey(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := createRewardKeeperKey(keyFile); err == nil {
		t.Fatal("overwrote an existing reward key")
	}
	if info, _ := os.Stat(keyFile); info.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode %v", info.Mode().Perm())
	}
	_ = os.WriteFile(file, []byte(`{"monthly_cap_syn":100000,"approved":[]}`), 0o600)
	cfg = loadRewardKeeperConfig()
	if !cfg.Enabled || cfg.MonthlyCap != 100000 || cfg.Address != strings.ToLower(addr) {
		t.Fatalf("key file + file cap should enable the keeper: %q %+v", cfg.Reason, cfg.MonthlyCap)
	}
}

func TestKeeperFirstRewardIsTheBondThenMonthly(t *testing.T) {
	s := keeperServer(rewardPeer("node-a", 31, addrA))
	cfg := keeperConfig(t, map[string]string{"node-a": addrA}, 100000)
	ch := &fakeRewardChain{}
	s.rewardKeeperCycle(keeperNow, cfg, ch)
	ch.commit()
	s.rewardKeeperCycle(keeperNow.Add(2*time.Minute), cfg, ch)

	// A month later, with 28 more days of uptime: the monthly amount.
	next := keeperNow.AddDate(0, 1, 0)
	p := s.state.Peers["node-a"]
	p.VerifiedUptimeMS += days(28)
	p.LastSeen = next.Add(-time.Minute).UnixMilli()
	s.state.Peers["node-a"] = p
	s.rewardKeeperCycle(next, cfg, ch)
	if len(ch.submitted) != 2 {
		t.Fatalf("want a second payment, got %d", len(ch.submitted))
	}
	if ch.submitted[0].Amount != 10000 || ch.submitted[1].Amount != monthlyRewardAmount(next) {
		t.Fatalf("amounts %d then %d", ch.submitted[0].Amount, ch.submitted[1].Amount)
	}
	if ch.submitted[0].Amount != validatorMinimumBondSYN {
		t.Fatal("first reward must equal the validator bond")
	}
}

func TestUnsignedPayoutAddressCanOnlyBeSetOnce(t *testing.T) {
	s := keeperServer(rewardPeer("node-a", 1, ""))
	post := func(addr string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/nodes/node-a/payout-address", strings.NewReader(`{"payout_address":"`+addr+`"}`))
		s.handleAPINodeByID(rec, req)
		return rec.Code
	}
	if code := post("not-an-address"); code != http.StatusBadRequest {
		t.Fatalf("bad address accepted: %d", code)
	}
	if code := post(addrA); code != http.StatusOK {
		t.Fatalf("first set failed: %d", code)
	}
	if code := post(addrB); code != http.StatusConflict {
		t.Fatalf("unsigned overwrite allowed: %d", code)
	}
	if got := s.state.Peers["node-a"].PayoutAddress; got != addrA {
		t.Fatalf("payout address changed to %s", got)
	}
}

func TestRewardsEndpointPublishesLedger(t *testing.T) {
	t.Setenv("SYNTHOS_REWARD_KEEPER_PRIVATE_KEY", "")
	s := keeperServer()
	s.state.RewardPayouts = []rewardPayout{{ID: "r1", NodeID: "node-a", Address: addrA, AmountSYN: 5000, Epoch: "2026-11", Status: payoutStatusConfirmed, TxID: "0xabc"}}
	rec := httptest.NewRecorder()
	s.handleAPIRewards(rec, httptest.NewRequest(http.MethodGet, "/api/rewards", nil))
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["enabled"] != false || body["not_paying_because"] == nil || len(body["payouts"].([]any)) != 1 {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}
