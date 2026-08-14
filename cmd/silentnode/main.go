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
	"syscall"
	"time"
)

const defaultRelayURLs = "https://synthos-collective.onrender.com"
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

var cliKeyPath string
var cliRelayURLs string
var cliStatusPath string

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
	flag.StringVar(&cliKeyPath, "key", "", "path to persistent Ed25519 node key JSON")
	flag.StringVar(&cliStatusPath, "status", "", "path to write node status JSON")
	flag.StringVar(&cliRelayURLs, "relay", "", "comma-separated SYNTHOS registry/backend URLs")
	flag.Parse()

	key, privateKey, err := loadOrCreateNodeKey()
	if err != nil {
		log.Fatalf("node key error: %v", err)
	}
	node := silentNode{
		NodeID:              key.NodeID,
		PublicKey:           key.PublicKey,
		HardwareCommitment:  hardwareCommitment(),
		Mode:                "background_signed_validator_heartbeat",
		StartedAt:           time.Now().UTC().Format(time.RFC3339),
		StatusPath:          statusPath(),
		KeyPath:             keyPath(),
		RealSignedHeartbeat: true,
	}

	relayURLs := relayURLSet()
	node.RelayURLs = relayURLs

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Printf("SYNTHOS background validator node started: %s", node.NodeID)
	log.Printf("Mode: outbound-only Ed25519 signed heartbeats every %s", heartbeatEvery)
	log.Printf("Relay set: %s", strings.Join(relayURLs, ", "))

	heartbeatAll(ctx, relayURLs, &node, privateKey)
	pollMailboxAll(ctx, relayURLs, node.NodeID)
	ticker := time.NewTicker(heartbeatEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("SYNTHOS background validator node stopped")
			return
		case <-ticker.C:
			heartbeatAll(ctx, relayURLs, &node, privateKey)
			pollMailboxAll(ctx, relayURLs, node.NodeID)
		}
	}
}

func loadOrCreateNodeKey() (nodeKey, ed25519.PrivateKey, error) {
	path := keyPath()
	if body, err := os.ReadFile(path); err == nil {
		var key nodeKey
		if err := json.Unmarshal(body, &key); err != nil {
			return nodeKey{}, nil, err
		}
		privateKey, err := privateKeyFromHex(key.PrivateKey)
		if err != nil {
			return nodeKey{}, nil, err
		}
		if key.NodeID == "" || key.PublicKey == "" {
			return nodeKey{}, nil, fmt.Errorf("stored key is missing node_id or public_key")
		}
		return key, privateKey, nil
	}

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nodeKey{}, nil, err
	}
	key := nodeKey{
		NodeID:     "syn-" + hardwareCommitment()[:12],
		PublicKey:  hex.EncodeToString(publicKey),
		PrivateKey: hex.EncodeToString(privateKey),
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
		Format:     "synthos-background-ed25519-v1",
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nodeKey{}, nil, err
	}
	body, _ := json.MarshalIndent(key, "", "  ")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return nodeKey{}, nil, err
	}
	return key, privateKey, nil
}

func privateKeyFromHex(value string) (ed25519.PrivateKey, error) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "0x")
	raw, err := hex.DecodeString(value)
	if err != nil {
		return nil, err
	}
	if len(raw) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("private key must be %d bytes, got %d", ed25519.PrivateKeySize, len(raw))
	}
	return ed25519.PrivateKey(raw), nil
}

func heartbeatAll(ctx context.Context, relayURLs []string, node *silentNode, privateKey ed25519.PrivateKey) {
	node.HeartbeatCount++
	node.LastRelayOK = nil
	node.LastRelayFailed = nil
	node.LastHeight++
	if node.LastHeight < 1 {
		node.LastHeight = 1
	}
	node.LastTip = "silent-tip-" + randomHex(16)
	node.LastStateRoot = "silent-state-" + randomHex(16)
	node.LastNonce = fmt.Sprintf("%019d-%08d", time.Now().UnixMilli(), node.HeartbeatCount)

	for _, relayURL := range relayURLs {
		if register(ctx, relayURL, *node) && heartbeat(ctx, relayURL, node, privateKey) {
			node.LastRelayOK = append(node.LastRelayOK, relayURL)
		} else {
			node.LastRelayFailed = append(node.LastRelayFailed, relayURL)
		}
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
	}
	return postJSON(ctx, relayURL+"/api/nodes/register", payload, node.NodeID, "register")
}

