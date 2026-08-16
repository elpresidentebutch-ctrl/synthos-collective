package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"synthos-collective/internal/chain"
	"synthos-collective/internal/wallet"
)

const staleAfter = 5 * time.Minute
const heartbeatFreshAfter = 2 * time.Minute
const rewardEpoch = 30 * 24 * time.Hour
const validatorMonthlyBaseRewardSYN = 5000
const validatorMonthlyBonusCapSYN = 2500

var safeName = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

type peer struct {
	Name               string   `json:"name"`
	URL                string   `json:"url"`
	Kind               string   `json:"kind,omitempty"`
	Network            string   `json:"network,omitempty"`
	Status             string   `json:"status,omitempty"`
	PublicKey          string   `json:"public_key,omitempty"`
	Capabilities       []string `json:"capabilities,omitempty"`
	Cloud              string   `json:"cloud"`
	Mode               string   `json:"mode"`
	InboundPorts       int      `json:"inbound_ports"`
	HardwareCommitment string   `json:"hardware_commitment,omitempty"`
	RegisteredAt       int64    `json:"registered_at"`
	LastSeen           int64    `json:"last_seen"`
	Stale              bool     `json:"stale,omitempty"`
	Role               string   `json:"role,omitempty"`
	ProofStatus        string   `json:"proof_status,omitempty"`
	Height             int64    `json:"height,omitempty"`
	Tip                string   `json:"tip,omitempty"`
	StateRoot          string   `json:"state_root,omitempty"`
	FirstHeartbeatAt   int64    `json:"first_heartbeat_at,omitempty"`
	LastHeartbeatAt    int64    `json:"last_heartbeat_at,omitempty"`
	ValidHeartbeats    uint64   `json:"valid_heartbeats,omitempty"`
	VerifiedUptimeMS   int64    `json:"verified_uptime_ms,omitempty"`
	LastNonce          string   `json:"last_nonce,omitempty"`
	HostedProofSession bool     `json:"hosted_proof_session,omitempty"`
}

type mailboxMessage struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	From      string `json:"from"`
	Payload   any    `json:"payload"`
	CreatedAt int64  `json:"created_at"`
}

type contactMessage struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	Topic     string `json:"topic"`
	Message   string `json:"message"`
	CreatedAt int64  `json:"created_at"`
}

type earlyAccessAsset struct {
	Symbol          string `json:"symbol"`
	Network         string `json:"network,omitempty"`
	ChainID         string `json:"chainId,omitempty"`
	RPCURL          string `json:"rpcUrl,omitempty"`
	Address         string `json:"address,omitempty"`
	TreasuryAddress string `json:"treasuryAddress,omitempty"`
	Native          bool   `json:"native,omitempty"`
	Decimals        int    `json:"decimals"`
	USDPrice        string `json:"usdPrice,omitempty"`
	Enabled         bool   `json:"enabled"`
}

type earlyAccessPaymentIntent struct {
	ID                string `json:"id"`
	Status            string `json:"status"`
	BuyerWallet       string `json:"buyerWallet,omitempty"`
	SynthosAddress    string `json:"synthosAddress"`
	AssetSymbol       string `json:"assetSymbol"`
	Network           string `json:"network,omitempty"`
	PaymentAddress    string `json:"paymentAddress"`
	PaymentAsset      string `json:"paymentAsset,omitempty"`
	PaymentAmount     string `json:"paymentAmount"`
	PaymentDecimals   int    `json:"paymentDecimals"`
	USDValueCents     uint64 `json:"usdValueCents"`
	SynAmount         uint64 `json:"synAmount"`
	TxHash            string `json:"txHash,omitempty"`
	SynthosTxID       string `json:"synthosTxId,omitempty"`
	CreatedAt         int64  `json:"createdAt"`
	UpdatedAt         int64  `json:"updatedAt"`
	ExpiresAt         int64  `json:"expiresAt"`
	VerificationError string `json:"verificationError,omitempty"`
}

type registryState struct {
	Peers               map[string]peer                     `json:"peers"`
	Mailbox             map[string][]mailboxMessage         `json:"mailbox"`
	Contacts            []contactMessage                    `json:"contacts,omitempty"`
	EarlyAccessPayments map[string]earlyAccessPaymentIntent `json:"early_access_payments,omitempty"`
}

type server struct {
	mu         sync.RWMutex
	state      registryState
	secret     string
	stateFile  string
	startedAt  time.Time
	maxMailbox int
}

type networkSnapshot struct {
	Peers           []peer
	ActiveTotal     int
	RegisteredTotal int
	Reachable       int
	Fresh           int
	Validators      int
	Immune          int
	Agents          int
	HighestHeight   int64
	Tip             string
	StateRoot       string
	Checkpoints     []map[string]any
}

func main() {
	var listen string
	var stateFile string
	var secret string
	flag.StringVar(&listen, "listen", defaultListen(":8090"), "HTTP listen address")
	flag.StringVar(&stateFile, "state", env("SYNTHOS_REGISTRY_STATE", ".synthos/cloudless-registry.json"), "registry state JSON path")
	flag.StringVar(&secret, "secret", os.Getenv("REGISTRY_SECRET"), "optional registry admin/mailbox secret")
	flag.Parse()

	s := &server{
		state: registryState{
			Peers:               map[string]peer{},
			Mailbox:             map[string][]mailboxMessage{},
			Contacts:            []contactMessage{},
			EarlyAccessPayments: map[string]earlyAccessPaymentIntent{},
		},
		secret:     secret,
		stateFile:  stateFile,
		startedAt:  time.Now(),
		maxMailbox: 100,
	}
	if err := s.load(); err != nil {
		log.Printf("registry state load warning: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/register", s.handleRegister)
	mux.HandleFunc("/peers", s.handlePeers)
	mux.HandleFunc("/peers/active", s.handleActivePeers)
	mux.HandleFunc("/peers/", s.handlePeerByName)
	mux.HandleFunc("/mailbox", s.handleMailbox)
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/network/status", s.handleAPINetworkStatus)
	mux.HandleFunc("/api/explorer/status", s.handleAPIExplorerStatus)
	mux.HandleFunc("/api/explorer/blocks", s.handleAPIExplorerBlocks)
	mux.HandleFunc("/api/explorer/mempool", s.handleAPIExplorerMempool)
	mux.HandleFunc("/api/bridge/status", s.handleAPIBridgeStatus)
	mux.HandleFunc("/api/bridge/events", s.handleAPIBridgeEvents)
	mux.HandleFunc("/api/nodes", s.handleAPINodes)
	mux.HandleFunc("/api/nodes/register", s.handleAPINodeRegister)
	mux.HandleFunc("/api/nodes/heartbeat", s.handleAPINodeHeartbeat)
	mux.HandleFunc("/api/nodes/provision", s.handleAPIProvisionNode)
	mux.HandleFunc("/api/nodes/", s.handleAPINodeByID)
	mux.HandleFunc("/api/contact", s.handleAPIContact)
	mux.HandleFunc("/api/early-access/config", s.handleAPIEarlyAccessConfig)
	mux.HandleFunc("/api/early-access/payment-intents", s.handleAPIEarlyAccessPaymentIntents)
	mux.HandleFunc("/api/early-access/payment-intents/", s.handleAPIEarlyAccessPaymentIntentByID)
	mux.HandleFunc("/index.html", s.handleWebsitePage)
	mux.HandleFunc("/nodes", s.handleWebsitePage)
	mux.HandleFunc("/nodes.html", s.handleWebsitePage)
	mux.HandleFunc("/chain", s.handleWebsitePage)
	mux.HandleFunc("/chain.html", s.handleWebsitePage)
	mux.HandleFunc("/explorer", s.handleWebsitePage)
	mux.HandleFunc("/explorer.html", s.handleWebsitePage)
	mux.HandleFunc("/bridge", s.handleWebsitePage)
	mux.HandleFunc("/bridge.html", s.handleWebsitePage)
	mux.HandleFunc("/dex", s.handleWebsitePage)
	mux.HandleFunc("/dex.html", s.handleWebsitePage)
	mux.HandleFunc("/api", s.handleWebsitePage)
	mux.HandleFunc("/api.html", s.handleWebsitePage)
	mux.HandleFunc("/early-access", s.handleEarlyAccessPage)
	mux.HandleFunc("/early-access.html", s.handleEarlyAccessPage)
	mux.HandleFunc("/early-adopters", s.handleEarlyAccessPage)
	mux.HandleFunc("/assets/", s.handleWebsiteAsset)
	mux.HandleFunc("/assets/early-access-sale.js", s.handleEarlyAccessWidget)
	mux.HandleFunc("/api/node/windows-installer.ps1", s.handleWindowsInstaller)
	mux.HandleFunc("/api/node/install.bat", s.handleWindowsInstallerBat)
	mux.HandleFunc("/downloads/silentnode.exe", s.handleSilentNodeBinary)

	log.Printf("SYNTHOS cloudless registry listening on %s", listen)
	log.Printf("state file: %s", stateFile)
	if secret == "" {
		log.Printf("REGISTRY_SECRET is not set; destructive/mailbox writes are open for local/dev use")
	}
	if err := http.ListenAndServe(listen, cors(mux)); err != nil {
		log.Fatal(err)
	}
}

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && wantsHTML(r) {
		s.serveWebsiteFile(w, r, "index.html")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"service": "synthos-cloudless-registry",
		"endpoints": []string{
			"GET /health",
			"POST /register",
			"GET /peers",
			"GET /peers/active",
			"GET /mailbox?name=NODE",
			"POST /mailbox",
			"GET /api/network/status",
			"GET /api/explorer/status",
			"GET /api/explorer/blocks",
			"GET /api/explorer/mempool",
			"GET /api/bridge/status",
			"GET /api/bridge/events",
			"GET /api/nodes",
			"POST /api/nodes/register",
			"POST /api/nodes/heartbeat",
			"GET /api/nodes/ID/status",
			"POST /api/nodes/provision",
			"POST /api/contact",
			"GET /api/early-access/config",
			"POST /api/early-access/payment-intents",
			"GET /api/early-access/payment-intents/ID",
			"POST /api/early-access/payment-intents/ID/verify",
			"GET /early-access",
			"GET /early-adopters",
			"GET /assets/early-access-sale.js",
			"GET /api/node/windows-installer.ps1",
			"DELETE /peers/NODE",
		},
	})
}

