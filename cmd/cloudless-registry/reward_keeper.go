package main

// Validator reward keeper: pays monthly SYN rewards to approved node
// operators from a dedicated rewards wallet.
//
// Rules, each fixing a problem in the first version of the keeper:
//
//  1. A reward is recorded as paid only after the chain confirms it, which
//     is when the rewards wallet's nonce moves past the transfer's nonce
//     (a failed transfer never advances the nonce). A failed or missing
//     transfer is never written down as paid, and no made-up hash is stored.
//  2. Transfers go out one batch at a time with consecutive nonces, from one
//     dedicated wallet (SYNTHOS_REWARD_KEEPER_PRIVATE_KEY). The keeper never
//     uses the early-access sale wallet. Nothing new is sent while an earlier
//     transfer is unconfirmed, and a transfer unconfirmed for over an hour
//     pauses the keeper until a person checks it.
//  3. Only nodes the founder approved are paid, and only to the payout
//     address written in that approval (SYNTHOS_REWARD_APPROVED_FILE), so a
//     changed payout address can never redirect a reward.
//  4. The first payment (10,000 SYN, the validator bond) needs 30 days of
//     verified uptime; each later monthly one needs 27 more days of it. The
//     node must be online when paid.
//  5. Total payouts per calendar month are capped
//     (SYNTHOS_REWARD_MONTHLY_CAP_SYN).
//
// Settings: the key comes from SYNTHOS_REWARD_KEEPER_PRIVATE_KEY or the file
// SYNTHOS_REWARD_KEEPER_KEY_FILE (default /etc/synthos/reward-keeper.key,
// made by `cloudless-registry -new-reward-key <path>`); the monthly cap from
// SYNTHOS_REWARD_MONTHLY_CAP_SYN or "monthly_cap_syn" in the approval file.
// With any setting missing, the keeper pays nothing and only logs who would
// qualify. GET /api/rewards publishes every payout and the keeper's state.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"synthos-collective/internal/chain"
	"synthos-collective/internal/wallet"
)

const (
	rewardKeeperEvery  = 2 * time.Minute
	rewardFirstUptime  = rewardEpoch         // 30 days before the first payment
	rewardRepeatUptime = 27 * 24 * time.Hour // new uptime needed for each later payment
	rewardStuckAfter   = time.Hour           // unconfirmed this long: pause and ask a person
	rewardTxFeeSYN     = 1
	// rewardActivationSYN is an operator's first payment: exactly the
	// minimum validator bond, so a month of uptime earns the bond.
	rewardActivationSYN   = validatorMinimumBondSYN
	payoutStatusSubmitted = "submitted"
	payoutStatusConfirmed = "confirmed"
	payoutStatusStuck     = "stuck"
)

var payoutAddressPattern = regexp.MustCompile(`^0x[0-9a-f]{40}$`)

// normalizePayoutAddress returns a lowercase Synthos Collective address, or
// "" when the input isn't one.
func normalizePayoutAddress(addr string) string {
	addr = strings.ToLower(strings.TrimSpace(addr))
	if !payoutAddressPattern.MatchString(addr) {
		return ""
	}
	return addr
}

// rewardPayout is one ledger line. It is created when a transfer is accepted
// into the mempool and becomes "confirmed" only once it is on chain.
type rewardPayout struct {
	ID          string `json:"id"`
	NodeID      string `json:"node_id"`
	Address     string `json:"address"`
	AmountSYN   uint64 `json:"amount_syn"`
	Epoch       string `json:"epoch"`
	Nonce       uint64 `json:"nonce"`
	TxID        string `json:"tx_id"`
	Status      string `json:"status"`
	UptimeMS    int64  `json:"uptime_ms"`
	SubmittedAt int64  `json:"submitted_at"`
	ConfirmedAt int64  `json:"confirmed_at,omitempty"`
}

