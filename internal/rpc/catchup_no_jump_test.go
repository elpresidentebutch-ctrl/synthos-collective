package rpc

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"synthos-collective/internal/chain"
)

// A peer whose history has a hole (it serves a recent window, and nothing
// between our tip and that window) must not make us jump. The old
// "fast-forward" catch-up rebuilt our chain around the peer's window
// without the blocks in between: it skipped verifying them, left our own
// history with the same hole, and could move our tip backwards. Live, that
// rolled mainnet back thousands of blocks and stranded a validator.
func TestCatchUpNeverJumpsAcrossMissingBlocks(t *testing.T) {
	genesis := chain.Genesis{
		ChainID:   "test-chain",
		TxChainID: 20260702,
		Alloc:     map[chain.Address]uint64{"0x825fd94aa826da6ce0b4e57487418b72aea09f5e": 100},
	}
	v := newConsensusTestValidator(t, "synthos-validator-12", genesis)
	before := v.chain.Height()
	beforeTip := v.chain.Tip().Hash
	beforeRoot := v.chain.State.Root()

	// The peer claims height 5000 and only serves blocks 4800..5000 (its
	// "hot window"). The blocks it serves are junk: nothing may be applied.
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sync/status":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"chain_id":        "test-chain",
				"height":          5000,
				"earliest_height": 4800,
				"tip":             "0xpeer",
				"state_root":      beforeRoot, // same root: the old code's trigger
			})
		case "/blocks":
			from, _ := strconv.Atoi(r.URL.Query().Get("from"))
			var blocks []*chain.Block
			if from >= 4800 {
				for h := from; h < from+5 && h <= 5000; h++ {
					blocks = append(blocks, &chain.Block{
						Header: chain.BlockHeader{Height: uint64(h), ParentHash: "0xparent", StateRoot: beforeRoot},
						Hash:   "0xjunk" + strconv.Itoa(h),
					})
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"blocks": blocks, "earliest_height": 4800})
		default:
			http.NotFound(w, r)
		}
	}))
	defer peer.Close()

	v.server.PeerURLs = []string{peer.URL}
	if err := v.server.CatchUpOnce(); err != nil {
		t.Fatal(err)
	}
	if got := v.chain.Height(); got != before {
		t.Fatalf("height moved from %d to %d without verified blocks", before, got)
	}
	if v.chain.Tip().Hash != beforeTip || v.chain.EarliestHeight() != 0 {
		t.Fatalf("chain was rebuilt around the peer's window: tip=%s earliest=%d", v.chain.Tip().Hash, v.chain.EarliestHeight())
	}
}
