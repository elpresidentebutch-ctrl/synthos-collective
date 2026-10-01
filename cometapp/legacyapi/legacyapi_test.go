package legacyapi

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cometbft/cometbft/rpc/client/local"

	"synthos-collective/cometapp/app"
	"synthos-collective/cometapp/devnode"
	"synthos-collective/internal/chain"
)

// These tests run one real CometBFT validator with the SYNTHOS app and hit
// the legacy API over HTTP, the way the website and wallets do.

const (
	testChainID   = "synthos-legacyapi-test"
	testTxChainID = 99
)

type wallet struct {
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
	addr chain.Address
}

func newWallet(t *testing.T) wallet {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return wallet{pub, priv, chain.AddressFromPublicKey(pub)}
}

func (w wallet) tx(t *testing.T, to chain.Address, nonce, amount, fee uint64, meta map[string]string) chain.Tx {
	t.Helper()
	tx := chain.Tx{ChainID: testTxChainID, From: w.addr, To: to, Amount: amount, Fee: fee, Nonce: nonce,
		PublicKey: "0x" + hex.EncodeToString(w.pub), Timestamp: time.Now().Unix()}
	for k, v := range meta {
		tx.Metadata = append(tx.Metadata, chain.KeyValuePair{Key: k, Value: v})
	}
	if err := tx.Sign(w.priv); err != nil {
		t.Fatal(err)
	}
	return tx
}

type env struct {
	t        *testing.T
	url      string
	user     wallet
	operator wallet
	treasury wallet
	founder  wallet
}

func start(t *testing.T, governance bool) *env { t.Helper(); return startWith(t, governance, nil) }

func startWith(t *testing.T, governance bool, faucet *Faucet) *env {
	t.Helper()
	// Not t.TempDir: CometBFT can still be flushing a file into the home
	// directory as the test ends, which makes t.TempDir's cleanup fail.
	dir, err := os.MkdirTemp("", "legacyapi-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	home := filepath.Join(dir, "node")
	key, err := devnode.ValidatorKey(home)
	if err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, user: newWallet(t), operator: newWallet(t), treasury: newWallet(t), founder: newWallet(t)}
	g := app.Genesis{
		Chain: chain.Genesis{ChainID: testChainID, TxChainID: testTxChainID, Alloc: map[chain.Address]uint64{
			e.user.addr: 1_000_000, e.operator.addr: 1_000_000, e.treasury.addr: 50_000_000,
		}},
		Staking: chain.ValidatorStakingParams{EnabledFromHeight: 1, MinSelfBond: 1_000, UnbondingBlocks: 20,
			EpochBlocks: 3, SlashFractionBps: 500, ReporterRewardBps: 1_000},
		Validators: []app.GenesisValidator{{Operator: e.operator.addr, ConsensusPubKey: key, SelfBond: 10_000, Moniker: "entry-1"}},
	}
	if faucet != nil {
		g.Chain.Alloc[faucet.address()] = 1_000_000
	}
	if governance {
		g.Chain.Metadata = map[string]any{
			"treasury_address":                 string(e.treasury.addr),
			"founder_address":                  string(e.founder.addr),
			"citizen_reward_rate_bps_per_year": float64(650),
		}
	}
	if err := devnode.WriteGenesis(home, g, time.Now().UTC().Truncate(time.Second)); err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p2p := l.Addr().String()
	l.Close()
	n, a, err := devnode.Start(home, devnode.Options{P2PAddr: "tcp://" + p2p, BlockInterval: 150 * time.Millisecond, LocalNetwork: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Stop(); n.Wait() })
	srv := httptest.NewServer((&Server{App: a, Comet: local.New(n), RequestsPerSecond: -1, Faucet: faucet, TrustForwardedFor: faucet != nil}).Handler())
	t.Cleanup(srv.Close)
	e.url = srv.URL
	e.waitHeight(2)
	return e
}

func (e *env) get(path string, out any) int {
	e.t.Helper()
	res, err := http.Get(e.url + path)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if out != nil && res.StatusCode == http.StatusOK {
		if err := json.Unmarshal(body, out); err != nil {
			e.t.Fatalf("GET %s: %v\n%s", path, err, body)
		}
	}
	return res.StatusCode
}

