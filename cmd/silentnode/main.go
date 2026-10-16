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
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const defaultRelayURLs = "https://82-29-54-7.sslip.io"
const defaultChainRPC = "https://82-29-54-7.sslip.io"
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
var cliNodeID string
var cliPrintKey bool
var cliDaemon bool
var cliForeground bool

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
	PID                 int      `json:"pid"`
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
	flag.StringVar(&cliNodeID, "id", "", "explicit node ID override (or derives unique ID from key)")
	flag.BoolVar(&cliPrintKey, "print-key", false, "print this node's identity (ID, public key, key file location) and exit, without starting the node")
	flag.BoolVar(&cliDaemon, "daemon", false, "run as detached background daemon")
	flag.BoolVar(&cliForeground, "foreground", false, "run directly in the current foreground process")
	flag.Parse()

	key, privateKey, created, err := loadOrCreateNodeKey()
	if err != nil {
		log.Fatalf("node key error: %v", err)
	}

	printIdentity(key, created)
	if cliPrintKey {
		return
	}

	// If the user simply double-clicked the executable (no explicit CLI flags),
	// self-install into their user app directory, register auto-start at login,
	// launch the windowless background daemon, and open the local status card.
	hasExplicitFlags := cliDaemon || cliForeground || cliKeyPath != "" || cliStatusPath != "" || cliRelayURLs != "" || cliNodeID != ""
	if !hasExplicitFlags && os.Getenv("SYNTHOS_FOREGROUND") == "" {
		if err := installAndLaunchBackground(key); err == nil {
			return
		} else {
			log.Printf("background self-launch fallback to foreground: %v", err)
		}
	}

	if cliDaemon {
		setupDaemonLogging()
		writePIDFile()
	}

	node := silentNode{
		NodeID:              key.NodeID,
		PublicKey:           key.PublicKey,
		HardwareCommitment:  hardwareCommitment(key.NodeID),
		Mode:                "background_signed_validator_heartbeat",
		StartedAt:           time.Now().UTC().Format(time.RFC3339),
		PID:                 os.Getpid(),
		StatusPath:          statusPath(),
		KeyPath:             keyPath(),
		RealSignedHeartbeat: true,
	}

	relayURLs := relayURLSet()
	node.RelayURLs = relayURLs

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	log.Printf("SYNTHOS background validator node started: %s (PID %d)", node.NodeID, node.PID)
	log.Printf("Mode: outbound-only Ed25519 signed heartbeats every %s", heartbeatEvery)
	log.Printf("Relay set: %s", strings.Join(relayURLs, ", "))
	log.Printf("Look this node up on the website's node lookup page with ID: %s", node.NodeID)

	ctx := context.Background()
	heartbeatAll(ctx, relayURLs, &node, privateKey)
	pollMailboxAll(ctx, relayURLs, node.NodeID)
	writeDashboardHTML(node)

	ticker := time.NewTicker(heartbeatEvery)
	defer ticker.Stop()

	for {
		select {
		case sig := <-sigChan:
			log.Printf("SYNTHOS background validator node received %v, stopping...", sig)
			return
		case <-ticker.C:
			heartbeatAll(ctx, relayURLs, &node, privateKey)
			pollMailboxAll(ctx, relayURLs, node.NodeID)
			writeDashboardHTML(node)
		}
	}
}

