package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const defaultRelayURL = "https://synthos-www.onrender.com"
const heartbeatEvery = 15 * time.Second

var coreCapabilities = []string{
	"ed25519",
	"canonical_serialization",
	"validator_registry",
	"proposal_rotation",
	"quorum",
	"replay_protection",
	"persistent_storage",
}

// allTargetNodes includes every operator, candidate, cloudless, and legacy node
// except Render validators 11, 14, and 15 (which the user specifically requested to decommission).
var allTargetNodes = []string{
	"syn-fleet-1",
	"syn-fleet-2",
	"syn-fleet-3",
	"syn-fleet-4",
	"syn-fleet-5",
	"syn-cc59c6b08899",
	"desktop-17d2b0d4d401",
	"synthos-home-1",
	"syn-02dbfc711cbc",
	"syn-095fc90f253d",
	"syn-5efbde2548fa",
	"syn-88a75fed0144",
	"syn-a73e07e40955",
	"syn-b6e883d073ab",
	"syn-cf22ae078fc9",
	"syn-fc33cc2328fa",
	"syn-d0e99dad9be7",
	"syn-gitlab-cloud-1",
	"syn-gitlab-cloud-2",
	"synthos-render-validator-1-test",
	"syn-6821379290e2",
}

type chainSync struct {
	mu        sync.RWMutex
	height    int64
	tip       string
	stateRoot string
}

var globalChainSync chainSync

func updateChainSync(relayURL string) {
	client := &http.Client{Timeout: 5 * time.Second}

	// 1. Try registry /api/network/status
	if resp, err := client.Get(relayURL + "/api/network/status"); err == nil && resp.StatusCode == 200 {
		var data struct {
			HighestHeight int64  `json:"highest_height"`
			Tip           string `json:"tip"`
			StateRoot     string `json:"state_root"`
		}
		if json.NewDecoder(resp.Body).Decode(&data) == nil && data.HighestHeight > 0 {
			_ = resp.Body.Close()
			globalChainSync.mu.Lock()
			globalChainSync.height = data.HighestHeight
			if data.Tip != "" {
				globalChainSync.tip = data.Tip
			}
			if data.StateRoot != "" {
				globalChainSync.stateRoot = data.StateRoot
			}
			globalChainSync.mu.Unlock()
			return
		}
		_ = resp.Body.Close()
	}

	// 2. Try RPC status
	if resp, err := client.Get("https://rpc.ishamwilliamsblockchains.com/status"); err == nil && resp.StatusCode == 200 {
		var data struct {
			Height    int64  `json:"height"`
			Tip       string `json:"tip"`
			StateRoot string `json:"state_root"`
		}
		if json.NewDecoder(resp.Body).Decode(&data) == nil && data.Height > 0 {
			_ = resp.Body.Close()
			globalChainSync.mu.Lock()
			globalChainSync.height = data.Height
			if data.Tip != "" {
				globalChainSync.tip = data.Tip
			}
			if data.StateRoot != "" {
				globalChainSync.stateRoot = data.StateRoot
			}
			globalChainSync.mu.Unlock()
			return
		}
		_ = resp.Body.Close()
	}
}

// getChainSync returns the chain position the network last reported. Until
// the first report arrives it returns zeros: a heartbeat must never claim a
// height, tip or state root it didn't actually observe.
func getChainSync() (int64, string, string) {
	globalChainSync.mu.RLock()
	defer globalChainSync.mu.RUnlock()
	return globalChainSync.height, globalChainSync.tip, globalChainSync.stateRoot
}

type nodeKey struct {
	NodeID     string `json:"node_id"`
	PublicKey  string `json:"public_key"`
	PrivateKey string `json:"private_key"`
	CreatedAt  string `json:"created_at"`
	Format     string `json:"format"`
}

type silentNode struct {
	NodeID              string   `json:"node_id"`
	PublicKey           string   `json:"public_key"`
	HardwareCommitment  string   `json:"hardware_commitment"`
	Mode                string   `json:"mode"`
	StartedAt           string   `json:"started_at"`
	HeartbeatCount      uint64   `json:"heartbeat_count"`
	LastNonce           string   `json:"last_nonce"`
	LastTip             string   `json:"last_tip"`
	LastStateRoot       string   `json:"last_state_root"`
	LastHeight          int64    `json:"last_height"`
	RelayURLs           []string `json:"relay_urls"`
	LastRelayOK         []string `json:"last_relay_ok"`
	LastRelayFailed     []string `json:"last_relay_failed"`
	StatusPath          string   `json:"status_path"`
	KeyPath             string   `json:"key_path"`
	RealSignedHeartbeat bool     `json:"real_signed_heartbeat"`
}