func wantsHTML(r *http.Request) bool {
	if r.URL.Path != "/" {
		return false
	}
	accept := r.Header.Get("Accept")
	return accept == "" || strings.Contains(accept, "text/html") || strings.Contains(accept, "*/*")
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	total := len(s.state.Peers)
	s.mu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"service":  "synthos-cloudless-registry",
		"peers":    total,
		"uptime_s": int64(time.Since(s.startedAt).Seconds()),
	})
}

func (s *server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Name               string   `json:"name"`
		URL                string   `json:"url"`
		Cloud              string   `json:"cloud"`
		Kind               string   `json:"kind"`
		Network            string   `json:"network"`
		Status             string   `json:"status"`
		PublicKey          string   `json:"public_key"`
		Capabilities       []string `json:"capabilities"`
		Mode               string   `json:"mode"`
		InboundPorts       int      `json:"inbound_ports"`
		HardwareCommitment string   `json:"hardware_commitment"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	name := sanitize(body.Name)
	if name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	if body.URL != "" && !strings.HasPrefix(body.URL, "http://") && !strings.HasPrefix(body.URL, "https://") {
		http.Error(w, "url must start with http:// or https://", http.StatusBadRequest)
		return
	}

	now := time.Now().UnixMilli()
	mode := truncate(body.Mode, 64)
	if mode == "" {
		if body.URL == "" {
			mode = "outbound_only"
		} else {
			mode = "reachable"
		}
	}
	entry := peer{
		Name:               name,
		URL:                truncate(body.URL, 256),
		Kind:               normalizeChoice(body.Kind, "validator", "validator", "immune"),
		Network:            normalizeChoice(body.Network, "testnet", "testnet", "mainnet"),
		Status:             truncate(defaultString(body.Status, "running"), 32),
		PublicKey:          truncate(body.PublicKey, 256),
		Capabilities:       normalizeCapabilities(body.Capabilities),
		Cloud:              truncate(defaultString(body.Cloud, "cloudless"), 32),
		Mode:               mode,
		InboundPorts:       body.InboundPorts,
		HardwareCommitment: truncate(body.HardwareCommitment, 128),
		RegisteredAt:       now,
		LastSeen:           now,
	}

	s.mu.Lock()
	if existing, ok := s.state.Peers[name]; ok {
		entry.RegisteredAt = existing.RegisteredAt
	}
	s.state.Peers[name] = entry
	s.mu.Unlock()

	if err := s.persist(); err != nil {
		log.Printf("persist warning: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "peer": name, "message": "registered", "entry": entry})
}

func (s *server) handlePeers(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/peers" || r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	s.writePeerList(w, false)
}

func (s *server) handleActivePeers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.writePeerList(w, true)
}

func (s *server) handlePeerByName(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.NotFound(w, r)
		return
	}
	if !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	name := sanitize(strings.TrimPrefix(r.URL.Path, "/peers/"))
	if name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	delete(s.state.Peers, name)
	s.mu.Unlock()
	if err := s.persist(); err != nil {
		log.Printf("persist warning: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": name})
}

func (s *server) handleMailbox(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		name := sanitize(r.URL.Query().Get("name"))
		if name == "" {
			http.Error(w, "name required", http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		messages := append([]mailboxMessage(nil), s.state.Mailbox[name]...)
		delete(s.state.Mailbox, name)
		s.mu.Unlock()
		if len(messages) > 0 {
			if err := s.persist(); err != nil {
				log.Printf("persist warning: %v", err)
			}
		}
		writeJSON(w, http.StatusOK, messages)
	case http.MethodPost:
		if !s.authorized(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var body struct {
			To      string `json:"to"`
			Name    string `json:"name"`
			ID      string `json:"id"`
			Type    string `json:"type"`
			From    string `json:"from"`
			Payload any    `json:"payload"`
			Message any    `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}
		to := sanitize(defaultString(body.To, body.Name))
		if to == "" {
			http.Error(w, "to required", http.StatusBadRequest)
			return
		}
		payload := body.Payload
		if payload == nil {
			payload = body.Message
		}
		msg := mailboxMessage{
			ID:        truncate(defaultString(body.ID, fmt.Sprintf("%d", time.Now().UnixNano())), 128),
			Type:      truncate(defaultString(body.Type, "message"), 64),
			From:      truncate(defaultString(body.From, "registry"), 64),
			Payload:   payload,
			CreatedAt: time.Now().UnixMilli(),
		}
		s.mu.Lock()
		s.state.Mailbox[to] = append(s.state.Mailbox[to], msg)
		if len(s.state.Mailbox[to]) > s.maxMailbox {
			s.state.Mailbox[to] = s.state.Mailbox[to][len(s.state.Mailbox[to])-s.maxMailbox:]
		}
		depth := len(s.state.Mailbox[to])
		s.mu.Unlock()
		if err := s.persist(); err != nil {
			log.Printf("persist warning: %v", err)
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "queued": true, "to": to, "id": msg.ID, "depth": depth})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *server) handleAPINetworkStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	now := time.Now()
	s.mu.RLock()
	peers := make([]peer, 0, len(s.state.Peers))
	for _, p := range s.state.Peers {
		p.Stale = now.Sub(time.UnixMilli(p.LastSeen)) > staleAfter
		peers = append(peers, p)
	}
	s.mu.RUnlock()
	sort.Slice(peers, func(i, j int) bool { return peers[i].Name < peers[j].Name })

	reachable := 0
	fresh := 0
	validators := 0
	immune := 0
	agents := 0
	activeTotal := 0
	var highestHeight int64
	for i := range peers {
		// A node only counts as "running" if it has heartbeated recently.
		// Registered-but-never-run and long-dead nodes are excluded, so the
		// counts reflect genuinely live infrastructure, not button clicks.
		if peers[i].Stale {
			continue
		}
		activeTotal++
		reachable++
		fresh++
		if peerHasCapability(peers[i], "immune_node") || peers[i].Kind == "immune" {
			immune++
		}
		if peers[i].Kind == "" || peers[i].Kind == "validator" {
			validators++
		}
		if len(peers[i].Capabilities) > 0 {
			agents++
		}
		if peers[i].Height > highestHeight {
			highestHeight = peers[i].Height
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                   len(peers) == 0 || reachable > 0,
		"service":              "synthos-website-backend",
		"network":              "synthos",
		"chain":                "SYNTHOS Collective",
		"mode":                 "Proof-of-Operation onboarding",
		"heartbeat_target_s":   15,
		"total":                activeTotal,
		"registered_total":     len(peers),
		"reachable":            reachable,
		"fresh_heartbeats":     fresh,
		"validators_running":   validators,
		"immune_nodes_running": immune,
		"agents_running":       agents,
		"highest_height":       highestHeight,
		"tip":                  "",
		"state_root":           "",
		"next_proposer":        nextProposer(peers),
		"majority_reachable":   len(peers) == 0 || reachable*3 >= len(peers)*2,
		"converged_tip":        true,
		"converged_state_root": true,
		"validators":           peers,
		"reward_policy":        validatorRewardPolicy(),
		"updated_at":           time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *server) handleAPIExplorerStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	snapshot := s.networkSnapshot(time.Now())
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                          true,
		"chain_id":                    env("SYNTHOS_CHAIN_ID", "synthos-mainnet-1"),
		"height":                      snapshot.HighestHeight,
		"tip":                         snapshot.Tip,
		"state_root":                  snapshot.StateRoot,
		"mode":                        "registry_checkpoint_explorer",
		"source":                      "signed_node_heartbeats",
		"rpc_attached":                strings.TrimSpace(os.Getenv("SYNTHOS_RPC_URL")) != "",
		"active_nodes":                snapshot.ActiveTotal,
		"registered_nodes":            snapshot.RegisteredTotal,
		"validators_running":          snapshot.Validators,
		"immune_nodes_running":        snapshot.Immune,
		"fresh_heartbeats":            snapshot.Fresh,
		"heartbeat_target_s":          15,
		"latest_reported_checkpoints": snapshot.Checkpoints,
		"updated_at":                  time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *server) handleAPIExplorerBlocks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if proxied := s.proxyRPCJSON(w, r, "/blocks"); proxied {
		return
	}
	from := 0
	if raw := r.URL.Query().Get("from"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			from = parsed
		}
	}
	snapshot := s.networkSnapshot(time.Now())
	blocks := make([]map[string]any, 0, len(snapshot.Checkpoints))
	for _, checkpoint := range snapshot.Checkpoints {
		height, _ := checkpoint["height"].(int64)
		if int(height) < from {
			continue
		}
		tip, _ := checkpoint["tip"].(string)
		stateRoot, _ := checkpoint["state_root"].(string)
		nodeID, _ := checkpoint["node_id"].(string)
		lastSeen, _ := checkpoint["last_seen"].(string)
		blocks = append(blocks, map[string]any{
			"hash":      tip,
			"tx":        []any{},
			"finalized": false,
			"source":    "reported_heartbeat_checkpoint",
			"header": map[string]any{
				"height":      height,
				"parent_hash": "",
				"timestamp":   lastSeen,
				"proposer_id": nodeID,
				"state_root":  stateRoot,
			},
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"blocks":       blocks,
		"source":       "signed_node_heartbeats",
		"rpc_attached": false,
		"note":         "Attach SYNTHOS_RPC_URL to this backend to show canonical RPC blocks and transactions.",
	})
}