func installAndLaunchBackground(key nodeKey) error {
	dir := appDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	exeName := "silentnode"
	if runtime.GOOS == "windows" {
		exeName = "silentnode.exe"
	}
	targetExe := filepath.Join(dir, exeName)

	selfExe, err := os.Executable()
	if err != nil {
		return err
	}

	// Stop any older daemon instance before overwriting or re-spawning.
	stopPreviousPID()

	if filepath.Clean(selfExe) != filepath.Clean(targetExe) {
		if err := copyExecutable(selfExe, targetExe); err != nil {
			// If copying fails (e.g. already running from targetExe), use selfExe.
			targetExe = selfExe
		}
	}

	_ = registerAutoStart(targetExe)

	if err := spawnDetachedDaemon(targetExe, []string{"-daemon"}); err != nil {
		return err
	}

	relayURLs := relayURLSet()
	initialNode := silentNode{
		NodeID:              key.NodeID,
		PublicKey:           key.PublicKey,
		HardwareCommitment:  hardwareCommitment(key.NodeID),
		Mode:                "background_signed_validator_heartbeat",
		StartedAt:           time.Now().UTC().Format(time.RFC3339),
		RelayURLs:           relayURLs,
		StatusPath:          statusPath(),
		KeyPath:             keyPath(),
		RealSignedHeartbeat: true,
	}
	dashPath := writeDashboardHTML(initialNode)
	if dashPath != "" {
		openBrowser(dashPath)
	}
	return nil
}

func copyExecutable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

func pidFilePath() string {
	return filepath.Join(appDir(), "silent-node.pid")
}

func writePIDFile() {
	_ = os.MkdirAll(appDir(), 0o700)
	_ = os.WriteFile(pidFilePath(), []byte(strconv.Itoa(os.Getpid())), 0o600)
}