type rewardKeeperConfig struct {
	Enabled    bool
	Reason     string // why the keeper is not paying, when !Enabled
	Wallet     *wallet.Wallet
	Address    string
	MonthlyCap uint64
	Approved   map[string]string // node ID -> approved payout address
	ChainID    uint64
}

// rewardChain is the slice of the chain the keeper needs; tests fake it.
type rewardChain interface {
	Nonce(address string) (uint64, error)
	Submit(tx chain.Tx) (string, error)
}

type httpRewardChain struct{ api string }

func (c httpRewardChain) Nonce(address string) (uint64, error) {
	return synthosNonce(c.api, address)
}

func (c httpRewardChain) Submit(tx chain.Tx) (string, error) {
	data, err := json.Marshal(tx)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Post(c.api+"/submitTx", "application/json", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("rejected: %s", strings.TrimSpace(string(body)))
	}
	var result struct {
		OK   bool   `json:"ok"`
		TxID string `json:"tx_id"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}
	if !result.OK || result.TxID == "" {
		return "", fmt.Errorf("not accepted: %s", strings.TrimSpace(string(body)))
	}
	return result.TxID, nil
}

// loadRewardKeeperConfig reads the settings on every cycle, so editing the
// approval file takes effect without a restart.
func loadRewardKeeperConfig() rewardKeeperConfig {
	cfg := rewardKeeperConfig{}
	key := strings.TrimSpace(os.Getenv("SYNTHOS_REWARD_KEEPER_PRIVATE_KEY"))
	if key == "" {
		if raw, err := os.ReadFile(env("SYNTHOS_REWARD_KEEPER_KEY_FILE", "/etc/synthos/reward-keeper.key")); err == nil {
			key = strings.TrimSpace(string(raw))
		}
	}
	if key == "" {
		cfg.Reason = "no rewards wallet key (SYNTHOS_REWARD_KEEPER_PRIVATE_KEY or the key file)"
		return cfg
	}
	w, err := wallet.FromPrivateKeyHex(key)
	if err != nil {
		cfg.Reason = "rewards wallet key is invalid"
		return cfg
	}
	addr, err := w.Address()
	if err != nil {
		cfg.Reason = "rewards wallet key is invalid"
		return cfg
	}
	cfg.Wallet, cfg.Address = w, strings.ToLower(string(addr))
	approved, fileCap, err := loadApprovedOperators(env("SYNTHOS_REWARD_APPROVED_FILE", "/etc/synthos/reward-approved.json"))
	if err != nil {
		cfg.Reason = "approval file: " + err.Error()
		return cfg
	}
	cfg.Approved = approved
	capSYN, err := strconv.ParseUint(strings.TrimSpace(os.Getenv("SYNTHOS_REWARD_MONTHLY_CAP_SYN")), 10, 64)
	if err != nil || capSYN == 0 {
		capSYN = fileCap
	}
	if capSYN == 0 {
		cfg.Reason = "no monthly cap (SYNTHOS_REWARD_MONTHLY_CAP_SYN or monthly_cap_syn in the approval file)"
		return cfg
	}
	cfg.MonthlyCap = capSYN
	if cfg.ChainID, err = synthosTxChainID(); err != nil {
		cfg.Reason = err.Error()
		return cfg
	}
	cfg.Enabled = true
	return cfg
}

// loadApprovedOperators reads
// {"monthly_cap_syn":100000,"approved":[{"node_id":"...","payout_address":"0x..."}]}.
func loadApprovedOperators(path string) (map[string]string, uint64, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	var file struct {
		MonthlyCapSYN uint64 `json:"monthly_cap_syn"`
		Approved      []struct {
			NodeID        string `json:"node_id"`
			PayoutAddress string `json:"payout_address"`
		} `json:"approved"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, 0, fmt.Errorf("not valid JSON: %w", err)
	}
	out := map[string]string{}
	for _, a := range file.Approved {
		addr := normalizePayoutAddress(a.PayoutAddress)
		if a.NodeID == "" || addr == "" {
			return nil, 0, fmt.Errorf("entry %q needs a node_id and a 0x payout_address", a.NodeID)
		}
		out[sanitize(a.NodeID)] = addr
	}
	return out, file.MonthlyCapSYN, nil
}

