// Package legacyapi serves the HTTP API of the original synthosd node
// (/status, /account, /submitTx, /blocks, ...) from a node running on
// CometBFT, with the same paths and JSON shapes. The website, wallets,
// explorer, agents and tools that talk to the old network can point at a
// CometBFT node without changes.
//
// Reads come from the app's committed state. Writes (/submitTx and the
// typed citizen/governance endpoints) go into CometBFT's mempool, which
// gossips them to the validators; they take effect once a block includes
// them, exactly as on the old network.
package legacyapi

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	ctypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/cometbft/cometbft/types"

	"synthos-collective/cometapp/app"
	"synthos-collective/internal/agent"
	"synthos-collective/internal/chain"
)

// Comet is the part of CometBFT's RPC client the API uses. The in-process
// client (rpc/client/local) satisfies it.
type Comet interface {
	Status(context.Context) (*ctypes.ResultStatus, error)
	Block(ctx context.Context, height *int64) (*ctypes.ResultBlock, error)
	BlockResults(ctx context.Context, height *int64) (*ctypes.ResultBlockResults, error)
	UnconfirmedTxs(ctx context.Context, limit *int) (*ctypes.ResultUnconfirmedTxs, error)
	NetInfo(context.Context) (*ctypes.ResultNetInfo, error)
	BroadcastTxSync(context.Context, types.Tx) (*ctypes.ResultBroadcastTx, error)
}

// Server serves the legacy API for one node.
type Server struct {
	App   *app.App
	Comet Comet

	// MaxBodyBytes caps request bodies (default 1 MiB).
	MaxBodyBytes int64
	// RequestsPerSecond is the per-client rate limit (default 50; a
	// negative value disables it).
	RequestsPerSecond int
	// TrustForwardedFor keys the rate limit on the first X-Forwarded-For
	// address. Only set it behind a proxy that overwrites that header
	// (Render, a load balancer), or clients can pick their own key.
	TrustForwardedFor bool
	// Faucet, when set, serves GET/POST /faucet (test networks only).
	Faucet *Faucet

	limiter *limiter
}

const (
	defaultMaxBody = 1 << 20
	defaultRPS     = 50
	// MaxBlocksPerPage bounds one /blocks response.
	MaxBlocksPerPage = 1000
	// DefaultBlocksPage is how many recent blocks /blocks returns when the
	// caller gives no "from".
	DefaultBlocksPage = 100
	requestTimeout    = 10 * time.Second
)

// Handler returns the HTTP handler with every legacy route.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	get := map[string]http.HandlerFunc{
		"/health":               s.health,
		"/status":               s.status,
		"/sync/status":          s.syncStatus,
		"/account":              s.account,
		"/balance":              s.balance,
		"/blocks":               s.blocks,
		"/mempool":              s.mempool,
		"/peers":                s.peers,
		"/capabilities":         s.capabilities,
		"/immune/status":        s.immuneStatus,
		"/citizen/status":       s.citizenStatus,
		"/governance/proposals": s.governanceProposals,
		"/economy/stats":        s.economyStats,
		"/validators/staking":   s.validatorStaking,
		"/dex/pools":            s.dexPools,
		"/bridge/status":        s.bridgeStatus,
		"/bridge/events":        s.bridgeEvents,
	}
	for path, h := range get {
		mux.HandleFunc(path, h)
	}
	mux.HandleFunc("/submitTx", s.submit(""))
	mux.HandleFunc("/faucet", s.faucet)
	mux.HandleFunc("/simulate/tx", s.simulateTx)
	for path, txType := range map[string]string{
		"/citizen/stake":         "citizen_stake",
		"/citizen/unstake":       "citizen_unstake",
		"/citizen/claim-rewards": "citizen_claim_rewards",
		"/governance/propose":    "governance_propose",
		"/governance/vote":       "governance_vote",
		"/governance/execute":    "governance_execute",
	} {
		mux.HandleFunc(path, s.submit(txType))
	}
	// Block production and voting belong to CometBFT now; the old
	// node-to-node endpoints have nothing to do on this network.
	for _, path := range []string{"/proposeBlock", "/gossip/block", "/consensus/propose", "/simulate/block"} {
		mux.HandleFunc(path, gone("blocks are proposed and voted on by CometBFT on this network; submit transactions to /submitTx"))
	}
	mux.HandleFunc("/dex/swap", gone("DEX mutation is disabled until swaps are signed transactions finalized by consensus"))
	for _, path := range []string{"/dex/quote", "/communicator/send", "/communicator/inbox", "/aen/status", "/enforcer/status"} {
		mux.HandleFunc(path, notYet)
	}

	s.limiter = newLimiter(s.rps())
	var h http.Handler = mux
	h = s.rateLimit(h)
	h = s.bodyLimit(h)
	return cors(h)
}

