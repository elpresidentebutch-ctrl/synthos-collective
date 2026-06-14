package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"sync"
	"time"

	"synthos-collective/internal/chain"
	"synthos-collective/internal/rpc"
	"synthos-collective/internal/storage"
)

const (
	defaultAgentAddr = "127.0.0.1:8788"
	defaultRPCAddr   = "127.0.0.1:8080"
	registryURL      = "https://synthos-peer-registry.jamesishamwilliams.workers.dev"
)

type desktopAgent struct {
	mu                 sync.Mutex
	nodeID             string
	hardwareCommitment string
	dataDir            string
	agentAddr          string
	rpcAddr            string
	rpcServer          *http.Server
	running            bool
	lastHeartbeat      time.Time
	lastError          string
	stopHeartbeat      context.CancelFunc
	rewardConfig       rewardConfig
	rewardConfigPath   string
}

type rewardConfig struct {
	Network               string `json:"network"`
	ChainRPC              string `json:"chain_rpc"`
	SynCoin               string `json:"syn_coin"`
	AdopterRewards        string `json:"adopter_rewards"`
	ActivationReward      string `json:"activation_reward"`
	HeartbeatReward       string `json:"heartbeat_reward"`
	HeartbeatInterval     string `json:"heartbeat_interval_seconds"`
	MaxHeartbeatClaims    string `json:"max_heartbeat_claims"`
	RegistrationStatus    string `json:"registration_status"`
	LastRegistrationTx    string `json:"last_registration_tx"`
	LastHeartbeatRewardTx string `json:"last_heartbeat_reward_tx"`
	UpdatedAt             string `json:"updated_at"`
}

func main() {
	agent, err := newDesktopAgent()
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", agent.handleHome)
	mux.HandleFunc("/agent/status", agent.handleStatus)
	mux.HandleFunc("/agent/start", agent.handleStart)
	mux.HandleFunc("/agent/stop", agent.handleStop)
	mux.HandleFunc("/agent/heartbeat", agent.handleHeartbeat)
	mux.HandleFunc("/agent/rewards/config", agent.handleRewardConfig)

	addr := os.Getenv("SYNTHOS_DESKTOP_AGENT_ADDR")
	if addr == "" {
		addr = defaultAgentAddr
	}

	log.Printf("SYNTHOS desktop agent dashboard: http://%s", addr)
	log.Printf("Node ID: %s", agent.nodeID)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func newDesktopAgent() (*desktopAgent, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	dataDir := os.Getenv("SYNTHOS_DESKTOP_DATA_DIR")
	if dataDir == "" {
		dataDir = configDir + string(os.PathSeparator) + "SynthosCollective" + string(os.PathSeparator) + "desktop-node"
	}

	hardwareCommitment := hardwareCommitment()
	nodeID := "desktop-" + hardwareCommitment[:12]
	rpcAddr := os.Getenv("SYNTHOS_DESKTOP_RPC_ADDR")
	if rpcAddr == "" {
		rpcAddr = defaultRPCAddr
	}
	agentAddr := os.Getenv("SYNTHOS_DESKTOP_AGENT_ADDR")
	if agentAddr == "" {
		agentAddr = defaultAgentAddr
	}

	rewardConfigPath := os.Getenv("SYNTHOS_DESKTOP_REWARD_CONFIG")
	if rewardConfigPath == "" {
		rewardConfigPath = filepath.Join(dataDir, "reward-config.json")
	}

	a := &desktopAgent{
		nodeID:             nodeID,
		hardwareCommitment: hardwareCommitment,
		dataDir:            dataDir,
		agentAddr:          agentAddr,
		rpcAddr:            rpcAddr,
		rewardConfigPath:   rewardConfigPath,
	}
	a.rewardConfig = a.loadRewardConfig()
	return a, nil
}

func hardwareCommitment() string {
	hostname, _ := os.Hostname()
	currentUser, _ := user.Current()
	username := ""
	if currentUser != nil {
		username = currentUser.Username
	}
	sum := sha256.Sum256([]byte(hostname + "|" + username + "|synthos-desktop-agent-v1"))
	return hex.EncodeToString(sum[:])
}

func (a *desktopAgent) handleHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(dashboardHTML))
}