// rewardAmountFor is 10,000 SYN for an operator's first reward and the
// monthly amount after that.
func rewardAmountFor(p peer, now time.Time) uint64 {
	if p.TotalRewardsPaidSYN == 0 && p.RewardUptimeAtPaidMS == 0 {
		return rewardActivationSYN
	}
	return monthlyRewardAmount(now)
}

// createRewardKeeperKey writes a new wallet key (root-only) and returns its
// address. It refuses to overwrite an existing key.
func createRewardKeeperKey(path string) (string, error) {
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("%s already exists; not overwriting", path)
	}
	w, err := wallet.New()
	if err != nil {
		return "", err
	}
	priv, err := w.PrivateKeyHex()
	if err != nil {
		return "", err
	}
	addr, err := w.Address()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir(path), 0o700); err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(priv + "\n"); err != nil {
		f.Close()
		return "", err
	}
	return string(addr), f.Close()
}

func (s *server) startValidatorRewardKeeper() {
	go func() {
		ticker := time.NewTicker(rewardKeeperEvery)
		defer ticker.Stop()
		for range ticker.C {
			s.executeRewardKeeperCycle()
		}
	}()
}

var rewardDryRunLog struct {
	sync.Mutex
	last time.Time
}

func (s *server) executeRewardKeeperCycle() {
	cfg := loadRewardKeeperConfig()
	api := strings.TrimRight(env("SYNTHOS_REWARD_CHAIN_API", "http://127.0.0.1:8080"), "/")
	s.rewardKeeperCycle(time.Now(), cfg, httpRewardChain{api: api})
}

// rewardCandidate is a node that qualifies for this month's reward.
type rewardCandidate struct {
	NodeID   string
	Address  string
	Amount   uint64
	UptimeMS int64
}