// ---- reads ---------------------------------------------------------------

func (s *Server) read(w http.ResponseWriter, fn func(app.Committed)) bool {
	if !s.App.Read(fn) {
		http.Error(w, "chain not initialized yet", http.StatusServiceUnavailable)
		return false
	}
	return true
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"ok": true, "service": "synthos-rpc", "engine": "cometbft"})
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	cs, err := s.Comet.Status(ctx)
	if err != nil {
		http.Error(w, "consensus engine unavailable: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	peers := s.peerList(ctx)
	body := map[string]any{}
	if !s.read(w, func(c app.Committed) {
		body = map[string]any{
			"chain_id":          c.ChainID,
			"tx_chain_id":       c.TxChainID,
			"height":            c.Height,
			"tip":               hexOf(cs.SyncInfo.LatestBlockHash),
			"state_root":        c.AppHash,
			"immune":            c.State.ImmuneStatus(),
			"peers":             peers,
			"capabilities":      agent.CoreCapabilities(),
			"immune_capable":    true,
			"engine":            "cometbft",
			"node_id":           string(cs.NodeInfo.DefaultNodeID),
			"moniker":           cs.NodeInfo.Moniker,
			"catching_up":       cs.SyncInfo.CatchingUp,
			"latest_block_time": cs.SyncInfo.LatestBlockTime.UTC().Format(time.RFC3339),
			"validator": map[string]any{
				"address":      hexOf(cs.ValidatorInfo.Address),
				"voting_power": cs.ValidatorInfo.VotingPower,
			},
		}
	}) {
		return
	}
	writeJSON(w, body)
}

func (s *Server) syncStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	cs, err := s.Comet.Status(ctx)
	if err != nil {
		http.Error(w, "consensus engine unavailable: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, map[string]any{
		"ok":          true,
		"height":      cs.SyncInfo.LatestBlockHeight,
		"catching_up": cs.SyncInfo.CatchingUp,
		"synced":      !cs.SyncInfo.CatchingUp,
	})
}

func (s *Server) account(w http.ResponseWriter, r *http.Request) {
	addr := r.URL.Query().Get("address")
	if addr == "" {
		http.Error(w, "missing address", http.StatusBadRequest)
		return
	}
	var acc chain.Account
	if s.read(w, func(c app.Committed) { acc = c.State.Get(chain.Address(addr)) }) {
		writeJSON(w, map[string]any{"address": addr, "balance": acc.Balance, "nonce": acc.Nonce, "assets": acc.Assets})
	}
}

func (s *Server) balance(w http.ResponseWriter, r *http.Request) {
	addr := r.URL.Query().Get("address")
	if addr == "" {
		http.Error(w, "missing address", http.StatusBadRequest)
		return
	}
	var bal uint64
	if s.read(w, func(c app.Committed) { bal = c.State.Get(chain.Address(addr)).Balance }) {
		writeJSON(w, map[string]any{"address": addr, "balance": bal})
	}
}

func (s *Server) capabilities(w http.ResponseWriter, r *http.Request) {
	body := map[string]any{"capabilities": agent.CoreCapabilities(), "immune_capable": true}
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	if cs, err := s.Comet.Status(ctx); err == nil {
		body["agent_id"] = string(cs.NodeInfo.DefaultNodeID)
		body["public_key"] = hexOf(cs.ValidatorInfo.PubKey.Bytes())
	}
	writeJSON(w, body)
}

