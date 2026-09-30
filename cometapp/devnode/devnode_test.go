package devnode

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	nm "github.com/cometbft/cometbft/node"
	"github.com/cometbft/cometbft/rpc/client/local"
	"github.com/cometbft/cometbft/types"

	"synthos-collective/cometapp/app"
	"synthos-collective/internal/chain"
)

// These tests run real CometBFT nodes (real p2p, real consensus, real
// block production) with the SYNTHOS app, in this process on 127.0.0.1.

const (
	netChainID   = "synthos-devnet-test"
	netTxChainID = 88
	blockEvery   = 150 * time.Millisecond
)

var netStaking = chain.ValidatorStakingParams{
	EnabledFromHeight: 1,
	MinSelfBond:       1_000,
	UnbondingBlocks:   20,
	EpochBlocks:       3,
	SlashFractionBps:  500,
	ReporterRewardBps: 1_000,
}

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

func (w wallet) sign(t *testing.T, to chain.Address, nonce, amount, fee uint64, meta map[string]string) types.Tx {
	t.Helper()
	tx := chain.Tx{ChainID: netTxChainID, From: w.addr, To: to, Amount: amount, Fee: fee, Nonce: nonce,
		PublicKey: "0x" + hex.EncodeToString(w.pub)}
	for k, v := range meta {
		tx.Metadata = append(tx.Metadata, chain.KeyValuePair{Key: k, Value: v})
	}
	if err := tx.Sign(w.priv); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(tx)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

type member struct {
	home     string
	p2pPort  int
	keyHex   string
	operator wallet
	node     *nm.Node
	client   *local.Local
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// newNetwork prepares n homes sharing one genesis. The first `validators`
// of them are genesis validators with `bond` each; every operator and the
// returned user wallet start with 1,000,000 SYN.
func newNetwork(t *testing.T, n, validators int, bond uint64) ([]*member, wallet) {
	t.Helper()
	user := newWallet(t)
	alloc := map[chain.Address]uint64{user.addr: 1_000_000}
	members := make([]*member, n)
	var gvals []app.GenesisValidator
	for i := range members {
		m := &member{home: filepath.Join(t.TempDir(), fmt.Sprintf("node%d", i)), p2pPort: freePort(t), operator: newWallet(t)}
		key, err := ValidatorKey(m.home)
		if err != nil {
			t.Fatal(err)
		}
		m.keyHex = key
		alloc[m.operator.addr] = 1_000_000
		if i < validators {
			gvals = append(gvals, app.GenesisValidator{Operator: m.operator.addr, ConsensusPubKey: key, SelfBond: bond, Moniker: fmt.Sprintf("val%d", i)})
		}
		members[i] = m
	}
	g := app.Genesis{
		Chain:      chain.Genesis{ChainID: netChainID, TxChainID: netTxChainID, Alloc: alloc},
		Staking:    netStaking,
		Validators: gvals,
	}
	genesisTime := time.Now().UTC().Truncate(time.Second)
	for _, m := range members {
		if err := WriteGenesis(m.home, g, genesisTime); err != nil {
			t.Fatal(err)
		}
	}
	return members, user
}

func (m *member) start(t *testing.T, peers []*member) {
	t.Helper()
	var list []string
	for _, p := range peers {
		if p == m {
			continue
		}
		id, err := NodeID(p.home)
		if err != nil {
			t.Fatal(err)
		}
		list = append(list, fmt.Sprintf("%s@127.0.0.1:%d", id, p.p2pPort))
	}
	n, _, err := Start(m.home, Options{
		P2PAddr:         fmt.Sprintf("tcp://127.0.0.1:%d", m.p2pPort),
		PersistentPeers: strings.Join(list, ","),
		BlockInterval:   blockEvery,
	})
	if err != nil {
		t.Fatalf("start %s: %v", m.home, err)
	}
	m.node, m.client = n, local.New(n)
	t.Cleanup(func() { m.stop() })
}

func (m *member) stop() {
	if m.node != nil && m.node.IsRunning() {
		_ = m.node.Stop()
		m.node.Wait()
	}
}

func (m *member) height(t *testing.T) int64 {
	t.Helper()
	st, err := m.client.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return st.SyncInfo.LatestBlockHeight
}

// waitHeight waits until m has committed height h.
func (m *member) waitHeight(t *testing.T, h int64, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for m.height(t) < h {
		if time.Now().After(deadline) {
			t.Fatalf("%s stuck at height %d, wanted %d", filepath.Base(m.home), m.height(t), h)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (m *member) balance(t *testing.T, addr chain.Address) uint64 {
	t.Helper()
	res, err := m.client.ABCIQuery(context.Background(), "account", []byte(addr))
	if err != nil || res.Response.Code != 0 {
		t.Fatalf("account query: %v %s", err, res.Response.Log)
	}
	var acc chain.Account
	if err := json.Unmarshal(res.Response.Value, &acc); err != nil {
		t.Fatal(err)
	}
	return acc.Balance
}

func (m *member) submit(t *testing.T, tx types.Tx) int64 {
	t.Helper()
	res, err := m.client.BroadcastTxCommit(context.Background(), tx)
	if err != nil {
		t.Fatalf("broadcast: %v", err)
	}
	if res.CheckTx.Code != 0 || res.TxResult.Code != 0 {
		t.Fatalf("tx rejected: check=%q exec=%q", res.CheckTx.Log, res.TxResult.Log)
	}
	return res.Height
}

func (m *member) cometValidators(t *testing.T) map[string]int64 {
	t.Helper()
	perPage := 100
	res, err := m.client.Validators(context.Background(), nil, nil, &perPage)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]int64{}
	for _, v := range res.Validators {
		out["0x"+hex.EncodeToString(v.PubKey.Bytes())] = v.VotingPower
	}
	return out
}

// TestSingleNodeDevnet: one validator produces real blocks, executes a
// SYNTHOS transfer, and after a restart carries on from where it stopped.
func TestSingleNodeDevnet(t *testing.T) {
	members, user := newNetwork(t, 1, 1, 10_000)
	m := members[0]
	m.start(t, nil)
	m.waitHeight(t, 2, 20*time.Second)

	dest := newWallet(t).addr
	h := m.submit(t, user.sign(t, dest, 0, 1_234, 10, nil))
	if got := m.balance(t, dest); got != 1_234 {
		t.Fatalf("recipient balance = %d, want 1234", got)
	}
	if got := m.balance(t, user.addr); got != 1_000_000-1_234-10 {
		t.Fatalf("sender balance = %d", got)
	}

	// Restart from disk. A node process that exits releases every file
	// lock, but inside one test process CometBFT keeps one of its
	// databases locked after Stop, so the restarted node runs from a copy
	// of the stopped node's home directory -- exactly what a new process
	// would find on disk.
	m.stop()
	restarted := filepath.Join(t.TempDir(), "node0-restarted")
	if err := copyDir(m.home, restarted); err != nil {
		t.Fatal(err)
	}
	m.home = restarted
	m.start(t, nil)
	if got := m.height(t); got < h {
		t.Fatalf("restarted node is at height %d, below the %d it had committed", got, h)
	}
	m.waitHeight(t, h+2, 20*time.Second)
	if got := m.balance(t, dest); got != 1_234 {
		t.Fatalf("after restart recipient balance = %d, want 1234", got)
	}
}

// TestValidatorNetwork: four validators on separate nodes reach consensus;
// a fifth node that nobody approved joins the validator set just by
// bonding SYN; then one original validator goes offline and the chain
// keeps producing blocks.
func TestValidatorNetwork(t *testing.T) {
	if testing.Short() {
		t.Skip("multi-node network test")
	}
	members, _ := newNetwork(t, 5, 4, 10_000)
	for _, m := range members {
		m.start(t, members)
	}
	for _, m := range members {
		m.waitHeight(t, 3, 30*time.Second)
	}

	// All five nodes agree on the chain.
	h := members[0].height(t) - 1
	var first string
	for i, m := range members {
		blk, err := m.client.Block(context.Background(), &h)
		if err != nil {
			t.Fatalf("node%d block %d: %v", i, h, err)
		}
		if i == 0 {
			first = blk.Block.AppHash.String()
		} else if blk.Block.AppHash.String() != first {
			t.Fatalf("node%d has a different app hash at height %d", i, h)
		}
	}

	joiner := members[4]
	if _, ok := joiner.cometValidators(t)[joiner.keyHex]; ok {
		t.Fatal("joiner is a validator before bonding")
	}
	bondHeight := joiner.submit(t, joiner.operator.sign(t, joiner.operator.addr, 0, 8_000, 10, map[string]string{
		"type": "validator_bond", "consensus_pub_key": joiner.keyHex, "moniker": "anyone",
	}))
	// The set updates at the next epoch boundary and CometBFT applies it
	// two blocks later.
	deadline := time.Now().Add(30 * time.Second)
	for {
		if p, ok := joiner.cometValidators(t)[joiner.keyHex]; ok {
			if p != 8_000 {
				t.Fatalf("joiner voting power = %d, want 8000", p)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("joiner never entered the validator set (bonded at height %d, now %d)", bondHeight, joiner.height(t))
		}
		time.Sleep(100 * time.Millisecond)
	}

	// One original validator goes offline. The rest hold 38,000 of 48,000
	// voting power (more than 2/3), so blocks must keep coming.
	members[1].stop()
	before := members[0].height(t)
	members[0].waitHeight(t, before+5, 30*time.Second)
	joiner.waitHeight(t, before+5, 30*time.Second)
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if os.IsNotExist(err) {
			return nil // a temp file CometBFT renamed away mid-walk
		}
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if info.Name() == "LOCK" {
			return nil // leveldb lock files; a fresh process starts without them
		}
		in, err := os.Open(path)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	})
}