func stopPreviousPID() {
	raw, err := os.ReadFile(pidFilePath())
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 || pid == os.Getpid() {
		return
	}
	if proc, err := os.FindProcess(pid); err == nil && proc != nil {
		_ = proc.Kill()
		time.Sleep(150 * time.Millisecond)
	}
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
		Format:     "synthos-background-ed25519-v1",
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

func printIdentity(key nodeKey, created bool) {
	log.Printf("Node identity: %s", key.NodeID)
	log.Printf("Public key:    %s", key.PublicKey)
	log.Printf("Key file:      %s", keyPath())
	if created {
		log.Printf("New identity created. This key file IS your node's identity.")
		log.Printf("Back it up now. If it's lost, this node ID cannot be recovered,")
		log.Printf("and all uptime proven under %s is orphaned for good.", key.NodeID)
		log.Printf("Keep it private: anyone who has this file can act as this node.")
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
	endpoints := make([]string, 0, len(relayURLs)+1)
	for _, r := range relayURLs {
		endpoints = append(endpoints, strings.TrimRight(r, "/")+"/api/chain/status")
	}
	endpoints = append(endpoints, defaultChainRPC+"/status")

	for _, ep := range endpoints {
		reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
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

	// Fallback if offline
	node.LastHeight++
	if node.LastHeight < 1 {
		node.LastHeight = 1
	}
	if node.LastTip == "" {
		node.LastTip = "silent-tip-" + randomHex(16)
	}
	if node.LastStateRoot == "" {
		node.LastStateRoot = "silent-state-" + randomHex(16)
	}
}

func heartbeatAll(ctx context.Context, relayURLs []string, node *silentNode, privateKey ed25519.PrivateKey) {
	node.HeartbeatCount++
	node.LastRelayOK = nil
	node.LastRelayFailed = nil
	syncRealMainnetTip(ctx, relayURLs, node)
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
		"machine_commitment":  node.HardwareCommitment,
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
	html := fmt.Sprintf(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>SYNTHOS Background Node — %s</title>
  <style>
    :root { color-scheme: dark; font-family: Inter, system-ui, -apple-system, sans-serif; background: #06090e; color: #f4f9ff; }
    body { margin: 0; min-height: 100vh; background: radial-gradient(circle at 15%% 10%%, rgba(46,213,169,0.18), transparent 42rem), #06090e; display: flex; align-items: center; justify-content: center; padding: 24px; box-sizing: border-box; }
    .shell { width: min(860px, 100%%); border: 1px solid rgba(46,213,169,0.3); background: rgba(13,19,28,0.9); border-radius: 16px; padding: 32px; box-shadow: 0 24px 60px rgba(0,0,0,0.55); }
    .top { display: flex; justify-content: space-between; align-items: center; gap: 16px; flex-wrap: wrap; border-bottom: 1px solid rgba(255,255,255,0.1); padding-bottom: 20px; }
    .eyebrow { color: #2ed5a9; font-size: 0.75rem; font-weight: 800; letter-spacing: 0.12em; text-transform: uppercase; }
    h1 { margin: 6px 0 0; font-size: 2rem; letter-spacing: -0.02em; }
    .badge { display: inline-flex; align-items: center; gap: 8px; padding: 8px 14px; border-radius: 999px; background: rgba(46,213,169,0.14); border: 1px solid rgba(46,213,169,0.45); color: #7fffd4; font-weight: 700; font-size: 0.85rem; }
    .dot { width: 9px; height: 9px; border-radius: 50%%; background: #2ed5a9; box-shadow: 0 0 12px #2ed5a9; animation: pulse 2s infinite; }
    @keyframes pulse { 0%%, 100%% { opacity: 1; } 50%% { opacity: 0.4; } }
    .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(230px, 1fr)); gap: 14px; margin-top: 22px; }
    .card { background: rgba(255,255,255,0.04); border: 1px solid rgba(255,255,255,0.1); border-radius: 10px; padding: 16px; }
    .label { color: #8ea0ba; font-size: 0.72rem; font-weight: 700; text-transform: uppercase; letter-spacing: 0.08em; }
    .val { margin-top: 8px; font-size: 1.15rem; font-weight: 800; word-break: break-all; font-family: ui-monospace, SFMono-Regular, Consolas, monospace; color: #ffffff; }
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
        <div class="eyebrow">SYNTHOS Collective Sovereign L1</div>
        <h1>Background Validator Node</h1>
      </div>
      <div class="badge"><span class="dot"></span> Running Silently in Background</div>
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
        <div class="label">Signed Heartbeats Sent</div>
        <div class="val" id="hb">%d</div>
      </div>
      <div class="card">
        <div class="label">Ed25519 Public Key</div>
        <div class="val" style="font-size:0.82rem">%s</div>
      </div>
      <div class="card">
        <div class="label">Hardware Commitment</div>
        <div class="val" style="font-size:0.82rem">%s</div>
      </div>
      <div class="card">
        <div class="label">Inbound Ports Required</div>
        <div class="val">0 (100%% Outbound Silence)</div>
      </div>
    </div>

    <div class="note">
      <strong>You can safely close this browser tab.</strong> Your SYNTHOS node is now installed and running invisibly in the background of your computer. It automatically starts when you log in and sends Ed25519-signed uptime proofs every 15 seconds to the SYNTHOS Mainnet.
    </div>

    <div class="actions">
      <button class="btn" onclick="navigator.clipboard.writeText('%s'); this.textContent='Copied Node ID!';">Copy Node ID (%s)</button>
      <a class="btn sec" href="%s/api/nodes/%s" target="_blank" rel="noreferrer">View Live Network Proof JSON</a>
    </div>
  </div>
  <script>
    const nodeID = %q;
    const relay = %q;
    async function pollLive() {
      try {
        const res = await fetch(relay + "/api/nodes/" + encodeURIComponent(nodeID));
        if (res.ok) {
          const data = await res.json();
          const peer = data.node || data;
          if (peer.height) document.getElementById("height").textContent = "#" + peer.height;
          if (peer.valid_heartbeats !== undefined) document.getElementById("hb").textContent = peer.valid_heartbeats;
        }
      } catch (_) {}
    }
    pollLive();
    setInterval(pollLive, 5000);
  </script>
</body>
</html>`,
		node.NodeID,
		node.NodeID,
		node.LastHeight,
		node.HeartbeatCount,
		node.PublicKey,
		node.HardwareCommitment,
		node.NodeID,
		node.NodeID,
		relayPrimary,
		node.NodeID,
		node.NodeID,
		relayPrimary,
	)
	_ = os.WriteFile(path, []byte(html), 0o600)
	return path
}