func (s *Server) immuneStatus(w http.ResponseWriter, r *http.Request) {
	var st chain.ImmuneStats
	if s.read(w, func(c app.Committed) { st = c.State.ImmuneStatus() }) {
		writeJSON(w, st)
	}
}

func (s *Server) citizenStatus(w http.ResponseWriter, r *http.Request) {
	addr := r.URL.Query().Get("address")
	if addr == "" {
		http.Error(w, "missing address", http.StatusBadRequest)
		return
	}
	a := chain.Address(addr)
	var body map[string]any
	if s.read(w, func(c app.Committed) {
		body = map[string]any{
			"ok":                       true,
			"address":                  addr,
			"stake":                    c.State.GetCitizenStake(a),
			"pending_rewards":          c.State.PendingCitizenRewards(a, time.Now().Unix()),
			"reward_rate_bps_per_year": c.State.CitizenRewardRateBpsPerYear,
			"rewards_configured":       c.State.TreasuryAddress != "" && c.State.CitizenRewardRateBpsPerYear != 0,
		}
	}) {
		writeJSON(w, body)
	}
}

func (s *Server) governanceProposals(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	var body map[string]any
	notFound := false
	if !s.read(w, func(c app.Committed) {
		st := c.State
		if st.GovernanceFounder == "" {
			body = map[string]any{"ok": true, "proposals": []chain.Proposal{}, "governance_configured": false}
			return
		}
		total := st.TotalStake()
		if id != "" {
			p, ok := st.GetGovernanceProposal(id)
			if !ok {
				notFound = true
				return
			}
			passed, _ := st.GovernanceProposalPassed(id, total)
			body = map[string]any{"ok": true, "proposal": p, "passed": passed, "total_stake": total,
				"quorum_basis_points": chain.GovernanceQuorumBasisPoints}
			return
		}
		body = map[string]any{
			"ok":                    true,
			"governance_configured": true,
			"proposals":             st.SnapshotGovernanceProposals(),
			"total_stake":           total,
			"quorum_basis_points":   chain.GovernanceQuorumBasisPoints,
			"founder_address":       st.GovernanceFounder,
			"treasury_address":      st.TreasuryAddress,
		}
	}) {
		return
	}
	if notFound {
		http.Error(w, "proposal not found", http.StatusNotFound)
		return
	}
	writeJSON(w, body)
}

func (s *Server) economyStats(w http.ResponseWriter, r *http.Request) {
	mempoolSize := 0
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	one := 1
	if res, err := s.Comet.UnconfirmedTxs(ctx, &one); err == nil {
		mempoolSize = res.Total
	}
	var body map[string]any
	if !s.read(w, func(c app.Committed) {
		st := c.State
		circulating := st.TotalStake()
		outstanding := uint64(0)
		if chain.MAX_SUPPLY > circulating {
			outstanding = chain.MAX_SUPPLY - circulating
		}
		body = map[string]any{
			"ok":                     true,
			"chain_id":               c.ChainID,
			"height":                 c.Height,
			"max_supply":             uint64(chain.MAX_SUPPLY),
			"circulating_supply":     circulating,
			"uncirculated_or_burned": outstanding,
			"burn_percent_of_fees":   chain.BURN_PERCENT,
			"account_count":          st.AccountCount(),
			"mempool_size":           mempoolSize,
			"dex_pools":              []any{},
			"validator_stake_locked": st.TotalValidatorLocked(),
		}
		if st.TreasuryAddress != "" {
			body["treasury_address"] = st.TreasuryAddress
			body["treasury_balance"] = st.Get(st.TreasuryAddress).Balance
			body["governance_quorum_basis_points"] = chain.GovernanceQuorumBasisPoints
		}
	}) {
		return
	}
	writeJSON(w, body)
}