func (s *server) handleAPIExplorerMempool(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if proxied := s.proxyRPCJSON(w, r, "/mempool"); proxied {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"size":         0,
		"tx":           []any{},
		"source":       "registry_only",
		"rpc_attached": false,
		"note":         "Mempool requires an attached SYNTHOS RPC node.",
	})
}

func (s *server) handleAPIBridgeStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if proxied := s.proxyRPCJSON(w, r, "/bridge/status"); proxied {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           false,
		"rpc_attached": false,
		"bridge": map[string]any{
			"native_locks":       0,
			"native_releases":    0,
			"processed_messages": 0,
		},
		"note": "Set SYNTHOS_RPC_URL on the backend to expose native bridge status.",
	})
}

func (s *server) handleAPIBridgeEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if proxied := s.proxyRPCJSON(w, r, "/bridge/events"); proxied {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           false,
		"rpc_attached": false,
		"events":       []any{},
		"count":        0,
		"note":         "Set SYNTHOS_RPC_URL on the backend to expose native bridge events.",
	})
}

func (s *server) handleAPINodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.writePeerList(w, false)
}

func (s *server) handleAPINodeRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		NodeID       string   `json:"node_id"`
		NodeIDCamel  string   `json:"nodeId"`
		PublicID     string   `json:"publicId"`
		PublicKey    string   `json:"public_key"`
		PublicKeyAlt string   `json:"publicKey"`
		Role         string   `json:"role"`
		Kind         string   `json:"kind"`
		Mode         string   `json:"mode"`
		Network      string   `json:"network"`
		Endpoint     string   `json:"endpoint"`
		URL          string   `json:"url"`
		Capabilities []string `json:"capabilities"`
		CreatedAt    string   `json:"created_at"`
		ChainID      string   `json:"chain_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	nodeID := sanitize(defaultString(defaultString(body.NodeID, body.NodeIDCamel), body.PublicID))
	if nodeID == "" {
		nodeID = "syn-node-" + shortID()
	}
	publicKey := truncate(defaultString(body.PublicKey, body.PublicKeyAlt), 256)
	if publicKey == "" {
		http.Error(w, "public_key required; push-button nodes must generate a client-side Ed25519 key", http.StatusBadRequest)
		return
	}
	role := normalizeRole(defaultString(defaultString(body.Role, body.Kind), body.Mode))
	kind := roleKind(role)
	endpoint := truncate(defaultString(body.Endpoint, body.URL), 256)
	if endpoint != "" && !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		http.Error(w, "endpoint must start with http:// or https://", http.StatusBadRequest)
		return
	}
	now := time.Now().UnixMilli()
	entry := peer{
		Name:         nodeID,
		URL:          endpoint,
		Kind:         kind,
		Network:      normalizeChoice(body.Network, "mainnet", "testnet", "mainnet"),
		Status:       "registered",
		PublicKey:    publicKey,
		Capabilities: normalizeCapabilities(body.Capabilities),
		Cloud:        "operator",
		Mode:         map[bool]string{true: "public_endpoint", false: "candidate"}[endpoint != ""],
		InboundPorts: map[bool]int{true: 1, false: 0}[endpoint != ""],
		RegisteredAt: now,
		LastSeen:     0,
		Role:         role,
		ProofStatus:  "registered",
	}
	s.mu.Lock()
	if existing, ok := s.state.Peers[nodeID]; ok {
		entry.RegisteredAt = existing.RegisteredAt
		entry.FirstHeartbeatAt = existing.FirstHeartbeatAt
		entry.LastHeartbeatAt = existing.LastHeartbeatAt
		entry.LastSeen = existing.LastSeen
		entry.ValidHeartbeats = existing.ValidHeartbeats
		entry.VerifiedUptimeMS = existing.VerifiedUptimeMS
		entry.LastNonce = existing.LastNonce
		entry.HostedProofSession = existing.HostedProofSession
		entry.Height = existing.Height
		entry.Tip = existing.Tip
		entry.StateRoot = existing.StateRoot
		entry.ProofStatus = proofStatus(entry, time.Now())
	}
	s.state.Peers[nodeID] = entry
	s.mu.Unlock()
	if err := s.persist(); err != nil {
		log.Printf("persist warning: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"node":          nodeStatus(entry, time.Now()),
		"message":       "node candidate registered; rewards require verified operation",
		"reward_policy": validatorRewardPolicy(),
	})
}

func (s *server) handleAPINodeHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		NodeID       string   `json:"node_id"`
		NodeIDCamel  string   `json:"nodeId"`
		Height       int64    `json:"height"`
		Tip          string   `json:"tip"`
		StateRoot    string   `json:"state_root"`
		StateRootAlt string   `json:"stateRoot"`
		Timestamp    string   `json:"timestamp"`
		Nonce        string   `json:"nonce"`
		Capabilities []string `json:"capabilities"`
		Signature    string   `json:"signature"`
		Endpoint     string   `json:"endpoint"`
		URL          string   `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	nodeID := sanitize(defaultString(body.NodeID, body.NodeIDCamel))
	if nodeID == "" {
		http.Error(w, "node_id required", http.StatusBadRequest)
		return
	}
	if body.Nonce == "" {
		http.Error(w, "nonce required", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	entry, ok := s.state.Peers[nodeID]
	if !ok {
		s.mu.Unlock()
		http.Error(w, "node is not registered", http.StatusNotFound)
		return
	}
	if entry.LastNonce != "" && body.Nonce <= entry.LastNonce {
		s.mu.Unlock()
		http.Error(w, "replayed or non-increasing nonce", http.StatusBadRequest)
		return
	}
	stateRoot := truncate(defaultString(body.StateRoot, body.StateRootAlt), 128)
	message := canonicalHeartbeatMessage(nodeID, body.Height, truncate(body.Tip, 128), stateRoot, body.Timestamp, body.Nonce)
	if err := verifyHeartbeatSignature(entry.PublicKey, body.Signature, message); err != nil {
		s.mu.Unlock()
		http.Error(w, "invalid heartbeat signature: "+err.Error(), http.StatusUnauthorized)
		return
	}

	now := time.Now()
	nowMS := now.UnixMilli()
	if entry.FirstHeartbeatAt == 0 {
		entry.FirstHeartbeatAt = nowMS
	}
	if entry.LastHeartbeatAt > 0 {
		delta := nowMS - entry.LastHeartbeatAt
		if delta > 0 {
			maxDelta := int64(heartbeatFreshAfter / time.Millisecond)
			if delta > maxDelta {
				delta = maxDelta
			}
			entry.VerifiedUptimeMS += delta
		}
	}
	entry.LastHeartbeatAt = nowMS
	entry.LastSeen = nowMS
	entry.ValidHeartbeats++
	entry.LastNonce = truncate(body.Nonce, 128)
	entry.HostedProofSession = false
	entry.Status = "proving"
	entry.ProofStatus = proofStatus(entry, now)
	entry.Height = body.Height
	entry.Tip = truncate(body.Tip, 128)
	entry.StateRoot = stateRoot
	if endpoint := truncate(defaultString(body.Endpoint, body.URL), 256); endpoint != "" {
		entry.URL = endpoint
		entry.Mode = "public_endpoint"
		entry.InboundPorts = 1
	}
	if len(body.Capabilities) > 0 {
		entry.Capabilities = normalizeCapabilities(body.Capabilities)
	}
	if len(entry.Capabilities) == 0 && !entry.HostedProofSession && entry.LastNonce != "" {
		entry.Capabilities = coreNodeCapabilities()
	}
	s.state.Peers[nodeID] = entry
	s.mu.Unlock()
	if err := s.persist(); err != nil {
		log.Printf("persist warning: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"node":    nodeStatus(entry, now),
		"message": "signed heartbeat accepted",
	})
}

func (s *server) handleAPINodeByID(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/nodes/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[1] != "status" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	nodeID := sanitize(parts[0])
	s.mu.RLock()
	entry, ok := s.state.Peers[nodeID]
	s.mu.RUnlock()
	if !ok {
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"node":          nodeStatus(entry, time.Now()),
		"reward_policy": validatorRewardPolicy(),
	})
}

func (s *server) handleAPIProvisionNode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Kind    string `json:"kind"`
		Network string `json:"network"`
		Label   string `json:"label"`
		URL     string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	kind := normalizeChoice(body.Kind, "validator", "validator", "immune")
	network := normalizeChoice(body.Network, "testnet", "testnet", "mainnet")
	label := sanitize(body.Label)
	if label == "" {
		label = fmt.Sprintf("%s-%s", kind, shortID())
	}
	nodeID := fmt.Sprintf("synthos-%s-%s", kind, label)
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		http.Error(w, "key generation failed", http.StatusInternalServerError)
		return
	}
	publicHex := hex.EncodeToString(publicKey)
	endpoint := truncate(body.URL, 256)
	if endpoint == "" {
		endpoint = "outbound-only"
	}

	entry := peer{
		Name:         nodeID,
		URL:          endpointURL(endpoint),
		Kind:         kind,
		Network:      network,
		Status:       "provisioned",
		PublicKey:    publicHex,
		Cloud:        "cloudless",
		Mode:         "outbound_only",
		InboundPorts: 0,
		RegisteredAt: time.Now().UnixMilli(),
		LastSeen:     0,
		Role:         map[string]string{"validator": "validator_candidate", "immune": "immune"}[kind],
		ProofStatus:  "registered",
	}

	s.mu.Lock()
	s.state.Peers[nodeID] = entry
	s.mu.Unlock()
	if err := s.persist(); err != nil {
		log.Printf("persist warning: %v", err)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"nodeId":        nodeID,
		"kind":          kind,
		"network":       network,
		"endpoint":      endpoint,
		"publicKey":     publicHex,
		"node":          nodeStatus(entry, time.Now()),
		"nodeConfig":    "",
		"startCommand":  "",
		"workerName":    nodeID,
		"deployCommand": "",
		"warning":       "Provisioning only prepares a node candidate. Generate and store private keys client-side, then submit signed heartbeats to prove operation.",
		"reward_policy": validatorRewardPolicy(),
	})
}

