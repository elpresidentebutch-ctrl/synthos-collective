package network

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// RelayTransport is a fully outbound-only Transport that connects to the
// Synthos peer registry over HTTP. It discovers peers dynamically, registers
// itself for discovery by others, and sends messages by POSTing JSON to
// peer HTTP endpoints — the same protocol the Cloudflare Workers validators
// and mobile PWA validators already speak.
//
// No inbound listener is required. The Go RPC server exposes /gossip/block
// and /gossip/tx-batch endpoints so peers can push messages to us.
type RelayTransport struct {
	// RegistryURL is the peer registry (e.g. https://synthos-peer-registry.example.workers.dev).
	RegistryURL string
	// SelfName is this node's validator name (e.g. "synthos-go-node-1").
	SelfName string
	// SelfURL is this node's publicly reachable URL (e.g. "https://my-node.fly.dev").
	SelfURL string
	// RegistrySecret is the optional X-Registry-Secret header value.
	RegistrySecret string
	// Cloud identifier for registry metadata.
	Cloud string

	// HeartbeatInterval controls how often we re-register and refresh peers.
	HeartbeatInterval time.Duration

	mu            sync.RWMutex
	agentHandler  func(fromAgentID string, payload []byte)
	topicHandlers map[string]func(fromAgentID string, payload []byte)
	started       bool
	cancel        context.CancelFunc

	// Dynamic peer list from registry.
	peers          []relayPeer // current active peers
	validatorOrder []string    // sorted validator names
	httpClient     *http.Client
	logf           func(format string, args ...any)
}

type relayPeer struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type registryResponse struct {
	Peers          []relayPeer `json:"peers"`
	URLs           []string    `json:"urls"`
	ValidatorOrder []string    `json:"validator_order"`
	Total          int         `json:"total"`
}

// RelayConfig holds configuration for NewRelayTransport.
type RelayConfig struct {
	RegistryURL    string
	SelfName       string
	SelfURL        string
	RegistrySecret string
	Cloud          string
	Logf           func(format string, args ...any)
}

// NewRelayTransport constructs a RelayTransport that speaks the Synthos
// HTTP peer protocol. The relays parameter is kept for backward compat
// but RegistryURL in config is preferred.
func NewRelayTransport(relays []string) *RelayTransport {
	registryURL := ""
	if len(relays) > 0 {
		registryURL = relays[0]
	}
	return &RelayTransport{
		RegistryURL:       registryURL,
		HeartbeatInterval: 30 * time.Second,
		topicHandlers:     make(map[string]func(string, []byte)),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// NewRelayTransportFromConfig constructs a fully configured RelayTransport.
func NewRelayTransportFromConfig(cfg RelayConfig) *RelayTransport {
	r := &RelayTransport{
		RegistryURL:       cfg.RegistryURL,
		SelfName:          cfg.SelfName,
		SelfURL:           cfg.SelfURL,
		RegistrySecret:    cfg.RegistrySecret,
		Cloud:             cfg.Cloud,
		HeartbeatInterval: 30 * time.Second,
		topicHandlers:     make(map[string]func(string, []byte)),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		logf: cfg.Logf,
	}
	if r.Cloud == "" {
		r.Cloud = "go-node"
	}
	return r
}

func (r *RelayTransport) log(format string, args ...any) {
	if r.logf != nil {
		r.logf(format, args...)
	}
}

// Peers returns the current active peer list (thread-safe).
func (r *RelayTransport) Peers() []relayPeer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]relayPeer, len(r.peers))
	copy(out, r.peers)
	return out
}

// ValidatorOrder returns the current sorted validator names.
func (r *RelayTransport) ValidatorOrder() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, len(r.validatorOrder))
	copy(out, r.validatorOrder)
	return out
}

// Start registers with the peer registry, fetches the initial peer list,
// and launches a background heartbeat goroutine.
func (r *RelayTransport) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started {
		return nil
	}
	r.started = true

	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel

	// Initial registration + peer fetch (best-effort).
	r.registerSelf(ctx)
	r.refreshPeers(ctx)

	// Background loops.
	go r.heartbeatLoop(ctx)
	go r.pollMailboxLoop(ctx)
	return nil
}

// Close shuts down the transport and deregisters from the peer registry.
func (r *RelayTransport) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.started {
		return nil
	}
	r.started = false
	if r.cancel != nil {
		r.cancel()
	}
	return nil
}

// SendToAgent sends a JSON payload to a specific agent by looking up
// its URL in the peer list and POSTing to /gossip/block.
func (r *RelayTransport) SendToAgent(agentID string, payload []byte) error {
	r.mu.RLock()
	var targetURL string
	for _, p := range r.peers {
		if p.Name == agentID {
			targetURL = p.URL
			break
		}
	}
	r.mu.RUnlock()

	if targetURL == "" {
		return fmt.Errorf("peer %q not found in registry", agentID)
	}

	return r.postGossip(targetURL, payload)
}

