package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// withTestnetSite runs the test from a directory holding a
// website/assets/testnet.json that names the given announcer, with a fresh
// relay whose node check is stubbed.
func withTestnetSite(t *testing.T, announcer string, check func(context.Context, string, string) error) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "website", "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := map[string]any{"network": "synthos-testnet-2", "tx_chain_id": 20261002,
		"api_url": "https://fallback.trycloudflare.com", "announcer": announcer}
	raw, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(dir, "website", "assets", "testnet.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	wd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	old := testnet
	testnet = &testnetRelay{path: filepath.Join(dir, "relay.json"), check: check}
	t.Cleanup(func() { _ = os.Chdir(wd); testnet = old })
	return dir
}

type announcer struct {
	priv ed25519.PrivateKey
	pub  ed25519.PublicKey
}

func newAnnouncer(t *testing.T) announcer {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return announcer{priv, pub}
}

func (a announcer) body(network, apiURL string, ts int64) map[string]any {
	sig := ed25519.Sign(a.priv, []byte(TestnetAnnounceMessage(network, apiURL, ts)))
	return map[string]any{"network": network, "url": apiURL, "timestamp": ts,
		"public_key": "0x" + hex.EncodeToString(a.pub), "signature": "0x" + hex.EncodeToString(sig)}
}

func postAnnounce(t *testing.T, body any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	(&server{}).handleTestnetAnnounce(rec, httptest.NewRequest(http.MethodPost, "/api/testnet/announce", bytes.NewReader(raw)))
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func currentConfig(t *testing.T) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	(&server{}).handleTestnetConfig(rec, httptest.NewRequest(http.MethodGet, "/assets/testnet.json", nil))
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("config: %d %s", rec.Code, rec.Header().Get("Cache-Control"))
	}
	var cfg map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &cfg)
	return cfg
}

func TestTestnetAnnounceUpdatesAddress(t *testing.T) {
	a := newAnnouncer(t)
	checked := ""
	withTestnetSite(t, addressOfKey(a.pub), func(_ context.Context, u, n string) error {
		checked = u + " " + n
		return nil
	})
	if got := currentConfig(t)["api_url"]; got != "https://fallback.trycloudflare.com" {
		t.Fatalf("before any announcement api_url = %v", got)
	}
	now := time.Now().Unix()
	if code, out := postAnnounce(t, a.body("synthos-testnet-2", "https://new-words.trycloudflare.com", now)); code != 200 || out["status"] != "updated" {
		t.Fatalf("announce: %d %v", code, out)
	}
	if checked != "https://new-words.trycloudflare.com synthos-testnet-2" {
		t.Fatalf("node check called with %q", checked)
	}
	if got := currentConfig(t)["api_url"]; got != "https://new-words.trycloudflare.com" {
		t.Fatalf("api_url = %v", got)
	}

	// Repeating the same announcement is fine; an older one can't roll back.
	if code, out := postAnnounce(t, a.body("synthos-testnet-2", "https://new-words.trycloudflare.com", now)); code != 200 || out["status"] != "already current" {
		t.Fatalf("repeat: %d %v", code, out)
	}
	if code, _ := postAnnounce(t, a.body("synthos-testnet-2", "https://older.trycloudflare.com", now-60)); code != http.StatusConflict {
		t.Fatalf("older announcement: HTTP %d", code)
	}

	// It survives a website restart (a new relay reading the same file).
	path := testnet.path
	testnet = &testnetRelay{path: path, check: testnet.check}
	if got := currentConfig(t)["api_url"]; got != "https://new-words.trycloudflare.com" {
		t.Fatalf("after restart api_url = %v", got)
	}
}

func TestTestnetAnnounceRejects(t *testing.T) {
	a := newAnnouncer(t)
	withTestnetSite(t, addressOfKey(a.pub), func(context.Context, string, string) error { return nil })
	now := time.Now().Unix()
	good := "https://ok.trycloudflare.com"

	other := newAnnouncer(t)
	if code, _ := postAnnounce(t, other.body("synthos-testnet-2", good, now)); code != http.StatusForbidden {
		t.Fatalf("someone else's key: HTTP %d", code)
	}
	tampered := a.body("synthos-testnet-2", good, now)
	tampered["url"] = "https://evil.trycloudflare.com"
	if code, _ := postAnnounce(t, tampered); code != http.StatusForbidden {
		t.Fatalf("tampered url: HTTP %d", code)
	}
	if code, _ := postAnnounce(t, a.body("synthos-testnet-2", good, now-3600)); code != http.StatusBadRequest {
		t.Fatalf("stale timestamp: HTTP %d", code)
	}
	if code, _ := postAnnounce(t, a.body("synthos-mainnet-1", good, now)); code != http.StatusBadRequest {
		t.Fatalf("wrong network: HTTP %d", code)
	}
	for _, bad := range []string{"http://ok.trycloudflare.com", "https://1.2.3.4", "https://ok.trycloudflare.com/x",
		"https://ok.trycloudflare.com:8443", "https://user@ok.trycloudflare.com", "https://localhost"} {
		if code, _ := postAnnounce(t, a.body("synthos-testnet-2", bad, now)); code != http.StatusBadRequest {
			t.Errorf("url %q: HTTP %d", bad, code)
		}
	}
	if got := currentConfig(t)["api_url"]; got != "https://fallback.trycloudflare.com" {
		t.Fatalf("a rejected announcement changed api_url to %v", got)
	}
}

func TestTestnetAnnounceNeedsLiveNode(t *testing.T) {
	a := newAnnouncer(t)
	withTestnetSite(t, addressOfKey(a.pub), func(context.Context, string, string) error {
		return errors.New("the node isn't answering at that address yet")
	})
	if code, out := postAnnounce(t, a.body("synthos-testnet-2", "https://dead.trycloudflare.com", time.Now().Unix())); code != http.StatusBadGateway {
		t.Fatalf("dead node: %d %v", code, out)
	}
	if got := currentConfig(t)["api_url"]; got != "https://fallback.trycloudflare.com" {
		t.Fatalf("api_url = %v", got)
	}
}

func TestCheckTestnetNode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"chain_id": "synthos-testnet-2"})
	}))
	defer srv.Close()
	if err := checkTestnetNode(context.Background(), srv.URL, "synthos-testnet-2"); err != nil {
		t.Fatal(err)
	}
	if err := checkTestnetNode(context.Background(), srv.URL, "synthos-testnet-9"); err == nil {
		t.Fatal("wrong chain accepted")
	}
}