func (a *desktopAgent) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	a.writeStatus(w)
}

func (a *desktopAgent) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := a.startNode(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	a.writeStatus(w)
}

func (a *desktopAgent) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := a.stopNode(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	a.writeStatus(w)
}

func (a *desktopAgent) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := a.registerHeartbeat(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	a.writeStatus(w)
}

func (a *desktopAgent) handleRewardConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.mu.Lock()
		cfg := a.rewardConfig
		a.mu.Unlock()
		writeIndentedJSON(w, cfg)
	case http.MethodPost:
		var cfg rewardConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}
		if cfg.UpdatedAt == "" {
			cfg.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		}
		if cfg.RegistrationStatus == "" {
			cfg.RegistrationStatus = "configured_not_registered"
		}
		if err := a.saveRewardConfig(cfg); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeIndentedJSON(w, cfg)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *desktopAgent) startNode() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.running {
		return nil
	}
	if !portAvailable(a.rpcAddr) {
		a.lastError = "RPC port is already in use: " + a.rpcAddr
		return errors.New(a.lastError)
	}

	st, err := storage.New(a.dataDir)
	if err != nil {
		a.lastError = err.Error()
		return err
	}

	ch, err := loadOrCreateChain(st)
	if err != nil {
		a.lastError = err.Error()
		return err
	}

	server := &http.Server{
		Addr:    a.rpcAddr,
		Handler: rpc.NewServer(ch, st, nil).Handler(),
	}
	a.rpcServer = server
	a.running = true
	a.lastError = ""

	ctx, cancel := context.WithCancel(context.Background())
	a.stopHeartbeat = cancel
	go a.heartbeatLoop(ctx)

	go func() {
		log.Printf("SYNTHOS RPC node listening on http://%s", a.rpcAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.mu.Lock()
			a.running = false
			a.lastError = err.Error()
			a.mu.Unlock()
			log.Printf("SYNTHOS RPC stopped unexpectedly: %v", err)
		}
	}()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = a.registerHeartbeat(ctx)
	}()

	return nil
}

func (a *desktopAgent) stopNode(ctx context.Context) error {
	a.mu.Lock()
	if !a.running {
		a.mu.Unlock()
		return nil
	}
	server := a.rpcServer
	if a.stopHeartbeat != nil {
		a.stopHeartbeat()
	}
	a.running = false
	a.rpcServer = nil
	a.mu.Unlock()

	if server == nil {
		return nil
	}
	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return server.Shutdown(shutdownCtx)
}

func (a *desktopAgent) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			hbCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			_ = a.registerHeartbeat(hbCtx)
			cancel()
		}
	}
}

func (a *desktopAgent) registerHeartbeat(ctx context.Context) error {
	a.mu.Lock()
	running := a.running
	payload := map[string]any{
		"name":                a.nodeID,
		"url":                 "http://" + a.rpcAddr,
		"cloud":               "desktop-native",
		"background":          true,
		"hardware_commitment": a.hardwareCommitment,
		"running":             running,
		"heartbeat_at":        time.Now().UTC().Format(time.RFC3339),
	}
	a.mu.Unlock()

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, registryURL+"/register", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		a.setError(err)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		err := fmt.Errorf("registry heartbeat failed: %s", resp.Status)
		a.setError(err)
		return err
	}

	a.mu.Lock()
	a.lastHeartbeat = time.Now().UTC()
	a.lastError = ""
	a.mu.Unlock()
	return nil
}

func (a *desktopAgent) setError(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lastError = err.Error()
}

func (a *desktopAgent) writeStatus(w http.ResponseWriter) {
	a.mu.Lock()
	cfg := a.rewardConfig
	status := map[string]any{
		"node_id":             a.nodeID,
		"hardware_commitment": a.hardwareCommitment,
		"running":             a.running,
		"agent_url":           "http://" + a.agentAddr,
		"rpc_url":             "http://" + a.rpcAddr,
		"data_dir":            a.dataDir,
		"last_heartbeat":      a.lastHeartbeat,
		"last_error":          a.lastError,
		"adopter_rewards":     cfg,
	}
	a.mu.Unlock()

	writeIndentedJSON(w, status)
}

