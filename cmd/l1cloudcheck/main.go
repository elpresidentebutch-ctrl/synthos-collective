package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type nodeRegistry struct {
	ChainID string      `json:"chain_id"`
	Nodes   []nodeEntry `json:"nodes"`
}

type nodeEntry struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Role string `json:"role"`
}

type statusResponse struct {
	ChainID      string `json:"chain_id"`
	Height       uint64 `json:"height"`
	Tip          string `json:"tip"`
	StateRoot    string `json:"state_root"`
	MempoolSize  int    `json:"mempool_size"`
	Validator    string `json:"validator"`
	NextProposer string `json:"next_proposer"`
	IsMyTurn     bool   `json:"is_my_turn"`
}

type healthResponse struct {
	OK      bool   `json:"ok"`
	Service string `json:"service"`
	Worker  bool   `json:"worker"`
}

type heartbeatResponse struct {
	TotalChecks        int    `json:"total_checks"`
	LastCheck          string `json:"last_check"`
	BlocksAutoProposed int    `json:"blocks_auto_proposed"`
	Validator          string `json:"validator"`
	IsMyTurn           bool   `json:"is_my_turn"`
}

type peersResponse struct {
	Self            string   `json:"self"`
	Peers           []string `json:"peers"`
	TotalValidators int      `json:"total_validators"`
	ValidatorOrder  []string `json:"validator_order"`
	CurrentProposer string   `json:"current_proposer"`
}

type blocksResponse struct {
	Blocks []any `json:"blocks"`
	Count  int   `json:"count"`
}

type nodeProof struct {
	Name             string             `json:"name"`
	URL              string             `json:"url"`
	Role             string             `json:"role"`
	Reachable        bool               `json:"reachable"`
	Converged        bool               `json:"converged"`
	HeartbeatFresh   bool               `json:"heartbeat_fresh"`
	HeartbeatAgeMs   int64              `json:"heartbeat_age_ms,omitempty"`
	Error            string             `json:"error,omitempty"`
	Health           *healthResponse    `json:"health,omitempty"`
	Status           *statusResponse    `json:"status,omitempty"`
	Heartbeat        *heartbeatResponse `json:"heartbeat,omitempty"`
	Peers            *peersResponse     `json:"peers,omitempty"`
	RecentBlockCount int                `json:"recent_block_count"`
	LatencyMillis    int64              `json:"latency_ms"`
}

type summary struct {
	OK                  bool        `json:"ok"`
	ConfigChainID       string      `json:"config_chain_id"`
	Reachable           int         `json:"reachable"`
	Total               int         `json:"total"`
	HighestHeight       uint64      `json:"highest_height"`
	ConvergedHeight     bool        `json:"converged_height"`
	ConvergedTip        bool        `json:"converged_tip"`
	ConvergedStateRoot  bool        `json:"converged_state_root"`
	MajorityRequirement int         `json:"majority_requirement"`
	MajorityReachable   bool        `json:"majority_reachable"`
	FreshHeartbeats     int         `json:"fresh_heartbeats"`
	ElapsedMillis       int64       `json:"elapsed_ms"`
	NodeProofs          []nodeProof `json:"node_proofs"`
}

func main() {
	started := time.Now()
	configPath := flag.String("config", "config/nodes.json", "path to Cloudflare node registry JSON")
	timeout := flag.Duration("timeout", 5*time.Second, "per-request timeout")
	heartbeatMaxAge := flag.Duration("heartbeat-max-age", time.Hour, "maximum accepted heartbeat age")
	fromHeight := flag.Int("blocks-from", 0, "height to use when sampling /blocks")
	flag.Parse()

	out, err := run(*configPath, *timeout, *heartbeatMaxAge, *fromHeight, started)
	if err != nil {
		_ = writeJSON(os.Stdout, map[string]any{
			"ok":         false,
			"error":      err.Error(),
			"elapsed_ms": time.Since(started).Milliseconds(),
		})
		os.Exit(1)
	}
	_ = writeJSON(os.Stdout, out)
	if !out.OK {
		os.Exit(1)
	}
}