func main() {
	var relayURL string
	var fleetDir string
	flag.StringVar(&relayURL, "relay", defaultRelayURL, "SYNTHOS registry/backend URL")
	flag.StringVar(&fleetDir, "dir", "", "fleet base storage directory")
	flag.Parse()

	if fleetDir == "" {
		appData := os.Getenv("LOCALAPPDATA")
		if appData == "" {
			appData = os.TempDir()
		}
		fleetDir = filepath.Join(appData, "SynthosCollective", "Fleet")
	}
	_ = os.MkdirAll(fleetDir, 0o755)

	log.Printf("=== SYNTHOS Node Fleet Manager Starting ===")
	log.Printf("Relay: %s", relayURL)
	log.Printf("Running %d node identities on this machine (they count as one machine: one validator, one reward)", len(allTargetNodes))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initial chain sync & background sync poller
	updateChainSync(relayURL)
	go func() {
		ticker := time.NewTicker(6 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				updateChainSync(relayURL)
			}
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	var wg sync.WaitGroup

	for _, nodeID := range allTargetNodes {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			runNodeWorker(ctx, id, relayURL, fleetDir)
		}(nodeID)
		time.Sleep(150 * time.Millisecond) // staggered startup
	}

	<-sigChan
	log.Printf("Stopping SYNTHOS fleet...")
	cancel()
	wg.Wait()
	log.Printf("All fleet nodes stopped.")
}

func runNodeWorker(ctx context.Context, nodeID string, relayURL string, fleetDir string) {
	nodeDir := filepath.Join(fleetDir, nodeID)
	_ = os.MkdirAll(nodeDir, 0o755)
	keyPath := filepath.Join(nodeDir, "silent-node-key.json")
	statusPath := filepath.Join(nodeDir, "silent-node-status.json")

	// Check if this node has a legacy roaming key (e.g. syn-cc59c6b08899)
	roamingKey := filepath.Join(os.Getenv("APPDATA"), "SynthosCollective", "silent-node-key.json")
	if nodeID == "syn-cc59c6b08899" && !fileExists(keyPath) && fileExists(roamingKey) {
		if data, err := os.ReadFile(roamingKey); err == nil {
			_ = os.WriteFile(keyPath, data, 0o600)
		}
	}

	key, privateKey, err := loadOrGenerateKey(nodeID, keyPath)
	if err != nil {
		log.Printf("[%s] key error: %v", nodeID, err)
		return
	}

	node := silentNode{
		NodeID:              key.NodeID,
		PublicKey:           key.PublicKey,
		HardwareCommitment:  hardwareCommitment(key.NodeID),
		Mode:                "background_signed_validator_heartbeat",
		StartedAt:           time.Now().UTC().Format(time.RFC3339),
		StatusPath:          statusPath,
		KeyPath:             keyPath,
		RelayURLs:           []string{relayURL},
		RealSignedHeartbeat: true,
	}

	// Clear any conflicted / stale registration
	clearStalePeer(ctx, relayURL, nodeID)

	// First registration & heartbeat
	register(ctx, relayURL, node)
	doHeartbeat(ctx, relayURL, &node, privateKey)
	pollMailbox(ctx, relayURL, nodeID)

	ticker := time.NewTicker(heartbeatEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			register(ctx, relayURL, node)
			doHeartbeat(ctx, relayURL, &node, privateKey)
			pollMailbox(ctx, relayURL, nodeID)
		}
	}
}

func clearStalePeer(ctx context.Context, relayURL string, nodeID string) {
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodDelete, relayURL+"/peers/"+url.PathEscape(nodeID), nil)
	if err == nil {
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
		}
	}
}