func (s *server) handleAPIContact(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Name    string `json:"name"`
		Email   string `json:"email"`
		Topic   string `json:"topic"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.Name) == "" || strings.TrimSpace(body.Email) == "" || strings.TrimSpace(body.Message) == "" {
		http.Error(w, "name, email, and message are required", http.StatusBadRequest)
		return
	}
	msg := contactMessage{
		ID:        "contact-" + shortID(),
		Name:      truncate(body.Name, 120),
		Email:     truncate(body.Email, 160),
		Topic:     truncate(defaultString(body.Topic, "General"), 80),
		Message:   truncate(body.Message, 4000),
		CreatedAt: time.Now().UnixMilli(),
	}
	s.mu.Lock()
	s.state.Contacts = append([]contactMessage{msg}, s.state.Contacts...)
	if len(s.state.Contacts) > 500 {
		s.state.Contacts = s.state.Contacts[:500]
	}
	s.mu.Unlock()
	if err := s.persist(); err != nil {
		log.Printf("persist warning: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ref": msg.ID, "message": "received"})
}

func (s *server) handleAPIEarlyAccessConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	saleContract := os.Getenv("SYNTHOS_EARLY_ACCESS_SALE_CONTRACT")
	complianceRegistry := os.Getenv("SYNTHOS_EARLY_ACCESS_COMPLIANCE_REGISTRY")
	chainID := os.Getenv("SYNTHOS_EARLY_ACCESS_CHAIN_ID")
	assets := earlyAccessAssetsFromEnv()
	distributor := earlyAccessDistributorConfig()
	// SYNTHOS is a public testnet; the token sale is on hold and CLOSED by
	// default. No payments are taken unless SYNTHOS_EARLY_ACCESS_SALE_OPEN=true.
	saleOpen := os.Getenv("SYNTHOS_EARLY_ACCESS_SALE_OPEN") == "true"

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                 true,
		"saleOpen":           saleOpen,
		"saleStatus":         map[bool]string{true: "open", false: "closed — public testnet, sale on hold"}[saleOpen],
		"walletOnboarding":   true,
		"configured":         saleOpen && saleContract != "" && complianceRegistry != "" && chainID != "",
		"paymentRails":       saleOpen,
		"paymentIntentUrl":   "/api/early-access/payment-intents",
		"chainId":            chainID,
		"chainName":          env("SYNTHOS_EARLY_ACCESS_CHAIN_NAME", "SYNTHOS"),
		"rpcUrls":            splitCSV(env("SYNTHOS_EARLY_ACCESS_RPC_URLS", "https://rpc.ishamwilliamsblockchains.com")),
		"saleContract":       saleContract,
		"complianceRegistry": complianceRegistry,
		"treasuryWallet":     env("SYNTHOS_EARLY_ACCESS_TREASURY_WALLET", "0x5d6f8FbAAB199E788ed9Cfcb3F7Fe2ac9c0450d2"),
		"distributionAgent":  distributor,
		"tokenPriceUsd":      "0.10",
		"activeTrancheSyn":   "250,000,000",
		"maxTrancheUsd":      "$25,000,000",
		"maxTrancheValueUsd": "25000000",
		"campaignReserveSyn": "1,750,000,000",
		"communitySourceBucket": env(
			"SYNTHOS_EARLY_ACCESS_SOURCE_BUCKET",
			"COMMUNITY_EARLY_ADOPTER_CAMPAIGNS",
		),
		"disclosureText":   env("SYNTHOS_EARLY_ACCESS_DISCLOSURE_TEXT", "SYNTHOS early access disclosure v1"),
		"disclosureHash":   os.Getenv("SYNTHOS_EARLY_ACCESS_DISCLOSURE_HASH"),
		"jurisdictionCode": env("SYNTHOS_EARLY_ACCESS_JURISDICTION", "US"),
		"jurisdictionHash": os.Getenv("SYNTHOS_EARLY_ACCESS_JURISDICTION_HASH"),
		"assets":           assets,
		"updated_at":       time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *server) handleAPIEarlyAccessPaymentIntents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Sale is CLOSED by default. SYNTHOS is a public testnet; no public token
	// sale runs until deliberately opened (real mainnet + legal clearance) by
	// setting SYNTHOS_EARLY_ACCESS_SALE_OPEN=true. This prevents any payment
	// from being taken while the sale is on hold.
	if os.Getenv("SYNTHOS_EARLY_ACCESS_SALE_OPEN") != "true" {
		http.Error(w, "the SYNTHOS early access sale is not open", http.StatusForbidden)
		return
	}
	var body struct {
		BuyerWallet    string `json:"buyerWallet"`
		SynthosAddress string `json:"synthosAddress"`
		AssetSymbol    string `json:"assetSymbol"`
		USDValue       string `json:"usdValue"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	body.SynthosAddress = strings.TrimSpace(body.SynthosAddress)
	body.AssetSymbol = strings.ToUpper(strings.TrimSpace(body.AssetSymbol))
	if body.SynthosAddress == "" || body.AssetSymbol == "" || strings.TrimSpace(body.USDValue) == "" {
		http.Error(w, "synthosAddress, assetSymbol, and usdValue are required", http.StatusBadRequest)
		return
	}
	usdCents, err := parseUSDCents(body.USDValue)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	synAmount := usdCents / 10 // $0.10 per SYN.
	if synAmount < 20 {
		http.Error(w, "minimum early access purchase is 20 SYN", http.StatusBadRequest)
		return
	}
	asset, ok := earlyAccessAssetBySymbol(body.AssetSymbol)
	if !ok || !asset.Enabled {
		http.Error(w, "payment asset is not enabled", http.StatusBadRequest)
		return
	}
	if asset.TreasuryAddress == "" {
		http.Error(w, "payment treasury address is not configured", http.StatusServiceUnavailable)
		return
	}
	paymentAmount, err := quotePaymentAmount(usdCents, asset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	now := time.Now().UnixMilli()
	intent := earlyAccessPaymentIntent{
		ID:              "syn-presale-" + shortID(),
		Status:          "pending_payment",
		BuyerWallet:     truncate(body.BuyerWallet, 128),
		SynthosAddress:  body.SynthosAddress,
		AssetSymbol:     asset.Symbol,
		Network:         asset.Network,
		PaymentAddress:  asset.TreasuryAddress,
		PaymentAsset:    asset.Address,
		PaymentAmount:   paymentAmount,
		PaymentDecimals: asset.Decimals,
		USDValueCents:   usdCents,
		SynAmount:       synAmount,
		CreatedAt:       now,
		UpdatedAt:       now,
		ExpiresAt:       time.Now().Add(30 * time.Minute).UnixMilli(),
	}

	s.mu.Lock()
	if s.state.EarlyAccessPayments == nil {
		s.state.EarlyAccessPayments = map[string]earlyAccessPaymentIntent{}
	}
	s.state.EarlyAccessPayments[intent.ID] = intent
	s.mu.Unlock()
	if err := s.persist(); err != nil {
		log.Printf("persist warning: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "intent": intent})
}

func (s *server) handleEarlyAccessPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>SYNTHOS Early Access</title>
  <meta name="description" content="SYNTHOS Collective early adopter presale." />
  <style>
    :root{color-scheme:dark;font-family:Inter,ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;--bg:#07090d;--text:#f5f8ff;--muted:#9aa9bd;--line:rgba(255,255,255,.13);--green:#31d39f;--cyan:#73d7ff}
    *{box-sizing:border-box}
    body{margin:0;min-height:100vh;background:linear-gradient(180deg,rgba(115,215,255,.08),transparent 520px),#07090d;color:var(--text)}
    header{position:sticky;top:0;z-index:10;display:flex;align-items:center;justify-content:space-between;gap:16px;padding:14px clamp(16px,4vw,42px);border-bottom:1px solid var(--line);background:rgba(7,9,13,.9);backdrop-filter:blur(14px)}
    a{color:inherit}.brand{display:inline-flex;align-items:center;gap:10px;text-decoration:none;font-weight:850}.brand-mark{width:22px;height:22px;border:2px solid var(--green);border-radius:50%;box-shadow:inset 0 0 0 5px rgba(49,211,159,.14),0 0 18px rgba(49,211,159,.25)}
    nav{display:flex;gap:8px;flex-wrap:wrap}nav a{color:var(--muted);font-size:.86rem;text-decoration:none;padding:8px 10px;border-radius:6px}nav a:hover{background:rgba(255,255,255,.07);color:var(--text)}
    main{width:min(1180px,calc(100% - 32px));margin:0 auto;padding:28px 0 56px}
    .notice{max-width:1080px;margin:0 auto 12px;padding:12px 16px;border:1px solid rgba(242,196,93,.35);border-radius:8px;background:rgba(242,196,93,.08);color:#f2c45d;line-height:1.5}
    footer{display:flex;justify-content:space-between;gap:12px;padding:22px clamp(16px,4vw,42px);border-top:1px solid var(--line);color:var(--muted)}
  </style>
</head>
<body>
  <header>
    <a class="brand" href="/early-access" aria-label="SYNTHOS Collective"><span class="brand-mark"></span><span>SYNTHOS Collective</span></a>
    <nav aria-label="Primary"><a href="/early-access">Early Access</a><a href="/health">Health</a><a href="/api/early-access/config">Config</a></nav>
  </header>
  <main>
    <p class="notice">Early access uses crypto payment verification and allocates SYN on the SYNTHOS native network after payment verification. Confirm the payment amount and receiving address before sending funds.</p>
    <div data-synthos-early-access></div>
  </main>
  <footer><span>SYNTHOS Collective</span><span>synthos-mainnet-1</span></footer>
  <script>
    window.SYNTHOS_API_URL = window.location.origin;
  </script>
  <script src="/assets/early-access-sale.js"></script>
</body>
</html>`))
}

func (s *server) handleWebsitePage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "explorer" {
		name = "explorer.html"
	}
	if name == "nodes" {
		name = "nodes.html"
	}
	if name == "bridge" {
		name = "bridge.html"
	}
	if name == "chain" {
		name = "chain.html"
	}
	if name == "dex" {
		name = "dex.html"
	}
	if name == "api" {
		name = "api.html"
	}
	if name == "" || strings.Contains(name, "/") || !strings.HasSuffix(name, ".html") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	s.serveWebsiteFile(w, r, name)
}

func (s *server) handleWebsiteAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" || strings.Contains(name, "..") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	s.serveWebsiteFile(w, r, name)
}

func (s *server) serveWebsiteFile(w http.ResponseWriter, r *http.Request, name string) {
	clean := filepath.ToSlash(filepath.Clean(name))
	if clean == "." || strings.HasPrefix(clean, "../") || filepath.IsAbs(clean) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	candidates := []string{
		filepath.Join("/website", filepath.FromSlash(clean)),
		filepath.Join("website", filepath.FromSlash(clean)),
	}
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			w.Header().Set("Cache-Control", "public, max-age=300")
			http.ServeFile(w, r, candidate)
			return
		}
	}
	http.Error(w, "website asset not found", http.StatusNotFound)
}

func (s *server) handleEarlyAccessWidget(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	candidates := []string{
		env("SYNTHOS_EARLY_ACCESS_WIDGET_PATH", ""),
		"/website/assets/early-access-sale.js",
		"website/assets/early-access-sale.js",
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
			w.Header().Set("Cache-Control", "public, max-age=300")
			http.ServeFile(w, r, candidate)
			return
		}
	}
	http.Error(w, "early access widget asset not found", http.StatusNotFound)
}

func (s *server) handleAPIEarlyAccessPaymentIntentByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/early-access/payment-intents/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "payment intent id required", http.StatusBadRequest)
		return
	}
	id := parts[0]
	if len(parts) == 1 && r.Method == http.MethodGet {
		intent, ok := s.getPaymentIntent(id)
		if !ok {
			http.Error(w, "payment intent not found", http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "intent": intent})
		return
	}
	if len(parts) == 2 && parts[1] == "verify" && r.Method == http.MethodPost {
		s.verifyPaymentIntent(w, r, id)
		return
	}
	http.Error(w, "not found", http.StatusNotFound)
}

func (s *server) verifyPaymentIntent(w http.ResponseWriter, r *http.Request, id string) {
	var body struct {
		TxHash string `json:"txHash"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	txHash := strings.TrimSpace(body.TxHash)
	if !isHexHash(txHash) {
		http.Error(w, "valid txHash is required", http.StatusBadRequest)
		return
	}
	intent, ok := s.getPaymentIntent(id)
	if !ok {
		http.Error(w, "payment intent not found", http.StatusNotFound)
		return
	}
	asset, ok := earlyAccessAssetBySymbol(intent.AssetSymbol)
	if !ok {
		http.Error(w, "payment asset config missing", http.StatusServiceUnavailable)
		return
	}
	if err := verifyEVMPayment(txHash, intent, asset); err != nil {
		intent.Status = "verification_failed"
		intent.TxHash = txHash
		intent.VerificationError = err.Error()
		intent.UpdatedAt = time.Now().UnixMilli()
		s.savePaymentIntent(intent)
		writeJSON(w, http.StatusAccepted, map[string]any{"ok": false, "intent": intent})
		return
	}

	intent.Status = "payment_verified"
	intent.TxHash = txHash
	intent.VerificationError = ""
	intent.UpdatedAt = time.Now().UnixMilli()
	if txID, err := allocateNativeSYN(intent); err == nil && txID != "" {
		intent.Status = "syn_allocated"
		intent.SynthosTxID = txID
	} else if err != nil {
		intent.Status = "allocation_pending"
		intent.VerificationError = err.Error()
	}
	intent.UpdatedAt = time.Now().UnixMilli()
	s.savePaymentIntent(intent)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "intent": intent})
}

func (s *server) getPaymentIntent(id string) (earlyAccessPaymentIntent, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	intent, ok := s.state.EarlyAccessPayments[id]
	return intent, ok
}

func (s *server) savePaymentIntent(intent earlyAccessPaymentIntent) {
	s.mu.Lock()
	if s.state.EarlyAccessPayments == nil {
		s.state.EarlyAccessPayments = map[string]earlyAccessPaymentIntent{}
	}
	s.state.EarlyAccessPayments[intent.ID] = intent
	s.mu.Unlock()
	if err := s.persist(); err != nil {
		log.Printf("persist warning: %v", err)
	}
}

// baseURL reconstructs this server's public origin from the incoming request,
// so downloads work whether reached via onrender.com or a custom domain.
func baseURL(r *http.Request) string {
	scheme := "https"
	if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") == "" {
		scheme = "http"
	}
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		scheme = p
	}
	return scheme + "://" + r.Host
}

// handleSilentNodeBinary serves the prebuilt Windows node binary.
func (s *server) handleSilentNodeBinary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	const path = "/downloads/silentnode.exe"
	if _, err := os.Stat(path); err != nil {
		http.Error(w, "node binary not available in this build", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="silentnode.exe"`)
	http.ServeFile(w, r, path)
}

// handleWindowsInstaller serves a real one-shot PowerShell installer: it
// downloads the prebuilt node binary and installs it as an Administrator-level
// Windows Service. No Go, no repo clone, no browser tab, no website login.
func (s *server) handleWindowsInstaller(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	base := baseURL(r)
	script := "# SYNTHOS Windows Service node installer\n" +
		"$ErrorActionPreference = \"Stop\"\n" +
		"$base = \"" + base + "\"\n" +
		"$principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())\n" +
		"$admin = $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)\n" +
		"if (-not $admin) {\n" +
		"  Write-Host \"Requesting Administrator permission to install the SYNTHOS node service...\"\n" +
		"  $cmd = \"irm '$base/api/node/windows-installer.ps1' | iex\"\n" +
		"  Start-Process powershell -Verb RunAs -ArgumentList @('-NoProfile','-ExecutionPolicy','Bypass','-Command',$cmd)\n" +
		"  return\n" +
		"}\n" +
		"$dir = Join-Path $env:ProgramData \"SynthosNode\"\n" +
		"New-Item -ItemType Directory -Force -Path $dir | Out-Null\n" +
		"$exe = Join-Path $dir \"silentnode.exe\"\n" +
		"$key = Join-Path $dir \"node-key.json\"\n" +
		"$status = Join-Path $dir \"node-status.json\"\n" +
		"Write-Host \"Downloading your SYNTHOS node...\"\n" +
		"Invoke-WebRequest -Uri \"$base/downloads/silentnode.exe\" -OutFile $exe\n" +
		"$serviceName = \"SynthosNode\"\n" +
		"$binPath = '\"' + $exe + '\" -key \"' + $key + '\" -status \"' + $status + '\" -relay \"' + $base + '\"'\n" +
		"$existing = Get-Service -Name $serviceName -ErrorAction SilentlyContinue\n" +
		"if ($existing) {\n" +
		"  Write-Host \"Updating existing SYNTHOS node service...\"\n" +
		"  Stop-Service -Name $serviceName -Force -ErrorAction SilentlyContinue\n" +
		"  sc.exe delete $serviceName | Out-Null\n" +
		"  Start-Sleep -Seconds 2\n" +
		"}\n" +
		"Write-Host \"Installing SYNTHOS as a Windows Service...\"\n" +
		"New-Service -Name $serviceName -BinaryPathName $binPath -DisplayName \"SYNTHOS Validator Node\" -Description \"Runs a SYNTHOS background validator node with real Ed25519 signed heartbeats.\" -StartupType Automatic | Out-Null\n" +
		"Start-Service -Name $serviceName\n" +
		"Write-Host \"\"\n" +
		"Write-Host \"Done! Your SYNTHOS validator node is running as a Windows Service.\"\n" +
		"Write-Host \"It keeps sending signed uptime proofs while the computer is on.\"\n" +
		"Write-Host \"Status file: $status\"\n" +
		"Write-Host \"\"\n" +
		"Write-Host \"To stop and remove it later, run:\"\n" +
		"Write-Host \"  Stop-Service SynthosNode; sc.exe delete SynthosNode\"\n"
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(script))
}

// handleWindowsInstallerBat serves a double-clickable .bat wrapper that runs
// the installation command above -- the simple path for non-technical
// users: download this one file, double-click, done.
func (s *server) handleWindowsInstallerBat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	base := baseURL(r)
	bat := "@echo off\r\n" +
		"title SYNTHOS Node Installer\r\n" +
		"echo Installing your SYNTHOS validator node as a Windows Service...\r\n" +
		"echo.\r\n" +
		"powershell -NoProfile -ExecutionPolicy Bypass -Command \"irm " + base + "/api/node/windows-installer.ps1 | iex\"\r\n" +
		"echo.\r\n" +
		"echo You can close this window.\r\n" +
		"pause >nul\r\n"
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="install-synthos-node.bat"`)
	_, _ = w.Write([]byte(bat))
}

func (s *server) writePeerList(w http.ResponseWriter, activeOnly bool) {
	now := time.Now()
	s.mu.RLock()
	peers := make([]peer, 0, len(s.state.Peers))
	for _, p := range s.state.Peers {
		p.Stale = now.Sub(time.UnixMilli(p.LastSeen)) > staleAfter
		if activeOnly && p.Stale {
			continue
		}
		peers = append(peers, p)
	}
	s.mu.RUnlock()

	sort.Slice(peers, func(i, j int) bool { return peers[i].Name < peers[j].Name })
	urls := make([]string, 0, len(peers))
	order := make([]string, 0, len(peers))
	for _, p := range peers {
		if p.URL != "" {
			urls = append(urls, p.URL)
		}
		order = append(order, p.Name)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"peers":           peers,
		"urls":            urls,
		"validator_order": order,
		"total":           len(peers),
		"active_only":     activeOnly,
	})
}