func run(configPath string, timeout time.Duration, heartbeatMaxAge time.Duration, fromHeight int, started time.Time) (summary, error) {
	registry, err := loadRegistry(configPath)
	if err != nil {
		return summary{}, err
	}
	client := http.Client{Timeout: timeout}
	proofs := make([]nodeProof, 0, len(registry.Nodes))
	var canonicalHeight uint64
	var canonicalTip string
	var canonicalRoot string
	reachable := 0
	freshHeartbeats := 0
	highest := uint64(0)

	for _, node := range registry.Nodes {
		proof := checkNode(client, node, heartbeatMaxAge, fromHeight)
		if proof.Reachable && proof.Status != nil {
			reachable++
			if proof.HeartbeatFresh {
				freshHeartbeats++
			}
			if proof.Status.Height > highest {
				highest = proof.Status.Height
			}
			if canonicalTip == "" {
				canonicalHeight = proof.Status.Height
				canonicalTip = proof.Status.Tip
				canonicalRoot = proof.Status.StateRoot
			}
			proof.Converged = proof.Status.Height == canonicalHeight &&
				proof.Status.Tip == canonicalTip &&
				proof.Status.StateRoot == canonicalRoot
		}
		proofs = append(proofs, proof)
	}

	convergedHeight := true
	convergedTip := true
	convergedRoot := true
	for _, proof := range proofs {
		if !proof.Reachable || proof.Status == nil {
			continue
		}
		if proof.Status.Height != canonicalHeight {
			convergedHeight = false
		}
		if proof.Status.Tip != canonicalTip {
			convergedTip = false
		}
		if proof.Status.StateRoot != canonicalRoot {
			convergedRoot = false
		}
	}

	required := (2*len(registry.Nodes) + 2) / 3
	majorityReachable := reachable >= required
	ok := reachable == len(registry.Nodes) && freshHeartbeats == len(registry.Nodes) && convergedHeight && convergedTip && convergedRoot
	return summary{
		OK:                  ok,
		ConfigChainID:       registry.ChainID,
		Reachable:           reachable,
		Total:               len(registry.Nodes),
		HighestHeight:       highest,
		ConvergedHeight:     convergedHeight,
		ConvergedTip:        convergedTip,
		ConvergedStateRoot:  convergedRoot,
		MajorityRequirement: required,
		MajorityReachable:   majorityReachable,
		FreshHeartbeats:     freshHeartbeats,
		ElapsedMillis:       time.Since(started).Milliseconds(),
		NodeProofs:          proofs,
	}, nil
}

func checkNode(client http.Client, node nodeEntry, heartbeatMaxAge time.Duration, fromHeight int) nodeProof {
	started := time.Now()
	baseURL := strings.TrimRight(node.URL, "/")
	proof := nodeProof{Name: node.Name, URL: baseURL, Role: node.Role}

	var health healthResponse
	if err := getJSON(client, baseURL+"/health", &health); err != nil {
		proof.Error = err.Error()
		proof.LatencyMillis = time.Since(started).Milliseconds()
		return proof
	}
	proof.Health = &health

	var status statusResponse
	if err := getJSON(client, baseURL+"/status", &status); err != nil {
		proof.Error = err.Error()
		proof.LatencyMillis = time.Since(started).Milliseconds()
		return proof
	}
	proof.Status = &status
	proof.Reachable = true

	var heartbeat heartbeatResponse
	if err := getJSON(client, baseURL+"/heartbeat", &heartbeat); err == nil {
		proof.Heartbeat = &heartbeat
		if heartbeat.LastCheck != "" {
			if lastCheck, err := time.Parse(time.RFC3339Nano, heartbeat.LastCheck); err == nil {
				age := time.Since(lastCheck)
				proof.HeartbeatAgeMs = age.Milliseconds()
				proof.HeartbeatFresh = age <= heartbeatMaxAge
			}
		}
	}

	var peers peersResponse
	if err := getJSON(client, baseURL+"/peers", &peers); err == nil {
		proof.Peers = &peers
	}

	var blocks blocksResponse
	if err := getJSON(client, fmt.Sprintf("%s/blocks?from=%d", baseURL, fromHeight), &blocks); err == nil {
		proof.RecentBlockCount = blocks.Count
	}

	proof.LatencyMillis = time.Since(started).Milliseconds()
	return proof
}

func loadRegistry(path string) (nodeRegistry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nodeRegistry{}, err
	}
	defer f.Close()
	var registry nodeRegistry
	if err := json.NewDecoder(f).Decode(&registry); err != nil {
		return nodeRegistry{}, err
	}
	if len(registry.Nodes) == 0 {
		return nodeRegistry{}, fmt.Errorf("no nodes configured in %s", path)
	}
	return registry, nil
}

func getJSON(client http.Client, url string, out any) error {
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("GET %s status=%d body=%s", url, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("GET %s decode: %w", url, err)
	}
	return nil
}

func writeJSON(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}
