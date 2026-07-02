package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const staleAfter = 5 * time.Minute

var safeName = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

type peer struct {
	Name               string `json:"name"`
	URL                string `json:"url"`
	Kind               string `json:"kind,omitempty"`
	Network            string `json:"network,omitempty"`
	Status             string `json:"status,omitempty"`
	PublicKey          string `json:"public_key,omitempty"`
	Cloud              string `json:"cloud"`
	Mode               string `json:"mode"`
	InboundPorts       int    `json:"inbound_ports"`
	HardwareCommitment string `json:"hardware_commitment,omitempty"`
	RegisteredAt       int64  `json:"registered_at"`
	LastSeen           int64  `json:"last_seen"`
	Stale              bool   `json:"stale,omitempty"`
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

type registryState struct {
	Peers    map[string]peer             `json:"peers"`
	Mailbox  map[string][]mailboxMessage `json:"mailbox"`
	Contacts []contactMessage            `json:"contacts,omitempty"`
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
	flag.StringVar(&listen, "listen", env("SYNTHOS_REGISTRY_LISTEN", ":8090"), "HTTP listen address")
	flag.StringVar(&stateFile, "state", env("SYNTHOS_REGISTRY_STATE", ".synthos/cloudless-registry.json"), "registry state JSON path")
	flag.StringVar(&secret, "secret", os.Getenv("REGISTRY_SECRET"), "optional registry admin/mailbox secret")
	flag.Parse()

	s := &server{
		state: registryState{
			Peers:    map[string]peer{},
			Mailbox:  map[string][]mailboxMessage{},
			Contacts: []contactMessage{},
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
	mux.HandleFunc("/api/node/windows-installer.ps1", s.handleWindowsInstaller)

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
			"GET /api/node/windows-installer.ps1",
			"DELETE /peers/NODE",
		},
	})
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
		Name               string `json:"name"`
		URL                string `json:"url"`
		Cloud              string `json:"cloud"`
		Kind               string `json:"kind"`
		Network            string `json:"network"`
		Status             string `json:"status"`
		PublicKey          string `json:"public_key"`
		Mode               string `json:"mode"`
		InboundPorts       int    `json:"inbound_ports"`
		HardwareCommitment string `json:"hardware_commitment"`
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
	for i := range peers {
		if !peers[i].Stale {
			reachable++
			fresh++
		}
		switch peers[i].Kind {
		case "immune":
			immune++
		default:
			validators++
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                   len(peers) == 0 || reachable > 0,
		"service":              "synthos-website-backend",
		"network":              "synthos",
		"total":                len(peers),
		"reachable":            reachable,
		"fresh_heartbeats":     fresh,
		"validators_running":   validators,
		"immune_nodes_running": immune,
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

func (s *server) handleWindowsInstaller(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="install-synthos-node.ps1"`)
	_, _ = w.Write([]byte(`# SYNTHOS background node installer
param(
  [string]$RegistryUrl = "http://127.0.0.1:8090"
)

Write-Host "SYNTHOS background node installer"
Write-Host "Registry: $RegistryUrl"
Write-Host "Clone the SYNTHOS repository, then run:"
Write-Host "powershell -ExecutionPolicy Bypass -File scripts\install_background_node.ps1 -RegistryUrl $RegistryUrl"
`))
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
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Registry-Secret")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
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

func normalizeChoice(value, fallback string, allowed ...string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, option := range allowed {
		if value == option {
			return value
		}
	}
	return fallback
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

func dir(path string) string {
	i := strings.LastIndexAny(path, `/\`)
	if i < 0 {
		return "."
	}
	return path[:i]
}