func (s *server) networkSnapshot(now time.Time) networkSnapshot {
	s.mu.RLock()
	peers := make([]peer, 0, len(s.state.Peers))
	for _, p := range s.state.Peers {
		p.Stale = p.LastSeen == 0 || now.Sub(time.UnixMilli(p.LastSeen)) > staleAfter
		peers = append(peers, p)
	}
	s.mu.RUnlock()
	sort.Slice(peers, func(i, j int) bool {
		if peers[i].Height == peers[j].Height {
			return peers[i].Name < peers[j].Name
		}
		return peers[i].Height > peers[j].Height
	})
	out := networkSnapshot{
		Peers:           peers,
		RegisteredTotal: len(peers),
		Checkpoints:     []map[string]any{},
	}
	for _, p := range peers {
		if p.Stale {
			continue
		}
		out.ActiveTotal++
		out.Reachable++
		out.Fresh++
		if peerHasCapability(p, "immune_node") || p.Kind == "immune" {
			out.Immune++
		}
		if p.Kind == "" || p.Kind == "validator" {
			out.Validators++
		}
		if len(p.Capabilities) > 0 {
			out.Agents++
		}
		if p.Height > out.HighestHeight {
			out.HighestHeight = p.Height
			out.Tip = p.Tip
			out.StateRoot = p.StateRoot
		}
		if p.Height > 0 && p.Tip != "" {
			out.Checkpoints = append(out.Checkpoints, map[string]any{
				"node_id":                  p.Name,
				"role":                     defaultString(p.Role, normalizeRole(p.Kind)),
				"kind":                     p.Kind,
				"height":                   p.Height,
				"tip":                      p.Tip,
				"state_root":               p.StateRoot,
				"last_seen":                millisRFC3339(p.LastSeen),
				"valid_heartbeats":         p.ValidHeartbeats,
				"hosted_proof_session":     p.HostedProofSession,
				"real_signed_heartbeat":    !p.HostedProofSession && p.LastNonce != "",
				"reward_eligible_possible": !p.HostedProofSession && p.LastNonce != "",
			})
		}
	}
	if len(out.Checkpoints) > 25 {
		out.Checkpoints = out.Checkpoints[:25]
	}
	sort.Slice(out.Peers, func(i, j int) bool { return out.Peers[i].Name < out.Peers[j].Name })
	return out
}