func (s *Server) validatorStaking(w http.ResponseWriter, r *http.Request) {
	op := r.URL.Query().Get("operator")
	var body map[string]any
	missing := false
	if !s.read(w, func(c app.Committed) {
		st := c.State
		if op != "" {
			rec, ok := st.GetValidator(chain.Address(op))
			if !ok {
				missing = true
				return
			}
			body = map[string]any{"ok": true, "validator": rec}
			return
		}
		records := st.ValidatorRecords()
		sort.Slice(records, func(i, j int) bool { return records[i].Operator < records[j].Operator })
		next := uint64(c.Height) + 1
		body = map[string]any{
			"ok":                 true,
			"params":             c.Staking,
			"enabled":            c.Staking.EnabledAt(next),
			"next_block_height":  next,
			"active_set":         st.ActiveValidatorSet(c.Staking),
			"authorizing_set":    st.AuthorizingValidatorSet(),
			"validators":         records,
			"total_locked":       st.TotalValidatorLocked(),
			"consensus_uses_set": true,
			"note":               "active_set is what stake implies right now; authorizing_set is the set last handed to CometBFT (updated every epoch_blocks blocks, and at once when a validator is slashed), which is what signs blocks.",
		}
	}) {
		return
	}
	if missing {
		http.Error(w, "no validator registered for this operator", http.StatusNotFound)
		return
	}
	writeJSON(w, body)
}

func (s *Server) dexPools(w http.ResponseWriter, r *http.Request) {
	// The old node's DEX pools lived in each node's memory, outside
	// consensus state, so there is nothing agreed-on to report yet.
	writeJSON(w, map[string]any{"ok": true, "pools": []any{}})
}

func (s *Server) bridgeStatus(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if s.read(w, func(c app.Committed) {
		body = map[string]any{"ok": true, "chain": c.ChainID, "height": c.Height, "bridge": c.State.BridgeStatus(),
			"updated": time.Now().UTC().Format(time.RFC3339)}
	}) {
		writeJSON(w, body)
	}
}

func (s *Server) bridgeEvents(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	eventType := strings.TrimSpace(r.URL.Query().Get("type"))
	var body map[string]any
	if s.read(w, func(c app.Committed) {
		out := make([]chain.BridgeRecord, 0, limit)
		for _, ev := range c.State.BridgeEventsSnapshot() {
			if eventType != "" && ev.Type != eventType {
				continue
			}
			out = append(out, ev)
			if len(out) >= limit {
				break
			}
		}
		body = map[string]any{"ok": true, "chain": c.ChainID, "height": c.Height, "events": out, "count": len(out)}
	}) {
		writeJSON(w, body)
	}
}

// ---- blocks, mempool, peers ------------------------------------------------

// Block is a CometBFT block in the old node's block JSON shape.
type Block struct {
	Header    BlockHeader `json:"header"`
	Tx        []chain.Tx  `json:"tx"`
	Hash      string      `json:"hash"`
	Finalized bool        `json:"finalized"`
	// Failed lists indexes into Tx of transactions the block included but
	// that failed when executed (they changed nothing, not even the fee).
	Failed []int `json:"failed_tx,omitempty"`
}

// BlockHeader mirrors the old header fields.
type BlockHeader struct {
	Height     int64  `json:"height"`
	ParentHash string `json:"parent_hash"`
	Timestamp  string `json:"timestamp"`
	ProposerID string `json:"proposer_id"`
	// ProposerAddress is the proposer's CometBFT consensus address.
	ProposerAddress string `json:"proposer_address"`
	TxMerkleRoot    string `json:"tx_merkle_root"`
	// StateRoot is the state after this block, as on the old chain.
	// (CometBFT's own header carries the state *before* the block.)
	StateRoot string `json:"state_root,omitempty"`
}