func (a *desktopAgent) loadRewardConfig() rewardConfig {
	cfg := rewardConfig{
		Network:            os.Getenv("SYNTHOS_REWARD_NETWORK"),
		ChainRPC:           os.Getenv("SYNTHOS_REWARD_CHAIN_RPC"),
		SynCoin:            os.Getenv("SYNTHOS_SYNCOIN_ADDRESS"),
		AdopterRewards:     os.Getenv("SYNTHOS_ADOPTER_REWARDS_ADDRESS"),
		ActivationReward:   os.Getenv("SYNTHOS_ADOPTER_ACTIVATION_REWARD"),
		HeartbeatReward:    os.Getenv("SYNTHOS_ADOPTER_HEARTBEAT_REWARD"),
		HeartbeatInterval:  os.Getenv("SYNTHOS_ADOPTER_HEARTBEAT_INTERVAL"),
		MaxHeartbeatClaims: os.Getenv("SYNTHOS_ADOPTER_MAX_HEARTBEAT_CLAIMS"),
		RegistrationStatus: "not_configured",
	}

	f, err := os.Open(a.rewardConfigPath)
	if err == nil {
		defer f.Close()
		_ = json.NewDecoder(f).Decode(&cfg)
	}

	if cfg.AdopterRewards != "" && cfg.RegistrationStatus == "" {
		cfg.RegistrationStatus = "configured_not_registered"
	}
	return cfg
}

func (a *desktopAgent) saveRewardConfig(cfg rewardConfig) error {
	if err := os.MkdirAll(filepath.Dir(a.rewardConfigPath), 0o700); err != nil {
		return err
	}
	f, err := os.Create(a.rewardConfigPath)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(cfg); err != nil {
		return err
	}
	a.mu.Lock()
	a.rewardConfig = cfg
	a.mu.Unlock()
	return nil
}

func writeIndentedJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func loadOrCreateChain(st *storage.Store) (*chain.Chain, error) {
	if snap, err := st.Load(); err == nil && snap != nil && len(snap.Blocks) > 0 && snap.State != nil {
		ch := &chain.Chain{
			ChainID: snap.ChainID,
			State:   snap.State,
			DEX:     chain.NewDEX(),
			Oracle:  chain.NewOracle(),
			Blocks:  snap.Blocks,
			Mempool: make(map[string]chain.Tx),
		}
		seedDEX(ch)
		return ch, nil
	}

	ch, err := chain.NewChain(chain.Genesis{
		ChainID: "synthos-desktop-local",
		Alloc: map[chain.Address]uint64{
			"agent-0": 100_000_000_000,
		},
		Metadata: map[string]any{"symbol": "SYN", "decimals": 0},
	})
	if err != nil {
		return nil, err
	}
	seedDEX(ch)
	return ch, st.Save(ch)
}

func seedDEX(ch *chain.Chain) {
	if ch.DEX == nil {
		ch.DEX = chain.NewDEX()
	}
	if len(ch.DEX.ListPools()) > 0 {
		return
	}
	ch.DEX.SeedPool("B12", 10_000_000, 50_000)
	ch.DEX.SeedPool("NGOT", 5_000_000, 100_000)
	ch.DEX.SeedPool("MOMENTUM", 2_000_000, 10_000)
}