func (e *env) post(path string, v any) (int, string) {
	e.t.Helper()
	raw, _ := json.Marshal(v)
	res, err := http.Post(e.url+path, "application/json", bytes.NewReader(raw))
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(body)
}

func (e *env) height() int64 {
	var st struct {
		Height int64 `json:"height"`
	}
	e.get("/status", &st)
	return st.Height
}

func (e *env) waitHeight(h int64) {
	e.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for e.height() < h {
		if time.Now().After(deadline) {
			e.t.Fatalf("stuck at height %d, wanted %d", e.height(), h)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (e *env) balance(addr chain.Address) uint64 {
	var acc struct {
		Balance uint64 `json:"balance"`
	}
	if code := e.get("/balance?address="+string(addr), &acc); code != 200 {
		e.t.Fatalf("/balance: HTTP %d", code)
	}
	return acc.Balance
}

// waitBalance waits for a submitted transaction to land in a block.
func (e *env) waitBalance(addr chain.Address, want uint64) {
	e.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for e.balance(addr) != want {
		if time.Now().After(deadline) {
			e.t.Fatalf("balance of %s = %d, want %d", addr, e.balance(addr), want)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestReadEndpoints(t *testing.T) {
	e := start(t, false)

	var health map[string]any
	if e.get("/health", &health); health["ok"] != true || health["service"] != "synthos-rpc" {
		t.Fatalf("/health = %v", health)
	}

	var st map[string]any
	e.get("/status", &st)
	if st["chain_id"] != testChainID || st["tx_chain_id"] != float64(testTxChainID) {
		t.Fatalf("/status chain ids = %v / %v", st["chain_id"], st["tx_chain_id"])
	}
	if caps, _ := st["capabilities"].([]any); len(caps) != 7 {
		t.Fatalf("/status capabilities = %v, want the seven core capabilities", st["capabilities"])
	}
	if root, _ := st["state_root"].(string); !strings.HasPrefix(root, "0x") || len(root) != 66 {
		t.Fatalf("/status state_root = %v", st["state_root"])
	}

	var acc map[string]any
	e.get("/account?address="+string(e.user.addr), &acc)
	if acc["balance"] != float64(1_000_000) || acc["nonce"] != float64(0) || acc["address"] != string(e.user.addr) {
		t.Fatalf("/account = %v", acc)
	}
	if code := e.get("/account", nil); code != http.StatusBadRequest {
		t.Fatalf("/account without address: HTTP %d", code)
	}

	var vs struct {
		OK         bool                    `json:"ok"`
		Validators []chain.ValidatorRecord `json:"validators"`
		Active     []chain.ActiveValidator `json:"active_set"`
	}
	e.get("/validators/staking", &vs)
	if !vs.OK || len(vs.Validators) != 1 || vs.Validators[0].Operator != e.operator.addr || len(vs.Active) != 1 {
		t.Fatalf("/validators/staking = %+v", vs)
	}

	var gov map[string]any
	e.get("/governance/proposals", &gov)
	if gov["governance_configured"] != false {
		t.Fatalf("/governance/proposals without a founder = %v", gov)
	}

	var econ map[string]any
	e.get("/economy/stats", &econ)
	if econ["ok"] != true || econ["max_supply"] != float64(chain.MAX_SUPPLY) {
		t.Fatalf("/economy/stats = %v", econ)
	}
	for _, path := range []string{"/peers", "/mempool", "/capabilities", "/immune/status", "/dex/pools", "/bridge/status", "/bridge/events", "/sync/status"} {
		if code := e.get(path, nil); code != 200 {
			t.Errorf("GET %s: HTTP %d", path, code)
		}
	}
	// The old node-to-node endpoints are gone; the reason is spelled out.
	if code, body := e.post("/proposeBlock", map[string]any{}); code != http.StatusGone || !strings.Contains(body, "CometBFT") {
		t.Fatalf("/proposeBlock: HTTP %d %s", code, body)
	}
}

func TestSubmitTxAndBlocks(t *testing.T) {
	e := start(t, false)
	dest := newWallet(t).addr

	tx := e.user.tx(t, dest, 0, 1_234, 10, nil)
	code, body := e.post("/submitTx", tx)
	if code != 200 || !strings.Contains(body, tx.ID) {
		t.Fatalf("/submitTx: HTTP %d %s", code, body)
	}
	e.waitBalance(dest, 1_234)
	if got := e.balance(e.user.addr); got != 1_000_000-1_234-10 {
		t.Fatalf("sender balance = %d", got)
	}

	// Rejections come back as HTTP 400 with the chain's reason.
	if code, body := e.post("/submitTx", e.user.tx(t, dest, 0, 1, 1, nil)); code != 400 || !strings.Contains(body, "nonce") {
		t.Fatalf("replayed nonce: HTTP %d %s", code, body)
	}
	forged := e.user.tx(t, dest, 1, 5, 1, nil)
	forged.Amount = 5_000
	if code, _ := e.post("/submitTx", forged); code != 400 {
		t.Fatalf("tampered transaction: HTTP %d", code)
	}

	// The block that carried it shows up in /blocks in the old shape.
	var res struct {
		Blocks []Block `json:"blocks"`
		Count  int     `json:"count"`
	}
	e.get("/blocks?from=1&limit=1000", &res)
	if res.Count == 0 || res.Blocks[0].Header.Height != 1 {
		t.Fatalf("/blocks from 1: count %d", res.Count)
	}
	found := false
	for _, b := range res.Blocks {
		for _, btx := range b.Tx {
			if btx.ID == tx.ID {
				found = true
				if b.Header.ProposerID != "entry-1" || !b.Finalized || b.Hash == "" || b.Header.StateRoot == "" {
					t.Fatalf("block %d header = %+v", b.Header.Height, b.Header)
				}
			}
		}
	}
	if !found {
		t.Fatal("submitted transaction not found in /blocks")
	}
	var page struct {
		Count int `json:"count"`
	}
	e.get("/blocks?from=1&limit=2", &page)
	if page.Count != 2 {
		t.Fatalf("/blocks limit=2 returned %d", page.Count)
	}
	if code := e.get("/blocks?from=-1", nil); code != 400 {
		t.Fatalf("/blocks from=-1: HTTP %d", code)
	}

	// Simulation runs the real rules without changing anything.
	var sim struct {
		OK     bool                   `json:"ok"`
		Result chain.SimulationResult `json:"result"`
	}
	_, simBody := e.post("/simulate/tx", e.user.tx(t, dest, 1, 100, 1, nil))
	if err := json.Unmarshal([]byte(simBody), &sim); err != nil || !sim.OK || sim.Result.ToBalanceAfter != 1_334 {
		t.Fatalf("/simulate/tx = %s", simBody)
	}
	if got := e.balance(dest); got != 1_234 {
		t.Fatalf("simulation changed state: balance %d", got)
	}
}

func TestTypedEndpointsAndGovernance(t *testing.T) {
	e := start(t, true)

	// A /citizen/stake request must carry a citizen_stake transaction.
	transfer := e.user.tx(t, e.user.addr, 0, 10, 1, nil)
	if code, body := e.post("/citizen/stake", transfer); code != 400 || !strings.Contains(body, "citizen_stake") {
		t.Fatalf("type mismatch: HTTP %d %s", code, body)
	}

	stake := e.user.tx(t, e.user.addr, 0, 100_000, 1, map[string]string{"type": "citizen_stake"})
	if code, body := e.post("/citizen/stake", stake); code != 200 {
		t.Fatalf("/citizen/stake: HTTP %d %s", code, body)
	}
	e.waitBalance(e.user.addr, 1_000_000-100_000-1)
	var cs struct {
		Stake struct {
			Amount uint64 `json:"amount"`
		} `json:"stake"`
		Configured bool `json:"rewards_configured"`
	}
	e.get("/citizen/status?address="+string(e.user.addr), &cs)
	if cs.Stake.Amount != 100_000 || !cs.Configured {
		t.Fatalf("/citizen/status = %+v", cs)
	}

	var gov map[string]any
	e.get("/governance/proposals", &gov)
	if gov["governance_configured"] != true || gov["founder_address"] != string(e.founder.addr) {
		t.Fatalf("/governance/proposals = %v", gov)
	}
	if code := e.get("/governance/proposals?id=nope", nil); code != 404 {
		t.Fatalf("unknown proposal: HTTP %d", code)
	}
}

func TestBodyLimitAndRateLimit(t *testing.T) {
	s := &Server{App: &app.App{}, Comet: nil, MaxBodyBytes: 64, RequestsPerSecond: 3}
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	res, err := http.Post(srv.URL+"/submitTx", "application/json", strings.NewReader(`{"memo":"`+strings.Repeat("x", 200)+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body: HTTP %d", res.StatusCode)
	}

	codes := []int{}
	for i := 0; i < 6; i++ {
		r, err := http.Get(srv.URL + "/health")
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		codes = append(codes, r.StatusCode)
	}
	// Burst of 3: the POST above used one token, so two GETs pass.
	if fmt.Sprint(codes) != "[200 200 429 429 429 429]" {
		t.Fatalf("rate limit: %v", codes)
	}
}

func TestLimiterRefills(t *testing.T) {
	l := newLimiter(2)
	now := time.Unix(1000, 0)
	if !l.allow("a", now) || !l.allow("a", now) || l.allow("a", now) {
		t.Fatal("burst of 2 not enforced")
	}
	if !l.allow("b", now) {
		t.Fatal("one client's limit affected another")
	}
	if !l.allow("a", now.Add(600*time.Millisecond)) {
		t.Fatal("bucket did not refill")
	}
}

func TestFaucet(t *testing.T) {
	key, addr, err := NewFaucetKey()
	if err != nil {
		t.Fatal(err)
	}
	e := startWith(t, false, &Faucet{Key: key, Amount: 5_000, MinInterval: time.Millisecond})

	var info struct {
		Address chain.Address `json:"address"`
		Balance uint64        `json:"balance"`
		Amount  uint64        `json:"amount"`
	}
	e.get("/faucet", &info)
	if info.Address != addr || info.Balance != 1_000_000 || info.Amount != 5_000 {
		t.Fatalf("GET /faucet = %+v", info)
	}

	drip := func(to chain.Address, client string) (int, string) {
		raw, _ := json.Marshal(map[string]string{"address": string(to)})
		req, _ := http.NewRequest(http.MethodPost, e.url+"/faucet", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Cf-Connecting-Ip", client)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(body)
	}

	// Two different people, back to back: both paid, consecutive nonces.
	a, b := newWallet(t).addr, newWallet(t).addr
	if code, body := drip(a, "1.1.1.1"); code != 200 {
		t.Fatalf("first drip: %d %s", code, body)
	}
	time.Sleep(5 * time.Millisecond)
	if code, body := drip(b, "2.2.2.2"); code != 200 {
		t.Fatalf("second drip: %d %s", code, body)
	}
	e.waitBalance(a, 5_000)
	e.waitBalance(b, 5_000)

	// Same address again, or same person with a new address: refused.
	if code, _ := drip(a, "3.3.3.3"); code != http.StatusTooManyRequests {
		t.Fatalf("repeat address: HTTP %d", code)
	}
	if code, _ := drip(newWallet(t).addr, "1.1.1.1"); code != http.StatusTooManyRequests {
		t.Fatalf("repeat client: HTTP %d", code)
	}
	if code, _ := drip("0xnot-an-address", "4.4.4.4"); code != http.StatusBadRequest {
		t.Fatalf("bad address: HTTP %d", code)
	}
	if got := e.balance(addr); got != 1_000_000-2*(5_000+1) {
		t.Fatalf("faucet balance %d", got)
	}
}

func TestNoFaucetByDefault(t *testing.T) {
	e := start(t, false)
	if code := e.get("/faucet", nil); code != http.StatusNotFound {
		t.Fatalf("GET /faucet without a faucet: HTTP %d", code)
	}
}