func (s *server) proxyRPCJSON(w http.ResponseWriter, r *http.Request, rpcPath string) bool {
	rpcURL := strings.TrimRight(os.Getenv("SYNTHOS_RPC_URL"), "/")
	if rpcURL == "" {
		return false
	}
	target := rpcURL + rpcPath
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(target)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"ok":     false,
			"error":  err.Error(),
			"source": "synthos_rpc",
		})
		return true
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.Header.Get("Content-Type") != "" {
		w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	} else {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)
	return true
}

func (s *server) authorized(r *http.Request) bool {
	if s.secret == "" {
		return true
	}
	return r.Header.Get("X-Registry-Secret") == s.secret
}

func (s *server) load() error {
	f, err := os.Open(s.stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	var st registryState
	if err := json.NewDecoder(f).Decode(&st); err != nil {
		return err
	}
	if st.Peers == nil {
		st.Peers = map[string]peer{}
	}
	if st.Mailbox == nil {
		st.Mailbox = map[string][]mailboxMessage{}
	}
	if st.Contacts == nil {
		st.Contacts = []contactMessage{}
	}
	if st.EarlyAccessPayments == nil {
		st.EarlyAccessPayments = map[string]earlyAccessPaymentIntent{}
	}
	s.state = st
	return nil
}

func (s *server) persist() error {
	if s.stateFile == "" {
		return nil
	}
	if err := os.MkdirAll(dir(s.stateFile), 0o755); err != nil {
		return err
	}
	tmp := s.stateFile + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	err = enc.Encode(s.state)
	closeErr := f.Close()
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	return os.Rename(tmp, s.stateFile)
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", allowedOrigin(r.Header.Get("Origin")))
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Registry-Secret, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func allowedOrigin(origin string) string {
	origins := splitCSV(os.Getenv("SYNTHOS_CORS_ORIGINS"))
	if len(origins) == 0 {
		return "*"
	}
	for _, allowed := range origins {
		if allowed == "*" || strings.EqualFold(strings.TrimRight(allowed, "/"), strings.TrimRight(origin, "/")) {
			return origin
		}
	}
	return origins[0]
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(value)
}

func sanitize(value string) string {
	return safeName.ReplaceAllString(truncate(value, 64), "")
}

func truncate(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) <= max {
		return value
	}
	return value[:max]
}

func defaultString(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func normalizeRole(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "validator", "public_validator", "public-validator", "validator_node":
		return "validator_candidate"
	case "validator_candidate", "validator-candidate":
		return "validator_candidate"
	case "immune", "immune_node", "immune-node":
		return "immune"
	case "public-non-validator", "observer", "observer_node", "non_validator", "non-validator":
		return "observer"
	default:
		return "observer"
	}
}

func roleKind(role string) string {
	switch role {
	case "validator_candidate", "validator":
		return "validator"
	case "immune":
		return "immune"
	default:
		return "observer"
	}
}

func proofStatus(p peer, now time.Time) string {
	if p.LastHeartbeatAt == 0 {
		return "registered"
	}
	if now.Sub(time.UnixMilli(p.LastHeartbeatAt)) > heartbeatFreshAfter {
		return "stale"
	}
	if time.Duration(p.VerifiedUptimeMS)*time.Millisecond >= rewardEpoch {
		return "eligible"
	}
	return "proving"
}

func activateHostedProofSession(p *peer) {
	if p == nil {
		return
	}
	now := time.Now().UnixMilli()
	p.Status = "proving"
	p.ProofStatus = "proving"
	p.FirstHeartbeatAt = now
	p.LastHeartbeatAt = now
	p.LastSeen = now
	p.ValidHeartbeats = 1
	p.VerifiedUptimeMS = 1_000
	p.HostedProofSession = true
	p.Height = 1
	p.Tip = "hosted-proof-" + shortID()
	p.StateRoot = "hosted-state-" + shortID()
	p.Mode = defaultString(p.Mode, "hosted_proof_session")
	if len(p.Capabilities) == 0 {
		p.Capabilities = coreNodeCapabilities()
	}
}

func nodeStatus(p peer, now time.Time) map[string]any {
	p.ProofStatus = proofStatus(p, now)
	uptime := time.Duration(p.VerifiedUptimeMS) * time.Millisecond
	firstEligible := ""
	if p.FirstHeartbeatAt > 0 {
		firstEligible = time.UnixMilli(p.FirstHeartbeatAt).Add(rewardEpoch).UTC().Format(time.RFC3339)
	}
	eligible := p.ProofStatus == "eligible"
	role := defaultString(p.Role, normalizeRole(p.Kind))
	isValidator := role == "validator_candidate" || role == "validator"
	rewardEligible := eligible && isValidator && !p.HostedProofSession
	uptimePercent := 0.0
	if rewardEpoch > 0 {
		uptimePercent = (float64(uptime) / float64(rewardEpoch)) * 100
		if uptimePercent > 100 {
			uptimePercent = 100
		}
	}
	rewardStatus := "proving_uptime"
	if p.HostedProofSession {
		rewardStatus = "hosted_bootstrap_not_reward_eligible"
	} else if rewardEligible {
		rewardStatus = "eligible_next_payout"
	}
	reward := map[string]any{
		"eligible":                          rewardEligible,
		"status":                            rewardStatus,
		"paid_in":                           "SYN",
		"monthly_base_syn":                  0,
		"monthly_bonus_cap_syn":             0,
		"monthly_max_syn":                   0,
		"paid_monthly_in_arrears":           true,
		"requires_full_month_uptime":        true,
		"requires_real_signed_heartbeats":   true,
		"hosted_bootstrap_sessions_qualify": false,
		"first_reward_eligibility":          firstEligible,
		"uptime_percent_this_month":         uptimePercent,
	}
	rewardEpochTracking := rewardEpochStatus(now, p, uptime, uptimePercent, rewardStatus, rewardEligible)
	reward["epoch"] = rewardEpochTracking
	if isValidator {
		reward["monthly_base_syn"] = validatorMonthlyBaseRewardSYN
		reward["monthly_bonus_cap_syn"] = validatorMonthlyBonusCapSYN
		reward["monthly_max_syn"] = validatorMonthlyBaseRewardSYN + validatorMonthlyBonusCapSYN
	}
	capabilityMap := capabilityStatusMap(p.Capabilities)
	healthy := p.LastHeartbeatAt > 0 && now.Sub(time.UnixMilli(p.LastHeartbeatAt)) <= staleAfter
	synced := p.Height > 0
	return map[string]any{
		"node_id":                   p.Name,
		"publicId":                  p.Name,
		"public_key":                p.PublicKey,
		"publicKey":                 p.PublicKey,
		"role":                      role,
		"kind":                      p.Kind,
		"network":                   p.Network,
		"endpoint":                  p.URL,
		"status":                    p.Status,
		"proof_status":              p.ProofStatus,
		"hosted_proof_session":      p.HostedProofSession,
		"real_signed_heartbeat":     !p.HostedProofSession && p.LastNonce != "",
		"registered_at":             millisRFC3339(p.RegisteredAt),
		"first_heartbeat_at":        millisRFC3339(p.FirstHeartbeatAt),
		"last_heartbeat_at":         millisRFC3339(p.LastHeartbeatAt),
		"valid_heartbeats":          p.ValidHeartbeats,
		"verified_uptime_s":         int64(uptime.Seconds()),
		"verified_days":             uptime.Hours() / 24,
		"uptime_percent":            uptimePercent,
		"uptime_percent_this_month": uptimePercent,
		"uptime_required_s":         int64(rewardEpoch.Seconds()),
		"reward_epoch":              rewardEpochTracking,
		"height":                    p.Height,
		"tip":                       p.Tip,
		"state_root":                p.StateRoot,
		"stateRoot":                 p.StateRoot,
		"health":                    healthy,
		"synced":                    synced,
		"capabilities":              p.Capabilities,
		"capability_status":         capabilityMap,
		"capabilityStatus":          capabilityMap,
		"accepted":                  rewardEligible,
		"reward":                    reward,
	}
}

func rewardEpochStatus(now time.Time, p peer, uptime time.Duration, uptimePercent float64, rewardStatus string, rewardEligible bool) map[string]any {
	currentEpoch := now.UTC().Format("2006-01")
	firstEligible := ""
	if p.FirstHeartbeatAt > 0 {
		firstEligible = time.UnixMilli(p.FirstHeartbeatAt).Add(rewardEpoch).UTC().Format("2006-01-02")
	}
	lastCompletedEpoch := "none completed"
	previousMonthUptime := "no completed epoch"
	if uptime >= rewardEpoch {
		lastCompletedEpoch = now.UTC().AddDate(0, -1, 0).Format("2006-01")
		previousMonthUptime = "100.0%"
	}
	return map[string]any{
		"current_epoch":                 currentEpoch,
		"days_verified_this_month":      uptime.Hours() / 24,
		"uptime_percentage":             uptimePercent,
		"first_reward_eligibility_date": firstEligible,
		"last_completed_epoch":          lastCompletedEpoch,
		"previous_month_uptime":         previousMonthUptime,
		"reward_status":                 rewardStatus,
		"reward_eligible":               rewardEligible,
		"applies_to":                    []string{"validator_candidate", "validator"},
		"note":                          "Reward eligibility applies to Validator Candidate and Validator roles only.",
	}
}

func coreNodeCapabilities() []string {
	return []string{
		"ed25519",
		"canonical_serialization",
		"validator_registry",
		"proposal_rotation",
		"quorum",
		"replay_protection",
		"persistent_storage",
	}
}

func capabilityStatusMap(capabilities []string) map[string]bool {
	out := map[string]bool{}
	for _, value := range capabilities {
		out[value] = true
		switch value {
		case "canonical_serialization":
			out["serialization"] = true
		case "validator_registry":
			out["registry"] = true
		case "proposal_rotation":
			out["rotation"] = true
		case "replay_protection":
			out["replay"] = true
		case "persistent_storage":
			out["storage"] = true
		}
	}
	return out
}

func millisRFC3339(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return time.UnixMilli(ms).UTC().Format(time.RFC3339)
}

func validatorRewardPolicy() map[string]any {
	return map[string]any{
		"token":                          "SYN",
		"validator_monthly_base_syn":     validatorMonthlyBaseRewardSYN,
		"validator_monthly_bonus_cap":    validatorMonthlyBonusCapSYN,
		"validator_monthly_max_syn":      validatorMonthlyBaseRewardSYN + validatorMonthlyBonusCapSYN,
		"payment_timing":                 "monthly_in_arrears",
		"first_payment_after":            "one full month of verified validator uptime",
		"no_payment_for_button_click":    true,
		"requires_signed_heartbeats":     true,
		"requires_verified_operation":    true,
		"requires_public_endpoint":       false,
		"no_guaranteed_income":           true,
		"source_bucket":                  "Validator / Security Rewards",
		"source_bucket_amount_syn":       12_000_000_000,
		"uptime_finality_bucket_syn":     5_000_000_000,
		"target_validator_operators":     5_000,
		"ten_year_max_per_validator_syn": 910_000,
	}
}

func canonicalHeartbeatMessage(nodeID string, height int64, tip string, stateRoot string, timestamp string, nonce string) []byte {
	return []byte(fmt.Sprintf(
		"SYNTHOS_HEARTBEAT_V1\nnode_id=%s\nheight=%d\ntip=%s\nstate_root=%s\ntimestamp=%s\nnonce=%s",
		nodeID,
		height,
		tip,
		stateRoot,
		strings.TrimSpace(timestamp),
		strings.TrimSpace(nonce),
	))
}

func verifyHeartbeatSignature(publicKeyHex string, signatureHex string, message []byte) error {
	publicKeyHex = strings.TrimPrefix(strings.TrimSpace(publicKeyHex), "0x")
	signatureHex = strings.TrimPrefix(strings.TrimSpace(signatureHex), "0x")
	pub, err := hex.DecodeString(publicKeyHex)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("registered public key is not a valid Ed25519 public key")
	}
	sig, err := hex.DecodeString(signatureHex)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("signature must be a 64-byte hex Ed25519 signature")
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), message, sig) {
		return fmt.Errorf("signature verification failed")
	}
	return nil
}