func portAvailable(addr string) bool {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

const dashboardHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>SYNTHOS Desktop Node</title>
  <style>
    :root { color-scheme: dark; font-family: Inter, system-ui, sans-serif; background: #07090d; color: #f7fbff; }
    body { margin: 0; min-height: 100vh; background: linear-gradient(135deg, rgba(46, 213, 169, .16), transparent 36rem), #07090d; }
    main { width: min(980px, calc(100% - 32px)); margin: 0 auto; padding: 48px 0; }
    header { display: flex; justify-content: space-between; gap: 24px; align-items: end; border-bottom: 1px solid rgba(255,255,255,.14); padding-bottom: 24px; }
    h1 { margin: 0; font-size: clamp(2.4rem, 6vw, 5rem); line-height: .94; letter-spacing: 0; }
    .eyebrow, .label { color: #8ea0ba; font-size: .75rem; font-weight: 800; letter-spacing: .08em; text-transform: uppercase; }
    .pill { border: 1px solid rgba(46, 213, 169, .45); background: rgba(46, 213, 169, .12); color: #8cffd8; border-radius: 999px; padding: 8px 12px; white-space: nowrap; }
    .grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 14px; margin-top: 28px; }
    .card { border: 1px solid rgba(255,255,255,.14); background: rgba(255,255,255,.06); border-radius: 8px; padding: 18px; }
    .value { margin-top: 10px; font-size: 1.6rem; font-weight: 850; overflow-wrap: anywhere; }
    .actions { display: flex; flex-wrap: wrap; gap: 12px; margin-top: 24px; }
    button, a.button { border: 0; border-radius: 8px; padding: 12px 16px; font-weight: 800; color: #07110d; background: #2ed5a9; cursor: pointer; text-decoration: none; }
    button.secondary, a.secondary { color: #f7fbff; background: rgba(255,255,255,.12); border: 1px solid rgba(255,255,255,.18); }
    pre { white-space: pre-wrap; overflow: auto; max-height: 340px; color: #dbe8ff; }
    @media (max-width: 760px) { header { align-items: start; flex-direction: column; } .grid { grid-template-columns: 1fr; } }
  </style>
</head>
<body>
  <main>
    <header>
      <div>
        <div class="eyebrow">Global Collective DEX of SYNTHOS</div>
        <h1>Desktop Node</h1>
      </div>
      <div id="pill" class="pill">Checking...</div>
    </header>

    <section class="grid">
      <article class="card"><div class="label">Node ID</div><div id="nodeId" class="value">-</div></article>
      <article class="card"><div class="label">RPC</div><div id="rpc" class="value">-</div></article>
      <article class="card"><div class="label">Rewards</div><div id="rewards" class="value">-</div></article>
    </section>

    <div class="actions">
      <button onclick="startNode()">Start Node</button>
      <button class="secondary" onclick="stopNode()">Stop Node</button>
      <button class="secondary" onclick="heartbeat()">Heartbeat</button>
      <a id="rpcLink" class="button secondary" href="#" target="_blank" rel="noreferrer">Open RPC Status</a>
    </div>

    <section class="card" style="margin-top:14px">
      <div class="label">Adopter Rewards Contract</div>
      <div id="rewardContract" class="value">Not configured</div>
    </section>

    <section class="card" style="margin-top:14px">
      <div class="label">Live Agent Status</div>
      <pre id="raw">Loading...</pre>
    </section>
  </main>
  <script>
    async function post(path) {
      const res = await fetch(path, { method: "POST" });
      if (!res.ok) throw new Error(await res.text());
      return refresh();
    }
    const startNode = () => post("/agent/start").catch(showError);
    const stopNode = () => post("/agent/stop").catch(showError);
    const heartbeat = () => post("/agent/heartbeat").catch(showError);
    function showError(err) {
      document.getElementById("pill").textContent = "Error";
      document.getElementById("raw").textContent = err.message;
    }
    async function refresh() {
      const res = await fetch("/agent/status");
      const status = await res.json();
      document.getElementById("pill").textContent = status.running ? "Node running in background" : "Node stopped";
      document.getElementById("nodeId").textContent = status.node_id;
      document.getElementById("rpc").textContent = status.rpc_url;
      document.getElementById("rewards").textContent = status.adopter_rewards.registration_status || "not_configured";
      document.getElementById("rewardContract").textContent = status.adopter_rewards.adopter_rewards || "Not configured";
      document.getElementById("rpcLink").href = status.rpc_url + "/status";
      document.getElementById("raw").textContent = JSON.stringify(status, null, 2);
    }
    refresh();
    setInterval(refresh, 5000);
  </script>
</body>
</html>`
