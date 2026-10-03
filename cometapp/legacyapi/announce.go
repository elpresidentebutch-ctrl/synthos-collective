package legacyapi

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// AnnounceMessage is the text a test node's PC signs to tell the website
// its current public address. It must match the website's
// TestnetAnnounceMessage (cmd/cloudless-registry/testnet.go) exactly.
func AnnounceMessage(network, apiURL string, timestamp int64) string {
	return fmt.Sprintf("SYNTHOS_TESTNET_ANNOUNCE_V1\nnetwork=%s\nurl=%s\ntimestamp=%d", network, apiURL, timestamp)
}

// Announce signs and sends this node's public address to the website.
func Announce(ctx context.Context, endpoint, network, apiURL string, key ed25519.PrivateKey, now time.Time) (string, error) {
	ts := now.Unix()
	body, _ := json.Marshal(map[string]any{
		"network":    network,
		"url":        apiURL,
		"timestamp":  ts,
		"public_key": "0x" + hex.EncodeToString(key.Public().(ed25519.PublicKey)),
		"signature":  "0x" + hex.EncodeToString(ed25519.Sign(key, []byte(AnnounceMessage(network, apiURL, ts)))),
	})
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	var out struct {
		OK     bool   `json:"ok"`
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	_ = json.Unmarshal(raw, &out)
	if res.StatusCode != http.StatusOK || !out.OK {
		if out.Error == "" {
			out.Error = fmt.Sprintf("HTTP %d", res.StatusCode)
		}
		return "", fmt.Errorf("website refused the address: %s", out.Error)
	}
	return out.Status, nil
}