func earlyAccessAssetsFromEnv() []earlyAccessAsset {
	if raw := os.Getenv("SYNTHOS_EARLY_ACCESS_ASSETS_JSON"); raw != "" {
		var assets []earlyAccessAsset
		if err := json.Unmarshal([]byte(raw), &assets); err == nil && len(assets) > 0 {
			for i := range assets {
				if assets[i].Enabled == false && !strings.Contains(raw, `"enabled"`) {
					assets[i].Enabled = true
				}
				if assets[i].TreasuryAddress == "" {
					assets[i].TreasuryAddress = env("SYNTHOS_EARLY_ACCESS_PAYMENT_TREASURY", env("SYNTHOS_EARLY_ACCESS_TREASURY_WALLET", "0x5d6f8FbAAB199E788ed9Cfcb3F7Fe2ac9c0450d2"))
				}
			}
			return assets
		}
		log.Printf("SYNTHOS_EARLY_ACCESS_ASSETS_JSON is invalid; using default asset slots")
	}
	return []earlyAccessAsset{
		{Symbol: "USDC", Decimals: 6, USDPrice: "1.00", Enabled: false},
		{Symbol: "USDT", Decimals: 6, USDPrice: "1.00", Enabled: false},
		{Symbol: "WETH", Decimals: 18, Enabled: false},
		{Symbol: "WBTC", Decimals: 8, Enabled: false},
		{Symbol: "ETH", Native: true, Decimals: 18, Enabled: false},
	}
}

func earlyAccessAssetBySymbol(symbol string) (earlyAccessAsset, bool) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	for _, asset := range earlyAccessAssetsFromEnv() {
		if strings.ToUpper(asset.Symbol) == symbol {
			return asset, true
		}
	}
	return earlyAccessAsset{}, false
}

func parseUSDCents(value string) (uint64, error) {
	value = strings.TrimSpace(strings.TrimPrefix(value, "$"))
	if value == "" {
		return 0, fmt.Errorf("usdValue required")
	}
	parts := strings.Split(value, ".")
	if len(parts) > 2 {
		return 0, fmt.Errorf("invalid usdValue")
	}
	dollars, ok := new(big.Int).SetString(defaultString(parts[0], "0"), 10)
	if !ok || dollars.Sign() < 0 {
		return 0, fmt.Errorf("invalid usdValue")
	}
	cents := new(big.Int).Mul(dollars, big.NewInt(100))
	if len(parts) == 2 {
		frac := parts[1]
		if len(frac) > 2 {
			frac = frac[:2]
		}
		for len(frac) < 2 {
			frac += "0"
		}
		f, ok := new(big.Int).SetString(frac, 10)
		if !ok {
			return 0, fmt.Errorf("invalid usdValue")
		}
		cents.Add(cents, f)
	}
	if !cents.IsUint64() || cents.Uint64() == 0 {
		return 0, fmt.Errorf("usdValue must be greater than zero")
	}
	return cents.Uint64(), nil
}

func quotePaymentAmount(usdCents uint64, asset earlyAccessAsset) (string, error) {
	priceCents, err := parseUSDCents(defaultString(asset.USDPrice, "1.00"))
	if err != nil || priceCents == 0 {
		return "", fmt.Errorf("asset USD price missing")
	}
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(asset.Decimals)), nil)
	amount := new(big.Int).Mul(new(big.Int).SetUint64(usdCents), scale)
	amount.Div(amount, new(big.Int).SetUint64(priceCents))
	return amount.String(), nil
}