func (s *server) rewardKeeperCycle(now time.Time, cfg rewardKeeperConfig, ch rewardChain) {
	epoch := now.UTC().Format("2006-01")

	if !cfg.Enabled {
		s.logDryRun(now, cfg.Reason, epoch)
		return
	}

	// Step 1: confirm earlier transfers against the chain.
	accountNonce, err := ch.Nonce(cfg.Address)
	if err != nil {
		log.Printf("[reward keeper] can't read rewards wallet nonce, skipping cycle: %v", err)
		return
	}
	s.mu.Lock()
	changed := false
	waiting := 0
	nextNonce := accountNonce
	for i := range s.state.RewardPayouts {
		po := &s.state.RewardPayouts[i]
		if po.Status != payoutStatusSubmitted && po.Status != payoutStatusStuck {
			continue
		}
		if po.Nonce < accountNonce {
			po.Status = payoutStatusConfirmed
			po.ConfirmedAt = now.UnixMilli()
			if p, ok := s.state.Peers[po.NodeID]; ok {
				p.LastRewardPaidEpoch = po.Epoch
				p.LastRewardPaidAt = po.ConfirmedAt
				p.TotalRewardsPaidSYN += po.AmountSYN
				p.LastRewardTxHash = po.TxID
				p.RewardUptimeAtPaidMS = po.UptimeMS
				s.state.Peers[po.NodeID] = p
			}
			log.Printf("[reward keeper] confirmed %d SYN to %s for node %s (%s), tx %s", po.AmountSYN, po.Address, po.NodeID, po.Epoch, po.TxID)
			changed = true
			continue
		}
		waiting++
		if po.Status == payoutStatusSubmitted && now.Sub(time.UnixMilli(po.SubmittedAt)) > rewardStuckAfter {
			po.Status = payoutStatusStuck
			log.Printf("[reward keeper] PAUSED: transfer %s (nonce %d) unconfirmed for over an hour; check the chain before clearing it", po.TxID, po.Nonce)
			changed = true
		}
	}
	if changed {
		if err := s.persist(); err != nil {
			log.Printf("[reward keeper] persist warning: %v", err)
		}
	}
	if waiting > 0 {
		// Never send while an earlier transfer is unconfirmed: its nonce is
		// still taken, and resending could pay someone twice.
		s.mu.Unlock()
		return
	}

	// Step 2: pick this month's qualifying, approved nodes, within the cap.
	candidates := s.rewardCandidates(now, cfg, epoch)
	s.mu.Unlock()
	if len(candidates) == 0 {
		return
	}

	// Step 3: send one at a time with consecutive nonces. Record each one
	// as submitted (not paid); confirmation happens in a later cycle.
	for _, c := range candidates {
		tx := chain.Tx{
			ChainID:   cfg.ChainID,
			From:      chain.Address(cfg.Address),
			To:        chain.Address(c.Address),
			Amount:    c.Amount,
			Fee:       rewardTxFeeSYN,
			Nonce:     nextNonce,
			PublicKey: mustPublicKeyHex(cfg.Wallet),
			Metadata: []chain.KeyValuePair{
				{Key: "reward_keeper", Value: "validator_monthly"},
				{Key: "node_id", Value: c.NodeID},
				{Key: "epoch", Value: epoch},
			},
		}
		if err := tx.Sign(cfg.Wallet.Private); err != nil {
			log.Printf("[reward keeper] signing failed, stopping: %v", err)
			return
		}
		txID, err := ch.Submit(tx)
		if err != nil {
			// Nothing is recorded: the node stays unpaid and is tried again
			// next cycle.
			log.Printf("[reward keeper] transfer to %s for node %s failed, stopping this cycle: %v", c.Address, c.NodeID, err)
			return
		}
		s.mu.Lock()
		s.state.RewardPayouts = append(s.state.RewardPayouts, rewardPayout{
			ID:          "reward-" + c.NodeID + "-" + epoch,
			NodeID:      c.NodeID,
			Address:     c.Address,
			AmountSYN:   c.Amount,
			Epoch:       epoch,
			Nonce:       nextNonce,
			TxID:        txID,
			Status:      payoutStatusSubmitted,
			UptimeMS:    c.UptimeMS,
			SubmittedAt: now.UnixMilli(),
		})
		if err := s.persist(); err != nil {
			log.Printf("[reward keeper] persist warning: %v", err)
		}
		s.mu.Unlock()
		log.Printf("[reward keeper] submitted %d SYN to %s for node %s (%s), tx %s, nonce %d", c.Amount, c.Address, c.NodeID, epoch, txID, nextNonce)
		nextNonce++
	}
}

// rewardCandidates must be called with s.mu held.
func (s *server) rewardCandidates(now time.Time, cfg rewardKeeperConfig, epoch string) []rewardCandidate {
	var spent uint64
	paidNodes := map[string]bool{}
	for _, po := range s.state.RewardPayouts {
		if po.Epoch == epoch {
			spent += po.AmountSYN
			paidNodes[po.NodeID] = true
		}
	}
	peers := make([]peer, 0, len(s.state.Peers))
	for _, p := range s.state.Peers {
		peers = append(peers, p)
	}
	sort.Slice(peers, func(i, j int) bool { return peers[i].Name < peers[j].Name })
	markSharedHardware(peers, now)

	var out []rewardCandidate
	for _, p := range peers {
		reason := rewardIneligibleReason(p, now, epoch, paidNodes[p.Name])
		approvedAddr, approved := cfg.Approved[p.Name]
		if reason == "" && !approved {
			reason = "not approved"
		}
		if reason == "" && p.PayoutAddress != "" && normalizePayoutAddress(p.PayoutAddress) != approvedAddr {
			reason = "payout address differs from the approved one"
			log.Printf("[reward keeper] node %s: payout address %q differs from approved %s; not paying", p.Name, p.PayoutAddress, approvedAddr)
		}
		if reason != "" {
			continue
		}
		amount := rewardAmountFor(p, now)
		if spent+amount > cfg.MonthlyCap {
			log.Printf("[reward keeper] monthly cap of %d SYN reached for %s; %s waits", cfg.MonthlyCap, epoch, p.Name)
			break
		}
		spent += amount
		out = append(out, rewardCandidate{NodeID: p.Name, Address: approvedAddr, Amount: amount, UptimeMS: p.VerifiedUptimeMS})
	}
	return out
}