// blocks: /blocks?from=H&limit=N. With no "from" it returns the most
// recent DefaultBlocksPage blocks (the old node returned the entire
// chain, which grows without bound).
func (s *Server) blocks(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*requestTimeout)
	defer cancel()
	cs, err := s.Comet.Status(ctx)
	if err != nil {
		http.Error(w, "consensus engine unavailable: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	latest, earliest := cs.SyncInfo.LatestBlockHeight, cs.SyncInfo.EarliestBlockHeight
	if earliest < 1 {
		earliest = 1
	}
	limit := DefaultBlocksPage
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}
		limit = n
	}
	if limit > MaxBlocksPerPage {
		limit = MaxBlocksPerPage
	}
	from := latest - int64(limit) + 1
	if raw := r.URL.Query().Get("from"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 0 {
			http.Error(w, "invalid from", http.StatusBadRequest)
			return
		}
		from = n
	}
	if from < earliest {
		from = earliest
	}
	names := s.proposerNames()
	out := make([]Block, 0, limit)
	for h := from; h <= latest && len(out) < limit; h++ {
		b, err := s.block(ctx, h, names)
		if err != nil {
			http.Error(w, fmt.Sprintf("block %d: %v", h, err), http.StatusInternalServerError)
			return
		}
		out = append(out, b)
	}
	writeJSON(w, map[string]any{"blocks": out, "count": len(out), "latest_height": latest, "earliest_height": earliest})
}

func (s *Server) block(ctx context.Context, h int64, names map[string]string) (Block, error) {
	rb, err := s.Comet.Block(ctx, &h)
	if err != nil {
		return Block{}, err
	}
	blk := rb.Block
	proposer := hexOf(blk.ProposerAddress)
	out := Block{
		Header: BlockHeader{
			Height:          blk.Height,
			ParentHash:      hexOf(blk.LastBlockID.Hash),
			Timestamp:       blk.Time.UTC().Format(time.RFC3339Nano),
			ProposerID:      proposer,
			ProposerAddress: proposer,
			TxMerkleRoot:    hexOf(blk.DataHash),
		},
		Tx:        make([]chain.Tx, 0, len(blk.Txs)),
		Hash:      hexOf(rb.BlockID.Hash),
		Finalized: true, // CometBFT commits a block only once 2/3+ of stake signed it
	}
	if name, ok := names[strings.ToLower(proposer)]; ok {
		out.Header.ProposerID = name
	}
	for _, raw := range blk.Txs {
		var tx chain.Tx
		if json.Unmarshal(raw, &tx) != nil {
			tx = chain.Tx{ID: hexOf(raw.Hash())} // undecodable bytes: show the hash only
		}
		out.Tx = append(out.Tx, tx)
	}
	if res, err := s.Comet.BlockResults(ctx, &h); err == nil {
		out.Header.StateRoot = hexOf(res.AppHash)
		for i, r := range res.TxsResults {
			if r.Code != 0 {
				out.Failed = append(out.Failed, i)
			}
		}
	}
	return out, nil
}

// proposerNames maps registered validators' consensus addresses (0x hex,
// lower case) to their moniker, or operator address when unnamed.
func (s *Server) proposerNames() map[string]string {
	names := map[string]string{}
	s.App.Read(func(c app.Committed) {
		for _, v := range c.State.ValidatorRecords() {
			addr, err := app.ConsensusAddress(v.ConsensusPubKey)
			if err != nil {
				continue
			}
			name := v.Moniker
			if name == "" {
				name = string(v.Operator)
			}
			names[strings.ToLower(hexOf(addr))] = name
		}
	})
	return names
}

