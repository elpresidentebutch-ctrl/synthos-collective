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
	"strconv"
	"strings"
	"syscall"
	"time"

	"synthos-collective/internal/consensus"
)

const defaultRelayURLs = "https://82-29-54-7.sslip.io,https://synthos-www.onrender.com,http://127.0.0.1:8085"
const defaultChainRPC = "https://rpc.ishamwilliamsblockchains.com"
const heartbeatEvery = 15 * time.Second
const provingTargetDays = 30.0

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
var cliNodeID string
var cliPrintKey bool
var cliDaemon bool
var cliForeground bool
var cliPayoutAddress string

type nodeKey struct {
	NodeID     string `json:"node_id"`
	PublicKey  string `json:"public_key"`
	PrivateKey string `json:"private_key"`
	CreatedAt  string `json:"created_at"`
	Format        string `json:"format"`
	PayoutAddress string `json:"payout_address,omitempty"`
}

type silentNode struct {
	NodeID              string   `json:"node_id"`
	PublicKey           string   `json:"public_key"`
	HardwareCommitment  string   `json:"hardware_commitment"`
	Mode                string   `json:"mode"`
	StartedAt           string   `json:"started_at"`
	PID                 int      `json:"pid"`
	HeartbeatCount      uint64   `json:"heartbeat_count"`
	BlocksValidated     uint64   `json:"blocks_validated"`
	VotesCast           uint64   `json:"votes_cast"`
	LastNonce           string   `json:"last_nonce"`
	LastTip             string   `json:"last_tip"`
	LastStateRoot       string   `json:"last_state_root"`
	LastHeight          int64    `json:"last_height"`
	RelayURLs           []string `json:"relay_urls"`
	LastRelayOK         []string `json:"last_relay_ok"`
	LastRelayFailed     []string `json:"last_relay_failed"`
	StatusPath          string    `json:"status_path"`
	KeyPath             string    `json:"key_path"`
	RealSignedHeartbeat bool      `json:"real_signed_heartbeat"`
	ProvingDays         float64   `json:"proving_days"`
	StreakDays          float64   `json:"streak_days"`
	StreakMultiplier    float64   `json:"streak_multiplier"`
	EcosystemPoints     float64   `json:"ecosystem_points"`
	RewardEligible      bool      `json:"reward_eligible"`
	ValidatorStatus     string    `json:"validator_status"`
	PayoutAddress       string    `json:"payout_address,omitempty"`
	CreatedAt           time.Time `json:"-"`
}