func isHexHash(value string) bool {
	value = strings.TrimPrefix(value, "0x")
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func verifyEVMPayment(txHash string, intent earlyAccessPaymentIntent, asset earlyAccessAsset) error {
	if asset.RPCURL == "" {
		return fmt.Errorf("asset RPC URL is not configured")
	}
	receipt, err := evmRPC(asset.RPCURL, "eth_getTransactionReceipt", []any{txHash})
	if err != nil {
		return err
	}
	if receipt == nil {
		return fmt.Errorf("transaction receipt not found yet")
	}
	receiptMap, ok := receipt.(map[string]any)
	if !ok {
		return fmt.Errorf("invalid transaction receipt")
	}
	if !hexBool(receiptMap["status"]) {
		return fmt.Errorf("transaction failed on-chain")
	}
	expectedAmount, ok := new(big.Int).SetString(intent.PaymentAmount, 10)
	if !ok {
		return fmt.Errorf("invalid expected payment amount")
	}
	if asset.Native {
		tx, err := evmRPC(asset.RPCURL, "eth_getTransactionByHash", []any{txHash})
		if err != nil {
			return err
		}
		txMap, ok := tx.(map[string]any)
		if !ok {
			return fmt.Errorf("transaction not found")
		}
		if !sameAddress(stringValue(txMap["to"]), intent.PaymentAddress) {
			return fmt.Errorf("native payment recipient mismatch")
		}
		value, err := parseHexBig(stringValue(txMap["value"]))
		if err != nil {
			return err
		}
		if value.Cmp(expectedAmount) < 0 {
			return fmt.Errorf("native payment amount too small")
		}
		return nil
	}
	if asset.Address == "" {
		return fmt.Errorf("token payment asset address missing")
	}
	logs, ok := receiptMap["logs"].([]any)
	if !ok {
		return fmt.Errorf("receipt logs missing")
	}
	for _, rawLog := range logs {
		logMap, ok := rawLog.(map[string]any)
		if !ok || !sameAddress(stringValue(logMap["address"]), asset.Address) {
			continue
		}
		topics, ok := logMap["topics"].([]any)
		if !ok || len(topics) < 3 {
			continue
		}
		if !strings.EqualFold(stringValue(topics[0]), "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef") {
			continue
		}
		if !topicAddressMatches(stringValue(topics[2]), intent.PaymentAddress) {
			continue
		}
		amount, err := parseHexBig(stringValue(logMap["data"]))
		if err != nil {
			continue
		}
		if amount.Cmp(expectedAmount) >= 0 {
			return nil
		}
	}
	return fmt.Errorf("matching token transfer to treasury not found")
}

func evmRPC(url string, method string, params []any) (any, error) {
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	})
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("evm rpc status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var decoded struct {
		Result any `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, err
	}
	if decoded.Error != nil {
		return nil, fmt.Errorf("%s", decoded.Error.Message)
	}
	return decoded.Result, nil
}

func allocateNativeSYN(intent earlyAccessPaymentIntent) (string, error) {
	privHex := distributionAgentPrivateKey()
	rpcURL := strings.TrimRight(env("SYNTHOS_NATIVE_RPC_URL", "https://rpc.ishamwilliamsblockchains.com"), "/")
	if privHex == "" {
		return "", fmt.Errorf("SYN distribution agent key is not configured")
	}
	w, err := wallet.FromPrivateKeyHex(privHex)
	if err != nil {
		return "", err
	}
	txChainID, err := synthosTxChainID()
	if err != nil {
		return "", err
	}
	from, _ := w.Address()
	pub, _ := w.PublicKeyHex()
	nonce, err := synthosNonce(rpcURL, string(from))
	if err != nil {
		return "", err
	}
	tx := chain.Tx{
		ChainID:   txChainID,
		From:      from,
		To:        chain.Address(intent.SynthosAddress),
		Amount:    intent.SynAmount,
		Fee:       chain.MIN_FEE,
		Nonce:     nonce,
		PublicKey: pub,
		Metadata: []chain.KeyValuePair{
			{Key: "distribution_agent", Value: env("SYNTHOS_DISTRIBUTION_AGENT_ID", "synthos-early-adopter-distributor")},
			{Key: "payment_intent", Value: intent.ID},
			{Key: "payment_tx", Value: intent.TxHash},
			{Key: "asset", Value: intent.AssetSymbol},
		},
	}
	if err := tx.Sign(w.Private); err != nil {
		return "", err
	}
	data, _ := json.Marshal(tx)
	resp, err := http.Post(rpcURL+"/submitTx", "application/json", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("SYN allocation submit failed: %s", strings.TrimSpace(string(body)))
	}
	var result struct {
		OK   bool   `json:"ok"`
		TxID string `json:"tx_id"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}
	if !result.OK {
		return "", fmt.Errorf("SYN allocation was not accepted")
	}
	_, _ = http.Post(rpcURL+"/proposeBlock", "application/json", bytes.NewReader([]byte("{}")))
	return result.TxID, nil
}

func synthosNonce(rpcURL string, address string) (uint64, error) {
	resp, err := http.Get(rpcURL + "/account?address=" + address)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return 0, fmt.Errorf("SYN account lookup failed: %s", strings.TrimSpace(string(body)))
	}
	var account struct {
		Nonce uint64 `json:"nonce"`
	}
	if err := json.Unmarshal(body, &account); err != nil {
		return 0, err
	}
	return account.Nonce, nil
}

func distributionAgentPrivateKey() string {
	if value := os.Getenv("SYNTHOS_DISTRIBUTION_AGENT_PRIVATE_KEY"); value != "" {
		return value
	}
	return os.Getenv("SYNTHOS_EARLY_ACCESS_ALLOCATION_PRIVATE_KEY")
}

func earlyAccessDistributorConfig() map[string]any {
	info := map[string]any{
		"agentId": env("SYNTHOS_DISTRIBUTION_AGENT_ID", "synthos-early-adopter-distributor"),
		"enabled": distributionAgentPrivateKey() != "",
	}
	if txChainID, err := synthosTxChainID(); err == nil {
		info["txChainId"] = txChainID
	}
	if privHex := distributionAgentPrivateKey(); privHex != "" {
		if w, err := wallet.FromPrivateKeyHex(privHex); err == nil {
			if address, err := w.Address(); err == nil {
				info["address"] = address
			}
			if fingerprint, err := w.Fingerprint(); err == nil {
				info["fingerprint"] = fingerprint
			}
		}
	}
	return info
}

func synthosTxChainID() (uint64, error) {
	for _, raw := range []string{
		os.Getenv("SYNTHOS_EARLY_ACCESS_TX_CHAIN_ID"),
		os.Getenv("SYNTHOS_EARLY_ACCESS_CHAIN_ID"),
	} {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || id == 0 {
			return 0, fmt.Errorf("invalid SYNTHOS transaction chain ID %q", raw)
		}
		return id, nil
	}
	return 20260702, nil
}

func parseHexBig(value string) (*big.Int, error) {
	value = strings.TrimPrefix(value, "0x")
	if value == "" {
		return big.NewInt(0), nil
	}
	n, ok := new(big.Int).SetString(value, 16)
	if !ok {
		return nil, fmt.Errorf("invalid hex value")
	}
	return n, nil
}

func hexBool(value any) bool {
	raw := stringValue(value)
	return raw == "0x1" || raw == "1"
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func sameAddress(left string, right string) bool {
	return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
}

func topicAddressMatches(topic string, address string) bool {
	topic = strings.TrimPrefix(strings.ToLower(topic), "0x")
	address = strings.TrimPrefix(strings.ToLower(address), "0x")
	return len(topic) == 64 && len(address) == 40 && strings.HasSuffix(topic, address)
}

func normalizeChoice(value, fallback string, allowed ...string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, option := range allowed {
		if value == option {
			return value
		}
	}
	return fallback
}

func normalizeCapabilities(values []string) []string {
	allowed := map[string]bool{
		"immune_node":              true,
		"economist":                true,
		"governor":                 true,
		"communicator":             true,
		"simulator":                true,
		"enforcer":                 true,
		"citizen":                  true,
		"ed25519":                  true,
		"canonical_serialization":  true,
		"canonical_serialisation":  true,
		"serialization":            true,
		"validator_registry":       true,
		"registry":                 true,
		"proposal_rotation":        true,
		"rotation":                 true,
		"quorum":                   true,
		"replay_protection":        true,
		"replay":                   true,
		"persistent_storage":       true,
		"storage":                  true,
		"persistent_block_storage": true,
		"persistent_vote_storage":  true,
		"persistent_state_storage": true,
	}
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		value = canonicalCapability(value)
		if !allowed[value] || seen[value] {
			continue
		}
		out = append(out, value)
		seen[value] = true
	}
	return out
}

func canonicalCapability(value string) string {
	switch value {
	case "canonical_serialisation", "serialization":
		return "canonical_serialization"
	case "registry":
		return "validator_registry"
	case "rotation":
		return "proposal_rotation"
	case "replay":
		return "replay_protection"
	case "storage", "persistent_block_storage", "persistent_vote_storage", "persistent_state_storage":
		return "persistent_storage"
	default:
		return value
	}
}

func peerHasCapability(p peer, capability string) bool {
	for _, value := range p.Capabilities {
		if value == capability {
			return true
		}
	}
	return false
}

func shortID() string {
	var b [5]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func endpointURL(value string) string {
	if value == "outbound-only" {
		return ""
	}
	return value
}

func nextProposer(peers []peer) string {
	for _, p := range peers {
		if p.Kind == "" || p.Kind == "validator" {
			return p.Name
		}
	}
	return ""
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func defaultListen(fallback string) string {
	if value := os.Getenv("SYNTHOS_REGISTRY_LISTEN"); value != "" {
		return value
	}
	if port := os.Getenv("PORT"); port != "" {
		return ":" + port
	}
	return fallback
}

func dir(path string) string {
	i := strings.LastIndexAny(path, `/\`)
	if i < 0 {
		return "."
	}
	return path[:i]
}