func (s *Server) mempool(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	limit := 100
	res, err := s.Comet.UnconfirmedTxs(ctx, &limit)
	if err != nil {
		http.Error(w, "consensus engine unavailable: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	txs := make([]chain.Tx, 0, len(res.Txs))
	for _, raw := range res.Txs {
		var tx chain.Tx
		if json.Unmarshal(raw, &tx) == nil {
			txs = append(txs, tx)
		}
	}
	writeJSON(w, map[string]any{"size": res.Total, "tx": txs})
}

func (s *Server) peerList(ctx context.Context) []string {
	ni, err := s.Comet.NetInfo(ctx)
	if err != nil {
		return []string{}
	}
	out := make([]string, 0, len(ni.Peers))
	for _, p := range ni.Peers {
		port := ""
		if _, pt, err := net.SplitHostPort(p.NodeInfo.ListenAddr); err == nil {
			port = ":" + pt
		}
		out = append(out, string(p.NodeInfo.DefaultNodeID)+"@"+p.RemoteIP+port)
	}
	sort.Strings(out)
	return out
}

func (s *Server) peers(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	list := s.peerList(ctx)
	writeJSON(w, map[string]any{"self": r.Host, "peers": list, "total": len(list)})
}

// ---- writes --------------------------------------------------------------

// submit decodes a signed transaction and hands it to CometBFT's mempool.
// With requiredType set, the transaction's metadata "type" must match
// (so /citizen/stake can't carry, say, a governance_execute).
func (s *Server) submit(requiredType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		tx, ok := decodeTx(w, r)
		if !ok {
			return
		}
		if requiredType != "" {
			got := ""
			for _, kv := range tx.Metadata {
				if kv.Key == "type" {
					got = kv.Value
					break
				}
			}
			if got != requiredType {
				http.Error(w, fmt.Sprintf("expected a signed transaction with metadata type=%q, got %q", requiredType, got), http.StatusBadRequest)
				return
			}
			if strings.HasPrefix(requiredType, "governance_") {
				configured := false
				s.App.Read(func(c app.Committed) { configured = c.State.GovernanceFounder != "" })
				if !configured {
					http.Error(w, "governance not configured for this deployment", http.StatusServiceUnavailable)
					return
				}
			}
		}
		// Re-encode so every copy of a transaction is byte-identical in
		// the mempool, whatever whitespace or field order the client sent.
		raw, err := json.Marshal(tx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
		defer cancel()
		res, err := s.Comet.BroadcastTxSync(ctx, raw)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if res.Code != 0 {
			http.Error(w, res.Log, http.StatusBadRequest)
			return
		}
		body := map[string]any{"ok": true, "tx_id": tx.ID, "tx_hash": hexOf(res.Hash)}
		if requiredType != "" {
			body["status"] = "submitted, will take effect once mined into a block"
		}
		writeJSON(w, body)
	}
}

func (s *Server) simulateTx(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tx, ok := decodeTx(w, r)
	if !ok {
		return
	}
	res, err := s.App.Simulate(tx)
	writeJSON(w, map[string]any{"ok": err == nil, "result": res})
}

func decodeTx(w http.ResponseWriter, r *http.Request) (chain.Tx, bool) {
	var tx chain.Tx
	if err := json.NewDecoder(r.Body).Decode(&tx); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return tx, false
		}
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return tx, false
	}
	return tx, true
}

// ---- helpers and middleware ------------------------------------------------

func gone(msg string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { http.Error(w, msg, http.StatusGone) }
}

func notYet(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not available on the CometBFT node yet", http.StatusNotImplemented)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func hexOf(b []byte) string { return "0x" + hex.EncodeToString(b) }

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) bodyLimit(next http.Handler) http.Handler {
	max := s.MaxBodyBytes
	if max <= 0 {
		max = defaultMaxBody
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, max)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) rps() int {
	if s.RequestsPerSecond == 0 {
		return defaultRPS
	}
	return s.RequestsPerSecond
}

func (s *Server) rateLimit(next http.Handler) http.Handler {
	if s.rps() < 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.limiter.allow(s.clientKey(r), time.Now()) {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) clientKey(r *http.Request) string {
	if s.TrustForwardedFor {
		// Cloudflare sets this to the real client and overwrites any copy
		// the client sent; X-Forwarded-For is for other proxies.
		if cf := strings.TrimSpace(r.Header.Get("Cf-Connecting-Ip")); cf != "" {
			return cf
		}
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			return strings.TrimSpace(strings.Split(xff, ",")[0])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// limiter is a per-client token bucket: rps tokens a second, bursts of up
// to rps requests.
type limiter struct {
	mu      sync.Mutex
	rps     float64
	buckets map[string]*bucket
	swept   time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newLimiter(rps int) *limiter {
	return &limiter{rps: float64(rps), buckets: map[string]*bucket{}}
}

func (l *limiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.swept) > time.Minute { // forget idle clients
		for k, b := range l.buckets {
			if now.Sub(b.last) > time.Minute {
				delete(l.buckets, k)
			}
		}
		l.swept = now
	}
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.rps, last: now}
		l.buckets[key] = b
	}
	b.tokens += now.Sub(b.last).Seconds() * l.rps
	if b.tokens > l.rps {
		b.tokens = l.rps
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
