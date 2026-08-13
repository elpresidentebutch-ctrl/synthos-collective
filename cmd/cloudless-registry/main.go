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
	mux.HandleFunc("/api/nodes", s.handleAPINodes)
	mux.HandleFunc("/api/nodes/provision", s.handleAPIProvisionNode)
	mux.HandleFunc("/api/contact", s.handleAPIContact)
	mux.HandleFunc("/api/early-access/config", s.handleAPIEarlyAccessConfig)
	mux.HandleFunc("/api/early-access/payment-intents", s.handleAPIEarlyAccessPaymentIntents)
	mux.HandleFunc("/api/early-access/payment-intents/", s.handleAPIEarlyAccessPaymentIntentByID)
	mux.HandleFunc("/index.html", s.handleWebsitePage)
	mux.HandleFunc("/chain.html", s.handleWebsitePage)
	mux.HandleFunc("/explorer", s.handleWebsitePage)
	mux.HandleFunc("/explorer.html", s.handleWebsitePage)
	mux.HandleFunc("/dex.html", s.handleWebsitePage)
	mux.HandleFunc("/api.html", s.handleWebsitePage)
	mux.HandleFunc("/early-access", s.handleEarlyAccessPage)
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
			"GET /api/nodes",
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
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                   len(peers) == 0 || reachable > 0,
		"service":              "synthos-website-backend",
		"network":              "synthos",
		"total":                activeTotal,
		"registered_total":     len(peers),
		"reachable":            reachable,
		"fresh_heartbeats":     fresh,
		"validators_running":   validators,
		"immune_nodes_running": immune,
		"agents_running":       agents,
		"highest_height":       0,
		"tip":                  "",
		"state_root":           "",
		"next_proposer":        nextProposer(peers),
		"majority_reachable":   len(peers) == 0 || reachable*3 >= len(peers)*2,
		"converged_tip":        true,
		"converged_state_root": true,
		"validators":           peers,
		"updated_at":           time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *server) handleAPINodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.writePeerList(w, false)
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
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		http.Error(w, "key generation failed", http.StatusInternalServerError)
		return
	}
	publicHex := hex.EncodeToString(publicKey)
	privateHex := hex.EncodeToString(privateKey.Seed())
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
		LastSeen:     time.Now().UnixMilli(),
	}

	s.mu.Lock()
	s.state.Peers[nodeID] = entry
	s.mu.Unlock()
	if err := s.persist(); err != nil {
		log.Printf("persist warning: %v", err)
	}

	configJSON := fmt.Sprintf(`{
  "node_id": "%s",
  "network": "%s",
  "kind": "%s",
  "registry_url": "http://127.0.0.1:8090",
  "private_key": "%s"
}`, nodeID, network, kind, privateHex)

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"nodeId":        nodeID,
		"kind":          kind,
		"network":       network,
		"endpoint":      endpoint,
		"publicKey":     publicHex,
		"privateKey":    privateHex,
		"nodeConfig":    configJSON,
		"startCommand":  "go run ./cmd/synthosd",
		"workerName":    nodeID,
		"deployCommand": "go run ./cmd/synthosd",
		"warning":       "Private key is returned once and is not stored by this registry. Save it locally before closing the page.",
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
// downloads the prebuilt node binary and registers it to run in the
// background at every login. No Go, no repo clone, no terminal skills.
func (s *server) handleWindowsInstaller(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	base := baseURL(r)
	script := "# SYNTHOS background node installer\n" +
		"$ErrorActionPreference = \"Stop\"\n" +
		"$base = \"" + base + "\"\n" +
		"$dir = Join-Path $env:LOCALAPPDATA \"SynthosNode\"\n" +
		"New-Item -ItemType Directory -Force -Path $dir | Out-Null\n" +
		"$exe = Join-Path $dir \"silentnode.exe\"\n" +
		"Write-Host \"Downloading your SYNTHOS node...\"\n" +
		"Invoke-WebRequest -Uri \"$base/downloads/silentnode.exe\" -OutFile $exe\n" +
		"Write-Host \"Setting it to run quietly in the background at login...\"\n" +
		"$action = New-ScheduledTaskAction -Execute $exe\n" +
		"$trigger = New-ScheduledTaskTrigger -AtLogOn\n" +
		"$settings = New-ScheduledTaskSettingsSet -StartWhenAvailable -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries\n" +
		"Register-ScheduledTask -TaskName \"SynthosNode\" -Action $action -Trigger $trigger -Settings $settings -Force | Out-Null\n" +
		"Start-ScheduledTask -TaskName \"SynthosNode\"\n" +
		"Write-Host \"\"\n" +
		"Write-Host \"Done! Your SYNTHOS node is now running in the background\"\n" +
		"Write-Host \"and will start automatically every time you log in.\"\n" +
		"Write-Host \"\"\n" +
		"Write-Host \"To stop and remove it later, run:\"\n" +
		"Write-Host \"  Stop-ScheduledTask -TaskName SynthosNode; Unregister-ScheduledTask -TaskName SynthosNode -Confirm:`$false\"\n"
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
		"echo Installing your SYNTHOS background node...\r\n" +
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
		"immune_node":  true,
		"economist":    true,
		"governor":     true,
		"communicator": true,
		"simulator":    true,
		"enforcer":     true,
		"citizen":      true,
	}
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if !allowed[value] || seen[value] {
			continue
		}
		out = append(out, value)
		seen[value] = true
	}
	return out
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
