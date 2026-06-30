package main

import (
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

type registryState struct {
	Peers   map[string]peer             `json:"peers"`
	Mailbox map[string][]mailboxMessage `json:"mailbox"`
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
			Peers:   map[string]peer{},
			Mailbox: map[string][]mailboxMessage{},
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
