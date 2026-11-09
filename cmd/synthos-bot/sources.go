package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Sources reads the network. Every call is read-only.
//   - RPC:      the validator's CometBFT RPC (status, validators, commits)
//   - ChainAPI: the validator's synthosd-compatible API (balances)
//   - Registry: the node registry (uptime nodes, node status, block proposers)
type Sources struct {
	RPC      string
	ChainAPI string
	Registry string
	HTTP     *http.Client
}

// Validator is one consensus validator.
type Validator struct {
	Address string // upper-case hex consensus address
	Power   int64
	Name    string // moniker from the registry's block list, when known
}

// Node is one entry in the node registry.
type Node struct {
	Name            string
	Cloud           string
	IP              string
	ValidHeartbeats int64
	RegisteredAt    time.Time
	LastSeen        time.Time
}

// Snapshot is everything the watchdog and the commands look at.
type Snapshot struct {
	Height     int64
	BlockTime  time.Time
	Validators []Validator
	Signed     map[string]bool // consensus address -> signed the latest commit
	Nodes      []Node
	NodesErr   error // the registry can be down while the chain is fine
}

func (s *Sources) getJSON(ctx context.Context, base, path string, out any) error {
	if base == "" {
		return fmt.Errorf("no URL configured for %s", path)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+path, nil)
	if err != nil {
		return err
	}
	client := s.HTTP
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", path, resp.StatusCode)
	}
	return json.Unmarshal(body, out)
}

// Snapshot reads the chain and the registry.
func (s *Sources) Snapshot(ctx context.Context) (*Snapshot, error) {
	var st struct {
		Result struct {
			SyncInfo struct {
				LatestBlockHeight string `json:"latest_block_height"`
				LatestBlockTime   string `json:"latest_block_time"`
			} `json:"sync_info"`
		} `json:"result"`
	}
	if err := s.getJSON(ctx, s.RPC, "/status", &st); err != nil {
		return nil, fmt.Errorf("chain status: %w", err)
	}
	h, err := strconv.ParseInt(st.Result.SyncInfo.LatestBlockHeight, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("chain status: bad height %q", st.Result.SyncInfo.LatestBlockHeight)
	}
	bt, err := time.Parse(time.RFC3339Nano, st.Result.SyncInfo.LatestBlockTime)
	if err != nil {
		return nil, fmt.Errorf("chain status: bad block time %q", st.Result.SyncInfo.LatestBlockTime)
	}
	snap := &Snapshot{Height: h, BlockTime: bt, Signed: map[string]bool{}}

	var vals struct {
		Result struct {
			Validators []struct {
				Address     string `json:"address"`
				VotingPower string `json:"voting_power"`
			} `json:"validators"`
		} `json:"result"`
	}
	if err := s.getJSON(ctx, s.RPC, "/validators?per_page=100", &vals); err != nil {
		return nil, fmt.Errorf("validators: %w", err)
	}
	for _, v := range vals.Result.Validators {
		p, _ := strconv.ParseInt(v.VotingPower, 10, 64)
		snap.Validators = append(snap.Validators, Validator{Address: strings.ToUpper(v.Address), Power: p})
	}

	var commit struct {
		Result struct {
			SignedHeader struct {
				Commit struct {
					Signatures []struct {
						BlockIDFlag      int    `json:"block_id_flag"`
						ValidatorAddress string `json:"validator_address"`
					} `json:"signatures"`
				} `json:"commit"`
			} `json:"signed_header"`
		} `json:"result"`
	}
	if err := s.getJSON(ctx, s.RPC, "/commit?height="+strconv.FormatInt(h, 10), &commit); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	for _, sig := range commit.Result.SignedHeader.Commit.Signatures {
		if sig.BlockIDFlag == 2 && sig.ValidatorAddress != "" { // 2 = BlockIDFlagCommit
			snap.Signed[strings.ToUpper(sig.ValidatorAddress)] = true
		}
	}

	// Names are best effort: the registry's block list maps proposer
	// addresses to monikers. Missing names never stop the watchdog.
	if names, err := s.validatorNames(ctx); err == nil {
		for i := range snap.Validators {
			snap.Validators[i].Name = names[snap.Validators[i].Address]
		}
	}
	snap.Nodes, snap.NodesErr = s.nodes(ctx)
	return snap, nil
}

func (s *Sources) validatorNames(ctx context.Context) (map[string]string, error) {
	var blocks struct {
		Blocks []struct {
			Header struct {
				ProposerID      string `json:"proposer_id"`
				ProposerAddress string `json:"proposer_address"`
			} `json:"header"`
		} `json:"blocks"`
	}
	if err := s.getJSON(ctx, s.Registry, "/api/explorer/blocks", &blocks); err != nil {
		return nil, err
	}
	names := map[string]string{}
	for _, b := range blocks.Blocks {
		addr := strings.ToUpper(strings.TrimPrefix(strings.ToLower(b.Header.ProposerAddress), "0x"))
		if addr != "" && b.Header.ProposerID != "" {
			names[addr] = b.Header.ProposerID
		}
	}
	return names, nil
}

func (s *Sources) nodes(ctx context.Context) ([]Node, error) {
	var out struct {
		Peers []struct {
			Name            string `json:"name"`
			Cloud           string `json:"cloud"`
			IP              string `json:"household_ip"`
			ValidHeartbeats int64  `json:"valid_heartbeats"`
			RegisteredAt    int64  `json:"registered_at"`
			LastSeen        int64  `json:"last_seen"`
		} `json:"peers"`
	}
	if err := s.getJSON(ctx, s.Registry, "/api/nodes", &out); err != nil {
		return nil, err
	}
	nodes := make([]Node, 0, len(out.Peers))
	for _, p := range out.Peers {
		nodes = append(nodes, Node{
			Name: p.Name, Cloud: p.Cloud, IP: p.IP, ValidHeartbeats: p.ValidHeartbeats,
			RegisteredAt: time.UnixMilli(p.RegisteredAt), LastSeen: time.UnixMilli(p.LastSeen),
		})
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
	return nodes, nil
}

// NodeStatus is the part of GET /api/nodes/ID/status the bot shows.
type NodeStatus struct {
	NodeID          string  `json:"node_id"`
	Role            string  `json:"role"`
	ProofStatus     string  `json:"proof_status"`
	ValidHeartbeats int64   `json:"valid_heartbeats"`
	VerifiedDays    float64 `json:"verified_days"`
	Health          bool    `json:"health"`
	Synced          bool    `json:"synced"`
	LastHeartbeatAt string  `json:"last_heartbeat_at"`
	RewardStatus    string  `json:"reward_status"`
}

// NodeStatus looks up one node in the registry.
func (s *Sources) NodeStatus(ctx context.Context, id string) (*NodeStatus, error) {
	var out struct {
		Node NodeStatus `json:"node"`
	}
	if err := s.getJSON(ctx, s.Registry, "/api/nodes/"+url.PathEscape(id)+"/status", &out); err != nil {
		return nil, err
	}
	return &out.Node, nil
}

// Balance reads an address's SYN balance from the chain.
func (s *Sources) Balance(ctx context.Context, addr string) (uint64, error) {
	var out struct {
		Balance uint64 `json:"balance"`
	}
	if err := s.getJSON(ctx, s.ChainAPI, "/balance?address="+url.QueryEscape(addr), &out); err != nil {
		return 0, err
	}
	return out.Balance, nil
}