// Broadcast sends a message to ALL active peers via HTTP POST.
func (r *RelayTransport) Broadcast(topic string, payload []byte) error {
	r.mu.RLock()
	peers := make([]relayPeer, len(r.peers))
	copy(peers, r.peers)
	r.mu.RUnlock()

	var lastErr error
	for _, p := range peers {
		if p.Name == r.SelfName {
			continue // don't send to self
		}
		if err := r.postGossip(p.URL, payload); err != nil {
			r.log("[RELAY] broadcast to %s failed: %v", p.Name, err)
			lastErr = err
		}
	}
	return lastErr
}

// OnAgentMessage registers a handler for direct messages.
func (r *RelayTransport) OnAgentMessage(handler func(fromAgentID string, payload []byte)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agentHandler = handler
}

// OnTopicMessage registers a handler for messages on a topic.
func (r *RelayTransport) OnTopicMessage(topic string, handler func(fromAgentID string, payload []byte)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.topicHandlers[topic] = handler
}

// DeliverInbound is called by the RPC server when it receives an
// inbound gossip message (POST /gossip/block or /gossip/tx-batch).
// It dispatches to the registered handlers.
func (r *RelayTransport) DeliverInbound(fromAgentID string, payload []byte) {
	// Try to decode as envelope and route by topic.
	var env Envelope
	if err := json.Unmarshal(payload, &env); err == nil && env.Topic != "" {
		r.mu.RLock()
		handler := r.topicHandlers[env.Topic]
		r.mu.RUnlock()
		if handler != nil {
			handler(env.FromAgentID, payload)
			return
		}
	}

	// Fall through to direct agent handler.
	r.mu.RLock()
	handler := r.agentHandler
	r.mu.RUnlock()
	if handler != nil {
		handler(fromAgentID, payload)
	}
}

// ─── Internal: HTTP peer protocol ───────────────────────────────────────────

func (r *RelayTransport) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(r.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.registerSelf(ctx)
			r.refreshPeers(ctx)
		}
	}
}

func (r *RelayTransport) pollMailboxLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second) // Poll every 5s for mail
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.fetchMail(ctx)
		}
	}
}

func (r *RelayTransport) fetchMail(ctx context.Context) {
	if r.RegistryURL == "" || r.SelfName == "" {
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.RegistryURL+"/mailbox?name="+r.SelfName, nil)
	if err != nil {
		return
	}
	if r.RegistrySecret != "" {
		req.Header.Set("X-Registry-Secret", r.RegistrySecret)
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var messages [][]byte
		if err := json.NewDecoder(resp.Body).Decode(&messages); err == nil {
			for _, msg := range messages {
				r.DeliverInbound("relay", msg)
			}
		}
	}
}

func (r *RelayTransport) registerSelf(ctx context.Context) {
	if r.RegistryURL == "" || r.SelfName == "" || r.SelfURL == "" {
		return
	}

	body, _ := json.Marshal(map[string]string{
		"name":  r.SelfName,
		"url":   r.SelfURL,
		"cloud": r.Cloud,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.RegistryURL+"/register", bytes.NewReader(body))
	if err != nil {
		r.log("[RELAY] register request build failed: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if r.RegistrySecret != "" {
		req.Header.Set("X-Registry-Secret", r.RegistrySecret)
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		r.log("[RELAY] register failed: %v", err)
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode == http.StatusOK {
		r.log("[RELAY] registered as %s at %s", r.SelfName, r.SelfURL)
	} else {
		r.log("[RELAY] register returned %d", resp.StatusCode)
	}
}

func (r *RelayTransport) refreshPeers(ctx context.Context) {
	if r.RegistryURL == "" {
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.RegistryURL+"/peers/active", nil)
	if err != nil {
		return
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		r.log("[RELAY] peer refresh failed: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		r.log("[RELAY] peer refresh returned %d", resp.StatusCode)
		return
	}

	var data registryResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		r.log("[RELAY] peer refresh decode failed: %v", err)
		return
	}

	r.mu.Lock()
	r.peers = data.Peers
	r.validatorOrder = data.ValidatorOrder
	r.mu.Unlock()

	r.log("[RELAY] refreshed %d peers, order: %v", data.Total, data.ValidatorOrder)
}

func (r *RelayTransport) postGossip(peerURL string, payload []byte) error {
	// Determine endpoint: parse the envelope to check message type.
	endpoint := "/gossip/block" // default

	var env Envelope
	if err := json.Unmarshal(payload, &env); err == nil {
		switch env.MessageType {
		case "transaction":
			endpoint = "/gossip/tx-batch"
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, peerURL+endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gossip", "true")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("gossip to %s: %w", peerURL, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("gossip to %s returned %d", peerURL, resp.StatusCode)
	}
	return nil
}