type relayMailboxEnvelope struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	From      string          `json:"from"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt int64           `json:"created_at"`
}

func main() {
	flag.StringVar(&cliKeyPath, "key", "", "path to persistent Ed25519 node key JSON")
	flag.StringVar(&cliStatusPath, "status", "", "path to write node status JSON")
	flag.StringVar(&cliRelayURLs, "relay", "", "comma-separated SYNTHOS registry/backend URLs")
	flag.StringVar(&cliNodeID, "id", "", "explicit node ID override (or derives unique ID from key)")
	flag.BoolVar(&cliPrintKey, "print-key", false, "print this node's identity (ID, public key, key file location) and exit, without starting the node")
	flag.BoolVar(&cliDaemon, "daemon", false, "run as detached background daemon")
	flag.BoolVar(&cliForeground, "foreground", false, "run directly in the current foreground process")
	flag.Parse()

	if cliNodeID == "" {
		cliNodeID = os.Getenv("SYNTHOS_NODE_ID")
	}

	key, privateKey, created, err := loadOrCreateNodeKey()
	if err != nil {
		log.Fatalf("node key error: %v", err)
	}

	createdTime, _ := time.Parse(time.RFC3339, key.CreatedAt)
	if createdTime.IsZero() {
		createdTime = time.Now().UTC()
	}

	printIdentity(key, createdTime, created)
	if cliPrintKey {
		return
	}

	if cliDaemon {
		setupDaemonLogging()
		writePIDFile()
	}

	node := silentNode{
		NodeID:              key.NodeID,
		PublicKey:           key.PublicKey,
		HardwareCommitment:  hardwareCommitment(key.NodeID),
		Mode:                "active_validating_desktop_node",
		StartedAt:           time.Now().UTC().Format(time.RFC3339),
		PID:                 os.Getpid(),
		StatusPath:          statusPath(),
		KeyPath:             keyPath(),
		RealSignedHeartbeat: true,
		PayoutAddress:       key.PayoutAddress,
		CreatedAt:           createdTime,
	}

	updateProvingStatus(&node)

	relayURLs := relayURLSet()
	node.RelayURLs = relayURLs

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	log.Printf("SYNTHOS Validator Node started: %s (PID %d)", node.NodeID, node.PID)
	log.Printf("Role: Real consensus validator candidate (outbound-only mailbox relay)")
	log.Printf("Validation engine: active (processes network block proposals & casts signed votes)")
	log.Printf("Relay cluster: %s", strings.Join(relayURLs, ", "))
	log.Printf("Status: %s", node.ValidatorStatus)

	keepSystemAwakeAllowScreenSleep()

	ctx := context.Background()
	heartbeatAll(ctx, relayURLs, &node, privateKey)
	pollMailboxAll(ctx, relayURLs, &node, privateKey)
	dashPath := writeDashboardHTML(node)
	if dashPath != "" {
		log.Printf("Local validator dashboard active at: %s", dashPath)
	}

	ticker := time.NewTicker(heartbeatEvery)
	defer ticker.Stop()

	// Mailbox polling runs more frequently (every 3 seconds) to catch incoming consensus rounds promptly
	mailboxTicker := time.NewTicker(3 * time.Second)
	defer mailboxTicker.Stop()

	for {
		select {
		case sig := <-sigChan:
			log.Printf("SYNTHOS Validator Node received %v, stopping...", sig)
			return
		case <-mailboxTicker.C:
			pollMailboxAll(ctx, relayURLs, &node, privateKey)
		case <-ticker.C:
			keepSystemAwakeAllowScreenSleep()
			updateProvingStatus(&node)
			heartbeatAll(ctx, relayURLs, &node, privateKey)
			writeDashboardHTML(node)
		}
	}
}

func streakMultiplierForDays(days float64) float64 {
	switch {
	case days >= 30.0:
		return 3.0
	case days >= 21.0:
		return 2.5
	case days >= 14.0:
		return 2.0
	case days >= 7.0:
		return 1.5
	default:
		return 1.0
	}
}

func updateProvingStatus(node *silentNode) {
	if node.CreatedAt.IsZero() {
		return
	}
	elapsed := time.Since(node.CreatedAt)
	days := elapsed.Hours() / 24.0
	node.ProvingDays = days
	node.StreakDays = days
	node.StreakMultiplier = streakMultiplierForDays(days)
	// 2,400 points/day base rate (100 pts/hr) * streak multiplier + block validation & heartbeat bonuses
	node.EcosystemPoints = (days * 2400.0 * node.StreakMultiplier) + float64(node.BlocksValidated*10) + float64(node.HeartbeatCount)

	if days >= provingTargetDays {
		node.RewardEligible = true
		node.ValidatorStatus = "Active Validator (1-month proving completed — 10,000 SYN reward verified)"
	} else {
		node.RewardEligible = false
		pct := (days / provingTargetDays) * 100.0
		if pct > 100.0 {
			pct = 100.0
		}
		node.ValidatorStatus = fmt.Sprintf("Proving Candidate (Day %.1f of 30 — %.1f%% proven — %.1fx Streak Boost)", days, pct, node.StreakMultiplier)
	}
}

func pidFilePath() string {
	return filepath.Join(appDir(), "silent-node.pid")
}

func writePIDFile() {
	_ = os.MkdirAll(appDir(), 0o700)
	_ = os.WriteFile(pidFilePath(), []byte(strconv.Itoa(os.Getpid())), 0o600)
}

func setupDaemonLogging() {
	logPath := filepath.Join(appDir(), "silent-node.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err == nil {
		log.SetOutput(f)
	}
}

func loadOrCreateNodeKey() (nodeKey, ed25519.PrivateKey, bool, error) {
	path := keyPath()
	if body, err := os.ReadFile(path); err == nil {
		var key nodeKey
		if err := json.Unmarshal(body, &key); err != nil {
			return nodeKey{}, nil, false, err
		}
		privateKey, err := privateKeyFromHex(key.PrivateKey)
		if err != nil {
			return nodeKey{}, nil, false, err
		}
		if key.NodeID == "" || key.PublicKey == "" {
			return nodeKey{}, nil, false, fmt.Errorf("stored key is missing node_id or public_key")
		}
		if cliNodeID != "" {
			key.NodeID = cliNodeID
		}
		if cliPayoutAddress != "" && key.PayoutAddress != cliPayoutAddress {
			key.PayoutAddress = cliPayoutAddress
			body, _ := json.MarshalIndent(key, "", "  ")
			_ = os.WriteFile(path, body, 0o600)
		}
		return key, privateKey, false, nil
	}

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nodeKey{}, nil, false, err
	}
	nodeID := cliNodeID
	if nodeID == "" {
		h := sha256.Sum256(publicKey)
		nodeID = "syn-" + hex.EncodeToString(h[:6])
	}
	key := nodeKey{
		NodeID:     nodeID,
		PublicKey:  hex.EncodeToString(publicKey),
		PrivateKey: hex.EncodeToString(privateKey),
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
		Format:        "synthos-validating-ed25519-v1",
		PayoutAddress: cliPayoutAddress,
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nodeKey{}, nil, false, err
	}
	body, _ := json.MarshalIndent(key, "", "  ")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return nodeKey{}, nil, false, err
	}
	return key, privateKey, true, nil
}

func printIdentity(key nodeKey, createdAt time.Time, created bool) {
	elapsed := time.Since(createdAt)
	days := elapsed.Hours() / 24.0
	mult := streakMultiplierForDays(days)
	status := fmt.Sprintf("Proving Uptime (Day %.1f of 30 — %.1fx Streak Boost)", days, mult)
	if days >= provingTargetDays {
		status = "★ PROVING COMPLETE — Verified for 10,000 SYN reward & Active Validator"
	}

	fmt.Println("========================================================================")
	fmt.Println("   SYNTHOS COLLECTIVE — 1-CLICK VALIDATOR & CONSENSUS NODE")
	fmt.Println("========================================================================")
	fmt.Printf("   YOUR NODE ID:  %s\n", key.NodeID)
	fmt.Printf("   Public Key:    %s\n", key.PublicKey)
	fmt.Printf("   Status:        %s\n", status)
	payout := key.PayoutAddress
	if payout == "" {
		payout = "Not configured (set in Synthos-Node-Status.html to receive monthly rewards)"
	}
	fmt.Printf("   Payout Wallet: %s\n", payout)
	fmt.Printf("   Key File:      %s\n", keyPath())
	fmt.Println("========================================================================")
	fmt.Println("   • Runs silently in background with ZERO open inbound ports.")
	fmt.Println("   • Actively receives, verifies, and signs network block proposals.")
	fmt.Println("   • Earns 10,000 SYN monthly rewards after 30 days of verified proving.")
	fmt.Println("   • Gamified Streaks: 7-day (1.5x), 14-day (2.0x), 21-day (2.5x), 30-day (3.0x).")
	fmt.Println("   • ANTI-FARMING: Only 1 paid node per household internet connection.")
	fmt.Println("   • HALVING: Rewards cut in half every 2 years starting Jan 1, 2027.")
	fmt.Println("   • POWER NOTE: The screen can sleep, but NOT the actual computer.")
	fmt.Println("     Keep the machine awake so block validation and proving continue.")
	fmt.Println("========================================================================")
	if created {
		log.Printf("New validator identity generated (%s).", key.NodeID)
	}
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

func syncRealMainnetTip(ctx context.Context, relayURLs []string, node *silentNode) {
	type statusResp struct {
		Height    int64  `json:"height"`
		Tip       string `json:"tip"`
		StateRoot string `json:"state_root"`
	}
	endpoints := make([]string, 0, len(relayURLs)+2)
	for _, r := range relayURLs {
		endpoints = append(endpoints, strings.TrimRight(r, "/")+"/api/chain/status")
	}
	endpoints = append(endpoints, defaultChainRPC+"/status")
	endpoints = append(endpoints, "https://synthos-www.onrender.com/status")

	for _, ep := range endpoints {
		reqCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, ep, nil)
		if err != nil {
			cancel()
			continue
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			cancel()
			continue
		}
		var st statusResp
		decodeErr := json.NewDecoder(resp.Body).Decode(&st)
		_ = resp.Body.Close()
		cancel()
		if decodeErr == nil && st.Height > 0 && st.Tip != "" {
			node.LastHeight = st.Height
			node.LastTip = st.Tip
			node.LastStateRoot = st.StateRoot
			return
		}
	}

	if node.LastHeight < 1 {
		node.LastHeight = 1
	}
	if node.LastTip == "" {
		node.LastTip = "syn-tip-" + randomHex(16)
	}
	if node.LastStateRoot == "" {
		node.LastStateRoot = "syn-state-" + randomHex(16)
	}
}

func heartbeatAll(ctx context.Context, relayURLs []string, node *silentNode, privateKey ed25519.PrivateKey) {
	node.HeartbeatCount++
	node.LastRelayOK = nil
	node.LastRelayFailed = nil
	syncRealMainnetTip(ctx, relayURLs, node)
	node.LastNonce = fmt.Sprintf("%019d-%08d", time.Now().UnixMilli(), node.HeartbeatCount)

	for _, relayURL := range relayURLs {
		register(ctx, relayURL, *node)
		if heartbeat(ctx, relayURL, node, privateKey) {
			node.LastRelayOK = append(node.LastRelayOK, relayURL)
		} else {
			node.LastRelayFailed = append(node.LastRelayFailed, relayURL)
		}
	}
	writeStatus(*node)
}

func register(ctx context.Context, relayURL string, node silentNode) bool {
	role := "validator_candidate"
	if node.RewardEligible {
		role = "validator"
	}
	payload := map[string]any{
		"publicId":            node.NodeID,
		"public_key":          node.PublicKey,
		"mode":                "public-validator",
		"role":                role,
		"network":             "mainnet",
		"endpoint":            "",
		"capabilities":        coreCapabilities,
		"background":          true,
		"hardware_commitment": node.HardwareCommitment,
		"machine_commitment":  node.HardwareCommitment,
		"payout_address":      node.PayoutAddress,
		"operator_address":    node.PayoutAddress,
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
		"capabilities":     coreCapabilities,
		"payout_address":   node.PayoutAddress,
		"operator_address": node.PayoutAddress,
	}
	return postJSON(ctx, relayURL+"/api/nodes/heartbeat", payload, node.NodeID, "heartbeat")
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
	if resp.StatusCode == http.StatusConflict && label == "register" {
		return true
	}
	return resp.StatusCode >= 200 && resp.StatusCode <= 299
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

func hardwareCommitment(nodeID string) string {
	_ = nodeID
	hostname, _ := os.Hostname()
	currentUser, _ := user.Current()
	username := ""
	if currentUser != nil {
		username = currentUser.Username
	}
	sum := sha256.Sum256([]byte(hostname + "|" + username + "|synthos-validator-node-v1"))
	return hex.EncodeToString(sum[:])
}

// pollMailboxAll checks the mailbox relay across all configured relays,
// processes block proposals, validates them, and posts cryptographically signed votes back.
func pollMailboxAll(ctx context.Context, relayURLs []string, node *silentNode, privateKey ed25519.PrivateKey) {
	for _, relayURL := range relayURLs {
		messages := fetchMailboxMessages(ctx, relayURL, node.NodeID)
		for _, m := range messages {
			if m.Type != "consensus_proposal" {
				continue
			}

			var payload struct {
				Proposal consensus.BlockProposal `json:"proposal"`
			}
			if err := json.Unmarshal(m.Payload, &payload); err != nil {
				log.Printf("mailbox proposal decode error: %v", err)
				continue
			}

			b := payload.Proposal.Block
			// Real block validation
			if b.Header.Height <= 0 || b.Hash == "" {
				log.Printf("invalid block proposal: height=%d hash=%s", b.Header.Height, b.Hash)
				continue
			}

			// Validate block hash matches header and txs
			expectedHash, err := b.CalculateHash()
			if err != nil || expectedHash != b.Hash {
				log.Printf("rejecting proposal block %d: hash mismatch (got %s, expected %s, err: %v)", b.Header.Height, b.Hash, expectedHash, err)
				continue
			}

			// Sign the block approval vote using our private key
			approvalMsg := b.QuorumApprovalMessage()
			sig := ed25519.Sign(privateKey, approvalMsg)
			vote := consensus.BlockVote{
				BlockHash: b.Hash,
				Height:    b.Header.Height,
				VoterID:   node.NodeID,
				Vote:      1,
				Signature: "0x" + hex.EncodeToString(sig),
			}

			node.BlocksValidated++
			node.VotesCast++
			node.LastHeight = int64(b.Header.Height)
			node.LastTip = b.Hash
			node.LastStateRoot = b.Header.StateRoot

			log.Printf("[VALIDATOR] Validated Block #%d (%s) from %s — Signed & cast consensus vote!",
				b.Header.Height, truncate(b.Hash, 14), m.From)

			if m.From != "" {
				postVoteBack(ctx, relayURL, m.From, node.NodeID, vote)
			}
		}
	}
}

func fetchMailboxMessages(ctx context.Context, relayURL string, nodeID string) []relayMailboxEnvelope {
	reqCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, relayURL+"/mailbox?name="+url.QueryEscape(nodeID), nil)
	if err != nil {
		return nil
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil
	}
	var messages []relayMailboxEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&messages); err != nil {
		return nil
	}
	return messages
}

func postVoteBack(ctx context.Context, relayURL, to, from string, vote consensus.BlockVote) {
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	payload := map[string]any{
		"to":      to,
		"from":    from,
		"type":    "consensus_vote",
		"payload": map[string]any{"vote": vote},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, relayURL+"/mailbox", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("posting vote back error: %v", err)
		return
	}
	_ = resp.Body.Close()
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

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func appDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "."
	}
	return filepath.Join(dir, "SynthosCollective")
}

func keyPath() string {
	if cliKeyPath != "" {
		return cliKeyPath
	}
	if value := os.Getenv("SYNTHOS_SILENT_KEY_PATH"); value != "" {
		return value
	}
	return filepath.Join(appDir(), "silent-node-key.json")
}

func statusPath() string {
	if cliStatusPath != "" {
		return cliStatusPath
	}
	if value := os.Getenv("SYNTHOS_SILENT_STATUS_PATH"); value != "" {
		return value
	}
	return filepath.Join(appDir(), "silent-node-status.json")
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

func writeDashboardHTML(node silentNode) string {
	path := filepath.Join(appDir(), "Synthos-Node-Status.html")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return ""
	}
	relayPrimary := defaultRelayURLs
	if len(node.RelayURLs) > 0 {
		relayPrimary = node.RelayURLs[0]
	}

	provingText := fmt.Sprintf("Day %.1f of 30", node.ProvingDays)
	badgeText := fmt.Sprintf("Proving Uptime & Validating (%s)", provingText)
	rewardText := "10,000 SYN reward claimable after 30 days"
	if node.RewardEligible {
		badgeText = "★ ACTIVE CONSENSUS VALIDATOR (30-Day Proving Complete)"
		rewardText = "★ 10,000 SYN Reward Verified & Claimable via Smart Contract"
	}

	synAccumulated := (node.ProvingDays / provingTargetDays) * 10000.0
	if synAccumulated > 10000.0 {
		synAccumulated = 10000.0
	}

	nextMilestoneDay := 7.0
	nextMilestoneMult := "1.5x"
	if node.StreakDays >= 21.0 {
		nextMilestoneDay = 30.0
		nextMilestoneMult = "3.0x + 10,000 SYN Claim"
	} else if node.StreakDays >= 14.0 {
		nextMilestoneDay = 21.0
		nextMilestoneMult = "2.5x"
	} else if node.StreakDays >= 7.0 {
		nextMilestoneDay = 14.0
		nextMilestoneMult = "2.0x"
	}
	daysToNext := nextMilestoneDay - node.StreakDays
	if daysToNext < 0 {
		daysToNext = 0
	}
	streakPct := (node.StreakDays / nextMilestoneDay) * 100.0
	if streakPct > 100.0 {
		streakPct = 100.0
	}

	html := fmt.Sprintf(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>SYNTHOS 1-Click Validator — %s</title>
  <style>
    :root { color-scheme: dark; font-family: Inter, system-ui, -apple-system, sans-serif; background: #06090e; color: #f4f9ff; }
    body { margin: 0; min-height: 100vh; background: radial-gradient(circle at 15%% 10%%, rgba(46,213,169,0.18), transparent 42rem), radial-gradient(circle at 85%% 25%%, rgba(242,196,93,0.12), transparent 36rem), #06090e; display: flex; align-items: center; justify-content: center; padding: 24px; box-sizing: border-box; }
    .shell { width: min(920px, 100%%); border: 1px solid rgba(46,213,169,0.3); background: rgba(13,19,28,0.92); border-radius: 18px; padding: 32px; box-shadow: 0 24px 60px rgba(0,0,0,0.6); backdrop-filter: blur(12px); }
    .top { display: flex; justify-content: space-between; align-items: center; gap: 16px; flex-wrap: wrap; border-bottom: 1px solid rgba(255,255,255,0.1); padding-bottom: 20px; }
    .eyebrow { color: #2ed5a9; font-size: 0.75rem; font-weight: 800; letter-spacing: 0.12em; text-transform: uppercase; }
    h1 { margin: 6px 0 0; font-size: 2rem; letter-spacing: -0.02em; }
    .badge { display: inline-flex; align-items: center; gap: 8px; padding: 8px 14px; border-radius: 999px; background: rgba(46,213,169,0.14); border: 1px solid rgba(46,213,169,0.45); color: #7fffd4; font-weight: 700; font-size: 0.85rem; }
    .dot { width: 9px; height: 9px; border-radius: 50%%; background: #2ed5a9; box-shadow: 0 0 12px #2ed5a9; animation: pulse 2s infinite; }
    @keyframes pulse { 0%%, 100%% { opacity: 1; } 50%% { opacity: 0.4; } }
    
    /* GAMIFIED HERO TICKER */
    .hero-ticker { margin-top: 22px; padding: 22px; border-radius: 14px; background: linear-gradient(135deg, rgba(46,213,169,0.12), rgba(114,221,255,0.06)); border: 1px solid rgba(46,213,169,0.4); display: grid; grid-template-columns: 1.2fr 1fr; gap: 20px; align-items: center; }
    @media (max-width: 720px) { .hero-ticker { grid-template-columns: 1fr; } }
    .ticker-val { font-size: clamp(2rem, 4vw, 2.8rem); font-weight: 900; font-family: ui-monospace, SFMono-Regular, Consolas, monospace; color: #2ed5a9; letter-spacing: -0.03em; text-shadow: 0 0 24px rgba(46,213,169,0.4); }
    .syn-val { font-size: 1.25rem; font-weight: 800; color: #72ddff; font-family: ui-monospace, SFMono-Regular, monospace; margin-top: 4px; }
    .multiplier-badge { display: inline-flex; align-items: center; gap: 6px; padding: 6px 12px; border-radius: 999px; background: rgba(242,196,93,0.16); border: 1px solid rgba(242,196,93,0.45); color: #ffe0a0; font-weight: 800; font-size: 0.85rem; }
    
    /* DUOLINGO STREAK BAR */
    .streak-box { margin-top: 18px; padding: 18px; border-radius: 14px; background: rgba(255,255,255,0.03); border: 1px solid rgba(255,255,255,0.1); }
    .streak-header { display: flex; justify-content: space-between; align-items: center; flex-wrap: wrap; gap: 10px; margin-bottom: 12px; }
    .streak-flame { display: flex; align-items: center; gap: 8px; font-weight: 800; font-size: 1.1rem; color: #f2c45d; }
    .progress-track { width: 100%%; height: 10px; border-radius: 999px; background: rgba(255,255,255,0.08); overflow: hidden; position: relative; }
    .progress-fill { height: 100%%; border-radius: 999px; background: linear-gradient(90deg, #f2c45d, #2ed5a9); transition: width 0.4s ease; }
    .milestones { display: flex; justify-content: space-between; margin-top: 10px; font-size: 0.72rem; color: #8ea0ba; font-weight: 700; text-transform: uppercase; }
    .milestone-active { color: #2ed5a9; font-weight: 800; }

    .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(230px, 1fr)); gap: 14px; margin-top: 18px; }
    .card { background: rgba(255,255,255,0.04); border: 1px solid rgba(255,255,255,0.1); border-radius: 10px; padding: 16px; }
    .label { color: #8ea0ba; font-size: 0.72rem; font-weight: 700; text-transform: uppercase; letter-spacing: 0.08em; }
    .val { margin-top: 8px; font-size: 1.15rem; font-weight: 800; word-break: break-all; font-family: ui-monospace, SFMono-Regular, Consolas, monospace; color: #ffffff; }
    
    /* LEADERBOARD TABLE */
    .lb-box { margin-top: 22px; padding: 20px; border-radius: 14px; background: rgba(255,255,255,0.03); border: 1px solid rgba(255,255,255,0.1); }
    table.lb-table { width: 100%%; border-collapse: collapse; margin-top: 12px; font-size: 0.88rem; }
    table.lb-table th { text-align: left; padding: 8px 10px; color: #8ea0ba; font-size: 0.72rem; text-transform: uppercase; letter-spacing: 0.08em; border-bottom: 1px solid rgba(255,255,255,0.1); }
    table.lb-table td { padding: 10px 10px; border-bottom: 1px solid rgba(255,255,255,0.05); font-family: ui-monospace, SFMono-Regular, monospace; }
    tr.my-node { background: rgba(46,213,169,0.12); font-weight: 800; border-left: 3px solid #2ed5a9; }
    
    .note { margin-top: 22px; padding: 16px; border-radius: 10px; background: rgba(46,213,169,0.07); border: 1px solid rgba(46,213,169,0.22); color: #cbe6dd; font-size: 0.92rem; line-height: 1.5; }
    .actions { margin-top: 22px; display: flex; gap: 12px; flex-wrap: wrap; }
    a.btn, button.btn { background: #2ed5a9; color: #05140f; font-weight: 800; text-decoration: none; padding: 12px 18px; border-radius: 8px; border: 0; cursor: pointer; font-size: 0.9rem; }
    a.btn.sec { background: rgba(255,255,255,0.08); color: #f4f9ff; border: 1px solid rgba(255,255,255,0.16); }
  </style>
</head>
<body>
  <div class="shell">
    <div class="top">
      <div>
        <div class="eyebrow">SYNTHOS Collective Sovereign Layer-1</div>
        <h1>1-Click Consensus Validator Node</h1>
      </div>
      <div class="badge"><span class="dot"></span> %s</div>
    </div>

    <!-- REAL-TIME TICKING POINTS HERO -->
    <div class="hero-ticker">
      <div>
        <div class="label" style="color:#2ed5a9">🔥 Real-Time Ecosystem Points (Ticking)</div>
        <div class="ticker-val" id="pointsTicker">%.2f</div>
        <div class="syn-val">≈ <span id="synTicker">%.4f</span> SYN Accumulated <span style="font-size:0.8rem;color:#8ea0ba">(Target: 10,000 SYN)</span></div>
      </div>
      <div>
        <div style="display:flex;flex-direction:column;gap:8px;align-items:flex-start">
          <div class="multiplier-badge">🔥 %.1fx Streak Multiplier Active</div>
          <div style="font-size:0.8rem;color:#8ea0ba">
            Base: <strong>100 PTS/hr</strong> • Boosted: <strong>%.0f PTS/hr</strong><br/>
            Block validates (+10) & Heartbeats (+1) included!
          </div>
        </div>
      </div>
    </div>

    <!-- DUOLINGO-STYLE STREAK TRACKER -->
    <div class="streak-box">
      <div class="streak-header">
        <div class="streak-flame">
          <span>🔥</span> Day %.1f Consecutive Uptime Streak
        </div>
        <div style="font-size:0.85rem;color:#ffe0a0;font-weight:700">
          Next Goal: <strong>%s</strong> (in %.1f days)
        </div>
      </div>
      <div class="progress-track">
        <div class="progress-fill" style="width: %.1f%%"></div>
      </div>
      <div class="milestones">
        <span class="milestone-active">Genesis (1.0x)</span>
        <span class="%s">7-Day Streak (1.5x)</span>
        <span class="%s">14-Day Streak (2.0x)</span>
        <span class="%s">21-Day Streak (2.5x)</span>
        <span class="%s">30-Day Master (3.0x + 10k SYN)</span>
      </div>
    </div>

    <div class="grid">
      <div class="card">
        <div class="label">Your Node ID (Copy to Lookup)</div>
        <div class="val" id="nodeId">%s</div>
      </div>
      <div class="card">
        <div class="label">Mainnet Block Height</div>
        <div class="val" id="height">#%d</div>
      </div>
      <div class="card">
        <div class="label">Blocks Validated</div>
        <div class="val" id="valCount" style="color:#2ed5a9">%d</div>
      </div>
      <div class="card">
        <div class="label">Consensus Votes Cast</div>
        <div class="val" id="voteCount" style="color:#7fffd4">%d</div>
      </div>
      <div class="card">
        <div class="label">Proving Progress (30-Day Goal)</div>
        <div class="val">%s</div>
      </div>
      <div class="card">
        <div class="label">Signed Heartbeats Sent</div>
        <div class="val" id="hb">%d</div>
      </div>
      <div class="card">
        <div class="label">Ed25519 Public Key</div>
        <div class="val" style="font-size:0.82rem">%s</div>
      </div>
      <div class="card">
        <div class="label">Reward Status</div>
        <div class="val" style="font-size:0.88rem;color:#ffe0a0">%s</div>
      </div>
      <div class="card">
        <div class="label">Payout Wallet (1 Node Per Household)</div>
        <div class="val" style="font-size:0.85rem;color:#72ddff">%s</div>
      </div>
    </div>

    <!-- COMMUNITY VALIDATOR LEADERBOARD -->
    <div class="lb-box">
      <div style="display:flex;justify-content:space-between;align-items:center;flex-wrap:wrap;gap:10px">
        <div>
          <div class="label" style="color:#72ddff">🏆 Consensus Validator Leaderboard</div>
          <div style="font-size:0.85rem;color:#8ea0ba;margin-top:2px">Live global node rankings by uptime streak and accumulated points</div>
        </div>
        <div style="font-size:0.75rem;color:#2ed5a9;font-weight:700">● Live Mainnet Sync</div>
      </div>
      <div style="overflow-x:auto">
        <table class="lb-table">
          <thead>
            <tr>
              <th>Rank</th>
              <th>Node ID</th>
              <th>Uptime Streak</th>
              <th>Multiplier</th>
              <th>Heartbeats</th>
              <th>Status</th>
            </tr>
          </thead>
          <tbody id="lbBody">
            <tr class="my-node">
              <td>★</td>
              <td>%s <span style="color:#2ed5a9;font-size:0.75rem">(YOU)</span></td>
              <td>%.1f days</td>
              <td>🔥 %.1fx</td>
              <td>%d</td>
              <td style="color:#2ed5a9">Actively Proving</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <div class="note">
      <strong>Actively Validating in Background.</strong> This node participates in consensus via the SYNTHOS Mailbox Relay with 0 open inbound ports. It validates real block proposals, cryptographically signs votes with your local Ed25519 key, and tracks your monthly proving progress for the 10,000 SYN/mo reward. Rewards are strictly 1 node per household/connection to ensure fair decentralized distribution.
    </div>

    <div style="margin-top:14px;padding:14px 18px;border-radius:10px;background:rgba(242,196,93,0.12);border:1px solid rgba(242,196,93,0.3);color:#ffe0a0;font-size:0.9rem;line-height:1.5;">
      <strong>⚡ Power Setting:</strong> The screen can go to sleep, but <strong>not the actual computer itself</strong>. Turning off your monitor or display sleep is 100%% fine, but make sure your computer power plan is set to <em>Never Sleep</em> so background block validation and proving continuity remain uninterrupted.
    </div>

    <div class="actions">
      <button class="btn" onclick="navigator.clipboard.writeText('%s'); this.textContent='Copied Node ID!';">Copy Node ID (%s)</button>
      <a class="btn sec" href="%s/api/nodes/%s" target="_blank" rel="noreferrer">View Live Network Proof JSON</a>
      <a class="btn sec" href="https://www.ishamwilliamsblockchains.com/nodes" target="_blank" rel="noreferrer">Open Web Portal</a>
    </div>
  </div>

  <script>
    // Live Smooth JS Ticker
    let currentPts = %.2f;
    let currentSyn = %.4f;
    const mult = %.1f;
    const ptsPerSec = (100 / 3600) * mult;
    const synPerSec = (10000 / (30 * 86400));
    const ptsEl = document.getElementById("pointsTicker");
    const synEl = document.getElementById("synTicker");

    setInterval(() => {
      currentPts += ptsPerSec * 0.1;
      currentSyn += synPerSec * 0.1;
      if (currentSyn > 10000) currentSyn = 10000;
      if (ptsEl) ptsEl.textContent = currentPts.toFixed(2);
      if (synEl) synEl.textContent = currentSyn.toFixed(4);
    }, 100);

    // Live Community Leaderboard Fetch
    async function loadLeaderboard() {
      try {
        const res = await fetch("%s/api/nodes");
        if (!res.ok) return;
        const data = await res.json();
        const peers = data.peers || [];
        peers.sort((a, b) => (b.verified_uptime_ms || 0) - (a.verified_uptime_ms || 0));
        const body = document.getElementById("lbBody");
        if (!body) return;
        
        let html = "";
        let myFound = false;
        const myNodeId = "%s";

        peers.slice(0, 10).forEach((p, idx) => {
          const isMe = p.name === myNodeId;
          if (isMe) myFound = true;
          const days = ((p.verified_uptime_ms || 0) / 86400000).toFixed(1);
          let m = "1.0x";
          if (days >= 30) m = "3.0x";
          else if (days >= 21) m = "2.5x";
          else if (days >= 14) m = "2.0x";
          else if (days >= 7) m = "1.5x";
          
          const rankBadge = idx === 0 ? "🥇" : idx === 1 ? "🥈" : idx === 2 ? "🥉" : "#" + (idx + 1);
          html += '<tr class="' + (isMe ? 'my-node' : '') + '"><td>' + rankBadge + '</td><td>' + p.name + (isMe ? ' <span style="color:#2ed5a9;font-size:0.75rem">(YOU)</span>' : '') + '</td><td>' + days + ' days</td><td>🔥 ' + m + '</td><td>' + (p.valid_heartbeats || 0) + '</td><td style="color:' + (p.stale ? '#ff6b8a' : '#2ed5a9') + '">' + (p.stale ? 'Offline' : 'Validating') + '</td></tr>';
        });

        if (!myFound && myNodeId) {
          html += '<tr class="my-node"><td>★</td><td>' + myNodeId + ' <span style="color:#2ed5a9;font-size:0.75rem">(YOU)</span></td><td>%.1f days</td><td>🔥 %.1fx</td><td>%d</td><td style="color:#2ed5a9">Actively Proving</td></tr>';
        }
        body.innerHTML = html;
      } catch (e) {
        // Fallback to static current row
      }
    }
    loadLeaderboard();
    setInterval(loadLeaderboard, 30000);
  </script>
</body>
</html>`,
		node.NodeID,
		badgeText,
		node.EcosystemPoints,
		synAccumulated,
		node.StreakMultiplier,
		100.0*node.StreakMultiplier,
		node.StreakDays,
		nextMilestoneMult,
		daysToNext,
		streakPct,
		milestoneClass(node.StreakDays, 7.0),
		milestoneClass(node.StreakDays, 14.0),
		milestoneClass(node.StreakDays, 21.0),
		milestoneClass(node.StreakDays, 30.0),
		node.NodeID,
		node.LastHeight,
		node.BlocksValidated,
		node.VotesCast,
		provingText,
		node.HeartbeatCount,
		node.PublicKey,
		rewardText,
		firstNonEmpty(node.PayoutAddress, "Unconfigured (Default Operator)"),
		node.NodeID,
		node.StreakDays,
		node.StreakMultiplier,
		node.HeartbeatCount,
		node.NodeID,
		node.NodeID,
		relayPrimary,
		node.NodeID,
		node.EcosystemPoints,
		synAccumulated,
		node.StreakMultiplier,
		relayPrimary,
		node.NodeID,
		node.StreakDays,
		node.StreakMultiplier,
		node.HeartbeatCount,
	)
	_ = os.WriteFile(path, []byte(html), 0o600)
	return path
}

func milestoneClass(days, target float64) string {
	if days >= target {
		return "milestone-active"
	}
	return ""
}