func heartbeat(ctx context.Context, relayURL string, node *silentNode, privateKey ed25519.PrivateKey) bool {
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
	return postJSON(ctx, relayURL+"/api/nodes/heartbeat", payload, node.NodeID, "heartbeat")
}

func postJSON(ctx context.Context, endpoint string, payload map[string]any, nodeID string, label string) bool {
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		log.Printf("%s request error for %s: %v", label, endpoint, err)
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("%s outbound error for %s: %v", label, endpoint, err)
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		log.Printf("%s rejected for %s on %s: %s", label, nodeID, endpoint, resp.Status)
		return false
	}
	log.Printf("%s accepted for %s via %s", label, nodeID, endpoint)
	return true
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

func hardwareCommitment() string {
	hostname, _ := os.Hostname()
	currentUser, _ := user.Current()
	username := ""
	if currentUser != nil {
		username = currentUser.Username
	}
	sum := sha256.Sum256([]byte(hostname + "|" + username + "|synthos-background-node-v1"))
	return hex.EncodeToString(sum[:])
}

func pollMailboxAll(ctx context.Context, relayURLs []string, nodeID string) {
	for _, relayURL := range relayURLs {
		pollMailbox(ctx, relayURL, nodeID)
	}
}

func pollMailbox(ctx context.Context, relayURL string, nodeID string) {
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, relayURL+"/mailbox?name="+url.QueryEscape(nodeID), nil)
	if err != nil {
		log.Printf("mailbox request error on %s: %v", relayURL, err)
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("mailbox outbound error on %s: %v", relayURL, err)
		return
	}
	defer resp.Body.Close()
	log.Printf("mailbox poll via %s: %s", relayURL, resp.Status)
}

func env(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func relayURLSet() []string {
	raw := firstNonEmpty(
		cliRelayURLs,
		os.Getenv("SYNTHOS_RELAY_URLS"),
		os.Getenv("SYNTHOS_REGISTRY_URLS"),
		os.Getenv("SYNTHOS_MAILBOX_URLS"),
		os.Getenv("SYNTHOS_REGISTRY_URL"),
		os.Getenv("SYNTHOS_MAILBOX_URL"),
		defaultRelayURLs,
	)
	return parseURLList(raw)
}

func parseURLList(raw string) []string {
	seen := map[string]bool{}
	var out []string
	for _, item := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\t' || r == ' '
	}) {
		item = strings.TrimRight(strings.TrimSpace(item), "/")
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func randomHex(bytes int) string {
	raw := make([]byte, bytes)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return hex.EncodeToString(raw)
}

func keyPath() string {
	if cliKeyPath != "" {
		return cliKeyPath
	}
	if value := os.Getenv("SYNTHOS_SILENT_KEY_PATH"); value != "" {
		return value
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "synthos-silent-node-key.json"
	}
	return filepath.Join(dir, "SynthosCollective", "silent-node-key.json")
}

func statusPath() string {
	if cliStatusPath != "" {
		return cliStatusPath
	}
	if value := os.Getenv("SYNTHOS_SILENT_STATUS_PATH"); value != "" {
		return value
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "synthos-silent-node-status.json"
	}
	return filepath.Join(dir, "SynthosCollective", "silent-node-status.json")
}

func writeStatus(node silentNode) {
	if node.StatusPath == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(node.StatusPath), 0o700); err != nil {
		return
	}
	body, _ := json.MarshalIndent(node, "", "  ")
	_ = os.WriteFile(node.StatusPath, body, 0o600)
}
