package legacyapi

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The website verifies exactly this text; changing it breaks announcing.
func TestAnnounceMessageFormat(t *testing.T) {
	want := "SYNTHOS_TESTNET_ANNOUNCE_V1\nnetwork=synthos-testnet-2\nurl=https://a-b.trycloudflare.com\ntimestamp=1790000000"
	if got := AnnounceMessage("synthos-testnet-2", "https://a-b.trycloudflare.com", 1790000000); got != want {
		t.Fatalf("message = %q", got)
	}
}

func TestAnnounceSignsAndReportsRefusals(t *testing.T) {
	key, _, _ := NewFaucetKey()
	now := time.Unix(1790000000, 0)
	var got map[string]any
	refuse := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		if refuse {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"ok":false,"error":"the node isn't answering at that address yet"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"status":"updated"}`))
	}))
	defer srv.Close()

	status, err := Announce(context.Background(), srv.URL, "synthos-testnet-2", "https://a-b.trycloudflare.com", key, now)
	if err != nil || status != "updated" {
		t.Fatalf("announce: %q %v", status, err)
	}
	pub, _ := hex.DecodeString(strings.TrimPrefix(got["public_key"].(string), "0x"))
	sig, _ := hex.DecodeString(strings.TrimPrefix(got["signature"].(string), "0x"))
	msg := AnnounceMessage(got["network"].(string), got["url"].(string), int64(got["timestamp"].(float64)))
	if !ed25519.Verify(pub, []byte(msg), sig) || got["timestamp"].(float64) != 1790000000 {
		t.Fatalf("bad signed announcement: %v", got)
	}

	refuse = true
	if _, err := Announce(context.Background(), srv.URL, "synthos-testnet-2", "https://a-b.trycloudflare.com", key, now); err == nil || !strings.Contains(err.Error(), "isn't answering") {
		t.Fatalf("refusal not reported: %v", err)
	}
}