func doHeartbeat(ctx context.Context, relayURL string, node *silentNode, privateKey ed25519.PrivateKey) {
	node.HeartbeatCount++

	// Fetch current chain height, tip, and state root so the node is 100% in sync
	chainH, chainTip, chainStateRoot := getChainSync()
	node.LastHeight = chainH
	node.LastTip = chainTip
	node.LastStateRoot = chainStateRoot
	node.LastNonce = fmt.Sprintf("%019d-%08d", time.Now().UnixMilli(), node.HeartbeatCount)

	timestamp := time.Now().UTC().Format(time.RFC3339)
	message := canonicalHeartbeatMessage(node.NodeID, node.LastHeight, node.LastTip, node.LastStateRoot, timestamp, node.LastNonce)
	signature := ed25519.Sign(privateKey, []byte(message))

	payload := map[string]any{
		"node_id":      node.NodeID,
		"height":       node.LastHeight,
		"tip":          node.LastTip,
		"state_root":   node.LastStateRoot,
		"timestamp":    timestamp,
		"nonce":        node.LastNonce,
		"signature":    hex.EncodeToString(signature),
		"capabilities": coreCapabilities,
	}

	if postJSON(ctx, relayURL+"/api/nodes/heartbeat", payload, node.NodeID, "heartbeat") {
		node.LastRelayOK = []string{relayURL}
		node.LastRelayFailed = nil
	} else {
		node.LastRelayOK = nil
		node.LastRelayFailed = []string{relayURL}
	}
	writeStatus(*node)
}

func register(ctx context.Context, relayURL string, node silentNode) bool {
	payload := map[string]any{
		"publicId":            node.NodeID,
		"public_key":          node.PublicKey,
		"mode":                "public-validator",
		"role":                "validator_candidate",
		"network":             "mainnet",
		"endpoint":            "",
		"capabilities":        coreCapabilities,
		"background":          true,
		"hardware_commitment": node.HardwareCommitment,
		"machine_commitment":  node.HardwareCommitment,
	}
	if !postJSON(ctx, relayURL+"/api/nodes/register", payload, node.NodeID, "register") {
		clearStalePeer(ctx, relayURL, node.NodeID)
		return postJSON(ctx, relayURL+"/api/nodes/register", payload, node.NodeID, "register-retry")
	}
	return true
}

func pollMailbox(ctx context.Context, relayURL string, nodeID string) {
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, relayURL+"/mailbox?name="+url.QueryEscape(nodeID), nil)
	if err == nil {
		if resp, err := http.DefaultClient.Do(req); err == nil {
			_ = resp.Body.Close()
		}
	}
}

func postJSON(ctx context.Context, endpoint string, payload map[string]any, nodeID string, label string) bool {
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

func canonicalHeartbeatMessage(nodeID string, height int64, tip string, stateRoot string, timestamp string, nonce string) string {
	return fmt.Sprintf(
		"SYNTHOS_HEARTBEAT_V1\nnode_id=%s\nheight=%d\ntip=%s\nstate_root=%s\ntimestamp=%s\nnonce=%s",
		nodeID,
		height,
		tip,
		stateRoot,
		strings.TrimSpace(timestamp),
		strings.TrimSpace(nonce),
	)
}

// hardwareCommitment identifies the machine, not the node: every node
// identity running on one computer reports the same value, so the network
// can tell they are one machine (one reward, one counted validator). It
// matches the desktop agent's commitment for the same computer. The nodeID
// parameter is unused and kept only so callers read naturally.
func hardwareCommitment(nodeID string) string {
	_ = nodeID
	hostname, _ := os.Hostname()
	currentUser, _ := user.Current()
	username := ""
	if currentUser != nil {
		username = currentUser.Username
	}
	sum := sha256.Sum256([]byte(hostname + "|" + username + "|synthos-desktop-agent-v1"))
	return hex.EncodeToString(sum[:])
}

func loadOrGenerateKey(nodeID string, path string) (nodeKey, ed25519.PrivateKey, error) {
	if body, err := os.ReadFile(path); err == nil {
		var key nodeKey
		if err := json.Unmarshal(body, &key); err == nil && key.PublicKey != "" && key.PrivateKey != "" {
			priv, err := privateKeyFromHex(key.PrivateKey)
			if err == nil {
				return key, priv, nil
			}
		}
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nodeKey{}, nil, err
	}
	key := nodeKey{
		NodeID:     nodeID,
		PublicKey:  hex.EncodeToString(pub),
		PrivateKey: hex.EncodeToString(priv),
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
		Format:     "synthos-background-ed25519-v1",
	}
	body, _ := json.MarshalIndent(key, "", "  ")
	_ = os.WriteFile(path, body, 0o600)
	return key, priv, nil
}

func privateKeyFromHex(value string) (ed25519.PrivateKey, error) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "0x")
	raw, err := hex.DecodeString(value)
	if err != nil {
		return nil, err
	}
	if len(raw) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid ed25519 key size %d", len(raw))
	}
	return ed25519.PrivateKey(raw), nil
}

func writeStatus(node silentNode) {
	if node.StatusPath == "" {
		return
	}
	body, err := json.MarshalIndent(node, "", "  ")
	if err == nil {
		_ = os.WriteFile(node.StatusPath, body, 0o600)
	}
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}
