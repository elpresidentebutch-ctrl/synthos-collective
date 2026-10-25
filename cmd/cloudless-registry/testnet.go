package main

// The test network node runs on a home PC behind a free Cloudflare tunnel,
// whose public address changes whenever the tunnel restarts. The node's PC
// announces each new address here, signed with the test network's faucet
// key; the website then hands the current address to the /testnet page
// through /assets/testnet.json.

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// TestnetAnnounceMessage is the exact text the announcer signs.
func TestnetAnnounceMessage(network, apiURL string, timestamp int64) string {
	return fmt.Sprintf("SYNTHOS_TESTNET_ANNOUNCE_V1\nnetwork=%s\nurl=%s\ntimestamp=%d", network, apiURL, timestamp)
}

const testnetAnnounceMaxSkew = 10 * time.Minute

type testnetAnnouncement struct {
	Network   string `json:"network"`
	URL       string `json:"url"`
	Timestamp int64  `json:"timestamp"`
	Accepted  string `json:"accepted"`
}

type testnetRelay struct {
	mu      sync.Mutex
	path    string // where the latest announcement is kept ("" = memory)
	current *testnetAnnouncement
	loaded  bool
	// check confirms an announced address really serves the network
	// (replaced in tests).
	check func(ctx context.Context, apiURL, network string) error
}

var testnet = &testnetRelay{check: checkTestnetNode}

// testnetConfig reads website/assets/testnet.json.
func testnetConfig() (map[string]any, error) {
	for _, p := range []string{"/website/assets/testnet.json", "website/assets/testnet.json"} {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var cfg map[string]any
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return nil, fmt.Errorf("testnet.json: %w", err)
		}
		return cfg, nil
	}
	return nil, os.ErrNotExist
}

func (t *testnetRelay) load() {
	if t.loaded {
		return
	}
	t.loaded = true
	if t.path == "" {
		return
	}
	raw, err := os.ReadFile(t.path)
	if err != nil {
		return
	}
	var a testnetAnnouncement
	if json.Unmarshal(raw, &a) == nil && a.URL != "" {
		t.current = &a
	}
}

func (t *testnetRelay) save() {
	if t.path == "" || t.current == nil {
		return
	}
	raw, err := json.Marshal(t.current)
	if err != nil {
		return
	}
	tmp := t.path + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		_ = os.Rename(tmp, t.path)
	}
}

// validTestnetURL accepts a bare https origin with a DNS name (no IP
// literal, port, path, query or credentials) and returns it normalized.
func validTestnetURL(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	if u.Path != "" && u.Path != "/" {
		return "", false
	}
	host := strings.ToLower(u.Host)
	if host == "" || strings.Contains(host, ":") || net.ParseIP(host) != nil || !strings.Contains(host, ".") {
		return "", false
	}
	for _, c := range host {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '.') {
			return "", false
		}
	}
	return "https://" + host, true
}

func checkTestnetNode(ctx context.Context, apiURL, network string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL+"/status", nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("the node isn't answering at that address yet: %v", err)
	}
	defer res.Body.Close()
	var st struct {
		ChainID string `json:"chain_id"`
	}
	if res.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&st) != nil {
		return fmt.Errorf("that address didn't answer like a SYNTHOS node (HTTP %d)", res.StatusCode)
	}
	if st.ChainID != network {
		return fmt.Errorf("that node runs %q, not %q", st.ChainID, network)
	}
	return nil
}

func addressOfKey(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return "0x" + hex.EncodeToString(sum[:20])
}

// handleTestnetAnnounce: POST /api/testnet/announce
//
//	{"network","url","timestamp","public_key","signature"}
func (s *server) handleTestnetAnnounce(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method not allowed"})
		return
	}
	var req struct {
		Network   string `json:"network"`
		URL       string `json:"url"`
		Timestamp int64  `json:"timestamp"`
		PublicKey string `json:"public_key"`
		Signature string `json:"signature"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "bad json"})
		return
	}
	fail := func(code int, msg string) { writeJSON(w, code, map[string]any{"ok": false, "error": msg}) }

	cfg, err := testnetConfig()
	if err != nil {
		fail(http.StatusServiceUnavailable, "no test network is configured on this site")
		return
	}
	network, _ := cfg["network"].(string)
	announcer, _ := cfg["announcer"].(string)
	if network == "" || announcer == "" {
		fail(http.StatusServiceUnavailable, "testnet.json has no network or announcer")
		return
	}
	if req.Network != network {
		fail(http.StatusBadRequest, fmt.Sprintf("this site follows %q, not %q", network, req.Network))
		return
	}
	apiURL, ok := validTestnetURL(req.URL)
	if !ok || apiURL != req.URL {
		fail(http.StatusBadRequest, "url must be a bare https address like https://name.trycloudflare.com")
		return
	}
	pub, err1 := hex.DecodeString(strings.TrimPrefix(req.PublicKey, "0x"))
	sig, err2 := hex.DecodeString(strings.TrimPrefix(req.Signature, "0x"))
	if err1 != nil || err2 != nil || len(pub) != ed25519.PublicKeySize || len(sig) != ed25519.SignatureSize {
		fail(http.StatusBadRequest, "bad public key or signature")
		return
	}
	if !strings.EqualFold(addressOfKey(pub), announcer) {
		fail(http.StatusForbidden, "this key may not announce the test network's address")
		return
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), []byte(TestnetAnnounceMessage(req.Network, req.URL, req.Timestamp)), sig) {
		fail(http.StatusForbidden, "bad signature")
		return
	}
	now := time.Now()
	if d := now.Sub(time.Unix(req.Timestamp, 0)); d > testnetAnnounceMaxSkew || d < -testnetAnnounceMaxSkew {
		fail(http.StatusBadRequest, "timestamp is too far from the current time")
		return
	}

	t := testnet
	t.mu.Lock()
	t.load()
	if t.current != nil && req.Timestamp <= t.current.Timestamp {
		same := t.current.URL == apiURL
		t.mu.Unlock()
		if same {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "url": apiURL, "status": "already current"})
		} else {
			fail(http.StatusConflict, "a newer address was already announced")
		}
		return
	}
	t.mu.Unlock()

	if err := t.check(r.Context(), apiURL, network); err != nil {
		fail(http.StatusBadGateway, err.Error())
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if t.current != nil && req.Timestamp <= t.current.Timestamp {
		fail(http.StatusConflict, "a newer address was already announced")
		return
	}
	t.current = &testnetAnnouncement{Network: network, URL: apiURL, Timestamp: req.Timestamp, Accepted: now.UTC().Format(time.RFC3339)}
	t.save()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "url": apiURL, "status": "updated"})
}

// handleTestnetConfig serves /assets/testnet.json with the latest
// announced address in place of the file's api_url.
func (s *server) handleTestnetConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method not allowed"})
		return
	}
	cfg, err := testnetConfig()
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	t := testnet
	t.mu.Lock()
	t.load()
	if t.current != nil && t.current.Network == cfg["network"] {
		cfg["api_url"] = t.current.URL
		cfg["api_url_updated"] = t.current.Accepted
	}
	t.mu.Unlock()
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, cfg)
}

// testnetRelayPath keeps announcements next to the registry's state file.
func testnetRelayPath(stateFile string) string {
	if stateFile == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(stateFile), "testnet-announce.json")
}