// rewardIneligibleReason returns "" when p has earned this month's reward,
// apart from approval.
func rewardIneligibleReason(p peer, now time.Time, epoch string, paidThisEpoch bool) string {
	switch {
	case paidThisEpoch || p.LastRewardPaidEpoch == epoch:
		return "already paid this month"
	case p.HostedProofSession || !isRealSignedHeartbeat(p):
		return "no real signed heartbeats"
	case peerIsStale(p, now):
		return "offline"
	case p.SharedHardwareWith != "" || p.SharedHouseholdWith != "":
		return "shares a machine or household with another node"
	case p.VerifiedUptimeMS < int64(rewardFirstUptime/time.Millisecond):
		return "under 30 days of verified uptime"
	case p.RewardUptimeAtPaidMS > 0 && p.VerifiedUptimeMS-p.RewardUptimeAtPaidMS < int64(rewardRepeatUptime/time.Millisecond):
		return "under 27 days of new uptime since the last reward"
	}
	return ""
}

func (s *server) logDryRun(now time.Time, reason, epoch string) {
	rewardDryRunLog.Lock()
	defer rewardDryRunLog.Unlock()
	if now.Sub(rewardDryRunLog.last) < time.Hour {
		return
	}
	rewardDryRunLog.last = now
	s.mu.RLock()
	defer s.mu.RUnlock()
	peers := make([]peer, 0, len(s.state.Peers))
	for _, p := range s.state.Peers {
		peers = append(peers, p)
	}
	markSharedHardware(peers, now)
	var names []string
	for _, p := range peers {
		if rewardIneligibleReason(p, now, epoch, false) == "" {
			names = append(names, p.Name)
		}
	}
	sort.Strings(names)
	log.Printf("[reward keeper] not paying (%s). Nodes that would qualify in %s, pending approval: %v", reason, epoch, names)
}

func mustPublicKeyHex(w *wallet.Wallet) string {
	pub, err := w.PublicKeyHex()
	if err != nil {
		return ""
	}
	return pub
}

// handleAPIRewards publishes the keeper's state and every payout, so anyone
// can check each one against the chain.
func (s *server) handleAPIRewards(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cfg := loadRewardKeeperConfig()
	now := time.Now()
	epoch := now.UTC().Format("2006-01")
	s.mu.RLock()
	payouts := append([]rewardPayout(nil), s.state.RewardPayouts...)
	s.mu.RUnlock()
	var spent uint64
	for _, po := range payouts {
		if po.Epoch == epoch {
			spent += po.AmountSYN
		}
	}
	sort.Slice(payouts, func(i, j int) bool { return payouts[i].SubmittedAt > payouts[j].SubmittedAt })
	body := map[string]any{
		"enabled":                        cfg.Enabled,
		"epoch":                          epoch,
		"first_reward_syn":               rewardActivationSYN,
		"monthly_reward_syn":             monthlyRewardAmount(now),
		"first_payment_after_days":       int(rewardFirstUptime.Hours() / 24),
		"later_payments_after_days":      int(rewardRepeatUptime.Hours() / 24),
		"requires_founder_approval":      true,
		"paid_or_pending_this_month_syn": spent,
		"payouts":                        payouts,
	}
	if cfg.Enabled {
		body["rewards_wallet"] = cfg.Address
		body["monthly_cap_syn"] = cfg.MonthlyCap
		body["approved_nodes"] = len(cfg.Approved)
	} else {
		body["not_paying_because"] = cfg.Reason
	}
	writeJSON(w, http.StatusOK, body)
}
